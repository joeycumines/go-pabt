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

	"github.com/joeycumines/go-pabt/examples/reefer-handoff/sim"
)

// vacateLocked walks a delivered tractor home: out of the bay to a waypoint and
// back to the apron, which holds any number. The reducer test walks a finished
// tractor home for exactly this reason: a tractor left standing in the bay
// blocks the route for the next delivery, and nothing else moves it.
//
// The walk is two ordinary MoveTo commands under the same one-in-flight rule,
// and it reports Running until the tractor is back on the apron. A refusal on
// the way out waits rather than fails: the walker holds no job and owns no
// plan, so there is nothing to re-expand, and the bay's next occupant leaves in
// turn. The caller holds the lock.
func (r *Runner) vacateLocked() Outcome {
	if !r.vacating {
		return OutcomeRunning
	}
	if r.inflight != nil {
		if r.inflight.result == nil {
			return OutcomeRunning
		}
		key := r.inflight.key
		res := *r.inflight.result
		r.inflight = nil
		if r.publisher != nil {
			r.publisher.NoteSettled(key, res)
		}
		delete(r.owners, res.Receipt.IdempotencyKey)
		if res.Abandoned != nil {
			return OutcomeRunning
		}
		if !res.Receipt.Applied() {
			// Someone is in the way out. Wait: the walker owns no plan, so a
			// failure has nowhere to expand to, and the holder leaves in turn.
			return OutcomeRunning
		}
		r.vacateSeq++
	}
	if r.inflight != nil {
		return OutcomeRunning
	}
	w := r.loop.Snapshot()
	tug, ok := w.Tractor(r.tug)
	if !ok {
		r.vacating = false
		return OutcomeRunning
	}
	if tug.At == sim.NodeStaging {
		r.vacating = false
		r.vacateSeq = 0
		return OutcomeRunning
	}
	var next sim.NodeID
	switch r.vacateSeq {
	case 0:
		// Out of the bay to whichever waypoint is free. The route home goes
		// through staging, which holds any number, so either approach does.
		if holder, taken := w.Occupant(sim.NodeEast); !taken || holder == r.tug {
			next = sim.NodeEast
		} else {
			next = sim.NodeWest
		}
		if tug.At != sim.NodeBay {
			next = sim.NodeStaging
		}
	default:
		next = sim.NodeStaging
	}
	r.tick++
	key := r.keyLocked("vac" + strconv.FormatUint(uint64(r.vacateSeq+1), 10))
	cmd := sim.Command{
		ID: key, IdempotencyKey: key, Kind: sim.CmdMoveTo, Tractor: r.tug,
		Node: next, Expect: r.vacateExpectLocked(w),
		PlanID: r.planIDLocked(),
		// The walk home is not submitted by a plan tick: the generation was
		// retired when the job was delivered, and the runner's own loop is
		// walking the tractor out of the bay. The zero stamp means "no plan
		// tick" — the envelope contract's own words — so the causality never
		// claims a tick that never happened. The in-flight record below still
		// fences on the live generation, and the key still names it.
	}
	ch, err := r.loop.Submit(cmd)
	if err != nil {
		return OutcomeRunning
	}
	r.owners[key] = nil
	r.inflight = &inflight{key: key, gen: r.gen, planTickSeq: r.tick}
	if r.publisher != nil {
		r.publisher.NoteSubmitted(cmd, 0)
	}
	go r.await(key, ch)
	return OutcomeRunning
}

// vacateExpectLocked builds the expectation for a walk-home command from a live
// world read. There is no pinned frame while vacating — the walker owns no plan
// — so the versions are read from the world the command is decided against.
func (r *Runner) vacateExpectLocked(w sim.World) sim.Expectation {
	o, ok := sim.Observe(w, r.tug, sim.Reefer1)
	if !ok {
		return sim.Expectation{WorldSeq: w.WorldSeq()}
	}
	return o.Expectation("")
}
