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

package logic

import (
	"context"
	"fmt"
	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt"
	"github.com/joeycumines/go-pabt/examples/tcell-pick-and-place/sim"
	"log"
	"math"
	"sync"
)

type (
	pickAndPlace struct {
		ctx        context.Context
		simulation sim.Simulation
		actor      sim.Actor
		moveState  struct {
			mu     sync.Mutex
			cancel context.CancelFunc
			done   chan struct{}
		}
	}

	stateInterface interface {
		getSimulation() sim.Simulation
	}

	stateVar interface {
		stateVar(state stateInterface) (any, error)
	}

	positionInfo struct {
		Space sim.Space
		Shape sim.Shape
	}

	pabtCond pabt.Condition

	// VARIABLES

	// Note that the following types suffixed with "Var" are the variable keys, implementing stateVar, while the
	// types suffixed with "Value" are the values returned by the stateVar method (always a pointer to a struct, and
	// never a nil interface value, for simplicity on the other end).

	// heldItemVar is used to map changes to the actor's held item
	heldItemVar struct {
		Actor sim.Actor
	}
	heldItemValue struct {
		item sim.Sprite
	}

	// positionVar is used to map changes to the physical position of each sprite, relative to other(s)
	positionVar struct {
		Sprite sim.Sprite
	}
	positionValue struct {
		// positions are all the _relevant_ sprite positions
		positions map[sim.Sprite]*positionInfo
	}

	// CONDITIONS

	simpleCond struct {
		key   any
		match func(r any) bool
	}

	//moveCollisionCond struct {
	//	pabtCond
	//	sprite sim.Sprite
	//}

	// EFFECTS

	simpleEffect struct {
		key   any
		value any
	}

	// ACTIONS

	simpleAction struct {
		conditions []pabt.Conditions
		effects    pabt.Effects
		node       bt.Node
	}
)

var (
	_ pabt.State = (*pickAndPlace)(nil)
)

func PickAndPlace(ctx context.Context, simulation sim.Simulation, actor sim.Actor) bt.Node {
	state := &pickAndPlace{
		ctx:        ctx,
		simulation: simulation,
		actor:      actor,
	}

	var successConditions []pabt.Conditions
	for pair := range actor.Criteria() {
		successConditions = append(successConditions, pabt.Conditions{
			// cube is on the goal (at least partially, though cubes are only 1x1 anyway)
			&simpleCond{
				key: positionVar{Sprite: pair.Cube},
				match: func(r any) bool {
					var (
						positions = r.(*positionValue).positions
						cubePos   = positions[pair.Cube]
						goalPos   = positions[pair.Goal]
					)
					return cubePos != nil &&
						goalPos != nil &&
						cubePos.Shape != nil &&
						goalPos.Shape != nil &&
						cubePos.Shape.Collides(goalPos.Shape)
				},
			},
		})
	}

	plan, err := pabt.INew(state, successConditions)
	if err != nil {
		panic(err)
	}

	return plan.Node()
}

func (p *pickAndPlace) Variable(key any) (any, error) {
	switch key := key.(type) {
	case stateVar:
		return key.stateVar(p)
	default:
		return nil, fmt.Errorf(`unexpected key (%T): %v`, key, key)
	}
}

func (p *pickAndPlace) Actions(failed pabt.Condition) (actions []pabt.Action, err error) {
	var (
		key = failed.Key()
		add = func(name string, limit int) func(a []pabt.Action, e error) bool {
			return func(a []pabt.Action, e error) bool {
				if e != nil {
					err = e
					return true
				}
				var count int
				for i, a := range a {
					var ok bool
					for _, effect := range a.Effects() {
						if effect.Key() == key && failed.Match(effect.Value()) {
							ok = true
							break
						}
					}
					if ok {
						log.Printf(`adding %d from %s for %T...`, i, name, key)
						actions = append(actions, a)
						count++
						if count == limit {
							break
						}
					}
				}
				return false
			}
		}
		snapshot = p.simulation.State()
	)

	for sprite := range snapshot.Sprites {
		if sprite == p.actor {
			continue
		}
		switch sprite := sprite.(type) {
		case sim.Cube:
			if add(`pick`, 0)(p.templatePick(failed, snapshot, sprite)) {
				return
			}

			for x := int32(0); x < snapshot.SpaceWidth; x++ {
				for y := int32(0); y < snapshot.SpaceHeight; y++ {
					if add(`place`, 0)(p.templatePlace(failed, snapshot, x, y, sprite)) {
						return
					}
				}
			}
		}
	}

	for x := int32(0); x < snapshot.SpaceWidth; x++ {
		for y := int32(0); y < snapshot.SpaceHeight; y++ {
			if add(`move`, 0)(p.templateMove(failed, snapshot, x, y)) {
				return
			}
		}
	}

	return
}

func (p *pickAndPlace) getSimulation() sim.Simulation { return p.simulation }

// templatePick will template actions to pickup the given sprite, note that these actions will be conditional on the
// sprite remaining in it's current, visible position, since that is critical to the planning (e.g. of actor movement)
//
//	fig 7.4
//
//	Pick(i)
//	con: o_r ∈ N_o_i
//	     h = /0
//	eff: h = i
//
//	Pick(cube)
//	con: o_r ∈ N_o_cube
//	     h = /0
//	eff: h = cube
func (p *pickAndPlace) templatePick(failed pabt.Condition, snapshot *sim.State, sprite sim.Sprite) (actions []pabt.Action, err error) {
	var ox, oy int32
	if spriteValue, ok := snapshot.Sprites[sprite]; !ok {
		return
	} else if spriteShape := spriteValue.Shape(); spriteShape == nil {
		return
	} else {
		ox, oy = spriteShape.Position()
	}

	positions := make(map[sim.Sprite]*positionInfo, len(snapshot.Sprites))
	for k, v := range snapshot.Sprites {
		positions[k] = &positionInfo{
			Space: v.Space(),
			Shape: v.Shape(),
		}
	}
	positions[sprite].Shape = nil

	pickupDistance := snapshot.PickupDistance

	var running bool

	snapshot = nil
	actions = append(actions, &simpleAction{
		conditions: []pabt.Conditions{
			{
				&simpleCond{
					key: positionVar{Sprite: sprite},
					match: func(r any) bool {
						if pos := r.(*positionValue).positions[sprite]; pos != nil && pos.Shape != nil {
							if running {
								return true
							}
							if nx, ny := pos.Shape.Position(); ox == nx && oy == ny {
								return true
							}
						}
						return false
					},
				},
				&simpleCond{
					key: heldItemVar{Actor: p.actor},
					match: func(r any) bool {
						return r.(*heldItemValue).item == nil
					},
				},
				&simpleCond{
					key: positionVar{Sprite: p.actor},
					match: func(r any) bool {
						var (
							positions = r.(*positionValue).positions
							spritePos = positions[sprite]
							actorPos  = positions[p.actor]
						)
						return spritePos != nil &&
							actorPos != nil &&
							spritePos.Shape != nil &&
							actorPos.Shape != nil &&
							spritePos.Shape.Distance(actorPos.Shape) <= pickupDistance
					},
				},
			},
		},
		effects: pabt.Effects{
			&simpleEffect{
				key:   heldItemVar{Actor: p.actor},
				value: &heldItemValue{item: sprite},
			},
			&simpleEffect{
				key:   positionVar{Sprite: sprite},
				value: &positionValue{positions: positions},
			},
		},
		node: bt.New(
			bt.Sequence,
			bt.New(func([]bt.Node) (bt.Status, error) {
				running = true
				return bt.Success, nil
			}),
			bt.New(bt.Async(p.tickPick(sprite))),
		),
	})
	return
}

// templatePlace accepts the actor position (x, y) and sprite (to place)
//
//	Place(i, p)
//	con: o_r ∈ N_p
//	     h = i
//	eff: o_i = p
func (p *pickAndPlace) templatePlace(failed pabt.Condition, snapshot *sim.State, x, y int32, sprite sim.Sprite) (actions []pabt.Action, err error) {
	spriteValue, ok := snapshot.Sprites[sprite]
	if !ok {
		return
	}

	var (
		spriteShape      sim.Shape
		positions        map[sim.Sprite]*positionInfo
		noCollisionConds pabt.Conditions
	)
	{
		var actorShape sim.Shape
		if actorValue, ok := snapshot.Sprites[p.actor]; ok {
			w, h := actorValue.Size()
			actorShape = sim.NewSpriteShape(x, y, w, h)
		}
		if actorShape == nil || snapshot.ValidateShape(actorShape) != nil {
			return
		}

		spriteShape = snapshot.ActorHeldItemReleaseShape(p.actor, x, y, sprite)
		if spriteShape == nil || snapshot.ValidateShape(spriteShape) != nil {
			return
		}

		positions = make(map[sim.Sprite]*positionInfo, len(snapshot.Sprites))
		for k, v := range snapshot.Sprites {
			positions[k] = &positionInfo{
				Space: v.Space(),
				Shape: v.Shape(),
			}
			if k != p.actor && k != sprite {
				k := k
				noCollisionConds = append(noCollisionConds, &simpleCond{
					key: positionVar{Sprite: k},
					match: func(r any) bool {
						if v := r.(*positionValue).positions[k]; v != nil && v.Shape != nil && v.Space.Collides(spriteValue.Space()) && v.Shape.Collides(spriteShape) {
							return false
						}
						return true
					},
				})
			}
		}
		positions[sprite].Shape = spriteShape
	}

	snapshot = nil
	actions = append(actions, &simpleAction{
		conditions: []pabt.Conditions{
			append(append(pabt.Conditions(nil),
				&simpleCond{
					key: heldItemVar{Actor: p.actor},
					match: func(r any) bool {
						return r.(*heldItemValue).item == sprite
					},
				},
				&simpleCond{
					key: positionVar{Sprite: p.actor},
					match: func(r any) bool {
						if v := r.(*positionValue).positions[p.actor]; v != nil && v.Shape != nil {
							if cx, cy := v.Shape.Position(); x == cx && y == cy {
								return true
							}
						}
						return false
					},
				},
			), noCollisionConds...),
		},
		effects: pabt.Effects{
			// actor will not be holding anything
			&simpleEffect{
				key:   heldItemVar{Actor: p.actor},
				value: new(heldItemValue),
			},
			&simpleEffect{
				key:   positionVar{Sprite: sprite},
				value: &positionValue{positions: positions},
			},
		},
		node: bt.New(bt.Async(p.tickPlace(sprite))),
	})
	return
}

// templateMove like
//
// MoveTo(p, τ)
// con: τ ⊂ CollFree
// eff: o_r = p
func (p *pickAndPlace) templateMove(failed pabt.Condition, snapshot *sim.State, x, y int32) (actions []pabt.Action, err error) {
	// dumb "pathfinding"
	var (
		space  sim.Space
		shapes []sim.Shape
	)
	if actorValue, ok := snapshot.Sprites[p.actor]; !ok {
		return
	} else if shape := actorValue.Shape(); shape == nil {
		return
	} else {
		space = actorValue.Space()

		var (
			cx, cy = actorValue.Position()
			dx, dy = float64(x) - cx, float64(y) - cy
		)
		if d := math.Sqrt(math.Pow(dx, 2) + math.Pow(dy, 2)); !math.IsNaN(d) && !math.IsInf(d, 0) && d > 0 {
			dx, dy = dx/d, dy/d
		}

		for nx, ny := sim.RoundPosition(cx, cy); nx != x || ny != y; nx, ny = sim.RoundPosition(cx, cy) {
			cx += dx
			cy += dy

			shape = shape.Clone()
			shape.SetPosition(sim.RoundPosition(cx, cy))

			if snapshot.ValidateShape(shape) != nil {
				return
			}

			shapes = append(shapes, shape)
		}
	}
	if len(shapes) == 0 {
		return
	}

	var (
		noCollisionConds pabt.Conditions
		positions        = make(map[sim.Sprite]*positionInfo, len(snapshot.Sprites))
	)
	for k, v := range snapshot.Sprites {
		positions[k] = &positionInfo{
			Space: v.Space(),
			Shape: v.Shape(),
		}
		if k != p.actor {
			k := k
			noCollisionConds = append(noCollisionConds, &simpleCond{
				key: positionVar{Sprite: k},
				match: func(r any) bool {
					if v := r.(*positionValue).positions[k]; v != nil && v.Shape != nil && v.Space.Collides(space) {
						for _, shape := range shapes {
							if shape.Collides(v.Shape) {
								return false
							}
						}
					}
					return true
				},
			})
		}
	}
	positions[p.actor].Shape = shapes[len(shapes)-1]

	snapshot = nil
	actions = append(actions, &simpleAction{
		conditions: []pabt.Conditions{
			noCollisionConds,
		},
		effects: pabt.Effects{
			&simpleEffect{
				key:   positionVar{Sprite: p.actor},
				value: &positionValue{positions: positions},
			},
		},
		node: bt.New(bt.Async(p.tickMove(x, y))),
	})
	return
}

func (p *pickAndPlace) tickPick(sprite sim.Sprite) bt.Tick {
	return func(children []bt.Node) (bt.Status, error) {
		log.Printf("pick(%s): start\n", string(sprite.Image()))
		if err := p.simulation.Grasp(p.ctx, p.actor, sprite); err != nil {
			log.Printf("pick(%s): failure\n", string(sprite.Image()))
			return bt.Failure, nil
		}
		log.Printf("pick(%s): success\n", string(sprite.Image()))
		return bt.Success, nil
	}
}
func (p *pickAndPlace) tickMove(x, y int32) bt.Tick {
	return func(children []bt.Node) (bt.Status, error) {
		log.Printf("move(%d, %d): start\n", x, y)
		if err := p.move(p.actor, float64(x), float64(y)); err != nil {
			log.Printf("move(%d, %d): failure\n", x, y)
			return bt.Failure, nil
		}
		log.Printf("move(%d, %d): success\n", x, y)
		return bt.Success, nil
	}
}
func (p *pickAndPlace) tickPlace(sprite sim.Sprite) bt.Tick {
	return func(children []bt.Node) (bt.Status, error) {
		log.Printf("place(%s): start\n", string(sprite.Image()))
		if err := p.simulation.Release(p.ctx, p.actor, sprite); err != nil {
			log.Printf("place(%s): failure\n", string(sprite.Image()))
			return bt.Failure, nil
		}
		log.Printf("place(%s): success\n", string(sprite.Image()))
		return bt.Success, nil
	}
}

func (p *pickAndPlace) move(sprite sim.Sprite, x, y float64) error {
	p.moveState.mu.Lock()

	if p.moveState.cancel != nil {
		p.moveState.cancel()
		<-p.moveState.done
	}

	p.moveState.done = make(chan struct{})
	defer close(p.moveState.done)

	var ctx context.Context
	ctx, p.moveState.cancel = context.WithCancel(p.ctx)
	defer p.moveState.cancel()

	p.moveState.mu.Unlock()

	return p.simulation.Move(ctx, sprite, x, y)
}

func (e *simpleEffect) Key() any   { return e.key }
func (e *simpleEffect) Value() any { return e.value }

func (c *simpleCond) Key() any             { return c.key }
func (c *simpleCond) Match(value any) bool { return c.match(value) }

func (a *simpleAction) Conditions() []pabt.Conditions { return a.conditions }
func (a *simpleAction) Effects() pabt.Effects         { return a.effects }
func (a *simpleAction) Node() bt.Node                 { return a.node }

func (a heldItemVar) stateVar(state stateInterface) (any, error) {
	var r heldItemValue
	if v, ok := state.getSimulation().State().Sprites[a.Actor]; ok {
		if v, ok := v.(sim.Actor); ok {
			r.item = v.HeldItem()
		}
	}
	return &r, nil
}

func (p positionVar) stateVar(state stateInterface) (any, error) {
	var r positionValue
	for k, v := range state.getSimulation().State().Sprites {
		if r.positions == nil {
			r.positions = make(map[sim.Sprite]*positionInfo)
		}
		r.positions[k] = &positionInfo{
			Space: v.Space(),
			Shape: v.Shape(),
		}
	}
	return &r, nil
}
