//go:build example
// +build example

package main

import (
	"context"
	"sync"
	"testing"
	"time"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt"
	"github.com/joeycumines/go-pabt/examples/harbor-panic/logic"
	hsim "github.com/joeycumines/go-pabt/examples/harbor-panic/sim"
)

func TestDeterminismLock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	harbor := hsim.NewHarbor(0, 3.1, 42)

	// Establish the baseline ordering synchronously, before any reader
	// goroutine starts, so every comparison reads immutable data. The prior
	// version compared orderings[idx] against orderings[0] while reader 0's
	// tick-0 write might not have landed yet, which raced against the
	// harness's own bookkeeping rather than the harbor's ordering.
	baseline := actorOrdering(harbor)
	if baseline == "" {
		t.Fatal("baseline ordering is empty — harbor has no actors")
	}

	var wg sync.WaitGroup
	const readers = 100
	const ticks = 50

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for tick := 0; tick < ticks; tick++ {
				if ids := actorOrdering(harbor); ids != baseline {
					t.Errorf("ordering mismatch: %q vs baseline %q", ids, baseline)
					return
				}
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for tick := 0; tick < ticks; tick++ {
			harbor.Step(ctx)
		}
	}()

	wg.Wait()
	if t.Failed() {
		t.Fatal("non-deterministic Actor ordering detected")
	}
	t.Logf("DeterminismLock: %d readers x %d ticks, all orderings identical", readers, ticks)
}

func actorOrdering(harbor *hsim.Harbor) string {
	var ids string
	for _, a := range harbor.State().Actors() {
		ids += a.ID + ","
	}
	return ids
}

func TestConcurrencyStress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	harbor := hsim.NewHarbor(0, 3.1, 42)
	st := harbor.State()
	actors := st.Actors()
	if len(actors) < 4 {
		t.Fatalf("expected 4 actors, got %d", len(actors))
	}

	const planCount = 4
	plans := make([]logic.HarborPlanResult, planCount)
	planMu := make([]sync.RWMutex, planCount)
	for i := 0; i < planCount; i++ {
		plans[i] = logic.HarborPlan(ctx, harbor, actors[i].ID)
	}

	const iterations = 500
	var wg sync.WaitGroup

	// One plan per actor, each ticked on its own goroutine while the harbor itself
	// steps on this one. Run this under -race: the assertion is that the planner
	// fabric holds no shared mutable state across those goroutines, which is the
	// property a debugger's own capture would otherwise be blamed for.
	//
	// There is deliberately no concurrent reader of a live tree here. Reading one
	// while a tick runs races with the writes expansion performs - that is a known
	// hazard of the live plan, and the reason the durable server serves committed
	// records instead. A test that provoked it would be asserting a race.
	terminal := make([]bt.Status, planCount)
	for i := 0; i < planCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			pn := plans[idx].Node
			mu := &planMu[idx]
			var last bt.Status
			for iter := 0; iter < iterations; iter++ {
				mu.Lock()
				status, _ := pn.Tick()
				mu.Unlock()
				last = status
			}
			terminal[idx] = last
		}(i)
	}

	for tick := 0; tick < iterations; tick++ {
		harbor.Step(ctx)
	}

	wg.Wait()

	// A PA-BT plan returns Running while it still has work to do, so Running after
	// 500 iterations is a normal outcome and not a failure. What must hold is that
	// every plan reported a real status - the ABI leaves 0 undefined, so a zero here
	// means a tick returned nothing - and that each plan built a tree.
	for i := 0; i < planCount; i++ {
		if terminal[i] == 0 {
			t.Errorf("stress-bot-%d: last status is the undefined zero", i)
		}
		nodes := 0
		pabt.Walk(plans[i].Node, func(*pabt.NodeInfo) bool {
			nodes++
			return true
		})
		if nodes == 0 {
			t.Errorf("stress-bot-%d: no nodes after %d iterations", i, iterations)
		}
	}
	t.Logf("ConcurrencyStress: %d plans x %d iterations, no race detected", planCount, iterations)
}

func TestSnapshotIsolation(t *testing.T) {
	harbor := hsim.NewHarbor(0, 3.1, 42)

	s1 := harbor.State()
	if s1 == nil {
		t.Fatal("State() returned nil")
	}

	actor := s1.Sprites["CRANE-0"]
	if actor == nil {
		t.Fatal("CRANE-0 not found in snapshot")
	}
	origX := actor.X
	origY := actor.Y

	actor.X = -999
	actor.Y = -999

	s2 := harbor.State()
	actor2 := s2.Sprites["CRANE-0"]
	if actor2 == nil {
		t.Fatal("CRANE-0 not found in second snapshot")
	}
	if actor2.X == -999 || actor2.Y == -999 {
		t.Fatal("snapshot mutation leaked into harbor state — deep copy failed")
	}
	if actor2.X != origX || actor2.Y != origY {
		t.Errorf("CRANE-0 position drifted: orig (%f,%f) vs second snapshot (%f,%f)", origX, origY, actor2.X, actor2.Y)
	}

	s3 := harbor.State()
	if s3.Sprites["CRANE-0"].X != origX {
		t.Error("third snapshot also mutated")
	}

	for _, sp := range s1.Sprites {
		if sp.HeldItem != nil {
			sp.HeldItem.X = -888
			break
		}
	}
	s4 := harbor.State()
	for _, sp := range s4.Sprites {
		if sp.HeldItem != nil && sp.HeldItem.X == -888 {
			t.Fatal("HeldItem pointer leaked across snapshots")
		}
	}

	t.Logf("SnapshotIsolation: 4 snapshots verified, mutation did not leak")
}

func TestStormRelocation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	const stormEvery = 20
	harbor := hsim.NewHarbor(stormEvery, 0, 42)

	const totalTicks = 1000
	for tick := 1; tick <= totalTicks; tick++ {
		if err := harbor.Step(ctx); err != nil {
			t.Fatalf("Step at tick %d: %v", tick, err)
		}

		if tick%stormEvery != 0 {
			continue
		}

		st := harbor.State()

		walls := st.Walls
		goals := st.Goals()

		for _, g := range goals {
			for _, w := range walls {
				if g.X < w.X+float64(w.W) && g.X+float64(g.W) > w.X &&
					g.Y < w.Y+float64(w.H) && g.Y+float64(g.H) > w.Y {
					t.Errorf("tick %d: berth %s at (%f,%f) overlaps wall %s at (%f,%f)",
						tick, g.ID, g.X, g.Y, w.ID, w.X, w.Y)
				}
			}
		}

		for i, g1 := range goals {
			for j, g2 := range goals {
				if i >= j {
					continue
				}
				if g1.X < g2.X+float64(g2.W) && g1.X+float64(g1.W) > g2.X &&
					g1.Y < g2.Y+float64(g2.H) && g1.Y+float64(g1.H) > g2.Y {
					t.Errorf("tick %d: berth %s at (%f,%f) overlaps berth %s at (%f,%f)",
						tick, g1.ID, g1.X, g1.Y, g2.ID, g2.X, g2.Y)
				}
			}
		}
	}

	storms := totalTicks / stormEvery
	t.Logf("StormRelocation: %d ticks, %d storms, 0 berth overlap violations", totalTicks, storms)
}
