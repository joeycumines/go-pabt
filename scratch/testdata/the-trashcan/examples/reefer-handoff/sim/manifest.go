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

// Package sim is the Reefer Handoff world: a container apron, three
// temperature-sensitive reefers, three powered slots, one transfer bay of
// capacity one, and three autonomous yard tractors.
//
// The domain is pure. Nothing in this package reads a clock, draws a random
// number, or performs I/O, and nothing here imports go-eventloop. State changes
// only through Reduce, which is a function from a world and a command to a new
// world, a receipt, and the world events the change produced.
package sim

// ReeferID identifies a temperature-sensitive container.
type ReeferID string

// SlotID identifies a powered reefer slot on the terminal.
type SlotID string

// TractorID identifies an autonomous yard tractor.
type TractorID string

// NodeID identifies a node of the terminal topology.
type NodeID string

// LaneID identifies a lane between two nodes.
type LaneID string

// JobID identifies one unit of work: a reefer that must reach a slot.
type JobID string

// The three reefers. REEFER-1 is a deep freeze, the other two are chilled, and
// REEFER-2's tag does not read, which is what forces the scan.
const (
	Reefer1 ReeferID = "REEFER-1"
	Reefer2 ReeferID = "REEFER-2"
	Reefer3 ReeferID = "REEFER-3"
)

// The three powered slots. SLOT-2 is the older unit and cannot take a
// deep-freeze reefer; the power desk takes its plug at round 40.
const (
	Slot1 SlotID = "SLOT-1"
	Slot2 SlotID = "SLOT-2"
	Slot3 SlotID = "SLOT-3"
)

// The three tractors. Which reefer each one ends up with is decided by the
// reducer awarding a claim, never by arithmetic on the tractor's index.
const (
	Tug1 TractorID = "TUG-1"
	Tug2 TractorID = "TUG-2"
	Tug3 TractorID = "TUG-3"
)

// The seven nodes. The three slot nodes are reefer destinations and are never
// tractor destinations, which is why a tractor places from the bay.
const (
	NodeStaging NodeID = "STAGING"
	NodeWest    NodeID = "WEST"
	NodeEast    NodeID = "EAST"
	NodeBay     NodeID = "BAY"
	NodeSlot1   NodeID = "SLOT-1"
	NodeSlot2   NodeID = "SLOT-2"
	NodeSlot3   NodeID = "SLOT-3"
)

// The seven lanes.
const (
	LaneWest    LaneID = "lane-west"
	LaneEast    LaneID = "lane-east"
	LaneBayWest LaneID = "lane-bay-west"
	LaneBayEast LaneID = "lane-bay-east"
	LaneSlot1   LaneID = "lane-slot-1"
	LaneSlot2   LaneID = "lane-slot-2"
	LaneSlot3   LaneID = "lane-slot-3"
)

// CutoffRound is the round at which the vessel sails. Both the success and the
// failure condition are stated in terms of it, so neither is untestable.
const CutoffRound = 120

// Reefers returns the three reefer records as the manifest declares them: all
// three on the apron, with REEFER-2's tag unreadable.
func Reefers() []Reefer {
	return []Reefer{
		{ID: Reefer1, TempC: -22, TagReadable: true, At: NodeStaging},
		{ID: Reefer2, TempC: -8, TagReadable: false, At: NodeStaging},
		{ID: Reefer3, TempC: -8, TagReadable: true, At: NodeStaging},
	}
}

// Slots returns the three slot records as the manifest declares them: all three
// plugged at round 0, with SLOT-2 under-rated for a deep-freeze reefer.
func Slots() []Slot {
	return []Slot{
		{ID: Slot1, Node: NodeSlot1, RatedMaxC: -30, Plugged: true, Version: 1},
		{ID: Slot2, Node: NodeSlot2, RatedMaxC: -18, Plugged: true, Version: 1},
		{ID: Slot3, Node: NodeSlot3, RatedMaxC: -30, Plugged: true, Version: 1},
	}
}

// Tractors returns the three tractor records as the manifest declares them, all
// three idle on the apron with no job and nothing in custody.
func Tractors() []Tractor {
	return []Tractor{
		{ID: Tug1, At: NodeStaging},
		{ID: Tug2, At: NodeStaging},
		{ID: Tug3, At: NodeStaging},
	}
}

// Jobs returns one job per reefer, in manifest order. Every job starts unclaimed.
func Jobs() []Job {
	return []Job{
		{ID: JobID(Reefer1), Reefer: Reefer1},
		{ID: JobID(Reefer2), Reefer: Reefer2},
		{ID: JobID(Reefer3), Reefer: Reefer3},
	}
}

// Reefer is one temperature-sensitive container. TempC never changes: a reefer
// holds its temperature because it is plugged into a slot that can hold it, not
// because anything in this domain warms or cools it.
//
// At and Custodian are kept consistent by the reducer. A reefer in custody
// stands where its custodian stands, so a reefer's position is either its own
// At or the position of the tractor holding it.
type Reefer struct {
	ID          ReeferID
	TempC       int
	TagReadable bool

	// At is the reefer's own position while it is on the apron. Once a tractor
	// takes custody, Custodian names the tractor and At tracks that tractor.
	At NodeID

	// Custodian is the tractor holding this reefer, empty when it is on the apron.
	Custodian TractorID

	// TagVersion increases when the tag's readability changes, and is the value a
	// PickUp or ScanTag command must expect.
	TagVersion uint64
}

// Slot is a powered reefer slot. A slot accepts a reefer when it is plugged and
// the reefer is no colder than the slot's rating, because a unit can only cool
// cargo down to its own rating.
type Slot struct {
	ID        SlotID
	Node      NodeID
	RatedMaxC int
	Plugged   bool

	// Occupied names the reefer standing in this slot, empty when it is free.
	Occupied ReeferID

	// Version increases on every change to this slot, and is the value a Place
	// command must expect.
	Version uint64
}

// Accepts reports whether this slot can take this reefer right now. Plugged and
// rating are the whole test; occupancy is contention, not acceptance, and a
// refusal names which of the two failed.
func (s Slot) Accepts(r Reefer) bool {
	return s.Plugged && s.RatingsAccept(r)
}

// RatingsAccept reports whether this slot is rated for this reefer, ignoring the
// plug. A unit can only cool cargo down to its own rating, so a reefer colder
// than the rating is refused however good the power is.
func (s Slot) RatingsAccept(r Reefer) bool { return r.TempC >= s.RatedMaxC }

// Tractor is an autonomous yard tractor. It holds at most one job and at most
// one reefer, and the plan runner that drives it is a separate concern that this
// domain never sees.
type Tractor struct {
	ID   TractorID
	At   NodeID
	Job  JobID
	Hold ReeferID
}

// Job is one unit of work. A job is claimed by exactly one tractor and completed
// when its reefer is delivered, so the reducer is what pairs a tractor with a
// reefer.
type Job struct {
	ID        JobID
	Reefer    ReeferID
	ClaimedBy TractorID
	Done      bool
}

// Node is a node of the terminal topology. Capacity bounds how many tractors
// may stand there, and ReeferDestination marks the slot nodes, which a tractor
// may never enter.
type Node struct {
	ID                NodeID
	Capacity          int
	ReeferDestination bool
}

// Nodes returns the seven nodes. The apron is the only node a tractor may share;
// the waypoints, the bay, and the slots each hold one.
func Nodes() []Node {
	return []Node{
		{ID: NodeStaging, Capacity: 0},
		{ID: NodeWest, Capacity: 1},
		{ID: NodeEast, Capacity: 1},
		{ID: NodeBay, Capacity: 1},
		{ID: NodeSlot1, Capacity: 1, ReeferDestination: true},
		{ID: NodeSlot2, Capacity: 1, ReeferDestination: true},
		{ID: NodeSlot3, Capacity: 1, ReeferDestination: true},
	}
}
