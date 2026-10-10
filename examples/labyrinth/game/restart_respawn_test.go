//go:build example

package game

import (
	"testing"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

// TestWorldResetOnAddSessionAfterWin verifies the B2 restart fix: after the
// world reaches a terminal phase, AddSession submits a reset command that
// restores a fresh running game on the next tick.
func TestWorldResetOnAddSessionAfterWin(t *testing.T) {
	m := maze.Generate(42, 21, 21)
	g := NewGame(m, 8, 42)

	// Drive the world to a terminal phase.
	g.World.Phase = world.PhaseWon
	g.World.Runner.HP = 0
	g.World.Runner.Pos = maze.Vec2{X: 5.5, Y: 5.5}

	// AddSession must queue a reset.
	_, _ = g.AddSession("")

	// Run one tick to apply the queued command.
	g.Loop.TickOnce()

	if g.World.Phase != world.PhaseRunning {
		t.Fatalf("phase = %v, want PhaseRunning after reset", g.World.Phase)
	}
	if g.World.Runner.HP != world.MaxHP {
		t.Fatalf("runner HP = %d, want %d", g.World.Runner.HP, world.MaxHP)
	}
	if g.World.Runner.Pos != m.Start {
		t.Fatalf("runner pos = %v, want maze start %v", g.World.Runner.Pos, m.Start)
	}
	if len(g.World.Minotaurs) != TargetActorCount {
		t.Fatalf("roster = %d actors, want %d", len(g.World.Minotaurs), TargetActorCount)
	}
	// Roster must be faction-balanced after reset.
	counts := [4]int{}
	for _, mm := range g.World.Minotaurs {
		counts[mm.Element]++
	}
	for e := 0; e < 4; e++ {
		if counts[e] == 0 {
			t.Fatalf("element %d has no actors after reset; counts=%v", e, counts)
		}
	}
}

// TestWorldResetOnRejoinAfterLoss verifies that a rejoining session also
// triggers the reset when the world is terminal.
func TestWorldResetOnRejoinAfterLoss(t *testing.T) {
	m := maze.Generate(42, 21, 21)
	g := NewGame(m, 8, 42)

	// Create a session, then disconnect it.
	sess, token := g.AddSession("")
	g.RemoveSession(sess.ID)

	// Drive the world to lost.
	g.World.Phase = world.PhaseLost

	// Rejoin with the same token.
	_, gotToken := g.AddSession(token)
	if gotToken != token {
		t.Fatalf("rejoin token = %q, want %q", gotToken, token)
	}

	g.Loop.TickOnce()

	if g.World.Phase != world.PhaseRunning {
		t.Fatalf("phase = %v, want PhaseRunning after rejoin reset", g.World.Phase)
	}
	if g.World.Runner.HP != world.MaxHP {
		t.Fatalf("runner HP = %d, want %d", g.World.Runner.HP, world.MaxHP)
	}
}

// TestNoResetWhenPhaseRunning verifies that AddSession does not reset a
// running world, preserving in-progress gameplay.
func TestNoResetWhenPhaseRunning(t *testing.T) {
	m := maze.Generate(42, 21, 21)
	g := NewGame(m, 8, 42)

	// Mutate the world so a reset would be visible.
	g.World.Runner.HP = 42
	g.World.Round = 123

	_, _ = g.AddSession("")
	g.Loop.TickOnce()

	if g.World.Runner.HP != 42 {
		t.Fatalf("runner HP = %d, want 42 (no reset)", g.World.Runner.HP)
	}
	// Round advances by 1 per tick; 123 + 1 = 124.
	if g.World.Round != 124 {
		t.Fatalf("round = %d, want 124 (no reset)", g.World.Round)
	}
}

// TestRespawnBalancesFactions verifies the B3 fix: respawning picks the
// least-populated element so all four factions maintain presence.
func TestRespawnBalancesFactions(t *testing.T) {
	m := maze.Generate(42, 21, 21)
	g := NewGame(m, 8, 42)
	w := g.World

	// Kill all fire actors (element 0).
	for _, mm := range w.Minotaurs {
		if mm.Element == 0 {
			mm.HP = 0
		}
	}

	// Respawn: should remove dead actors and refill to TargetActorCount,
	// prioritizing fire (the least populated element).
	g.respawnDeadActors(w)

	if len(w.Minotaurs) != TargetActorCount {
		t.Fatalf("roster = %d, want %d", len(w.Minotaurs), TargetActorCount)
	}

	counts := [4]int{}
	for _, mm := range w.Minotaurs {
		counts[mm.Element]++
	}
	if counts[0] == 0 {
		t.Fatalf("fire faction has no actors after respawn; counts=%v", counts)
	}
	// No faction should dominate: max count should be reasonable.
	for e := 0; e < 4; e++ {
		if counts[e] > TargetActorCount/2+1 {
			t.Fatalf("element %d dominates with %d actors; counts=%v", e, counts[e], counts)
		}
	}
}

// TestLeastPopulatedElementTieBreak verifies deterministic tie-breaking by
// lowest element index.
func TestLeastPopulatedElementTieBreak(t *testing.T) {
	m := maze.Generate(42, 21, 21)
	g := NewGame(m, 8, 42)
	w := g.World

	// Empty roster: all counts zero, should pick element 0.
	w.Minotaurs = nil
	if got := g.leastPopulatedElement(w); got != ElementFire {
		t.Fatalf("leastPopulatedElement(empty) = %v, want ElementFire", got)
	}

	// Equal counts: should pick element 0.
	w.Minotaurs = []*world.Minotaur{
		{Element: 0, HP: 10},
		{Element: 1, HP: 10},
		{Element: 2, HP: 10},
		{Element: 3, HP: 10},
	}
	if got := g.leastPopulatedElement(w); got != ElementFire {
		t.Fatalf("leastPopulatedElement(equal) = %v, want ElementFire", got)
	}

	// Element 2 is least populated.
	w.Minotaurs = []*world.Minotaur{
		{Element: 0, HP: 10},
		{Element: 0, HP: 10},
		{Element: 1, HP: 10},
		{Element: 3, HP: 10},
	}
	if got := g.leastPopulatedElement(w); got != ElementLightning {
		t.Fatalf("leastPopulatedElement = %v, want ElementLightning", got)
	}
}
