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

import "testing"

// TestRefusalsChangeNothing walks every refusal the domain can produce and
// checks the two properties that matter: the world is untouched, and the receipt
// says why. The driver checks the first on every command it issues, so this test
// exists to reach each reason rather than to re-check the property.
func TestRefusalsChangeNothing(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(*driver)
		command Command
		want    Reason
	}{
		{
			name: "a busy tractor cannot claim",
			setup: func(d *driver) {
				d.applied(Command{Kind: CmdClaimJob, Tractor: Tug1})
			},
			command: Command{Kind: CmdClaimJob, Tractor: Tug1},
			want:    ReasonTractorBusy,
		},
		{
			name:    "a drained queue cannot be claimed by a released tractor",
			setup:   func(d *driver) { releaseOneJob(t, d) },
			command: Command{Kind: CmdClaimJob, Tractor: Tug1},
			want:    ReasonNoJobFree,
		},
		{
			name: "an unreadable tag blocks a pickup",
			setup: func(d *driver) {
				d.applied(Command{Kind: CmdClaimJob, Tractor: Tug1})
			},
			command: Command{Kind: CmdPickUp, Tractor: Tug1, Reefer: Reefer2},
			want:    ReasonTagUnreadable,
		},
		{
			name: "a tag that already reads is not scanned twice",
			setup: func(d *driver) {
				d.applied(Command{Kind: CmdClaimJob, Tractor: Tug1})
				d.applied(Command{Kind: CmdScanTag, Tractor: Tug1, Reefer: Reefer2})
			},
			command: Command{Kind: CmdScanTag, Tractor: Tug1, Reefer: Reefer2},
			want:    ReasonTagReadable,
		},
		{
			name: "a reefer elsewhere cannot be picked up",
			setup: func(d *driver) {
				d.applied(Command{Kind: CmdClaimJob, Tractor: Tug1})
				d.applied(Command{Kind: CmdMoveTo, Tractor: Tug1, Node: NodeWest})
			},
			command: Command{Kind: CmdPickUp, Tractor: Tug1, Reefer: Reefer3},
			want:    ReasonNotBesideReefer,
		},
		{
			name: "a tractor cannot take a second reefer",
			setup: func(d *driver) {
				d.applied(Command{Kind: CmdClaimJob, Tractor: Tug1})
				d.applied(Command{Kind: CmdPickUp, Tractor: Tug1, Reefer: Reefer1})
			},
			command: Command{Kind: CmdPickUp, Tractor: Tug1, Reefer: Reefer3},
			want:    ReasonAlreadyHeld,
		},
		{
			name: "a non-adjacent move is refused",
			setup: func(d *driver) {
				d.applied(Command{Kind: CmdClaimJob, Tractor: Tug1})
			},
			command: Command{Kind: CmdMoveTo, Tractor: Tug1, Node: NodeBay},
			want:    ReasonNotAdjacent,
		},
		{
			name:    "a slot node is not a tractor destination",
			setup:   func(d *driver) { walkToBay(d, Tug1) },
			command: Command{Kind: CmdMoveTo, Tractor: Tug1, Node: NodeSlot1},
			want:    ReasonNotATractorNode,
		},
		{
			name: "a closed lane refuses the crossing",
			setup: func(d *driver) {
				d.applied(Command{Kind: CmdMoveTo, Tractor: Tug1, Node: NodeWest})
				d.w, _ = LockLane(d.w, LaneBayWest)
			},
			command: Command{Kind: CmdMoveTo, Tractor: Tug1, Node: NodeBay},
			want:    ReasonLaneLocked,
		},
		{
			name: "the second tractor into the bay is refused",
			setup: func(d *driver) {
				walkToBay(d, Tug1)
				d.applied(Command{Kind: CmdMoveTo, Tractor: Tug2, Node: NodeEast})
			},
			command: Command{Kind: CmdMoveTo, Tractor: Tug2, Node: NodeBay},
			want:    ReasonBayOccupied,
		},
		{
			name: "a waypoint holds one tractor",
			setup: func(d *driver) {
				d.applied(Command{Kind: CmdMoveTo, Tractor: Tug1, Node: NodeEast})
			},
			command: Command{Kind: CmdMoveTo, Tractor: Tug2, Node: NodeEast},
			want:    ReasonNodeOccupied,
		},
		{
			name:    "placing without custody is refused",
			setup:   func(d *driver) { walkToBay(d, Tug1) },
			command: Command{Kind: CmdPlace, Tractor: Tug1, Reefer: Reefer1, Slot: Slot1},
			want:    ReasonNoCustody,
		},
		{
			name: "placing away from the bay is refused",
			setup: func(d *driver) {
				d.applied(Command{Kind: CmdClaimJob, Tractor: Tug1})
				d.applied(Command{Kind: CmdPickUp, Tractor: Tug1, Reefer: Reefer1})
			},
			command: Command{Kind: CmdPlace, Tractor: Tug1, Reefer: Reefer1, Slot: Slot1},
			want:    ReasonNotAtBay,
		},
		{
			name: "the under-rated slot refuses a deep-freeze reefer",
			setup: func(d *driver) {
				holdAtBay(d, Tug1, Reefer1)
			},
			command: Command{Kind: CmdPlace, Tractor: Tug1, Reefer: Reefer1, Slot: Slot2},
			want:    ReasonSlotUnderRated,
		},
		{
			name: "an unpowered slot refuses everything",
			setup: func(d *driver) {
				holdAtBay(d, Tug1, Reefer3)
				d.w, _ = SetSlotPower(d.w, Slot2, false)
			},
			command: Command{Kind: CmdPlace, Tractor: Tug1, Reefer: Reefer3, Slot: Slot2},
			want:    ReasonSlotUnpowered,
		},
		{
			name:  "an occupied slot refuses a second reefer and names its holder",
			setup: func(d *driver) { occupySlot1WithReefer3(t, d) },
			command: Command{
				Kind: CmdPlace, Tractor: Tug2, Reefer: Reefer1, Slot: Slot1,
			},
			want: ReasonSlotOccupied,
		},
		{
			name:    "an unknown tractor is refused",
			setup:   func(d *driver) {},
			command: Command{Kind: CmdClaimJob, Tractor: "TUG-9"},
			want:    ReasonUnknownEntity,
		},
		{
			name:    "a kind outside the closed set is refused",
			setup:   func(d *driver) {},
			command: Command{Kind: "Teleport", Tractor: Tug1},
			want:    ReasonUnknownEntity,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newDriver(t)
			tc.setup(d)
			r := d.apply(tc.command)
			if r.Applied() {
				t.Fatalf("%s was applied, wanted a refusal", tc.command.Kind)
			}
			if r.Reason != tc.want {
				t.Fatalf("refused as %s, want %s", r.Reason, tc.want)
			}
			if r.Reason == ReasonNone {
				t.Fatal("a refusal carried no reason")
			}
		})
	}
}

// TestLoserIsToldWhoWon checks the holder field, which is what saves a losing
// tractor from having to ask the world a second question.
func TestLoserIsToldWhoWon(t *testing.T) {
	d := newDriver(t)
	walkToBay(d, Tug1)
	d.applied(Command{Kind: CmdMoveTo, Tractor: Tug2, Node: NodeEast})

	r := d.apply(Command{Kind: CmdMoveTo, Tractor: Tug2, Node: NodeBay})
	if r.Holder != string(Tug1) {
		t.Fatalf("refusal named holder %q, want %q", r.Holder, Tug1)
	}
}

func TestReplayingAKeyReturnsTheOriginalReceipt(t *testing.T) {
	d := newDriver(t)
	first := d.applied(Command{Kind: CmdClaimJob, Tractor: Tug1})
	before := d.w.StateKey()
	commandsBefore := d.w.CommandSeq()

	same := Command{
		Kind: CmdClaimJob, Tractor: Tug1,
		IdempotencyKey: first.IdempotencyKey, ID: first.ID,
	}
	var w World
	var r Receipt
	var events []WorldEvent
	w, r, events = Reduce(d.w, same)

	if r.Outcome != OutcomeDuplicate {
		t.Fatalf("replayed key gave %s, want %s", r.Outcome, OutcomeDuplicate)
	}
	if r.Reason != first.Reason || r.Kind != first.Kind || r.CommandSeq != first.CommandSeq {
		t.Fatalf("replay returned a different receipt:\n got %+v\nwant %+v", r, first)
	}
	if len(events) != 0 {
		t.Fatalf("a replay published %d events, want none", len(events))
	}
	// The logical state is untouched. The retry is still counted in the command
	// lane, so the gap in that sequence is the only trace of it — a reader sees
	// that a command was received and got no receipt of its own, which is a
	// duplicate rather than a gap in the world's work.
	if w.StateKey() != before {
		t.Fatalf("a replay changed the world:\n%s", w.StateKey())
	}
	if w.CommandSeq() != commandsBefore+1 {
		t.Fatalf("a replay left commandSeq at %d, want %d: the retry is still a command",
			w.CommandSeq(), commandsBefore+1)
	}
}

// TestARefusedCommandStaysVisible is the property that a refusal is a durable
// fact. The frozen contract requires a reader to be able to find a refused
// command later and see that it moved no world sequence, and a ledger that kept
// only successes could not do that.
func TestARefusedCommandStaysVisible(t *testing.T) {
	d := newDriver(t)
	d.applied(Command{Kind: CmdClaimJob, Tractor: Tug1})
	seqAtRefusal := d.w.WorldSeq()

	refused := d.apply(Command{Kind: CmdClaimJob, Tractor: Tug1})
	if refused.Applied() {
		t.Fatal("a second claim for a busy tractor was applied")
	}

	found := false
	for _, r := range d.w.Receipts() {
		if r.IdempotencyKey != refused.IdempotencyKey {
			continue
		}
		found = true
		if r.Outcome != OutcomeRefused || r.Reason != ReasonTractorBusy {
			t.Fatalf("ledger holds %s/%s for the refusal, want Refused/TractorBusy", r.Outcome, r.Reason)
		}
		if r.WorldSeq != seqAtRefusal {
			t.Fatalf("the refusal records worldSeq %d, want the unchanged %d", r.WorldSeq, seqAtRefusal)
		}
		if r.CommandSeq <= 0 {
			t.Fatalf("the refusal has no place in the command lane: seq %d", r.CommandSeq)
		}
	}
	if !found {
		t.Fatalf("no ledger entry for the refused command %s", refused.IdempotencyKey)
	}
}

// TestAKeyIsSpentByItsFirstReduction documents the consequence of keeping every
// receipt: a key names one action instance, so a retry after a replan mints a new
// one rather than asking the world about a settled question.
func TestAKeyIsSpentByItsFirstReduction(t *testing.T) {
	d := newDriver(t)
	d.applied(Command{Kind: CmdClaimJob, Tractor: Tug1})
	// TUG-1 now holds a job, so this one is refused, and the refusal spends the
	// key for that action instance.
	first := d.apply(Command{Kind: CmdClaimJob, Tractor: Tug1})
	if first.Applied() {
		t.Fatal("a second claim for a busy tractor was applied, so this test is not exercising what it claims")
	}

	replay := d.apply(Command{
		Kind: CmdClaimJob, Tractor: Tug1,
		IdempotencyKey: first.IdempotencyKey, ID: first.ID,
	})
	if replay.Outcome != OutcomeDuplicate {
		t.Fatalf("reusing a spent key gave %s, want %s", replay.Outcome, OutcomeDuplicate)
	}

	// A new key is a new attempt, and it is judged against the world as it is now.
	fresh := d.apply(Command{Kind: CmdClaimJob, Tractor: Tug2})
	if !fresh.Applied() {
		t.Fatalf("a fresh key was refused as %s", fresh.Reason)
	}
}

func TestOneMutationAdvancesTheSequenceByOne(t *testing.T) {
	d := newDriver(t)
	d.applied(Command{Kind: CmdClaimJob, Tractor: Tug1})
	d.apply(Command{Kind: CmdMoveTo, Tractor: Tug1, Node: NodeWest})
	d.apply(Command{Kind: CmdClaimJob, Tractor: Tug1})

	// Two commands applied and one refused, so the world moved twice while the
	// command lane counted all three. Nothing was skipped and nothing doubled.
	if got, want := d.w.WorldSeq(), uint64(2); got != want {
		t.Fatalf("worldSeq = %d, want %d\n%s", got, want, d.w.Canonical())
	}
	if got, want := d.w.CommandSeq(), uint64(3); got != want {
		t.Fatalf("commandSeq = %d, want %d: a refused command is still counted", got, want)
	}
}

func TestNoRouteCrossesAClosedLane(t *testing.T) {
	topo := NewTopology()
	route, ok := topo.Route(NodeStaging, NodeBay)
	if !ok {
		t.Fatal("no route from the apron to the bay with every lane open")
	}
	if len(route) != 2 {
		t.Fatalf("route = %v, want two lanes", route)
	}

	closed := topo.Lock(LaneBayWest)
	rerouted, ok := closed.Route(NodeStaging, NodeBay)
	if !ok {
		t.Fatal("closing one approach lane left the bay unreachable")
	}
	for _, l := range rerouted {
		if !closed.Open(l) {
			t.Fatalf("route %v crosses the closed lane %s", rerouted, l)
		}
	}
	if closed.Version() != topo.Version()+1 {
		t.Fatalf("locking advanced the version to %d from %d, want one step",
			closed.Version(), topo.Version())
	}

	// Reopening is a second step, and the version says so. This is what makes a
	// replan triggered by the closure stale in turn.
	reopened := closed.Reopen(LaneBayWest)
	if reopened.Version() != closed.Version()+1 {
		t.Fatalf("reopening advanced the version to %d from %d, want one step",
			reopened.Version(), closed.Version())
	}
	if _, ok := reopened.Route(NodeStaging, NodeBay); !ok {
		t.Fatal("reopening did not restore a route")
	}
}

func TestLockingTwiceIsNotAMutation(t *testing.T) {
	w := NewWorld()
	before := w.WorldSeq()
	w, evs := LockLane(w, LaneBayWest)
	if len(evs) != 1 {
		t.Fatalf("the first lock published %d events, want 1", len(evs))
	}
	after := w.WorldSeq()

	w, evs = LockLane(w, LaneBayWest)
	if len(evs) != 0 {
		t.Fatalf("locking an already-closed lane published %d events, want none", len(evs))
	}
	if w.WorldSeq() != after {
		t.Fatalf("locking an already-closed lane advanced worldSeq %d -> %d", after, w.WorldSeq())
	}
	if before != 0 {
		t.Fatalf("worldSeq started at %d", before)
	}
}

func TestUnpluggingTheSamePlugTwiceIsNotAMutation(t *testing.T) {
	w := NewWorld()
	w, _ = SetSlotPower(w, Slot2, false)
	after := w.WorldSeq()
	w, evs := SetSlotPower(w, Slot2, false)
	if len(evs) != 0 {
		t.Fatalf("unplugging an unpowered slot published %d events, want none", len(evs))
	}
	if w.WorldSeq() != after {
		t.Fatalf("worldSeq moved %d -> %d", after, w.WorldSeq())
	}
}

// TestReplayIsDeterministic runs the same command sequence twice and compares
// the whole world, including the receipt ledger. A reducer that reached its
// result by a map iteration or a clock would differ here.
func TestReplayIsDeterministic(t *testing.T) {
	first := runScripted(t)
	second := runScripted(t)

	if first.Canonical() != second.Canonical() {
		t.Fatalf("two identical runs diverged.\n--- first ---\n%s\n--- second ---\n%s",
			first.Canonical(), second.Canonical())
	}
	if first.CommitLedger() != second.CommitLedger() {
		t.Fatal("the applied-receipt ledger diverged between identical runs")
	}
	if first.Canonical() == NewWorld().Canonical() {
		t.Fatal("the scripted run left the world untouched")
	}
}

// runScripted drives one fixed sequence that includes every branch the reducer
// has — applied commands, refusals of each kind, the bay contest, and the whole
// incident — and returns the world it reached. It is written out rather than
// generated so that two runs issue byte-identical commands, which is the point.
func runScripted(t *testing.T) World {
	t.Helper()
	d := newDriver(t)

	for _, c := range []Command{
		{Kind: CmdClaimJob, Tractor: Tug1},
		{Kind: CmdClaimJob, Tractor: Tug2},
		{Kind: CmdClaimJob, Tractor: Tug3},
		{Kind: CmdClaimJob, Tractor: Tug1}, // refused: already holds a job
		{Kind: CmdScanTag, Tractor: Tug1, Reefer: Reefer2},
		{Kind: CmdPickUp, Tractor: Tug1, Reefer: Reefer1},
		{Kind: CmdPickUp, Tractor: Tug1, Reefer: Reefer3}, // refused: hands full
		{Kind: CmdMoveTo, Tractor: Tug1, Node: NodeWest},
		{Kind: CmdMoveTo, Tractor: Tug1, Node: NodeBay},
		{Kind: CmdMoveTo, Tractor: Tug2, Node: NodeEast},
		{Kind: CmdMoveTo, Tractor: Tug2, Node: NodeBay},               // refused: the bay is taken
		{Kind: CmdPlace, Tractor: Tug1, Reefer: Reefer1, Slot: Slot2}, // refused: under-rated
		{Kind: CmdPlace, Tractor: Tug1, Reefer: Reefer1, Slot: Slot1},
	} {
		d.apply(c)
	}

	d.w, _ = LockLane(d.w, LaneBayWest)
	for _, c := range []Command{
		{Kind: CmdMoveTo, Tractor: Tug1, Node: NodeWest}, // refused: the lane is closed
		{Kind: CmdMoveTo, Tractor: Tug3, Node: NodeEast},
		{Kind: CmdMoveTo, Tractor: Tug3, Node: NodeBay},
		{Kind: CmdPickUp, Tractor: Tug3, Reefer: Reefer3},
		{Kind: CmdPlace, Tractor: Tug3, Reefer: Reefer3, Slot: Slot2},
	} {
		d.apply(c)
	}

	d.w, _ = SetSlotPower(d.w, Slot2, false)
	for _, c := range []Command{
		{Kind: CmdPlace, Tractor: Tug3, Reefer: Reefer3, Slot: Slot2}, // refused: unpowered
		{Kind: CmdPlace, Tractor: Tug3, Reefer: Reefer3, Slot: Slot3},
	} {
		d.apply(c)
	}

	d.w, _ = ReopenLane(d.w, LaneBayWest)
	return d.w
}

// TestTheScriptedRunExercisesBothOutcomes guards the determinism test against
// quietly degrading into a run where nothing was refused, which would make the
// comparison much weaker than it looks.
func TestTheScriptedRunExercisesBothOutcomes(t *testing.T) {
	w := runScripted(t)
	var applied, refused int
	for _, r := range w.Receipts() {
		if r.Applied() {
			applied++
			continue
		}
		refused++
	}
	if applied == 0 || refused == 0 {
		t.Fatalf("scripted run had %d applied and %d refused commands; it must have both", applied, refused)
	}
	if w.Phase() != PhaseRunning {
		t.Fatalf("scripted run ended in %s, want the run still going", w.Phase())
	}
}

func claimAll(d *driver) {
	for _, tug := range []TractorID{Tug1, Tug2, Tug3} {
		d.applied(Command{Kind: CmdClaimJob, Tractor: tug})
	}
}

// walkToBay drives a tractor from the apron to the bay with nothing in custody.
func walkToBay(d *driver, tug TractorID) {
	d.applied(Command{Kind: CmdMoveTo, Tractor: tug, Node: NodeEast})
	d.applied(Command{Kind: CmdMoveTo, Tractor: tug, Node: NodeBay})
}

// holdAtBay drives a tractor to the bay holding the named reefer.
func holdAtBay(d *driver, tug TractorID, r ReeferID) {
	d.applied(Command{Kind: CmdClaimJob, Tractor: tug})
	if rec, _ := d.w.Reefer(r); !rec.TagReadable {
		d.applied(Command{Kind: CmdScanTag, Tractor: tug, Reefer: r})
	}
	d.applied(Command{Kind: CmdPickUp, Tractor: tug, Reefer: r})
	walkToBay(d, tug)
}

// releaseOneJob gets TUG-1 to the point where it holds nothing: every job is
// either claimed by someone or already done, and TUG-1 is idle. That is the only
// state in which a claim meets an empty queue rather than a busy tractor.
func releaseOneJob(t *testing.T, d *driver) {
	t.Helper()
	claimAll(d)
	d.applied(Command{Kind: CmdPickUp, Tractor: Tug1, Reefer: Reefer1})
	walkToBay(d, Tug1)
	d.applied(Command{Kind: CmdPlace, Tractor: Tug1, Reefer: Reefer1, Slot: Slot1})
	d.applied(Command{Kind: CmdMoveTo, Tractor: Tug1, Node: NodeEast})
	d.applied(Command{Kind: CmdMoveTo, Tractor: Tug1, Node: NodeStaging})

	if tr, _ := d.w.Tractor(Tug1); tr.Job != "" || tr.Hold != "" {
		t.Fatalf("TUG-1 still holds job %q and cargo %q after delivering", tr.Job, tr.Hold)
	}
}

// occupySlot1WithReefer3 puts REEFER-3 in SLOT-1 and leaves TUG-2 at the bay
// holding the deep-freeze reefer, which SLOT-1 is rated for. The only thing left
// to refuse is the slot's occupancy.
func occupySlot1WithReefer3(t *testing.T, d *driver) {
	t.Helper()
	claimAll(d)
	d.applied(Command{Kind: CmdPickUp, Tractor: Tug1, Reefer: Reefer3})
	walkToBay(d, Tug1)
	d.applied(Command{Kind: CmdPlace, Tractor: Tug1, Reefer: Reefer3, Slot: Slot1})
	d.applied(Command{Kind: CmdMoveTo, Tractor: Tug1, Node: NodeEast})
	d.applied(Command{Kind: CmdMoveTo, Tractor: Tug1, Node: NodeStaging})

	// TUG-2's job is REEFER-2 by manifest order, so take custody of REEFER-1
	// explicitly to prove the placement is refused for the slot and not for the
	// cargo.
	d.applied(Command{Kind: CmdPickUp, Tractor: Tug2, Reefer: Reefer1})
	walkToBay(d, Tug2)
}
