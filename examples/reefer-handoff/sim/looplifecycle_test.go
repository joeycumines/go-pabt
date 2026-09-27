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
	"sync"
	"testing"
	"time"
)

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
		if r.Abandoned == nil {
			t.Fatalf("the command was settled with a receipt rather than abandoned: %+v", r)
		}
	case <-time.After(resultTimeout):
		t.Fatal("a command admitted before shutdown was never settled and never reconciled")
	}

	// The regression this test exists for: a command the producer was told was
	// abandoned must not have changed the world. The terminal drain still runs
	// queued tasks, so without the admitting guard the world would commit a
	// change for a command already reported as never having happened.
	after := w.Snapshot()
	if after.WorldSeq() != 0 {
		t.Fatalf("a command reconciled as abandoned still moved the world to worldSeq %d\n%s",
			after.WorldSeq(), after.Canonical())
	}
	if n := len(after.Receipts()); n != 0 {
		t.Fatalf("a command reconciled as abandoned still reached the ledger: %v", after.Receipts())
	}
}

// A second concurrent Shutdown must join the first one's teardown rather than
// returning early, because "no work is outstanding" is a claim about the teardown
// and not about which caller arrived first.
//
// Whether a submitted command reduces before or after the first caller stops
// admission is a genuine race, so the test asserts what holds either way: every
// caller returns cleanly, every submitted command is answered exactly once, and
// the world is left consistent.
func TestASecondShutdownJoinsTheTeardown(t *testing.T) {
	w, err := NewWorldLoop(ModeDeterministic)
	if err != nil {
		t.Fatalf("NewWorldLoop: %v", err)
	}
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	const submitted = 4
	channels := make([]<-chan Result, 0, submitted)
	for i := 0; i < submitted; i++ {
		ch, err := w.Submit(command(t, w, Tug1, Reefer1, keyFor(0, i), CmdClaimJob, "", ""))
		if err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
		channels = append(channels, ch)
	}

	var wg sync.WaitGroup
	errs := make([]error, submitted)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = w.Shutdown(context.Background())
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent Shutdown %d: %v", i, err)
		}
	}

	// Every admitted command is answered, and answered once: a settled or an
	// abandoned marker, never both and never neither.
	for i, ch := range channels {
		select {
		case r := <-ch:
			settled := r.Abandoned != nil
			decided := r.Receipt.Outcome != "" || r.Receipt.WorldRevision != ""
			if settled == decided {
				t.Fatalf("command %d was answered with both or neither: %+v", i, r)
			}
		case <-time.After(resultTimeout):
			t.Fatalf("command %d was never answered by a concurrent teardown", i)
		}
	}

	if left := w.Abandoned(); len(left) != 0 {
		t.Fatalf("after a concurrent teardown %v", left)
	}
	assertInvariants(t, w.Snapshot())
	if _, err := w.Submit(command(t, w, Tug1, Reefer1, "late", CmdClaimJob, "", "")); err != ErrLoopTerminated {
		t.Fatalf("a command after the teardown was answered with %v, want %v", err, ErrLoopTerminated)
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
	drained := w.DrainEvents()
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

// An exogenous change is admitted through the same path a command takes, so the
// two are ordered by the loop rather than by whichever goroutine won a race.
func TestExogenousChangesGoThroughTheLoop(t *testing.T) {
	w := newStartedLoop(t, ModeDeterministic)

	// Framed while the western lane is open, so the crossing it asks for becomes
	// uncrossable before the command is submitted.
	across := command(t, w, Tug1, Reefer1, "across", CmdMoveTo, "", NodeWest)

	events := changeThroughLoop(t, w, func(world World) (World, []WorldEvent) {
		return LockLane(world, LaneWest)
	})
	if len(events) != 1 || events[0].Kind != EvLaneLocked {
		t.Fatalf("locking published %v", events)
	}
	locked := w.Snapshot().Topology().LockedLanes()
	if len(locked) != 1 || locked[0] != LaneWest {
		t.Fatalf("the snapshot reports closed lanes %v, want [%s]", locked, LaneWest)
	}

	// The crossing is refused by the reducer, naming the lane, and the events
	// published so far are still readable.
	refused := mustResult(t, w, across)
	if refused.Receipt.Applied() {
		t.Fatal("a move across a closed lane was applied")
	}
	if refused.Receipt.Reason != ReasonLaneLocked {
		t.Fatalf("a move across the closed lane gave %q, want %s", refused.Receipt.Reason, ReasonLaneLocked)
	}
	if refused.Receipt.Holder != string(LaneWest) {
		t.Fatalf("the refusal named holder %q, want the closed lane %q", refused.Receipt.Holder, LaneWest)
	}

	drained := w.DrainEvents()
	var sawLock bool
	for _, e := range drained {
		if e.Kind == EvLaneLocked && e.Lane == LaneWest {
			sawLock = true
		}
	}
	if !sawLock {
		t.Fatalf("the exogenous change was not readable: %v", drained)
	}
}
