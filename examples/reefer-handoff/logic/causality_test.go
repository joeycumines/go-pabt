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

package logic

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt/examples/reefer-handoff/sim"
	"github.com/joeycumines/go-pabt/pabtdebug"
)

// The P5 causality tests read committed records only. Nothing here traverses a
// live plan: a runner publishes through its session, and the assertions walk
// the store's own paged read path, which verifies every digest it returns. A
// reader that could only reach a live tree would be the defect these tests
// exist to keep out.

// newPublishedRunner builds a runner with its publisher attached, one store
// file under a fresh temp dir. The publisher closes on cleanup; a test that
// read its records before then sees whatever the final flush added, which is
// nothing when the buffers are already drained.
func newPublishedRunner(t *testing.T, loop *sim.WorldLoop, tug sim.TractorID) (*Runner, *Publisher) {
	t.Helper()
	pub := NewPublisher(tug, t.TempDir())
	r := New(tug, loop)
	r.AttachPublisher(pub)
	t.Cleanup(func() {
		if err := pub.Close(); err != nil {
			t.Errorf("closing %s's publisher: %v", tug, err)
		}
	})
	return r, pub
}

// readRun pages the run's committed records, oldest first, through the store's
// own bounded read path.
func readRun(t *testing.T, pub *Publisher) []pabtdebug.StoredRecord {
	t.Helper()
	ctx := context.Background()
	var out []pabtdebug.StoredRecord
	for after := uint64(0); ; {
		page, err := pub.Store().RecordsSince(ctx, pub.RunKey(), after)
		if err != nil {
			t.Fatalf("RecordsSince after %d: %v", after, err)
		}
		out = append(out, page.Records...)
		if !page.HasMore {
			return out
		}
		after = page.Through
	}
}

// causalString reads one causality string back off a committed record. The
// exact visibility is the assertion's ground: a causality ID that was redacted
// or defaulted to unknown would break the link it carries, so a test that
// found one refuses rather than guessing at the original bytes.
func causalString(t *testing.T, v pabtdebug.StoredValue) string {
	t.Helper()
	if v.Visibility != pabtdebug.ValueExact {
		t.Fatalf("causality string %q is %s, want exact", string(v.Data), v.Visibility)
	}
	return string(v.Data)
}

// causalOptional reads an entity field the application may have left unnamed.
// An absent link is a statement — no such entity was named — and reads as the
// empty string here.
func causalOptional(t *testing.T, v *pabtdebug.StoredValue) string {
	t.Helper()
	if v == nil {
		return ""
	}
	return causalString(t, *v)
}

func submissionByKey(t *testing.T, records []pabtdebug.StoredRecord, key string) (int, pabtdebug.StoredSubmission, bool) {
	t.Helper()
	for i, rec := range records {
		if rec.Causality == nil {
			continue
		}
		for _, sub := range rec.Causality.Submitted {
			if causalString(t, sub.IdempotencyKey) == key {
				return i, sub, true
			}
		}
	}
	return 0, pabtdebug.StoredSubmission{}, false
}

func submissionByCommandID(t *testing.T, records []pabtdebug.StoredRecord, id string) (int, pabtdebug.StoredSubmission, bool) {
	t.Helper()
	for i, rec := range records {
		if rec.Causality == nil {
			continue
		}
		for _, sub := range rec.Causality.Submitted {
			if causalString(t, sub.CommandID) == id {
				return i, sub, true
			}
		}
	}
	return 0, pabtdebug.StoredSubmission{}, false
}

// driveUntil ticks a runner until it stops making progress, reporting the
// outcome and any error instead of failing in place. The concurrency test
// needs it because a reader goroutine must be joined before the test can
// report a failure, and t.Fatalf from inside a drive loop would strand it.
func driveUntil(t *testing.T, r *Runner) (Outcome, error) {
	t.Helper()
	deadline := time.Now().Add(tickTimeout)
	for time.Now().Before(deadline) {
		out, err := r.Tick()
		if err != nil {
			return out, err
		}
		settle(t, r)
		switch out {
		case OutcomeDelivered, OutcomeBlocked, OutcomeIdle:
			return out, nil
		}
	}
	return OutcomeRunning, fmt.Errorf("%s did not settle its job within %v; it is holding %q in generation %d",
		r.Tractor(), tickTimeout, r.Reefer(), r.Generation())
}

// A world event a command caused is followed to the command and the plan tick
// that produced it, by explicit IDs read off committed records. The link is
// total — every caused event names a submission and shares its idempotency key
// — and for a command submitted during a plan tick the submission strictly
// precedes the settlement in the chain, because the append lease keeps a tick
// from committing its own receipt. The claim is the buffered case DD-37 names:
// its submission and settlement ride in the first plan record together, so the
// test counts precedence across the whole log rather than demanding it of every
// event.
func TestCausalityFollowsAWorldEventToItsCommandAndTick(t *testing.T) {
	loop := newLoop(t)
	r, pub := newPublishedRunner(t, loop, sim.Tug1)
	if out := drive(t, r); out != OutcomeDelivered {
		t.Fatalf("ended as %s, want %s; blocked on %q", out, OutcomeDelivered, r.Blocked())
	}
	records := readRun(t, pub)
	if len(records) == 0 {
		t.Fatal("a published run committed no records")
	}
	if _, err := pub.Store().VerifyChain(context.Background(), pub.RunKey()); err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}

	links, preceding := 0, 0
	for i, rec := range records {
		if rec.Causality == nil {
			continue
		}
		for _, st := range rec.Causality.Settled {
			for _, ev := range st.Events {
				cause := causalOptional(t, ev.CauseCommand)
				if cause == "" {
					continue
				}
				links++
				si, sub, ok := submissionByCommandID(t, records, cause)
				if !ok {
					t.Fatalf("event %s at record %d names cause command %s, which was never submitted",
						causalString(t, ev.Kind), rec.Sequence, cause)
				}
				if causalString(t, sub.IdempotencyKey) != causalString(t, st.IdempotencyKey) {
					t.Fatalf("command %s was submitted under key %s, but its receipt answers key %s",
						cause, causalString(t, sub.IdempotencyKey), causalString(t, st.IdempotencyKey))
				}
				if ev.WorldSeq != st.WorldSeq {
					t.Fatalf("event %s carries world sequence %d, its receipt was reduced at %d",
						causalString(t, ev.Kind), ev.WorldSeq, st.WorldSeq)
				}
				// The submission carries the tick that caused it, and the
				// record it rode in carries the same stamp: this is the
				// "followed to the plan tick" half. A zero stamp means no
				// plan tick submitted it — a claim, or a walk-home hop
				// after the generation retired — and the two-generation
				// test pins that every such submission carries it.
				if sub.Generation != 0 {
					stamp := records[si].Causality
					if stamp.Generation != sub.Generation || stamp.PlanTickSeq != sub.PlanTickSeq {
						t.Fatalf("command %s was submitted in generation %d tick %d, but rode in a record stamped %d/%d",
							cause, sub.Generation, sub.PlanTickSeq, stamp.Generation, stamp.PlanTickSeq)
					}
				}
				if si < i {
					preceding++
				}
			}
		}
	}
	if links == 0 {
		t.Fatal("no recorded world event names a causing command")
	}
	if preceding == 0 {
		t.Fatal("no command-caused event has its submission in an earlier record; cause never precedes effect in the chain")
	}
}

// The record whose plan reports Success is the record whose tree shows the
// goal met, and the receipt that met it — the applied Place — settles into
// that same record: the drain at the top of the tick notes it, the action
// verifies it against the fresh frame, and the record build drains both. The
// Place's submission rides in an earlier record, and the plan revision moved
// between them, so "this tree change happened because the world applied this
// command" is a reading of two records joined by one key, not an inference
// from their adjacency.
func TestTheDeliveryTreeNamesTheReceiptThatCausedIt(t *testing.T) {
	loop := newLoop(t)
	r, pub := newPublishedRunner(t, loop, sim.Tug1)
	if out := drive(t, r); out != OutcomeDelivered {
		t.Fatalf("ended as %s, want %s; blocked on %q", out, OutcomeDelivered, r.Blocked())
	}
	records := readRun(t, pub)

	succIdx := -1
	for i, rec := range records {
		if rec.StatusPresent && bt.Status(rec.Status) == btSuccess {
			succIdx = i
		}
	}
	if succIdx < 0 {
		t.Fatal("no record reports success")
	}
	succ := records[succIdx]
	if succ.Causality == nil || len(succ.Causality.Settled) == 0 {
		t.Fatal("the success record carries no settlement")
	}
	var place *pabtdebug.StoredSettlement
	for i := range succ.Causality.Settled {
		st := &succ.Causality.Settled[i]
		if causalString(t, st.Kind) == string(sim.CmdPlace) &&
			causalString(t, st.Outcome) == string(sim.OutcomeApplied) {
			place = st
		}
	}
	if place == nil {
		t.Fatalf("the success record settled no applied Place: %+v", succ.Causality.Settled)
	}
	key := causalString(t, place.IdempotencyKey)
	si, _, ok := submissionByKey(t, records[:succIdx], key)
	if !ok {
		t.Fatalf("the applied Place %s has no submission before the success record", key)
	}
	// The goal did not hold in the record that submitted the causing command and
	// holds in the record that settled it: the tree change the receipt caused is
	// the status flip the two records straddle. The plan revision need not move —
	// verifying an already-expanded Place changes no structure — so the honest
	// reading is the statuses, not the revision counter.
	if !records[si].StatusPresent || bt.Status(records[si].Status) == btSuccess {
		t.Fatalf("the submission record %d already reports the goal met, so the applied Place %s caused no change",
			records[si].Sequence, key)
	}
}

// A refused placement is recorded with its typed reason and no world events:
// the world decided nothing and changed nothing, and the log says so rather
// than leaving a reader to infer it from an absence. The refusal links to its
// submission by idempotency key, and the record that carries it already shows
// a tree the previous one did not — a refusal is precisely a failed
// precondition, and the planner's answer is to expand onto a slot that is
// still powered.
func TestARejectedCommandIsRecordedWithNoWorldEffect(t *testing.T) {
	loop := newLoop(t)
	r, pub := newPublishedRunner(t, loop, sim.Tug1)
	reefer := claimFor(t, r)
	if reefer == "" {
		t.Skip("no job was claimed in the rounds available")
	}
	aim := aimFor(t, loop, r, reefer)
	if aim == "" {
		t.Skipf("%s never reached the bay for %s", r.Tractor(), reefer)
	}
	if _, err := loop.Change(func(w sim.World) (sim.World, []sim.WorldEvent) {
		return sim.SetSlotPower(w, aim, false)
	}); err != nil {
		t.Fatalf("pulling the plug: %v", err)
	}
	if out := drive(t, r); out != OutcomeDelivered {
		t.Fatalf("ended as %s, want %s; blocked on %q", out, OutcomeDelivered, r.Blocked())
	}
	records := readRun(t, pub)

	refIdx := -1
	var refusal pabtdebug.StoredSettlement
	for i, rec := range records {
		if rec.Causality == nil {
			continue
		}
		for _, st := range rec.Causality.Settled {
			if causalString(t, st.Outcome) == string(sim.OutcomeRefused) &&
				causalOptional(t, st.Reason) == string(sim.ReasonSlotUnpowered) {
				refIdx, refusal = i, st
			}
		}
	}
	if refIdx < 0 {
		t.Fatal("the refused placement was never recorded")
	}
	if len(refusal.Events) != 0 {
		t.Fatalf("a refusal carries %d world events, want none", len(refusal.Events))
	}
	// The refusal is precisely a failed precondition: the planner's answer is to
	// place onto a slot that is still powered. Read the refused command's own
	// slot off its submission, then find an applied Place elsewhere in the log
	// naming a different slot — the recovery is a reading of the records, not an
	// inference from the run ending well. A re-expansion onto an already-grown
	// sibling moves no revision, so the slot identities are the honest evidence.
	_, refusedSub, ok := submissionByKey(t, records[:refIdx+1], causalString(t, refusal.IdempotencyKey))
	if !ok {
		t.Fatalf("the refused command %s has no submission record",
			causalString(t, refusal.IdempotencyKey))
	}
	refusedSlot := causalOptional(t, refusedSub.Slot)
	if refusedSlot == "" {
		t.Fatalf("the refused Place %s names no slot",
			causalString(t, refusal.IdempotencyKey))
	}
	recovered := false
	for _, rec := range records {
		if rec.Causality == nil {
			continue
		}
		for _, st := range rec.Causality.Settled {
			if causalString(t, st.Kind) != string(sim.CmdPlace) ||
				causalString(t, st.Outcome) != string(sim.OutcomeApplied) {
				continue
			}
			_, sub, found := submissionByKey(t, records, causalString(t, st.IdempotencyKey))
			if !found {
				continue
			}
			if slot := causalOptional(t, sub.Slot); slot != "" && slot != refusedSlot {
				recovered = true
			}
		}
	}
	if !recovered {
		t.Fatalf("no applied Place landed on a slot other than the refused %s; the re-expansion is not visible in the log", refusedSlot)
	}
}

// A walk-home receipt that settled after the last plan tick has no tick to
// ride in. The flush commits it as one tree-less control record — the lane's
// filler, carrying causality and no plan state — and the chain still verifies
// across it. The single keyframe it forces at the end of a history is the
// accepted cost DD-37 names; mid-history a caller that flushes is accepting
// that same cost.
func TestAFlushCommitsBufferedCausalityAsAControlRecord(t *testing.T) {
	loop := newLoop(t)
	r, pub := newPublishedRunner(t, loop, sim.Tug1)
	if out := drive(t, r); out != OutcomeDelivered {
		t.Fatalf("ended as %s, want %s; blocked on %q", out, OutcomeDelivered, r.Blocked())
	}

	// Walk home. The vacate hops submit and settle with no plan tick to carry
	// them, so they buffer until the flush.
	deadline := time.Now().Add(tickTimeout)
	walked := false
	for time.Now().Before(deadline) {
		if _, err := r.Tick(); err != nil {
			t.Fatalf("%s tick: %v", r.Tractor(), err)
		}
		settle(t, r)
		if tug, ok := loop.Snapshot().Tractor(sim.Tug1); ok && tug.At == sim.NodeStaging {
			walked = true
			break
		}
	}
	if !walked {
		t.Fatalf("%s never walked home to the apron", sim.Tug1)
	}
	if err := pub.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	records := readRun(t, pub)
	last := records[len(records)-1]
	if last.Kind != pabtdebug.RecordControl {
		t.Fatalf("the flush committed %q, want a control record", last.Kind)
	}
	if last.Tree != nil {
		t.Fatal("a control record carries a tree")
	}
	if last.Causality == nil || len(last.Causality.Settled) == 0 {
		t.Fatal("the buffered walk-home receipts did not ride in the control record")
	}
	var moveTo *pabtdebug.StoredSettlement
	for i := range last.Causality.Settled {
		st := &last.Causality.Settled[i]
		if causalString(t, st.Kind) == string(sim.CmdMoveTo) &&
			causalString(t, st.Outcome) == string(sim.OutcomeApplied) {
			moveTo = st
		}
	}
	if moveTo == nil {
		t.Fatalf("the control record carries no applied walk-home move: %+v", last.Causality.Settled)
	}
	if _, err := pub.Store().VerifyChain(context.Background(), pub.RunKey()); err != nil {
		t.Fatalf("VerifyChain across the control record: %v", err)
	}
}

// World events no command caused — a lane the wind controller closed — ride
// in the next committed record with no cause command, which is what tells an
// exogenous change apart from a command's effect. The host drains them through
// NoteEvents; the envelope carries them verbatim, world sequence and revision
// included.
func TestExogenousEventsRideWithoutACauseCommand(t *testing.T) {
	loop := newLoop(t)
	r, pub := newPublishedRunner(t, loop, sim.Tug1)
	if claimFor(t, r) == "" {
		t.Skip("no job was claimed in the rounds available")
	}
	pub.NoteEvents([]sim.WorldEvent{{
		Kind:          sim.EvLaneLocked,
		WorldSeq:      7,
		WorldRevision: "rev-exogenous",
		Lane:          sim.LaneBayWest,
	}})
	if _, err := r.Tick(); err != nil {
		t.Fatalf("%s tick: %v", r.Tractor(), err)
	}
	settle(t, r)

	records := readRun(t, pub)
	for _, rec := range records {
		if rec.Causality == nil {
			continue
		}
		for _, ev := range rec.Causality.Exogenous {
			if causalString(t, ev.Kind) != string(sim.EvLaneLocked) {
				continue
			}
			if causalOptional(t, ev.CauseCommand) != "" {
				t.Fatalf("an exogenous change names cause command %q", causalOptional(t, ev.CauseCommand))
			}
			if ev.WorldSeq != 7 {
				t.Fatalf("the exogenous event carries world sequence %d, want 7", ev.WorldSeq)
			}
			if causalString(t, ev.WorldRevision) != "rev-exogenous" {
				t.Fatalf("the exogenous event carries revision %q", causalString(t, ev.WorldRevision))
			}
			if causalOptional(t, ev.Lane) != string(sim.LaneBayWest) {
				t.Fatalf("the exogenous event names lane %q", causalOptional(t, ev.Lane))
			}
			return
		}
	}
	t.Fatal("the drained exogenous event rode in no record")
}

// Concurrent readers see complete old-or-new records, never a half-written
// one. The reader pages the whole run while the writer commits into it; the
// store verifies every digest it returns, so a torn record would surface as a
// read error, and an out-of-order page would surface as a sequence regression.
// Run under -race, this is the acceptance clause that no reader traverses a
// live plan: everything a reader touches is committed bytes.
func TestConcurrentReadsSeeCompleteRecords(t *testing.T) {
	loop := newLoop(t)
	r, pub := newPublishedRunner(t, loop, sim.Tug1)

	var (
		reads atomic.Int64
		errs  = make(chan error, 1)
		done  = make(chan struct{})
		stop  = make(chan struct{})
	)
	go func() {
		defer close(done)
		ctx := context.Background()
		for {
			select {
			case <-stop:
				return
			default:
			}
			store := pub.Store()
			if store == nil {
				// The store is bootstrapped with the first generation; a reader
				// that starts before then has nothing to page yet.
				time.Sleep(time.Millisecond)
				continue
			}
			page, err := store.RecordsSince(ctx, pub.RunKey(), 0)
			if err != nil {
				errs <- fmt.Errorf("RecordsSince: %w", err)
				return
			}
			reads.Add(int64(len(page.Records)))
			for i := 1; i < len(page.Records); i++ {
				if page.Records[i].Sequence <= page.Records[i-1].Sequence {
					errs <- fmt.Errorf("record %d follows %d out of order",
						page.Records[i].Sequence, page.Records[i-1].Sequence)
					return
				}
			}
			time.Sleep(time.Millisecond)
		}
	}()

	out, driveErr := driveUntil(t, r)
	close(stop)
	<-done
	if driveErr != nil {
		t.Fatalf("drive: %v", driveErr)
	}
	select {
	case err := <-errs:
		t.Fatalf("a concurrent read failed: %v", err)
	default:
	}
	if out != OutcomeDelivered {
		t.Fatalf("ended as %s, want %s; blocked on %q", out, OutcomeDelivered, r.Blocked())
	}
	if reads.Load() == 0 {
		t.Fatal("the reader never saw a record")
	}
	if _, err := pub.Store().VerifyChain(context.Background(), pub.RunKey()); err != nil {
		t.Fatalf("VerifyChain: %v", err)
	}
}

// Two deliveries on one tractor's run is DD-35's headline: successive plan
// generations on one store and one run. The walk home and the second claim
// settle with no plan tick to ride in, so they buffer across the generation
// boundary; BeginGeneration flushes them as one tree-less control record
// before the new plan's first tree is built. That record is the blocker that
// forces the new plan's first tree to be a keyframe — two independent plans
// share the run-prefixed occurrence-ID space, and a delta chained from one
// plan's last tree to the other's first does not reconstruct. The test then
// reads the whole run back and holds the attribution to the envelope's own
// contract: a submission a plan tick made matches the stamp of the record it
// rode in, and a submission no plan tick made — a claim, or a walk-home hop
// after the generation retired — carries the zero stamp that means "no plan
// tick". Without the boundary flush the second generation's first plan tick
// fails to commit and drive reports the error; without the zero stamp the
// walk-home hops name a plan tick that never submitted them.
func TestTwoGenerationsOnOneRunCommitAndAttributeTruthfully(t *testing.T) {
	loop := newLoop(t)
	r, pub := newPublishedRunner(t, loop, sim.Tug1)

	if out := drive(t, r); out != OutcomeDelivered {
		t.Fatalf("first delivery ended as %s, want %s; blocked on %q",
			out, OutcomeDelivered, r.Blocked())
	}
	firstGen := r.Generation()

	// The second drive walks the tractor out of the bay, claims again, and
	// delivers. This is the path every earlier test stopped short of.
	if out := drive(t, r); out != OutcomeDelivered {
		t.Fatalf("second delivery ended as %s, want %s; blocked on %q",
			out, OutcomeDelivered, r.Blocked())
	}
	secondGen := r.Generation()
	if secondGen <= firstGen {
		t.Fatalf("the second delivery did not open a new generation: still %d", secondGen)
	}

	records := readRun(t, pub)
	if _, err := pub.Store().VerifyChain(context.Background(), pub.RunKey()); err != nil {
		t.Fatalf("VerifyChain across two generations: %v", err)
	}

	controls, claims, vacates, planSubs, stamped := 0, 0, 0, 0, 0
	for _, rec := range records {
		if rec.Kind == pabtdebug.RecordControl {
			controls++
			if rec.Tree != nil {
				t.Fatalf("control record %d carries a tree", rec.Sequence)
			}
		}
		if rec.Causality == nil {
			continue
		}
		if rec.Causality.Generation == secondGen {
			stamped++
		}
		for _, sub := range rec.Causality.Submitted {
			key := causalString(t, sub.IdempotencyKey)
			kind := causalString(t, sub.Kind)
			switch {
			case kind == string(sim.CmdClaimJob):
				claims++
				if sub.Generation != 0 || sub.PlanTickSeq != 0 {
					t.Fatalf("claim %s carries stamp %d/%d, but no plan tick submitted it",
						key, sub.Generation, sub.PlanTickSeq)
				}
			case kind == string(sim.CmdMoveTo) && strings.Contains(key, "/vac"):
				// The walk home is identified by the key grammar keyLocked
				// mints for it, which is external to the stamp under test.
				vacates++
				if sub.Generation != 0 || sub.PlanTickSeq != 0 {
					t.Fatalf("walk-home hop %s carries stamp %d/%d, but no plan tick submitted it",
						key, sub.Generation, sub.PlanTickSeq)
				}
			default:
				planSubs++
				if sub.Generation == 0 {
					t.Fatalf("plan submission %s carries no stamp; it rode in a record stamped %d/%d",
						key, rec.Causality.Generation, rec.Causality.PlanTickSeq)
				}
				if rec.Causality.Generation != sub.Generation ||
					rec.Causality.PlanTickSeq != sub.PlanTickSeq {
					t.Fatalf("command %s was submitted in generation %d tick %d, but rode in a record stamped %d/%d",
						key, sub.Generation, sub.PlanTickSeq,
						rec.Causality.Generation, rec.Causality.PlanTickSeq)
				}
			}
		}
	}

	if controls == 0 {
		t.Fatal("no tree-less control record sits at the generation boundary; the buffered cross-generation causality had nowhere to commit, and the second plan chained onto the first")
	}
	if stamped == 0 {
		t.Fatalf("no record is stamped with the second generation %d; the run holds one plan, not two", secondGen)
	}
	// The run really did cross the boundary in both directions: two claims
	// (the first born before any generation, the second after a retirement),
	// the walk home over its two hops, and plan submissions the stamp governs.
	if claims < 2 {
		t.Fatalf("the run recorded %d claim submissions, want at least 2", claims)
	}
	if vacates < 2 {
		t.Fatalf("the run recorded %d walk-home hops, want at least 2", vacates)
	}
	if planSubs == 0 {
		t.Fatal("the run recorded no plan-tick submissions")
	}
}

// TestAnnounceWiringCarriesEveryCommittedRecordToTheLiveStream wires the
// publisher to a real durable server and drives one tractor through two
// deliveries, so the assertion covers both halves of the wiring: the eager
// Open that lets a server exist before the first generation opens, and the
// boundary control record the generation flush commits. The gate is the
// server's own — Announce resolves the store's active run and refuses a
// record naming any other — so an announce list that matches the paged run
// proves the DD-35 identity satisfies it for every record the publisher
// commits. A record that committed but never reached the stream is exactly
// the split-brain a live reader would hit: durable history the stream never
// carried. The hub resolves lazily and publishes to zero subscribers as a
// documented no-op, so this exercises the production Announce path without
// binding a port to a test.
func TestAnnounceWiringCarriesEveryCommittedRecordToTheLiveStream(t *testing.T) {
	loop := newLoop(t)
	pub := NewPublisher(sim.Tug1, t.TempDir())

	// Open eagerly: a store bootstrapped inside the first BeginGeneration
	// would leave no server to build before that generation's first tick, and
	// the earliest records would commit off the live stream.
	if err := pub.Open(context.Background()); err != nil {
		t.Fatalf("Open: %v", err)
	}
	server, err := pabtdebug.NewDurableServer(pub.Store(), pabtdebug.DurableServerConfig{})
	if err != nil {
		t.Fatalf("NewDurableServer: %v", err)
	}

	// The wrapper counts only what the server's gate accepted. A refused
	// announce returns its error to the session and never reaches the list,
	// so a divergence between this list and the paged run is the defect.
	var announced []uint64
	pub.AttachAnnounce(func(ctx context.Context, receipt pabtdebug.CommitReceipt, record pabtdebug.StoredRecord) error {
		if err := server.Announce(ctx, receipt, record); err != nil {
			return err
		}
		announced = append(announced, receipt.Sequence)
		return nil
	})

	// Cleanup order: the publisher's Close flushes the buffered walk-home
	// causality as a final control record and announces it, so the server has
	// to outlive the publisher. t.Cleanup runs last-in-first-out, so the
	// server's close registers first.
	t.Cleanup(func() { _ = server.Close() })
	t.Cleanup(func() {
		if err := pub.Close(); err != nil {
			t.Errorf("closing %s's publisher: %v", sim.Tug1, err)
		}
	})

	r := New(sim.Tug1, loop)
	r.AttachPublisher(pub)

	if out := drive(t, r); out != OutcomeDelivered {
		t.Fatalf("first delivery ended as %s, want %s; blocked on %q",
			out, OutcomeDelivered, r.Blocked())
	}
	if out := drive(t, r); out != OutcomeDelivered {
		t.Fatalf("second delivery ended as %s, want %s; blocked on %q",
			out, OutcomeDelivered, r.Blocked())
	}

	records := readRun(t, pub)
	if len(records) == 0 {
		t.Fatal("the run committed no records")
	}

	announcedSet := make(map[uint64]bool, len(announced))
	for _, seq := range announced {
		if announcedSet[seq] {
			t.Fatalf("sequence %d was announced twice", seq)
		}
		announcedSet[seq] = true
	}
	if len(announced) != len(records) {
		t.Fatalf("%d records reached the live stream but the run holds %d",
			len(announced), len(records))
	}

	// Every committed record, and only the committed records, rode the
	// stream. The boundary control record is named here too: it carries the
	// walk-home causality that no plan tick will ever ride in, so a live
	// watcher that misses it misses the walk for the rest of the run.
	controls := 0
	for _, rec := range records {
		if !announcedSet[rec.Sequence] {
			t.Fatalf("committed record %d (%s) never reached the live stream",
				rec.Sequence, rec.Kind)
		}
		if rec.Kind == pabtdebug.RecordControl {
			controls++
		}
	}
	if controls == 0 {
		t.Fatal("the run holds no control record; the generation boundary never flushed")
	}
}
