/*
   Copyright 2021 Joseph Cumines

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package pabt

import "fmt"

// StateRevision identifies a version of application state.
//
// PlanRevision is the planner's own structural version and is always filled in.
// WorldRevision and ModelRevision are supplied by the application through a
// state view; an application that has no such notion leaves them empty, which
// is different from claiming a revision it cannot support.
type StateRevision struct {
	PlanRevision  uint64 `json:"planRevision"`
	WorldRevision string `json:"worldRevision"`
	ModelRevision string `json:"modelRevision"`
}

// StateCoherence says how much a state view can be trusted to represent one
// world rather than a moving target.
type StateCoherence string

const (
	// CoherenceSnapshot is a view over an immutable copy of state. Every read
	// within the view returns the same world.
	CoherenceSnapshot StateCoherence = "snapshot"

	// CoherenceRevisioned is a view that reports a revision and is guaranteed
	// stable while that revision holds.
	CoherenceRevisioned StateCoherence = "revisioned"

	// CoherenceLive reads the application's state directly. Two reads can
	// return different worlds, so a transition claim spanning two live reads
	// is not authoritative.
	CoherenceLive StateCoherence = "live"

	// CoherenceUnknown is a view that could not state its own coherence.
	CoherenceUnknown StateCoherence = "unknown"
)

// coherent reports whether two reads bracketing a transition can support an
// authoritative claim about what the transition changed.
func (c StateCoherence) coherent() bool {
	switch c {
	case CoherenceSnapshot, CoherenceRevisioned:
		return true
	}
	return false
}

// StateViewRole says which boundary a state view was opened for.
type StateViewRole string

const (
	// StateViewPreTick is opened once before the planner executes and is the
	// only world the planner reads from for the whole tick.
	StateViewPreTick StateViewRole = "pre_tick"

	// StateViewPostAction is opened after one action node returns, for
	// observing what that action changed.
	StateViewPostAction StateViewRole = "post_action"
)

// StateViewRequest asks a provider to open a view for one boundary.
type StateViewRequest struct {
	Role         StateViewRole
	PlanRevision uint64
	ActionID     OccurrenceID
}

// BorrowedStateView is the read-only, non-owning view handed to an execution
// observer or a transition verifier.
//
// It deliberately has no Close method. The session that opened the underlying
// view owns it and closes it exactly once, so a handler cannot close a view
// another handler is still using.
//
// Only Variable is exposed. Action discovery is a typed call the planner makes
// itself, and Go cannot assign Action[T] to Action[Condition] for an arbitrary
// T, so a borrowed view cannot pretend to offer it.
type BorrowedStateView interface {
	// Variable returns the current value for key.
	Variable(key any) (value any, err error)

	// Revision returns the view's revision.
	Revision() StateRevision

	// Coherence returns how far the view can be trusted as one world.
	Coherence() StateCoherence
}

// TypedStateView is the planner's view of application state for one tick.
//
// The planner reads every condition value and every action list through this
// view rather than through the raw [State], which is what makes a captured tick
// reflect one world instead of a mixture of reads taken at different times.
type TypedStateView[T Condition] interface {
	BorrowedStateView

	// Actions returns the actions that can achieve the failed condition.
	Actions(failed T) ([]Action[T], error)

	// Close releases the view. The caller that opened it owns this call.
	Close() error
}

// TypedStateViewProvider opens typed state views.
//
// A provider is authoritative for a captured plan: if one is configured, the
// planner uses it and never silently falls back to reading the raw state.
type TypedStateViewProvider[T Condition] interface {
	// OpenStateView opens a view for the requested boundary.
	OpenStateView(StateViewRequest) (TypedStateView[T], error)
}

// LiveStateViewProvider opens views that read the planner's own [State]
// directly and label themselves [CoherenceLive].
//
// It is the default for a captured plan, chosen at construction rather than as
// a fallback after a configured provider fails.
type LiveStateViewProvider[T Condition] struct {
	state State[T]
}

// NewLiveStateViewProvider returns a provider reading the given state.
func NewLiveStateViewProvider[T Condition](state State[T]) *LiveStateViewProvider[T] {
	return &LiveStateViewProvider[T]{state: state}
}

// OpenStateView implements [TypedStateViewProvider].
func (p *LiveStateViewProvider[T]) OpenStateView(StateViewRequest) (TypedStateView[T], error) {
	if p == nil || p.state == nil {
		return nil, fmt.Errorf(`pabt: nil state for live state view`)
	}
	return liveStateView[T]{state: p.state}, nil
}

type liveStateView[T Condition] struct {
	state State[T]
}

func (v liveStateView[T]) Variable(key any) (any, error) { return v.state.Variable(key) }

func (v liveStateView[T]) Actions(failed T) ([]Action[T], error) {
	return v.state.Actions(failed)
}

func (liveStateView[T]) Revision() StateRevision { return StateRevision{} }

func (liveStateView[T]) Coherence() StateCoherence { return CoherenceLive }

func (liveStateView[T]) Close() error { return nil }

// borrowed erases a typed view for a handler that has no business depending on
// the application's condition type.
type borrowed[T Condition] struct {
	inner TypedStateView[T]
}

func (b borrowed[T]) Variable(key any) (any, error) { return b.inner.Variable(key) }

func (b borrowed[T]) Revision() StateRevision { return b.inner.Revision() }

func (b borrowed[T]) Coherence() StateCoherence { return b.inner.Coherence() }

var _ BorrowedStateView = borrowed[Condition]{}
