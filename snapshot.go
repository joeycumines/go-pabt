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
	"slices"
	"time"

	bt "github.com/joeycumines/go-behaviortree"
)

// CallbackPhase distinguishes a fact read once while the plan was built from a
// fact read while the plan was ticking.
//
// The two are never merged. A goal's condition keys are read during
// construction, before any tick exists, and reporting them as tick evidence
// would claim a world that never ticked.
type CallbackPhase string

const (
	// CallbackPhaseConstruction is a call made while building the plan.
	CallbackPhaseConstruction CallbackPhase = "construction"

	// CallbackPhaseTick is a call made while the plan was ticking.
	CallbackPhaseTick CallbackPhase = "tick"
)

// CallbackKind names which application callback was observed.
type CallbackKind string

const (
	CallbackKey              CallbackKind = "key"
	CallbackVariableRead     CallbackKind = "variable_read"
	CallbackMatch            CallbackKind = "condition_match"
	CallbackStateActions     CallbackKind = "state_actions"
	CallbackActionEffects    CallbackKind = "action_effects"
	CallbackActionConditions CallbackKind = "action_conditions"
	CallbackActionNode       CallbackKind = "action_node"
	CallbackEffectValue      CallbackKind = "effect_value"
	CallbackExpected         CallbackKind = "expected"
)

// MatchUse says why Match was called.
//
// The planner calls [Condition.Match] in three different worlds: against the
// value read from application state, against a candidate effect's value while
// deciding whether an action supports its post-condition, and against an
// existing effect's value while testing for a conflict. The same condition can
// see all three in one tick, so without the use a recorded match is ambiguous.
type MatchUse string

const (
	UseRuntimeEvaluation MatchUse = "runtime_evaluation"
	UseEffectSupport     MatchUse = "effect_support"
	UseConflictTest      MatchUse = "conflict_test"
)

// SubjectRole names what a callback was called on.
type SubjectRole string

const (
	SubjectCondition SubjectRole = "condition"
	SubjectEffect    SubjectRole = "effect"
	SubjectAction    SubjectRole = "action"
)

// RecordedError is an error observed during a tick.
//
// The message is the application's own text and is not sanitised here, because
// this package cannot know which literals are secret. A consumer must scrub it
// before the error reaches a digest, a response body, a log, or durable storage.
type RecordedError struct {
	Type    string
	Message string
}

// CallbackEvidence is one observed call into application code.
//
// Evidence holds the values the application returned. It copies nothing,
// because an arbitrary Go value cannot be copied without a codec, so a snapshot
// is only meaningful while the tick that produced it is still running.
type CallbackEvidence struct {
	Ordinal   uint32
	Phase     CallbackPhase
	Kind      CallbackKind
	Use       MatchUse
	Subject   SubjectRole
	SubjectID OccurrenceID
	NodeID    OccurrenceID
	Key       any
	Input     any
	Result    any
	Known     bool
	Err       *RecordedError
	Duration  time.Duration
}

// NodeOccurrence is one behavior-tree node in a captured tick.
type NodeOccurrence struct {
	ID      OccurrenceID
	Lineage LineageID
	Parent  OccurrenceID
	Ordinal uint32
	Type    NodeType
	Name    string
	Status  NodeStatusSnapshot
	Frame   *FrameInfo
}

// ConditionOccurrence is one condition instance in a captured tick.
//
// Expected is the value the application declared through [Expecting].
// ExpectedKnown distinguishes a declared value, including a declared nil, from a
// condition that declares nothing, which is reported as unavailable rather than
// inferred from Key.
type ConditionOccurrence struct {
	ID            OccurrenceID
	Key           any
	Expected      any
	ExpectedKnown bool
	NodeID        OccurrenceID
	PostNodeID    OccurrenceID
	Truth         bt.Status
	TruthKnown    bool
}

// EffectOccurrence is one effect an action declares.
type EffectOccurrence struct {
	ID         OccurrenceID
	ActionID   OccurrenceID
	Key        any
	Value      any
	ValueKnown bool
}

// ActionOccurrence is one action instance in a captured tick.
//
// It carries no lineage: expansion creates an action once and never copies it,
// so there is no derivation to record. Only nodes are copied.
type ActionOccurrence struct {
	ID              OccurrenceID
	NodeID          OccurrenceID
	Name            string
	PostconditionID OccurrenceID
	EffectIDs       []OccurrenceID
	ConditionIDs    []OccurrenceID
}

// TickSnapshot is a detached, tick-scoped description of what a plan did.
//
// A snapshot is valid only for the tick that produced it. Its values alias
// application state that the next tick may change, so a consumer must encode
// what it needs before the tick returns rather than retaining the snapshot.
type TickSnapshot struct {
	PlanRevision  uint64
	CapturedAt    time.Time
	RootID        OccurrenceID
	StateRevision StateRevision
	Coherence     StateCoherence
	CloseError    *RecordedError
	Construction  []CallbackEvidence
	Callbacks     []CallbackEvidence
	Nodes         []NodeOccurrence
	Conditions    []ConditionOccurrence
	Effects       []EffectOccurrence
	Actions       []ActionOccurrence
	Executions    []ActionEvidence
}

// actionCache holds application-derived facts read once, so that inspecting a
// captured plan never calls back into application code.
//
// It is populated only while a capture is active. A plan built with [New] keeps
// the previous behaviour of computing these facts on demand, so no existing
// caller sees a new application call.
type actionCache struct {
	name    string
	effects []EffectOccurrence
	values  map[any]any
}

// snapshot walks the plan and builds its detached description. It performs no
// application callback: names, keys, and effect values all come from the cache
// populated when the planner called into the application.
func (p *Plan[T]) snapshot(c *tickCapture[T]) TickSnapshot {
	out := TickSnapshot{
		PlanRevision:  p.PlannerRevision(),
		CapturedAt:    time.Now(),
		Construction:  p.construction,
		Callbacks:     c.callbacks,
		Executions:    c.executions,
		StateRevision: c.stateRevision,
		Coherence:     c.coherence,
		CloseError:    c.closeError,
	}
	if p.root == nil {
		return out
	}
	out.RootID = p.root.id
	// A condition is reachable from more than one node: before expansion it is
	// the leaf that evaluates it, and after expansion the leaf is replaced by a
	// ppa root while a post-condition node re-evaluates the same condition. The
	// same precondition is therefore seen twice, and is collected once here.
	seen := make(map[OccurrenceID]bool)
	var walk func(n *node[T], parent OccurrenceID, ordinal uint32)
	walk = func(n *node[T], parent OccurrenceID, ordinal uint32) {
		out.Nodes = append(out.Nodes, NodeOccurrence{
			ID:      n.id,
			Lineage: n.lineage,
			Parent:  parent,
			Ordinal: ordinal,
			Type:    n.nodeType(),
			Name:    n.cachedName(),
			Status:  n.status.Snapshot(),
			Frame:   n.frame,
		})
		if pre := n.precondition; pre != nil && !seen[pre.id] {
			seen[pre.id] = true
			out.Conditions = append(out.Conditions, ConditionOccurrence{
				ID:            pre.id,
				Key:           pre.key,
				Expected:      pre.expected,
				ExpectedKnown: pre.expectedKnown,
				NodeID:        pre.root.id,
				PostNodeID:    pre.postNodeID,
				Truth:         pre.status,
				TruthKnown:    pre.ticked,
			})
		}
		if act := n.action; act != nil && act.node == n {
			out.Actions = append(out.Actions, act.occurrence(n.id))
			if act.cache != nil {
				out.Effects = append(out.Effects, act.cache.effects...)
			}
		}
		var child uint32
		for c := n.first; c != nil; c = c.next {
			walk(c, n.id, child)
			child++
		}
	}
	walk(p.root, "", 0)
	return out
}

// occurrence describes an action from facts already captured for it.
func (a *action[T]) occurrence(nodeID OccurrenceID) ActionOccurrence {
	out := ActionOccurrence{
		ID:              a.id,
		NodeID:          nodeID,
		PostconditionID: a.postconditionID,
	}
	if a.cache != nil {
		out.Name = a.cache.name
		for _, e := range a.cache.effects {
			out.EffectIDs = append(out.EffectIDs, e.ID)
		}
	}
	for _, or := range a.or {
		for _, pre := range or.and {
			if pre != nil && pre.id != "" {
				out.ConditionIDs = append(out.ConditionIDs, pre.id)
			}
		}
	}
	// Map iteration order is unspecified, so the guarded conditions are sorted
	// to keep a snapshot's bytes stable for identical planner state.
	slices.Sort(out.ConditionIDs)
	return out
}

// capturedEffectID returns the occurrence identity of one of the action's
// effects, or an empty identity when no capture ran.
func (a *action[T]) capturedEffectID(key any) OccurrenceID {
	if a.cache == nil {
		return ""
	}
	for _, e := range a.cache.effects {
		if e.Key == key {
			return e.ID
		}
	}
	return ""
}

// capturedEffects returns the action's effects as captured, or nil when no
// capture ran.
func (a *action[T]) capturedEffects() []EffectOccurrence {
	if a.cache == nil {
		return nil
	}
	return a.cache.effects
}
