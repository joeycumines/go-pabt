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

// Command durable-target runs a finite, deterministic PA-BT run against a
// temporary store and serves it, so the browser client can be inspected against
// something real rather than a fixture held in memory.
//
// It exists to be measured, not used. Everything about it is chosen so that one
// run plants every condition the client has to survive:
//
//   - a condition that declares an expected value, and one that does not, so the
//     difference between a declared expectation and an unavailable one is visible
//     in the page rather than argued about;
//   - a secret the application declares, and a field whose name the recorder
//     recognises, so both redaction mechanisms are exercised;
//   - application text carrying markup, so a page that interpolated a recorded
//     value instead of setting text would execute it;
//   - an action that panics on one tick, so a record with no tree exists;
//   - an observer and a verifier, so an observed value and a relation are present.
//
// The run is deterministic: the store's clock is a counter, not the wall clock,
// so two runs of this binary over a fresh store commit byte-identical records and
// their digests can be compared. A runner that captured the same artifacts twice
// and got different digests would have found a real difference rather than a
// clock reading.
//
// It publishes readiness without a token. The server's ready file carries the
// bound address and nothing else, which is T6's invariant, and the capability
// goes to a separate file that only the runner reads. The file is removed on
// every exit path, including a signal, so a failed run leaves no capability
// behind.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt"
	"github.com/joeycumines/go-pabt/pabtdebug"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "durable-target:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		storePath   = flag.String("store", "", "the store file to create; required")
		readyFile   = flag.String("ready-file", "", "the file the server writes the bound address to")
		capFile     = flag.String("capability-file", "", "the file the capability is written to; required")
		ticks       = flag.Int("ticks", 6, "how many ticks to run")
		interval    = flag.Duration("tick-interval", 0, "how long to wait between ticks; zero runs them all before serving")
		hubEvents   = flag.Uint("hub-events", 0, "the hub's per-client event bound; zero uses the default")
		panicOnTick = flag.Int("panic-on-tick", 2, "the tick whose action panics; zero never panics. It has to be a tick on which an action still runs, so a tick before the goal settles: once it holds, nothing executes and nothing can panic.")
	)
	flag.Parse()
	if *storePath == "" {
		return errors.New("-store is required")
	}
	if *capFile == "" {
		return errors.New("-capability-file is required")
	}
	if *ticks < 1 {
		return fmt.Errorf("-ticks must be at least 1, not %d", *ticks)
	}

	// The handler goes in before anything that can block, and that is the whole
	// point of it. Installed after the tick loop, a SIGTERM arriving during the run
	// would take the default disposition and kill the process outright: no deferred
	// function would run, so the capability file would be left on disk and the
	// ordered shutdown would not happen at all. A target that leaks its capability
	// when it is interrupted is worse than one that leaks nothing when it finishes.
	ctx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stopSignals()

	// The capability file's directory is created first, and the file itself is
	// removed on every exit including a signal, so a run that fails leaves nothing
	// behind that a later process could use.
	if err := prepareCapabilityFile(*capFile); err != nil {
		return err
	}
	defer func() { _ = os.Remove(*capFile) }()

	store, err := pabtdebug.OpenSQLiteStore(ctx, pabtdebug.SQLiteStoreConfig{
		Path:  *storePath,
		Clock: countingClock(),
	})
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	identity, err := store.Bootstrap(ctx, pabtdebug.BootstrapRequest{
		PlanID: "evidence-plan",
		RunID:  "evidence-run",
		Title:  "pabtdebug browser evidence",
	})
	if err != nil {
		return err
	}

	limits := pabtdebug.DefaultDurableHubLimits()
	if *hubEvents > 0 {
		limits.MaxQueuedEvents = uint32(*hubEvents)
		// One event has to fit, or the hub refuses the subscription rather than
		// letting a client attach and be told it is behind immediately.
		limits.MaxQueuedBytes = limits.MaxQueuedBytes / 64
	}
	server, err := pabtdebug.NewDurableServer(store, pabtdebug.DurableServerConfig{
		ReadyFile: *readyFile,
		HubLimits: limits,
		// A short keep-alive so a run does not wait fifteen seconds to notice a
		// connection has gone.
		KeepAlive: 250 * time.Millisecond,
	})
	if err != nil {
		return err
	}
	defer func() { _ = server.Close() }()
	// The capability is minted by the server, so the file is written only after
	// the server exists and never before.
	if err := os.WriteFile(*capFile, []byte(server.Capability()+"\n"), 0o600); err != nil {
		return fmt.Errorf("write the capability file: %w", err)
	}

	world := newWorld(*panicOnTick)
	planner, err := pabt.NewCaptured[cond](world, world.goal(),
		pabt.WithExecutionObserver[cond](observer{}),
		pabt.WithTransitionVerifier[cond](verifier{}),
	)
	if err != nil {
		return err
	}
	session, err := pabtdebug.OpenSession(ctx, store, planner, pabtdebug.SessionConfig{
		Identity: identity,
		Announce: server.Announce,
	})
	if err != nil {
		return err
	}

	// The store's clock is deterministic, but the plan's own timing is not read
	// from it, so a tick interval is the only thing that makes a run take time.
	served := make(chan error, 1)
	go func() { served <- server.Start() }()

	if *interval > 0 {
		// Announce readiness before the first tick when the run is meant to be
		// watched, so a client can attach and then see the records arrive.
		fmt.Println("ready", server.Addr())
		for i := range *ticks {
			// The wait is interruptible, so a signal during the run reaches the
			// ordered shutdown rather than being lost until the run happens to end.
			select {
			case <-ctx.Done():
				fmt.Printf("interrupted before tick %d\n", i+1)
				return shutdown(server)
			case <-time.After(*interval):
			}
			if err := tick(ctx, session, world, i+1); err != nil {
				return err
			}
			fmt.Printf("tick %d\n", i+1)
		}
	} else {
		for i := range *ticks {
			if err := tick(ctx, session, world, i+1); err != nil {
				return err
			}
		}
		fmt.Println("ready", server.Addr())
	}
	// The capability is never printed. Standard output is a log, and a log is one
	// of the places T6's boundary test says a token must not appear; the runner
	// reads the file instead.

	// Alive after the last tick until terminated, which is what lets a browser
	// attach, be measured, and be torn down without racing the run's own output.
	select {
	case <-ctx.Done():
		fmt.Println("stopping")
	case err := <-served:
		if err != nil && !errors.Is(err, os.ErrClosed) {
			return fmt.Errorf("serve: %w", err)
		}
	}
	return shutdown(server)
}

// shutdown reports that the ordered shutdown has run.
//
// The deferred closes unwind in order on the way out of run - server, then store,
// then the capability file - so reaching here means all three have already
// happened. It takes the server only so a caller cannot reach it without having
// started one.
func shutdown(server *pabtdebug.Server) error {
	_ = server
	fmt.Println("stopped")
	return nil
}

// tick runs one tick of the given index. A tick that records a failure is still a
// tick that happened, so only a failure to commit at all stops the run.
func tick(ctx context.Context, session *pabtdebug.Session, world *world, index int) error {
	world.tick = index
	result := session.Tick(ctx)
	if result.Err != nil {
		return fmt.Errorf("tick %d: %w", result.Sequence, result.Err)
	}
	return nil
}

// prepareCapabilityFile creates the directory the capability will be written
// into. The file itself is written only once the server has minted a token, so
// there is no window in which the file exists and is empty.
func prepareCapabilityFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("capability file directory: %w", err)
	}
	return nil
}

// countingClock is a deterministic replacement for the wall clock. It starts at
// a fixed instant and advances a millisecond per reading, so a run's timestamps
// depend on the run's shape and not on when it happened.
func countingClock() func() time.Time {
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	var readings int
	return func() time.Time {
		readings++
		return start.Add(time.Duration(readings) * time.Millisecond)
	}
}

/* ------------------------------------------------------------------ fixture */

// cond is the plan's condition type. It is an interface on purpose: whether a
// condition declares an expected value is decided by a type assertion on the
// value, so a concrete type parameter could express either case but not both in
// one plan. The browser evidence needs both, because showing a declared
// expectation and an unavailable one correctly is the whole point of keeping the
// four evidence fields apart.
type cond interface{ pabt.Condition }

// secret is a value the application declares secret. Nothing about its name
// would tell a reader that, which is the point: the declaration is the only
// reason it is redacted.
type secret struct{ value string }

func (s secret) PabtSensitive() bool { return true }

// credentials exercises the name rule rather than a declaration, so both
// redaction mechanisms appear in one record.
type credentials struct {
	User     string
	Password string
	Endpoint string
}

// plainCondition is a condition that declares nothing, so its expected value is
// unavailable and must never be inferred from its key.
type plainCondition struct {
	key  string
	want any
}

func (c *plainCondition) Key() any { return c.key }

// Match is the condition's own test. With no declared expectation it holds once
// the variable is set at all, which is the honest behaviour: there is nothing to
// compare against, so the application decides.
func (c *plainCondition) Match(value any) bool {
	if c.want == nil {
		return value != nil
	}
	return value == c.want
}

// declaredCondition is the same condition with an explicit declaration, so the
// two differ in exactly one thing: whether Expected exists.
type declaredCondition struct{ *plainCondition }

func (d declaredCondition) Expected() any { return d.want }

// action is one action the world offers for a failed condition.
type action struct {
	world *world
	key   string
}

func (a *action) Conditions() []pabt.Conditions[cond] { return nil }

func (a *action) Effects() pabt.Effects {
	return pabt.Effects{
		effect{key: a.key, value: a.world.desired[a.key]},
		effect{key: "audit.last", value: credentials{
			User:     "operator",
			Password: "hunter2-planted",
			Endpoint: "harbor://manifest",
		}},
		effect{key: "session.token", value: secret{value: "planted-capability-value"}},
	}
}

func (a *action) Node() bt.Node {
	return bt.New(func([]bt.Node) (bt.Status, error) {
		if a.world.panicOn() {
			panic("evidence: the action panicked on purpose")
		}
		a.world.set(a.key, a.world.desired[a.key])
		return bt.Success, nil
	})
}

type effect struct {
	key   string
	value any
}

func (e effect) Key() any   { return e.key }
func (e effect) Value() any { return e.value }

// world is the application state the planner reads and the action writes.
type world struct {
	variables map[string]any
	desired   map[string]any
	// tick is the tick currently running, set by the target's loop rather than
	// counted from action executions. An execution count would depend on how many
	// alternatives the planner tried, which is exactly the sort of thing a
	// deterministic evidence run must not depend on.
	tick      int
	panicTick int
}

func newWorld(panicTick int) *world {
	w := &world{
		variables: map[string]any{},
		desired:   map[string]any{},
		panicTick: panicTick,
	}
	// Both conditions have a value their action can set, so the goal can settle.
	// Only one of them declares that value as an expectation, and the difference
	// between the two is what the page has to show.
	for _, condition := range w.conditions() {
		w.variables[condition.key] = nil
		w.desired[condition.key] = condition.want
	}
	return w
}

// untrustedKey is application text that is also markup. It is the canary for a
// client that builds the page by interpolating a recorded value.
const untrustedKey = `<img src=x onerror="window.__pabtPwned=1">harbor.manifest.sealed`

// conditions are the two preconditions. The first declares its expectation and
// the second does not; the second's key carries markup, because a client that
// interpolated a recorded value into the page would create an element and run its
// handler, while a client that sets text shows the characters.
func (w *world) conditions() []*plainCondition {
	return []*plainCondition{
		{key: "harbor.crate.delivered", want: true},
		{key: untrustedKey, want: "sealed"},
	}
}

func (w *world) goal() []pabt.Conditions[cond] {
	conditions := w.conditions()
	return []pabt.Conditions[cond]{{
		declaredCondition{conditions[0]},
		conditions[1],
	}}
}

func (w *world) Variable(key any) (any, error) {
	name, _ := key.(string)
	return w.variables[name], nil
}

func (w *world) Actions(failed cond) ([]pabt.Action[cond], error) {
	key, _ := failed.Key().(string)
	return []pabt.Action[cond]{&action{world: w, key: key}}, nil
}

func (w *world) set(key string, value any) { w.variables[key] = value }

func (w *world) panicOn() bool { return w.panicTick > 0 && w.tick == w.panicTick }

/* -------------------------------------------------------------- observation */

// observer reads the post-action world through the view it is handed, which is
// what makes the observed value a reading rather than a restatement of the
// declared one.
type observer struct{}

func (observer) ObserveExecution(
	req pabt.ActionObservationRequest,
) (pabt.ActionObservation, error) {
	out := pabt.ActionObservation{}
	for _, effect := range req.Effects {
		value, err := req.View.Variable(effect.Key)
		if err != nil {
			out.EffectValues = append(out.EffectValues, pabt.EffectObservation{
				EffectID: effect.ID, Err: &pabt.RecordedError{Type: "evidence", Message: err.Error()},
			})
			continue
		}
		out.EffectValues = append(out.EffectValues, pabt.EffectObservation{
			EffectID: effect.ID, Value: value, Known: true,
		})
	}
	return out, nil
}

// verifier concludes one relation per effect. Under the default live state view
// the planner marks none of them authoritative, which is the honest answer: two
// live reads can be different worlds. The page has to show that rather than
// presenting the claim as proven.
type verifier struct{}

func (verifier) VerifyTransitions(req pabt.TransitionRequest) ([]pabt.RelationVerdict, error) {
	out := make([]pabt.RelationVerdict, 0, len(req.Effects))
	for _, effect := range req.Effects {
		out = append(out, pabt.RelationVerdict{
			EffectID:        effect.ID,
			PostconditionID: req.PostconditionID,
			Result:          pabt.RelationDoesNotDecide,
			Method:          "evidence_target_compares_nothing",
		})
	}
	return out, nil
}
