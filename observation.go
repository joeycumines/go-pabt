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

import (
	"time"

	bt "github.com/joeycumines/go-behaviortree"
)

// RelationResult is a verifier's conclusion about one effect/post-condition
// pair.
type RelationResult string

const (
	// RelationPreserves means the effect leaves the post-condition satisfied.
	RelationPreserves RelationResult = "preserves"

	// RelationBreaks means the effect leaves the post-condition unsatisfied.
	RelationBreaks RelationResult = "breaks"

	// RelationDoesNotDecide means the effect neither satisfies nor breaks the
	// post-condition, for example because the effect touches a different
	// variable.
	RelationDoesNotDecide RelationResult = "does_not_decide"

	// RelationContradicted means the post-condition's status after the action
	// is inconsistent with the effect's declared value.
	RelationContradicted RelationResult = "contradicted"

	// RelationUnavailable means the transition could not be decided.
	RelationUnavailable RelationResult = "unavailable"
)

// ActionExecution is one action node completing during a tick.
type ActionExecution struct {
	ActionID      OccurrenceID
	NodeID        OccurrenceID
	Status        bt.Status
	StatusPresent bool
	Err           *RecordedError
	StartedAt     time.Time
	Duration      time.Duration
}

// EffectObservation is one post-action value an observer read for an effect.
type EffectObservation struct {
	EffectID OccurrenceID
	Value    any
	Known    bool
	Err      *RecordedError
}

// ActionObservation is what an observer reports for one action boundary.
type ActionObservation struct {
	EffectValues []EffectObservation
}

// ActionObservationRequest asks an observer what one action changed.
//
// View is borrowed: the handler must not close it, and the session closes the
// underlying view exactly once after every handler has returned.
type ActionObservationRequest struct {
	PlanRevision    uint64
	ActionID        OccurrenceID
	NodeID          OccurrenceID
	Execution       ActionExecution
	Effects         []EffectOccurrence
	PostconditionID OccurrenceID
	View            BorrowedStateView
}

// ExecutionObserver observes what a single action changed.
//
// The observer is the only source of an observed value. A value that the
// planner merely declared, or that a consumer inferred, is not an observation
// and must not be presented as one.
type ExecutionObserver interface {
	// ObserveExecution reports post-action values for the request's effects.
	ObserveExecution(ActionObservationRequest) (ActionObservation, error)
}

// RelationVerdict is a verifier's conclusion about one effect's contribution to
// one post-condition.
//
// Authoritative is set by the planner, not by the verifier. It is true only
// when the revisions bracketing the action were both read from coherent views
// and the verifier returned no error, so a verdict spanning two live reads is
// always visible as non-authoritative.
type RelationVerdict struct {
	EffectID        OccurrenceID
	PostconditionID OccurrenceID
	Result          RelationResult
	Method          string
	Authoritative   bool
	Err             *RecordedError
}

// TransitionRequest asks a verifier whether an effect produced its
// post-condition.
//
// BeforeRevision and BeforeCoherence are captured at the action's entry from
// the pre-tick view; AfterRevision and AfterCoherence are read from the
// post-action view. Passing both, with their coherence, is what lets the
// planner refuse an authoritative claim that the inputs cannot support.
type TransitionRequest struct {
	PlanRevision    uint64
	ActionID        OccurrenceID
	Execution       ActionExecution
	Effects         []EffectOccurrence
	PostconditionID OccurrenceID
	Observation     ActionObservation
	View            BorrowedStateView
	BeforeRevision  StateRevision
	BeforeCoherence StateCoherence
	AfterRevision   StateRevision
	AfterCoherence  StateCoherence
}

// TransitionVerifier decides whether an effect actually achieved the
// post-condition it is claimed to support.
//
// The verifier is the only source of a causal relation. A shared variable key
// is support, not causation, and a consumer's selection is not evidence.
type TransitionVerifier interface {
	// VerifyTransitions returns one verdict per effect the verifier decided.
	VerifyTransitions(TransitionRequest) ([]RelationVerdict, error)
}

// ActionEvidence bundles one action's execution with what was observed about it
// and what was concluded from it.
type ActionEvidence struct {
	Execution   ActionExecution
	Observation ActionObservation
	Relations   []RelationVerdict
	Err         *RecordedError
}
