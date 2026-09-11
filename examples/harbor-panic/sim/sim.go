// Copyright 2021 Joseph Cumines
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build example
// +build example

package sim

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sync"
)

const (
	SpaceWidth     int32   = 40
	SpaceHeight    int32   = 18
	PickupDistance float64 = 1.5
	StepDistance   float64 = 1.0
)

type SpriteKind int

const (
	KindActor SpriteKind = iota
	KindCube
	KindGoal
	KindWall
	KindHumanForklift
)

type Sprite struct {
	ID       string
	Kind     SpriteKind
	X, Y     float64
	W, H     int32
	Rune     rune
	HeldItem *Sprite
}

type State struct {
	Sprites map[string]*Sprite
	Walls   []*Sprite
	Tick    int64
}

type Harbor struct {
	mu         sync.Mutex
	state      *State
	stormEvery int
	humanSpeed float64
	rng        *rand.Rand
}

func NewHarbor(stormEvery int, humanSpeed float64, seed int64) *Harbor {
	h := &Harbor{
		state:      &State{Sprites: make(map[string]*Sprite)},
		stormEvery: stormEvery,
		humanSpeed: humanSpeed,
		rng:        rand.New(rand.NewSource(seed)),
	}
	h.init()
	return h
}

func (h *Harbor) init() {
	s := h.state
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("CRANE-%d", i)
		s.Sprites[id] = &Sprite{ID: id, Kind: KindActor, X: float64(2 + i*9), Y: 2, W: 2, H: 2, Rune: rune('0' + i)}
	}
	colors := []rune{'R', 'G', 'B', 'Y', 'M', 'C'}
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("CONTAINER-%d", i+1)
		s.Sprites[id] = &Sprite{ID: id, Kind: KindCube, X: float64(5 + i*5), Y: 8, W: 1, H: 1, Rune: colors[i]}
	}
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("BERTH-%d", i)
		s.Sprites[id] = &Sprite{ID: id, Kind: KindGoal, X: float64(3 + i*9), Y: 15, W: 2, H: 2, Rune: '!'}
	}
	s.Sprites["HUMAN"] = &Sprite{ID: "HUMAN", Kind: KindHumanForklift, X: 20, Y: 9, W: 1, H: 1, Rune: 'H'}
	wallPositions := [][2]float64{{10, 4}, {10, 5}, {10, 6}, {25, 10}, {25, 11}, {25, 12}}
	for i, pos := range wallPositions {
		id := fmt.Sprintf("WALL-%d", i)
		w := &Sprite{ID: id, Kind: KindWall, X: pos[0], Y: pos[1], W: 1, H: 1, Rune: '#'}
		s.Sprites[id] = w
		s.Walls = append(s.Walls, w)
	}
}

func (h *Harbor) State() *State {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.snapshotLocked()
}

func (h *Harbor) snapshotLocked() *State {
	cp := &State{Sprites: make(map[string]*Sprite, len(h.state.Sprites)), Tick: h.state.Tick}
	for k, v := range h.state.Sprites {
		s := *v
		cp.Sprites[k] = &s
	}
	cp.Walls = h.state.Walls
	return cp
}

func (h *Harbor) Step(ctx context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.state.Tick++

	if h.stormEvery > 0 && int(h.state.Tick)%h.stormEvery == 0 {
		for _, sp := range h.state.Sprites {
			if sp.Kind == KindGoal {
				sp.X = float64(h.rng.Intn(int(SpaceWidth - sp.W)))
				sp.Y = float64(12 + h.rng.Intn(int(SpaceHeight-12-sp.H)))
			}
		}
	}

	human := h.state.Sprites["HUMAN"]
	if human != nil {
		var nearest *Sprite
		minDist := math.MaxFloat64
		for _, sp := range h.state.Sprites {
			if sp.Kind == KindCube && sp.HeldItem == nil {
				d := math.Hypot(sp.X-human.X, sp.Y-human.Y)
				if d < minDist {
					minDist = d
					nearest = sp
				}
			}
		}
		if nearest != nil {
			dx := nearest.X - human.X
			dy := nearest.Y - human.Y
			d := math.Hypot(dx, dy)
			if d > 0.1 {
				human.X += (dx / d) * h.humanSpeed
				human.Y += (dy / d) * h.humanSpeed
			}
			if d < PickupDistance {
				nearest.X = float64(h.rng.Intn(int(SpaceWidth - nearest.W)))
				nearest.Y = float64(h.rng.Intn(int(SpaceHeight - nearest.H)))
			}
		}
	}

	return nil
}

func (h *Harbor) Move(ctx context.Context, spriteID string, x, y float64) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	sp, ok := h.state.Sprites[spriteID]
	if !ok {
		return fmt.Errorf("sprite %s not found", spriteID)
	}
	if x < 0 || x+float64(sp.W) > float64(SpaceWidth) || y < 0 || y+float64(sp.H) > float64(SpaceHeight) {
		return fmt.Errorf("out of bounds")
	}
	for _, w := range h.state.Walls {
		if x < w.X+float64(w.W) && x+float64(sp.W) > w.X && y < w.Y+float64(w.H) && y+float64(sp.H) > w.Y {
			return fmt.Errorf("wall collision")
		}
	}
	sp.X = x
	sp.Y = y
	return nil
}

func (h *Harbor) Grasp(ctx context.Context, actorID, cubeID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	actor, ok := h.state.Sprites[actorID]
	if !ok {
		return fmt.Errorf("actor %s not found", actorID)
	}
	cube, ok := h.state.Sprites[cubeID]
	if !ok {
		return fmt.Errorf("cube %s not found", cubeID)
	}
	if actor.HeldItem != nil {
		return fmt.Errorf("already holding")
	}
	d := math.Hypot(cube.X-actor.X, cube.Y-actor.Y)
	if d > PickupDistance {
		return fmt.Errorf("too far")
	}
	actor.HeldItem = cube
	return nil
}

func (h *Harbor) Release(ctx context.Context, actorID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	actor, ok := h.state.Sprites[actorID]
	if !ok {
		return fmt.Errorf("actor %s not found", actorID)
	}
	if actor.HeldItem == nil {
		return fmt.Errorf("not holding")
	}
	actor.HeldItem = nil
	return nil
}

func (s *State) Actors() []*Sprite {
	var out []*Sprite
	for _, sp := range s.Sprites {
		if sp.Kind == KindActor {
			out = append(out, sp)
		}
	}
	return out
}

func (s *State) Cubes() []*Sprite {
	var out []*Sprite
	for _, sp := range s.Sprites {
		if sp.Kind == KindCube {
			out = append(out, sp)
		}
	}
	return out
}

func (s *State) Goals() []*Sprite {
	var out []*Sprite
	for _, sp := range s.Sprites {
		if sp.Kind == KindGoal {
			out = append(out, sp)
		}
	}
	return out
}
