//go:build example

package world

import (
	"testing"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
)

func TestStepAdvancesRound(t *testing.T) {
	m := maze.Generate(42, 11, 11)
	w := New(m)
	if w.Round != 0 {
		t.Fatalf("initial round = %d, want 0", w.Round)
	}
	w.Step(1.0 / 30.0)
	if w.Round != 1 {
		t.Fatalf("round after one step = %d, want 1", w.Round)
	}
	w.Step(1.0 / 30.0)
	if w.Round != 2 {
		t.Fatalf("round after two steps = %d, want 2", w.Round)
	}
}

func TestSnapshotDefensiveCopy(t *testing.T) {
	m := maze.Generate(42, 11, 11)
	w := New(m)
	w.Minotaurs = append(w.Minotaurs, &Minotaur{ID: "M-1", Pos: m.Start})

	snap := w.Copy()
	if len(snap.Minotaurs) != 1 {
		t.Fatalf("snapshot minotaurs = %d, want 1", len(snap.Minotaurs))
	}
	if snap.Minotaurs[0].ID != "M-1" {
		t.Fatalf("snapshot minotaur ID = %q, want M-1", snap.Minotaurs[0].ID)
	}

	// Mutate the world after snapshot.
	w.Minotaurs[0].ID = "M-CHANGED"
	w.Round = 99

	// Snapshot must be unaffected.
	if snap.Minotaurs[0].ID != "M-1" {
		t.Fatalf("snapshot mutated: minotaur ID = %q", snap.Minotaurs[0].ID)
	}
	if snap.Round != 0 {
		t.Fatalf("snapshot mutated: round = %d", snap.Round)
	}
}

func TestSnapshotRunnerIsValueCopy(t *testing.T) {
	m := maze.Generate(42, 11, 11)
	w := New(m)
	snap := w.Copy()
	originalPos := snap.Runner.Pos

	w.Runner.Pos.X = 999
	w.Runner.Pos.Y = 999

	if snap.Runner.Pos != originalPos {
		t.Fatalf("snapshot runner position mutated: %v", snap.Runner.Pos)
	}
}

func TestStepFreezesTerminalPhase(t *testing.T) {
	m := maze.Generate(42, 11, 11)
	w := New(m)
	w.Runner.Pos = m.Start
	w.Phase = PhaseLost

	w.Step(1.0 / 30.0)
	if w.Round != 0 {
		t.Fatalf("round advanced after loss: %d", w.Round)
	}
	if w.Runner.Pos != m.Start {
		t.Fatalf("runner moved after loss: %v", w.Runner.Pos)
	}
}
