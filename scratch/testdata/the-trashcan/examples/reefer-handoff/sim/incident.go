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

// The exogenous half of the world. The wind controller, the slot power desk,
// and the vessel's cutoff do not send commands and hold no plan; they change the
// world from outside, and a plan that was settled before the change finds its
// post-condition false on its next tick.
//
// Every one of these is a value transition like any other, so each one advances
// the world sequence exactly once and publishes exactly one event. A change that
// would alter nothing — locking a lane that is already closed — publishes
// nothing and does not advance the sequence.

// LockLane closes a lane on the wind controller's behalf.
func LockLane(w World, l LaneID) (World, []WorldEvent) {
	if !w.topology.Open(l) {
		return w, nil
	}
	w.topology = w.topology.Lock(l)
	return w.publish(WorldEvent{Kind: EvLaneLocked, Lane: l})
}

// ReopenLane opens a lane on the wind controller's behalf.
func ReopenLane(w World, l LaneID) (World, []WorldEvent) {
	if w.topology.Open(l) {
		return w, nil
	}
	w.topology = w.topology.Reopen(l)
	return w.publish(WorldEvent{Kind: EvLaneReopened, Lane: l})
}

// SetSlotPower plugs or unplugs a slot on the power desk's behalf. An unpluggable
// slot still exists and is still rated; it simply refuses everything, and its
// version advances so a Place that was decided against the old power is visible
// as having been decided against something else.
func SetSlotPower(w World, id SlotID, plugged bool) (World, []WorldEvent) {
	s, ok := w.Slot(id)
	if !ok || s.Plugged == plugged {
		return w, nil
	}
	s.Plugged = plugged
	s.Version++
	w.setSlot(s)
	return w.publish(WorldEvent{Kind: EvSlotPowerChanged, Slot: id})
}

// publish advances the world sequence by one and stamps an event with it, so an
// exogenous change is ordered in the same lane as a command's effect. A phase
// the change happens to cause is a derivation of the new state, not a second
// mutation, so it rides at the same sequence rather than advancing one.
func (w World) publish(ev WorldEvent) (World, []WorldEvent) {
	w = w.clone()
	w.worldSeq++
	ev.WorldSeq = w.worldSeq
	ev.WorldRevision = w.Revision()
	events := []WorldEvent{ev}
	if phase := w.settle(); phase != nil {
		phase.WorldSeq = w.worldSeq
		phase.WorldRevision = w.Revision()
		events = append(events, *phase)
	}
	return w, events
}

// Scripted returns the incident as a table of exogenous changes keyed by the
// round they fire on, so the scenario is one literal list rather than a
// scattering of magic numbers across the runtime.
//
// The wind lockout closes the western approach at round 20 and reopens it at
// round 55, which is what makes a replan itself go stale. The power desk takes
// SLOT-2's plug at round 40, after which every placement there is refused.
func Scripted(round int) []Incident {
	all := []Incident{
		{Round: 20, Apply: func(w World) (World, []WorldEvent) { return LockLane(w, LaneBayWest) }},
		{Round: 40, Apply: func(w World) (World, []WorldEvent) { return SetSlotPower(w, Slot2, false) }},
		{Round: 55, Apply: func(w World) (World, []WorldEvent) { return ReopenLane(w, LaneBayWest) }},
	}
	var due []Incident
	for _, in := range all {
		if in.Round == round {
			due = append(due, in)
		}
	}
	return due
}

// Incident is one exogenous change due at a round.
type Incident struct {
	Round int
	Apply func(World) (World, []WorldEvent)
}
