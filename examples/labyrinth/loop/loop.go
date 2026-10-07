//go:build example

package loop

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

// Hz is the fixed simulation rate. The loop ticks at exactly 30 Hz.
const Hz = 30

// TickInterval is the fixed timestep duration.
const TickInterval = time.Second / Hz

// Command is a mutation submitted to the authoritative loop. It is applied
// at the next tick boundary, never mid-tick.
type Command func(w *world.World)

// Loop is the authoritative, single-writer game loop. All world mutations
// happen on one goroutine (the writer). Readers observe state only through
// Snapshot, which returns a defensive copy published atomically after each
// tick.
type Loop struct {
	world *world.World

	// Command queue. Producers submit from any goroutine; the writer drains
	// at tick boundaries.
	cmdMu   sync.Mutex
	cmdList []Command

	// Snapshot pointer. Published after each tick. Readers load atomically.
	snapshot atomic.Pointer[world.Snapshot]

	// Lifecycle.
	stopCh chan struct{}
	doneCh chan struct{}

	// Round counter for tests.
	round atomic.Int64
}

// New creates a loop over the given world and publishes the initial snapshot.
func New(w *world.World) *Loop {
	l := &Loop{
		world:  w,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
	l.snapshot.Store(w.Copy())
	return l
}

// Submit enqueues a command. It is safe to call from any goroutine. The
// command is applied at the next tick boundary, not immediately.
func (l *Loop) Submit(cmd Command) {
	l.cmdMu.Lock()
	l.cmdList = append(l.cmdList, cmd)
	l.cmdMu.Unlock()
}

// Snapshot returns the latest defensive-copy snapshot. It is safe to call
// from any goroutine at any time.
func (l *Loop) Snapshot() *world.Snapshot {
	return l.snapshot.Load()
}

// Round returns the current round number (number of ticks completed).
func (l *Loop) Round() int64 {
	return l.round.Load()
}

// Run starts the fixed-rate loop and blocks until the context is cancelled
// or Stop is called. It must be called exactly once, typically as `go l.Run(ctx)`.
func (l *Loop) Run(ctx context.Context) {
	defer close(l.doneCh)

	ticker := time.NewTicker(TickInterval)
	defer ticker.Stop()

	// Drift correction: track the ideal absolute time so that accumulated
	// scheduler jitter does not drift the long-run frequency.
	next := time.Now().Add(TickInterval)

	for {
		select {
		case <-ctx.Done():
			return
		case <-l.stopCh:
			return
		case <-ticker.C:
			l.tick()

			// Reset with drift correction.
			now := time.Now()
			next = next.Add(TickInterval)
			if now.After(next) {
				// We fell behind; resync to avoid a burst of catch-up ticks.
				next = now.Add(TickInterval)
			}
			delay := time.Until(next)
			if delay < 0 {
				delay = 0
			}
			ticker.Reset(delay)
		}
	}
}

// Stop signals the loop to stop and waits for it to finish.
func (l *Loop) Stop() {
	close(l.stopCh)
	<-l.doneCh
}

// tick is one authoritative step: drain commands, advance the world, publish
// a snapshot. Only the Run goroutine calls this.
func (l *Loop) tick() {
	// Drain the command queue.
	l.cmdMu.Lock()
	cmds := l.cmdList
	l.cmdList = nil
	l.cmdMu.Unlock()

	for _, cmd := range cmds {
		cmd(l.world)
	}

	// Advance the world by one fixed timestep.
	l.world.Step(TickInterval.Seconds())

	// Publish a defensive-copy snapshot for readers.
	l.snapshot.Store(l.world.Copy())
	l.round.Store(int64(l.world.Round))
}

// TickOnce runs a single tick synchronously. This is the same logic as the
// timer-driven path, exposed for deterministic testing without a wall clock.
func (l *Loop) TickOnce() {
	l.tick()
}
