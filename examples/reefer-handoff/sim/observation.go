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

import "sort"

// VarKind names a fact a plan can read. The split matters: four of these are
// post-conditions a plan tries to make true, and three are read only while
// choosing what to do about a failed one. Putting a generation-time read in the
// post-condition set would let a plan hold a value the world can change under it.
type VarKind uint8

const (
	// Post-conditions: a plan tries to make these true.
	VarDelivered    VarKind = iota // 0
	VarHolding                     // 1
	VarAt                          // 2
	VarTagConfirmed                // 3

	// Generation-time reads: consulted while choosing an action, never held.
	VarTemp        // 4
	VarSlotPowered // 5
	VarSlotRating  // 6
)

// GenerationTime reports whether a variable is read only while choosing an action,
// as opposed to being a post-condition a plan tries to make true.
func (k VarKind) GenerationTime() bool {
	switch k {
	case VarTemp, VarSlotPowered, VarSlotRating:
		return true
	}
	return false
}

func (k VarKind) String() string {
	switch k {
	case VarDelivered:
		return "Delivered"
	case VarHolding:
		return "Holding"
	case VarAt:
		return "At"
	case VarTagConfirmed:
		return "TagConfirmed"
	case VarTemp:
		return "TempC"
	case VarSlotPowered:
		return "SlotPowered"
	case VarSlotRating:
		return "SlotRating"
	}
	return "Unknown"
}

// VarKey identifies one fact. It is comparable, so a plan may use it as a map
// key, and it names only the entities the fact is about, so a plan keyed by
// tractor and reefer cannot read another job's state by accident.
type VarKey struct {
	Kind    VarKind
	Tractor TractorID
	Reefer  ReeferID
	Slot    SlotID
	Node    NodeID
}

// DeliveredKey is the goal condition for one reefer.
func DeliveredKey(r ReeferID) VarKey { return VarKey{Kind: VarDelivered, Reefer: r} }

// HoldingKey asks whether a tractor holds a reefer.
func HoldingKey(t TractorID, r ReeferID) VarKey {
	return VarKey{Kind: VarHolding, Tractor: t, Reefer: r}
}

// AtKey asks where a tractor stands.
func AtKey(t TractorID) VarKey { return VarKey{Kind: VarAt, Tractor: t} }

// TagKey asks whether a reefer's tag reads.
func TagKey(r ReeferID) VarKey { return VarKey{Kind: VarTagConfirmed, Reefer: r} }

// TempKey reads a reefer's temperature.
func TempKey(r ReeferID) VarKey { return VarKey{Kind: VarTemp, Reefer: r} }

// SlotPoweredKey reads a slot's plug state.
func SlotPoweredKey(s SlotID) VarKey { return VarKey{Kind: VarSlotPowered, Slot: s} }

// SlotRatingKey reads a slot's rating.
func SlotRatingKey(s SlotID) VarKey { return VarKey{Kind: VarSlotRating, Slot: s} }

// String renders a key the way a reader sees it, so a debugger record and a
// condition name agree.
func (k VarKey) String() string {
	switch k.Kind {
	case VarDelivered:
		return "Delivered(" + string(k.Reefer) + ")"
	case VarHolding:
		return "Holding(" + string(k.Tractor) + "," + string(k.Reefer) + ")"
	case VarAt:
		return "At(" + string(k.Tractor) + ")"
	case VarTagConfirmed:
		return "TagConfirmed(" + string(k.Reefer) + ")"
	case VarTemp:
		return "TempC(" + string(k.Reefer) + ")"
	case VarSlotPowered:
		return "SlotPowered(" + string(k.Slot) + ")"
	case VarSlotRating:
		return "SlotRating(" + string(k.Slot) + ")"
	}
	return "Unknown"
}

// Observation is one agent-facing frame of the world: the facts a single tractor
// reads, frozen at the world sequence and revision it was taken at.
//
// A frame is a value. A runner pins one for a whole root tick and every read in
// that tick comes from it, so a tick cannot see half of one world and half of
// another. The world itself is reachable only through the three generation-time
// reads, and only for the job this frame was taken for.
type Observation struct {
	// WorldSeq is the sequence this frame was taken at, and Revision is the
	// revision it names. Both travel with every command the frame produces.
	WorldSeq uint64
	Revision string
	Round    int
	Phase    Phase

	// Tractor is whose frame this is, and Reefer is the job it was taken for.
	Tractor TractorID
	Reefer  ReeferID

	at        NodeID
	holding   ReeferID
	delivered bool
	tagOK     bool
	tempC     int

	reeferAt    NodeID
	slotPowered map[SlotID]bool
	slotRating  map[SlotID]int
	slotVersion map[SlotID]uint64

	laneVersion uint64
	tagVersion  uint64
}

// Observe freezes the world into a frame for one tractor and one job.
//
// The frame reports the reefer's tag as it reads in the world, which in this
// scenario is a single shared truth: REEFER-2's tag does not read until it has
// been scanned. It reports the other two reefers as present but carries no
// detail about them, so a plan for one job has nothing to reason about from
// another's cargo.
func Observe(w World, t TractorID, jobReefer ReeferID) (Observation, bool) {
	tractor, ok := w.Tractor(t)
	if !ok {
		return Observation{}, false
	}
	reefer, ok := w.Reefer(jobReefer)
	if !ok {
		return Observation{}, false
	}
	o := Observation{
		WorldSeq:    w.WorldSeq(),
		Revision:    w.Revision(),
		Round:       w.Round(),
		Phase:       w.Phase(),
		Tractor:     t,
		Reefer:      jobReefer,
		at:          tractor.At,
		holding:     tractor.Hold,
		delivered:   w.Delivered(jobReefer),
		tagOK:       reefer.TagReadable,
		tempC:       reefer.TempC,
		reeferAt:    reefer.At,
		slotPowered: make(map[SlotID]bool, len(w.slots)),
		slotRating:  make(map[SlotID]int, len(w.slots)),
		slotVersion: make(map[SlotID]uint64, len(w.slots)),
		laneVersion: w.topology.Version(),
		tagVersion:  reefer.TagVersion,
	}
	for _, s := range w.slots {
		o.slotPowered[s.ID] = s.Plugged
		o.slotRating[s.ID] = s.RatedMaxC
		o.slotVersion[s.ID] = s.Version
	}
	return o, true
}

// Read returns the value of a key in this frame, and whether the key is one this
// frame answers. A key about another tractor's job is not answered rather than
// answered wrongly, so a plan cannot read across jobs by guessing a key.
func (o Observation) Read(k VarKey) (any, bool) {
	switch k.Kind {
	case VarDelivered:
		if k.Reefer != o.Reefer {
			return nil, false
		}
		return o.delivered, true
	case VarHolding:
		if k.Tractor != o.Tractor || k.Reefer != o.Reefer {
			return nil, false
		}
		return o.holding == o.Reefer, true
	case VarAt:
		if k.Tractor != o.Tractor {
			return nil, false
		}
		return o.at, true
	case VarTagConfirmed:
		if k.Reefer != o.Reefer {
			return nil, false
		}
		return o.tagOK, true
	case VarTemp:
		if k.Reefer != o.Reefer {
			return nil, false
		}
		return o.tempC, true
	case VarSlotPowered:
		v, ok := o.slotPowered[k.Slot]
		return v, ok
	case VarSlotRating:
		v, ok := o.slotRating[k.Slot]
		return v, ok
	}
	return nil, false
}

// At returns the node this frame's tractor is standing on. It is the frame's own
// answer rather than a read of the world, so an action generator choosing a hop
// works from the same snapshot the conditions were evaluated against.
func (o Observation) At() NodeID { return o.at }

// Holding returns the reefer this frame's tractor has in custody, empty when it
// has nothing.
func (o Observation) Holding() ReeferID { return o.holding }

// ReeferAt returns the node the job reefer is standing on. A pickup needs the
// tractor beside it, and the node is read from the same frame as everything else,
// so the guard on a pickup is decided from the world the plan is reasoning over.
func (o Observation) ReeferAt() NodeID { return o.reeferAt }

// TempC returns the job reefer's temperature, which is what decides which slots
// will accept it.
func (o Observation) TempC() int { return o.tempC }

// Expectation returns the versions a command issued from this frame must expect.
// A command that carries these and is reduced against a world where one of them
// has moved is visibly out of date, which is what lets a reader tell a stale
// command from a current one.
func (o Observation) Expectation(slot SlotID) Expectation {
	return Expectation{
		WorldSeq:    o.WorldSeq,
		LaneVersion: o.laneVersion,
		TagVersion:  o.tagVersion,
		SlotVersion: o.slotVersion[slot],
	}
}

// AcceptingSlots returns the slots that would take this job's reefer right now,
// in slot-ID order, and is what the action generator enumerates candidates from.
// A reefer in custody has not reached a slot yet, so custody does not affect the
// answer; only the plug and the rating do.
func (o Observation) AcceptingSlots() []SlotID {
	out := make([]SlotID, 0, len(o.slotPowered))
	for s, plugged := range o.slotPowered {
		if plugged && o.tempC >= o.slotRating[s] {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
