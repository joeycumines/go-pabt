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

// Package logic holds the plan side of Reefer Handoff: the runner that drives
// one tractor's plan, the pabt.State it runs against, and the action templates
// it grows. Nothing here mutates the world; a runner frames the world once per
// root tick, ticks its plan, and submits typed commands the world answers with
// receipts.
//
// The files divide by lifecycle: runner.go owns the Runner type, its small
// accessors, and the receipt-attachment helper the tests wait on; tick.go owns
// the root tick with the claim, build, and retire paths; vacate.go owns the
// walk-home path a delivered tractor takes before it claims again; settle.go
// owns receipt settlement and fencing; route.go owns live-world routing;
// submit.go owns command admission, guard checks, and post-condition
// verification.
package logic

import (
	"sync"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt"
	"github.com/joeycumines/go-pabt/examples/reefer-handoff/sim"
)

// The two plan statuses a runner has to tell apart. bt.Running is the default
// case, because it means both "expanded and progressing" and "waiting".
const (
	btSuccess = bt.Success
	btFailure = bt.Failure
)

// Outcome is what one root tick did. These are the runner's states and not
// behavior-tree statuses: a pabt tick returns bt.Success, bt.Failure, or
// bt.Running, and the runner interprets what a Failure meant.
type Outcome string

const (
	// OutcomeIdle means the runner holds no generation yet.
	OutcomeIdle Outcome = "Idle"

	// OutcomeRunning means the tick expanded or advanced the tree and the goal is
	// still open.
	OutcomeRunning Outcome = "Running"

	// OutcomeClaimed means this tick claimed a job and built a generation for it.
	OutcomeClaimed Outcome = "Claimed"

	// OutcomeDelivered means the job's reefer reached a powered, correctly rated
	// slot and the generation has been retired.
	OutcomeDelivered Outcome = "Delivered"

	// OutcomeBlocked means the tree is fully expanded and the goal is unreachable
	// from the pinned frame. The generation is retired and the binding released;
	// the next claim rejoins the run wherever it stands. Slot loss inside a job
	// recovers earlier, by in-tree re-expansion onto a powered slot, which is
	// why a plan aimed at a slot that loses power re-targets without retiring.
	OutcomeBlocked Outcome = "Blocked"

	// OutcomeWithdrawn means the in-flight command was abandoned, so it never
	// happened. The runner does not treat that as progress and does not ascribe a
	// reason to it.
	OutcomeWithdrawn Outcome = "Withdrawn"
)

// Runner drives one tractor's plan.
//
// It owns exactly one plan generation at a time, and that generation owns exactly
// one job. A job is claimed from the world rather than assigned, so which reefer
// a tractor ends up with is the reducer's decision. When the job is delivered, or
// when the tree is fully expanded and cannot progress, the generation is retired
// and a new one is built — never a second job folded into the same tree, because
// goal slices are OR alternatives and satisfying either would report success with
// the other reefer still on the apron.
type Runner struct {
	tug  sim.TractorID
	loop *sim.WorldLoop

	// mu serializes a runner's own bookkeeping. The world has its own ordering; a
	// runner is single-threaded by contract, and this is what makes that contract
	// enforced rather than assumed.
	mu sync.Mutex

	state  *state
	plan   *pabt.IPlan
	gen    uint64
	reefer sim.ReeferID
	tick   uint64

	inflight *inflight

	// owners maps a key to the action instance that submitted under it, so a
	// settled receipt is handed to the node that asked for it. The in-flight
	// record names the key too, and the entry is removed when it settles.
	owners map[string]*pending

	// travel is the pending for the movement action, shared because a runner walks
	// one route at a time. It has no post-condition: the travel node checks its own
	// arrival against a frame rather than declaring a condition satisfied.
	travel *pending

	// claim is the pending for the job claim, which has no post-condition: there
	// is nothing to verify, only a receipt to read and an event to learn the job
	// from.
	claim *pending

	// lastDelivered records the reefer of the most recently completed
	// generation. Retiring clears the live job binding, so without this the
	// fact of what was delivered would retire with it.
	lastDelivered sim.ReeferID

	// vacating reports whether the runner is walking home after a delivery.
	// A delivered tractor stands in the bay holding nothing, and the bay holds
	// one, so it steps out to a waypoint and back to the apron before it
	// claims again. The walk is ordinary MoveTo commands under the same
	// one-in-flight rule; vacateSeq counts the hops taken for the current walk.
	vacating  bool
	vacateSeq int

	// blocked is the condition the last blocked tick could not achieve, so a
	// report can name it instead of only saying the goal was unreachable.
	blocked string

	// publisher, when non-nil, commits each root tick's plan record through a
	// pabtdebug session and appends its causal bundle to the committed
	// world/command log. The tick still runs exactly once: the session drives
	// the plan's captured tick instead of the runner ticking the bare node.
	// A nil publisher ticks the bare node and publishes nothing, which is how
	// every existing test drives the runner.
	publisher *Publisher

	// beliefs is the runner's persistent epistemic state. It survives across
	// ticks and generations, accumulating knowledge as commands settle. Each
	// tick overlays these beliefs onto the fresh Observation so the plan reads
	// what the agent knows rather than what the world is. Only the owning
	// runner mutates this map, under mu.
	beliefs map[sim.VarKey]sim.Belief
}

// pending is one action instance's place in the runner's single in-flight slot.
// For a single-step action it holds the post-condition that action achieves; the
// travel action holds no post-condition, because it verifies its own arrival
// against a fresh frame on every tick.
type pending struct {
	post    condition
	key     string
	attempt uint64

	// outcome is the settled result of this action's last command, waiting for the
	// node to read it. It is parked here rather than left on the runner's
	// in-flight record because the node that submitted a command is not always the
	// node ticked next: a selector may run a sibling branch, and a receipt parked
	// only on the in-flight record would be stranded with nothing able to read it.
	outcome *sim.Result
}

// inflight is the one command a runner may have outstanding.
//
// It records the generation it was submitted under, so a receipt can be fenced
// without the world's receipt having to carry a plan identity. The world knows
// nothing about plans; the runner is what knows which generation asked.
type inflight struct {
	key         string
	gen         uint64
	planTickSeq uint64
	actionSeq   uint64
	result      *sim.Result
	expect      sim.Expectation // the frame versions this command was decided against
}

// New builds a runner for one tractor. It holds no plan until its first tick.
func New(tug sim.TractorID, loop *sim.WorldLoop) *Runner {
	r := &Runner{
		tug:     tug,
		loop:    loop,
		owners:  make(map[string]*pending),
		beliefs: make(map[sim.VarKey]sim.Belief),
	}
	r.state = &state{runner: r}
	return r
}

// Tractor returns which tractor this runner drives.
func (r *Runner) Tractor() sim.TractorID { return r.tug }

// AttachPublisher attaches the owner-side publisher for this tractor. The next
// generation built claims its plan as captured and publishes each root tick.
// It is set once, before the first tick, and never detached: detaching would
// leave half a history.
func (r *Runner) AttachPublisher(p *Publisher) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.publisher = p
}

// Generation returns the current plan generation, zero when none is held.
func (r *Runner) Generation() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.gen
}

// Reefer returns the job's reefer, empty when no generation is held. After a
// delivery it names the reefer just delivered, so the fact survives the
// retirement that reports it.
func (r *Runner) Reefer() sim.ReeferID {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.reefer != "" {
		return r.reefer
	}
	return r.lastDelivered
}

// PlanTickSeq returns how many root ticks this runner has performed, which is the
// plan lane's own sequence and is never the world's.
func (r *Runner) PlanTickSeq() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tick
}

// Blocked returns the condition the last blocked tick could not achieve.
func (r *Runner) Blocked() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.blocked
}

// Settled reports whether the runner has no outstanding command: either none
// was submitted or its receipt has been attached. The test harness waits on it
// between ticks, because a receipt reduced by the owner but not yet attached by
// the await goroutine reads the same as no receipt. It takes the lock, because
// the await goroutine writes the record it reads.
func (r *Runner) Settled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.inflight == nil || r.inflight.result != nil
}

// Beliefs returns a snapshot of the runner's persistent belief store. The
// returned map is a defensive copy safe for concurrent read by the TUI or
// debugger without holding the runner's lock.
func (r *Runner) Beliefs() map[sim.VarKey]sim.Belief {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[sim.VarKey]sim.Belief, len(r.beliefs))
	for k, v := range r.beliefs {
		out[k] = v
	}
	return out
}
