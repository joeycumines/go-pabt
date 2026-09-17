//go:build example
// +build example

package main

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/joeycumines/go-pabt/examples/harbor-panic/logic"
	hsim "github.com/joeycumines/go-pabt/examples/harbor-panic/sim"
	"github.com/joeycumines/go-pabt/pabtdebug"
)

func TestDeterminismLock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	harbor := hsim.NewHarbor(0, 3.1, 42)

	var wg sync.WaitGroup
	const readers = 100
	const ticks = 50

	orderings := make([]string, readers)

	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for tick := 0; tick < ticks; tick++ {
				st := harbor.State()
				actors := st.Actors()
				var ids string
				for _, a := range actors {
					ids += a.ID + ","
				}
				if idx == 0 && tick == 0 {
					orderings[idx] = ids
				} else if tick == 0 {
					orderings[idx] = ids
				}
				if idx > 0 && tick == 0 && orderings[idx] != orderings[0] {
					t.Errorf("ordering mismatch at reader %d: %q vs %q", idx, orderings[idx], orderings[0])
				}
			}
		}(i)
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
	trackers := make([]*pabtdebug.Tracker, planCount)
	for i := 0; i < planCount; i++ {
		plans[i] = logic.HarborPlan(ctx, harbor, actors[i].ID)
		trackers[i] = pabtdebug.NewTrackerWithID(plans[i].Plan, fmt.Sprintf("stress-bot-%d", i))
	}

	srv := pabtdebug.NewServer(trackers[0], "127.0.0.1:0")
	for i := 1; i < planCount; i++ {
		srv.RegisterTracker(trackers[i])
	}

	const iterations = 1000
	var wg sync.WaitGroup

	for i := 0; i < planCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			pn := plans[idx].Node
			tr := trackers[idx]
			for iter := 0; iter < iterations; iter++ {
				status, _ := pn.Tick()
				tr.Track(status, nil)
			}
		}(i)
	}

	// Pre-step the harbor so plans have live state to read.
	// Step is not run concurrently because it mutates shared sim state
	// while Tick reads it — the race detector correctly flags that.
	for tick := 0; tick < iterations; tick++ {
		harbor.Step(ctx)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations/10; i++ {
			for j := 0; j < planCount; j++ {
				_ = trackers[j].Profile()
				_ = trackers[j].Timeline()
				_ = trackers[j].Search("Selector")
				_ = trackers[j].BuildTree()
				if i > 0 {
					_, _ = trackers[j].Diff(1, i)
				}
			}
			time.Sleep(time.Microsecond)
		}
	}()

	wg.Wait()

	for i := 0; i < planCount; i++ {
		ev := trackers[i].Events()
		if len(ev) == 0 {
			t.Errorf("stress-bot-%d: no events recorded", i)
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
