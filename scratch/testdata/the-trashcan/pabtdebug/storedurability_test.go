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
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestADurableHistorySurvivesAKilledProcess is the durability evidence.
//
// The other restart tests close a store cleanly, which issues the lock's
// rollback and proves nothing about a crash. This one starts a child process
// that writes records, takes a sequence, marks execution started, and then kills
// itself with SIGKILL: no Close, no rollback, no connection teardown. The parent
// then has to reopen the history immediately, see the committed record intact,
// see the abandoned intent reported, and finish with a gap-free chain.
func TestADurableHistorySurvivesAKilledProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a signal kill is not available on windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "history", "session.sqlite")

	// The child commits two records, then takes a third sequence and dies.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestStoreCrashHelper", "-test.v")
	cmd.Env = append(os.Environ(),
		"PABTDEBUG_CRASH_STORE="+path,
		"PABTDEBUG_CRASH_HELPER=1",
	)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	// The child kills itself, so a non-zero exit is expected and its message is
	// the only report there will be.
	//
	// The context is attached to the command, not merely consulted afterwards: a
	// child that neither died nor deadlocked would otherwise hang the suite, and
	// reading the context only after Wait returned is not a bound at all.
	runErr := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("the crash helper neither died nor reported within the bound:\n%s", out.String())
	}
	if runErr == nil {
		t.Fatal("the crash helper exited cleanly, so no process was killed")
	}
	if !strings.Contains(out.String(), "CRASH_HELPER_COMMITTED") {
		t.Fatalf("the crash helper did not reach its commit:\n%s", out.String())
	}

	// The lock must not be left holding the history, or a killed debugger would
	// make the store permanently unopenable.
	store, err := OpenSQLiteStore(context.Background(), SQLiteStoreConfig{Path: path})
	if err != nil {
		t.Fatalf("a store killed mid-write could not be reopened: %v", err)
	}
	defer func() { _ = store.Close() }()

	installation, err := store.Installation(context.Background())
	if err != nil {
		t.Fatalf("Installation after the kill: %v", err)
	}
	if installation.Recovery == RecoveryCorrupt {
		t.Fatal("the reopened store considers itself corrupt")
	}

	// The session is the one the child created, which the installation row names.
	// Bootstrapping again would be refused, and a second session would not be the
	// history the child wrote.
	key := RunKey{
		SessionID: installation.ActiveSessionID,
		PlanID:    "p",
		RunID:     "r",
	}

	// The session the child created is the one the parent must see, so the child
	// prints its identity and the parent reads it back from the store.
	head, err := store.Head(context.Background(), key)
	if err != nil {
		t.Fatalf("Head after the kill: %v", err)
	}
	if head.HeadSequence < 2 {
		t.Fatalf("head sequence after the kill = %d, want the two committed records", head.HeadSequence)
	}
	if verified, err := store.VerifyChain(context.Background(), key); err != nil {
		t.Errorf("the chain did not survive the kill: %v", err)
	} else if verified != head.HeadSequence {
		t.Errorf("chain verified %d records, want %d", verified, head.HeadSequence)
	}

	// The abandoned intent is reported rather than dropped, and the sequence it
	// held is not a gap in the chain.
	pending, err := store.PendingIntents(context.Background())
	if err != nil {
		t.Fatalf("PendingIntents after the kill: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("pending intents after the kill = %d, want 1", len(pending))
	}
	if !pending[0].ExecutionStarted {
		t.Error("the abandoned intent is not marked as having started, so recovery cannot tell " +
			"a crash during a tick from a crash before one")
	}
	if pending[0].Sequence != head.HeadSequence+1 {
		t.Errorf("the abandoned intent holds sequence %d, want %d", pending[0].Sequence, head.HeadSequence+1)
	}
	// Nothing was half-written at the abandoned sequence.
	if _, err := store.RecordAt(context.Background(), key, pending[0].Sequence); !errors.Is(err, ErrNotFound) {
		t.Errorf("the abandoned sequence holds a record: %v", err)
	}
	// The dead attempt still holds the run's lease, so a new tick is told rather
	// than allowed to interleave with it.
	if _, err := store.Begin(context.Background(), BeginRequest{
		RunKey: key, ExpectedPlanRevision: testPlanRevision,
	}); !errors.Is(err, ErrAppendBusy) {
		t.Errorf("a tick after the kill = %v, want the dead attempt's lease to be reported", err)
	}
	// Resolving it lets the run continue with no gap.
	if err := store.Abort(context.Background(), pending[0]); err != nil {
		t.Fatalf("Abort after the kill: %v", err)
	}
	ticket, err := store.Begin(context.Background(), BeginRequest{
		RunKey: key, ExpectedPlanRevision: testPlanRevision,
	})
	if err != nil {
		t.Fatalf("Begin after resolving the killed attempt: %v", err)
	}
	if _, err := store.Commit(context.Background(), ticket, makeRecord(smallTree("n"))); err != nil {
		t.Fatalf("Commit after the kill: %v", err)
	}
	if verified, err := store.VerifyChain(context.Background(), key); err != nil {
		t.Errorf("the chain has a gap after the kill was resolved: %v", err)
	} else if verified != head.HeadSequence+1 {
		t.Errorf("chain verified %d records, want %d", verified, head.HeadSequence+1)
	}
}

// TestStoreCrashHelper is not a test. It is the child process
// TestADurableHistorySurvivesAKilledProcess kills, and it does nothing unless the
// environment says it is being run as that helper.
func TestStoreCrashHelper(t *testing.T) {
	path := os.Getenv("PABTDEBUG_CRASH_STORE")
	if path == "" || os.Getenv("PABTDEBUG_CRASH_HELPER") != "1" {
		t.Skip("not running as the crash helper")
	}
	store, err := OpenSQLiteStore(context.Background(), SQLiteStoreConfig{Path: path})
	if err != nil {
		t.Fatalf("helper open: %v", err)
	}
	identity, err := store.Bootstrap(context.Background(), BootstrapRequest{PlanID: "p", RunID: "r"})
	if err != nil {
		t.Fatalf("helper bootstrap: %v", err)
	}
	key := RunKey{SessionID: identity.SessionID, PlanID: identity.PlanID, RunID: identity.RunID}
	for i := range 2 {
		ticket, err := store.Begin(context.Background(),
			BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision})
		if err != nil {
			t.Fatalf("helper begin %d: %v", i, err)
		}
		if _, err := store.Commit(context.Background(), ticket, makeRecord(smallTree("n"))); err != nil {
			t.Fatalf("helper commit %d: %v", i, err)
		}
	}
	t.Log("CRASH_HELPER_COMMITTED")
	if f, ok := any(os.Stdout).(interface{ Sync() error }); ok {
		_ = f.Sync()
	}
	// Take a sequence and mark the tick started, then die with no cleanup at all.
	abandoned, err := store.Begin(context.Background(),
		BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision})
	if err != nil {
		t.Fatalf("helper begin abandoned: %v", err)
	}
	if err := store.MarkExecutionStarted(context.Background(), abandoned); err != nil {
		t.Fatalf("helper mark: %v", err)
	}
	t.Log("CRASH_HELPER_ABANDONED")
	if f, ok := any(os.Stdout).(interface{ Sync() error }); ok {
		_ = f.Sync()
	}
	// SIGKILL cannot be caught, so nothing after this line runs.
	if process, err := os.FindProcess(os.Getpid()); err == nil {
		_ = process.Kill()
	}
	select {}
}
