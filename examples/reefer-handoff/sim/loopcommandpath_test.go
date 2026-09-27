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
	"sort"
	"sync"
	"testing"
	"time"
)

// Concurrent producers must still get one total world sequence. Each applied
// receipt names the sequence it produced, and a total order means those are
// distinct and contiguous.
func TestConcurrentProducersGetOneTotalWorldSequence(t *testing.T) {
	w := newStartedLoop(t, ModeDeterministic)

	const producers, each = 8, 6
	tractors := []TractorID{Tug1, Tug2, Tug3}
	var wg sync.WaitGroup
	results := make([][]Result, producers)
	for p := 0; p < producers; p++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			// Every producer frames the world itself, as an independent runner
			// would, and asks for something that is always admissible. The
			// tractors are shared on purpose, so most of these are refused as
			// busy and the world sequence has to stay total regardless.
			for i := 0; i < each; i++ {
				k := keyFor(p, i)
				c, ok := commandOnLoop(w, tractors[i%len(tractors)], Reefer1, k, CmdClaimJob, "", "")
				if !ok {
					t.Errorf("producer %d could not frame the world for %s", p, k)
					return
				}
				ch, err := w.Submit(c)
				if err != nil {
					t.Errorf("producer %d submit: %v", p, err)
					return
				}
				select {
				case r := <-ch:
					results[p] = append(results[p], r)
				case <-time.After(resultTimeout):
					t.Errorf("producer %d got no result for %s", p, k)
					return
				}
			}
		}(p)
	}
	wg.Wait()

	applied := map[uint64]string{}
	for _, rs := range results {
		for _, r := range rs {
			if !r.Receipt.Applied() {
				continue
			}
			if other, clash := applied[r.Receipt.WorldSeq]; clash {
				t.Fatalf("worldSeq %d was produced by both %s and %s",
					r.Receipt.WorldSeq, other, r.Receipt.IdempotencyKey)
			}
			applied[r.Receipt.WorldSeq] = r.Receipt.IdempotencyKey
		}
	}
	if len(applied) == 0 {
		t.Fatal("no command was applied, so the ordering was never exercised")
	}
	seqs := make([]int, 0, len(applied))
	for s := range applied {
		seqs = append(seqs, int(s))
	}
	sort.Ints(seqs)
	for i, s := range seqs {
		if s != i+1 {
			t.Fatalf("applied world sequences are %v, want 1..%d with no gap or repeat", seqs, len(seqs))
		}
	}
	if got := int(w.Snapshot().WorldSeq()); got != len(applied) {
		t.Fatalf("the world is at worldSeq %d but %d commands applied", got, len(applied))
	}
}

// A key that is already admitted must not be admitted twice, and the second
// caller must end up with the first caller's result rather than a second one.
func TestSubmittingAnAdmittedKeyTwiceYieldsOneResult(t *testing.T) {
	w := newStartedLoop(t, ModeDeterministic)
	c := command(t, w, Tug1, Reefer1, "dup", CmdClaimJob, "", "")

	first, err := w.Submit(c)
	if err != nil {
		t.Fatalf("first Submit: %v", err)
	}
	second, err := w.Submit(c)
	if err != nil {
		t.Fatalf("second Submit: %v", err)
	}
	if first != second {
		t.Fatal("the second Submit handed back a different channel")
	}

	select {
	case r := <-first:
		if !r.Receipt.Applied() {
			t.Fatalf("the claim was refused as %s", r.Receipt.Reason)
		}
	case <-time.After(resultTimeout):
		t.Fatal("no result on the shared channel")
	}
	if got := w.Snapshot().WorldSeq(); got != 1 {
		t.Fatalf("worldSeq = %d after one admitted key, want 1", got)
	}
}

// A duplicate reaches Reduce and comes back as the original receipt.
func TestReplayingASettledKeyReturnsTheOriginalReceipt(t *testing.T) {
	w := newStartedLoop(t, ModeDeterministic)
	first := mustResult(t, w, command(t, w, Tug1, Reefer1, "settled", CmdClaimJob, "", ""))
	if !first.Receipt.Applied() {
		t.Fatalf("the first claim was refused as %s", first.Receipt.Reason)
	}

	again := mustResult(t, w, command(t, w, Tug1, Reefer1, "settled", CmdClaimJob, "", ""))
	if again.Receipt.Outcome != OutcomeDuplicate {
		t.Fatalf("a replayed key gave %s, want %s", again.Receipt.Outcome, OutcomeDuplicate)
	}
	if again.Receipt.CommandSeq != first.Receipt.CommandSeq {
		t.Fatalf("the replay names commandSeq %d, the original %d",
			again.Receipt.CommandSeq, first.Receipt.CommandSeq)
	}
	if got := w.Snapshot().WorldSeq(); got != 1 {
		t.Fatalf("a replay moved worldSeq to %d, want 1", got)
	}
}

// A command framed against a state that has since moved is refused, and the
// refusal names the resource that moved. The reducer owns that judgement, and the
// reason must not depend on when the command arrived: a placement into a slot that
// was taken gets SlotOccupied and the holder, and the same placement into a slot
// that lost power gets SlotUnpowered. Both are true whether the command was framed
// before the change or after it.
func TestACommandFramedAgainstAMovedStateIsRefused(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(*testing.T, *WorldLoop)
		frame   func(*testing.T, *WorldLoop) Command
		disturb func(*testing.T, *WorldLoop)
		want    Reason
	}{
		{
			name: "a slot that was taken while the command was in flight",
			setup: func(t *testing.T, w *WorldLoop) {
				claimAllThroughLoop(t, w)
				holdAtBayThroughLoop(t, w, Tug1, Reefer3)
				placeThroughLoop(t, w, Tug1, Reefer3, Slot1, "take")
			},
			frame: func(t *testing.T, w *WorldLoop) Command {
				holdAtBayThroughLoop(t, w, Tug2, Reefer1)
				return command(t, w, Tug2, Reefer1, "loser", CmdPlace, Slot1, "")
			},
			disturb: func(t *testing.T, w *WorldLoop) {},
			want:    ReasonSlotOccupied,
		},
		{
			name: "a slot that lost power while the command was in flight",
			setup: func(t *testing.T, w *WorldLoop) {
				holdAtBayThroughLoop(t, w, Tug1, Reefer3)
			},
			frame: func(t *testing.T, w *WorldLoop) Command {
				return command(t, w, Tug1, Reefer3, "dead", CmdPlace, Slot2, "")
			},
			disturb: func(t *testing.T, w *WorldLoop) {
				changeThroughLoop(t, w, func(world World) (World, []WorldEvent) {
					return SetSlotPower(world, Slot2, false)
				})
			},
			want: ReasonSlotUnpowered,
		},
		{
			name:  "a lane that closed while the command was in flight",
			setup: func(t *testing.T, w *WorldLoop) {},
			frame: func(t *testing.T, w *WorldLoop) Command {
				return command(t, w, Tug1, Reefer1, "closed", CmdMoveTo, "", NodeWest)
			},
			disturb: func(t *testing.T, w *WorldLoop) {
				changeThroughLoop(t, w, func(world World) (World, []WorldEvent) {
					return LockLane(world, LaneWest)
				})
			},
			want: ReasonLaneLocked,
		},
		{
			name: "a tag that was scanned while the command was in flight",
			setup: func(t *testing.T, w *WorldLoop) {
				claimAllThroughLoop(t, w)
			},
			frame: func(t *testing.T, w *WorldLoop) Command {
				return command(t, w, Tug1, Reefer2, "scanned", CmdScanTag, "", "")
			},
			disturb: func(t *testing.T, w *WorldLoop) {
				// Someone else reads the tag, under a different action's key.
				pre := mustResult(t, w, command(t, w, Tug1, Reefer2, "prescan", CmdScanTag, "", ""))
				if !pre.Receipt.Applied() {
					t.Fatalf("the pre-scan was refused as %s", pre.Receipt.Reason)
				}
			},
			want: ReasonTagReadable,
		},
		{
			name:  "a command from a world that has not happened",
			setup: func(t *testing.T, w *WorldLoop) {},
			frame: func(t *testing.T, w *WorldLoop) Command {
				return Command{ID: "ghost", IdempotencyKey: "ghost", Kind: CmdClaimJob, Tractor: Tug1, Expect: Expectation{WorldSeq: 99}}
			},
			disturb: func(t *testing.T, w *WorldLoop) {},
			want:    ReasonFromTheFuture,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newStartedLoop(t, ModeDeterministic)
			tc.setup(t, w)
			// The command is framed first and submitted second. Framing it after the
			// disturbance would hand it a current expectation and prove nothing,
			// which is how a test of staleness passes for the wrong reason.
			c := tc.frame(t, w)
			tc.disturb(t, w)
			before := w.Snapshot().StateKey()
			seqBefore := w.Snapshot().WorldSeq()

			r := mustResult(t, w, c)

			// A command framed against a world that has not happened is withdrawn
			// rather than refused: nothing was decided, so it reaches no receipt.
			if tc.want == ReasonFromTheFuture {
				if r.Abandoned == nil || r.Abandoned.Reason != ReasonFromTheFuture {
					t.Fatalf("a command from the future gave %+v, want an abandoned marker", r)
				}
				if r.Receipt.Applied() || len(r.Events) != 0 {
					t.Fatalf("a withdrawn command also produced a receipt or events: %+v", r)
				}
				return
			}

			if r.Receipt.Applied() {
				t.Fatal("a command framed against a moved state was applied")
			}
			if r.Receipt.Reason != tc.want {
				t.Fatalf("refused as %s, want %s", r.Receipt.Reason, tc.want)
			}
			if got := w.Snapshot().StateKey(); got != before {
				t.Fatalf("a refused command changed the world.\nbefore:\n%s\nafter:\n%s", before, got)
			}
			if len(r.Events) != 0 {
				t.Fatalf("a refused command published %d events, want none", len(r.Events))
			}
			// The refusal reached the world's ledger and names the sequence it did
			// not move, so a reader can find it later and see that nothing happened.
			if r.Receipt.WorldSeq != seqBefore {
				t.Fatalf("the refusal names worldSeq %d, want the unchanged %d", r.Receipt.WorldSeq, seqBefore)
			}
			found := false
			for _, e := range w.Snapshot().Receipts() {
				if e.IdempotencyKey == c.IdempotencyKey {
					found = true
					if e.Outcome != OutcomeRefused || e.Reason != tc.want {
						t.Fatalf("the ledger holds %s/%s for the refusal, want Refused/%s",
							e.Outcome, e.Reason, tc.want)
					}
				}
			}
			if !found {
				t.Fatalf("the refusal for %s is not in the world's ledger", c.IdempotencyKey)
			}
		})
	}
}

// A command framed against an older world is not stale merely because the world
// moved on. Nothing it acts on changed, so the reducer admits it, and treating
// the frame's own sequence as a precondition would refuse nearly every command in
// a busy world.
func TestAnOlderFrameIsNotItselfStale(t *testing.T) {
	w := newStartedLoop(t, ModeDeterministic)
	stale := command(t, w, Tug1, Reefer1, "old", CmdClaimJob, "", "")

	changeThroughLoop(t, w, func(world World) (World, []WorldEvent) {
		return LockLane(world, LaneWest)
	})
	if w.Snapshot().WorldSeq() == stale.Expect.WorldSeq {
		t.Fatal("the world did not move, so this test is not exercising what it claims")
	}

	r := mustResult(t, w, stale)
	if !r.Receipt.Applied() {
		t.Fatalf("a command framed against an older world was refused as %s", r.Receipt.Reason)
	}
}

// A producer that abandons its result channel must not hold up the loop.
func TestAnAbandonedProducerDoesNotBlockTheLoop(t *testing.T) {
	w := newStartedLoop(t, ModeDeterministic)

	const stranded = 20
	for i := 0; i < stranded; i++ {
		if _, err := w.Submit(command(t, w, Tug1, Reefer1, keyFor(0, i), CmdClaimJob, "", "")); err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
		// The channel is dropped on the floor every time. The first claim wins and
		// the rest are refused as busy, and neither may wedge the owner.
	}

	// One more command, whose result is read. The external task phase is
	// first-in first-out, so this settling proves every abandoned command before
	// it settled too — without the test having to guess when the drain finished.
	final := mustResult(t, w, command(t, w, Tug2, Reefer2, "after", CmdClaimJob, "", ""))
	if !final.Receipt.Applied() && final.Receipt.Reason != ReasonTractorBusy {
		t.Fatalf("the command after the abandoned ones was refused as %s", final.Receipt.Reason)
	}
	if left := w.Abandoned(); len(left) != 0 {
		t.Fatalf("abandoning result channels left %d commands unsettled: %v", len(left), left)
	}
	if got := w.Snapshot().WorldSeq(); got == 0 {
		t.Fatal("nothing was applied while producers were abandoning their results")
	}
}

// Both modes must run the same reducer and the same command path, so a run is
// reproducible. The two are driven identically — same commands, same host round
// boundaries — and must reach a byte-identical world, round counter included.
//
// The previous version of this test relied on the realtime timer not firing during
// a run that finishes in milliseconds, which is a timing margin rather than a
// property. What actually distinguishes the modes is tested separately, in
// TestRealtimeModeAdvancesRoundsAndDeterministicDoesNot.
func TestBothModesUseTheSameCommandPath(t *testing.T) {
	det := runScriptThroughLoop(t, ModeDeterministic)
	real := runScriptThroughLoop(t, ModeRealtime)

	if det.Canonical() != real.Canonical() {
		t.Fatalf("the two modes diverged.\ndeterministic:\n%s\nrealtime:\n%s",
			det.Canonical(), real.Canonical())
	}
	if det.Phase() != PhaseComplete {
		t.Fatalf("the scripted run ended in %s, want %s", det.Phase(), PhaseComplete)
	}
	if det.Round() != real.Round() {
		t.Fatalf("the two modes reached rounds %d and %d from the same script",
			det.Round(), real.Round())
	}
}

// The modes differ in exactly one respect: realtime mode advances rounds on its
// own and deterministic mode waits for the host. Both halves are asserted, so a
// build with the timer wired up in both places, or in neither, fails.
func TestRealtimeModeAdvancesRoundsAndDeterministicDoesNot(t *testing.T) {
	real := newStartedLoop(t, ModeRealtime)
	det := newStartedLoop(t, ModeDeterministic)

	// Long enough for several timer intervals, short enough to keep the test quick.
	time.Sleep(3 * RoundInterval)

	if got := real.Snapshot().Round(); got < 2 {
		t.Fatalf("realtime mode reached round %d after %v without the host asking", got, 3*RoundInterval)
	}
	if got := det.Snapshot().Round(); got != 1 {
		t.Fatalf("deterministic mode reached round %d with no host boundary, want 1", got)
	}

	// And the host still drives deterministic mode.
	det.BeginRound()
	waitForRound(t, det, 2)
}

// Cancel withdraws a command the owner has not reached, and reports the loss when
// the command has already settled.
func TestCancelWithdrawsAnUnreducedCommand(t *testing.T) {
	w := newStartedLoop(t, ModeDeterministic)
	c := command(t, w, Tug1, Reefer1, "cancelme", CmdClaimJob, "", "")

	ch, err := w.Submit(c)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if !w.Cancel(c.IdempotencyKey) {
		// The owner beat the cancel. That is a legitimate race, so the only thing
		// to assert is that the producer is answered either way.
		select {
		case <-ch:
		case <-time.After(resultTimeout):
			t.Fatal("the command was neither withdrawn nor reduced")
		}
		return
	}
	select {
	case r := <-ch:
		if r.Abandoned == nil {
			t.Fatalf("Cancel reported success but the result was %+v", r)
		}
		if r.Receipt.Applied() || len(r.Events) != 0 {
			t.Fatalf("a withdrawn command also produced a receipt or events: %+v", r)
		}
	case <-time.After(resultTimeout):
		t.Fatal("a withdrawn command was never settled")
	}
	if got := w.Snapshot().WorldSeq(); got != 0 {
		t.Fatalf("a withdrawn command moved worldSeq to %d, want 0", got)
	}

	// A second cancel finds nothing outstanding.
	if w.Cancel(c.IdempotencyKey) {
		t.Fatal("cancelling a settled command reported success")
	}
}

// runScriptThroughLoop drives three whole deliveries through the loop rather than
// through the reducer directly, so the mode comparison covers the real command
// path. The jobs run one at a time because the bay and the waypoints each hold a
// single tractor.
func runScriptThroughLoop(t *testing.T, mode Mode) World {
	t.Helper()
	w := newStartedLoop(t, mode)
	driveThroughLoop(t, w, Tug1, "t1")
	driveThroughLoop(t, w, Tug2, "t2")
	driveThroughLoop(t, w, Tug3, "t3")
	// The host drives the same boundaries in both modes, so the round counter is
	// part of what is being compared rather than a source of noise.
	for i := 0; i < 5; i++ {
		w.BeginRound()
	}
	waitForRound(t, w, 6)
	return w.Snapshot()
}

// driveThroughLoop runs one tractor through a whole job: claim, scan if its tag
// does not read, pick up, walk the eastern approach, place in the lowest-numbered
// free slot that will take it, and walk home.
func driveThroughLoop(t *testing.T, w *WorldLoop, tug TractorID, prefix string) {
	t.Helper()
	claim := mustResult(t, w, command(t, w, tug, Reefer1, prefix+"-claim", CmdClaimJob, "", ""))
	if !claim.Receipt.Applied() {
		t.Fatalf("%s could not claim: %s", tug, claim.Receipt.Reason)
	}
	// The awarded job arrives as a world event rather than on the receipt: a
	// committed command is not a loser, so its receipt names no holder.
	reefer := ReeferID("")
	for _, e := range claim.Events {
		if e.Kind == EvJobClaimed {
			reefer = e.Reefer
		}
	}
	if reefer == "" {
		t.Fatalf("%s's claim published no EvJobClaimed: %v", tug, claim.Events)
	}

	if _, ok := Observe(w.Snapshot(), tug, reefer); !ok {
		t.Fatalf("could not frame the world for %s and %s", tug, reefer)
	}
	holdAtBayThroughLoop(t, w, tug, reefer)
	framed, _ := Observe(w.Snapshot(), tug, reefer)
	slot := freeAcceptingSlot(w.Snapshot(), framed, reefer)
	if slot == "" {
		t.Fatalf("%s has no free slot that will take %s", tug, reefer)
	}
	placeThroughLoop(t, w, tug, reefer, slot, prefix)
}

// freeAcceptingSlot picks the lowest-numbered slot the reefer's frame says will
// take it and that nobody is standing in. Acceptance and occupancy are separate
// facts: a frame lists every slot that could hold the reefer, and a planner that
// wanted a delivery this round also skips the ones already filled.
func freeAcceptingSlot(w World, o Observation, r ReeferID) SlotID {
	for _, s := range o.AcceptingSlots() {
		if slot, ok := w.Slot(s); ok && slot.Occupied == "" {
			return s
		}
	}
	return ""
}

func waitForRound(t *testing.T, w *WorldLoop, want int) {
	t.Helper()
	deadline := time.Now().Add(resultTimeout)
	for time.Now().Before(deadline) {
		if w.Snapshot().Round() >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("round never reached %d, last was %d", want, w.Snapshot().Round())
}
