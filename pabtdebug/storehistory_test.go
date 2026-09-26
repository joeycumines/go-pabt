/*
   Copyright 2021 Joseph Cumines

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package pabtdebug

import (
	"context"
	"errors"
	"strings"
	"testing"
)

const testPlanRevision = 1

// makeRecord builds a record carrying a small tree, with the digests a commit
// expects the store to fill in.

func TestRecordsSurviveAProcessRestart(t *testing.T) {
	path := testStorePath(t)
	first := openTestStore(t, path)
	identity := bootstrapTestStore(t, first)
	key := RunKey{SessionID: identity.SessionID, PlanID: identity.PlanID, RunID: identity.RunID}

	const ticks = 25
	written := make([]CommitReceipt, 0, ticks)
	for i := range ticks {
		ticket, err := first.Begin(context.Background(), BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision})
		if err != nil {
			t.Fatalf("Begin %d: %v", i, err)
		}
		if err := first.MarkExecutionStarted(context.Background(), ticket); err != nil {
			t.Fatalf("MarkExecutionStarted %d: %v", i, err)
		}
		receipt, err := first.Commit(context.Background(), ticket, makeRecord(smallTree("n")))
		if err != nil {
			t.Fatalf("Commit %d: %v", i, err)
		}
		written = append(written, receipt)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// A new process must see byte-identical records and trees.
	second := openTestStore(t, path)
	head, err := second.Head(context.Background(), key)
	if err != nil {
		t.Fatalf("Head after restart: %v", err)
	}
	if head.HeadSequence != ticks {
		t.Fatalf("head sequence = %d, want %d", head.HeadSequence, ticks)
	}
	for i, want := range written {
		got, err := second.RecordAt(context.Background(), key, uint64(i+1))
		if err != nil {
			t.Fatalf("RecordAt %d after restart: %v", i+1, err)
		}
		if got.RecordSHA256 != want.RecordSHA256 {
			t.Errorf("record %d digest = %q after restart, want %q", i+1, got.RecordSHA256, want.RecordSHA256)
		}
		if got.PreviousRecordSHA256 != (func() string {
			if i == 0 {
				return zeroDigest
			}
			return written[i-1].RecordSHA256
		})() {
			t.Errorf("record %d links to %q, want the previous record's digest", i+1, got.PreviousRecordSHA256)
		}
	}
	tree, err := second.TreeAt(context.Background(), key, ticks)
	if err != nil {
		t.Fatalf("TreeAt after restart: %v", err)
	}
	if tree.Digest != written[ticks-1].TreeSHA256 {
		t.Errorf("tree digest = %q after restart, want %q", tree.Digest, written[ticks-1].TreeSHA256)
	}
	if verified, err := second.VerifyChain(context.Background(), key); err != nil {
		t.Fatalf("VerifyChain after restart: %v", err)
	} else if verified != ticks {
		t.Errorf("chain verified %d records, want %d", verified, ticks)
	}
}

func TestAnOpenIntentLeavesNoGapAndNoHalfRecord(t *testing.T) {
	path := testStorePath(t)
	first := openTestStore(t, path)
	identity := bootstrapTestStore(t, first)
	key := RunKey{SessionID: identity.SessionID, PlanID: identity.PlanID, RunID: identity.RunID}

	// Two committed records, then a tick that dies after taking its sequence.
	for i := range 2 {
		ticket, err := first.Begin(context.Background(), BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision})
		if err != nil {
			t.Fatalf("Begin %d: %v", i, err)
		}
		if _, err := first.Commit(context.Background(), ticket, makeRecord(nil)); err != nil {
			t.Fatalf("Commit %d: %v", i, err)
		}
	}
	abandoned, err := first.Begin(context.Background(), BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision})
	if err != nil {
		t.Fatalf("Begin abandoned: %v", err)
	}
	if err := first.MarkExecutionStarted(context.Background(), abandoned); err != nil {
		t.Fatalf("MarkExecutionStarted: %v", err)
	}
	// The process dies here: no commit, no abort.
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second := openTestStore(t, path)
	// The abandoned sequence is reported, not silently dropped and not written.
	pending, err := second.PendingIntents(context.Background())
	if err != nil {
		t.Fatalf("PendingIntents: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending intents = %d, want 1", len(pending))
	}
	if pending[0].Sequence != 3 || pending[0].AttemptID != abandoned.AttemptID {
		t.Errorf("pending intent = %+v, want sequence 3 attempt %s", pending[0], abandoned.AttemptID)
	}
	// Nothing was half-written: the head is still at the last committed record.
	head, err := second.Head(context.Background(), key)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.HeadSequence != 2 {
		t.Errorf("head sequence = %d, want 2: an uncommitted sequence must not advance the head", head.HeadSequence)
	}
	if _, err := second.RecordAt(context.Background(), key, 3); !errors.Is(err, ErrNotFound) {
		t.Errorf("RecordAt for the abandoned sequence = %v, want not found", err)
	}
	// Resolving it releases the sequence, and the next one follows the head, so
	// the abandoned sequence is reused rather than leaving a permanent gap.
	if err := second.Abort(context.Background(), pending[0]); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	ticket, err := second.Begin(context.Background(), BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision})
	if err != nil {
		t.Fatalf("Begin after an abort: %v", err)
	}
	if ticket.Sequence != 3 {
		t.Errorf("sequence after the abort = %d, want 3", ticket.Sequence)
	}
	if _, err := second.RecordAt(context.Background(), key, 3); !errors.Is(err, ErrNotFound) {
		t.Errorf("the aborted sequence holds a record: %v", err)
	}
	if _, err := second.Commit(context.Background(), ticket, makeRecord(nil)); err != nil {
		t.Errorf("committing into the released sequence: %v", err)
	}
	if verified, err := second.VerifyChain(context.Background(), key); err != nil {
		t.Errorf("the chain has a gap after an abort: %v", err)
	} else if verified != 3 {
		t.Errorf("chain verified %d records, want 3", verified)
	}
}

func TestTheAppendLeaseSerialisesTicks(t *testing.T) {
	store := openTestStore(t, testStorePath(t))
	identity := bootstrapTestStore(t, store)
	key := RunKey{SessionID: identity.SessionID, PlanID: identity.PlanID, RunID: identity.RunID}

	first, err := store.Begin(context.Background(), BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	// A second tick while the first holds the lease is told, not allowed to
	// interleave and leave two records claiming one predecessor.
	if _, err := store.Begin(context.Background(), BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision}); !errors.Is(err, ErrAppendBusy) {
		t.Errorf("a second Begin = %v, want the lease to be reported busy", err)
	}
	if _, err := store.Commit(context.Background(), first, makeRecord(nil)); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	// With the lease released the next tick proceeds, and the chain has no gap.
	second, err := store.Begin(context.Background(), BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision})
	if err != nil {
		t.Fatalf("Begin after commit: %v", err)
	}
	if second.Sequence != 2 {
		t.Errorf("second sequence = %d, want 2", second.Sequence)
	}
}

func TestACommitIsRefusedWhenItDoesNotMatchItsTicket(t *testing.T) {
	store := openTestStore(t, testStorePath(t))
	identity := bootstrapTestStore(t, store)
	key := RunKey{SessionID: identity.SessionID, PlanID: identity.PlanID, RunID: identity.RunID}

	ticket, err := store.Begin(context.Background(), BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	// A ticket the store did not issue cannot commit anything.
	forged := ticket
	forged.proof[0] ^= 0xff
	if _, err := store.Commit(context.Background(), forged, makeRecord(nil)); err == nil {
		t.Error("a forged ticket committed a record")
	}
	// A record that claims a different sequence is refused.
	wrong := makeRecord(nil)
	wrong.Sequence = 99
	if _, err := store.Commit(context.Background(), ticket, wrong); err == nil {
		t.Error("a record with the wrong sequence committed")
	}
	// A record naming another run is refused. The record is moved rather than the
	// ticket, because moving the ticket only reaches the lease lookup and proves
	// nothing about the record-versus-ticket check.
	elsewhere := makeRecord(nil)
	elsewhere.RunKey = RunKey{SessionID: key.SessionID, PlanID: "other", RunID: "other"}
	if _, err := store.Commit(context.Background(), ticket, elsewhere); err == nil {
		t.Error("a record from another run committed")
	}
	// A record claiming a sequence the ticket does not carry is refused.
	wrongSequence := makeRecord(nil)
	wrongSequence.Sequence = ticket.Sequence + 5
	if _, err := store.Commit(context.Background(), ticket, wrongSequence); err == nil {
		t.Error("a record with an unissued sequence committed")
	}
	// The refusals above left the attempt's lease held, which is what lets recovery
	// tell a half-written attempt from a finished one. Resolving it and taking a
	// fresh one shows the run still accepts work.
	if err := store.Abort(context.Background(), ticket); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	fresh, err := store.Begin(context.Background(),
		BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision})
	if err != nil {
		t.Fatalf("Begin after resolving: %v", err)
	}
	if _, err := store.Commit(context.Background(), fresh, makeRecord(nil)); err != nil {
		t.Errorf("a genuine ticket was refused: %v", err)
	}

	// A record below the revision the attempt began at describes a structure the
	// attempt cannot have produced, so it is refused.
	below, err := store.Begin(context.Background(),
		BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision + 3})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := store.Commit(context.Background(), below, makeRecord(nil)); err == nil {
		t.Error("a record below the attempt's plan revision committed")
	}
	if err := store.Abort(context.Background(), below); err != nil {
		t.Fatalf("Abort: %v", err)
	}

	// A record above it is ordinary. A tick that expands the plan ends at a higher
	// revision than it began at, which is the common case rather than an anomaly.
	expanded, err := store.Begin(context.Background(),
		BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	above := makeRecord(nil)
	above.Revision.PlanRevision = testPlanRevision + 3
	if _, err := store.Commit(context.Background(), expanded, above); err != nil {
		t.Errorf("a record above the attempt's plan revision was refused: %v", err)
	}
}

func TestReadsAreBoundedByRowsAndBytes(t *testing.T) {
	store := openTestStore(t, testStorePath(t))
	identity := bootstrapTestStore(t, store)
	key := RunKey{SessionID: identity.SessionID, PlanID: identity.PlanID, RunID: identity.RunID}

	const records = 40
	for i := range records {
		ticket, err := store.Begin(context.Background(), BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision})
		if err != nil {
			t.Fatalf("Begin %d: %v", i, err)
		}
		// A deliberately large record, so the byte bound is reachable before the
		// row bound is.
		record := makeRecord(nil)
		record.PlannerError = &StoredError{
			Code:    ErrorCodeValue,
			Type:    "test",
			Message: strings.Repeat("x", 2000),
		}
		if _, err := store.Commit(context.Background(), ticket, record); err != nil {
			t.Fatalf("Commit %d: %v", i, err)
		}
	}

	limits := store.Limits()
	page, err := store.Events(context.Background(), EventQuery{RunKey: key, Limit: 5})
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(page.Events) != 5 {
		t.Errorf("page rows = %d, want 5", len(page.Events))
	}
	if !page.HasMore {
		t.Error("a truncated page did not say there was more")
	}
	if page.NewestSeq != 5 {
		t.Errorf("page ends at %d, want 5", page.NewestSeq)
	}
	// The next page continues where the first stopped, so paging loses nothing.
	next, err := store.Events(context.Background(), EventQuery{RunKey: key, After: &[]uint64{page.NewestSeq}[0]})
	if err != nil {
		t.Fatalf("Events after: %v", err)
	}
	if len(next.Events) == 0 || next.Events[0].Sequence != 6 {
		t.Errorf("the next page starts at %v, want 6", next.Events)
	}
	// A byte-bound test: a tiny byte limit must still return a page.
	tiny := limits
	tiny.MaxPageBytes = 1
	store.mu.Lock()
	store.limits = tiny
	store.mu.Unlock()
	page, err = store.Events(context.Background(), EventQuery{RunKey: key})
	if err != nil {
		t.Fatalf("Events with a tiny byte bound: %v", err)
	}
	if len(page.Events) > 1 {
		t.Errorf("the byte bound returned %d rows", len(page.Events))
	}
	if !page.HasMore {
		t.Error("the byte bound did not report more remaining")
	}
}

func TestAnAlteredRecordIsCaught(t *testing.T) {
	path := testStorePath(t)
	store := openTestStore(t, path)
	identity := bootstrapTestStore(t, store)
	key := RunKey{SessionID: identity.SessionID, PlanID: identity.PlanID, RunID: identity.RunID}

	var receipt CommitReceipt
	for i := range 3 {
		ticket, err := store.Begin(context.Background(), BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision})
		if err != nil {
			t.Fatalf("Begin %d: %v", i, err)
		}
		receipt, err = store.Commit(context.Background(), ticket, makeRecord(smallTree("n")))
		if err != nil {
			t.Fatalf("Commit %d: %v", i, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Alter a stored record's bytes directly, the way corruption or tampering
	// would, and prove the store notices rather than serving it.
	second := openTestStore(t, path)
	if err := second.alterRecordBytes(t, key, 2, "altered"); err != nil {
		t.Fatalf("altering a stored record: %v", err)
	}
	if _, err := second.RecordAt(context.Background(), key, 2); !errors.Is(err, ErrStoreCorrupt) {
		t.Errorf("RecordAt on an altered record = %v, want a corruption error", err)
	}
	if _, err := second.VerifyChain(context.Background(), key); !errors.Is(err, ErrStoreCorrupt) {
		t.Errorf("VerifyChain on an altered run = %v, want a corruption error", err)
	}
	_ = receipt
}

func TestRunStateAndRecordLookup(t *testing.T) {
	store := openTestStore(t, testStorePath(t))
	identity := bootstrapTestStore(t, store)
	key := RunKey{SessionID: identity.SessionID, PlanID: identity.PlanID, RunID: identity.RunID}

	state, err := store.RunState(context.Background(), key)
	if err != nil {
		t.Fatalf("RunState: %v", err)
	}
	if state != "active" {
		t.Errorf("run state = %q, want active", state)
	}
	if _, err := store.RecordAt(context.Background(), key, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("RecordAt for an unwritten sequence = %v, want not found", err)
	}
	if _, err := store.Head(context.Background(), RunKey{SessionID: "nope", PlanID: "p", RunID: "r"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Head for an unknown session = %v, want not found", err)
	}
	if _, err := store.Head(context.Background(), RunKey{PlanID: "p", RunID: "r"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("Head with an incomplete key = %v, want not found", err)
	}
	if _, err := store.Installation(context.Background()); err != nil {
		t.Errorf("Installation: %v", err)
	}
}

func TestTheChainCoversTheTreeAndNotOnlyTheTreeItself(t *testing.T) {
	// The store keeps the tree twice: inside the record, where the chain covers
	// it, and in the representation table, where a base and a delta reference it.
	// A check of the representation against its own digest is satisfied by a
	// rewrite that recomputes that digest, which is what a buggy assembly path
	// would produce.
	store := openTestStore(t, testStorePath(t))
	identity := bootstrapTestStore(t, store)
	key := RunKey{SessionID: identity.SessionID, PlanID: identity.PlanID, RunID: identity.RunID}

	ticket, err := store.Begin(context.Background(), BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	receipt, err := store.Commit(context.Background(), ticket, makeRecord(smallTree("n")))
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if receipt.TreeSHA256 == "" {
		t.Fatal("a commit with a tree produced no tree digest")
	}
	if err := store.alterTreeBytes(t, key, 1, 9999); err != nil {
		t.Fatalf("altering a stored tree: %v", err)
	}
	// The forged tree is internally consistent, so only the chain can catch it.
	row := store.readers.QueryRow(
		"SELECT tree_sha256 FROM representations WHERE session_id = ? AND run_id = ? AND seq = 1",
		key.SessionID, key.RunID)
	var stored []byte
	if err := row.Scan(&stored); err != nil {
		t.Fatalf("read the altered digest: %v", err)
	}
	if hexString(stored) == receipt.TreeSHA256 {
		t.Skip("the alteration did not change the stored digest, so this case proves nothing")
	}
	if _, err := store.TreeAt(context.Background(), key, 1); !errors.Is(err, ErrStoreCorrupt) {
		t.Errorf("TreeAt on a tree the chain does not cover = %v, want a corruption error", err)
	}
	// The chain itself is still intact, which is exactly why it is the arbiter.
	if verified, err := store.VerifyChain(context.Background(), key); err != nil {
		t.Errorf("VerifyChain on a run whose representation was rewritten: %v", err)
	} else if verified != 1 {
		t.Errorf("chain verified %d records, want 1", verified)
	}
	// A page row names the tree digest, so a reader can cross-check it.
	page, err := store.Events(context.Background(), EventQuery{RunKey: key})
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(page.Events) != 1 {
		t.Fatalf("page rows = %d, want 1", len(page.Events))
	}
	if page.Events[0].TreeSHA256 == "" {
		t.Error("a page row for a tree-eligible record names no tree digest")
	}
}

func hexString(raw []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(raw)*2)
	for _, b := range raw {
		out = append(out, digits[b>>4], digits[b&0x0f])
	}
	return string(out)
}

func TestARecordCannotClaimATreeItDoesNotHave(t *testing.T) {
	store := openTestStore(t, testStorePath(t))
	identity := bootstrapTestStore(t, store)
	key := RunKey{SessionID: identity.SessionID, PlanID: identity.PlanID, RunID: identity.RunID}

	ticket, err := store.Begin(context.Background(),
		BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	// A record that claims it is tree eligible while carrying no tree. The
	// eligibility flag is derived by the store, so the stored record must not
	// carry the claim.
	liar := makeRecord(nil)
	liar.TreeEligible = true
	liar.PayloadState = PayloadPartial
	if _, err := store.Commit(context.Background(), ticket, liar); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	record, err := store.RecordAt(context.Background(), key, 1)
	if err != nil {
		t.Fatalf("RecordAt: %v", err)
	}
	if record.TreeEligible {
		t.Error("a record with no tree was stored as tree eligible")
	}
	page, err := store.Events(context.Background(), EventQuery{RunKey: key})
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(page.Events) != 1 || page.Events[0].TreeEligible {
		t.Errorf("the page says the record is tree eligible: %+v", page.Events)
	}
	if _, err := store.TreeAt(context.Background(), key, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("TreeAt for a record with no tree = %v, want not found", err)
	}
}

func mustPending(t *testing.T, store *SQLiteStore) []IntentTicket {
	t.Helper()
	pending, err := store.PendingIntents(context.Background())
	if err != nil {
		t.Fatalf("PendingIntents: %v", err)
	}
	if len(pending) == 0 {
		t.Fatal("no intent is pending")
	}
	return pending
}
