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

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Phase is where the run has got to. Success and failure are both terminal.
type Phase string

const (
	PhaseRunning  Phase = "Running"
	PhaseComplete Phase = "Complete"
	PhaseCutoff   Phase = "Cutoff"
)

// World is the whole terminal at one point in the run.
//
// It holds slices only, which is what makes two worlds reached the same way
// comparable without a canonicalisation step, but a slice header still shares
// its backing array. So a World is only safe to hand out after clone, and every
// mutation path clones first. Without that, Reduce would write through the
// caller's arrays and a reader holding an earlier World would see it change
// underneath them, which is exactly the pinned-frame guarantee the plan runner
// and the debugger records both depend on.
type World struct {
	reefers  []Reefer
	slots    []Slot
	tractors []Tractor
	jobs     []Job
	nodes    []Node
	topology Topology

	// round is the host's tick counter. It is advanced by BeginRound, not by a
	// mutation, because a round and a world revision are different domains: a
	// round may contain several revisions and may contain none.
	round int

	// worldSeq counts committed mutations and nothing else. A refused command
	// and a round boundary both leave it alone, which is what makes "one
	// mutation, one sequence" checkable.
	worldSeq uint64

	// commandSeq counts every command the world received, applied or not, so a
	// reader can order the command lane independently of the world lane.
	commandSeq uint64

	phase Phase

	// receipts is the append-only ledger keyed by idempotency key. It is what
	// makes Reduce idempotent, and it is part of the state a replay must
	// reproduce.
	receipts []Receipt
}

// clone returns a world whose slices are freshly allocated, so that writing
// through one copy cannot be observed by a holder of another. It is the only
// thing that makes World a value rather than a set of shared views.
func (w World) clone() World {
	w.reefers = append([]Reefer(nil), w.reefers...)
	w.slots = append([]Slot(nil), w.slots...)
	w.tractors = append([]Tractor(nil), w.tractors...)
	w.jobs = append([]Job(nil), w.jobs...)
	w.nodes = append([]Node(nil), w.nodes...)
	w.receipts = append([]Receipt(nil), w.receipts...)
	return w
}

// NewWorld returns the world the manifest declares: three reefers on the apron,
// three slots plugged, three idle tractors, every lane open, and the run in its
// first round.
func NewWorld() World {
	return World{
		reefers:  Reefers(),
		slots:    Slots(),
		tractors: Tractors(),
		jobs:     Jobs(),
		nodes:    Nodes(),
		topology: NewTopology(),
		round:    1,
		phase:    PhaseRunning,
	}
}

// Round returns the host's round counter. The first round is 1, and BeginRound
// is the only thing that advances it, so the cutoff is compared against host
// ticks rather than against how much the world happened to change.
func (w World) Round() int { return w.round }

// WorldSeq returns the world sequence, which counts committed mutations.
func (w World) WorldSeq() uint64 { return w.worldSeq }

// CommandSeq returns the command sequence, which counts received commands.
func (w World) CommandSeq() uint64 { return w.commandSeq }

// Phase returns where the run has got to.
func (w World) Phase() Phase { return w.phase }

// Topology returns the lane graph.
func (w World) Topology() Topology { return w.topology }

// Revision is the world's revision as a string, which is the type pabt's
// StateRevision carries for a world.
func (w World) Revision() string { return "w" + strconv.FormatUint(w.worldSeq, 10) }

// Receipts returns the world's receipt ledger in the order it was appended. The
// returned slice is the world's own and must not be modified.
func (w World) Receipts() []Receipt { return w.receipts }

// Reefer returns a reefer record by ID.
func (w World) Reefer(id ReeferID) (Reefer, bool) {
	for _, r := range w.reefers {
		if r.ID == id {
			return r, true
		}
	}
	return Reefer{}, false
}

// Slot returns a slot record by ID.
func (w World) Slot(id SlotID) (Slot, bool) {
	for _, s := range w.slots {
		if s.ID == id {
			return s, true
		}
	}
	return Slot{}, false
}

// Tractor returns a tractor record by ID.
func (w World) Tractor(id TractorID) (Tractor, bool) {
	for _, t := range w.tractors {
		if t.ID == id {
			return t, true
		}
	}
	return Tractor{}, false
}

// Job returns a job record by ID.
func (w World) Job(id JobID) (Job, bool) {
	for _, j := range w.jobs {
		if j.ID == id {
			return j, true
		}
	}
	return Job{}, false
}

// Node returns a node record by ID.
func (w World) Node(id NodeID) (Node, bool) {
	for _, n := range w.nodes {
		if n.ID == id {
			return n, true
		}
	}
	return Node{}, false
}

// SlotAtNode returns the slot standing at a node. A reefer's position is a node,
// so this is how a delivery is checked without giving the reefer a second notion
// of which slot it is in.
func (w World) SlotAtNode(n NodeID) (Slot, bool) {
	for _, s := range w.slots {
		if s.Node == n {
			return s, true
		}
	}
	return Slot{}, false
}

// Delivered reports whether a reefer is standing in a slot that is plugged and
// rated for it. This is the world fact the plan's goal condition reads, and the
// reducer is the only thing that computes it.
func (w World) Delivered(id ReeferID) bool {
	r, ok := w.Reefer(id)
	if !ok || r.Custodian != "" {
		return false
	}
	s, ok := w.SlotAtNode(r.At)
	if !ok {
		return false
	}
	return s.Occupied == id && s.Accepts(r)
}

// Occupant returns the tractor standing at a node, which is how contention is
// decided and how a refusal names the winner.
func (w World) Occupant(n NodeID) (TractorID, bool) {
	for _, t := range w.tractors {
		if t.At == n {
			return t.ID, true
		}
	}
	return "", false
}

// CheckExpectation reports why a command was decided against a world this one has
// moved past, or ReasonNone when the command is still current.
//
// Only resource versions are a precondition. Expect.WorldSeq records what the
// producer saw and is not a guard: in a busy world the sequence advances between
// a plan pinning its frame and its command being reduced, so treating that as a
// mismatch would refuse nearly every command. What must still hold is the version
// of the specific thing the command acts on, because a placement aimed at a slot
// whose plug was pulled, or a crossing of a lane whose topology moved, is acting
// on a state that no longer exists.
func (w World) CheckExpectation(c Command) Reason {
	if c.Expect.WorldSeq > w.WorldSeq() {
		return ReasonFromTheFuture
	}
	switch c.Kind {
	case CmdPlace:
		s, ok := w.Slot(c.Slot)
		if !ok {
			return ReasonUnknownEntity
		}
		if c.Expect.SlotVersion != s.Version {
			return ReasonSlotMoved
		}
	case CmdMoveTo:
		if c.Expect.LaneVersion != w.topology.Version() {
			return ReasonLaneMoved
		}
	case CmdPickUp, CmdScanTag:
		r, ok := w.Reefer(c.Reefer)
		if !ok {
			return ReasonUnknownEntity
		}
		if c.Expect.TagVersion != r.TagVersion {
			return ReasonTagMoved
		}
	}
	return ReasonNone
}

// Route returns the lanes a tractor must cross to get from one node to another,
// and whether such a route exists over open lanes.
//
// A reefer destination is never a tractor destination: a tractor hands a box over
// from the bay and does not enter the slot. Refusing the route here as well as at
// MoveTo keeps the two from disagreeing, and the plan's action generator reads
// this one.
func (w World) Route(from, to NodeID) ([]LaneID, bool) {
	if n, ok := w.Node(to); ok && n.ReeferDestination {
		return nil, false
	}
	return w.topology.Route(from, to)
}

// StateKey renders the world's logical state and nothing else: the phase, the
// topology, every entity, and every job. It deliberately omits the command
// sequence and the receipt ledger, because a refused command is still counted in
// the command lane and still leaves no trace here. It is the fingerprint that
// answers "did this command change anything".
func (w World) StateKey() string {
	var b strings.Builder
	fmt.Fprintf(&b, "round=%d worldSeq=%d phase=%s topologyVersion=%d\n",
		w.Round(), w.worldSeq, w.phase, w.topology.version)
	w.writeState(&b)
	return b.String()
}

// Canonical returns a deterministic, sorted, human-readable rendering of the
// whole world including the command sequence and the receipt ledger. Two runs
// that issued the same commands in the same order produce the same string, which
// is what the deterministic-replay check compares.
func (w World) Canonical() string {
	var b strings.Builder
	fmt.Fprintf(&b, "round=%d worldSeq=%d commandSeq=%d phase=%s topologyVersion=%d\n",
		w.Round(), w.worldSeq, w.commandSeq, w.phase, w.topology.version)
	w.writeState(&b)
	for _, r := range w.receipts {
		fmt.Fprintf(&b, "receipt key=%s kind=%s seq=%d worldSeq=%d outcome=%s reason=%s holder=%q\n",
			r.IdempotencyKey, r.Kind, r.CommandSeq, r.WorldSeq, r.Outcome, r.Reason, r.Holder)
	}
	return b.String()
}

func (w World) writeState(b *strings.Builder) {
	if locked := w.topology.LockedLanes(); len(locked) > 0 {
		fmt.Fprintf(b, "lockedLanes=%s\n", joinLanes(locked))
	}
	for _, t := range w.tractors {
		fmt.Fprintf(b, "tractor %s at=%s job=%q hold=%q\n", t.ID, t.At, t.Job, t.Hold)
	}
	for _, r := range w.reefers {
		fmt.Fprintf(b, "reefer %s temp=%d at=%s custodian=%q tagReadable=%t tagVersion=%d\n",
			r.ID, r.TempC, r.At, r.Custodian, r.TagReadable, r.TagVersion)
	}
	for _, s := range w.slots {
		fmt.Fprintf(b, "slot %s ratedMax=%d plugged=%t occupied=%q version=%d\n",
			s.ID, s.RatedMaxC, s.Plugged, s.Occupied, s.Version)
	}
	for _, j := range w.jobs {
		fmt.Fprintf(b, "job %s reefer=%s claimedBy=%q done=%t\n", j.ID, j.Reefer, j.ClaimedBy, j.Done)
	}
}

// CommitLedger returns the applied receipts in a canonical order, for the
// deterministic digest the completion gate compares across runs.
func (w World) CommitLedger() string {
	applied := make([]string, 0, len(w.receipts))
	for _, r := range w.receipts {
		if r.Applied() {
			applied = append(applied, fmt.Sprintf("%s|%s|%d|%d",
				r.IdempotencyKey, r.Kind, r.CommandSeq, r.WorldSeq))
		}
	}
	sort.Strings(applied)
	return strings.Join(applied, "\n")
}

func joinLanes(l []LaneID) string {
	parts := make([]string, len(l))
	for i, id := range l {
		parts[i] = string(id)
	}
	return strings.Join(parts, ",")
}
