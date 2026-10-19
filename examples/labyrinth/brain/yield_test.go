//go:build example

package brain

import (
	"testing"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

// TestAllyYieldingFlagsSameElementApproach pins the I4 trigger: AllyYielding
// must be set when the runner is SAME element, detected, and within 3u — and
// must stay clear for a different-element runner at the same distance.
func TestAllyYieldingFlagsSameElementApproach(t *testing.T) {
	m := maze.Generate(42, 11, 11)
	w := world.New(m)
	minotaur := &world.Minotaur{ID: "M-1", Pos: m.Start, Element: 0}
	w.Minotaurs = append(w.Minotaurs, minotaur)

	b, err := NewBrain("M-1", m, &minotaur.Pos)
	if err != nil {
		t.Fatalf("NewBrain: %v", err)
	}

	// Same element, within sight range (6u) and yield range (3u).
	w.Runner.Element = 0
	w.Runner.Pos = maze.Vec2{X: minotaur.Pos.X + 2, Y: minotaur.Pos.Y}
	snap := w.Copy()
	b.UpdateWorldView(snap, minotaur)
	if !b.world.AllyYielding {
		t.Fatal("AllyYielding not set for same-element runner within 3u")
	}

	// Different element: no yielding even at the same distance.
	w.Runner.Element = 1
	snap = w.Copy()
	b.UpdateWorldView(snap, minotaur)
	if b.world.AllyYielding {
		t.Fatal("AllyYielding set for different-element runner")
	}
}

// TestPursueActionYieldsLaterally pins the I4 path shape: with AllyYielding
// set, PursueAction's first-tick path aims at the LATERALLY OFFSET target,
// not the runner's position. The acceptance case: same-element actor at
// (5,5), runner at (5,3) approaching +Y — the offset path's first waypoint
// must displace the actor off x==5 (the lateral shift), while the non-yield
// path stays on the direct line.
func TestPursueActionYieldsLaterally(t *testing.T) {
	// Hand-authored open 11x11 maze (no interior walls) so the A* path
	// geometry is deterministic and the lateral offset is observable.
	m := &maze.Maze{W: 11, H: 11, Grid: make([]maze.Cell, 11*11)}
	for i := range m.Grid {
		m.Grid[i] = maze.Cell{Kind: maze.CellFloor}
	}
	w := world.New(m)

	selfPos := maze.Vec2{X: 5, Y: 5}
	minotaur := &world.Minotaur{ID: "M-1", Pos: selfPos, Element: 0}
	w.Minotaurs = append(w.Minotaurs, minotaur)
	w.Runner.Element = 0
	w.Runner.Pos = maze.Vec2{X: 5, Y: 3}

	state := NewState(&WorldView{
		PlayerDetected: true,
		PlayerPos:      w.Runner.Pos,
		LastKnownPos:   w.Runner.Pos,
		HasLastKnown:   true,
		SelfElement:    0,
		PlayerElement:  0,
		AllyYielding:   true,
	}, m, &selfPos)

	pursue := NewPursueAction(state)
	status, err := pursue.Node().Tick()
	if err != nil {
		t.Fatalf("pursue tick: %v", err)
	}
	if status.String() != "running" && status.String() != "Running" {
		t.Fatalf("pursue status = %v, want running", status)
	}

	// With yielding on, the actor must have moved off the x==5 line the
	// runner is approaching along (the perpendicular offset direction).
	if selfPos.X == 5 && selfPos.Y != 5 {
		t.Fatalf("yielding pursue moved straight along x=5: %+v", selfPos)
	}
	if selfPos.X == 5 && selfPos.Y == 5 {
		t.Fatalf("yielding pursue did not move the actor at all: %+v", selfPos)
	}
}

// TestYieldOffsetGeometry pins the pure offset math: the shifted target is
// yieldDist (1.5u) from the original, perpendicular to the actor→target
// direction.
func TestYieldOffsetGeometry(t *testing.T) {
	self := maze.Vec2{X: 5, Y: 5}
	target := maze.Vec2{X: 5, Y: 3} // straight below
	shifted := yieldOffset(target, self)

	dx := shifted.X - target.X
	dy := shifted.Y - target.Y
	d := dx*dx + dy*dy
	if d < 1.4*1.4 || d > 1.6*1.6 {
		t.Fatalf("offset magnitude %f, want ~1.5", d)
	}
	// Original direction self->target is (0,-1); perpendicular is (-1,0) or (1,0).
	if dx == 0 {
		t.Fatalf("offset not lateral: %+v", shifted)
	}
	if dy != 0 {
		t.Fatalf("offset has along-axis component: %+v", shifted)
	}
}
