//go:build example

package game

import (
	mrand "math/rand"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

// spawnPowerUps places one power-up per element at a random walkable cell in
// the authoritative world. Called once from NewGame before the loop starts,
// so it needs no synchronization.
func spawnPowerUps(w *world.World, rng *mrand.Rand) {
	for elem := 0; elem < elementCount; elem++ {
		for attempts := 0; attempts < 200; attempts++ {
			cx := 1 + rng.Intn(w.Maze.W-2)
			cy := 1 + rng.Intn(w.Maze.H-2)
			if !w.Maze.At(cx, cy).Walkable() {
				continue
			}
			pos := maze.Vec2{X: float64(cx) + 0.5, Y: float64(cy) + 0.5}
			// Avoid start/gate proximity.
			if distSq(pos, w.Maze.Start) < 25 || distSq(pos, w.Maze.Gate) < 25 {
				continue
			}
			w.PowerUps = append(w.PowerUps, &world.PowerUp{
				Pos:     pos,
				Element: elem,
				Active:  true,
			})
			break
		}
	}
}

// checkPowerUpPickup runs on the single-writer goroutine. If the runner is
// standing on an active power-up it deactivates it and sets the runner's
// element, returning the picked-up power-up (or nil).
func checkPowerUpPickup(w *world.World) *world.PowerUp {
	for _, pu := range w.PowerUps {
		if !pu.Active {
			continue
		}
		dx := pu.Pos.X - w.Runner.Pos.X
		dy := pu.Pos.Y - w.Runner.Pos.Y
		if dx*dx+dy*dy < 0.5*0.5 {
			pu.Active = false
			w.Runner.Element = pu.Element
			return pu
		}
	}
	return nil
}
