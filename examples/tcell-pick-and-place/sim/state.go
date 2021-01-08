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

// +build example

package sim

import (
	"fmt"
	"sync"
)

type (
	State struct {
		SpaceWidth     int32
		SpaceHeight    int32
		PickupDistance float64
		// Sprites will enumerate all sprites, note that each value will be one of Actor, Cube, or Goal, which may all
		// be compared by equality (to identify the actual underlying thing they refer to), where the map key is
		// the actual Sprite, and the value is a (detached) snapshot of the same
		Sprites    map[Sprite]Sprite
		PlanConfig PlanConfig
	}

	PlanConfig struct {
		Actors []Actor
	}

	Criteria map[CriteriaKey]CriteriaValue

	CriteriaKey struct {
		Cube Cube
		Goal Goal
	}

	CriteriaValue struct {
	}

	Sprite interface {
		Position() (x, y float64)
		Size() (w, h int32)
		Shape() Shape
		Space() Space
		Velocity() (dx, dy float64)
		Stopped() bool
		Image() []rune
		Collides(space Space, shape Shape) bool
		Deleted() bool
		snapshot() Sprite
		sprite() *spriteModel
	}

	Actor struct {
		spriteState
		actorState
	}

	Cube struct {
		spriteState
		cubeState
	}

	Goal struct {
		spriteState
		goalState
	}

	state struct {
		plan    PlanConfig
		mu      sync.RWMutex
		sprites map[*spriteModel]*spriteModel
		actors  map[*actorModel]*actorModel
		cubes   map[*cubeModel]*cubeModel
		goals   map[*goalModel]*goalModel
	}

	spriteState struct {
		state *state
		model *spriteModel
	}

	actorState struct {
		state *state
		model *actorModel
	}

	cubeState struct {
		state *state
		model *cubeModel
	}

	goalState struct {
		state *state
		model *goalModel
	}
)

var (
	_ Sprite = Actor{}
	_ Sprite = Cube{}
	_ Sprite = Goal{}
)

// ActorHeldItemReleaseShape will return a Shape modeling where the Sprite (item) would be released, were it held by
// the given Actor (actor), then released, at the given (visible / screen) position (of actor), denoted by x and y,
// without performing ValidateShape, checking for collision, etc, also note that a panic will occur if either actor or
// item are a not valid keys from the receiver's Sprites map
func (s *State) ActorHeldItemReleaseShape(actor Actor, x, y int32, item Sprite) Shape {
	var actorValue Actor
	{
		sprite, ok := s.Sprites[actor]
		if ok {
			actorValue, ok = sprite.(Actor)
		}
		if !ok {
			panic(fmt.Errorf(`sim.State.ActorHeldItemReleaseShape invalid actor`))
		}
	}

	itemValue, ok := s.Sprites[item]
	if !ok {
		panic(fmt.Errorf(`sim.State.ActorHeldItemReleaseShape invalid item`))
	}

	var (
		w, _   = actorValue.Size()
		rw, rh = itemValue.Size()
		rx, ry = actorHeldItemReleasePosition(x, y, w, rw, rh)
	)

	return NewSpriteShape(rx, ry, rw, rh)
}

// ValidateShape will return an error if shape is not within the bounds of the space (receiver's SpaceWidth and
// SpaceHeight)
func (s *State) ValidateShape(shape Shape) error {
	var (
		x, y = shape.Position()
		w, h = shape.Size()
	)
	return validateSpriteSpace(s.SpaceWidth, s.SpaceHeight, x, y, w, h)
}

func (s *state) State() *State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sprites := make(map[Sprite]Sprite, len(s.sprites))
	for k, v := range s.sprites {
		sprite := s.new(k, v.Owner)
		sprites[sprite] = sprite.snapshot()
	}
	return &State{
		SpaceWidth:     spaceWidth,
		SpaceHeight:    spaceHeight,
		PickupDistance: pickupDistance,
		Sprites:        sprites,
		PlanConfig:     s.plan,
	}
}
func (s *state) new(sprite *spriteModel, owner interface{}) Sprite {
	switch owner := owner.(type) {
	case *actorModel:
		return Actor{spriteState{s, sprite}, actorState{s, owner}}
	case *cubeModel:
		return Cube{spriteState{s, sprite}, cubeState{s, owner}}
	case *goalModel:
		return Goal{spriteState{s, sprite}, goalState{s, owner}}
	default:
		panic(owner)
	}
}

func (x Actor) Deleted() bool    { return x.actorState.Deleted() || x.spriteState.Deleted() }
func (x Actor) snapshot() Sprite { return Actor{x.spriteState.snapshot(), x.actorState.snapshot()} }

func (x Cube) Deleted() bool    { return x.cubeState.Deleted() || x.spriteState.Deleted() }
func (x Cube) snapshot() Sprite { return Cube{x.spriteState.snapshot(), x.cubeState.snapshot()} }

func (x Goal) Deleted() bool    { return x.goalState.Deleted() || x.spriteState.Deleted() }
func (x Goal) snapshot() Sprite { return Goal{x.spriteState.snapshot(), x.goalState.snapshot()} }

func (s spriteState) Deleted() bool {
	if s.state != nil {
		s.state.mu.RLock()
		defer s.state.mu.RUnlock()
		if _, ok := s.state.sprites[s.model]; ok {
			return false
		}
	}
	return true
}
func (s spriteState) Shape() Shape { return s.getRLock().Shape }
func (s spriteState) Space() Space { return s.getRLock().Space }
func (s spriteState) Velocity() (float64, float64) {
	v := s.getRLock()
	return v.DX, v.DY
}
func (s spriteState) Stopped() bool { return s.getRLock().Stop }
func (s spriteState) Image() []rune { return s.getRLock().Image }
func (s spriteState) Position() (float64, float64) {
	v := s.getRLock()
	return v.X, v.Y
}
func (s spriteState) Size() (int32, int32) {
	v := s.getRLock()
	return v.Width, v.Height
}
func (s spriteState) Collides(space Space, shape Shape) bool {
	if s.state != nil {
		s.state.mu.RLock()
		defer s.state.mu.RUnlock()
	}
	return s.get().collides(space, shape)
}
func (s spriteState) sprite() *spriteModel { return s.model }
func (s spriteState) snapshot() spriteState {
	if s.state != nil {
		return spriteState{nil, s.get()}
	}
	return s
}
func (s spriteState) get() *spriteModel {
	if s.state != nil {
		if v, ok := s.state.sprites[s.model]; ok {
			return v
		}
	}
	return s.model
}
func (s spriteState) getRLock() *spriteModel {
	if s.state != nil {
		s.state.mu.RLock()
		defer s.state.mu.RUnlock()
	}
	return s.get()
}

func (s actorState) Deleted() bool {
	if s.state != nil {
		s.state.mu.RLock()
		defer s.state.mu.RUnlock()
		if _, ok := s.state.actors[s.model]; ok {
			return false
		}
	}
	return true
}
func (s actorState) Criteria() Criteria { return s.getRLock().Criteria }
func (s actorState) Keyboard() bool     { return s.getRLock().Keyboard }
func (s actorState) HeldItem() Sprite   { return s.getRLock().HeldItem }
func (s actorState) snapshot() actorState {
	if s.state != nil {
		return actorState{nil, s.get()}
	}
	return s
}
func (s actorState) get() *actorModel {
	if s.state != nil {
		if v, ok := s.state.actors[s.model]; ok {
			return v
		}
	}
	return s.model
}
func (s actorState) getRLock() *actorModel {
	if s.state != nil {
		s.state.mu.RLock()
		defer s.state.mu.RUnlock()
	}
	return s.get()
}

func (s cubeState) Deleted() bool {
	if s.state != nil {
		s.state.mu.RLock()
		defer s.state.mu.RUnlock()
		if _, ok := s.state.cubes[s.model]; ok {
			return false
		}
	}
	return true
}
func (s cubeState) snapshot() cubeState {
	if s.state != nil {
		return cubeState{nil, s.get()}
	}
	return s
}
func (s cubeState) get() *cubeModel {
	if s.state != nil {
		if v, ok := s.state.cubes[s.model]; ok {
			return v
		}
	}
	return s.model
}

func (s goalState) Deleted() bool {
	if s.state != nil {
		s.state.mu.RLock()
		defer s.state.mu.RUnlock()
		if _, ok := s.state.goals[s.model]; ok {
			return false
		}
	}
	return true
}
func (s goalState) snapshot() goalState {
	if s.state != nil {
		return goalState{nil, s.get()}
	}
	return s
}
func (s goalState) get() *goalModel {
	if s.state != nil {
		if v, ok := s.state.goals[s.model]; ok {
			return v
		}
	}
	return s.model
}
