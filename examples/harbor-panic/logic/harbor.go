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
	"math"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt"
	hsim "github.com/joeycumines/go-pabt/examples/harbor-panic/sim"
)

type (
	harborState struct {
		ctx     context.Context
		harbor  *hsim.Harbor
		actorID string
	}

	stateInterface interface {
		getHarbor() *hsim.Harbor
	}

	stateVar interface {
		stateVar(state stateInterface) (any, error)
	}

	heldItemVar struct {
		ActorID string
	}
	heldItemValue struct {
		ItemID string
	}

	positionVar struct {
		SpriteID string
	}
	positionInfo struct {
		X, Y float64
		W, H int32
	}
	positionValue struct {
		Positions map[string]*positionInfo
	}

	simpleCond struct {
		key   any
		match func(r any) bool
	}

	simpleEffect struct {
		key   any
		value any
	}

	simpleAction struct {
		conditions []pabt.IConditions
		effects    pabt.Effects
		node       bt.Node
	}
)

var _ pabt.IState = (*harborState)(nil)

var rankByCost bool

func SetRankByCost(enabled bool) {
	rankByCost = enabled
}

func actionCost(act *simpleAction) float64 {
	if act == nil {
		return 0
	}
	// default costs; will be refined per template type via condition keys
	// pick 1.0, place 2.0 + distance/10, move 0.5 + distance/10
	// Heuristic: detect heldItemVar and positionVar conditions
	hasHeldItem := false
	hasPos := false
	for _, conds := range act.conditions {
		for _, c := range conds {
			if _, ok := c.Key().(heldItemVar); ok {
				hasHeldItem = true
			}
			if _, ok := c.Key().(positionVar); ok {
				hasPos = true
			}
		}
	}
	// simple heuristic based on number of effects/conditions to approximate type
	if hasHeldItem && hasPos {
		// pick or place - differentiate by number of conditions
		// this will be overridden by explicit cost field when we add it
		return 1.5
	}
	if hasPos && !hasHeldItem {
		return 0.8
	}
	return 1.0
}

type HarborPlanResult struct {
	Plan *pabt.IPlan
	Node bt.Node
}

func HarborPlan(ctx context.Context, harbor *hsim.Harbor, actorID string) HarborPlanResult {
	state := &harborState{
		ctx:     ctx,
		harbor:  harbor,
		actorID: actorID,
	}

	st := harbor.State()
	goals := st.Goals()
	cubes := st.Cubes()

	// Assign each actor exactly 2 cube->goal pairs matching tcell pattern.
	var successConditions []pabt.IConditions
	actorIdx := -1
	actorsSorted := st.Actors()
	for i, a := range actorsSorted {
		if a.ID == actorID {
			actorIdx = i
			break
		}
	}
	if actorIdx == -1 {
		panic(fmt.Sprintf("harbor plan actor %s not found among %d actors", actorID, len(actorsSorted)))
	}

	pairsPerActor := 2
	startPair := actorIdx * pairsPerActor
	pairCount := 0
	for gi, g := range goals {
		for ci, c := range cubes {
			pairIdx := gi*len(cubes) + ci
			if pairIdx >= startPair && pairCount < pairsPerActor {
				goalID := g.ID
				cubeID := c.ID
				successConditions = append(successConditions, pabt.IConditions{
					&simpleCond{
						key: positionVar{SpriteID: cubeID},
						match: func(r any) bool {
							pos := r.(*positionValue).Positions
							cp := pos[cubeID]
							gp := pos[goalID]
							if cp == nil || gp == nil {
								return false
							}
							return math.Hypot(cp.X-gp.X, cp.Y-gp.Y) < 1.5
						},
					},
				})
				pairCount++
			}
		}
	}

	plan, err := pabt.INew(state, successConditions)
	if err != nil {
		panic(fmt.Sprintf("harbor plan %s: %v", actorID, err))
	}

	return HarborPlanResult{Plan: plan, Node: plan.Node()}
}

func (h *harborState) Variable(key any) (any, error) {
	switch key := key.(type) {
	case stateVar:
		return key.stateVar(h)
	default:
		return nil, fmt.Errorf("unexpected key (%T): %v", key, key)
	}
}

func (h *harborState) Actions(failed pabt.Condition) (actions []pabt.IAction, err error) {
	key := failed.Key()
	add := func(name string) func(a []pabt.IAction, e error) bool {
		return func(a []pabt.IAction, e error) bool {
			if e != nil {
				err = e
				return true
			}
			for _, act := range a {
				for _, effect := range act.Effects() {
					if effect.Key() == key && failed.Match(effect.Value()) {
						actions = append(actions, act)
						break
					}
				}
			}
			return false
		}
	}

	st := h.harbor.State()

	for _, cube := range st.Cubes() {
		if add("pick")(h.templatePick(failed, st, cube)) {
			return
		}
		for x := int32(0); x < hsim.SpaceWidth; x += 2 {
			for y := int32(0); y < hsim.SpaceHeight; y += 2 {
				if add("place")(h.templatePlace(failed, st, x, y, cube)) {
					return
				}
			}
		}
	}

	for x := int32(0); x < hsim.SpaceWidth; x += 2 {
		for y := int32(0); y < hsim.SpaceHeight; y += 2 {
			if add("move")(h.templateMove(failed, st, x, y)) {
				return
			}
		}
	}

	return
}

func (h *harborState) getHarbor() *hsim.Harbor { return h.harbor }

// buildFullPositions builds a complete position map of ALL sprites,
// matching the tcell-pick-and-place pattern exactly. This ensures
// effect values satisfy success condition match functions.
func buildFullPositions(st *hsim.State) map[string]*positionInfo {
	positions := make(map[string]*positionInfo, len(st.Sprites))
	for id, sp := range st.Sprites {
		positions[id] = &positionInfo{X: sp.X, Y: sp.Y, W: sp.W, H: sp.H}
	}
	return positions
}

func (h *harborState) templatePick(failed pabt.Condition, st *hsim.State, cube *hsim.Sprite) (actions []pabt.IAction, err error) {
	var running bool

	// Full position map — cube removed from position (picked up)
	positions := buildFullPositions(st)
	delete(positions, cube.ID)

	actions = append(actions, &simpleAction{
		conditions: []pabt.IConditions{
			{
				&simpleCond{
					key: heldItemVar{ActorID: h.actorID},
					match: func(r any) bool {
						return r.(*heldItemValue).ItemID == ""
					},
				},
				&simpleCond{
					key: positionVar{SpriteID: cube.ID},
					match: func(r any) bool {
						if running {
							return true
						}
						pos := r.(*positionValue).Positions
						cp := pos[cube.ID]
						ap := pos[h.actorID]
						if cp == nil || ap == nil {
							return false
						}
						return math.Hypot(cp.X-ap.X, cp.Y-ap.Y) <= hsim.PickupDistance
					},
				},
			},
		},
		effects: pabt.Effects{
			&simpleEffect{
				key:   heldItemVar{ActorID: h.actorID},
				value: &heldItemValue{ItemID: cube.ID},
			},
			&simpleEffect{
				key:   positionVar{SpriteID: cube.ID},
				value: &positionValue{Positions: positions},
			},
		},
		node: bt.New(
			bt.Sequence,
			bt.New(func([]bt.Node) (bt.Status, error) {
				running = true
				return bt.Success, nil
			}),
			bt.New(h.tickPick(cube.ID)),
		),
	})
	return
}

func (h *harborState) templatePlace(failed pabt.Condition, st *hsim.State, x, y int32, cube *hsim.Sprite) (actions []pabt.IAction, err error) {
	fx, fy := float64(x), float64(y)
	if fx < 0 || fx+float64(cube.W) > float64(hsim.SpaceWidth) || fy < 0 || fy+float64(cube.H) > float64(hsim.SpaceHeight) {
		return nil, nil
	}

	// Full position map — cube placed at new position
	positions := buildFullPositions(st)
	positions[cube.ID] = &positionInfo{X: fx, Y: fy, W: cube.W, H: cube.H}

	actions = append(actions, &simpleAction{
		conditions: []pabt.IConditions{
			{
				&simpleCond{
					key: heldItemVar{ActorID: h.actorID},
					match: func(r any) bool {
						return r.(*heldItemValue).ItemID == cube.ID
					},
				},
				&simpleCond{
					key: positionVar{SpriteID: h.actorID},
					match: func(r any) bool {
						pos := r.(*positionValue).Positions
						ap := pos[h.actorID]
						if ap == nil {
							return false
						}
						return math.Hypot(ap.X-fx, ap.Y-fy) <= hsim.PickupDistance+1.0
					},
				},
			},
		},
		effects: pabt.Effects{
			&simpleEffect{
				key:   heldItemVar{ActorID: h.actorID},
				value: &heldItemValue{ItemID: ""},
			},
			&simpleEffect{
				key:   positionVar{SpriteID: cube.ID},
				value: &positionValue{Positions: positions},
			},
		},
		node: bt.New(h.tickPlace(cube.ID, fx, fy)),
	})
	return
}

func (h *harborState) templateMove(failed pabt.Condition, st *hsim.State, x, y int32) (actions []pabt.IAction, err error) {
	fx, fy := float64(x), float64(y)
	if fx < 0 || fx >= float64(hsim.SpaceWidth) || fy < 0 || fy >= float64(hsim.SpaceHeight) {
		return nil, nil
	}

	// Full position map — actor moved to new position
	positions := buildFullPositions(st)
	positions[h.actorID] = &positionInfo{X: fx, Y: fy, W: 2, H: 2}

	actions = append(actions, &simpleAction{
		conditions: []pabt.IConditions{
			{
				&simpleCond{
					key: positionVar{SpriteID: h.actorID},
					match: func(r any) bool {
						pos := r.(*positionValue).Positions
						return pos[h.actorID] != nil
					},
				},
			},
		},
		effects: pabt.Effects{
			&simpleEffect{
				key:   positionVar{SpriteID: h.actorID},
				value: &positionValue{Positions: positions},
			},
		},
		node: bt.New(h.tickMove(fx, fy)),
	})
	return
}

func (h *harborState) tickPick(cubeID string) bt.Tick {
	return func(children []bt.Node) (bt.Status, error) {
		if err := h.harbor.Grasp(h.ctx, h.actorID, cubeID); err != nil {
			return bt.Failure, nil
		}
		return bt.Success, nil
	}
}

func (h *harborState) tickPlace(cubeID string, x, y float64) bt.Tick {
	return func(children []bt.Node) (bt.Status, error) {
		if err := h.harbor.Release(h.ctx, h.actorID); err != nil {
			return bt.Failure, nil
		}
		if err := h.harbor.Move(h.ctx, cubeID, x, y); err != nil {
			return bt.Failure, nil
		}
		// Verified effects: re-read state to confirm mutation committed.
		// Guards against phantom Success from stale reads (bt-02 §7, ungrounded-02 §21).
		st := h.harbor.State()
		sp := st.Sprites[cubeID]
		if sp == nil {
			return bt.Failure, nil
		}
		if sp.X != x || sp.Y != y {
			return bt.Failure, nil
		}
		return bt.Success, nil
	}
}

func (h *harborState) tickMove(x, y float64) bt.Tick {
	return func(children []bt.Node) (bt.Status, error) {
		if err := h.move(x, y); err != nil {
			return bt.Failure, nil
		}
		return bt.Success, nil
	}
}

func (h *harborState) move(x, y float64) error {
	return h.harbor.Move(h.ctx, h.actorID, x, y)
}

func (e *simpleEffect) Key() any   { return e.key }
func (e *simpleEffect) Value() any { return e.value }

func (c *simpleCond) Key() any             { return c.key }
func (c *simpleCond) Match(value any) bool { return c.match(value) }

func (a *simpleAction) Conditions() []pabt.IConditions { return a.conditions }
func (a *simpleAction) Effects() pabt.Effects          { return a.effects }
func (a *simpleAction) Node() bt.Node                  { return a.node }

func (v heldItemVar) stateVar(state stateInterface) (any, error) {
	st := state.getHarbor().State()
	sp := st.Sprites[v.ActorID]
	itemID := ""
	if sp != nil && sp.HeldItem != nil {
		itemID = sp.HeldItem.ID
	}
	return &heldItemValue{ItemID: itemID}, nil
}

func (v positionVar) stateVar(state stateInterface) (any, error) {
	st := state.getHarbor().State()
	positions := make(map[string]*positionInfo, len(st.Sprites))
	for id, sp := range st.Sprites {
		positions[id] = &positionInfo{X: sp.X, Y: sp.Y, W: sp.W, H: sp.H}
	}
	return &positionValue{Positions: positions}, nil
}

// NewTestHarborState creates a harborState for testing verified effects.
func NewTestHarborState(ctx context.Context, harbor *hsim.Harbor, actorID string) TestHarborState {
	return TestHarborState{hs: &harborState{ctx: ctx, harbor: harbor, actorID: actorID}}
}

// TestHarborState wraps harborState for test access.
type TestHarborState struct {
	hs *harborState
}

// TickPlace exposes tickPlace for testing.
func (t TestHarborState) TickPlace(cubeID string, x, y float64) bt.Tick {
	return t.hs.tickPlace(cubeID, x, y)
}
