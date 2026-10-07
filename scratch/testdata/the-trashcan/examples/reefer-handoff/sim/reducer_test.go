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

func TestManifestIsAsDeclared(t *testing.T) {
	w := NewWorld()
	if got, want := len(w.reefers), 3; got != want {
		t.Fatalf("reefers = %d, want %d", got, want)
	}
	if got, want := len(w.slots), 3; got != want {
		t.Fatalf("slots = %d, want %d", got, want)
	}
	if got, want := len(w.tractors), 3; got != want {
		t.Fatalf("tractors = %d, want %d", got, want)
	}
	if got, want := len(w.topology.Lanes()), 7; got != want {
		t.Fatalf("lanes = %d, want %d", got, want)
	}
	if w.Phase() != PhaseRunning {
		t.Fatalf("phase = %s, want %s", w.Phase(), PhaseRunning)
	}
	if w.WorldSeq() != 0 {
		t.Fatalf("worldSeq = %d, want 0 before anything has happened", w.WorldSeq())
	}
	for _, s := range w.slots {
		if !s.Plugged {
			t.Fatalf("slot %s is not plugged at round 0", s.ID)
		}
	}
	if r, _ := w.Reefer(Reefer2); r.TagReadable {
		t.Fatalf("REEFER-2's tag reads at the start, which removes the RFID fault")
	}
}

// A deep-freeze reefer fits the two modern slots and not the older one, and that
// exclusion is what makes slot choice a planning decision rather than a constant.
func TestRatingExcludesTheOlderSlot(t *testing.T) {
	w := NewWorld()
	deep, _ := w.Reefer(Reefer1)
	old, _ := w.Slot(Slot2)
	modern, _ := w.Slot(Slot1)

	if old.RatingsAccept(deep) {
		t.Fatalf("SLOT-2 rated to %d accepted a %d reefer", old.RatedMaxC, deep.TempC)
	}
	if !modern.RatingsAccept(deep) {
		t.Fatalf("SLOT-1 rated to %d refused a %d reefer", modern.RatedMaxC, deep.TempC)
	}

	o, ok := Observe(w, Tug1, Reefer1)
	if !ok {
		t.Fatal("Observe refused to frame the world for TUG-1")
	}
	got := o.AcceptingSlots()
	if len(got) != 2 || got[0] != Slot1 || got[1] != Slot3 {
		t.Fatalf("REEFER-1's candidate slots = %v, want [SLOT-1 SLOT-3]", got)
	}
}

func TestClaimAwardsEachJobToOneTractor(t *testing.T) {
	d := newDriver(t)
	for _, tug := range []TractorID{Tug1, Tug2, Tug3} {
		d.applied(Command{Kind: CmdClaimJob, Tractor: tug})
	}
	claimed := map[ReeferID]TractorID{}
	for _, j := range d.w.jobs {
		if j.ClaimedBy == "" {
			t.Fatalf("job %s is unclaimed after three claims", j.ID)
		}
		tr, _ := d.w.Tractor(j.ClaimedBy)
		if tr.Job != j.ID {
			t.Fatalf("%s holds job %q but %s claims it", tr.ID, tr.Job, j.ID)
		}
		claimed[j.Reefer] = j.ClaimedBy
	}
	if len(claimed) != 3 {
		t.Fatalf("%d distinct reefers claimed, want 3", len(claimed))
	}

	// A fourth claim finds an empty queue, and a busy tractor is refused.
	r := d.apply(Command{Kind: CmdClaimJob, Tractor: Tug1})
	if r.Applied() {
		t.Fatal("TUG-1 claimed a second job while already holding one")
	}
	if r.Reason != ReasonTractorBusy {
		t.Fatalf("busy claim refused as %s, want %s", r.Reason, ReasonTractorBusy)
	}
}

func TestCompleteDeliveryForEveryReefer(t *testing.T) {
	d := newDriver(t)

	// Each tractor claims the job the queue awards it, then walks the eastern
	// approach to the bay and places. The bay holds one, so they queue.
	order := []TractorID{Tug1, Tug2, Tug3}
	leased := make(map[TractorID]ReeferID, 3)
	for _, tug := range order {
		d.applied(Command{Kind: CmdClaimJob, Tractor: tug})
		leased[tug] = ""
		for _, j := range d.w.jobs {
			if j.ClaimedBy == tug {
				leased[tug] = j.Reefer
			}
		}
		if leased[tug] == "" {
			t.Fatalf("%s claimed nothing", tug)
		}
	}

	// The three claims must have produced three distinct reefers, or two tractors
	// would be racing for the same cargo.
	seen := map[ReeferID]TractorID{}
	for _, tug := range order {
		if other, clash := seen[leased[tug]]; clash {
			t.Fatalf("%s and %s both leased %s", other, tug, leased[tug])
		}
		seen[leased[tug]] = tug
	}

	for _, tug := range order {
		reefer := leased[tug]
		if rec, _ := d.w.Reefer(reefer); !rec.TagReadable {
			d.applied(Command{Kind: CmdScanTag, Tractor: tug, Reefer: reefer})
		}
		d.applied(Command{Kind: CmdPickUp, Tractor: tug, Reefer: reefer})
		d.applied(Command{Kind: CmdMoveTo, Tractor: tug, Node: NodeEast})
		d.applied(Command{Kind: CmdMoveTo, Tractor: tug, Node: NodeBay})
		slot := pickSlot(d.w, reefer, tug)
		d.applied(Command{Kind: CmdPlace, Tractor: tug, Reefer: reefer, Slot: slot})
		// The bay and the waypoints each hold one tractor, and only the apron
		// does not. Returning home is what frees the route for the next
		// delivery, so a finished tractor is walked back rather than left in the
		// way. Once the run is over the world is terminal and refuses everything,
		// so there is nothing left to drive for.
		if d.w.Phase() == PhaseRunning {
			d.applied(Command{Kind: CmdMoveTo, Tractor: tug, Node: NodeEast})
			d.applied(Command{Kind: CmdMoveTo, Tractor: tug, Node: NodeStaging})
		}
	}

	if d.w.Phase() != PhaseComplete {
		t.Fatalf("phase = %s after three deliveries, want %s", d.w.Phase(), PhaseComplete)
	}
	for _, r := range []ReeferID{Reefer1, Reefer2, Reefer3} {
		if !d.w.Delivered(r) {
			t.Fatalf("%s is not delivered", r)
		}
	}
}

// pickSlot returns the lowest-numbered slot the reefer's own frame says will take
// it and that is free, which is the choice a plan makes from that frame. It goes
// through the observation rather than re-reading the world, so the test exercises
// the same path a plan will and the two cannot drift apart unnoticed.
func pickSlot(w World, r ReeferID, tug TractorID) SlotID {
	o, ok := Observe(w, tug, r)
	if !ok {
		return ""
	}
	for _, s := range o.AcceptingSlots() {
		slot, _ := w.Slot(s)
		if slot.Occupied == "" {
			return s
		}
	}
	return ""
}
