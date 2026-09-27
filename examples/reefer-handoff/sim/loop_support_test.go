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
	"strconv"
	"testing"
	"time"
)

// resultTimeout bounds every wait for a result. It is a guard against a hang,
// not a timing expectation: a healthy loop answers in microseconds.
const resultTimeout = 5 * time.Second

func newStartedLoop(t *testing.T, mode Mode) *WorldLoop {
	t.Helper()
	w, err := NewWorldLoop(mode)
	if err != nil {
		t.Fatalf("NewWorldLoop: %v", err)
	}
	if err := w.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if err := w.Shutdown(context.Background()); err != nil {
			t.Errorf("Shutdown: %v", err)
		}
	})
	return w
}

// claimAllThroughLoop has every tractor claim a job, through the loop.
func claimAllThroughLoop(t *testing.T, w *WorldLoop) {
	t.Helper()
	for _, tug := range []TractorID{Tug1, Tug2, Tug3} {
		r := mustResult(t, w, command(t, w, tug, Reefer1, "claim-"+string(tug), CmdClaimJob, "", ""))
		if !r.Receipt.Applied() {
			t.Fatalf("%s could not claim: %s", tug, r.Receipt.Reason)
		}
	}
}

// holdAtBayThroughLoop drives a tractor to the bay holding a named reefer. It does
// not assume which job that tractor was awarded, because a reefer's identity is
// what the custody check is about.
func holdAtBayThroughLoop(t *testing.T, w *WorldLoop, tug TractorID, r ReeferID) {
	t.Helper()
	tagOK := false
	if o, ok := Observe(w.Snapshot(), tug, r); ok {
		if v, known := o.Read(TagKey(r)); known {
			tagOK, _ = v.(bool)
		}
	}
	if !tagOK {
		s := mustResult(t, w, command(t, w, tug, r, "scan-"+string(tug), CmdScanTag, "", ""))
		if !s.Receipt.Applied() {
			t.Fatalf("%s could not scan %s: %s", tug, r, s.Receipt.Reason)
		}
	}
	if p := mustResult(t, w, command(t, w, tug, r, "pick-"+string(tug), CmdPickUp, "", "")); !p.Receipt.Applied() {
		t.Fatalf("%s could not pick up %s: %s", tug, r, p.Receipt.Reason)
	}
	for i, n := range []NodeID{NodeEast, NodeBay} {
		m := mustResult(t, w, command(t, w, tug, r, "move-"+string(tug)+itoa(i), CmdMoveTo, "", n))
		if !m.Receipt.Applied() {
			t.Fatalf("%s could not reach %s: %s", tug, n, m.Receipt.Reason)
		}
	}
}

// placeThroughLoop places a held reefer and then walks the tractor home, because
// the bay and the waypoints each hold one.
func placeThroughLoop(t *testing.T, w *WorldLoop, tug TractorID, r ReeferID, slot SlotID, prefix string) {
	t.Helper()
	p := mustResult(t, w, command(t, w, tug, r, prefix+"-place", CmdPlace, slot, ""))
	if !p.Receipt.Applied() {
		t.Fatalf("%s could not place %s in %s: %s", tug, r, slot, p.Receipt.Reason)
	}
	// The bay and the waypoints each hold one, so the next tractor needs this one
	// out of the way.
	if w.Snapshot().Phase() == PhaseRunning {
		for i, n := range []NodeID{NodeEast, NodeStaging} {
			m := mustResult(t, w, command(t, w, tug, r, prefix+"-home"+itoa(i), CmdMoveTo, "", n))
			if !m.Receipt.Applied() {
				t.Fatalf("%s could not get home from the bay: %s", tug, m.Receipt.Reason)
			}
		}
	}
}

// changeThroughLoop applies an exogenous change and waits for its events.
func changeThroughLoop(t *testing.T, w *WorldLoop, change func(World) (World, []WorldEvent)) []WorldEvent {
	t.Helper()
	ch, err := w.Change(change)
	if err != nil {
		t.Fatalf("Change: %v", err)
	}
	select {
	case evs := <-ch:
		return evs
	case <-time.After(resultTimeout):
		t.Fatalf("no events within %v", resultTimeout)
	}
	return nil
}

func itoa(n int) string { return strconv.Itoa(n) }

// keyFor names one command from one producer, so a concurrent test can tell whose
// result it is looking at.
func keyFor(p, i int) string {
	return "p" + itoa(p) + "-" + itoa(i)
}

// command builds a command framed against the loop's current world, which is what
// a runner does: it reads one frame and decides everything from it.
func command(t *testing.T, w *WorldLoop, tug TractorID, r ReeferID, key string, kind CommandKind, slot SlotID, node NodeID) Command {
	t.Helper()
	c, ok := commandOnLoop(w, tug, r, key, kind, slot, node)
	if !ok {
		t.Fatalf("could not frame the world for %s and %s", tug, r)
	}
	return c
}

// commandOnLoop is the framing step on its own, for a producer goroutine that must
// not call t.Fatalf — FailNow off the test goroutine is incorrect.
func commandOnLoop(w *WorldLoop, tug TractorID, r ReeferID, key string, kind CommandKind, slot SlotID, node NodeID) (Command, bool) {
	o, ok := Observe(w.Snapshot(), tug, r)
	if !ok {
		return Command{}, false
	}
	return Command{
		ID: key, IdempotencyKey: key, Kind: kind, Tractor: tug, Reefer: r,
		Slot: slot, Node: node, Expect: o.Expectation(slot),
	}, true
}

func mustResult(t *testing.T, w *WorldLoop, c Command) Result {
	t.Helper()
	ch, err := w.Submit(c)
	if err != nil {
		t.Fatalf("Submit %s: %v", c.Kind, err)
	}
	select {
	case r := <-ch:
		return r
	case <-time.After(resultTimeout):
		t.Fatalf("no result for %s within %v", c.Kind, resultTimeout)
	}
	return Result{}
}
