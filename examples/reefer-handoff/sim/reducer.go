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

package sim

// Reduce is the world's only mutator. It is a function from a world and a
// command to a new world, a receipt, and the events the change published, and it
// touches no clock, no goroutine, and no I/O.
//
// Four properties hold for every command, and the reducer tests assert all four:
//
//   - The world it was given is not touched. A caller holding an earlier World
//     still sees the state that world had.
//   - A refused command changes no logical state and does not advance worldSeq.
//   - A committed mutation advances worldSeq by exactly one.
//   - Re-submitting a command whose idempotency key is already in the ledger
//     returns the original receipt and changes nothing.
//
// Every receipt is kept, applied or refused, because a refused command is a fact
// a reader has to be able to see later with no world effect attached. The ledger
// is what makes a refusal visible; the world sequence is what shows it changed
// nothing. A key is therefore spent by its first reduction, so a genuine retry
// after a replan mints a new key for what is a new action instance.
func Reduce(w World, c Command) (World, Receipt, []WorldEvent) {
	// Clone first. The transitions write through the world's slices by index, so
	// without this the caller's world would change underneath them.
	w = w.clone()
	w.commandSeq++

	// Idempotency is checked before anything else, so a retry can never apply
	// twice even if the world has moved on since the first attempt.
	if prior, ok := w.receiptFor(c.IdempotencyKey); ok {
		dup := prior
		dup.Outcome = OutcomeDuplicate
		return w, dup, nil
	}

	if w.phase != PhaseRunning {
		receipt := w.stamp(w.declined(c, ReasonOfPhase(w.phase), ""))
		w.record(receipt)
		return w, receipt, nil
	}

	receipt, events, changed := w.dispatch(c)
	if !changed {
		// A refusal is stamped with the world it was refused against, so a reader
		// can see which state turned it down. It advances nothing.
		receipt = w.stamp(receipt)
		w.record(receipt)
		return w, receipt, nil
	}

	w.worldSeq++
	receipt = w.stamp(receipt)
	w.stampEvents(events)
	w.record(receipt)
	if phase := w.settle(); phase != nil {
		phase.WorldSeq = w.worldSeq
		phase.WorldRevision = w.Revision()
		events = append(events, *phase)
	}
	return w, receipt, events
}

// dispatch routes a command to its transition. The returned receipt carries the
// outcome and, on refusal, the reason and the contended holder; changed reports
// whether the world moved.
func (w World) dispatch(c Command) (Receipt, []WorldEvent, bool) {
	switch c.Kind {
	case CmdClaimJob:
		return w.claimJob(c)
	case CmdScanTag:
		return w.scanTag(c)
	case CmdPickUp:
		return w.pickUp(c)
	case CmdMoveTo:
		return w.moveTo(c)
	case CmdPlace:
		return w.place(c)
	}
	return w.decline(c, ReasonUnknownEntity, "")
}

// BeginRound advances the host's round counter and re-derives the terminal
// phases. It is the only thing that advances a round, which is what keeps the
// cutoff a statement about elapsed time instead of about how much the world
// happened to change. A round boundary is not a mutation, so it does not advance
// the world sequence; the phase it may cause shares the sequence already reached.
func BeginRound(w World) (World, []WorldEvent) {
	w = w.clone()
	w.round++
	if phase := w.settle(); phase != nil {
		return w, []WorldEvent{*phase}
	}
	return w, nil
}

// settle re-derives the terminal phases from the physical state and returns the
// event for a phase it entered, or nil. Reaching Complete means every job's
// reefer is standing in a plugged, correctly rated slot right now — a latch
// recording that a job was once fulfilled would let the power desk take a plug
// out of an occupied slot and leave the run claiming a delivery it no longer has.
func (w *World) settle() *WorldEvent {
	if w.phase != PhaseRunning {
		return nil
	}
	if w.allDelivered() {
		w.phase = PhaseComplete
		return &WorldEvent{Kind: EvWorldComplete}
	}
	if w.round > CutoffRound {
		w.phase = PhaseCutoff
		return &WorldEvent{Kind: EvCutoffReached}
	}
	return nil
}

func (w World) allDelivered() bool {
	if len(w.jobs) == 0 {
		return false
	}
	for _, j := range w.jobs {
		if !w.Delivered(j.Reefer) {
			return false
		}
	}
	return true
}

// Shortfall names the reefers the run did not deliver, in manifest order, so the
// Cutoff phase can say what it missed instead of only that it missed something.
func (w World) Shortfall() []ReeferID {
	var out []ReeferID
	for _, j := range w.jobs {
		if !w.Delivered(j.Reefer) {
			out = append(out, j.Reefer)
		}
	}
	return out
}

func (w World) receiptFor(key string) (Receipt, bool) {
	if key == "" {
		return Receipt{}, false
	}
	for _, r := range w.receipts {
		if r.IdempotencyKey == key {
			return r, true
		}
	}
	return Receipt{}, false
}

// declined builds a refusal receipt for a command the world will not reduce at
// all, which is the case of a command arriving after a terminal phase.
func (w World) declined(c Command, reason Reason, holder string) Receipt {
	return Receipt{
		ID:             c.ID,
		IdempotencyKey: c.IdempotencyKey,
		Kind:           c.Kind,
		Tractor:        c.Tractor,
		CommandSeq:     w.commandSeq,
		Outcome:        OutcomeRefused,
		Reason:         reason,
		Holder:         holder,
	}
}

// stamp puts the world's current sequence and revision on a receipt. It is called
// once the sequence has been advanced for a commit and before it for a refusal,
// which is the whole difference between the two.
func (w World) stamp(r Receipt) Receipt {
	r.WorldSeq = w.worldSeq
	r.WorldRevision = w.Revision()
	return r
}

// stampEvents puts the current sequence and revision on the events a transition
// published.
func (w World) stampEvents(events []WorldEvent) []WorldEvent {
	for i := range events {
		events[i].WorldSeq = w.worldSeq
		events[i].WorldRevision = w.Revision()
	}
	return events
}

// record appends a receipt to the ledger. Every receipt is kept, so a reader can
// find the refusal later and see the world sequence it did not move.
func (w *World) record(r Receipt) {
	w.receipts = append(w.receipts, r)
}

// ReasonOfPhase names the terminal phase a run has reached, so a command refused
// after the fact carries a reason a reader can act on.
func ReasonOfPhase(p Phase) Reason {
	switch p {
	case PhaseCutoff:
		return ReasonWorldCutoff
	case PhaseComplete:
		return ReasonWorldComplete
	}
	return ReasonUnknownEntity
}
