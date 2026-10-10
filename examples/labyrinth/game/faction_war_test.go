//go:build example

package game

import (
	"math"
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

	// Same faction: no friendly fire once cooldowns decay through the real
	// tick path.
	for i := 0; i < FactionCombatCooldown; i++ {
		g.tickTimers(w)
	}
	if g.actorStates[0].FactionAttackCooldown != 0 || g.actorStates[1].FactionAttackCooldown != 0 {
		t.Fatalf("cooldowns did not decay to zero: %d, %d",
			g.actorStates[0].FactionAttackCooldown, g.actorStates[1].FactionAttackCooldown)
	}
	b.Element = int(ElementFire)
	hpA3, hpB3 := a.HP, b.HP
	g.factionCombat(w)
	if a.HP != hpA3 || b.HP != hpB3 {
		t.Fatalf("friendly fire: %d->%d, %d->%d", hpA3, a.HP, hpB3, b.HP)
	}
}

func TestFactionCooldownDecaysThroughTicks(t *testing.T) {
	g := newTestGame(t)
	w := g.World

	for k := 2; k < len(w.Minotaurs); k++ {
		w.Minotaurs[k].Pos = maze.Vec2{X: 1.5 + 2*float64(k), Y: 18.5}
	}
	a, b := w.Minotaurs[0], w.Minotaurs[1]
	a.Element, b.Element = int(ElementFire), int(ElementIce)
	a.Pos = maze.Vec2{X: 5.5, Y: 5.5}
	b.Pos = maze.Vec2{X: 6.0, Y: 5.5}

	g.factionCombat(w)
	if g.actorStates[0].FactionAttackCooldown != FactionCombatCooldown {
		t.Fatalf("cooldown after clash = %d, want %d",
			g.actorStates[0].FactionAttackCooldown, FactionCombatCooldown)
	}

	// The combat cooldown must count down on the same tick path as every
	// other timer, or each actor fights exactly once per lifetime.
	for i := FactionCombatCooldown; i > 0; i-- {
		g.tickTimers(w)
		if g.actorStates[0].FactionAttackCooldown != i-1 {
			t.Fatalf("after %d ticks cooldown = %d, want %d",
				FactionCombatCooldown-i+1, g.actorStates[0].FactionAttackCooldown, i-1)
		}
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

func TestFactionCombatDeadActorStopsFighting(t *testing.T) {
	g := newTestGame(t)
	w := g.World

	// Park the roster far apart so only the arranged trio can clash.
	for k := 0; k < len(w.Minotaurs); k++ {
		w.Minotaurs[k].Pos = maze.Vec2{X: 1.5 + 2*float64(k), Y: 18.5}
	}

	// A fire swarm actor between two void rivals: void beats fire (2x),
	// so each counter-hit deals 16, lethal against 15 HP.
	a, v1, v2 := w.Minotaurs[0], w.Minotaurs[1], w.Minotaurs[2]
	a.Element = int(ElementFire)
	a.HP = 15
	v1.Element, v2.Element = int(ElementVoid), int(ElementVoid)
	a.Pos = maze.Vec2{X: 5.5, Y: 5.5}
	v1.Pos = maze.Vec2{X: 6.0, Y: 5.5}
	v2.Pos = maze.Vec2{X: 5.0, Y: 5.5}
	hpV1, hpV2 := v1.HP, v2.HP

	g.factionCombat(w)

	if a.HP > 0 {
		t.Fatalf("fire actor HP = %d, want lethal", a.HP)
	}
	if v1.HP != hpV1-4 { // fire attacking void is a disadvantage: 8*0.5
		t.Fatalf("v1 HP = %d, want %d", v1.HP, hpV1-4)
	}
	if v2.HP != hpV2 {
		t.Fatalf("dead actor hit a second rival: %d -> %d", hpV2, v2.HP)
	}
	if len(w.Hazards) != 1 {
		t.Fatalf("hazards = %d, want exactly 1", len(w.Hazards))
	}
}

func TestStrategyMovementPathfollowsToTarget(t *testing.T) {
	g := newTestGame(t)
	w := g.World

	m := w.Minotaurs[0]
	st := g.actorStates[0]
	m.Pos = g.Maze.Start
	setStrategyTarget(st, g.Maze.Gate)

	for i := 0; i < 6000 && st.HasTarget; i++ {
		g.applyStrategyMovement(w)
	}
	if st.HasTarget {
		t.Fatalf("actor never reached the gate target")
	}
	dx := m.Pos.X - g.Maze.Gate.X
	dy := m.Pos.Y - g.Maze.Gate.Y
	if dx*dx+dy*dy > 0.5*0.5 {
		t.Fatalf("actor stopped at %v, gate at %v", m.Pos, g.Maze.Gate)
	}
}

func TestStrategyMovementYieldsToBrainSearch(t *testing.T) {
	g := newTestGame(t)
	w := g.World

	m := w.Minotaurs[0]
	st := g.actorStates[0]

	// The brain saw the player once, then lost sight: its search action
	// owns movement this tick.
	g.Brains[0].InjectWorldView(maze.Vec2{X: 1.5, Y: 1.5})
	snap := w.Copy()
	snap.Runner.Pos = maze.Vec2{X: 999, Y: 999}
	g.Brains[0].UpdateWorldView(snap, m)
	// The planner expands the tree on the tick it observes the failed
	// condition and executes the new action on the next tick, so allow a
	// couple of ticks for the search to own movement.
	moved := false
	for i := 0; i < 3 && !moved; i++ {
		g.Brains[0].Tick("test")
		moved = g.Brains[0].MovedLastTick()
	}
	if !moved {
		t.Fatalf("brain search did not move the actor")
	}

	setStrategyTarget(st, g.Maze.Gate)
	pos := m.Pos
	g.applyStrategyMovement(w)
	if m.Pos != pos {
		t.Fatalf("strategy moved an actor while the brain searches: %v -> %v", pos, m.Pos)
	}

	// Positive control: an actor whose brain did not move it this tick is
	// free to follow its strategy path.
	m2 := w.Minotaurs[1]
	st2 := g.actorStates[1]
	g.Brains[1].UpdateWorldView(w.Copy(), m2)
	for i := 0; i < 3; i++ {
		g.Brains[1].Tick("test")
		if g.Brains[1].MovedLastTick() {
			t.Fatalf("idle brain reported movement")
		}
	}
	setStrategyTarget(st2, g.Maze.Gate)
	pos2 := m2.Pos
	g.applyStrategyMovement(w)
	if m2.Pos == pos2 {
		t.Fatalf("strategy movement blocked for an actor the brain did not move")
	}
}

func TestActorsSeparateWhenOverlapping(t *testing.T) {
	g := newTestGame(t)
	w := g.World

	// The push needs room to land: inside a single corridor cell the maze
	// walls clamp both actors back together, so place the pair in an open
	// 3x3 area where separation 0.7 is physically achievable.
	cx, cy, ok := findOpenArea(g.Maze)
	if !ok {
		t.Fatalf("maze has no open 3x3 area")
	}
	a, b := w.Minotaurs[0], w.Minotaurs[1]
	a.Pos = maze.Vec2{X: float64(cx) + 0.5, Y: float64(cy) + 0.5}
	b.Pos = maze.Vec2{X: float64(cx) + 0.6, Y: float64(cy) + 0.5}
	g.separateActors(w)
	d := math.Hypot(b.Pos.X-a.Pos.X, b.Pos.Y-a.Pos.Y)
	if d < ActorSeparation-1e-6 {
		t.Fatalf("separation = %f, want >= %f", d, ActorSeparation)
	}

	// Far-apart actors are untouched.
	c := w.Minotaurs[2]
	c.Pos = maze.Vec2{X: float64(cx) + 0.5, Y: float64(cy) + 2.5}
	before := c.Pos
	g.separateActors(w)
	if c.Pos != before {
		t.Fatalf("distant actor moved: %v -> %v", before, c.Pos)
	}
}

// findOpenArea returns the center cell of the first 3x3 fully-walkable
// block in the maze, or false if none exists.
func findOpenArea(m *maze.Maze) (int, int, bool) {
	for y := 1; y < m.H-1; y++ {
		for x := 1; x < m.W-1; x++ {
			open := true
			for dy := -1; dy <= 1 && open; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if !m.At(x+dx, y+dy).Walkable() {
						open = false
						break
					}
				}
			}
			if open {
				return x, y, true
			}
		}
	}
	return 0, 0, false
}

func TestCaughtClearsWhenSightLost(t *testing.T) {
	g := newTestGame(t)
	w := g.World

	m := w.Minotaurs[0]
	// Runner standing on the actor: detected, pursued, caught.
	snap := w.Copy()
	snap.Runner.Pos = m.Pos
	g.Brains[0].UpdateWorldView(snap, m)
	caught := false
	for i := 0; i < 3 && !caught; i++ {
		g.Brains[0].Tick("test")
		caught = g.Brains[0].WorldViewCopy().Caught
	}
	if !caught {
		t.Fatalf("caught not set while standing on the player")
	}

	// Runner leaves: the catch is over and the planner must work again.
	snap.Runner.Pos = maze.Vec2{X: 999, Y: 999}
	g.Brains[0].UpdateWorldView(snap, m)
	if g.Brains[0].WorldViewCopy().Caught {
		t.Fatalf("caught latched after line of sight lost")
	}
}
