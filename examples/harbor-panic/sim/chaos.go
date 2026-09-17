//go:build example
// +build example

package sim

import "math/rand"

func chaosFreePos(h *Harbor, w, hh int32, minY, maxY int32, rng *rand.Rand) (float64, float64, bool) {
	for attempt := 0; attempt < 30; attempt++ {
		nx := float64(rng.Intn(int(SpaceWidth - w)))
		ny := float64(minY + int32(rng.Intn(int(maxY-minY-hh+1))))
		free := true
		for _, wall := range h.state.Walls {
			if nx < wall.X+float64(wall.W) && nx+float64(w) > wall.X && ny < wall.Y+float64(wall.H) && ny+float64(hh) > wall.Y {
				free = false
				break
			}
		}
		if free {
			return nx, ny, true
		}
	}
	return 0, 0, false
}

func InjectChaos(h *Harbor, rng *rand.Rand) {
	roll := rng.Intn(3)
	switch roll {
	case 0:
		h.mu.Lock()
		nx, ny, ok := chaosFreePos(h, 1, 3, 0, SpaceHeight-3, rng)
		if ok {
			chaosWall := &Sprite{ID: "WALL-CHAOS-0", Kind: KindWall, X: nx, Y: ny, W: 1, H: 3, Rune: '#'}
			walls := make([]*Sprite, 0, len(h.state.Walls)+1)
			walls = append(walls, h.state.Walls...)
			walls = append(walls, chaosWall)
			h.mu.Unlock()
			h.SetWalls(walls)
		} else {
			h.mu.Unlock()
		}
	case 1:
		h.mu.Lock()
		if human, ok := h.state.Sprites["HUMAN"]; ok {
			nx, ny, found := chaosFreePos(h, human.W, human.H, 0, SpaceHeight-1, rng)
			if found {
				human.X = nx
				human.Y = ny
			}
		}
		h.mu.Unlock()
	case 2:
		h.mu.Lock()
		for _, sp := range h.state.Sprites {
			if sp.Kind == KindGoal {
				nx, ny, found := chaosFreePos(h, sp.W, sp.H, 12, SpaceHeight-1, rng)
				if found {
					sp.X = nx
					sp.Y = ny
				}
			}
		}
		h.mu.Unlock()
	}
}
