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
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testStorePath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "history", "session.sqlite")
}

// storeClock returns a clock that advances by a fixed step on every call, so
// stored timestamps are distinct and ordered without depending on wall time.
//
// The counter is atomic because the store reads its clock from whichever goroutine
// is driving it, and two of them can be driving the same store at once.

func storeClock() func() time.Time {
	var n atomic.Int64
	base := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	return func() time.Time {
		return base.Add(time.Duration(n.Add(1)) * time.Millisecond)
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

// testPlanRevision is the plan revision every fixture uses. Begin records the
// revision an attempt began at and Commit refuses a record that disagrees, so a
// fixture that used two different values would be asserting a divergence rather
// than a contract.

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
	installation, err := first.Installation(context.Background())
	if err != nil {
		t.Fatalf("Installation after a refused second writer: %v", err)
	}
	if installation.StoreID == "" {
		t.Error("the first store lost its identity when a second writer was refused")
	}
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

	ticket, err := store.Begin(context.Background(), BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision})
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

func TestTheConnectionPolicyReachesEveryConnection(t *testing.T) {
	// A pragma issued through a pool reaches one connection. The policy is in the
	// data source name, so every connection the pool mints has it, and the
	// foreign key is therefore in force rather than only declared.
	//
	// The connections are held open at the same time. A loop that took one
	// connection and closed it before taking the next would only ever re-test the
	// same one, and would pass against a store whose policy reached nothing but
	// that connection.
	store := openTestStore(t, testStorePath(t))
	bootstrapTestStore(t, store)

	// The store keeps one writer connection, so the pool is widened here on
	// purpose. The claim under test is that the policy travels in the data source
	// name and therefore reaches any connection the pool mints, whatever size the
	// pool is; a future change to more than one must not silently lose it.
	store.writer.SetMaxOpenConns(3)
	const simultaneous = 3
	conns := make([]*sql.Conn, 0, simultaneous)
	defer func() {
		for _, conn := range conns {
			_ = conn.Close()
		}
	}()
	for range simultaneous {
		conn, err := store.writer.Conn(context.Background())
		if err != nil {
			t.Fatalf("writer connection: %v", err)
		}
		conns = append(conns, conn)
	}
	for i, conn := range conns {
		for name, want := range map[string]int{"foreign_keys": 1, "busy_timeout": 5000} {
			var got int
			if err := conn.QueryRowContext(context.Background(), "PRAGMA "+name).Scan(&got); err != nil {
				t.Fatalf("connection %d: read %s: %v", i, name, err)
			}
			if got != want {
				t.Errorf("connection %d has %s=%d, want %d: the policy did not reach it",
					i, name, got, want)
			}
		}
	}

	// A row naming a session that does not exist must be refused on every one of
	// them. If the foreign key were inert on any connection, this insert would
	// land and no read path could ever surface it.
	orphan := "INSERT INTO records (session_id, event_uid, plan_id, run_id, seq, attempt_id, kind, " +
		"payload_state, tree_eligible, execution_started, recorded_at, record_json, " +
		"payload_sha256, previous_record_sha256, record_sha256) " +
		"VALUES (?, 'e', 'p', 'r', 1, 'a', 'tick', 'none', 0, 0, 'now', x'00', " +
		"zeroblob(32), zeroblob(32), zeroblob(32))"
	for i, conn := range conns {
		if _, err := conn.ExecContext(context.Background(), orphan, orphanSessionID); err == nil {
			t.Errorf("an orphan record committed on connection %d: the foreign key is not in force", i)
		}
	}
}

func TestACrashBeforeTheFirstCommitStillOpens(t *testing.T) {
	// A run whose first tick was interrupted holds an intent and no records. Its
	// chain is empty, which is valid: refusing this would make the easiest crash
	// to hit the one that permanently needed a hand-edited database.
	path := testStorePath(t)
	first := openTestStore(t, path)
	identity := bootstrapTestStore(t, first)
	key := RunKey{SessionID: identity.SessionID, PlanID: identity.PlanID, RunID: identity.RunID}
	if _, err := first.Begin(context.Background(),
		BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision}); err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := OpenSQLiteStore(context.Background(), SQLiteStoreConfig{Path: path})
	if err != nil {
		t.Fatalf("a store interrupted during its first tick could not be reopened: %v", err)
	}
	defer func() { _ = second.Close() }()
	installation, err := second.Installation(context.Background())
	if err != nil {
		t.Fatalf("Installation: %v", err)
	}
	if installation.Recovery != RecoveryPending {
		t.Errorf("recovery = %q, want pending", installation.Recovery)
	}
	pending := mustPending(t, second)
	if err := second.Abort(context.Background(), pending[0]); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	// The run is usable again and the next sequence is the first.
	ticket, err := second.Begin(context.Background(),
		BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision})
	if err != nil {
		t.Fatalf("Begin after resolving: %v", err)
	}
	if ticket.Sequence != 1 {
		t.Errorf("sequence after resolving an empty run = %d, want 1", ticket.Sequence)
	}
	if _, err := second.Commit(context.Background(), ticket, makeRecord(smallTree("n"))); err != nil {
		t.Errorf("Commit: %v", err)
	}
	if verified, err := second.VerifyChain(context.Background(), key); err != nil {
		t.Errorf("VerifyChain: %v", err)
	} else if verified != 1 {
		t.Errorf("chain verified %d records, want 1", verified)
	}
}

func TestASymlinkedPathIsTheSameStore(t *testing.T) {
	// A path through a symlink must resolve to one file, or two processes could
	// open one history through two paths and neither would be refused. The file
	// is what matters, not the directory: an earlier version of this test
	// symlinked a directory, which the store already resolved, so it passed
	// against a build that did not resolve the final component at all.
	dir := t.TempDir()
	real := filepath.Join(dir, "real", "session.sqlite")
	if err := os.MkdirAll(filepath.Dir(real), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Symlink(real, filepath.Join(dir, "alias.sqlite")); err != nil {
		t.Skipf("symlinks are unavailable here: %v", err)
	}
	viaLink := filepath.Join(dir, "alias.sqlite")

	// The store does not exist yet, so the symlink is dangling and the path can
	// only be resolved once the file is created. If the store keeps the link's
	// name, it takes its lock on a different path and the target stays unlocked.
	viaLinkStore := openTestStore(t, viaLink)
	bootstrapTestStore(t, viaLinkStore)
	// The expected path is compared in its fully resolved form, because the store
	// resolves the whole path and a system temporary directory is itself a symlink.
	// It is resolved after the open, because the file did not exist before.
	resolvedReal, err := filepath.EvalSymlinks(real)
	if err != nil {
		t.Fatalf("resolve the target: %v", err)
	}
	if viaLinkStore.Path() != resolvedReal {
		t.Fatalf("the store resolved to %q, want %q", viaLinkStore.Path(), resolvedReal)
	}
	// Opening the same history by its real name must be refused.
	if _, err := OpenSQLiteStore(context.Background(), SQLiteStoreConfig{Path: real}); err == nil {
		t.Fatal("a symlinked path bypassed the second-writer lock")
	} else if !errors.Is(err, ErrStoreLocked) {
		t.Errorf("opening the target while the link is open = %v, want a lock error", err)
	}
}

func TestAnExistingSymlinkedStoreIsTheSameStore(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real", "session.sqlite")
	store := openTestStore(t, real)
	bootstrapTestStore(t, store)
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := os.Symlink(real, filepath.Join(dir, "alias.sqlite")); err != nil {
		t.Skipf("symlinks are unavailable here: %v", err)
	}
	// The first store holds the lock for the whole test, so the second open of
	// the same file through a different name must be refused.
	openTestStore(t, real)
	if viaLink, err := OpenSQLiteStore(context.Background(), SQLiteStoreConfig{
		Path: filepath.Join(dir, "alias.sqlite"),
	}); err == nil {
		_ = viaLink.Close()
		t.Fatal("an existing store was opened twice through two paths")
	} else if !errors.Is(err, ErrStoreLocked) {
		t.Errorf("opening an existing store through a symlink = %v, want a lock error", err)
	}
}

func TestAnOpenIntentIsRecordedAsPendingRecovery(t *testing.T) {
	path := testStorePath(t)
	first := openTestStore(t, path)
	identity := bootstrapTestStore(t, first)
	key := RunKey{SessionID: identity.SessionID, PlanID: identity.PlanID, RunID: identity.RunID}
	ticket, err := first.Begin(context.Background(),
		BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := first.Commit(context.Background(), ticket, makeRecord(nil)); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if _, err := first.Begin(context.Background(),
		BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision}); err != nil {
		t.Fatalf("Begin abandoned: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// A store that opened successfully is not necessarily ready to record into, so
	// the state is read rather than assumed.
	second := openTestStore(t, path)
	installation, err := second.Installation(context.Background())
	if err != nil {
		t.Fatalf("Installation: %v", err)
	}
	if installation.Recovery != RecoveryPending {
		t.Errorf("recovery = %q with an open intent, want %q", installation.Recovery, RecoveryPending)
	}
	if err := second.Abort(context.Background(), mustPending(t, second)[0]); err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if err := second.setRecovery(context.Background(), RecoveryReady); err != nil {
		t.Fatalf("setRecovery: %v", err)
	}
	installation, err = second.Installation(context.Background())
	if err != nil {
		t.Fatalf("Installation: %v", err)
	}
	if installation.Recovery != RecoveryReady {
		t.Errorf("recovery = %q after resolving, want ready", installation.Recovery)
	}
}
