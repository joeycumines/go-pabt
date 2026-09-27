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
// Cancelled reports that the loop accepted the command and then withdrew it
// because a cancel for the same key succeeded first.
type Result struct {
	Receipt   Receipt
	Events    []WorldEvent
	Cancelled bool
}

// ErrNotStarted is returned by every entry point before Start has succeeded. A
// task admitted to a loop that has not entered Run does not run on the callback
// owner, so the loop refuses the work rather than quietly doing it elsewhere.
var ErrNotStarted = errors.New("sim: world loop has not started")

// ErrLoopTerminated is returned once the loop has stopped admitting.
var ErrLoopTerminated = errors.New("sim: world loop has terminated")

// WorldLoop is the single owner of the world.
//
// One goroutine, the loop's callback owner, mutates the world. Producers
// anywhere admit work with Submit and learn the outcome from the channel Submit
// returns. Nothing a producer does reaches the world's state directly, and
// nothing the loop does reaches a producer except by writing to a channel that
// is already buffered, so a callback can never be held up by a reader that has
// gone away.
//
// Ordering is the loop's external task phase, which is first-in first-out. A
// command is therefore reduced in the order it was admitted, and a command
// submitted after another has been reduced sees the world the first one left.
type WorldLoop struct {
	loop *eventloop.Loop
	mode Mode

	// mu guards every field below, because producers on other goroutines read
	// them. The owner takes it for the duration of the pure Reduce and the
	// bookkeeping around it; a reader copies and releases. Nothing slow happens
	// while it is held, so a callback cannot be blocked by an external reader for
	// longer than one reduction.
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

	// lastEvents holds what the most recent round boundary published. A boundary
	// settles no command, so it has no channel and is read by draining.
	lastEvents []WorldEvent

	running   bool
	admitting bool
	stopped   bool
	timer     eventloop.TimerID
	timed     bool
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
	w.admitting = true
	w.mu.Unlock()

	go func() { _ = w.loop.Run(context.Background()) }()

	// Wait for the owner rather than assume it. Run blocks until the loop exits,
	// so a probe is the only way to know it started.
	deadline := time.Now().Add(startTimeout)
	for {
		switch state := w.loop.State(); state {
		case eventloop.StateRunning, eventloop.StateSleeping:
			w.started()
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("sim: world loop did not reach the running state, last was %v", w.loop.State())
		}
		time.Sleep(time.Millisecond)
	}
}

func (w *WorldLoop) started() {
	if w.mode == ModeRealtime {
		w.armTimer()
	}
}

// Submit admits a command from any goroutine and returns the channel its result
// will arrive on. The channel is buffered, so a producer may abandon it.
//
// Submitting a key that is already admitted and unsettled returns the same
// channel rather than admitting the work twice, so a producer that retries
// without waiting still ends up with one result.
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
		w.abandon(c.IdempotencyKey, ch)
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
		w.abandon(c.IdempotencyKey, ch)
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.cancelled[c.IdempotencyKey] {
		delete(w.cancelled, c.IdempotencyKey)
		w.finishLocked(c.IdempotencyKey, ch, Result{Cancelled: true})
		return
	}

	// A command decided against a slot, lane, or tag that has since moved is
	// refused here, before Reduce, so it cannot mutate anything.
	if reason := w.world.CheckExpectation(c); reason != ReasonNone {
		w.finishLocked(c.IdempotencyKey, ch, Result{Receipt: w.staleReceipt(c, reason)})
		return
	}

	world, receipt, events := Reduce(w.world, c)
	w.world = world
	w.finishLocked(c.IdempotencyKey, ch, Result{Receipt: receipt, Events: events})
}

// staleReceipt builds the receipt for a command the loop refused because the
// state it was decided against has moved. It is counted in neither sequence: no
// command reached the world, so there is nothing to order.
func (w *WorldLoop) staleReceipt(c Command, reason Reason) Receipt {
	return Receipt{
		ID:             c.ID,
		IdempotencyKey: c.IdempotencyKey,
		Kind:           c.Kind,
		Tractor:        c.Tractor,
		WorldSeq:       w.world.WorldSeq(),
		WorldRevision:  w.world.Revision(),
		Outcome:        OutcomeRefused,
		Reason:         reason,
	}
}

// finishLocked removes a command from the in-flight set and delivers its result.
// The caller holds the lock, and this is the channel's only writer, so the send
// cannot block — with one exception that matters at shutdown.
//
// Reconciliation may have settled a key already, from the caller's goroutine,
// before the owner's queued task gets its turn in the terminal drain. Sending
// again would block forever on a channel whose single slot is already taken, and
// the loop would never finish draining. So a key that is no longer outstanding is
// dropped here rather than answered twice, which is what makes "settled exactly
// once" hold across a teardown.
func (w *WorldLoop) finishLocked(key string, ch chan Result, r Result) {
	if _, outstanding := w.pending[key]; !outstanding {
		return
	}
	delete(w.inFlight, key)
	delete(w.pending, key)
	ch <- r
}

// abandon closes out a command that will never be reduced, so its producer is not
// left waiting on a result that cannot arrive.
func (w *WorldLoop) abandon(key string, ch chan Result) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, still := w.pending[key]; !still {
		return
	}
	r := Result{Receipt: Receipt{Outcome: OutcomeRefused, Reason: ReasonAbandoned}}
	if c, ok := w.inFlight[key]; ok {
		r.Receipt.ID = c.ID
		r.Receipt.IdempotencyKey = c.IdempotencyKey
		r.Receipt.Kind = c.Kind
		r.Receipt.Tractor = c.Tractor
	}
	r.Receipt.WorldSeq = w.world.WorldSeq()
	r.Receipt.WorldRevision = w.world.Revision()
	w.finishLocked(key, ch, r)
}

// Snapshot returns the world as it stands, copied under the lock, so a reader
// gets one whole world rather than a mixture of two.
func (w *WorldLoop) Snapshot() World {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.world.clone()
}

// Abandoned reports the commands that were admitted and never settled. A clean
// run leaves it empty; a run torn down with work in flight leaves one entry per
// stranded command, which is the reconciliation the contract requires.
func (w *WorldLoop) Abandoned() []Abandoned {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]Abandoned, 0, len(w.inFlight))
	for _, c := range w.inFlight {
		out = append(out, Abandoned{CommandID: c.ID, Reason: ReasonAbandoned})
	}
	return out
}

// DrainRoundEvents returns and clears what the most recent round boundary
// published. A boundary settles no command, so it has no result channel.
func (w *WorldLoop) DrainRoundEvents() []WorldEvent {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := w.lastEvents
	w.lastEvents = nil
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
	// Appended, not replaced. A boundary that ends the run publishes the terminal
	// event, and the boundary after it publishes nothing — replacing here would
	// throw the end of the run away before a reader had drained it.
	w.lastEvents = append(w.lastEvents, events...)
}

// Apply runs an exogenous change — the wind controller, the power desk — as a
// world event on the owner. It is the same admission path a command takes, so an
// exogenous change and a command are ordered against each other by the loop
// rather than by whichever goroutine won a race.
func (w *WorldLoop) Apply(change func(World) (World, []WorldEvent)) (<-chan []WorldEvent, error) {
	w.mu.Lock()
	if !w.admitting {
		w.mu.Unlock()
		return nil, ErrLoopTerminated
	}
	w.mu.Unlock()

	out := make(chan []WorldEvent, 1)
	if err := w.loop.Submit(func() { w.applyOnOwner(change, out) }); err != nil {
		out <- nil
	}
	return out, nil
}

func (w *WorldLoop) applyOnOwner(change func(World) (World, []WorldEvent), out chan []WorldEvent) {
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
	w.lastEvents = events
	out <- events
}

// Shutdown stops admitting, cancels the realtime timer, and reconciles anything
// still in flight before the loop is closed. It returns once the loop's cleanup
// barrier has closed, so a caller knows no work is outstanding afterwards.
//
// It is safe to call more than once, which matters because a test's cleanup and
// its own teardown both want it and the second call must not fail on a loop that
// has already terminated.
func (w *WorldLoop) Shutdown(ctx context.Context) error {
	w.mu.Lock()
	if !w.running || w.stopped {
		w.mu.Unlock()
		return nil
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

	w.reconcile()

	if err := w.loop.Shutdown(ctx); err != nil {
		return fmt.Errorf("sim: shutting the world loop down: %w", err)
	}
	select {
	case <-w.loop.Done():
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// reconcile settles every command still in flight. Without this a producer would
// wait on a result that cannot arrive, because the loop is about to stop
// reducing. It runs on the caller's goroutine rather than a callback, because by
// this point the loop is no longer admitting and a callback would not run.
func (w *WorldLoop) reconcile() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for key, ch := range w.pending {
		r := Result{Receipt: Receipt{Outcome: OutcomeRefused, Reason: ReasonAbandoned}}
		if c, ok := w.inFlight[key]; ok {
			r.Receipt.ID = c.ID
			r.Receipt.IdempotencyKey = c.IdempotencyKey
			r.Receipt.Kind = c.Kind
			r.Receipt.Tractor = c.Tractor
		}
		r.Receipt.WorldSeq = w.world.WorldSeq()
		r.Receipt.WorldRevision = w.world.Revision()
		w.finishLocked(key, ch, r)
	}
	w.cancelled = make(map[string]bool)
}
