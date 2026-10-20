//go:build example

package maze

import "math"

// node is one entry in the A* search history: the best cost found so far
// and the predecessor cell index used to reconstruct the path.
type node struct {
	g, f float64
	prev int
}

// Path finds the shortest walkable cell path from the cell containing start
// to the cell containing goal using A* with a Manhattan heuristic. Returns nil
// if either endpoint is unwalkable or unreachable. Movement is 4-directional
// (no diagonals) to match grid collision. Gaps are pathfindable (the player
// jumps over them) but not standable (handled by collision, not pathfinding).
func (m *Maze) Path(start, goal Vec2) []Vec2 {
	sc := m.cellOf(start)
	gc := m.cellOf(goal)
	if !m.Pathable(sc.x, sc.y) || !m.Pathable(gc.x, gc.y) {
		return nil
	}

	idx := func(x, y int) int { return y*m.W + x }
	open := newHeap()
	nodes := make(map[int]*node)
	h := func(x, y int) float64 {
		dx := math.Abs(float64(x - gc.x))
		dy := math.Abs(float64(y - gc.y))
		return dx + dy
	}
	startIdx := idx(sc.x, sc.y)
	goalIdx := idx(gc.x, gc.y)
	nodes[startIdx] = &node{g: 0, f: h(sc.x, sc.y), prev: -1}
	open.push(startIdx, h(sc.x, sc.y))

	dirs := [][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}
	closed := make(map[int]bool)

	for open.Len() > 0 {
		cur, _ := open.pop()
		if closed[cur] {
			continue
		}
		closed[cur] = true
		if cur == goalIdx {
			return m.reconstruct(nodes, cur)
		}
		cx, cy := cur%m.W, cur/m.W
		for _, d := range dirs {
			nx, ny := cx+d[0], cy+d[1]
			if !m.Pathable(nx, ny) {
				continue
			}
			ni := idx(nx, ny)
			if closed[ni] {
				continue
			}
			ng := nodes[cur].g + 1
			if prev, ok := nodes[ni]; !ok || ng < prev.g {
				nodes[ni] = &node{g: ng, f: ng + h(nx, ny), prev: cur}
				open.push(ni, ng+h(nx, ny))
			}
		}
	}
	return nil
}

// Pathable reports whether a cell can be traversed by the pathfinder. Floors,
// platforms, and gaps are pathable; walls are not. The distinction from
// Walkable is that gaps are pathable (the player jumps) but not walkable
// (standing on them is falling).
func (m *Maze) Pathable(cx, cy int) bool {
	c := m.At(cx, cy)
	return c.Kind != CellWall
}

// PathWalkable finds the shortest 4-connected path from start to goal traversing
// only Walkable cells (CellFloor and CellPlatform, avoiding CellGap so non-jumping
// NPCs never route into pits). It also trims the starting cell center out[0] when
// the actor is already at or progressing past out[0] toward out[1], preventing
// backward mid-cell oscillation. Falls back to m.Path if start/goal are only
// connected across a gap.
func (m *Maze) PathWalkable(start, goal Vec2) []Vec2 {
	sc := m.cellOf(start)
	gc := m.cellOf(goal)
	if !m.Pathable(sc.x, sc.y) || !m.Pathable(gc.x, gc.y) {
		return nil
	}

	idx := func(x, y int) int { return y*m.W + x }
	open := newHeap()
	nodes := make(map[int]*node)
	h := func(x, y int) float64 {
		dx := math.Abs(float64(x - gc.x))
		dy := math.Abs(float64(y - gc.y))
		return dx + dy
	}
	startIdx := idx(sc.x, sc.y)
	goalIdx := idx(gc.x, gc.y)
	nodes[startIdx] = &node{g: 0, f: h(sc.x, sc.y), prev: -1}
	open.push(startIdx, h(sc.x, sc.y))

	dirs := [][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}
	closed := make(map[int]bool)

	var out []Vec2
	for open.Len() > 0 {
		cur, _ := open.pop()
		if closed[cur] {
			continue
		}
		closed[cur] = true
		if cur == goalIdx {
			out = m.reconstruct(nodes, cur)
			break
		}
		cx, cy := cur%m.W, cur/m.W
		for _, d := range dirs {
			nx, ny := cx+d[0], cy+d[1]
			if (nx != gc.x || ny != gc.y) && !m.At(nx, ny).Walkable() {
				continue
			}
			if !m.Pathable(nx, ny) {
				continue
			}
			ni := idx(nx, ny)
			if closed[ni] {
				continue
			}
			ng := nodes[cur].g + 1
			if prev, ok := nodes[ni]; !ok || ng < prev.g {
				nodes[ni] = &node{g: ng, f: ng + h(nx, ny), prev: cur}
				open.push(ni, ng+h(nx, ny))
			}
		}
	}
	if len(out) == 0 {
		out = m.Path(start, goal)
	}
	if len(out) >= 2 {
		// Trim out[0] if start is already at out[0] or progressing toward out[1].
		vx := out[1].X - out[0].X
		vy := out[1].Y - out[0].Y
		sx := start.X - out[0].X
		sy := start.Y - out[0].Y
		if math.Hypot(sx, sy) < 1e-4 || (sx*vx+sy*vy) >= -1e-6 {
			out = out[1:]
		}
	}
	return out
}

func (m *Maze) cellOf(p Vec2) struct{ x, y int } {
	x := int(math.Floor(p.X))
	y := int(math.Floor(p.Y))
	return struct{ x, y int }{x, y}
}

func (m *Maze) reconstruct(nodes map[int]*node, cur int) []Vec2 {
	var out []Vec2
	for cur != -1 {
		x, y := cur%m.W, cur/m.W
		out = append(out, Vec2{float64(x) + 0.5, float64(y) + 0.5})
		cur = nodes[cur].prev
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// heap is a min-heap of (cellIndex, priority) pairs for A*.
type heap struct {
	items []heapItem
}

type heapItem struct {
	idx  int
	prio float64
}

func newHeap() *heap     { return &heap{} }
func (h *heap) Len() int { return len(h.items) }
func (h *heap) push(i int, p float64) {
	h.items = append(h.items, heapItem{i, p})
	for c := len(h.items) - 1; c > 0; {
		par := (c - 1) / 2
		if h.items[par].prio <= h.items[c].prio {
			break
		}
		h.items[par], h.items[c] = h.items[c], h.items[par]
		c = par
	}
}
func (h *heap) pop() (int, float64) {
	top := h.items[0]
	last := len(h.items) - 1
	h.items[0] = h.items[last]
	h.items = h.items[:last]
	for c := 0; ; {
		l, r := 2*c+1, 2*c+2
		sm := c
		if l < len(h.items) && h.items[l].prio < h.items[sm].prio {
			sm = l
		}
		if r < len(h.items) && h.items[r].prio < h.items[sm].prio {
			sm = r
		}
		if sm == c {
			break
		}
		h.items[c], h.items[sm] = h.items[sm], h.items[c]
		c = sm
	}
	return top.idx, top.prio
}
