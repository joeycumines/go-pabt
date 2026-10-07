//go:build example

package loop

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

func TestCommandAppliedAtTickBoundary(t *testing.T) {
	m := maze.Generate(42, 11, 11)
	w := world.New(m)
	l := New(w)

	var applied atomic.Bool
	l.Submit(func(w *world.World) {
		applied.Store(true)
	})

	// Before any tick, the command must not have been applied.
	if applied.Load() {
		t.Fatal("command applied before tick")
	}

	// Tick once.
	l.TickOnce()
	if !applied.Load() {
		t.Fatal("command not applied after tick")
	}
}

func TestMidTickSubmissionAppliesNextTick(t *testing.T) {
	m := maze.Generate(42, 11, 11)
	w := world.New(m)
	l := New(w)

	// Tick 1: submit a command during the tick via a command.
	var secondApplied atomic.Bool
	l.Submit(func(w *world.World) {
		// This runs during tick 1. Submit another command here.
		l.Submit(func(w *world.World) {
			secondApplied.Store(true)
		})
	})

	l.TickOnce() // tick 1: first command runs, submits second
	if secondApplied.Load() {
		t.Fatal("second command applied during the same tick it was submitted")
	}

	l.TickOnce() // tick 2: second command should now apply
	if !secondApplied.Load() {
		t.Fatal("second command not applied at next tick")
	}
}

func TestSnapshotPublishedAfterTick(t *testing.T) {
	m := maze.Generate(42, 11, 11)
	w := world.New(m)
	l := New(w)

	snap := l.Snapshot()
	if snap.Round != 0 {
		t.Fatalf("initial snapshot round = %d, want 0", snap.Round)
	}

	l.TickOnce()
	snap = l.Snapshot()
	if snap.Round != 1 {
		t.Fatalf("snapshot round after tick = %d, want 1", snap.Round)
	}
}

func TestSnapshotImmunity(t *testing.T) {
	m := maze.Generate(42, 11, 11)
	w := world.New(m)
	w.Minotaurs = append(w.Minotaurs, &world.Minotaur{ID: "M-1"})
	l := New(w)

	snap := l.Snapshot()
	if len(snap.Minotaurs) != 1 || snap.Minotaurs[0].ID != "M-1" {
		t.Fatalf("snapshot minotaurs wrong: %+v", snap.Minotaurs)
	}

	// Mutate via a command.
	l.Submit(func(w *world.World) {
		w.Minotaurs[0].ID = "CHANGED"
	})
	l.TickOnce()

	// Old snapshot must be unaffected.
	if snap.Minotaurs[0].ID != "M-1" {
		t.Fatalf("old snapshot mutated: %q", snap.Minotaurs[0].ID)
	}

	// New snapshot reflects the change.
	snap2 := l.Snapshot()
	if snap2.Minotaurs[0].ID != "CHANGED" {
		t.Fatalf("new snapshot not updated: %q", snap2.Minotaurs[0].ID)
	}
}

func TestTickRate(t *testing.T) {
	m := maze.Generate(42, 11, 11)
	w := world.New(m)
	l := New(w)

	// Run for ~10 seconds and verify we get roughly 300 ticks (30 Hz).
	// Acceptance: "the loop holds 30 Hz within tolerance over a 10 s measured run".
	done := make(chan struct{})
	go func() {
		l.Run(t.Context())
		close(done)
	}()

	time.Sleep(10 * time.Second)
	l.Stop()
	<-done

	round := l.Round()
	// 30 Hz * 10s = 300 ticks. Allow 20% tolerance for CI scheduler jitter.
	if round < 240 || round > 360 {
		t.Fatalf("tick rate off: got %d rounds in ~10s, want ~300 (240-360)", round)
	}
}
