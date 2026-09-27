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

// CommandKind is the closed set of things a tractor may ask the world to do.
// There is no reservation, no lease, and no separate commit: each of these is
// one atomic transition, and contention is decided when it is reduced.
type CommandKind string

const (
	// CmdClaimJob asks for one unclaimed job. Two tractors claiming in the same
	// round produce one winner and one refusal.
	CmdClaimJob CommandKind = "ClaimJob"

	// CmdScanTag reads a reefer's tag, which is the only thing that can make an
	// unreadable reefer pickable.
	CmdScanTag CommandKind = "ScanTag"

	// CmdPickUp takes custody of a reefer standing at the tractor's node.
	CmdPickUp CommandKind = "PickUp"

	// CmdMoveTo crosses one lane into an adjacent node.
	CmdMoveTo CommandKind = "MoveTo"

	// CmdPlace hands the held reefer to a slot, from the bay.
	CmdPlace CommandKind = "Place"
)

// Command is one request from a tractor to the world.
//
// IdempotencyKey is the runner's own identity for the action that produced this
// command, and it is the key a receipt is matched by. Re-submitting a command
// with a key the world has already reduced returns the original receipt and
// changes nothing, so a runner that retries after an uncertain wait cannot apply
// an action twice.
//
// Expect records the versions the command was decided against. A command is
// reduced against the world as it is now, and these values are what let a reader
// see that the runner and the world agreed about the state they were reasoning
// over.
type Command struct {
	ID             string
	IdempotencyKey string
	Kind           CommandKind

	Tractor TractorID
	Reefer  ReeferID
	Slot    SlotID
	Node    NodeID
	Lane    LaneID

	PlanID      string
	Generation  uint64
	ActionID    uint64
	PlanTickSeq uint64

	Expect Expectation
}

// Expectation is the set of versions a command was decided against.
type Expectation struct {
	// WorldSeq is the world sequence the runner's pinned frame was taken at.
	WorldSeq uint64

	// SlotVersion is the target slot's version, for CmdPlace.
	SlotVersion uint64

	// LaneVersion is the topology version, for CmdMoveTo.
	LaneVersion uint64

	// TagVersion is the target tag's version, for CmdPickUp and CmdScanTag.
	TagVersion uint64
}

// Outcome is what the world did with a command.
type Outcome string

const (
	// OutcomeApplied means the world changed.
	OutcomeApplied Outcome = "Applied"

	// OutcomeRefused means the world did not change, and Reason says why.
	OutcomeRefused Outcome = "Refused"

	// OutcomeDuplicate means the key had already been reduced. The original
	// receipt is returned unchanged and the world did not change.
	OutcomeDuplicate Outcome = "Duplicate"
)

// Reason is the typed cause of a refusal. A refusal is never free text: the
// debugger and the monitor both key off these values.
type Reason string

const (
	ReasonNone Reason = ""

	// Contention. The job queue is not a loser-takes-all resource: with three
	// jobs and three tractors every claim is awarded a distinct job, so the
	// queue's only refusal is that it has run dry.
	ReasonBayOccupied  Reason = "BayOccupied"
	ReasonSlotOccupied Reason = "SlotOccupied"
	ReasonNodeOccupied Reason = "NodeOccupied"
	ReasonNoJobFree    Reason = "NoJobFree"
	ReasonTractorBusy  Reason = "TractorBusy"

	// Slot state. A placement into a slot that is unpowered or under-rated is
	// refused without committing anything.
	ReasonSlotUnpowered  Reason = "SlotUnpowered"
	ReasonSlotUnderRated Reason = "SlotUnderRated"

	// Custody and tags.
	ReasonNoCustody       Reason = "NoCustody"
	ReasonAlreadyHeld     Reason = "AlreadyHeld"
	ReasonNotBesideReefer Reason = "NotBesideReefer"
	ReasonTagUnreadable   Reason = "TagUnreadable"
	ReasonTagReadable     Reason = "TagReadable"

	// Phase. Both terminal phases refuse every command, which is what makes them
	// terminal rather than advisory.
	ReasonWorldComplete Reason = "WorldComplete"
	ReasonWorldCutoff   Reason = "WorldCutoff"

	// ReasonUnknownEntity names a command addressed to something the world does
	// not have, and is the refusal for a command kind outside the closed set.
	ReasonUnknownEntity Reason = "UnknownEntity"

	// Topology.
	ReasonLaneLocked      Reason = "LaneLocked"
	ReasonNotAdjacent     Reason = "NotAdjacent"
	ReasonNotATractorNode Reason = "NotATractorNode"
	ReasonNotAtBay        Reason = "NotAtBay"
)

// Receipt is the world's answer to one command. It is the only way a runner
// learns what happened, and it is the record the debugger shows.
type Receipt struct {
	ID             string
	IdempotencyKey string
	Kind           CommandKind
	Tractor        TractorID

	// CommandSeq orders commands as the world received them.
	CommandSeq uint64

	// WorldSeq is the sequence at which this command was reduced, and
	// WorldRevision is the revision it produced. A refusal produces neither a new
	// revision nor a new sequence.
	WorldSeq      uint64
	WorldRevision string

	Outcome Outcome
	Reason  Reason

	// Holder names the current holder of a contended resource on a refusal, so a
	// loser learns who won without a second query.
	Holder string
}

// Applied reports whether this receipt committed a change to the world.
func (r Receipt) Applied() bool { return r.Outcome == OutcomeApplied }

// WorldEventKind is a fact the world published. Events are what the debugger
// records as the world lane, and each one names the world sequence and revision
// it produced.
type WorldEventKind string

const (
	EvJobClaimed       WorldEventKind = "JobClaimed"
	EvTagScanned       WorldEventKind = "TagScanned"
	EvPickedUp         WorldEventKind = "PickedUp"
	EvMoved            WorldEventKind = "Moved"
	EvPlaced           WorldEventKind = "Placed"
	EvJobCompleted     WorldEventKind = "JobCompleted"
	EvLaneLocked       WorldEventKind = "LaneLocked"
	EvLaneReopened     WorldEventKind = "LaneReopened"
	EvSlotPowerChanged WorldEventKind = "SlotPowerChanged"
	EvCutoffReached    WorldEventKind = "CutoffReached"
	EvWorldComplete    WorldEventKind = "WorldComplete"
)

// WorldEvent is one published fact about the world.
//
// A refusal is not one of these. A refused command changed nothing, so it has a
// receipt and no world event, and that is what tells a reader the difference
// between a command that was turned down and a change that happened.
type WorldEvent struct {
	Kind          WorldEventKind
	WorldSeq      uint64
	WorldRevision string
	Tractor       TractorID
	Reefer        ReeferID
	Slot          SlotID
	Node          NodeID
	Lane          LaneID
	// CauseCommand is the ID of the command that produced this event, empty for an
	// event the exogenous actors or the host's round boundary caused.
	CauseCommand string
}

// Abandoned marks a command the world admitted and then never reduced, which
// happens when an immediate close discards admitted work. A runner waiting on a
// receipt that will never arrive reads this instead of waiting forever, and it
// is published during shutdown reconciliation rather than by Reduce.
type Abandoned struct {
	CommandID string
	Reason    Reason
}

// ReasonAbandoned is the reason carried by every Abandoned marker.
const ReasonAbandoned Reason = "Abandoned"
