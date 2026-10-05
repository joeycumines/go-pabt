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

// Command reefer-handoff runs the Reefer Handoff scenario end to end: one
// serialized world, three independent PA-BT plan runners, and one durable
// debugger server per tractor, so a browser can follow a world event to the
// plan tick and command that caused it without ever reading a live plan.
//
// The drive is the calm seeded manifest, on the same tick contract the tests
// use: each round ticks every active runner to settlement, advances the
// host's round, and drains the boundary events into every publisher so the
// next committed record carries them. REEFER-2's unreadable tag forces a scan
// into the tree, and the capacity-one bay serializes the handoffs. The fixed
// incident fires at its own rounds through loop.Change, before the round's
// commands, so a command that meets the closed lane is refused in the same
// round the wind controller closed it.
//
// Each tractor's history lands in its own store file under -dir — one store
// and one run per tractor per DD-35 — and each store is served by exactly one
// durable server, because NewDurableServer binds one server to one store. The
// browser address and the capability are printed per tractor. Unlike the
// evidence target, which hands its token to a harness through a file, this
// binary's audience is the operator at the terminal: the servers bind loopback
// only, the token is the second boundary against other local readers, and the
// terminal is where an operator is meant to see it.
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

	"github.com/joeycumines/go-pabt/examples/reefer-handoff/logic"
	"github.com/joeycumines/go-pabt/examples/reefer-handoff/sim"
	"github.com/joeycumines/go-pabt/examples/reefer-handoff/tui"
	"github.com/joeycumines/go-pabt/pabtdebug"
)

// tractor is one runner, its publisher, and the server serving its history.
// The three always move together: the publisher is the runner's only route to
// durable history, and the server is the publisher's only route to a live
// reader.
type tractor struct {
	tug    sim.TractorID
	pub    *logic.Publisher
	server *pabtdebug.Server
	runner *logic.Runner
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "reefer-handoff:", err)
		os.Exit(1)
	}
}

func run() error {
	dir := flag.String("dir", filepath.Join("scratch", "reefer-handoff"),
		"the directory the per-tractor store files land in")
	flag.Parse()

	// The handler goes in before anything that can block, so a signal during
	// the run reaches the ordered shutdown rather than killing the process
	// with the stores half-written.
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	if err := os.MkdirAll(*dir, 0o755); err != nil {
		return fmt.Errorf("making the store directory: %w", err)
	}

	loop, err := sim.NewWorldLoop(sim.ModeDeterministic)
	if err != nil {
		return err
	}
	if err := loop.Start(); err != nil {
		return err
	}

	tractors, err := wire(ctx, *dir, loop)
	if err != nil {
		_ = loop.Shutdown(context.Background())
		return err
	}
	defer teardown(loop, tractors)

	served := make(chan error, len(tractors))
	for _, t := range tractors {
		go func(t *tractor) { served <- t.server.Start() }(t)
	}

	// The attach points are printed before the first tick, so a browser can
	// be open while the records arrive.
	for _, t := range tractors {
		fmt.Printf("%s  http://%s/debug/pabt/ui  capability %s\n",
			t.tug, t.server.Addr(), t.server.Capability())
	}

	ctrl := NewController()

	// The operations monitor renders the same world/plan trace the browser
	// shows. It runs on its own goroutine and routes keyboard input through
	// the controller. If there is no TTY (headless CI), it fails gracefully
	// and the run continues without it.
	views := make([]tui.TractorView, len(tractors))
	for i, t := range tractors {
		views[i] = tui.TractorView{ID: t.tug, Addr: t.server.Addr(), Cap: t.server.Capability()}
	}
	mon := tui.NewMonitor(loop, views, ctrl)
	go func() {
		if err := mon.Run(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "monitor: %v\n", err)
		}
	}()

	if err := drive(ctx, loop, tractors, ctrl); err != nil {
		if errors.Is(err, context.Canceled) {
			// A signal during the run is an operator stopping the demo, not
			// a failure: the ordered teardown still releases the world, the
			// servers, and the stores on the way out.
			fmt.Println("stopping")
			return nil
		}
		return err
	}

	// The world settled, but a receipt may have attached after its runner's
	// last tick — the completing Place finishes the world and drive's
	// top-of-round check exits before that tractor ticks again — and a receipt
	// no tick drains is a reduction no store ever hears. Drain and flush while
	// the servers are still up, so a browser holding the run sees the finished
	// trace rather than a submission whose answer never arrived.
	for _, t := range tractors {
		settle(t.runner)
		t.runner.DrainSettled()
		if err := t.pub.Flush(); err != nil {
			return fmt.Errorf("%s flushing the final receipts: %w", t.tug, err)
		}
	}

	// Alive after the run until terminated, which is what lets a browser
	// attach, follow the causal links across the committed history, and be
	// torn down without racing the run's own output.
	select {
	case <-ctx.Done():
		fmt.Println("stopping")
	case err := <-served:
		if err != nil && !errors.Is(err, os.ErrClosed) {
			return fmt.Errorf("serve: %w", err)
		}
	}
	return nil
}

// wire builds the three tractors' publishers, servers, and runners in the
// evidence target's construction order: open the store, build the server on
// it, attach the announce, and only then let a generation open. A session
// that opened without an announce would publish durably but silently, which
// is the split-brain a live reader must never see. On failure it releases
// everything it already made.
func wire(ctx context.Context, dir string, loop *sim.WorldLoop) ([]*tractor, error) {
	var made []*tractor
	release := func() {
		for _, t := range made {
			_ = t.server.Close()
			_ = t.pub.Close()
		}
	}
	for _, tug := range []sim.TractorID{sim.Tug1, sim.Tug2, sim.Tug3} {
		pub := logic.NewPublisher(tug, dir)
		if err := pub.Open(ctx); err != nil {
			release()
			return nil, err
		}
		server, err := pabtdebug.NewDurableServer(pub.Store(), pabtdebug.DurableServerConfig{})
		if err != nil {
			release()
			_ = pub.Close()
			return nil, err
		}
		pub.AttachAnnounce(server.Announce)
		runner := logic.New(tug, loop)
		runner.AttachPublisher(pub)
		made = append(made, &tractor{tug: tug, pub: pub, server: server, runner: runner})
	}
	return made, nil
}

// drive ticks the three runners in a fixed serial order, one round at a time,
// until the world settles. The order is the host's choice and it is what
// makes the demo deterministic: the world only ever changes through the
// owner, and the owner's schedule here is tick, settle, tick, settle, round
// boundary, drain — every round, the same way, for every tractor.
func drive(ctx context.Context, loop *sim.WorldLoop, tractors []*tractor, ctrl *Controller) error {
	active := map[sim.TractorID]bool{sim.Tug1: true, sim.Tug2: true, sim.Tug3: true}
	// The guard bounds the demo, not the world: the reducer reaches Cutoff on
	// its own, and a run that somehow outlives the cutoff plus a margin is a
	// bug to report rather than a horizon to wait on.
	for rounds := 0; ; rounds++ {
		// The controller mediates between the operator (TUI keyboard or future
		// API) and this loop. It does NOT touch WorldLoop.admitting, which
		// remains the lifecycle/shutdown gate. Pause freezes new admission by
		// skipping the tick/boundary cycle; already-queued commands in the
		// eventloop still reduce because they were admitted before the freeze.
		// Step consumes exactly one permit, allowing one round through before
		// re-freezing. Speed scales the pacing interval.
		paused, stepOne, speed, quit := ctrl.Check()
		if quit {
			return context.Canceled
		}
		if paused && !stepOne {
			// While paused, sleep proportional to speed and re-check. Don't
			// count paused iterations against the round guard.
			pace := time.Duration(float64(sim.RoundInterval) / speed)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(pace):
			}
			rounds--
			continue
		}

		world := loop.Snapshot()
		if world.Phase() == sim.PhaseComplete || world.Phase() == sim.PhaseCutoff {
			printSummary(world)
			return nil
		}
		if rounds > sim.CutoffRound+5 {
			return fmt.Errorf("the run did not settle within %d rounds; phase %s",
				rounds, world.Phase())
		}
		// The scripted incident applies at the round its controller acts, and
		// only there: each Apply is idempotent, and waiting on the change's
		// channel means the round's commands queue behind it, so a MoveTo that
		// crosses the western approach is refused by the closure itself. The
		// events join the boundary drain below, which is how every publisher
		// records them.
		for _, incident := range sim.Scripted(world.Round()) {
			out, err := loop.Change(incident.Apply)
			if err != nil {
				return fmt.Errorf("incident at round %d: %w", world.Round(), err)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-out:
			}
		}
		live := false
		for _, t := range tractors {
			if !active[t.tug] {
				continue
			}
			live = true
			out, err := t.runner.Tick()
			if err != nil {
				return fmt.Errorf("%s tick: %w", t.tug, err)
			}
			settle(t.runner)
			switch out {
			case logic.OutcomeBlocked:
				active[t.tug] = false
				fmt.Printf("%s blocked on %s\n", t.tug, t.runner.Blocked())
			case logic.OutcomeDelivered:
				fmt.Printf("%s delivered %s\n", t.tug, t.runner.Reefer())
			}
		}
		if !live {
			printSummary(loop.Snapshot())
			return nil
		}
		loop.BeginRound()
		// The pace is the demo's, not the world's: deterministic mode leaves
		// rounds to the host, and the host is here. The wait also gives the
		// owner time to reduce the boundary before the drain reads it. Speed
		// scales the interval so the operator can slow down or accelerate.
		pace := time.Duration(float64(sim.RoundInterval) / speed)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pace):
		}
		if events := loop.DrainEvents(); len(events) > 0 {
			for _, t := range tractors {
				t.pub.NoteEvents(events)
			}
		}
	}
}

// settle waits for a runner's in-flight command to attach its receipt, bounded
// so a stuck world reports rather than hangs. The same bound the tests use.
func settle(r *logic.Runner) {
	for i := 0; i < 200 && !r.Settled(); i++ {
		time.Sleep(time.Millisecond)
	}
}

// printSummary reports how the run ended, from the world's own record rather
// than from what any runner claims it moved.
func printSummary(world sim.World) {
	fmt.Printf("phase %s after round %d\n", world.Phase(), world.Round())
	for _, id := range []sim.ReeferID{sim.Reefer1, sim.Reefer2, sim.Reefer3} {
		state := "still on the apron"
		if world.Delivered(id) {
			state = "delivered"
		}
		fmt.Printf("%s %s\n", id, state)
	}
}

// teardown stops the world, then drains any receipt that attached with no
// tick left to note it — Shutdown answers what was still in flight, so an
// await that was waiting on the world attaches before the drain reads it —
// then the servers, then the stores. The order is deliberate: a store closed
// under a live server would answer an in-flight browser request with a
// closed-database error, so serving stops first. The price is that a
// publisher's final flush — the walk-home receipts that settled after the
// last plan tick — commits unannounced. That is the contract Flush already
// carries: the causality is durable and a reader that pages the run sees it;
// only a live subscriber at that instant was not told.
func teardown(loop *sim.WorldLoop, tractors []*tractor) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := loop.Shutdown(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "reefer-handoff: shutting the world loop: %v\n", err)
	}
	for _, t := range tractors {
		settle(t.runner)
		t.runner.DrainSettled()
	}
	for _, t := range tractors {
		if err := t.server.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "reefer-handoff: %s closing the server: %v\n", t.tug, err)
		}
	}
	for _, t := range tractors {
		if err := t.pub.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "reefer-handoff: %s closing the publisher: %v\n", t.tug, err)
		}
	}
	fmt.Println("stopped")
}
