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

// Tick performs one root tick: pin one frame, tick the plan once against it,
// and interpret the result.
//
// It never blocks. A command whose receipt has not arrived leaves the tick
// reporting Running, which is what a plan legitimately does while it waits.
func (r *Runner) Tick() (Outcome, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.vacating {
		return r.vacateLocked(), nil
	}

	if r.plan == nil {
		return r.claimLocked()
	}

	// One frame for the whole tick. Every condition read and every action list
	// the plan consults in this tick comes from this observation and no other, so
	// a tick cannot see half of one world and half of another.
	frame, ok := sim.Observe(r.loop.Snapshot(), r.tug, r.reefer)
	if !ok {
		return OutcomeIdle, fmt.Errorf("logic: %s cannot frame the world for %s", r.tug, r.reefer)
	}
	r.state.pin(frame, r.plan.PlannerRevision())

	r.tick++
	status, err := r.plan.Node().Tick()
	if err != nil {
		r.state.unpin()
		return OutcomeRunning, fmt.Errorf("logic: %s tick %d: %w", r.tug, r.tick, err)
	}

	switch status {
	case btSuccess:
		// The goal holds. Retire this generation: the job is done, and the world
		// has already released the tractor. The tractor stands in the bay
		// holding nothing, and the bay holds one, so the runner walks home
		// before it claims again (see vacate.go). The delivery is still
		// reported now: the walk is a consequence of it, not part of the job.
		r.lastDelivered = r.reefer
		r.retireLocked()
		r.vacating = true
		r.vacateSeq = 0
		return OutcomeDelivered, nil
	case btFailure:
		// The root failed and pabt could not expand anything further, so the goal
		// is unreachable from the pinned frame. Name it while the frame is still
		// pinned: the report reads the frame the plan failed against. Retire
		// and re-claim: a fresh frame sees the world as it stands, and the new
		// generation is the replan. The job binding is released with the
		// generation — the world still records which tractor holds which job,
		// so the next claim rejoins the run wherever it stands rather than
		// replaying a job it cannot achieve.
		r.blocked = r.unreachableLocked()
		r.retireLocked()
		return OutcomeBlocked, nil
	default:
		r.state.unpin()
		return OutcomeRunning, nil
	}
}

// claimLocked takes a job from the world and builds a generation for it.
//
// The claim is a command like any other and goes through the same one-in-flight
// discipline, so it takes two ticks: one to submit and one to build. Probing for a
// settled result instead would submit a fresh claim every tick, and a tractor
// holding a job answers each of those with a refusal it never asked for.
func (r *Runner) claimLocked() (Outcome, error) {
	if r.inflight == nil {
		r.tick++
		// No frame is pinned here: the claim carries no slot provenance and its
		// world sequence is the reducer's to stamp, so a pinned frame would be
		// observed by nothing. The versions a claim is decided against are the
		// world's own at reduce time.
		r.claim = &pending{}
		r.submitPendingLocked(r.claim, func(expect sim.Expectation, key string) sim.Command {
			return sim.Command{
				ID: key, IdempotencyKey: key, Kind: sim.CmdClaimJob, Expect: expect,
			}
		})
		return OutcomeRunning, nil
	}

	if r.inflight == nil || r.inflight.result == nil {
		return OutcomeRunning, nil
	}
	settled := *r.inflight.result
	r.inflight = nil

	if settled.Abandoned != nil {
		return OutcomeWithdrawn, nil
	}
	if settled.Receipt.Outcome == sim.OutcomeDuplicate {
		// The key was already reduced, so this receipt answers the already-known
		// and the queue was not consulted. Retire the spent key so the next
		// submit mints a fresh one; claiming again under the same key would
		// answer Duplicate forever.
		r.claim = nil
		return OutcomeRunning, nil
	}
	if !settled.Receipt.Applied() {
		// The queue is contended and this tractor has to wait its turn. That is a
		// normal outcome, not an error, and not progress.
		return OutcomeRunning, nil
	}

	reefer := sim.ReeferID("")
	for _, ev := range settled.Events {
		if ev.Kind == sim.EvJobClaimed {
			reefer = ev.Reefer
		}
	}
	if reefer == "" {
		return OutcomeRunning, fmt.Errorf("logic: %s's claim published no job", r.tug)
	}
	if err := r.buildLocked(reefer); err != nil {
		return OutcomeRunning, err
	}
	return OutcomeClaimed, nil
}

// reeferOrAny returns the job's reefer, or a reefer that exists when no job is
// held, so a frame can be taken before one is claimed. The frame is only used for
// the command's expectation in that case.
func (r *Runner) reeferOrAny() sim.ReeferID {
	if r.reefer != "" {
		return r.reefer
	}
	return sim.Reefer1
}

// buildLocked retires any generation and builds a fresh one for a reefer.
//
// It takes a fresh frame rather than reusing the one the claim was decided
// against: the world has moved since, and a plan built on a stale frame would
// enumerate candidates from a state that no longer holds.
func (r *Runner) buildLocked(reefer sim.ReeferID) error {
	frame, ok := sim.Observe(r.loop.Snapshot(), r.tug, reefer)
	if !ok {
		return fmt.Errorf("logic: %s cannot frame the world for %s", r.tug, reefer)
	}
	r.retireLocked()
	r.gen++
	r.reefer = reefer
	r.lastDelivered = ""
	r.state.pin(frame, 0)

	// Exactly one goal condition. A zero-length slice would build a bare sequence
	// that reports success having achieved nothing, and two slices would make the
	// root a selector over OR alternatives — the wrong shape for a plan that owns
	// one job.
	//
	// Construction reads no state — the goal keys are attributed to the
	// construction phase, not evaluated — so the frame is unpinned once the plan
	// exists. Between ticks a Variable read fails loudly instead of answering
	// from a stale frame.
	plan, err := pabt.INew(r.state, []pabt.IConditions{{delivered(reefer)}})
	if err != nil {
		r.state.unpin()
		return fmt.Errorf("logic: %s building a generation for %s: %w", r.tug, reefer, err)
	}
	r.state.unpin()
	r.plan = plan
	r.blocked = ""
	return nil
}

// retireLocked drops the current generation. A retired generation is never ticked
// again and never reused. It also drops any walk-home state and any parked
// receipt: a new claim starts from wherever the tractor stands, a stale vacate
// flag would walk it away from a job it just took, and a parked outcome belongs
// to the action instance that submitted it — handing it to the next generation
// would answer a command with another command's receipt.
func (r *Runner) retireLocked() {
	r.plan = nil
	r.reefer = ""
	r.state.unpin()
	r.inflight = nil
	r.vacating = false
	r.vacateSeq = 0
	for _, p := range r.owners {
		if p != nil {
			p.outcome = nil
		}
	}
	r.owners = make(map[string]*pending)
	if r.travel != nil {
		r.travel.outcome = nil
	}
	if r.claim != nil {
		r.claim.outcome = nil
	}
}

// unreachableLocked names the condition the last blocked tick could not achieve,
// so a report says what failed rather than only that something did.
func (r *Runner) unreachableLocked() string {
	if r.reefer == "" {
		return "no job held"
	}
	if o, ok := r.state.frame(); ok && len(o.AcceptingSlots()) == 0 {
		return fmt.Sprintf("no slot will take %s", r.reefer)
	}
	return delivered(r.reefer).String()
}
