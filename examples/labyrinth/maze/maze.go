//go:build example

package maze

import "math/rand"

// CellKind classifies a maze cell for rendering, collision, and pathfinding.
type CellKind uint8

const (
	CellWall     CellKind = iota // solid, impassable
	CellFloor                    // walkable ground
	CellPlatform                 // walkable, raised
	CellGap                      // must be jumped; standing on it is falling
)

// Cell is one tile of the maze grid.
type Cell struct {
	Kind CellKind
	// Height is the floor elevation in cell units. Ground is 0; a platform
	// is 1. Gaps carry the height of the floor they fall onto (0).
	Height int
}

// Vec2 is a world-space position. The maze grid uses one unit per cell, so
// cell (cx, cy) occupies [cx, cx+1) x [cy, cy+1).
type Vec2 struct {
	X, Y float64
}

// Maze is a deterministic, seeded maze grid. It is immutable after Generate.
type Maze struct {
	W, H int // dimensions in cells
	Grid []Cell

	Start Vec2 // runner spawn (center of a floor cell)
	Gate  Vec2 // escape gate (center of a floor cell)
}

// At returns the cell at (cx, cy). Out-of-range reads are walls.
func (m *Maze) At(cx, cy int) Cell {
	if cx < 0 || cy < 0 || cx >= m.W || cy >= m.H {
		return Cell{Kind: CellWall, Height: 0}
	}
	return m.Grid[cy*m.W+cx]
}

// Walkable reports whether an entity can stand on the cell. Gaps are not
// walkable (you fall); platforms and floor are.
func (c Cell) Walkable() bool {
	return c.Kind == CellFloor || c.Kind == CellPlatform
}

// Generate builds a maze of size w x h from the given seed. Dimensions are
// forced to odd values >= 9 so the recursive-backtracker corridors align.
// The result is byte-identical for the same seed.
func Generate(seed uint64, w, h int) *Maze {
	if w < 9 {
		w = 9
	}
	if h < 9 {
		h = 9
	}
	if w%2 == 0 {
		w++
	}
	if h%2 == 0 {
		h++
	}

	rng := rand.New(rand.NewSource(int64(seed)))
	m := &Maze{W: w, H: h, Grid: make([]Cell, w*h)}
	for i := range m.Grid {
		m.Grid[i] = Cell{Kind: CellWall}
	}

	// Recursive backtracker on odd coordinates. Cells at odd (x,y) are
	// rooms; even coordinates are walls until carved.
	type pt struct{ x, y int }
	stack := []pt{{1, 1}}
	m.set(1, 1, Cell{Kind: CellFloor})
	dirs := [][2]int{{0, -2}, {0, 2}, {-2, 0}, {2, 0}}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		// Shuffle neighbors.
		var open [][2]int
		for _, d := range dirs {
			nx, ny := cur.x+d[0], cur.y+d[1]
			if nx > 0 && ny > 0 && nx < w-1 && ny < h-1 && m.At(nx, ny).Kind == CellWall {
				open = append(open, d)
			}
		}
		if len(open) == 0 {
			stack = stack[:len(stack)-1]
			continue
		}
		d := open[rng.Intn(len(open))]
		nx, ny := cur.x+d[0], cur.y+d[1]
		// Carve the wall between and the destination.
		m.set(cur.x+d[0]/2, cur.y+d[1]/2, Cell{Kind: CellFloor})
		m.set(nx, ny, Cell{Kind: CellFloor})
		stack = append(stack, pt{nx, ny})
	}

	// Carve a few rooms to create open spaces.
	rooms := 2 + rng.Intn(3)
	for i := 0; i < rooms; i++ {
		rw, rh := 3+rng.Intn(3), 3+rng.Intn(3)
		rx := 1 + rng.Intn(w-rw-1)
		ry := 1 + rng.Intn(h-rh-1)
		for y := ry; y < ry+rh; y++ {
			for x := rx; x < rx+rw; x++ {
				if m.At(x, y).Kind == CellWall {
					m.set(x, y, Cell{Kind: CellFloor})
				}
			}
		}
	}

	// Add verticality: mark a small number of corridor cells as platforms
	// or gaps. Never touch start/gate neighborhoods.
	m.addVerticality(rng)

	// Start at top-left region, gate at bottom-right region, both floor.
	sx, sy := m.findFloorNear(1, 1, rng)
	gx, gy := m.findFloorNear(w-2, h-2, rng)
	m.set(sx, sy, Cell{Kind: CellFloor})
	m.set(gx, gy, Cell{Kind: CellFloor})
	m.Start = Vec2{float64(sx) + 0.5, float64(sy) + 0.5}
	m.Gate = Vec2{float64(gx) + 0.5, float64(gy) + 0.5}

	return m
}

func (m *Maze) set(cx, cy int, c Cell) {
	if cx >= 0 && cy >= 0 && cx < m.W && cy < m.H {
		m.Grid[cy*m.W+cx] = c
	}
}

// addVerticality sprinkles raised platforms and jump gaps over corridor
// floor cells, avoiding cells adjacent to start/gate (set later, so we just
// avoid the extreme corners).
func (m *Maze) addVerticality(rng *rand.Rand) {
	count := m.W * m.H / 40
	for i := 0; i < count; i++ {
		x := 2 + rng.Intn(m.W-4)
		y := 2 + rng.Intn(m.H-4)
		if m.At(x, y).Kind != CellFloor {
			continue
		}
		if rng.Intn(2) == 0 {
			m.set(x, y, Cell{Kind: CellPlatform, Height: 1})
		} else {
			m.set(x, y, Cell{Kind: CellGap})
		}
	}
}

// findFloorNear walks from a target toward the interior until it finds a
// floor cell, so start/gate always land on walkable ground.
func (m *Maze) findFloorNear(tx, ty int, rng *rand.Rand) (int, int) {
	for r := 0; r < m.W+m.H; r++ {
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				x, y := tx+dx, ty+dy
				if x > 0 && y > 0 && x < m.W-1 && y < m.H-1 && m.At(x, y).Kind == CellFloor {
					return x, y
				}
			}
		}
	}
	return 1, 1
}
