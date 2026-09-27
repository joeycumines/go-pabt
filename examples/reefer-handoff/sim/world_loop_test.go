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
	"context"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"
)

// resultTimeout bounds every wait for a result. It is a guard against a hang,
// not a timing expectation: a healthy loop answers in microseconds.
const resultTimeout = 5 * time.Second

func newStartedLoop(t *testing.T, mode Mode) *WorldLoop {
	t.Helper()
	w, err := NewWorldLoop(mode)
	if err != nil {
		t.Fatalf("NewWorldLoop: %v", err)
	}
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := w.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	})
	return w
}

// command builds a command framed against the loop's current world, which is what
// a runner does: it reads one frame and decides everything from it.
func command(t *testing.T, w *WorldLoop, tug TractorID, r ReeferID, key string, kind CommandKind, slot SlotID, node NodeID) Command {
	t.Helper()
	o, ok := Observe(w.Snapshot(), tug, r)
	if !ok {
		t.Fatalf("could not frame the world for %s and %s", tug, r)
	}
	return Command{
		ID: key, IdempotencyKey: key, Kind: kind, Tractor: tug, Reefer: r,
		Slot: slot, Node: node, Expect: o.Expectation(slot),
	}
}

func mustResult(t *testing.T, w *WorldLoop, c Command) Result {
	t.Helper()
	ch, err := w.Submit(c)
	if err != nil {
		t.Fatalf("Submit %s: %v", c.Kind, err)
	}
	select {
	case r := <-ch:
		return r
	case <-time.After(resultTimeout):
		t.Fatalf("no result for %s within %v", c.Kind, resultTimeout)
	}
	return Result{}
}

// The loop must refuse work before the owner exists. A task admitted to a loop
// that has not entered Run does not run on the callback owner, and the whole
// ownership guarantee rests on that being true rather than assumed.
func TestTheLoopRefusesWorkBeforeItStarts(t *testing.T) {
	w, err := NewWorldLoop(ModeDeterministic)
	if err != nil {
		t.Fatalf("NewWorldLoop: %v", err)
	}
	c := Command{ID: "c1", IdempotencyKey: "k1", Kind: CmdClaimJob, Tractor: Tug1}
	if _, err := w.Submit(c); err != ErrNotStarted {
		t.Fatalf("Submit before Start gave %v, want %v", err, ErrNotStarted)
	}
	if err := w.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown before Start: %v", err)
	}
}

func TestStartingTwiceIsRefused(t *testing.T) {
	w := newStartedLoop(t, ModeDeterministic)
	if err := w.Start(); err == nil {
		t.Fatal("a second Start was accepted")
	}
}

// Every command in a busy run must be reduced by the owner. The owner's assertion
// is not observable directly, so this checks its consequence: a command reduced
// off the owner is reported as abandoned, and a clean run has none.
func TestEveryCommandInACleanRunIsReducedByTheOwner(t *testing.T) {
	w := newStartedLoop(t, ModeDeterministic)

	mustResult(t, w, command(t, w, Tug1, Reefer1, "c1", CmdClaimJob, "", ""))
	mustResult(t, w, command(t, w, Tug2, Reefer2, "c2", CmdClaimJob, "", ""))
	mustResult(t, w, command(t, w, Tug3, Reefer3, "c3", CmdClaimJob, "", ""))
	mustResult(t, w, command(t, w, Tug2, Reefer2, "c4", CmdScanTag, "", ""))
	mustResult(t, w, command(t, w, Tug1, Reefer1, "c5", CmdPickUp, "", ""))

	if left := w.Abandoned(); len(left) != 0 {
		t.Fatalf("%d commands were abandoned in a clean run: %v", len(left), left)
	}
}

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
				c := command(t, w, tractors[i%len(tractors)], Reefer1, k, CmdClaimJob, "", "")
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

func keyFor(p, i int) string {
	return "p" + strconv.Itoa(p) + "-" + strconv.Itoa(i)
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

// A command decided against a slot, lane, or tag that has moved is refused before
// the reducer sees it, and changes nothing.
func TestStaleCommandsMutateNothing(t *testing.T) {
	cases := []struct {
		name    string
		disturb func(*testing.T, *WorldLoop)
		build   func(*testing.T, *WorldLoop) Command
		want    Reason
	}{
		{
			name: "a slot whose plug was pulled",
			disturb: func(t *testing.T, w *WorldLoop) {
				applyChange(t, w, func(world World) (World, []WorldEvent) {
					return SetSlotPower(world, Slot1, false)
				})
			},
			build: func(t *testing.T, w *WorldLoop) Command {
				c := command(t, w, Tug1, Reefer1, "k1", CmdClaimJob, "", "")
				mustResult(t, w, c)
				mustResult(t, w, command(t, w, Tug1, Reefer1, "k2", CmdPickUp, "", ""))
				mustResult(t, w, command(t, w, Tug1, Reefer1, "k3", CmdMoveTo, "", NodeEast))
				mustResult(t, w, command(t, w, Tug1, Reefer1, "k4", CmdMoveTo, "", NodeBay))
				// Framed while SLOT-1 was still plugged, then the desk pulled it.
				return command(t, w, Tug1, Reefer1, "k5", CmdPlace, Slot1, "")
			},
			want: ReasonSlotMoved,
		},
		{
			name: "a lane whose topology moved",
			disturb: func(t *testing.T, w *WorldLoop) {
				applyChange(t, w, func(world World) (World, []WorldEvent) {
					return LockLane(world, LaneBayWest)
				})
			},
			build: func(t *testing.T, w *WorldLoop) Command {
				return command(t, w, Tug1, Reefer1, "m1", CmdMoveTo, "", NodeWest)
			},
			want: ReasonLaneMoved,
		},
		{
			name: "a tag whose version moved",
			disturb: func(t *testing.T, w *WorldLoop) {
				mustResult(t, w, command(t, w, Tug1, Reefer2, "t2", CmdScanTag, "", ""))
			},
			build: func(t *testing.T, w *WorldLoop) Command {
				return command(t, w, Tug1, Reefer2, "t3", CmdPickUp, "", "")
			},
			want: ReasonTagMoved,
		},
		{
			name:    "a command from a world that has not happened",
			disturb: func(t *testing.T, w *WorldLoop) {},
			build: func(t *testing.T, w *WorldLoop) Command {
				c := command(t, w, Tug1, Reefer1, "f1", CmdClaimJob, "", "")
				c.Expect.WorldSeq = 99
				return c
			},
			want: ReasonFromTheFuture,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := newStartedLoop(t, ModeDeterministic)
			// The command is framed first and submitted second. Framing it after
			// the disturbance would hand it a current expectation and prove
			// nothing, which is how a test of staleness passes for the wrong reason.
			c := tc.build(t, w)
			tc.disturb(t, w)
			before := w.Snapshot().StateKey()

			r := mustResult(t, w, c)
			if r.Receipt.Applied() {
				t.Fatalf("a stale %s was applied", c.Kind)
			}
			if r.Receipt.Reason != tc.want {
				t.Fatalf("stale %s refused as %s, want %s", c.Kind, r.Receipt.Reason, tc.want)
			}
			if got := w.Snapshot().StateKey(); got != before {
				t.Fatalf("a stale %s changed the world.\nbefore:\n%s\nafter:\n%s", c.Kind, before, got)
			}
			if len(r.Events) != 0 {
				t.Fatalf("a stale %s published %d events, want none", c.Kind, len(r.Events))
			}
		})
	}
}

// A command framed against an older world is not stale merely because the world
// moved on. Only the resource it acts on has to still match, or a busy world
// would refuse nearly everything.
func TestAnOlderFrameIsNotItselfStale(t *testing.T) {
	w := newStartedLoop(t, ModeDeterministic)
	stale := command(t, w, Tug1, Reefer1, "old", CmdClaimJob, "", "")

	// Move the world on in a way that touches nothing the claim acts on.
	mustResult(t, w, command(t, w, Tug2, Reefer2, "other", CmdClaimJob, "", ""))
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

// A command admitted and never reduced must be reconciled at shutdown, or its
// producer waits forever.
func TestShutdownReconcilesWhatItNeverReduced(t *testing.T) {
	w, err := NewWorldLoop(ModeDeterministic)
	if err != nil {
		t.Fatalf("NewWorldLoop: %v", err)
	}
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	ch, err := w.Submit(command(t, w, Tug1, Reefer1, "stranded", CmdClaimJob, "", ""))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	// The loop may or may not have reduced it by the time we stop admitting.
	// Either way the producer is answered.
	if err := w.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case r := <-ch:
		if r.Receipt.Applied() {
			t.Fatalf("the reconciled command was applied, want it settled or abandoned: %+v", r.Receipt)
		}
		if r.Receipt.Reason != ReasonAbandoned && r.Receipt.Reason != ReasonNone {
			t.Fatalf("reconciled with reason %s, want Abandoned or a real refusal", r.Receipt.Reason)
		}
	case <-time.After(resultTimeout):
		t.Fatal("a command admitted before shutdown was never settled and never reconciled")
	}

	if left := w.Abandoned(); len(left) != 0 {
		t.Fatalf("after shutdown %v", left)
	}
}

// Both modes must run the same reducer and the same command path, so a run is
// reproducible apart from its timing. The comparison is deliberately about the
// world the commands produced and not about the host's round counter, which
// realtime mode advances on its own and deterministic mode never advances.
func TestBothModesUseTheSameCommandPath(t *testing.T) {
	det := runScriptThroughLoop(t, ModeDeterministic)
	real := runScriptThroughLoop(t, ModeRealtime)

	if det.Phase() != PhaseComplete || real.Phase() != PhaseComplete {
		t.Fatalf("the scripted run ended in %s and %s, want both %s",
			det.Phase(), real.Phase(), PhaseComplete)
	}
	if det.CommitLedger() != real.CommitLedger() {
		t.Fatalf("the two modes applied different commands.\ndeterministic:\n%s\nrealtime:\n%s",
			det.CommitLedger(), real.CommitLedger())
	}
	if det.Canonical() != real.Canonical() {
		t.Fatalf("the two modes diverged.\ndeterministic:\n%s\nrealtime:\n%s",
			det.Canonical(), real.Canonical())
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
	return w.Snapshot()
}

// driveThroughLoop runs one tractor through a whole job: claim, scan if its tag
// does not read, pick up, walk the eastern approach, place in the lowest-numbered
// slot that will take it, and walk home.
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

	world := w.Snapshot()
	o, ok := Observe(world, tug, reefer)
	if !ok {
		t.Fatalf("could not frame the world for %s and %s", tug, reefer)
	}
	if tagOK, known := o.Read(TagKey(reefer)); !known || tagOK != true {
		scan := mustResult(t, w, command(t, w, tug, reefer, prefix+"-scan", CmdScanTag, "", ""))
		if !scan.Receipt.Applied() {
			t.Fatalf("%s could not scan %s: %s", tug, reefer, scan.Receipt.Reason)
		}
	}
	framed, _ := Observe(w.Snapshot(), tug, reefer)
	slot := freeAcceptingSlot(w.Snapshot(), framed, reefer)
	if slot == "" {
		t.Fatalf("%s has no free slot that will take %s", tug, reefer)
	}

	steps := []struct {
		kind CommandKind
		slot SlotID
		node NodeID
	}{
		{kind: CmdPickUp},
		{kind: CmdMoveTo, node: NodeEast},
		{kind: CmdMoveTo, node: NodeBay},
		{kind: CmdPlace, slot: slot},
		{kind: CmdMoveTo, node: NodeEast},
		{kind: CmdMoveTo, node: NodeStaging},
	}
	for i, s := range steps {
		// The last placement may end the run, and a terminal world refuses
		// everything including the drive home.
		if s.kind == CmdMoveTo && w.Snapshot().Phase() != PhaseRunning {
			return
		}
		key := prefix + "-" + strconv.Itoa(i)
		r := mustResult(t, w, command(t, w, tug, reefer, key, s.kind, s.slot, s.node))
		if !r.Receipt.Applied() {
			t.Fatalf("%s %s was refused as %s", tug, s.kind, r.Receipt.Reason)
		}
	}
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
		if !r.Cancelled {
			t.Fatalf("Cancel reported success but the result was %+v", r)
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

func TestShutdownIsIdempotent(t *testing.T) {
	w, err := NewWorldLoop(ModeDeterministic)
	if err != nil {
		t.Fatalf("NewWorldLoop: %v", err)
	}
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := w.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown %d: %v", i, err)
		}
	}
}

// An exogenous change is admitted through the same path a command takes, so the
// two are ordered by the loop rather than by whichever goroutine won a race.
func TestExogenousChangesGoThroughTheLoop(t *testing.T) {
	w := newStartedLoop(t, ModeDeterministic)

	// Framed while the lane is open, so the expectation is genuinely behind.
	across := command(t, w, Tug1, Reefer1, "across", CmdMoveTo, "", NodeWest)

	events := applyChange(t, w, func(world World) (World, []WorldEvent) {
		return LockLane(world, LaneBayWest)
	})
	if len(events) != 1 || events[0].Kind != EvLaneLocked {
		t.Fatalf("locking published %v", events)
	}
	locked := w.Snapshot().Topology().LockedLanes()
	if len(locked) != 1 || locked[0] != LaneBayWest {
		t.Fatalf("the snapshot reports closed lanes %v, want [%s]", locked, LaneBayWest)
	}

	stale := mustResult(t, w, across)
	if stale.Receipt.Reason != ReasonLaneMoved {
		t.Fatalf("a move framed before the closure gave %q, want %s", stale.Receipt.Reason, ReasonLaneMoved)
	}
}

func applyChange(t *testing.T, w *WorldLoop, change func(World) (World, []WorldEvent)) []WorldEvent {
	t.Helper()
	ch, err := w.Apply(change)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	select {
	case evs := <-ch:
		return evs
	case <-time.After(resultTimeout):
		t.Fatalf("no events within %v", resultTimeout)
	}
	return nil
}

// BeginRound advances the host's clock on the owner and publishes what the
// boundary caused, which is how a reader sees the run end.
func TestBeginRoundPublishesThePhaseItCauses(t *testing.T) {
	w := newStartedLoop(t, ModeDeterministic)

	for i := 0; i < CutoffRound+2; i++ {
		w.BeginRound()
	}
	deadline := time.Now().Add(resultTimeout)
	for time.Now().Before(deadline) {
		if w.Snapshot().Phase() != PhaseRunning {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if got := w.Snapshot().Phase(); got != PhaseCutoff {
		t.Fatalf("phase = %s after %d rounds, want %s", got, CutoffRound+2, PhaseCutoff)
	}

	// Draining empties the buffer, so it is read once and inspected.
	drained := w.DrainRoundEvents()
	var sawCutoff bool
	for _, e := range drained {
		if e.Kind == EvCutoffReached {
			sawCutoff = true
		}
	}
	if !sawCutoff {
		t.Fatalf("no EvCutoffReached among %v", drained)
	}
}
