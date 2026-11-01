//go:build example

package game

import (
	mrand "math/rand"
	"testing"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

func TestSigilsSpawnFourQuadrants(t *testing.T) {
	m := maze.Generate(21, 21, 12345)
	w := world.New(m)
	rng := mrand.New(mrand.NewSource(12345))

	spawnSigils(w, m, rng)

	if len(w.Sigils) != 4 {
		t.Fatalf("expected 4 sigils, got %d", len(w.Sigils))
	}
	if w.SigilsHeld != 0 {
		t.Fatalf("expected 0 sigils held initially, got %d", w.SigilsHeld)
	}

	foundElems := make(map[int]bool)
	for i, sig := range w.Sigils {
		if sig.Taken {
			t.Errorf("sigil %d was marked taken on spawn", i)
		}
		if !m.At(int(sig.Pos.X), int(sig.Pos.Y)).Walkable() {
			t.Errorf("sigil %d at (%v, %v) is not walkable", i, sig.Pos.X, sig.Pos.Y)
		}
		foundElems[sig.Element] = true
	}
	for elem := 0; elem < 4; elem++ {
		if !foundElems[elem] {
			t.Errorf("element %d has no corresponding sigil", elem)
		}
	}
}

func TestGateLockedBelowThreeSigils(t *testing.T) {
	m := maze.Generate(21, 21, 12345)
	g := NewGame(m, 0, 12345)

	// Position runner directly at the gate with 0 sigils.
	g.World.Runner.Pos = m.Gate
	g.World.SigilsHeld = 0
	g.tickLogic(g.World)

	if g.World.Phase == world.PhaseWon {
		t.Fatalf("gate must remain locked with 0 sigils, but game was won")
	}

	// With 1 and 2 sigils, gate remains locked.
	g.World.SigilsHeld = 1
	g.tickLogic(g.World)
	if g.World.Phase == world.PhaseWon {
		t.Fatalf("gate must remain locked with 1 sigil")
	}

	g.World.SigilsHeld = 2
	g.tickLogic(g.World)
	if g.World.Phase == world.PhaseWon {
		t.Fatalf("gate must remain locked with 2 sigils")
	}

	// With 3 sigils, reaching the gate triggers PhaseWon.
	g.World.SigilsHeld = 3
	g.tickLogic(g.World)
	if g.World.Phase != world.PhaseWon {
		t.Fatalf("expected PhaseWon with 3 sigils, got %v", g.World.Phase)
	}
}

func TestSigilPickupIncrementsAndTriggersHunt(t *testing.T) {
	m := maze.Generate(21, 21, 12345)
	g := NewGame(m, 4, 12345)

	if len(g.World.Sigils) == 0 {
		t.Fatalf("no sigils spawned")
	}
	sig := g.World.Sigils[0]
	initialIntensity := g.World.Intensity

	// Move runner to the sigil location.
	g.World.Runner.Pos = sig.Pos
	g.tickLogic(g.World)

	if !sig.Taken {
		t.Fatalf("expected sigil to be taken")
	}
	if g.World.SigilsHeld != 1 {
		t.Fatalf("expected 1 sigil held, got %d", g.World.SigilsHeld)
	}
	if g.World.Intensity != initialIntensity+1 {
		t.Fatalf("expected intensity to increase by 1 to %d, got %d", initialIntensity+1, g.World.Intensity)
	}
	if g.World.HuntRemaining[sig.Element] != SigilHuntDuration-1 {
		t.Fatalf("expected hunt remaining %d (decremented by tickHunts), got %d", SigilHuntDuration-1, g.World.HuntRemaining[sig.Element])
	}
}

func TestHuntExpiresAfterDuration(t *testing.T) {
	m := maze.Generate(21, 21, 12345)
	g := NewGame(m, 0, 12345)

	g.World.HuntRemaining[0] = 3
	g.tickLogic(g.World)
	if g.World.HuntRemaining[0] != 2 {
		t.Fatalf("expected 2 ticks remaining, got %d", g.World.HuntRemaining[0])
	}
	g.tickLogic(g.World)
	g.tickLogic(g.World)
	if g.World.HuntRemaining[0] != 0 {
		t.Fatalf("expected hunt to expire at 0, got %d", g.World.HuntRemaining[0])
	}
}

func TestWorldResetRestoresSigils(t *testing.T) {
	m := maze.Generate(21, 21, 12345)
	g := NewGame(m, 4, 12345)

	// Simulate winning or losing.
	g.World.SigilsHeld = 3
	for _, s := range g.World.Sigils {
		s.Taken = true
	}
	g.World.Phase = world.PhaseLost

	g.submitWorldResetIfTerminal(nil)
	// Run loop tick to execute command.
	g.Loop.TickOnce()

	snap := g.Loop.Snapshot()
	if snap.SigilsHeld != 0 {
		t.Fatalf("expected reset to clear held sigils, got %d", snap.SigilsHeld)
	}
	if !snap.GateLocked {
		t.Fatalf("expected reset gate to be locked")
	}
	if len(snap.Sigils) != 4 {
		t.Fatalf("expected 4 sigils in snapshot after reset, got %d", len(snap.Sigils))
	}
	for i, s := range snap.Sigils {
		if s.Taken {
			t.Fatalf("sigil %d was taken after reset", i)
		}
	}
}

func TestPowerUpRespawnsAfterFortyFiveSeconds(t *testing.T) {
	m := maze.Generate(21, 21, 12345)
	g := NewGame(m, 0, 12345)

	if len(g.World.PowerUps) == 0 {
		t.Fatalf("no power-ups spawned")
	}
	pu := g.World.PowerUps[0]
	// Step on power-up to collect it.
	g.World.Runner.Pos = pu.Pos
	g.tickLogic(g.World)

	if pu.Active {
		t.Fatalf("power-up should be inactive after collection")
	}
	if pu.RespawnTicks != 45*30 {
		t.Fatalf("expected 1350 respawn ticks, got %d", pu.RespawnTicks)
	}

	// Fast-forward 1349 ticks: still inactive.
	for i := 0; i < 1349; i++ {
		tickPowerUps(g.World)
	}
	if pu.Active {
		t.Fatalf("power-up should not respawn before 1350 ticks")
	}

	// 1350th tick: power-up becomes active again.
	tickPowerUps(g.World)
	if !pu.Active {
		t.Fatalf("power-up should have respawned at tick 1350")
	}
}
