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
	"github.com/joeycumines/go-pabt/examples/reefer-handoff/sim"
)

// settlePendingLocked moves a landed result onto the action instance that
// submitted it, routed by the submit key.
//
// This is deliberately not done by the node that happens to be ticked. A command
// is settled by the loop owner, and the node that submitted it may not be the one
// the selector runs next, so a result left on the in-flight record alone would be
// stranded: the next node would see an outstanding command that was not its own and
// wait for a result nobody would ever read.
//
// The parked outcome is read exactly once, by the next tick of the node that
// submitted it. A node that is not ticked next leaves its outcome parked; the
// memorized selector re-runs the parked branch first, so the result is still
// there when its owner runs.
//
// An Abandoned marker carries no receipt, so it is routed on the submit key and
// the generation instead of on a receipt key.
func (r *Runner) settlePendingLocked() {
	if r.inflight == nil || r.inflight.result == nil {
		return
	}
	key := r.inflight.key
	res := *r.inflight.result
	gen := r.inflight.gen
	owner := r.owners[key]
	r.inflight = nil
	if res.Abandoned == nil {
		delete(r.owners, res.Receipt.IdempotencyKey)
	}
	// A result for a generation this runner has since retired can neither mutate
	// state nor report success, so it is dropped rather than handed on. The key
	// check matters too: a late result from a retired generation can land in the
	// slot after the next generation has submitted, and handing it to the new
	// submitter would answer a command with another command's result.
	if owner == nil || gen != r.gen {
		return
	}
	if res.Abandoned == nil && key != res.Receipt.IdempotencyKey {
		return
	}
	owner.outcome = &res
}

// takeOutcome reads and clears this action's settled result.
func (r *Runner) takeOutcome(p *pending) *sim.Result {
	if p == nil || p.outcome == nil {
		return nil
	}
	res := p.outcome
	p.outcome = nil
	return res
}

// settleTravelLocked moves a landed travel result onto the shared travel
// pending, where the travel node reads it. Only the travel node calls it, and
// it runs before any other node on the tick, so the result it moves is the
// travel command's own: a non-travel receipt can only be outstanding when no
// travel node runs, and then this never executes. The key check is still kept,
// because a result can land between the attach and the next tick, and a late
// one must not be read as the current hop's answer.
func (r *Runner) settleTravelLocked() {
	if r.inflight == nil || r.inflight.result == nil {
		return
	}
	key := r.inflight.key
	res := *r.inflight.result
	gen := r.inflight.gen
	r.inflight = nil
	if res.Abandoned == nil {
		delete(r.owners, res.Receipt.IdempotencyKey)
	}
	// A result for a generation this runner has since retired can neither mutate
	// state nor report success, so it is dropped rather than handed on.
	if gen != r.gen || r.travel == nil {
		return
	}
	if res.Abandoned == nil && key != res.Receipt.IdempotencyKey {
		return
	}
	r.travel.outcome = &res
}

// takeTravelOutcome reads and clears the shared travel pending's settled result.
func (r *Runner) takeTravelOutcome() *sim.Result {
	if r.travel == nil || r.travel.outcome == nil {
		return nil
	}
	res := r.travel.outcome
	r.travel.outcome = nil
	return res
}
