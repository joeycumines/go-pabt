//go:build example

package game

import (
	"math"
	"testing"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

// hitRecords returns the 'hit' events the server has broadcast.
func hitRecords(g *Game) []EventRecord {
	var out []EventRecord
	for _, rec := range g.EventRecords() {
		if rec.Type == "hit" {
			out = append(out, rec)
		}
	}
	return out
}

// TestHazardDamageEmitsHitEvent: a rift that hurts the player must say so.
// Without an event the client has no cue, so a player loses HP with nothing on
// screen to explain it (player-eye D6).
func TestHazardDamageEmitsHitEvent(t *testing.T) {
	m := maze.Generate(42, 21, 21)
	g := NewGame(m, 0, 42)
	w := g.World
	w.Runner.Pos = maze.Vec2{X: 10.5, Y: 10.5}
	w.Runner.HP = 100
	w.Hazards = []*world.Hazard{{
		Pos:            w.Runner.Pos,
		Element:        0,
		TTL:            60,
		DamageCooldown: 0,
	}}

	g.tickHazards(w)

	if w.Runner.HP != 100-HazardDamage {
		t.Fatalf("hazard damage: HP = %d, want %d", w.Runner.HP, 100-HazardDamage)
	}
	hits := hitRecords(g)
	if len(hits) != 1 {
		t.Fatalf("hazard damage broadcast %d hit events, want 1", len(hits))
	}
	data, ok := hits[0].Data.(map[string]interface{})
	if !ok {
		t.Fatalf("hit event data type = %T", hits[0].Data)
	}
	if data["source"] != "hazard" {
		t.Fatalf("hit source = %v, want hazard", data["source"])
	}
	if data["hp"] != w.Runner.HP {
		t.Fatalf("hit hp = %v, want %d", data["hp"], w.Runner.HP)
	}
	if _, ok := data["x"]; !ok {
		t.Fatalf("hit event carries no attacker position: %v", data)
	}
}

// floorMaze builds a fully walkable grid so collision tests exercise real
// movement instead of an all-wall maze whose Resolve output is meaningless.
func floorMaze(w, h int) *maze.Maze {
	m := &maze.Maze{W: w, H: h, Grid: make([]maze.Cell, w*h)}
	for i := range m.Grid {
		m.Grid[i] = maze.Cell{Kind: maze.CellFloor}
	}
	m.Start = maze.Vec2{X: 1.5, Y: 1.5}
	m.Gate = maze.Vec2{X: float64(w) - 1.5, Y: float64(h) - 1.5}
	return m
}

func TestDamageRunnerClampsHPAtZero(t *testing.T) {
	m := floorMaze(9, 9)
	g := NewGame(m, 0, 42)
	w := g.World
	w.Runner.HP = 3

	if !g.damageRunner(w, 20, "contact", maze.Vec2{X: 2.5, Y: 1.5}, "brute-1") {
		t.Fatal("damage should have landed on a fresh runner")
	}
	if w.Runner.HP != 0 {
		t.Fatalf("HP = %d, want 0 (clamped)", w.Runner.HP)
	}
	hits := hitRecords(g)
	if len(hits) != 1 {
		t.Fatalf("hit events = %d, want 1", len(hits))
	}
	if data := hits[0].Data.(map[string]interface{}); data["hp"] != 0 {
		t.Fatalf("hit event hp = %v, want 0", data["hp"])
	}
}

func TestDamageRunnerInvulnBlocksSecondHit(t *testing.T) {
	m := floorMaze(9, 9)
	g := NewGame(m, 0, 42)
	w := g.World
	w.Runner.HP = 100

	g.damageRunner(w, 5, "contact", maze.Vec2{}, "a")
	if w.Runner.HP != 95 {
		t.Fatalf("first hit: HP = %d, want 95", w.Runner.HP)
	}
	if g.damageRunner(w, 5, "contact", maze.Vec2{}, "a") {
		t.Fatal("second hit inside the invulnerability window must be ignored")
	}
	if w.Runner.HP != 95 {
		t.Fatalf("second hit changed HP to %d, want 95", w.Runner.HP)
	}

	for i := 0; i < InvulnTicks; i++ {
		g.tickTimers(w)
	}
	if w.Runner.InvulnTicks != 0 {
		t.Fatalf("invulnerability = %d ticks after %d ticks, want 0", w.Runner.InvulnTicks, InvulnTicks)
	}
	if !g.damageRunner(w, 5, "contact", maze.Vec2{}, "a") {
		t.Fatal("hit after the invulnerability window should land")
	}
	if w.Runner.HP != 90 {
		t.Fatalf("post-window hit: HP = %d, want 90", w.Runner.HP)
	}
}

func TestKnockRunnerStopsAtWall(t *testing.T) {
	m := floorMaze(5, 5)
	m.Grid[2*m.W+3] = maze.Cell{Kind: maze.CellWall}
	g := NewGame(m, 0, 42)
	w := g.World
	w.Runner.Pos = maze.Vec2{X: 2.5, Y: 2.5}

	g.knockRunner(w, maze.Vec2{X: 1, Y: 0}, RunnerKnockbackMax)

	cx := int(math.Floor(w.Runner.Pos.X))
	cy := int(math.Floor(w.Runner.Pos.Y))
	if !m.At(cx, cy).Walkable() {
		t.Fatalf("knockback ended inside a solid cell: pos=%v cell=(%d,%d)", w.Runner.Pos, cx, cy)
	}
	if w.Runner.Pos.X > 3-maze.Radius+1e-9 {
		t.Fatalf("knockback crossed the wall face: x=%v, max %v", w.Runner.Pos.X, 3-maze.Radius)
	}
}

func TestCasterBoltEmitsHitWithSource(t *testing.T) {
	m := floorMaze(9, 9)
	g := NewGame(m, 1, 42)
	w := g.World
	w.Runner.HP = 100
	m0 := w.Minotaurs[0]

	g.casterBolt(w, m0)

	if w.Runner.HP != 100-BoltDamage {
		t.Fatalf("bolt damage: HP = %d, want %d", w.Runner.HP, 100-BoltDamage)
	}
	hits := hitRecords(g)
	if len(hits) != 1 {
		t.Fatalf("bolt broadcast %d hit events, want 1", len(hits))
	}
	data := hits[0].Data.(map[string]interface{})
	if data["source"] != "bolt" {
		t.Fatalf("hit source = %v, want bolt", data["source"])
	}
	if data["attacker"] != m0.ID {
		t.Fatalf("hit attacker = %v, want %s", data["attacker"], m0.ID)
	}
}

func TestOutOfCombatRegen(t *testing.T) {
	m := floorMaze(9, 9)
	g := NewGame(m, 0, 42)
	w := g.World
	w.Runner.HP = 50
	w.Runner.LastDamageRound = 0

	w.Round = RegenDelayTicks - RegenIntervalTicks
	g.tickTimers(w)
	if w.Runner.HP != 50 {
		t.Fatalf("regen started inside the quiet window: HP = %d, want 50", w.Runner.HP)
	}

	w.Round = RegenDelayTicks
	g.tickTimers(w)
	if w.Runner.HP != 51 {
		t.Fatalf("regen did not tick after the quiet window: HP = %d, want 51", w.Runner.HP)
	}

	w.Runner.HP = world.MaxHP
	g.tickTimers(w)
	if w.Runner.HP != world.MaxHP {
		t.Fatalf("regen exceeded max HP: %d", w.Runner.HP)
	}
}

func TestResolveRunnerAgainstActorsBlocksPassThrough(t *testing.T) {
	m := floorMaze(9, 9)
	g := NewGame(m, 0, 42)
	w := g.World
	// Place a Swarm minotaur at (4.5, 4.5).
	a := &world.Minotaur{
		ID:   "S-1",
		Type: int(ActorSwarm),
		HP:   20,
		Pos:  maze.Vec2{X: 4.5, Y: 4.5},
	}
	w.Minotaurs = []*world.Minotaur{a}

	minSep := maze.Radius + actorBodyRadius(ActorSwarm)

	// Case 1: Runner approaches from south (prevPos Y=3.5) and inputs forward
	// past the actor to newPos Y=5.0.
	prevPos := maze.Vec2{X: 4.5, Y: 3.5}
	newPos := maze.Vec2{X: 4.5, Y: 5.0}
	resolved := g.resolveRunnerAgainstActors(w, prevPos, newPos)

	// Runner must be blocked on the south side (Y < a.Pos.Y), separated by minSep.
	if resolved.Y >= a.Pos.Y {
		t.Fatalf("runner jumped across actor: resolved=%v, actor=%v", resolved, a.Pos)
	}
	dist := math.Hypot(resolved.X-a.Pos.X, resolved.Y-a.Pos.Y)
	if math.Abs(dist-minSep) > 1e-6 {
		t.Fatalf("separation = %f, want %f", dist, minSep)
	}

	// Case 2: Runner approaches from west (prevPos X=3.5) and steps past east to X=5.0.
	prevPos = maze.Vec2{X: 3.5, Y: 4.5}
	newPos = maze.Vec2{X: 5.0, Y: 4.5}
	resolved = g.resolveRunnerAgainstActors(w, prevPos, newPos)
	if resolved.X >= a.Pos.X {
		t.Fatalf("runner jumped across actor horizontally: resolved=%v, actor=%v", resolved, a.Pos)
	}
	dist = math.Hypot(resolved.X-a.Pos.X, resolved.Y-a.Pos.Y)
	if math.Abs(dist-minSep) > 1e-6 {
		t.Fatalf("separation = %f, want %f", dist, minSep)
	}
}
