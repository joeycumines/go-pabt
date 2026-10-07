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
	"strconv"

	"github.com/joeycumines/go-pabt"
	"github.com/joeycumines/go-pabt/examples/reefer-handoff/sim"
)

// submitPendingLocked admits one action's command, at most one outstanding at a
// time. The caller holds the lock.
func (r *Runner) submitPendingLocked(p *pending, build func(sim.Expectation, string) sim.Command) {
	if r.inflight != nil {
		return
	}
	p.attempt++
	p.key = r.keyLocked("a" + strconv.FormatUint(p.attempt, 10))
	cmd := build(sim.Expectation{}, p.key)
	cmd.Tractor = r.tug
	cmd.PlanID = r.planIDLocked()
	// A claim is not submitted by a plan tick. The first claim is born before
	// any generation exists; a later one is born after a delivery retired its
	// generation, when copying r.gen would name a plan tick that never
	// submitted it. The zero stamp means "no plan tick" — the envelope
	// contract's own words. The in-flight record still fences on the live
	// generation.
	planTick := p != r.claim
	if planTick {
		cmd.Generation = r.gen
		cmd.PlanTickSeq = r.tick
		cmd.ActionID = p.attempt
	}

	ch, err := r.loop.Submit(cmd)
	if err != nil {
		// The loop is not admitting, so nothing was submitted and there is nothing
		// to wait for. The attempt is spent, which is correct: the next attempt is
		// a new command and will mint a new key.
		return
	}
	r.owners[p.key] = p
	r.inflight = &inflight{key: p.key, gen: r.gen, planTickSeq: r.tick, actionSeq: p.attempt, expect: cmd.Expect}
	if r.publisher != nil {
		actionID := uint64(0)
		if planTick {
			actionID = p.attempt
		}
		r.publisher.NoteSubmitted(cmd, actionID)
	}
	go r.await(p.key, ch)
}

// awaitingLocked decides what an action node reports about a settled command, and
// the three answers are not interchangeable.
//
// A receipt is evidence that a command happened, not that the post-condition now
// holds, so Applied still has to be verified against a fresh frame; the tick's
// frame predates the receipt.
//
// A refusal is different, and returning Running for it would be a deadlock. The
// planner only looks for a failed precondition to expand when the whole tree
// reports Failure, so an action that waits forever after being refused would stop
// the tree from ever growing. A refusal is precisely a failed precondition, and
// the planner's answer to one is to expand it — which is how a plan aimed at a
// slot that lost power gets a new set of candidates.
func (r *Runner) awaitingLocked(p *pending) (btStatus, error) {
	res := r.takeOutcome(p)
	if res == nil {
		return btStatusRunning, nil
	}
	if res.Abandoned != nil {
		// Nothing was decided, so there is nothing to verify and nothing to
		// attribute. The next attempt is a new command.
		return btStatusRunning, nil
	}
	if !res.Receipt.Applied() {
		return btStatusFailure, nil
	}
	if p.post.Key() == nil || !r.verifiesLocked(p.post) {
		return btStatusRunning, nil
	}
	return btStatusSuccess, nil
}

// guardsHoldLocked reports whether an action's own preconditions hold in a frame taken
// now. A node checks this before submitting, so an action never fires past a guard
// it has not passed: it reports Failure instead, and the planner expands whichever
// precondition is actually false.
func (r *Runner) guardsHoldLocked(guards []pabt.IConditions) bool {
	if len(guards) == 0 {
		return true
	}
	frame, ok := r.frameLocked()
	if !ok {
		// Without a frame the guards cannot be shown to hold, and submitting on the
		// strength of an unshown guard is how a plan acts on a state it never read.
		return false
	}
	for _, group := range guards {
		for _, c := range group {
			key, ok := c.Key().(sim.VarKey)
			if !ok {
				return false
			}
			v, ok := frame.Read(key)
			if !ok || !c.Match(v) {
				return false
			}
		}
	}
	return true
}

// frameLocked takes a frame for the runner's current job, or reports that it
// cannot.
func (r *Runner) frameLocked() (sim.Observation, bool) {
	if r.reefer == "" {
		return sim.Observation{}, false
	}
	return sim.Observe(r.loop.Snapshot(), r.tug, r.reefer)
}

// submitKeyedLocked admits a command under a key the caller has already minted.
// The travel node mints its own keys because one action instance makes many
// attempts over one route, each of which is a distinct command.
func (r *Runner) submitKeyedLocked(key string, build func(sim.Expectation, string) sim.Command) {
	if r.inflight != nil {
		return
	}
	cmd := build(sim.Expectation{}, key)
	cmd.Tractor = r.tug
	cmd.PlanID = r.planIDLocked()
	cmd.Generation = r.gen
	cmd.PlanTickSeq = r.tick

	ch, err := r.loop.Submit(cmd)
	if err != nil {
		// The loop is not admitting, so nothing was submitted and there is nothing
		// to wait for. The attempt is spent, which is right: the next attempt is a
		// distinct command and mints its own key.
		return
	}
	r.owners[key] = r.travel
	r.inflight = &inflight{key: key, gen: r.gen, planTickSeq: r.tick, expect: cmd.Expect}
	if r.publisher != nil {
		r.publisher.NoteSubmitted(cmd, r.travel.attempt)
	}
	go r.await(key, ch)
}

// submitTravelLocked admits one crossing toward a node, minting a key for the
// attempt. The travel action makes many commands over one route and each is a
// distinct action instance as far as the world is concerned.
func (r *Runner) submitTravelLocked(to sim.NodeID) {
	if r.travel == nil {
		r.travel = &pending{}
	}
	r.travel.attempt++
	r.travel.key = r.keyLocked("hop" + strconv.FormatUint(r.travel.attempt, 10))
	r.submitKeyedLocked(r.travel.key, func(expect sim.Expectation, key string) sim.Command {
		return sim.Command{
			ID: key, IdempotencyKey: key, Kind: sim.CmdMoveTo, Tractor: r.tug,
			Node: to, Expect: expect,
		}
	})
}

// resetTravelLocked puts the movement action back to its first attempt, so a route
// abandoned part-way does not leave an attempt counter that keeps climbing.
func (r *Runner) resetTravelLocked() {
	if r.travel != nil {
		r.travel.attempt = 0
	}
}

// verifiesLocked reports whether a frame taken now shows a condition holding. It
// frames the world fresh, because the tick's frame predates the receipt.
func (r *Runner) verifiesLocked(c condition) bool {
	key, ok := c.Key().(sim.VarKey)
	if !ok {
		return false
	}
	reefer := r.reefer
	if reefer == "" {
		return false
	}
	frame, ok := sim.Observe(r.loop.Snapshot(), r.tug, reefer)
	if !ok {
		return false
	}
	v, ok := frame.Read(key)
	if !ok {
		return false
	}
	return c.Match(v)
}

// keyLocked mints an idempotency key for one action instance. The key names the
// plan, the generation, and the action, so a key is spent by its first reduction
// and a genuine retry after a replan is a new action instance with a new key.
func (r *Runner) keyLocked(suffix string) string {
	return r.planIDLocked() + "/g" + strconv.FormatUint(r.gen, 10) +
		"/t" + strconv.FormatUint(r.tick, 10) + "/" + suffix
}

func (r *Runner) planIDLocked() string { return string(r.tug) }

// await waits for one command's result and attaches it to the in-flight record. It
// runs on its own goroutine so the tick never blocks, and it takes the lock
// because it is not the goroutine holding it.
//
// The key is captured at submit: only a result answering the outstanding command
// may fill the slot. An Abandoned marker carries no receipt, so it is fenced on
// the submit key and the generation instead of on a receipt key. A late result
// from a retired generation or a retired hop must not clobber the next
// generation's command, so a mismatch never fills the slot — but it is still
// recorded (noteLateLocked), because the world reduced that command and a
// causal log that loses reductions is exactly what the lane exists to prevent.
// Either way the settle path re-checks the key, because a result can also land
// between the attach and the next tick.
func (r *Runner) await(key string, ch <-chan sim.Result) {
	res, ok := <-ch
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inflight == nil {
		// The command's generation was retired while the world was still
		// deciding it — the delivery tick's frame showed the goal met before
		// this receipt attached. No node may act on it, but the reduction is
		// a fact, so it is noted and rides in the next committed record,
		// linked to its submission by idempotency key.
		r.noteLateLocked(key, res)
		return
	}
	if r.inflight.gen != r.gen || r.inflight.key != key {
		// A result for a generation this runner has since retired can neither
		// mutate state nor report success. The slot is cleared only when it
		// still holds the awaited command; otherwise a newer generation has
		// already submitted and must not lose its place to a stale arrival.
		if r.inflight.key == key {
			r.inflight = nil
		}
		r.noteLateLocked(key, res)
		return
	}
	if res.Abandoned == nil && key != res.Receipt.IdempotencyKey {
		r.inflight = nil
		r.noteLateLocked(key, res)
		return
	}
	r.inflight.result = &res
}

// noteLateLocked records a result that arrived for a command no node can any
// longer act on: a retired generation, a cleared slot, a mismatched key. The
// world still reduced it, so it is noted to the publisher rather than dropped;
// it drains into the next committed record's envelope, linked to its submission
// by the idempotency key, and never fills an in-flight slot.
func (r *Runner) noteLateLocked(key string, res sim.Result) {
	if r.publisher != nil {
		r.publisher.NoteSettled(key, res)
	}
}
