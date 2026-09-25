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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testStorePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "history", "session.sqlite")
}

// storeClock returns a clock that advances by a fixed step on every call, so
// stored timestamps are distinct and ordered without depending on wall time.
func storeClock() func() time.Time {
	var n int
	base := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	return func() time.Time {
		n++
		return base.Add(time.Duration(n) * time.Millisecond)
	}
}

func openTestStore(t *testing.T, path string) *SQLiteStore {
	t.Helper()
	store, err := OpenSQLiteStore(context.Background(), SQLiteStoreConfig{
		Path:  path,
		Clock: storeClock(),
	})
	if err != nil {
		t.Fatalf("OpenSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func bootstrapTestStore(t *testing.T, store *SQLiteStore) SessionIdentity {
	t.Helper()
	identity, err := store.Bootstrap(context.Background(), BootstrapRequest{
		PlanID: "p", RunID: "r", Title: "test plan",
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	return identity
}

// makeRecord builds a record carrying a small tree, with the digests a commit
// expects the store to fill in.
func makeRecord(tree *StoredTree) StoredRecord {
	record := StoredRecord{
		Kind:          RecordTick,
		PayloadState:  PayloadComplete,
		TreeEligible:  tree != nil,
		Tree:          tree,
		StartedAt:     Timestamp(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)),
		CompletedAt:   Timestamp(time.Date(2026, 9, 25, 12, 0, 0, 1, time.UTC)),
		StatusPresent: true,
		Status:        2,
	}
	return record
}

func smallTree(rootID string) *StoredTree {
	node := StoredNode{
		ID:        rootID,
		Ordinal:   0,
		Type:      "root",
		Name:      "root",
		EffectIDs: []string{},
		Status:    StoredNodeStatus{TickCount: 1, LastStatus: 2, StatusPresent: true, Version: 1},
	}
	tree := &StoredTree{
		RootID:       rootID,
		Nodes:        []StoredNode{node},
		NodeCount:    1,
		PlanRevision: 1,
	}
	nodeDigest, err := HashNode(node)
	if err != nil {
		panic(err)
	}
	tree.Nodes[0].ContentHash = nodeDigest
	digest, err := HashTree(*tree)
	if err != nil {
		panic(err)
	}
	tree.Digest = digest
	return tree
}

func TestBootstrapCreatesTheWholeStore(t *testing.T) {
	path := testStorePath(t)
	store := openTestStore(t, path)
	identity := bootstrapTestStore(t, store)

	if identity.StoreID == "" || identity.SessionID == "" {
		t.Fatalf("bootstrap did not name the store and the session: %+v", identity)
	}
	if identity.Generation != 1 {
		t.Errorf("generation = %d, want 1", identity.Generation)
	}
	installation, err := store.Installation(context.Background())
	if err != nil {
		t.Fatalf("Installation: %v", err)
	}
	if installation.StoreID != identity.StoreID {
		t.Errorf("installation store id = %q, want %q", installation.StoreID, identity.StoreID)
	}
	if installation.ActiveSessionID != identity.SessionID {
		t.Errorf("active session = %q, want %q", installation.ActiveSessionID, identity.SessionID)
	}
	if installation.Recovery != RecoveryReady {
		t.Errorf("recovery = %q, want ready", installation.Recovery)
	}
	// The file and its directory hold the user's history and must be owner-only.
	for _, target := range []string{path, filepath.Dir(path)} {
		info, err := os.Stat(target)
		if err != nil {
			t.Fatalf("stat %s: %v", target, err)
		}
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			t.Errorf("%s has mode %v, want owner-only", target, perm)
		}
	}
	// A second bootstrap is refused rather than making a second history.
	if _, err := store.Bootstrap(context.Background(), BootstrapRequest{PlanID: "p2", RunID: "r2"}); !errors.Is(err, ErrHeadMismatch) {
		t.Errorf("a second bootstrap = %v, want a conflict", err)
	}
}

func TestASecondWriterIsRefused(t *testing.T) {
	path := testStorePath(t)
	first := openTestStore(t, path)
	bootstrapTestStore(t, first)

	// A second process on the same file must be told, not left to interleave.
	second, err := OpenSQLiteStore(context.Background(), SQLiteStoreConfig{
		Path: path, Clock: storeClock(),
	})
	if err == nil {
		_ = second.Close()
		t.Fatal("a second writer opened the same store")
	}
	if !errors.Is(err, ErrStoreLocked) {
		t.Errorf("a second writer = %v, want a lock error", err)
	}
	// The first store still works, because the refusal cost it nothing.
	head, err := first.Head(context.Background(), RunKey{
		SessionID: "", PlanID: "p", RunID: "r",
	})
	if err == nil {
		_ = head
	}
}

func TestRecordsSurviveAProcessRestart(t *testing.T) {
	path := testStorePath(t)
	first := openTestStore(t, path)
	identity := bootstrapTestStore(t, first)
	key := RunKey{SessionID: identity.SessionID, PlanID: identity.PlanID, RunID: identity.RunID}

	const ticks = 25
	written := make([]CommitReceipt, 0, ticks)
	for i := range ticks {
		ticket, err := first.Begin(context.Background(), BeginRequest{RunKey: key, ExpectedPlanRevision: 1})
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
		ticket, err := first.Begin(context.Background(), BeginRequest{RunKey: key, ExpectedPlanRevision: 1})
		if err != nil {
			t.Fatalf("Begin %d: %v", i, err)
		}
		if _, err := first.Commit(context.Background(), ticket, makeRecord(nil)); err != nil {
			t.Fatalf("Commit %d: %v", i, err)
		}
	}
	abandoned, err := first.Begin(context.Background(), BeginRequest{RunKey: key, ExpectedPlanRevision: 1})
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
	ticket, err := second.Begin(context.Background(), BeginRequest{RunKey: key, ExpectedPlanRevision: 1})
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

	first, err := store.Begin(context.Background(), BeginRequest{RunKey: key, ExpectedPlanRevision: 1})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	// A second tick while the first holds the lease is told, not allowed to
	// interleave and leave two records claiming one predecessor.
	if _, err := store.Begin(context.Background(), BeginRequest{RunKey: key, ExpectedPlanRevision: 1}); !errors.Is(err, ErrAppendBusy) {
		t.Errorf("a second Begin = %v, want the lease to be reported busy", err)
	}
	if _, err := store.Commit(context.Background(), first, makeRecord(nil)); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	// With the lease released the next tick proceeds, and the chain has no gap.
	second, err := store.Begin(context.Background(), BeginRequest{RunKey: key, ExpectedPlanRevision: 1})
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

	ticket, err := store.Begin(context.Background(), BeginRequest{RunKey: key, ExpectedPlanRevision: 1})
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
	// A record from another run is refused.
	elsewhere := ticket
	elsewhere.RunKey.RunID = "other"
	elsewhere.proof = store.proof(elsewhere.RunKey, elsewhere.Sequence, elsewhere.EventUID, elsewhere.AttemptID)
	if _, err := store.Commit(context.Background(), elsewhere, makeRecord(nil)); err == nil {
		t.Error("a record from another run committed")
	}
	// The genuine ticket still works, so the refusals cost nothing.
	if _, err := store.Commit(context.Background(), ticket, makeRecord(nil)); err != nil {
		t.Errorf("the genuine ticket was refused: %v", err)
	}
}

func TestReadsAreBoundedByRowsAndBytes(t *testing.T) {
	store := openTestStore(t, testStorePath(t))
	identity := bootstrapTestStore(t, store)
	key := RunKey{SessionID: identity.SessionID, PlanID: identity.PlanID, RunID: identity.RunID}

	const records = 40
	for i := range records {
		ticket, err := store.Begin(context.Background(), BeginRequest{RunKey: key, ExpectedPlanRevision: 1})
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
		ticket, err := store.Begin(context.Background(), BeginRequest{RunKey: key, ExpectedPlanRevision: 1})
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

func TestAReadOnlyStoreRefusesWritesAndIsNotCreated(t *testing.T) {
	path := testStorePath(t)
	store := openTestStore(t, path)
	identity := bootstrapTestStore(t, store)
	key := RunKey{SessionID: identity.SessionID, PlanID: identity.PlanID, RunID: identity.RunID}
	if _, err := store.Head(context.Background(), key); err != nil {
		t.Fatalf("Head: %v", err)
	}

	readOnlyMode := StoreModeReadOnly
	reader, err := OpenSQLiteStore(context.Background(), SQLiteStoreConfig{
		Path: path, Mode: &readOnlyMode, Clock: storeClock(),
	})
	if err != nil {
		t.Fatalf("open read-only: %v", err)
	}
	defer func() { _ = reader.Close() }()
	if _, err := reader.Head(context.Background(), key); err != nil {
		t.Errorf("a read-only store could not read: %v", err)
	}
	if _, err := reader.Begin(context.Background(), BeginRequest{RunKey: key}); !errors.Is(err, ErrStoreReadOnly) {
		t.Errorf("a write to a read-only store = %v, want a read-only error", err)
	}
	if _, err := reader.Bootstrap(context.Background(), BootstrapRequest{PlanID: "x", RunID: "y"}); !errors.Is(err, ErrStoreReadOnly) {
		t.Errorf("a bootstrap on a read-only store = %v, want a read-only error", err)
	}

	// A read-only open of a file that does not exist must not create one.
	missing := filepath.Join(t.TempDir(), "absent", "session.sqlite")
	if _, err := OpenSQLiteStore(context.Background(), SQLiteStoreConfig{
		Path: missing, Mode: &readOnlyMode,
	}); !errors.Is(err, ErrNotFound) {
		t.Errorf("opening an absent store read-only = %v, want not found", err)
	}
	if _, err := os.Stat(missing); err == nil {
		t.Error("a read-only open created the file")
	}
}

func TestANewerSchemaIsRefusedRatherThanGuessedAt(t *testing.T) {
	path := testStorePath(t)
	store := openTestStore(t, path)
	bootstrapTestStore(t, store)
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// A file written by a newer build must be refused, not opened optimistically.
	if err := bumpUserVersion(t, path, SchemaVersion+1); err != nil {
		t.Fatalf("bump version: %v", err)
	}
	if _, err := OpenSQLiteStore(context.Background(), SQLiteStoreConfig{Path: path}); !errors.Is(err, ErrUnsupportedSchema) {
		t.Errorf("opening a newer store = %v, want an unsupported schema error", err)
	}
}

func TestStoreLimitsAreEnforced(t *testing.T) {
	path := testStorePath(t)
	limits := DefaultStoreLimits()
	limits.MaxRecordBytes = 512
	store, err := OpenSQLiteStore(context.Background(), SQLiteStoreConfig{
		Path: path, Limits: &limits, Clock: storeClock(),
	})
	if err != nil {
		t.Fatalf("OpenSQLiteStore: %v", err)
	}
	defer func() { _ = store.Close() }()
	identity := bootstrapTestStore(t, store)
	key := RunKey{SessionID: identity.SessionID, PlanID: identity.PlanID, RunID: identity.RunID}

	ticket, err := store.Begin(context.Background(), BeginRequest{RunKey: key, ExpectedPlanRevision: 1})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	big := makeRecord(nil)
	big.PlannerError = &StoredError{Code: ErrorCodeValue, Type: "t", Message: strings.Repeat("y", 2000)}
	if _, err := store.Commit(context.Background(), ticket, big); !errors.Is(err, ErrValueUnencodable) {
		t.Errorf("an oversized record = %v, want it refused", err)
	}
	// The refusal left the lease held and nothing written, so the run is still
	// recoverable rather than half-committed.
	head, err := store.Head(context.Background(), key)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.HeadSequence != 0 {
		t.Errorf("head sequence = %d after a refused commit, want 0", head.HeadSequence)
	}
}

func TestStoreRejectsAnUnusableConfiguration(t *testing.T) {
	ctx := context.Background()
	if _, err := OpenSQLiteStore(ctx, SQLiteStoreConfig{}); err == nil {
		t.Error("an empty path was accepted")
	}
	limits := DefaultStoreLimits()
	limits.MaxPageRows = 0
	if _, err := OpenSQLiteStore(ctx, SQLiteStoreConfig{
		Path: testStorePath(t), Limits: &limits,
	}); err == nil {
		t.Error("incomplete limits were accepted")
	}
	if err := DefaultStoreLimits().Validate(); err != nil {
		t.Errorf("the default limits do not validate: %v", err)
	}
	store := openTestStore(t, testStorePath(t))
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := store.Head(ctx, RunKey{SessionID: "s", PlanID: "p", RunID: "r"}); !errors.Is(err, ErrStoreClosed) {
		t.Errorf("using a closed store = %v, want a closed error", err)
	}
	// Close is idempotent, so a caller may close defensively.
	if err := store.Close(); err != nil {
		t.Errorf("a second Close = %v, want nil", err)
	}
}

func TestBootstrapRequiresAPlanAndRun(t *testing.T) {
	store := openTestStore(t, testStorePath(t))
	for _, request := range []BootstrapRequest{
		{PlanID: "", RunID: "r"},
		{PlanID: "p", RunID: ""},
		{PlanID: "  ", RunID: "r"},
	} {
		if _, err := store.Bootstrap(context.Background(), request); err == nil {
			t.Errorf("bootstrap %+v was accepted", request)
		}
	}
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
	fmt.Fprint(new(strings.Builder), "")
}
