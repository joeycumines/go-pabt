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

// drainSettledLocked moves an attached receipt onto the action instance that
// submitted it, routed by the owners map, and records it with the publisher. It
// runs at the top of every plan-held tick, before the plan ticks and before any
// retire, so a receipt that settled between ticks is attributed to the command
// that caused it and a retirement cannot outrun the node that submitted it.
//
// Routing by owner is what keeps a single-step action's receipt from being
// parked on the shared travel pending when the At-subtree happens to tick first:
// the owners map names the pending that submitted under the key, and only that
// pending reads the outcome. A travel receipt finds r.travel the same way,
// because the travel submitter registered r.travel under its hop key. Under the
// real tree shape the misroute was unreachable (a travel hop is the only command
// outstanding while the travel node ticks), but it was load-bearing on that
// shape; routing by owner is correct whatever the shape becomes.
//
// The parked outcome is read exactly once, by the next tick of the node that
// submitted it. A node that is not ticked next leaves its outcome parked; the
// memorized selector re-runs the parked branch first, so the result is still
// there when its owner runs.
//
// An Abandoned marker carries no receipt, so it is routed on the submit key and
// the generation instead of on a receipt key. The caller holds the lock.
func (r *Runner) drainSettledLocked() {
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
	// The world reduced this command, or withdrew it, and that is a fact
	// regardless of which node may still act on it, so it is recorded before
	// any fencing. A result for a generation this runner has since retired can
	// neither mutate state nor report success, so it is dropped rather than
	// handed on; the key check matters too, because a late result can land in
	// the slot after the next generation has submitted, and handing it to the
	// new submitter would answer a command with another command's result.
	if r.publisher != nil {
		r.publisher.NoteSettled(key, res)
	}
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

// takeTravelOutcome reads and clears the shared travel pending's settled result.
func (r *Runner) takeTravelOutcome() *sim.Result {
	if r.travel == nil || r.travel.outcome == nil {
		return nil
	}
	res := r.travel.outcome
	r.travel.outcome = nil
	return res
}
