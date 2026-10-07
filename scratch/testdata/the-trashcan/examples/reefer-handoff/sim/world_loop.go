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
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/joeycumines/go-eventloop"
)

// Mode selects who advances the round counter.
type Mode int

const (
	// ModeDeterministic leaves rounds to the host, which calls BeginRound. It is
	// the mode a replay uses, because nothing here depends on elapsed time.
	ModeDeterministic Mode = iota

	// ModeRealtime paces rounds on the loop's own timer. It is not
	// timing-deterministic, and the contract says so.
	ModeRealtime
)

// RoundInterval is how long one round takes in realtime mode, so the vessel's
// cutoff is 24 seconds away.
const RoundInterval = 200 * time.Millisecond

// startTimeout bounds the wait for the loop to reach its running state. It is a
// guard against a hang, not a timing dependency: a healthy loop reaches the
// state in microseconds.
const startTimeout = 5 * time.Second

// Result is what a producer learns about a command it submitted.
//
// Abandoned is set instead of a Receipt when the loop accepted the command and
// then withdrew it without reducing it, either because a cancel arrived first or
// because the run was torn down. It is deliberately not shaped like a receipt:
// nothing was decided, so there is no outcome, no sequence, and nothing to record
// in the world's ledger. A producer that gets one must not read it as a refusal
// the world reached.
type Result struct {
	Receipt   Receipt
	Events    []WorldEvent
	Abandoned *Abandoned
}

var (
	// ErrNotStarted is returned by every entry point before Start has succeeded. A
	// task admitted to a loop that has not entered Run does not run on the callback
	// owner, so the loop refuses the work rather than quietly doing it elsewhere.
	ErrNotStarted = errors.New("sim: world loop has not started")

	// ErrLoopTerminated is returned once the loop has stopped admitting.
	ErrLoopTerminated = errors.New("sim: world loop has terminated")
)

// WorldLoop is the single owner of the world.
//
// One goroutine, the loop's callback owner, mutates the world. Producers
// anywhere admit work with Submit and learn the outcome from the channel Submit
// returns. Nothing a producer does reaches the world's state directly, and
// nothing the loop does reaches a producer except by writing to a channel that
// is already buffered, so a callback can never be held up by a reader that has
// gone away.
//
// The loop is the admission point, not a second authority. It does not decide
// whether a command is acceptable: the reducer does, from the world as it stands
// when the command is reduced, and its typed reasons are the ones a producer and
// a debugger see. A transport-level pre-check that produced different reasons for
// the same physical situation would make the reason for a refusal depend on
// timing, which is the opposite of what a record is for.
type WorldLoop struct {
	loop *eventloop.Loop
	mode Mode

	// mu guards every field below, because producers on other goroutines read
	// them. The owner takes it for the duration of the pure Reduce and the
	// bookkeeping around it; a reader copies and releases.
	//
	// Nothing that blocks may run while it is held. A callback reduces a pure
	// function and writes to a buffered channel, and that is all.
	mu sync.Mutex

	world World

	// pending holds one result channel per admitted command, keyed by
	// idempotency key. Each is buffered to one, so the owner that settles it
	// never blocks.
	pending map[string]chan Result

	// inFlight holds every command admitted and not yet settled, so shutdown can
	// reconcile the ones that never will be.
	inFlight map[string]Command

	// cancelled holds keys whose command was withdrawn before the owner reduced
	// it.
	cancelled map[string]bool

	// events buffers what the round boundaries and the exogenous changes have
	// published since the last drain. It is appended to, never replaced, so an
	// event that ends the run is not thrown away by the next boundary that has
	// nothing to say.
	events []WorldEvent

	running   bool
	admitting bool
	stopped   bool
	runErr    error
	timer     eventloop.TimerID
	timed     bool

	// drained closes once the loop's cleanup barrier has closed, so a second
	// concurrent Shutdown joins the same teardown instead of returning early, and
	// drainOnce makes that close happen exactly once however the callers overlap.
	drained     chan struct{}
	drainOnce   sync.Once
	shutdownErr error
}

// NewWorldLoop builds the loop. It admits nothing: a task handed to a loop that
// has not entered Run does not run on the callback owner, so the owner has to
// exist before any world work is accepted.
func NewWorldLoop(mode Mode) (*WorldLoop, error) {
	l, err := eventloop.New(eventloop.WithAutoExit(false))
	if err != nil {
		return nil, fmt.Errorf("sim: creating the world loop: %w", err)
	}
	return &WorldLoop{
		loop:      l,
		mode:      mode,
		world:     NewWorld(),
		pending:   make(map[string]chan Result),
		inFlight:  make(map[string]Command),
		cancelled: make(map[string]bool),
		drained:   make(chan struct{}),
	}, nil
}

// Start begins the loop's execution and returns once the owner is running, so a
// caller that gets past it knows the work it admits afterwards reaches the
// callback owner.
func (w *WorldLoop) Start() error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return fmt.Errorf("sim: world loop already started")
	}
	w.running = true
	w.mu.Unlock()

	go func() {
		if err := w.loop.Run(context.Background()); err != nil {
			w.recordRunError(err)
		}
	}()

	// Wait for the owner rather than assume it. Run blocks until the loop exits,
	// so a probe is the only way to know it started.
	deadline := time.Now().Add(startTimeout)
	for {
		switch w.loop.State() {
		case eventloop.StateRunning, eventloop.StateSleeping:
			w.mu.Lock()
			w.admitting = true
			stopped, err := w.stopped, w.runErr
			w.mu.Unlock()
			if stopped {
				// Run exited before it ever reached a running state, so the
				// object must not go on reporting itself alive.
				w.failStart(err)
				return fmt.Errorf("sim: world loop exited before running: %v", err)
			}
			if w.mode == ModeRealtime {
				w.armTimer()
			}
			return nil
		}
		if time.Now().After(deadline) {
			state := w.loop.State()
			err := fmt.Errorf("world loop did not reach the running state, last was %v", state)
			w.failStart(err)
			return fmt.Errorf("sim: %w", err)
		}
		time.Sleep(time.Millisecond)
	}
}

// failStart puts the loop back into a state that admits nothing and stops the
// owner goroutine. A Start that never reached an owner must not leave the object
// looking alive, and under WithAutoExit(false) a Run goroutine that is not stopped
// runs for the life of the process.
func (w *WorldLoop) failStart(err error) {
	w.mu.Lock()
	w.admitting = false
	w.runErr = err
	w.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), startTimeout)
	defer cancel()
	if shutdownErr := w.loop.Shutdown(ctx); shutdownErr != nil {
		// Shutdown is the graceful path and can refuse a loop that never ran.
		// Close is the immediate one, and it is what guarantees the goroutine is
		// gone.
		_ = w.loop.Close()
	}
	<-w.loop.Done()
}

// recordRunError keeps the reason a running loop exited, so a caller that finds a
// loop admitting nothing can say why rather than reporting a nil.
func (w *WorldLoop) recordRunError(err error) {
	w.mu.Lock()
	w.runErr = err
	w.mu.Unlock()
}

// Submit admits a command from any goroutine and returns the channel its result
// will arrive on. The channel is buffered, so a producer may abandon it.
//
// Submitting a key that is already admitted and unsettled returns the same
// channel rather than admitting the work twice, so a producer that retries
// without waiting still ends up with one result.
//
// Ordering is the order in which Submit calls reach the loop, which is the order
// the loop's external task phase drains. Two producers racing each other are not
// ordered relative to one another, and nothing in this package promises they are.
func (w *WorldLoop) Submit(c Command) (<-chan Result, error) {
	w.mu.Lock()
	switch {
	case !w.running:
		w.mu.Unlock()
		return nil, ErrNotStarted
	case !w.admitting:
		w.mu.Unlock()
		return nil, ErrLoopTerminated
	}
	if c.IdempotencyKey == "" {
		w.mu.Unlock()
		return nil, fmt.Errorf("sim: command %q has no idempotency key", c.ID)
	}
	if ch, dup := w.pending[c.IdempotencyKey]; dup {
		w.mu.Unlock()
		return ch, nil
	}
	ch := make(chan Result, 1)
	w.pending[c.IdempotencyKey] = ch
	w.inFlight[c.IdempotencyKey] = c
	w.mu.Unlock()

	if err := w.loop.Submit(func() { w.reduceOnOwner(c, ch) }); err != nil {
		w.abandon(c.IdempotencyKey, ch, ReasonAbandoned)
	}
	return ch, nil
}

// Cancel withdraws an admitted command the owner has not reduced yet, and reports
// whether the withdrawal won. A cancel that returns false lost the race: the
// command had already settled, and its receipt stands.
func (w *WorldLoop) Cancel(key string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, known := w.pending[key]; !known {
		return false
	}
	w.cancelled[key] = true
	return true
}

// reduceOnOwner is the only place the world changes. It runs on the callback
// owner, holds the lock for the pure Reduce and its bookkeeping, and writes into a
// channel that is already buffered.
func (w *WorldLoop) reduceOnOwner(c Command, ch chan Result) {
	if !w.loop.IsCallbackOwner() {
		w.abandon(c.IdempotencyKey, ch, ReasonAbandoned)
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	// Once the loop has stopped admitting, a queued command is not reduced. The
	// terminal drain still runs the tasks already queued, and without this the
	// world would commit a change for a command its producer was already told
	// never happened. Reconciliation has settled these keys by then, so the
	// finishLocked guard below makes the two agree.
	if !w.admitting {
		return
	}

	if w.cancelled[c.IdempotencyKey] {
		delete(w.cancelled, c.IdempotencyKey)
		w.finishLocked(c.IdempotencyKey, ch, Result{Abandoned: &Abandoned{
			CommandID: c.ID, Reason: ReasonAbandoned,
		}})
		return
	}

	// Idempotency is Reduce's to answer. The loop does not pre-check it, because
	// two owners of one rule is how they start disagreeing, and Reduce checks the
	// ledger before anything else anyway.

	// A command claiming to come from a world this one has not reached is a
	// transport fault, not a domain refusal. Nothing correct produces one.
	if w.world.FromTheFuture(c) {
		w.finishLocked(c.IdempotencyKey, ch, Result{Abandoned: &Abandoned{
			CommandID: c.ID, Reason: ReasonFromTheFuture,
		}})
		return
	}

	world, receipt, events := Reduce(w.world, c)
	w.world = world
	w.finishLocked(c.IdempotencyKey, ch, Result{Receipt: receipt, Events: events})
}

// finishLocked removes a command from the in-flight set and delivers its result.
// The caller holds the lock, and this is the channel's only writer, so the send
// cannot block — with one exception that matters at shutdown.
//
// Reconciliation may have settled a key already, from the caller's goroutine,
// before the owner's queued task gets its turn. Sending again would block forever
// on a channel whose single slot is already taken, and the loop would never finish
// draining. So a key that is no longer outstanding is dropped here rather than
// answered twice, which is what makes "settled exactly once" hold across a
// teardown.
func (w *WorldLoop) finishLocked(key string, ch chan Result, r Result) {
	if _, outstanding := w.pending[key]; !outstanding {
		return
	}
	delete(w.inFlight, key)
	delete(w.pending, key)
	ch <- r
}

// abandon closes out a command that will never be reduced, so its producer is not
// left waiting on a result that cannot arrive. It records no receipt: nothing was
// decided, so there is nothing for the world's ledger to hold.
func (w *WorldLoop) abandon(key string, ch chan Result, reason Reason) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, still := w.pending[key]; !still {
		return
	}
	id := ""
	if c, ok := w.inFlight[key]; ok {
		id = c.ID
	}
	w.finishLocked(key, ch, Result{Abandoned: &Abandoned{CommandID: id, Reason: reason}})
}

// Snapshot returns the world as it stands, copied under the lock, so a reader
// gets one whole world rather than a mixture of two.
func (w *WorldLoop) Snapshot() World {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.world.clone()
}

// Abandoned reports the commands that are admitted and not yet settled. It is
// therefore a reading of a live loop: once Shutdown has run, reconciliation has
// settled every one of them and the answer is always empty. It exists to be
// sampled before a teardown, which is what tells a caller whether the loop is
// idle or still carrying work.
func (w *WorldLoop) Abandoned() []Abandoned {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]Abandoned, 0, len(w.inFlight))
	for _, c := range w.inFlight {
		out = append(out, Abandoned{CommandID: c.ID, Reason: ReasonAbandoned})
	}
	return out
}

// DrainEvents returns and clears what the round boundaries and the exogenous
// changes have published since the last drain.
func (w *WorldLoop) DrainEvents() []WorldEvent {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := w.events
	w.events = nil
	return out
}

// armTimer schedules the next realtime round. The library has no repeating
// variant, so the callback re-arms itself and the handle is kept so shutdown can
// cancel it.
func (w *WorldLoop) armTimer() {
	id, err := w.loop.ScheduleTimer(RoundInterval, func() { w.realtimeRound() })
	if err != nil {
		return
	}
	w.mu.Lock()
	w.timer = id
	w.timed = true
	w.mu.Unlock()
}

func (w *WorldLoop) realtimeRound() {
	w.BeginRound()
	w.mu.Lock()
	admitting := w.admitting
	w.mu.Unlock()
	if admitting {
		w.armTimer()
	}
}

// BeginRound advances the host's round counter on the owner. Deterministic mode
// calls it directly; realtime mode calls it from the timer.
func (w *WorldLoop) BeginRound() {
	if err := w.loop.Submit(w.beginRoundOnOwner); err != nil {
		return
	}
}

func (w *WorldLoop) beginRoundOnOwner() {
	if !w.loop.IsCallbackOwner() {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.admitting {
		return
	}
	world, events := BeginRound(w.world)
	w.world = world
	w.events = append(w.events, events...)
}

// Change is an exogenous world change: the wind controller closing a lane, the
// power desk pulling a plug. It is admitted through the same path a command
// takes, so an exogenous change and a command are ordered against each other by
// the loop rather than by whichever goroutine won a race.
//
// The function runs on the owner with the loop's lock held, so it must be a pure
// transition over the world. It must not block, perform I/O, or call back into
// the loop: the lock is not reentrant.
func (w *WorldLoop) Change(change func(World) (World, []WorldEvent)) (<-chan []WorldEvent, error) {
	w.mu.Lock()
	switch {
	case !w.running:
		w.mu.Unlock()
		return nil, ErrNotStarted
	case !w.admitting:
		w.mu.Unlock()
		return nil, ErrLoopTerminated
	}
	w.mu.Unlock()

	out := make(chan []WorldEvent, 1)
	if err := w.loop.Submit(func() { w.changeOnOwner(change, out) }); err != nil {
		out <- nil
	}
	return out, nil
}

func (w *WorldLoop) changeOnOwner(change func(World) (World, []WorldEvent), out chan []WorldEvent) {
	if !w.loop.IsCallbackOwner() {
		out <- nil
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.admitting {
		out <- nil
		return
	}
	world, events := change(w.world)
	w.world = world
	w.events = append(w.events, events...)
	out <- events
}

// Shutdown stops admitting, cancels the realtime timer, and reconciles anything
// still in flight before the loop is closed. It returns once the loop's cleanup
// barrier has closed, so a caller knows no work is outstanding afterwards — and a
// second caller joins the same barrier rather than returning early, because "no
// work is outstanding" is a claim about the whole teardown and not about the
// caller that happened to arrive first.
//
// It is safe to call more than once, which matters because a test's cleanup and
// its own teardown both want it.
func (w *WorldLoop) Shutdown(ctx context.Context) error {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return nil
	}
	if w.stopped {
		drained := w.drained
		w.mu.Unlock()
		select {
		case <-drained:
			// A joining caller learns how the teardown went, not merely that it
			// is over. Reporting nil here would make a failed teardown
			// indistinguishable from a successful one.
			w.mu.Lock()
			err := w.shutdownErr
			w.mu.Unlock()
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	w.admitting = false
	w.stopped = true
	timed, timer := w.timed, w.timer
	w.mu.Unlock()

	if timed {
		// A timer that already fired reports an error rather than succeeding
		// silently, and a fired timer is not something to fail a shutdown over.
		_ = w.loop.CancelTimer(timer)
	}

	// Reconcile before stopping the loop, so a producer is answered before the
	// owner stops draining. The drain then finds every key already settled and
	// reduces nothing.
	w.reconcile()

	err := w.stopLoop(ctx)
	w.closeDrained()
	return err
}

// stopLoop stops the loop and waits for its barrier, recording the outcome for any
// caller that joins later.
func (w *WorldLoop) stopLoop(ctx context.Context) error {
	err := w.loop.Shutdown(ctx)
	if err == nil {
		select {
		case <-w.loop.Done():
		case <-ctx.Done():
			err = ctx.Err()
		}
	}
	if err != nil {
		err = fmt.Errorf("sim: shutting the world loop down: %w", err)
	}
	w.mu.Lock()
	w.shutdownErr = err
	w.mu.Unlock()
	return err
}

func (w *WorldLoop) closeDrained() {
	w.drainOnce.Do(func() { close(w.drained) })
}

// reconcile settles every command still in flight. Without this a producer would
// wait on a result that cannot arrive, because the loop is no longer reducing.
func (w *WorldLoop) reconcile() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for key, ch := range w.pending {
		id := ""
		if c, ok := w.inFlight[key]; ok {
			id = c.ID
		}
		w.finishLocked(key, ch, Result{Abandoned: &Abandoned{CommandID: id, Reason: ReasonAbandoned}})
	}
	w.cancelled = make(map[string]bool)
}
