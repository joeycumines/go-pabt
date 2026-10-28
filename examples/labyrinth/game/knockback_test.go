//go:build example

package game

import (
	"math"
	"testing"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
	"github.com/joeycumines/go-pabt/examples/labyrinth/netcode"
	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

// TestActorMassTable confirms the mass table is sensible: brute > caster >
// stalker > swarm, and RunnerMass is between swarm and stalker so the player
// is meaningfully lighter than a brute but heavier than a swarm actor.
func TestActorMassTable(t *testing.T) {
	b := actorMass(ActorBrute)
	c := actorMass(ActorCaster)
	s := actorMass(ActorStalker)
	w := actorMass(ActorSwarm)
	if !(b > c && c > s && s > w) {
		t.Fatalf("mass ordering broken: brute=%v caster=%v stalker=%v swarm=%v", b, c, s, w)
	}
	if RunnerMass <= w {
		t.Fatalf("runner must be heavier than swarm (RunnerMass=%v, swarm=%v)", RunnerMass, w)
	}
	if RunnerMass >= b {
		t.Fatalf("runner must be lighter than brute (RunnerMass=%v, brute=%v)", RunnerMass, b)
	}
}

// TestContactKnockbackDisplacesBoth verifies that contact damage pushes both
// the runner and the actor apart by a mass-ratio impulse — the player can no
// longer stand inside an enemy with no physical consequence beyond HP loss.
func TestContactKnockbackDisplacesBoth(t *testing.T) {
	// Use a simple open-cell maze fragment so Resolve doesn't clamp.
	m := floorMaze(21, 21)
	w := world.New(m)
	w.Runner.Pos = maze.Vec2{X: 10.0, Y: 10.0}
	w.Runner.HP = 100

	// Brute actor right on top of the runner (distance 0.3 < 0.7 contact range).
	brute := &world.Minotaur{
		ID:             "brute-1",
		Pos:            maze.Vec2{X: 10.3, Y: 10.0},
		Type:           int(ActorBrute),
		HP:             40,
		AttackCooldown: 0,
	}
	w.Minotaurs = append(w.Minotaurs, brute)

	g := NewGame(m, 1, 42)
	// Inject our custom runner+actor so tickLogic finds them.
	g.World = w

	beforeRunner := w.Runner.Pos
	beforeActor := brute.Pos

	// One tick of contact: HP must drop, and both bodies must move.
	g.tickLogic(w)

	if w.Runner.HP >= 100 {
		t.Fatalf("runner HP should have decreased from contact damage, got %v", w.Runner.HP)
	}
	runnerDist := math.Hypot(w.Runner.Pos.X-beforeRunner.X, w.Runner.Pos.Y-beforeRunner.Y)
	actorDist := math.Hypot(brute.Pos.X-beforeActor.X, brute.Pos.Y-beforeActor.Y)
	if runnerDist < 0.05 {
		t.Fatalf("runner should have been pushed at least 0.05u by contact knockback, got %v", runnerDist)
	}
	if actorDist < 0.01 {
		t.Fatalf("actor should have been nudged by contact knockback, got %v", actorDist)
	}
	// Runner knockback is capped, but the brute's mass-ratio share is still
	// smaller than the runner's share.
	if runnerDist > RunnerKnockbackMax+1e-9 {
		t.Fatalf("runner knockback %v exceeds the %v cap", runnerDist, RunnerKnockbackMax)
	}
	if actorDist >= runnerDist {
		t.Fatalf("brute (heavy) must move less than runner (light): actor=%v runner=%v", actorDist, runnerDist)
	}
}

// TestPulseKnockbackPushesActorAway verifies that FirePulse displaces a hit
// actor along the shot direction via the mass-ratio knockback. This test
// exercises the actual FirePulse code path (not a duplicated formula).
func TestPulseKnockbackPushesActorAway(t *testing.T) {
	m := floorMaze(21, 21)
	w := world.New(m)
	w.Runner.Pos = maze.Vec2{X: 10.0, Y: 10.0}

	// Swarm actor (light mass) directly ahead of the runner on the +X axis.
	swarm := &world.Minotaur{
		ID:   "swarm-1",
		Pos:  maze.Vec2{X: 12.0, Y: 10.0},
		Type: int(ActorSwarm),
		HP:   15,
	}
	w.Minotaurs = append(w.Minotaurs, swarm)

	g := NewGame(m, 0, 42)
	g.World = w

	beforeActor := swarm.Pos

	// Exercise the actual knockback code path extracted from FirePulse.
	aim := netcode.RayHitOrigin{
		Pos:   w.Runner.Pos,
		Yaw:   math.Pi / 2,
		Pitch: 0,
	}
	g.applyPulseKnockback(swarm, aim)

	afterActor := swarm.Pos
	delta := math.Hypot(afterActor.X-beforeActor.X, afterActor.Y-beforeActor.Y)
	// Swarm is light (0.5), so kick should be large: 1.2 * (1/(1+0.5)) = 0.8u.
	if delta < 0.5 {
		t.Fatalf("swarm actor should have been knocked back >= 0.5u by pulse, got %v", delta)
	}
	// And the direction should be along +X (the shot axis).
	if afterActor.X <= beforeActor.X {
		t.Fatalf("actor should have moved +X (shot direction), before=%v after=%v", beforeActor, afterActor)
	}
}

// TestSeparateRunnerFromActorsOverlapping verifies the separation step pushes
// a player standing on top of an actor apart before contact damage triggers,
// so the player can never remain embedded inside an enemy between ticks.
func TestSeparateRunnerFromActorsOverlapping(t *testing.T) {
	m := floorMaze(21, 21)
	w := world.New(m)
	w.Runner.Pos = maze.Vec2{X: 10.0, Y: 10.0}

	// Perfectly overlapping caster actor — separation must still push apart
	// thanks to the fixed-axis nudge when d < 1e-9.
	caster := &world.Minotaur{
		ID:   "caster-1",
		Pos:  maze.Vec2{X: 10.0, Y: 10.0},
		Type: int(ActorCaster),
		HP:   25,
	}
	w.Minotaurs = append(w.Minotaurs, caster)

	g := NewGame(m, 1, 42)
	g.World = w
	g.separateRunnerFromActors(w)

	dist := math.Hypot(w.Runner.Pos.X-caster.Pos.X, w.Runner.Pos.Y-caster.Pos.Y)
	want := maze.Radius + actorBodyRadius(ActorCaster)
	if dist < want*0.99 {
		t.Fatalf("overlapping runner+actor should have been separated to >= %v (body radii), got %v", want, dist)
	}
}
