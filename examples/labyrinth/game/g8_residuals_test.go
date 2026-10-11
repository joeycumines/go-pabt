//go:build example

package game

import (
	"math"
	"testing"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
)

// TestSeparateActorsNudgesPerfectOverlap: two actors at identical (x,z) get
// nudged apart (the d2<1e-9 epsilon path), not skipped.
func TestSeparateActorsNudgesPerfectOverlap(t *testing.T) {
	g := newTestGame(t)
	w := g.World

	cx, cy, ok := findOpenArea(g.Maze)
	if !ok {
		t.Fatalf("maze has no open 3x3 area")
	}
	// Both actors at exactly the same position.
	pos := maze.Vec2{X: float64(cx) + 0.5, Y: float64(cy) + 0.5}
	w.Minotaurs[0].Pos = pos
	w.Minotaurs[1].Pos = pos

	before0 := w.Minotaurs[0].Pos
	before1 := w.Minotaurs[1].Pos
	g.separateActors(w)

	// At least one of the two must have moved. The fixed-axis nudge
	// (dx=1e-9, dy=0) pushes them apart along +X.
	moved0 := w.Minotaurs[0].Pos != before0
	moved1 := w.Minotaurs[1].Pos != before1
	if !moved0 && !moved1 {
		t.Fatalf("neither actor moved after perfect overlap: both at %v", pos)
	}
	// The resulting separation should be >= ActorSeparation (or at least
	// non-zero — if the maze walls clamp it, at least it's not zero).
	d := math.Hypot(w.Minotaurs[1].Pos.X-w.Minotaurs[0].Pos.X,
		w.Minotaurs[1].Pos.Y-w.Minotaurs[0].Pos.Y)
	if d < 1e-9 {
		t.Fatalf("separation after nudge = %g, want > 0", d)
	}
}

// TestIntensityIncreasesActorSpeed: actorSpeed with intensity>0 scales the
// base speed by (1 + 0.1*intensity).
func TestIntensityIncreasesActorSpeed(t *testing.T) {
	base := actorSpeed(ActorBrute, 0)
	lvl3 := actorSpeed(ActorBrute, 3)
	lvl5 := actorSpeed(ActorBrute, 5)

	want3 := base * 1.3
	want5 := base * 1.5
	if math.Abs(lvl3-want3) > 1e-9 {
		t.Fatalf("actorSpeed(Brute, 3) = %g, want %g", lvl3, want3)
	}
	if math.Abs(lvl5-want5) > 1e-9 {
		t.Fatalf("actorSpeed(Brute, 5) = %g, want %g", lvl5, want5)
	}
	if lvl5 <= base {
		t.Fatalf("intensity 5 speed %g not greater than base %g", lvl5, base)
	}
}

// TestTickLogicRefreshesBrainSpeedWithIntensity: after Intensity bumps,
// tickLogic updates each brain's MoveSpeed without requiring a respawn.
func TestTickLogicRefreshesBrainSpeedWithIntensity(t *testing.T) {
	g := newTestGame(t)
	w := g.World

	// Baseline: brain speed matches intensity 0.
	if got := g.Brains[0].GetMoveSpeed(); math.Abs(got-actorSpeed(ActorType(w.Minotaurs[0].Type), 0)) > 1e-9 {
		t.Fatalf("baseline brain speed = %g, want %g", got, actorSpeed(ActorType(w.Minotaurs[0].Type), 0))
	}

	// Bump intensity and tick once.
	w.Intensity = 5
	g.tickLogic(w)

	want := actorSpeed(ActorType(w.Minotaurs[0].Type), 5)
	if got := g.Brains[0].GetMoveSpeed(); math.Abs(got-want) > 1e-9 {
		t.Fatalf("brain speed after intensity 5 = %g, want %g", got, want)
	}
}

// TestSeparateActorsResolvesAfterNudge: after a perfect-overlap nudge, both
// actors remain inside the maze (Resolve is called).
func TestSeparateActorsResolvesAfterNudge(t *testing.T) {
	g := newTestGame(t)
	w := g.World

	cx, cy, ok := findOpenArea(g.Maze)
	if !ok {
		t.Fatalf("maze has no open 3x3 area")
	}
	pos := maze.Vec2{X: float64(cx) + 0.5, Y: float64(cy) + 0.5}
	w.Minotaurs[0].Pos = pos
	w.Minotaurs[1].Pos = pos

	g.separateActors(w)

	// Both actors must still be in walkable cells after Resolve.
	for i := 0; i < 2; i++ {
		p := w.Minotaurs[i].Pos
		if !g.Maze.At(int(p.X), int(p.Y)).Walkable() {
			t.Fatalf("actor %d in non-walkable cell after nudge: %v", i, p)
		}
	}
}
