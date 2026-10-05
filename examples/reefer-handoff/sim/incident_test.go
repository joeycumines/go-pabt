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
	"testing"
)

// The single-run tension: a calm three-tractor run completes at round 22,
// before Scripted(40) and Scripted(55) fire. These tests therefore drive
// the world loop with controlled command sequences that keep the run alive
// past the incident rounds, rather than relying on the natural completion
// order. The acceptance clause they evidence is: "the wind lockout and RFID
// fault produce recorded replans; a power rejection commits no placement."
// The logic-level re-routing around a locked lane is proven separately by
// the causality tests; these tests pin the world-side contract that makes
// that re-routing necessary.

// TestScriptedIncidentsRefuseCommandsAtTheirRounds drives the world loop
// through each scripted incident round with a command in flight that meets
// the changed state, and asserts the typed refusal the reducer produces.
// Each sub-test isolates one incident so a failure names exactly which
// contract broke.
func TestScriptedIncidentsRefuseCommandsAtTheirRounds(t *testing.T) {
	t.Run("wind lockout refuses a crossing in the same round", func(t *testing.T) {
		w := newStartedLoop(t, ModeDeterministic)

		// Drive TUG-1 to NodeWest so its next hop crosses LaneBayWest.
		claimAllThroughLoop(t, w)
		mustResult(t, w, command(t, w, Tug1, Reefer1, "move-west", CmdMoveTo, "", NodeWest))

		// Advance to round 20 and apply the lockout before any command.
		// BeginRound is asynchronous (it submits through the loop), so each
		// call needs a barrier to confirm it landed before the next one fires.
		// A no-op Change serves as that barrier: it rides the same Submit queue
		// and its result channel confirms everything ahead of it settled.
		for w.Snapshot().Round() < 20 {
			w.BeginRound()
			changeThroughLoop(t, w, func(world World) (World, []WorldEvent) {
				return world, nil
			})
		}
		evs := changeThroughLoop(t, w, func(world World) (World, []WorldEvent) {
			return LockLane(world, LaneBayWest)
		})
		if len(evs) != 1 || evs[0].Kind != EvLaneLocked {
			t.Fatalf("lockout published %v, want one LaneLocked", evs)
		}

		// A MoveTo across the locked lane is refused in this same round.
		r := mustResult(t, w, command(t, w, Tug1, Reefer1, "move-bay-locked", CmdMoveTo, "", NodeBay))
		if r.Receipt.Applied() {
			t.Fatal("a MoveTo across a locked lane was applied")
		}
		if r.Receipt.Reason != ReasonLaneLocked {
			t.Fatalf("refused as %s, want %s", r.Receipt.Reason, ReasonLaneLocked)
		}
	})

	t.Run("reopen advances the topology version", func(t *testing.T) {
		w := newStartedLoop(t, ModeDeterministic)

		// Lock, record version, reopen, check version advanced.
		changeThroughLoop(t, w, func(world World) (World, []WorldEvent) {
			return LockLane(world, LaneBayWest)
		})
		lockedVersion := w.Snapshot().Topology().Version()

		evs := changeThroughLoop(t, w, func(world World) (World, []WorldEvent) {
			return ReopenLane(world, LaneBayWest)
		})
		if len(evs) != 1 || evs[0].Kind != EvLaneReopened {
			t.Fatalf("reopen published %v, want one LaneReopened", evs)
		}
		reopenedVersion := w.Snapshot().Topology().Version()
		if reopenedVersion != lockedVersion+1 {
			t.Fatalf("reopen left topology version at %d, want %d",
				reopenedVersion, lockedVersion+1)
		}
	})

	t.Run("power rejection refuses a placement with no world effect", func(t *testing.T) {
		w := newStartedLoop(t, ModeDeterministic)

		// Get TUG-1 to the bay holding REEFER-3 (chilled, TempC=-8, accepted
		// by any slot's rating when powered).
		claimAllThroughLoop(t, w)
		holdAtBayThroughLoop(t, w, Tug1, Reefer3)

		// Unplug Slot1 before the Place arrives. REEFER-3 (TempC=-8) fits
		// any slot's rating when powered, so without the unplug this Place
		// would succeed. The refusal reason must be SlotUnpowered.
		evs := changeThroughLoop(t, w, func(world World) (World, []WorldEvent) {
			return SetSlotPower(world, Slot1, false)
		})
		if len(evs) != 1 || evs[0].Kind != EvSlotPowerChanged {
			t.Fatalf("unplug published %v, want one SlotPowerChanged", evs)
		}

		// The Place is refused with SlotUnpowered and publishes no events.
		r := mustResult(t, w, command(t, w, Tug1, Reefer3, "place-unpowered", CmdPlace, Slot1, ""))
		if r.Receipt.Applied() {
			t.Fatal("a Place into an unpowered slot was applied")
		}
		if r.Receipt.Reason != ReasonSlotUnpowered {
			t.Fatalf("refused as %s, want %s", r.Receipt.Reason, ReasonSlotUnpowered)
		}
		if len(r.Events) != 0 {
			t.Fatalf("a refused Place published %d events, want none", len(r.Events))
		}

		// The world sequence did not advance for the refusal.
		seqBefore := w.Snapshot().WorldSeq()
		_ = mustResult(t, w, command(t, w, Tug1, Reefer3, "place-unpowered-2", CmdPlace, Slot1, ""))
		if w.Snapshot().WorldSeq() != seqBefore {
			t.Fatalf("a refused Place advanced worldSeq from %d to %d",
				seqBefore, w.Snapshot().WorldSeq())
		}
	})
}

// TestScriptedReturnsTheRightIncidentsAtEachRound pins the incident table
// itself: each round maps to exactly the incidents due there, and no other
// round fires them. A regression here silently disables an incident.
func TestScriptedReturnsTheRightIncidentsAtEachRound(t *testing.T) {
	cases := []struct {
		round int
		want  int
	}{
		{19, 0},
		{20, 1}, // LockLane
		{21, 0},
		{39, 0},
		{40, 1}, // SetSlotPower
		{41, 0},
		{54, 0},
		{55, 1}, // ReopenLane
		{56, 0},
		{120, 0},
	}
	for _, tc := range cases {
		got := Scripted(tc.round)
		if len(got) != tc.want {
			t.Fatalf("Scripted(%d) returned %d incidents, want %d", tc.round, len(got), tc.want)
		}
	}
}
