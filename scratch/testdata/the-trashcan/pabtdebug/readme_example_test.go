/*
   Copyright 2026 Joseph Cumines

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

package pabtdebug_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/joeycumines/go-pabt"
	"github.com/joeycumines/go-pabt/pabtdebug"
)

// Compile-time signature checks for the debug server example documented in
// README.md. These assignments compile only if the real API matches the README,
// so any drift fails this build rather than a reader's program. The example is
// transcribed here rather than described.
var (
	_ func(context.Context, pabtdebug.SQLiteStoreConfig) (*pabtdebug.SQLiteStore, error)                                       = pabtdebug.OpenSQLiteStore
	_ func(*pabtdebug.SQLiteStore, context.Context, pabtdebug.BootstrapRequest) (pabtdebug.SessionIdentity, error)             = (*pabtdebug.SQLiteStore).Bootstrap
	_ func(*pabtdebug.SQLiteStore, pabtdebug.DurableServerConfig) (*pabtdebug.Server, error)                                   = pabtdebug.NewDurableServer
	_ func(context.Context, *pabtdebug.SQLiteStore, pabt.CapturedPlanner, pabtdebug.SessionConfig) (*pabtdebug.Session, error) = pabtdebug.OpenSession
	_ func(*pabtdebug.Session, context.Context) pabtdebug.TickResult                                                           = (*pabtdebug.Session).Tick
	_ func(*pabtdebug.Server) error                                                                                            = (*pabtdebug.Server).Start
	_ func(*pabtdebug.Server) error                                                                                            = (*pabtdebug.Server).Close
	_ func(*pabtdebug.Server) string                                                                                           = (*pabtdebug.Server).Capability
	_ func(*pabtdebug.Server) string                                                                                           = (*pabtdebug.Server).Addr
)

// readmeDurableCondition is the condition type the README's example is written
// over, so NewCaptured's type argument has something real to name.
type readmeDurableCondition struct{ key, value string }

func (c *readmeDurableCondition) Key() any       { return c.key }
func (c *readmeDurableCondition) Match(any) bool { return false }

// readmeDurableState is a State over that condition type, enough for the
// example to construct a plan. The example's substance is the store, the
// server, and the session; the planner's own behaviour is not what this pins.
type readmeDurableState struct{}

func (s *readmeDurableState) Variable(key any) (any, error) { return nil, nil }

func (s *readmeDurableState) Actions(failed *readmeDurableCondition) (
	[]pabt.Action[*readmeDurableCondition], error,
) {
	return nil, nil
}

func (s *readmeDurableState) LiveStateView() pabt.TypedStateViewProvider[*readmeDurableCondition] {
	return pabt.NewLiveStateViewProvider[*readmeDurableCondition](s)
}

// TestREADMEDurableServerExample runs the README's durable setup against a
// temporary store, so the documented wiring is executed rather than only
// type-checked. The ticks are not driven: the evidence target owns that, and a
// tick here would assert nothing the constructor calls do not already prove.
func TestREADMEDurableServerExample(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "run.db")

	store, err := pabtdebug.OpenSQLiteStore(ctx, pabtdebug.SQLiteStoreConfig{Path: path})
	if err != nil {
		t.Fatalf("OpenSQLiteStore: %v", err)
	}
	defer func() { _ = store.Close() }()

	identity, err := store.Bootstrap(ctx, pabtdebug.BootstrapRequest{
		PlanID: "my-plan",
		RunID:  "my-run",
		Title:  "My run",
	})
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	// The goal is a slice of condition groups, not a flat list: each element is
	// the set of conditions that must all hold for one goal.
	goal := []pabt.Conditions[*readmeDurableCondition]{
		{{key: "actor", value: "s5"}},
	}
	planner, err := pabt.NewCaptured[*readmeDurableCondition](&readmeDurableState{}, goal)
	if err != nil {
		t.Fatalf("NewCaptured: %v", err)
	}

	server, err := pabtdebug.NewDurableServer(store, pabtdebug.DurableServerConfig{
		Addr: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("NewDurableServer: %v", err)
	}
	defer func() { _ = server.Close() }()

	// The README tells the reader to print these two, so they have to be
	// readable off a live server: a capability that is empty cannot be pasted.
	if server.Capability() == "" {
		t.Error("server.Capability() is empty; the README tells a reader to print it")
	}
	if server.Addr() == "" {
		t.Error("server.Addr() is empty; the README tells a reader to print it")
	}

	session, err := pabtdebug.OpenSession(ctx, store, planner, pabtdebug.SessionConfig{
		Identity: identity,
		Announce: server.Announce,
	})
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if session.RunKey().RunID != "my-run" {
		t.Errorf("session RunID = %q, want my-run", session.RunKey().RunID)
	}
}
