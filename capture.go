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
	"fmt"
	"time"

	bt "github.com/joeycumines/go-behaviortree"
)

// TickEnvironment is the per-tick context handed to a captured tick.
type TickEnvironment struct {
	// AttemptID identifies this attempt to the caller. The planner echoes it
	// and does not interpret it.
	AttemptID string
}

// TickExecution is the result of one captured tick.
//
// StatusKnown is false when the tick did not produce one of the three statuses
// go-behaviortree defines, which is different from the tick producing Failure.
type TickExecution struct {
	Status       bt.Status
	StatusKnown  bool
	PlannerError *RecordedError
	CaptureError *RecordedError
	Snapshot     TickSnapshot
	PlanRevision uint64
	StartedAt    time.Time
	CompletedAt  time.Time
}

// CapturedPlanner is the non-generic boundary a durable session drives.
//
// It mentions no application type parameter, so one session implementation can
// drive a plan over any condition type.
type CapturedPlanner interface {
	PlannerRevision() uint64
	TickCaptured(TickEnvironment) TickExecution
}

var _ CapturedPlanner = (*Plan[Condition])(nil)

// NewCaptured constructs a [Plan] that records what it does.
//
// The returned plan reads application state through a [TypedStateViewProvider]
// and reports every call it makes into application code. A plan built with
// [New] keeps the previous behaviour and makes no such report.
func NewCaptured[T Condition](
	state State[T],
	goal []Conditions[T],
	opts ...Option[T],
) (*Plan[T], error) {
	return newPlan[T](state, goal, true, opts...)
}

func newPlan[T Condition](
	state State[T],
	goal []Conditions[T],
	captured bool,
	opts ...Option[T],
) (*Plan[T], error) {
	if state == nil {
		return nil, fmt.Errorf(`pabt: nil state`)
	}
	ids, err := newIdentity()
	if err != nil {
		return nil, err
	}
	p := Plan[T]{config: config[T]{state: state, goal: goal}, ids: ids}
	for _, opt := range opts {
		if err := opt.applyOption(&p.config); err != nil {
			return nil, err
		}
	}
	if captured && p.config.views == nil {
		p.config.views = NewLiveStateViewProvider[T](state)
	}
	// Construction happens with a capture published so the goal's initial
	// condition keys are attributed to the construction phase.
	if captured {
		p.capture.Store(&tickCapture[T]{plan: &p, phase: CallbackPhaseConstruction})
	}
	err = p.init()
	if captured {
		c := p.capture.Swap(nil)
		p.construction = c.callbacks
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// PlannerRevision returns the plan's current structural version.
//
// It increases once per structural change, so a consumer holding a fact
// recorded at one revision can tell that a later read describes a different
// structure rather than the same one observed twice.
func (p *Plan[T]) PlannerRevision() uint64 { return p.revision.Load() }

// mint allocates an occurrence identity for a newly created planner object.
func (p *Plan[T]) mint() OccurrenceID { return p.ids.mint() }

// bump advances the plan's structural revision.
func (p *Plan[T]) bump() { p.revision.Add(1) }

// mint allocates an occurrence identity from the plan owning this goal.
func (g *goal[T]) mint() OccurrenceID {
	if g == nil || g.plan == nil {
		return ""
	}
	return g.plan.mint()
}

// bump advances the structural revision of the plan owning this goal.
func (g *goal[T]) bump() {
	if g == nil || g.plan == nil {
		return
	}
	g.plan.bump()
}

// capture returns the tick capture published for the current tick, or nil when
// no capture is running.
func (g *goal[T]) capture() *tickCapture[T] {
	if g == nil || g.plan == nil {
		return nil
	}
	return g.plan.capture.Load()
}

// TickCaptured runs one tick and returns everything it observed.
//
// Every condition value and action list the planner reads comes from a single
// pre-tick view, so the returned snapshot describes one world. A view is opened
// for each action that runs and is closed exactly once, including when the
// planner panics. A concurrent or reentrant call returns immediately with a
// capture error rather than blocking or running interleaved.
func (p *Plan[T]) TickCaptured(env TickEnvironment) (out TickExecution) {
	out = TickExecution{PlanRevision: p.PlannerRevision(), StartedAt: time.Now()}
	if !p.gate.TryLock() {
		out.CaptureError = &RecordedError{
			Type:    "pabt.ErrPlannerBusy",
			Message: "a tick is already in progress",
		}
		out.CompletedAt = time.Now()
		return
	}
	defer p.gate.Unlock()

	view, err := p.config.views.OpenStateView(StateViewRequest{
		Role:         StateViewPreTick,
		PlanRevision: p.PlannerRevision(),
	})
	if err != nil {
		out.CaptureError = recordedError(err)
		out.CompletedAt = time.Now()
		return
	}
	c := &tickCapture[T]{
		plan:          p,
		view:          view,
		phase:         CallbackPhaseTick,
		stateRevision: view.Revision(),
		coherence:     view.Coherence(),
	}
	p.capture.Store(c)
	defer func() {
		p.capture.Store(nil)
		closeErr := view.Close()
		c.closeError = recordedError(closeErr)
		out.CaptureError = mergeError(out.CaptureError, c.closeError)
		out.Snapshot = p.snapshot(c)
		out.CompletedAt = time.Now()
	}()

	status, err := p.tickOnce()
	out.Status = status
	out.StatusKnown = ValidateStatus(status) == nil
	if err != nil {
		out.PlannerError = recordedError(err)
	}
	return
}

// tickOnce executes the root tick, unpublishing the capture on the way out so
// a panic cannot leave the plan reporting a finished tick as an active one.
func (p *Plan[T]) tickOnce() (status bt.Status, err error) {
	defer p.capture.Store(nil)
	tick, children := p.bt()
	return tick(children)
}

// tickCapture accumulates everything observed during one tick.
//
// The tick gate means at most one is published on a plan at a time, so its
// slices are written only by the ticking goroutine.
type tickCapture[T Condition] struct {
	plan          *Plan[T]
	view          TypedStateView[T]
	phase         CallbackPhase
	stateRevision StateRevision
	coherence     StateCoherence
	closeError    *RecordedError
	ordinal       uint32
	callbacks     []CallbackEvidence
	executions    []ActionEvidence
}

func (c *tickCapture[T]) record(ev CallbackEvidence) {
	if c == nil {
		return
	}
	c.ordinal++
	ev.Ordinal = c.ordinal
	ev.Phase = c.phase
	c.callbacks = append(c.callbacks, ev)
}

func (c *tickCapture[T]) recordKey(subject SubjectRole, subjectID, nodeID OccurrenceID, key any) {
	if c == nil {
		return
	}
	c.record(CallbackEvidence{
		Kind:      CallbackKey,
		Subject:   subject,
		SubjectID: subjectID,
		NodeID:    nodeID,
		Key:       key,
	})
}

func (c *tickCapture[T]) recordVariable(
	nodeID, conditionID OccurrenceID,
	key, value any,
	err error,
	d time.Duration,
) {
	if c == nil {
		return
	}
	c.record(CallbackEvidence{
		Kind:      CallbackVariableRead,
		Subject:   SubjectCondition,
		SubjectID: conditionID,
		NodeID:    nodeID,
		Key:       key,
		Result:    value,
		Err:       recordedError(err),
		Duration:  d,
	})
}

func (c *tickCapture[T]) recordMatch(
	nodeID, conditionID OccurrenceID,
	use MatchUse,
	key, input any,
	matched bool,
	d time.Duration,
) {
	if c == nil {
		return
	}
	c.record(CallbackEvidence{
		Kind:      CallbackMatch,
		Use:       use,
		Subject:   SubjectCondition,
		SubjectID: conditionID,
		NodeID:    nodeID,
		Key:       key,
		Input:     input,
		Result:    matched,
		Duration:  d,
	})
}

func (c *tickCapture[T]) recordEffectValue(
	nodeID, effectID OccurrenceID,
	key, value any,
	use MatchUse,
	d time.Duration,
) {
	if c == nil {
		return
	}
	c.record(CallbackEvidence{
		Kind:      CallbackEffectValue,
		Use:       use,
		Subject:   SubjectEffect,
		SubjectID: effectID,
		NodeID:    nodeID,
		Key:       key,
		Result:    value,
		Duration:  d,
	})
}

func (c *tickCapture[T]) recordStateActions(conditionID OccurrenceID, count int, d time.Duration) {
	if c == nil {
		return
	}
	c.record(CallbackEvidence{
		Kind:      CallbackStateActions,
		Subject:   SubjectCondition,
		SubjectID: conditionID,
		Result:    count,
		Duration:  d,
	})
}

func (c *tickCapture[T]) recordActionCalls(
	kind CallbackKind,
	nodeID, actionID OccurrenceID,
	result any,
	d time.Duration,
) {
	if c == nil {
		return
	}
	c.record(CallbackEvidence{
		Kind:      kind,
		Subject:   SubjectAction,
		SubjectID: actionID,
		NodeID:    nodeID,
		Result:    result,
		Duration:  d,
	})
}

// variable reads one variable through the pre-tick view and records the pair of
// calls that produced a condition's current value.
func (c *tickCapture[T]) variable(key any, nodeID, conditionID OccurrenceID) (any, error) {
	started := time.Now()
	value, err := c.view.Variable(key)
	c.recordVariable(nodeID, conditionID, key, value, err, time.Since(started))
	return value, err
}

// match applies the condition and records which world the value came from.
func (c *tickCapture[T]) match(
	conditionID, nodeID OccurrenceID,
	use MatchUse,
	key, input any,
	fn func(any) bool,
) bool {
	started := time.Now()
	matched := fn(input)
	c.recordMatch(nodeID, conditionID, use, key, input, matched, time.Since(started))
	return matched
}

// actions lists candidate actions through the pre-tick view so action
// discovery is read from the same world as condition evaluation.
func (c *tickCapture[T]) actions(failed T, conditionID OccurrenceID) ([]Action[T], error) {
	started := time.Now()
	acts, err := c.view.Actions(failed)
	c.recordStateActions(conditionID, len(acts), time.Since(started))
	return acts, err
}

// afterAction completes one action boundary: it opens the post-action view,
// runs the observer, then the verifier, and records what each concluded.
//
// The before-revision is the one read at the action's entry, so a transition
// claim is bracketed by two real reads rather than by two reads of the same
// post-action world.
func (c *tickCapture[T]) afterAction(
	g *goal[T],
	act *action[T],
	nodeID OccurrenceID,
	exec ActionExecution,
	before StateRevision,
	beforeCoherence StateCoherence,
	started time.Time,
) {
	if c == nil {
		return
	}
	exec.Duration = time.Since(started)
	ev := ActionEvidence{Execution: exec}
	defer func() { c.executions = append(c.executions, ev) }()

	cfg := c.plan.config
	if cfg.views == nil {
		return
	}
	view, err := cfg.views.OpenStateView(StateViewRequest{
		Role:         StateViewPostAction,
		PlanRevision: c.plan.PlannerRevision(),
		ActionID:     act.id,
	})
	if err != nil {
		ev.Err = recordedError(err)
		return
	}
	defer func() {
		// A close failure invalidates the evidence gathered from this view, so
		// it is recorded rather than discarded.
		if closeErr := view.Close(); closeErr != nil {
			ev.Err = mergeError(ev.Err, recordedError(closeErr))
		}
	}()
	borrowed := borrowed[T]{inner: view}
	effects := act.capturedEffects()
	if cfg.observer != nil {
		observation, oerr := cfg.observer.ObserveExecution(ActionObservationRequest{
			PlanRevision:    c.plan.PlannerRevision(),
			ActionID:        act.id,
			NodeID:          nodeID,
			Execution:       exec,
			Effects:         effects,
			PostconditionID: act.postconditionID,
			View:            borrowed,
		})
		ev.Observation = observation
		ev.Err = mergeError(ev.Err, recordedError(oerr))
	}
	if cfg.verifier == nil {
		return
	}
	after, afterCoherence := view.Revision(), view.Coherence()
	verdicts, verr := cfg.verifier.VerifyTransitions(TransitionRequest{
		PlanRevision:    c.plan.PlannerRevision(),
		ActionID:        act.id,
		Execution:       exec,
		Effects:         effects,
		PostconditionID: act.postconditionID,
		Observation:     ev.Observation,
		View:            borrowed,
		BeforeRevision:  before,
		BeforeCoherence: beforeCoherence,
		AfterRevision:   after,
		AfterCoherence:  afterCoherence,
	})
	// The planner, not the verifier, decides whether the bracketing reads can
	// support an authoritative claim.
	for i := range verdicts {
		verdicts[i].Authoritative = beforeCoherence.coherent() && afterCoherence.coherent() &&
			verdicts[i].Err == nil
	}
	ev.Relations = verdicts
	ev.Err = mergeError(ev.Err, recordedError(verr))
}

func recordedError(err error) *RecordedError {
	if err == nil {
		return nil
	}
	return &RecordedError{Type: fmt.Sprintf("%T", err), Message: err.Error()}
}

func mergeError(a, b *RecordedError) *RecordedError {
	switch {
	case b == nil:
		return a
	case a == nil:
		return b
	}
	return &RecordedError{Type: a.Type, Message: a.Message + "; " + b.Message}
}
