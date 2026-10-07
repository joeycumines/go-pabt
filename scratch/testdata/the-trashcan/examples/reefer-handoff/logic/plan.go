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

	"github.com/joeycumines/go-pabt"
	"github.com/joeycumines/go-pabt/examples/reefer-handoff/sim"
)

// state is the pabt.IState a generation runs against, and the
// TypedStateViewProvider that feeds it.
//
// It holds one pinned observation. The runner pins a frame at the start of a root
// tick and unpins it at the end, and every read in that tick — every condition
// value and every action list — comes from that one observation. That is what
// stops a tick from seeing half of one world and half of another, and it is why
// the state is shared across generations rather than rebuilt with each one: the
// frame changes, the reader does not.
type state struct {
	runner *Runner

	pinned    sim.Observation
	hasPinned bool
	revision  uint64
}

// Variable returns a key's value from the pinned frame.
//
// A key about another tractor's job is refused rather than answered, so a plan
// cannot read across jobs by guessing a key. The error names the key, because a
// refused read is a bug in the plan's construction and not a runtime condition to
// be tolerated.
func (s *state) Variable(key any) (any, error) {
	if !s.hasPinned {
		return nil, fmt.Errorf("logic: %s read %v with no frame pinned", s.runner.tug, key)
	}
	k, ok := key.(sim.VarKey)
	if !ok {
		return nil, fmt.Errorf("logic: %s asked for an unexpected key of type %T", s.runner.tug, key)
	}
	v, ok := s.pinned.Read(k)
	if !ok {
		return nil, fmt.Errorf("logic: %s cannot answer %s from the frame at %s",
			s.runner.tug, k, s.pinned.Revision)
	}
	return v, nil
}

// pin holds a frame for the duration of one root tick.
func (s *state) pin(o sim.Observation, planRevision uint64) {
	s.pinned = o
	s.hasPinned = true
	s.revision = planRevision
}

// unpin drops the frame, so a read outside a tick fails loudly.
func (s *state) unpin() { s.hasPinned = false }

// frame returns the pinned observation, if there is one.
func (s *state) frame() (sim.Observation, bool) { return s.pinned, s.hasPinned }

// expectationLocked returns the versions a command issued from the pinned frame
// must expect. The slot is the candidate slot for a Place and empty otherwise,
// so the slot version names the world the runner reasoned over for the slot it
// aimed at.
func (s *state) expectationLocked(slot sim.SlotID) sim.Expectation {
	if !s.hasPinned {
		return sim.Expectation{}
	}
	return s.pinned.Expectation(slot)
}

// OpenStateView satisfies TypedStateViewProvider, so a plan reads every value
// through a labelled view instead of the raw state. The view is a snapshot and
// says so, which is what lets a verdict about a transition be authoritative.
func (s *state) OpenStateView(req pabt.StateViewRequest) (pabt.TypedStateView[pabt.Condition], error) {
	if !s.hasPinned {
		return nil, fmt.Errorf("logic: %s opened a %s view with no frame pinned", s.runner.tug, req.Role)
	}
	return frameView{state: s, role: req.Role}, nil
}

// frameView is the pinned frame presented as a planner state view.
type frameView struct {
	state *state
	role  pabt.StateViewRole
}

func (v frameView) Variable(key any) (any, error) { return v.state.Variable(key) }

func (v frameView) Actions(failed pabt.Condition) ([]pabt.IAction, error) {
	return v.state.Actions(failed)
}

// Close is a no-op: the runner owns the frame and unpins it at the end of the
// tick. A view that could be closed twice would let one handler end another's.
func (v frameView) Close() error { return nil }

// Revision names the world the frame was taken at and the plan structure it was
// read against. PlanRevision is filled by the planner; the world revision is a
// string because that is the type the field declares.
func (v frameView) Revision() pabt.StateRevision {
	return pabt.StateRevision{
		PlanRevision:  v.state.revision,
		WorldRevision: v.state.pinned.Revision,
	}
}

// Coherence says the view is one whole world.
func (v frameView) Coherence() pabt.StateCoherence { return pabt.CoherenceSnapshot }

var (
	_ pabt.IState                                 = (*state)(nil)
	_ pabt.TypedStateViewProvider[pabt.Condition] = (*state)(nil)
	_ pabt.TypedStateView[pabt.Condition]         = frameView{}
)

// provider wraps the state so it can be handed to pabt.New as an option.
func (s *state) provider() pabt.TypedStateViewProvider[pabt.Condition] { return s }
