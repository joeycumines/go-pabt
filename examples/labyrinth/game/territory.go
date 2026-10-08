//go:build example

package game

import (
	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
)

// TerritoryOf returns the faction element that controls the quadrant
// containing pos. The maze is split into four static quadrants, one per
// faction. S2 keeps territories static so the player can read faction
// ground at a glance; S3 makes control shift as the gang war evolves.
//
// Quadrant layout (maze Y grows downward):
//
//	north-west = Ice (Frost Covenant)
//	north-east = Fire (Ember Legion)
//	south-west = Void (Void Cult)
//	south-east = Lightning (Storm Pact)
func TerritoryOf(m *maze.Maze, pos maze.Vec2) Element {
	east := pos.X >= float64(m.W)/2
	north := pos.Y < float64(m.H)/2
	switch {
	case north && east:
		return ElementFire
	case north && !east:
		return ElementIce
	case !north && east:
		return ElementLightning
	default:
		return ElementVoid
	}
}

// homeTerritoryPos returns a random walkable position inside the given
// faction's home quadrant, at least 9 cells from start and gate. It returns
// false if no suitable cell was found, so the caller can fall back to an
// unbiased spawn.
func (g *Game) homeTerritoryPos(elem Element) (maze.Vec2, bool) {
	for attempts := 0; attempts < 100; attempts++ {
		cx := 1 + g.rng.Intn(g.Maze.W-2)
		cy := 1 + g.rng.Intn(g.Maze.H-2)
		if !g.Maze.At(cx, cy).Walkable() {
			continue
		}
		pos := maze.Vec2{X: float64(cx) + 0.5, Y: float64(cy) + 0.5}
		if TerritoryOf(g.Maze, pos) != elem {
			continue
		}
		if distSq(pos, g.Maze.Start) < 81 || distSq(pos, g.Maze.Gate) < 81 {
			continue
		}
		return pos, true
	}
	return maze.Vec2{}, false
}
