//go:build example

package game

import (
	"testing"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

func newTestGame(t *testing.T) *Game {
	t.Helper()
	m := maze.Generate(7, 21, 21)
	return NewGame(m, 8, 7)
}

func TestFactionCombatDamagesRivalsOnly(t *testing.T) {
	g := newTestGame(t)
	w := g.World

	a, b := w.Minotaurs[0], w.Minotaurs[1]
	a.Element, b.Element = int(ElementFire), int(ElementIce)
	a.Pos = maze.Vec2{X: 5.5, Y: 5.5}
	b.Pos = maze.Vec2{X: 6.0, Y: 5.5}
	// Park the rest of the roster far apart so unrelated pairs cannot clash
	// and pollute the hazard assertion.
	for k := 2; k < len(w.Minotaurs); k++ {
		w.Minotaurs[k].Pos = maze.Vec2{X: 1.5 + 2*float64(k), Y: 18.5}
	}
	hpA, hpB := a.HP, b.HP

	g.factionCombat(w)

	// Fire beats ice: ice takes 8*2, fire takes 8*0.5.
	if b.HP != hpB-16 {
		t.Fatalf("ice actor HP = %d, want %d", b.HP, hpB-16)
	}
	if a.HP != hpA-4 {
		t.Fatalf("fire actor HP = %d, want %d", a.HP, hpA-4)
	}
	// The clash leaves a rift.
	if len(w.Hazards) != 1 {
		t.Fatalf("hazards = %d, want 1", len(w.Hazards))
	}
	// Cooldown blocks an immediate re-hit.
	hpA2, hpB2 := a.HP, b.HP
	g.factionCombat(w)
	if a.HP != hpA2 || b.HP != hpB2 {
		t.Fatalf("cooldown not respected: %d->%d, %d->%d", hpA2, a.HP, hpB2, b.HP)
	}

	// Same faction: no friendly fire once cooldowns clear.
	g.actorStates[0].FactionAttackCooldown = 0
	g.actorStates[1].FactionAttackCooldown = 0
	b.Element = int(ElementFire)
	hpA3, hpB3 := a.HP, b.HP
	g.factionCombat(w)
	if a.HP != hpA3 || b.HP != hpB3 {
		t.Fatalf("friendly fire: %d->%d, %d->%d", hpA3, a.HP, hpB3, b.HP)
	}
}

func TestTerritoryControlFollowsPresence(t *testing.T) {
	g := newTestGame(t)
	w := g.World

	// Park every actor in the NW quadrant as fire: NW control must flip.
	for _, m := range w.Minotaurs {
		m.Element = int(ElementFire)
		m.Pos = maze.Vec2{X: 2.5, Y: 2.5}
	}
	ctrl := territoryControl(w)
	if ctrl[1] != int(ElementFire) {
		t.Fatalf("NW control = %d, want fire (%d)", ctrl[1], ElementFire)
	}

	// An empty world keeps innate control: NE fire, NW ice, SE lightning, SW void.
	w.Minotaurs = nil
	ctrl = territoryControl(w)
	want := [4]int{int(ElementFire), int(ElementIce), int(ElementLightning), int(ElementVoid)}
	if ctrl != want {
		t.Fatalf("empty control = %v, want %v", ctrl, want)
	}
}

func TestRespawnDeadActorsKeepsArraysAligned(t *testing.T) {
	g := newTestGame(t)
	w := g.World

	w.Minotaurs[0].HP = 0
	w.Minotaurs[3].HP = 0
	g.respawnDeadActors(w)

	if len(w.Minotaurs) != TargetActorCount {
		t.Fatalf("actors = %d, want %d", len(w.Minotaurs), TargetActorCount)
	}
	if len(g.Brains) != len(w.Minotaurs) || len(g.Histories) != len(w.Minotaurs) || len(g.actorStates) != len(w.Minotaurs) {
		t.Fatalf("parallel arrays drifted: minotaurs=%d brains=%d histories=%d states=%d",
			len(w.Minotaurs), len(g.Brains), len(g.Histories), len(g.actorStates))
	}
	seen := make(map[string]bool, len(w.Minotaurs))
	for _, m := range w.Minotaurs {
		if seen[m.ID] {
			t.Fatalf("duplicate actor ID %q after respawn", m.ID)
		}
		seen[m.ID] = true
	}
}

func TestHazardDamagesAndExpires(t *testing.T) {
	g := newTestGame(t)
	w := g.World

	w.Hazards = append(w.Hazards, &world.Hazard{
		Pos:            w.Runner.Pos,
		Element:        int(ElementFire),
		TTL:            3,
		DamageCooldown: 1,
	})
	hp := w.Runner.HP

	g.tickHazards(w) // cooldown hits zero: damage applies
	if w.Runner.HP != hp-HazardDamage {
		t.Fatalf("runner HP = %d, want %d", w.Runner.HP, hp-HazardDamage)
	}

	g.tickHazards(w) // cooldown rebuilding: no damage
	if w.Runner.HP != hp-HazardDamage {
		t.Fatalf("rift damaged off-interval: HP = %d", w.Runner.HP)
	}

	g.tickHazards(w) // TTL hits zero: rift dropped
	if len(w.Hazards) != 0 {
		t.Fatalf("hazards = %d after expiry, want 0", len(w.Hazards))
	}
}

func TestStrategiesRetreatWhenHurt(t *testing.T) {
	g := newTestGame(t)
	w := g.World

	m := w.Minotaurs[0]
	m.HP = 1 // badly hurt: must retreat
	m.Stunned = false
	g.tickStrategies(w)
	if g.actorStates[0].Strategy != StrategyRetreat {
		t.Fatalf("strategy = %d, want retreat (%d)", g.actorStates[0].Strategy, StrategyRetreat)
	}
	if m.Strategy != StrategyRetreat {
		t.Fatalf("world strategy = %d, want retreat", m.Strategy)
	}

	m.HP = actorHP(ActorType(m.Type)) // full health with rivals present: raid
	g.actorStates[0].StrategyTimer = 0
	g.actorStates[0].HasTarget = false
	g.tickStrategies(w)
	if g.actorStates[0].Strategy != StrategyRaid {
		t.Fatalf("strategy = %d, want raid (%d)", g.actorStates[0].Strategy, StrategyRaid)
	}
}
