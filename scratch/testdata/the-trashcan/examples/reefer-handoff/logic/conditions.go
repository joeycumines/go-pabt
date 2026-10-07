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
	"fmt"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt"
	"github.com/joeycumines/go-pabt/examples/reefer-handoff/sim"
)

// condition is a pabt.Condition over one sim.VarKey.
//
// The key is the whole identity. Two conditions about the same variable are the
// same condition as far as the planner is concerned, and a plan can therefore be
// keyed per job by naming the job's tractor and reefer in every key — which is
// what stops one plan from reading another's state by guessing a key.
type condition struct {
	key   sim.VarKey
	match func(value any) bool

	// node is the destination, for a condition about position. The key names the
	// variable and the node is what it constrains, so the constraint is carried
	// here rather than folded into the key: the key has to stay the variable, or
	// two conditions about the same position would look like two variables.
	node sim.NodeID
}

// Key returns the variable this condition constrains.
func (c condition) Key() any { return c.key }

// Match reports whether a value satisfies the constraint.
func (c condition) Match(value any) bool { return c.match(value) }

// String renders the condition the way a debugger record shows it, so a key in a
// tree and a key in a frame are spelled the same.
func (c condition) String() string { return c.key.String() }

// The four post-conditions a plan holds. They are the only ones it tries to make
// true; the generation-time reads in actions.go are consulted while choosing an
// action and never become post-conditions, because a value the world can change
// underneath is not something a plan should hold.

func delivered(r sim.ReeferID) condition {
	return condition{
		key:   sim.DeliveredKey(r),
		match: func(v any) bool { b, ok := v.(bool); return ok && b },
	}
}

func holding(t sim.TractorID, r sim.ReeferID) condition {
	return condition{
		key:   sim.HoldingKey(t, r),
		match: func(v any) bool { b, ok := v.(bool); return ok && b },
	}
}

func at(t sim.TractorID, n sim.NodeID) condition {
	return condition{
		key: sim.AtKey(t),
		// The frame answers a position key with a sim.NodeID and an effect declares
		// one, so both cases are accepted. A match that only understood one of them
		// would make the condition permanently unsatisfiable, and a plan would then
		// either stall or run its action past a guard it had not passed.
		match: func(v any) bool {
			switch got := v.(type) {
			case sim.NodeID:
				return got == n
			case string:
				return sim.NodeID(got) == n
			}
			return false
		},
		node: n,
	}
}

func tagConfirmed(r sim.ReeferID) condition {
	return condition{
		key:   sim.TagKey(r),
		match: func(v any) bool { b, ok := v.(bool); return ok && b },
	}
}

// effect is a pabt.Effect over one sim.VarKey. The value is what the action
// promises the world will become, and the planner admits an action only when one
// of its effects satisfies the condition that failed — so an effect is a claim
// the world has to honour, not a description of what usually happens.
type effect struct {
	key   sim.VarKey
	value any
}

// Key returns the variable this effect changes.
func (e effect) Key() any { return e.key }

// Value returns the value the action expects the variable to take.
func (e effect) Value() any { return e.value }

// The effect values. They mirror the types sim.Observation.Read returns, and a
// mismatch between one of these and what the frame reports is a bug in the pair,
// which the runner's verification catches rather than a silent mismatch.

// trueValue is the value every satisfied post-condition takes, and nodeValue the
// value a position effect declares. Both are the types the frame answers with, so
// an effect and a later read of the same key are the same type and a condition's
// match sees one shape rather than two.
var (
	trueValue = true
	nodeValue = func(n sim.NodeID) sim.NodeID { return n }
)

func deliveredEffect(r sim.ReeferID) effect {
	return effect{key: sim.DeliveredKey(r), value: trueValue}
}

func holdingEffect(t sim.TractorID, r sim.ReeferID) effect {
	return effect{key: sim.HoldingKey(t, r), value: trueValue}
}

func atEffect(t sim.TractorID, n sim.NodeID) effect {
	return effect{key: sim.AtKey(t), value: nodeValue(n)}
}

func tagEffect(r sim.ReeferID) effect {
	return effect{key: sim.TagKey(r), value: trueValue}
}

// action is a pabt.IAction: the preconditions guarding it, the effects it
// promises, and the behavior-tree node that does the work.
//
// The conditions slice is a set of OR alternatives, and the conditions inside one
// group are an AND. A guard that must hold alongside another therefore belongs in
// one group with it, not beside it.
type action struct {
	name       string
	conditions []pabt.IConditions
	effects    pabt.Effects
	node       bt.Node
}

// Conditions returns the guards that must hold before this action runs.
func (a *action) Conditions() []pabt.IConditions { return a.conditions }

// Effects returns what this action claims it will change.
func (a *action) Effects() pabt.Effects { return a.effects }

// Node returns the behavior-tree node that performs the work.
func (a *action) Node() bt.Node { return a.node }

// String names the action, so a tree read shows which template and which target
// produced a node rather than a bare occurrence ID.
func (a *action) String() string { return a.name }

// String satisfies the conditions' and effects' own String methods, so a key
// rendered in a debugger record is the key a frame would answer.
var (
	_ pabt.Condition = condition{}
	_ pabt.Effect    = effect{}
	_ pabt.IAction   = (*action)(nil)
	_ fmt.Stringer   = (*action)(nil)
	_ fmt.Stringer   = condition{}
)
