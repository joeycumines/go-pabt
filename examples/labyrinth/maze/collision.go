//go:build example

package maze

import "math"

// Radius is the default cylinder radius for the runner and minotaurs.
const Radius = 0.3

// Solid reports whether a cell blocks horizontal movement. Walls and gaps
// block ground movement (gaps block because standing on them is falling,
// so ground collision treats them as holes). Platforms are solid at their
// raised height but are handled by height logic, not Solid.
func (m *Maze) Solid(cx, cy int) bool {
	c := m.At(cx, cy)
	return c.Kind == CellWall
}

// Resolve pushes a cylinder of the given radius at pos out of any solid
// cells, returning the corrected position. Movement is clamped to the grid
// bounds. This is the authoritative server movement step for horizontal
// collision.
func (m *Maze) Resolve(pos Vec2, radius float64) Vec2 {
	pos.X = clamp(pos.X, radius, float64(m.W)-radius)
	pos.Y = clamp(pos.Y, radius, float64(m.H)-radius)

	minX := int(math.Floor(pos.X - radius))
	maxX := int(math.Floor(pos.X + radius))
	minY := int(math.Floor(pos.Y - radius))
	maxY := int(math.Floor(pos.Y + radius))

	for cy := minY; cy <= maxY; cy++ {
		for cx := minX; cx <= maxX; cx++ {
			if !m.Solid(cx, cy) {
				continue
			}
			// Nearest point on the cell AABB to the cylinder center.
			nx := clamp(pos.X, float64(cx), float64(cx+1))
			ny := clamp(pos.Y, float64(cy), float64(cy+1))
			dx, dy := pos.X-nx, pos.Y-ny
			dist2 := dx*dx + dy*dy
			if dist2 >= radius*radius {
				continue
			}
			if dist2 > 1e-12 {
				dist := math.Sqrt(dist2)
				push := radius - dist
				pos.X += dx / dist * push
				pos.Y += dy / dist * push
			} else {
				// Center inside the cell: push out along the smallest axis.
				left := pos.X - float64(cx)
				right := float64(cx+1) - pos.X
				up := pos.Y - float64(cy)
				down := float64(cy+1) - pos.Y
				mn := math.Min(math.Min(left, right), math.Min(up, down))
				switch mn {
				case left:
					pos.X = float64(cx) - radius
				case right:
					pos.X = float64(cx+1) + radius
				case up:
					pos.Y = float64(cy) - radius
				default:
					pos.Y = float64(cy+1) + radius
				}
			}
		}
	}
	return pos
}

// HeightAt returns the ground elevation under pos. Gaps return a low value
// so gravity logic treats them as falling; platforms return their height.
func (m *Maze) HeightAt(pos Vec2) int {
	c := m.At(int(math.Floor(pos.X)), int(math.Floor(pos.Y)))
	switch c.Kind {
	case CellPlatform:
		return c.Height
	case CellGap:
		return -1 // falling
	default:
		return 0
	}
}

// OnGap reports whether the cylinder center is over a gap cell.
func (m *Maze) OnGap(pos Vec2) bool {
	return m.At(int(math.Floor(pos.X)), int(math.Floor(pos.Y))).Kind == CellGap
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
