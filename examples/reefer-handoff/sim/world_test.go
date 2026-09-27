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

// TestReduceDoesNotTouchTheWorldItWasGiven is the value-semantics guarantee the
// plan runner's pinned frame and the debugger's stored records both rest on. The
// transitions write through the world's slices by index, so without a clone at
// the top of Reduce the caller's world would change underneath a reader holding
// it, and every earlier record would show the newest state.
func TestReduceDoesNotTouchTheWorldItWasGiven(t *testing.T) {
	before := NewWorld()
	beforeKey := before.StateKey()
	beforeSeq := before.WorldSeq()
	beforeTug, _ := before.Tractor(Tug1)

	after, receipt, _ := Reduce(before, Command{
		ID: "c1", IdempotencyKey: "k1", Kind: CmdClaimJob, Tractor: Tug1,
	})
	if !receipt.Applied() {
		t.Fatalf("the claim was refused as %s", receipt.Reason)
	}
	if before.StateKey() != beforeKey {
		t.Fatalf("Reduce changed the world it was given.\nbefore:\n%s\nafter:\n%s",
			beforeKey, before.StateKey())
	}
	if before.WorldSeq() != beforeSeq {
		t.Fatalf("Reduce advanced the argument's worldSeq %d -> %d", beforeSeq, before.WorldSeq())
	}
	afterTug, _ := after.Tractor(Tug1)
	if afterTug.Job == "" {
		t.Fatal("the returned world did not record the claim either")
	}
	if beforeTug.Job != "" {
		t.Fatalf("the argument's TUG-1 now holds %q", beforeTug.Job)
	}
}

// TestBeginRoundDoesNotTouchTheWorldItWasGiven is the same guarantee for the
// host's round boundary, which the real-time driver calls every tick.
func TestBeginRoundDoesNotTouchTheWorldItWasGiven(t *testing.T) {
	before := NewWorld()
	beforeKey := before.StateKey()
	after, evs := BeginRound(before)
	if len(evs) != 0 {
		t.Fatalf("the first round boundary published %d events, want none", len(evs))
	}
	if before.Round() != 1 {
		t.Fatalf("BeginRound advanced the argument to round %d", before.Round())
	}
	if after.Round() != 2 {
		t.Fatalf("returned round = %d, want 2", after.Round())
	}
	if before.StateKey() != beforeKey {
		t.Fatal("BeginRound changed the world it was given")
	}
}

// TestCutoffIsReachableAndNamesTheShortfall drives the run past the vessel's
// round with cargo still on the apron. The failure condition is half of what the
// contract calls success, and it has to be a state a run can actually reach.
func TestCutoffIsReachableAndNamesTheShortfall(t *testing.T) {
	d := newDriver(t)
	d.applied(Command{Kind: CmdClaimJob, Tractor: Tug1})
	d.applied(Command{Kind: CmdPickUp, Tractor: Tug1, Reefer: Reefer1})
	walkToBay(d, Tug1)
	d.applied(Command{Kind: CmdPlace, Tractor: Tug1, Reefer: Reefer1, Slot: Slot1})

	if d.w.Phase() != PhaseRunning {
		t.Fatalf("one delivery of three ended the run in %s", d.w.Phase())
	}

	var sawCutoff bool
	for r := d.w.Round(); r <= CutoffRound+1; r++ {
		var evs []WorldEvent
		d.w, evs = BeginRound(d.w)
		for _, e := range evs {
			if e.Kind == EvCutoffReached {
				sawCutoff = true
			}
		}
	}

	if !sawCutoff {
		t.Fatal("no EvCutoffReached was published as the vessel's round passed")
	}
	if d.w.Phase() != PhaseCutoff {
		t.Fatalf("phase = %s at round %d, want %s", d.w.Phase(), d.w.Round(), PhaseCutoff)
	}
	short := d.w.Shortfall()
	if len(short) != 2 || short[0] != Reefer2 || short[1] != Reefer3 {
		t.Fatalf("shortfall = %v, want [REEFER-2 REEFER-3]", short)
	}

	// The terminal phase refuses everything, and says which phase it was.
	r := d.apply(Command{Kind: CmdClaimJob, Tractor: Tug2})
	if r.Applied() || r.Reason != ReasonWorldCutoff {
		t.Fatalf("a command after cutoff gave %s/%s, want Refused/WorldCutoff", r.Outcome, r.Reason)
	}
}

// TestARoundIsNotAMutation pins the distinction the cutoff depends on: a round
// boundary moves the host's clock and nothing else, so it must not consume a
// world sequence.
func TestARoundIsNotAMutation(t *testing.T) {
	w := NewWorld()
	seq := w.WorldSeq()
	w, evs := BeginRound(w)
	if len(evs) != 0 {
		t.Fatalf("a round boundary with nothing to report published %d events", len(evs))
	}
	if w.WorldSeq() != seq {
		t.Fatalf("a round boundary moved worldSeq %d -> %d", seq, w.WorldSeq())
	}
	if w.Round() != 2 {
		t.Fatalf("round = %d after one boundary, want 2", w.Round())
	}
}

// TestTheScriptedIncidentMatchesTheContract pins the three rounds the frozen
// contract makes load-bearing. The wind closure is what forces a re-route, and
// its reopening is what makes that re-route stale in turn, so a transcription
// slip here would quietly remove the incident.
func TestTheScriptedIncidentMatchesTheContract(t *testing.T) {
	want := map[int]int{20: 1, 40: 1, 55: 1}
	for round := 1; round <= CutoffRound; round++ {
		got := len(Scripted(round))
		if want[round] != got {
			t.Fatalf("round %d has %d scripted incidents, want %d", round, got, want[round])
		}
	}

	// The three changes are the ones the contract names, in order.
	w := NewWorld()
	var kinds []WorldEventKind
	for _, in := range []Incident{
		Scripted(20)[0], Scripted(40)[0], Scripted(55)[0],
	} {
		var evs []WorldEvent
		w, evs = in.Apply(w)
		if len(evs) != 1 {
			t.Fatalf("round %d published %d events, want 1", in.Round, len(evs))
		}
		kinds = append(kinds, evs[0].Kind)
		assertInvariants(t, w)
	}
	want3 := []WorldEventKind{EvLaneLocked, EvSlotPowerChanged, EvLaneReopened}
	for i, k := range kinds {
		if k != want3[i] {
			t.Fatalf("incident %d published %s, want %s", i, k, want3[i])
		}
	}
	if s, _ := w.Slot(Slot2); s.Plugged {
		t.Fatal("SLOT-2 is still plugged after the power desk acted")
	}
	if w.Topology().Version() != 2 {
		t.Fatalf("topology version = %d after a lock and a reopen, want 2", w.Topology().Version())
	}
}

// TestExogenousChangesAdvanceTheSequenceByOne is the sequence discipline for the
// half of the world no tractor commands. Each case prepares whatever its change
// needs, then measures the change itself, so no case has to skip or assert
// inside another.
func TestExogenousChangesAdvanceTheSequenceByOne(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(World) World
		apply   func(World) (World, []WorldEvent)
	}{
		{
			name:  "lock a lane",
			apply: func(w World) (World, []WorldEvent) { return LockLane(w, LaneBayWest) },
		},
		{
			name:    "reopen a lane",
			prepare: func(w World) World { w, _ = LockLane(w, LaneBayWest); return w },
			apply:   func(w World) (World, []WorldEvent) { return ReopenLane(w, LaneBayWest) },
		},
		{
			name:  "unplug a slot",
			apply: func(w World) (World, []WorldEvent) { return SetSlotPower(w, Slot2, false) },
		},
		{
			name:    "replug a slot",
			prepare: func(w World) World { w, _ = SetSlotPower(w, Slot2, false); return w },
			apply:   func(w World) (World, []WorldEvent) { return SetSlotPower(w, Slot2, true) },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := NewWorld()
			if tc.prepare != nil {
				w = tc.prepare(w)
			}
			before := w.WorldSeq()
			next, evs := tc.apply(w)
			if next.WorldSeq() != before+1 {
				t.Fatalf("worldSeq %d -> %d, want one step", before, next.WorldSeq())
			}
			if len(evs) != 1 {
				t.Fatalf("published %d events, want 1", len(evs))
			}
			if evs[0].WorldSeq != next.WorldSeq() {
				t.Fatalf("the event carries worldSeq %d but the world is at %d",
					evs[0].WorldSeq, next.WorldSeq())
			}
			if evs[0].WorldRevision != next.Revision() {
				t.Fatalf("the event carries revision %q but the world is at %q",
					evs[0].WorldRevision, next.Revision())
			}
			assertInvariants(t, next)
		})
	}
}

// TestARouteNeverEndsInASlot keeps the routing helper and the move command from
// disagreeing about whether a tractor may stand in a slot.
func TestARouteNeverEndsInASlot(t *testing.T) {
	w := NewWorld()
	if _, ok := w.Route(NodeStaging, NodeSlot1); ok {
		t.Fatal("the world offered a tractor route into SLOT-1")
	}
	if _, ok := w.Topology().Route(NodeStaging, NodeSlot1); !ok {
		t.Fatal("the lane graph found no path to SLOT-1, so the two disagree about the lanes")
	}
	if _, ok := w.Route(NodeStaging, NodeBay); !ok {
		t.Fatal("no tractor route from the apron to the bay")
	}
}

// TestAPoweredSlotNeverHoldsCargoItCannotHold is the standing half of the
// placement rule: the other half is checked at the moment of placement, because
// the power desk may legitimately take a plug out afterwards.
func TestAPoweredSlotNeverHoldsCargoItCannotHold(t *testing.T) {
	d := newDriver(t)
	claimAll(d)
	d.applied(Command{Kind: CmdPickUp, Tractor: Tug1, Reefer: Reefer1})
	walkToBay(d, Tug1)
	d.applied(Command{Kind: CmdPlace, Tractor: Tug1, Reefer: Reefer1, Slot: Slot1})

	// Taking the plug out afterwards is legal and does not trip the invariant.
	d.w, _ = SetSlotPower(d.w, Slot1, false)
	assertInvariants(t, d.w)
	if d.w.Delivered(Reefer1) {
		t.Fatal("a reefer in an unpowered slot is still reported as delivered")
	}
}

// TestCompletionIsJudgedFromThePhysicalState is the case that separates the
// physical predicate from the job latch. TUG-1 delivers REEFER-1, the power desk
// takes the plug out of the slot it is standing in, and the run then finishes the
// other two. With a latch, every job reads as done and the run would announce
// Complete while a reefer sat in a dead slot.
func TestCompletionIsJudgedFromThePhysicalState(t *testing.T) {
	d := newDriver(t)
	claimAll(d)

	d.applied(Command{Kind: CmdPickUp, Tractor: Tug1, Reefer: Reefer1})
	walkToBay(d, Tug1)
	d.applied(Command{Kind: CmdPlace, Tractor: Tug1, Reefer: Reefer1, Slot: Slot1})
	d.applied(Command{Kind: CmdMoveTo, Tractor: Tug1, Node: NodeEast})
	d.applied(Command{Kind: CmdMoveTo, Tractor: Tug1, Node: NodeStaging})
	d.w, _ = SetSlotPower(d.w, Slot1, false)

	job, _ := d.w.Job(JobID(Reefer1))
	if !job.Done {
		t.Fatal("REEFER-1's job is not latched done, so this test is not exercising what it claims")
	}
	if d.w.Delivered(Reefer1) {
		t.Fatal("REEFER-1 is still reported as delivered after its slot lost power")
	}

	// Finish the other two. The run must not complete with a reefer in a dead slot.
	d.applied(Command{Kind: CmdScanTag, Tractor: Tug2, Reefer: Reefer2})
	d.applied(Command{Kind: CmdPickUp, Tractor: Tug2, Reefer: Reefer2})
	walkToBay(d, Tug2)
	d.applied(Command{Kind: CmdPlace, Tractor: Tug2, Reefer: Reefer2, Slot: Slot2})
	d.applied(Command{Kind: CmdMoveTo, Tractor: Tug2, Node: NodeEast})
	d.applied(Command{Kind: CmdMoveTo, Tractor: Tug2, Node: NodeStaging})

	d.applied(Command{Kind: CmdPickUp, Tractor: Tug3, Reefer: Reefer3})
	walkToBay(d, Tug3)
	d.applied(Command{Kind: CmdPlace, Tractor: Tug3, Reefer: Reefer3, Slot: Slot3})

	if d.w.Phase() == PhaseComplete {
		t.Fatalf("the run completed with %s in an unpowered slot; shortfall %v",
			Reefer1, d.w.Shortfall())
	}
	if got := d.w.Shortfall(); len(got) != 1 || got[0] != Reefer1 {
		t.Fatalf("shortfall = %v, want [%s]", got, Reefer1)
	}
}

// TestLockingDoesNotDisturbAnEarlierTopology checks the value guarantee on the
// lane graph. Appending to a slice whose capacity is already large enough writes
// through the backing array an earlier Lock result is still holding, so a
// topology taken after three locks would start reporting a fourth.
func TestLockingDoesNotDisturbAnEarlierTopology(t *testing.T) {
	afterOne := NewTopology().Lock(LaneWest)
	afterTwo := afterOne.Lock(LaneEast)
	afterThree := afterTwo.Lock(LaneBayWest)

	// The fourth lock is the one that reuses an existing array, so the topology
	// that shares it is the one at risk. All three are checked because a fix that
	// only protected the first would still be wrong.
	for _, tc := range []struct {
		what string
		got  Topology
		want int
	}{
		{"after one lock", afterOne, 1},
		{"after two locks", afterTwo, 2},
		{"after three locks", afterThree, 3},
	} {
		if n := len(tc.got.LockedLanes()); n != tc.want {
			t.Fatalf("the topology %s reports %d closed lanes, want %d", tc.what, n, tc.want)
		}
	}

	fourth := afterThree.Lock(LaneBayEast)
	if got, want := len(fourth.LockedLanes()), 4; got != want {
		t.Fatalf("the topology after four locks reports %d closed lanes, want %d", got, want)
	}

	// The corruption is in the contents, not the count: a permutation leaves the
	// slice just as long. So the earlier topology is compared lane by lane.
	want := []LaneID{LaneBayEast, LaneBayWest, LaneEast, LaneWest}
	if got := fourth.LockedLanes(); !sameLanes(got, want) {
		t.Fatalf("the topology after four locks is %v, want %v", got, want)
	}
	wantThree := []LaneID{LaneBayWest, LaneEast, LaneWest}
	if got := afterThree.LockedLanes(); !sameLanes(got, wantThree) {
		t.Fatalf("locking a fourth lane rewrote the topology from three locks to %v, want %v",
			got, wantThree)
	}
	wantTwo := []LaneID{LaneEast, LaneWest}
	if got := afterTwo.LockedLanes(); !sameLanes(got, wantTwo) {
		t.Fatalf("later locks rewrote the topology from two locks to %v, want %v", got, wantTwo)
	}
}

func sameLanes(got, want []LaneID) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
