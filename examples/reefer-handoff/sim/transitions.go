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

// The five transitions. Each one is atomic: it either commits its whole effect
// and publishes its events, or it changes nothing and returns a reason together
// with the holder of whatever it lost to.
//
// A transition is a method on World returning a modified copy, so the world it
// was called on is never touched. That is what lets a refused command leave the
// caller's world byte-identical.

// claimJob awards one unclaimed job to one tractor. Which tractor gets which
// reefer is decided here, in manifest order, and never by arithmetic on the
// tractor's index.
func (w World) claimJob(c Command) (Receipt, []WorldEvent, bool) {
	t, ok := w.Tractor(c.Tractor)
	if !ok {
		return w.decline(c, ReasonUnknownEntity, "")
	}
	if t.Job != "" {
		return w.decline(c, ReasonTractorBusy, "")
	}
	idx := -1
	for i, j := range w.jobs {
		if j.ClaimedBy == "" && !j.Done {
			idx = i
			break
		}
	}
	if idx < 0 {
		return w.decline(c, ReasonNoJobFree, "")
	}
	job := w.jobs[idx]
	job.ClaimedBy = t.ID
	w.jobs[idx] = job
	t.Job = job.ID
	w.setTractor(t)

	return w.ok(c, []WorldEvent{{
		Kind: EvJobClaimed, Tractor: t.ID, Reefer: job.Reefer, CauseCommand: c.ID,
	}})
}

// scanTag makes an unreadable reefer pickable. It is the only thing in the
// domain that can do so, which is why a plan whose pickup is blocked on an
// unconfirmed tag has to grow this action rather than route around it.
func (w World) scanTag(c Command) (Receipt, []WorldEvent, bool) {
	t, ok := w.Tractor(c.Tractor)
	if !ok {
		return w.decline(c, ReasonUnknownEntity, "")
	}
	r, ok := w.Reefer(c.Reefer)
	if !ok {
		return w.decline(c, ReasonUnknownEntity, "")
	}
	if r.At != t.At {
		return w.decline(c, ReasonNotBesideReefer, "")
	}
	if r.TagReadable {
		return w.decline(c, ReasonTagReadable, "")
	}
	w.setReefer(Reefer{
		ID: r.ID, TempC: r.TempC, At: r.At, Custodian: r.Custodian,
		TagReadable: true, TagVersion: r.TagVersion + 1,
	})

	return w.ok(c, []WorldEvent{{
		Kind: EvTagScanned, Tractor: t.ID, Reefer: r.ID, CauseCommand: c.ID,
	}})
}

// pickUp takes custody of a reefer standing at the tractor's node. A reefer
// whose tag does not read cannot be picked up, which is the refusal the RFID
// fault produces.
func (w World) pickUp(c Command) (Receipt, []WorldEvent, bool) {
	t, ok := w.Tractor(c.Tractor)
	if !ok {
		return w.decline(c, ReasonUnknownEntity, "")
	}
	if t.Hold != "" {
		return w.decline(c, ReasonAlreadyHeld, string(t.Hold))
	}
	r, ok := w.Reefer(c.Reefer)
	if !ok {
		return w.decline(c, ReasonUnknownEntity, "")
	}
	if r.Custodian != "" {
		return w.decline(c, ReasonAlreadyHeld, string(r.Custodian))
	}
	if r.At != t.At {
		return w.decline(c, ReasonNotBesideReefer, "")
	}
	if !r.TagReadable {
		return w.decline(c, ReasonTagUnreadable, "")
	}

	r.Custodian = t.ID
	r.At = t.At
	w.setReefer(r)
	t.Hold = r.ID
	w.setTractor(t)

	return w.ok(c, []WorldEvent{{
		Kind: EvPickedUp, Tractor: t.ID, Reefer: r.ID, Node: r.At, CauseCommand: c.ID,
	}})
}

// moveTo crosses one lane into an adjacent node. Three separate things can
// refuse it, and the reason says which: the destination is not a tractor node,
// there is no lane, the lane is closed, or the node is already taken.
func (w World) moveTo(c Command) (Receipt, []WorldEvent, bool) {
	t, ok := w.Tractor(c.Tractor)
	if !ok {
		return w.decline(c, ReasonUnknownEntity, "")
	}
	n, ok := w.Node(c.Node)
	if !ok {
		return w.decline(c, ReasonUnknownEntity, "")
	}
	if n.ReeferDestination {
		return w.decline(c, ReasonNotATractorNode, "")
	}
	lane, ok := w.topology.LaneBetween(t.At, c.Node)
	if !ok {
		return w.decline(c, ReasonNotAdjacent, "")
	}
	if !w.topology.Open(lane) {
		return w.decline(c, ReasonLaneLocked, string(lane))
	}
	if holder, taken := w.Occupant(c.Node); taken && n.Capacity > 0 {
		return w.decline(c, occupancyReason(c.Node), string(holder))
	}

	t.At = c.Node
	w.setTractor(t)
	if t.Hold != "" {
		if r, ok := w.Reefer(t.Hold); ok {
			r.At = c.Node
			w.setReefer(r)
		}
	}

	return w.ok(c, []WorldEvent{{
		Kind: EvMoved, Tractor: t.ID, Node: c.Node, Lane: lane, CauseCommand: c.ID,
	}})
}

// occupancyReason names the bay specifically. The transfer bay is the contention
// the scenario is built around, and a reader wants it called out on its own
// instead of lumped in with the waypoints.
func occupancyReason(n NodeID) Reason {
	if n == NodeBay {
		return ReasonBayOccupied
	}
	return ReasonNodeOccupied
}

// place hands the held reefer to a slot from the bay. A slot that is unpowered
// or under-rated refuses without committing anything, and a plan aimed at such a
// slot re-expands instead of retrying the same placement.
func (w World) place(c Command) (Receipt, []WorldEvent, bool) {
	t, ok := w.Tractor(c.Tractor)
	if !ok {
		return w.decline(c, ReasonUnknownEntity, "")
	}
	if t.Hold != c.Reefer || c.Reefer == "" {
		return w.decline(c, ReasonNoCustody, "")
	}
	r, ok := w.Reefer(c.Reefer)
	if !ok {
		return w.decline(c, ReasonUnknownEntity, "")
	}
	s, ok := w.Slot(c.Slot)
	if !ok {
		return w.decline(c, ReasonUnknownEntity, "")
	}
	if t.At != NodeBay {
		return w.decline(c, ReasonNotAtBay, string(t.At))
	}
	if !s.Plugged {
		return w.decline(c, ReasonSlotUnpowered, string(s.ID))
	}
	if !s.RatingsAccept(r) {
		return w.decline(c, ReasonSlotUnderRated, string(s.ID))
	}
	if s.Occupied != "" {
		return w.decline(c, ReasonSlotOccupied, string(s.Occupied))
	}

	s.Occupied = r.ID
	s.Version++
	w.setSlot(s)
	r.Custodian = ""
	r.At = s.Node
	w.setReefer(r)
	t.Hold = ""
	w.setTractor(t)
	job, released := w.completeJobFor(r.ID)

	events := []WorldEvent{{
		Kind: EvPlaced, Tractor: t.ID, Reefer: r.ID, Slot: s.ID, Node: s.Node, CauseCommand: c.ID,
	}}
	if released {
		events = append(events, WorldEvent{
			Kind: EvJobCompleted, Reefer: r.ID, Tractor: job, CauseCommand: c.ID,
		})
	}
	return w.ok(c, events)
}

// ok builds the receipt for a command that committed, before the world sequence
// is stamped onto it, and pairs it with the events the transition published.
// Holder stays empty: it names the loser of a contest, and a committed command
// is not a loser.
func (w World) ok(c Command, evs []WorldEvent) (Receipt, []WorldEvent, bool) {
	return Receipt{
		ID:             c.ID,
		IdempotencyKey: c.IdempotencyKey,
		Kind:           c.Kind,
		Tractor:        c.Tractor,
		CommandSeq:     w.commandSeq,
		Outcome:        OutcomeApplied,
		Reason:         ReasonNone,
	}, evs, true
}

// decline refuses a command from inside a transition. It reports the contended
// holder where there is one, so a loser learns who won without a second query.
func (w World) decline(c Command, reason Reason, holder string) (Receipt, []WorldEvent, bool) {
	return Receipt{
		ID:             c.ID,
		IdempotencyKey: c.IdempotencyKey,
		Kind:           c.Kind,
		Tractor:        c.Tractor,
		CommandSeq:     w.commandSeq,
		Outcome:        OutcomeRefused,
		Reason:         reason,
		Holder:         holder,
	}, nil, false
}

func (w World) setTractor(t Tractor) {
	for i := range w.tractors {
		if w.tractors[i].ID == t.ID {
			w.tractors[i] = t
			return
		}
	}
}

func (w World) setReefer(r Reefer) {
	for i := range w.reefers {
		if w.reefers[i].ID == r.ID {
			w.reefers[i] = r
			return
		}
	}
}

func (w World) setSlot(s Slot) {
	for i := range w.slots {
		if w.slots[i].ID == s.ID {
			w.slots[i] = s
			return
		}
	}
}

// completeJobFor latches the job for a reefer as done and releases the tractor
// that held it, so the tractor can claim again. The latch records that the work
// was carried out; whether the reefer is still delivered is a separate question
// the phase answers from the physical state, because the power desk can take a
// plug out of an occupied slot. It reports the released tractor, and false when
// the job was already latched.
func (w World) completeJobFor(reefer ReeferID) (TractorID, bool) {
	for i := range w.jobs {
		if w.jobs[i].Reefer != reefer {
			continue
		}
		if w.jobs[i].Done {
			return "", false
		}
		claimant := w.jobs[i].ClaimedBy
		w.jobs[i].Done = true
		if claimant == "" {
			return "", false
		}
		if t, ok := w.Tractor(claimant); ok {
			t.Job = ""
			w.setTractor(t)
		}
		return claimant, true
	}
	return "", false
}
