//go:build example

package maze

import (
	"testing"
)

func TestDeterministicGeneration(t *testing.T) {
	seed := uint64(42)
	m1 := Generate(seed, 21, 21)
	m2 := Generate(seed, 21, 21)

	if m1.W != m2.W || m1.H != m2.H {
		t.Fatalf("dimensions differ: %dx%d vs %dx%d", m1.W, m1.H, m2.W, m2.H)
	}
	if m1.Start != m2.Start {
		t.Fatalf("start differs: %v vs %v", m1.Start, m2.Start)
	}
	if m1.Gate != m2.Gate {
		t.Fatalf("gate differs: %v vs %v", m1.Gate, m2.Gate)
	}
	for i := range m1.Grid {
		if m1.Grid[i] != m2.Grid[i] {
			t.Fatalf("grid cell %d differs: %+v vs %+v", i, m1.Grid[i], m2.Grid[i])
		}
	}
}

func TestDifferentSeedsDiffer(t *testing.T) {
	m1 := Generate(1, 21, 21)
	m2 := Generate(2, 21, 21)
	same := true
	for i := range m1.Grid {
		if m1.Grid[i] != m2.Grid[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatal("different seeds produced identical grids")
	}
}

func TestPathStartToGate(t *testing.T) {
	m := Generate(42, 21, 21)
	path := m.Path(m.Start, m.Gate)
	if path == nil {
		t.Fatal("no path from start to gate")
	}
	// First point should be start cell center, last should be gate cell center.
	if len(path) < 2 {
		t.Fatalf("path too short: %d", len(path))
	}
}

func TestPathUnreachable(t *testing.T) {
	// Build a 5x5 maze with a wall ring isolating the center.
	m := &Maze{W: 5, H: 5, Grid: make([]Cell, 25)}
	for i := range m.Grid {
		m.Grid[i] = Cell{Kind: CellFloor}
	}
	// Surround center cell (2,2) with walls.
	for dx := -1; dx <= 1; dx++ {
		for dy := -1; dy <= 1; dy++ {
			if dx == 0 && dy == 0 {
				continue
			}
			m.set(2+dx, 2+dy, Cell{Kind: CellWall})
		}
	}
	start := Vec2{0.5, 0.5}
	goal := Vec2{2.5, 2.5}
	path := m.Path(start, goal)
	if path != nil {
		t.Fatalf("expected nil path to unreachable cell, got %d points", len(path))
	}
}

func TestPathShortestOnOpenGrid(t *testing.T) {
	m := &Maze{W: 5, H: 5, Grid: make([]Cell, 25)}
	for i := range m.Grid {
		m.Grid[i] = Cell{Kind: CellFloor}
	}
	start := Vec2{0.5, 0.5}
	goal := Vec2{4.5, 4.5}
	path := m.Path(start, goal)
	if path == nil {
		t.Fatal("no path on open grid")
	}
	// Manhattan distance is 8 steps, plus the start cell = 9 points.
	if len(path) != 9 {
		t.Fatalf("expected 9 points, got %d", len(path))
	}
}

func TestCollisionBlocksWall(t *testing.T) {
	m := &Maze{W: 5, H: 5, Grid: make([]Cell, 25)}
	for i := range m.Grid {
		m.Grid[i] = Cell{Kind: CellFloor}
	}
	// Wall at (2,2).
	m.set(2, 2, Cell{Kind: CellWall})

	// Try to place a cylinder overlapping the wall.
	pos := Vec2{2.1, 2.5}
	resolved := m.Resolve(pos, 0.3)
	// The resolved position must not overlap the wall cell [2,3)x[2,3).
	// Pushed left means X <= 2.0 - 0.3 = 1.7; pushed right means X >= 3.0 + 0.3 = 3.3.
	if resolved.X <= 1.7 {
		// pushed left, ok
	} else if resolved.X >= 3.3 {
		// pushed right, ok
	} else if resolved.Y <= 1.7 || resolved.Y >= 3.3 {
		// pushed up or down, ok
	} else {
		t.Fatalf("resolved position %v still overlaps wall [2,3)x[2,3)", resolved)
	}
}

func TestGapNotWalkable(t *testing.T) {
	c := Cell{Kind: CellGap}
	if c.Walkable() {
		t.Fatal("gap should not be walkable")
	}
	m := &Maze{W: 3, H: 3, Grid: make([]Cell, 9)}
	for i := range m.Grid {
		m.Grid[i] = Cell{Kind: CellFloor}
	}
	m.set(1, 1, Cell{Kind: CellGap})
	if !m.Pathable(1, 1) {
		t.Fatal("gap should be pathfindable")
	}
	if m.At(1, 1).Walkable() {
		t.Fatal("gap should not be walkable")
	}
}
