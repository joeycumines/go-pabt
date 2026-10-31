//go:build example

package brain

import (
	"testing"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

// TestSameElementActorsAreHostile pins the T7 combat rule: same-element mobs
// are hostile to the player (AllyYielding stays false) and pursue directly.
func TestSameElementActorsAreHostile(t *testing.T) {
	m := maze.Generate(42, 11, 11)
	w := world.New(m)
	minotaur := &world.Minotaur{ID: "M-1", Pos: m.Start, Element: 0, HP: 40}
	w.Minotaurs = append(w.Minotaurs, minotaur)

	b, err := NewBrain("M-1", m, &minotaur.Pos)
	if err != nil {
		t.Fatalf("NewBrain: %v", err)
	}

	w.Runner.Element = 0
	w.Runner.Pos = maze.Vec2{X: minotaur.Pos.X + 2, Y: minotaur.Pos.Y}
	snap := w.Copy()
	b.UpdateWorldView(snap, minotaur)
	if b.world.AllyYielding {
		t.Fatal("AllyYielding must stay false: same-element actors are hostile to the player")
	}

	// Different element also keeps AllyYielding false.
	w.Runner.Element = 1
	snap = w.Copy()
	b.UpdateWorldView(snap, minotaur)
	if b.world.AllyYielding {
		t.Fatal("AllyYielding set for different-element runner")
	}
}

// TestPursueActionAdvancesDirectlyForSameElement verifies that a same-element
// actor pursues directly along the line to the runner rather than stepping aside.
func TestPursueActionAdvancesDirectlyForSameElement(t *testing.T) {
	m := &maze.Maze{W: 11, H: 11, Grid: make([]maze.Cell, 11*11)}
	for i := range m.Grid {
		m.Grid[i] = maze.Cell{Kind: maze.CellFloor}
	}
	w := world.New(m)

	selfPos := maze.Vec2{X: 5.5, Y: 5.5}
	minotaur := &world.Minotaur{ID: "M-1", Pos: selfPos, Element: 0, HP: 40}
	w.Minotaurs = append(w.Minotaurs, minotaur)
	w.Runner.Element = 0
	w.Runner.Pos = maze.Vec2{X: 5.5, Y: 3.5}

	state := NewState(&WorldView{
		PlayerDetected: true,
		PlayerPos:      w.Runner.Pos,
		LastKnownPos:   w.Runner.Pos,
		HasLastKnown:   true,
		SelfElement:    0,
		PlayerElement:  0,
	}, m, &selfPos)

	pursue := NewPursueAction(state)
	status, err := pursue.Node().Tick()
	if err != nil {
		t.Fatalf("pursue tick: %v", err)
	}
	if status.String() != "running" && status.String() != "Running" {
		t.Fatalf("pursue status = %v, want running", status)
	}
	if selfPos.X != 5.5 || selfPos.Y >= 5.5 {
		t.Fatalf("same-element pursue should advance directly toward runner along x=5.5: %+v", selfPos)
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
