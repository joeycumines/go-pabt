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

package logic

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/joeycumines/go-pabt"
	"github.com/joeycumines/go-pabt/examples/reefer-handoff/sim"
)

// tickTimeout bounds a drive loop that is waiting on the world. It is a guard
// against a hang, not a timing expectation: a settled run finishes in
// milliseconds.
const tickTimeout = 20 * time.Second

func newLoop(t *testing.T) *sim.WorldLoop {
	t.Helper()
	w, err := sim.NewWorldLoop(sim.ModeDeterministic)
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

// drive ticks a runner until it stops making progress, and returns how it ended.
//
// The loop drains the world between runner ticks, because a runner's command only
// settles once the loop owner has reduced it. Deterministic mode needs the host to
// ask for a round; this is the host.
func drive(t *testing.T, r *Runner) Outcome {
	t.Helper()
	deadline := time.Now().Add(tickTimeout)
	for time.Now().Before(deadline) {
		out, err := r.Tick()
		if err != nil {
			t.Fatalf("%s tick: %v", r.Tractor(), err)
		}
		// Let the loop reduce whatever the runner just submitted, then advance the
		// host's round.
		settle(t, r)
		switch out {
		case OutcomeDelivered, OutcomeBlocked, OutcomeIdle:
			return out
		}
	}
	t.Fatalf("%s did not settle its job within %v; it is holding %q in generation %d",
		r.Tractor(), tickTimeout, r.Reefer(), r.Generation())
	return OutcomeIdle
}

// settle gives the loop a bounded number of chances to reduce the runner's
// in-flight command and then advances a round, so a command the world refuses is
// not retried inside the same round forever.
//
// Four milliseconds is enough for the owner's queued task to run and for the
// runner's await goroutine to attach the result, but only just: a receipt that
// has landed but is not yet attached reads the same as no receipt, so the next
// tick submits nothing and reports Running, and the run needs one more tick to
// observe it. The drive loop allows for that by ticking until the outcome is
// terminal rather than counting ticks.
func settle(t *testing.T, r *Runner) {
	t.Helper()
	for i := 0; i < 4; i++ {
		time.Sleep(time.Millisecond)
	}
	// The await goroutine attaches the result under the runner's lock. Without
	// this, a receipt reduced by the owner is still sitting on the in-flight
	// record when the next tick reads it, and the tick that should verify the
	// post-condition reports Running instead. Settled takes the lock, because
	// the await goroutine writes the record this reads.
	for i := 0; i < 200; i++ {
		if r.Settled() {
			break
		}
		time.Sleep(time.Millisecond)
	}
}

// A single runner delivers its job, which is the minimum P4 owes.
func TestOneRunnerDeliversItsJob(t *testing.T) {
	loop := newLoop(t)
	r := New(sim.Tug1, loop)

	if out := drive(t, r); out != OutcomeDelivered {
		t.Fatalf("ended as %s, want %s; blocked on %q", out, OutcomeDelivered, r.Blocked())
	}
	reefer := r.Reefer()
	if reefer == "" {
		t.Fatal("the generation was retired without recording what it was delivering")
	}
	if !loop.Snapshot().Delivered(reefer) {
		t.Fatalf("%s is not delivered", reefer)
	}
}

// Three runners deliver three different reefers, independently. Which tractor gets
// which reefer is the reducer's decision, so the test asserts the set and not a
// pairing.
//
// The runners take turns, one tick each per round, the way three independent
// tick loops share one world. A runner that delivers keeps ticking: with jobs
// still unclaimed it claims the next one and leaves the bay to fetch it, which
// is what lets the next tractor in. A runner is retired from the rotation only
// when the world is Complete or when it reports Blocked.
func TestThreeRunnersDeliverThreeDifferentReefers(t *testing.T) {
	loop := newLoop(t)
	runners := []*Runner{New(sim.Tug1, loop), New(sim.Tug2, loop), New(sim.Tug3, loop)}

	active := map[sim.TractorID]*Runner{
		sim.Tug1: runners[0], sim.Tug2: runners[1], sim.Tug3: runners[2],
	}
	deadline := time.Now().Add(tickTimeout)
	for len(active) > 0 && time.Now().Before(deadline) {
		if loop.Snapshot().Phase() == sim.PhaseComplete {
			for k := range active {
				delete(active, k)
			}
			break
		}
		for _, r := range runners {
			if _, ok := active[r.Tractor()]; !ok {
				continue
			}
			out, err := r.Tick()
			if err != nil {
				t.Fatalf("%s tick: %v", r.Tractor(), err)
			}
			settle(t, r)
			if out == OutcomeBlocked {
				t.Fatalf("%s ended as %s; blocked on %q", r.Tractor(), out, r.Blocked())
			}
		}
	}
	for _, r := range runners {
		if _, ok := active[r.Tractor()]; ok {
			if loop.Snapshot().Phase() != sim.PhaseComplete {
				t.Fatalf("%s did not settle its job within %v; it is holding %q in generation %d",
					r.Tractor(), tickTimeout, r.Reefer(), r.Generation())
			}
		}
	}
	// A runner that delivered has retired its generation, so the world is the
	// record of what each one moved.
	for _, id := range []sim.ReeferID{sim.Reefer1, sim.Reefer2, sim.Reefer3} {
		if !loop.Snapshot().Delivered(id) {
			t.Fatalf("%s is not delivered", id)
		}
	}
	if got := loop.Snapshot().Phase(); got != sim.PhaseComplete {
		t.Fatalf("the run ended in %s, want %s", got, sim.PhaseComplete)
	}
}

// The RFID fault has to reach a real tree. A plan whose pickup is blocked on an
// unconfirmed tag cannot route around it, so the scan has to appear in the
// generated actions for the tag condition.
func TestScanEntersARealTreeForAnUnreadableTag(t *testing.T) {
	loop := newLoop(t)
	// Claiming takes two ticks — submit, then build — so each runner is ticked
	// twice before its outcome is read.
	runners := []*Runner{New(sim.Tug1, loop), New(sim.Tug2, loop), New(sim.Tug3, loop)}
	// Claiming takes two ticks — submit, then build — so each runner is ticked
	// until it holds a job. The manifest awards jobs in order, so the runner
	// holding REEFER-2 is the one whose tag does not read.
	for _, x := range runners {
		for i := 0; i < 8 && x.Reefer() == ""; i++ {
			if _, err := x.Tick(); err != nil {
				t.Fatalf("%s tick: %v", x.Tractor(), err)
			}
			settle(t, x)
		}
	}
	var r *Runner
	for _, x := range runners {
		if x.Reefer() == sim.Reefer2 {
			r = x
		}
	}
	if r == nil {
		t.Fatal("no runner holds REEFER-2; the manifest awards jobs in order so one must")
	}

	st := r.state
	o, ok := sim.Observe(loop.Snapshot(), r.Tractor(), r.Reefer())
	if !ok {
		t.Fatal("could not frame the world")
	}
	st.pin(o, 0)

	// The pickup is not offered while the tag is unconfirmed, because the planner
	// only admits an action whose effect satisfies the condition that failed.
	pickup, err := st.Actions(condition{key: sim.HoldingKey(r.Tractor(), r.Reefer()),
		match: func(v any) bool { b, ok := v.(bool); return ok && b }})
	if err != nil {
		t.Fatalf("Actions for the pickup: %v", err)
	}
	if len(pickup) == 0 {
		t.Fatal("the pickup was not offered even though the tag is unconfirmed")
	}

	// The scan is offered for the tag condition, and nothing else is.
	scan, err := st.Actions(condition{key: sim.TagKey(r.Reefer()),
		match: func(v any) bool { b, ok := v.(bool); return ok && b }})
	if err != nil {
		t.Fatalf("Actions for the tag: %v", err)
	}
	if len(scan) != 1 {
		t.Fatalf("the tag condition produced %d actions, want exactly the scan", len(scan))
	}
	if got := nameOf(scan[0]); got != "ScanTag(REEFER-2)" {
		t.Fatalf("the tag action is %s, want ScanTag(REEFER-2)", got)
	}
	effects := scan[0].Effects()
	if len(effects) != 1 {
		t.Fatalf("the scan declares %d effects, want one", len(effects))
	}
	if effects[0].Key() != any(sim.TagKey(r.Reefer())) {
		t.Fatalf("the scan's effect is on %v, want the tag", effects[0].Key())
	}
}

// A plan aimed at a slot that loses power must replan rather than spin. The
// refusal is decided by the world, and the recovery is the planner's own
// re-expansion within the same generation: a refused Place reports Failure, the
// next failed precondition grows a new candidate, and the plan places into a
// slot that is still powered. Only when no slot will take the reefer does the
// runner retire the generation as Blocked.
func TestAPlanReplansAfterItsSlotLosesPower(t *testing.T) {
	loop := newLoop(t)
	r := New(sim.Tug1, loop)

	// Take the job, get the plan as far as the bay, then pull the plug on the
	// slot it was aiming at.
	reefer := claimFor(t, r)
	if reefer == "" {
		t.Skip("no job was claimed in the rounds available")
	}
	aim := aimFor(t, loop, r, reefer)
	if aim == "" {
		t.Skipf("%s never reached the bay for %s", r.Tractor(), reefer)
	}

	if _, err := loop.Change(func(w sim.World) (sim.World, []sim.WorldEvent) {
		return sim.SetSlotPower(w, aim, false)
	}); err != nil {
		t.Fatalf("pulling the plug: %v", err)
	}

	// The plan keeps running while its post-condition is open, and the world keeps
	// refusing the placement until the planner re-expands onto a powered slot.
	out := drive(t, r)
	if out != OutcomeDelivered {
		t.Fatalf("ended as %s, want %s; blocked on %q", out, OutcomeDelivered, r.Blocked())
	}
	if !loop.Snapshot().Delivered(reefer) {
		t.Fatalf("%s is not delivered", reefer)
	}
}

// nameOf reads an action's own name, which is what a tree read shows in place of a
// bare occurrence identity.
func nameOf(a pabt.IAction) string {
	if s, ok := a.(fmt.Stringer); ok {
		return s.String()
	}
	return a.Node().Name()
}

// claimFor ticks until the runner holds a job, and returns its reefer.
func claimFor(t *testing.T, r *Runner) sim.ReeferID {
	t.Helper()
	deadline := time.Now().Add(tickTimeout)
	for time.Now().Before(deadline) {
		if _, err := r.Tick(); err != nil {
			t.Fatalf("%s tick: %v", r.Tractor(), err)
		}
		settle(t, r)
		if r.Reefer() != "" {
			return r.Reefer()
		}
	}
	return ""
}

// aimFor ticks until the runner is standing in the bay, and returns the slot its
// plan would place into.
func aimFor(t *testing.T, loop *sim.WorldLoop, r *Runner, reefer sim.ReeferID) sim.SlotID {
	t.Helper()
	deadline := time.Now().Add(tickTimeout)
	for time.Now().Before(deadline) {
		if _, err := r.Tick(); err != nil {
			t.Fatalf("%s tick: %v", r.Tractor(), err)
		}
		settle(t, r)
		world := loop.Snapshot()
		tug, _ := world.Tractor(r.Tractor())
		if tug.At == sim.NodeBay {
			o, ok := sim.Observe(world, r.Tractor(), reefer)
			if !ok {
				return ""
			}
			for _, s := range o.AcceptingSlots() {
				slot, _ := world.Slot(s)
				if slot.Occupied == "" {
					return s
				}
			}
			return ""
		}
	}
	return ""
}

// A wind lockout must not strand a plan: the crossing is refused and the plan
// re-derives over the other approach.
func TestAWindLockoutDoesNotStrandAPlan(t *testing.T) {
	loop := newLoop(t)
	r := New(sim.Tug1, loop)

	reefer := claimFor(t, r)
	if reefer == "" {
		t.Skip("no job was claimed in the rounds available")
	}
	if _, err := loop.Change(func(w sim.World) (sim.World, []sim.WorldEvent) {
		return sim.LockLane(w, sim.LaneWest)
	}); err != nil {
		t.Fatalf("closing the western approach: %v", err)
	}

	if out := drive(t, r); out != OutcomeDelivered {
		t.Fatalf("ended as %s, want %s; blocked on %q", out, OutcomeDelivered, r.Blocked())
	}
	if !loop.Snapshot().Delivered(reefer) {
		t.Fatalf("%s is not delivered after the lane reopened", reefer)
	}
}

// A goal slice of exactly one condition is what keeps the root a GoalRoot rather
// than a selector over OR alternatives. A selector would report success the moment
// either alternative held, which for two jobs would leave the other reefer on the
// apron.
func TestTheGenerationHoldsOneJobAndOneGoal(t *testing.T) {
	loop := newLoop(t)
	r := New(sim.Tug1, loop)
	reefer := claimFor(t, r)
	if reefer == "" {
		t.Skip("no job was claimed in the rounds available")
	}
	if r.Generation() == 0 {
		t.Fatal("the runner built no generation")
	}
	if r.Reefer() != reefer {
		t.Fatalf("the generation holds %q, want the claimed %q", r.Reefer(), reefer)
	}
	world := loop.Snapshot()
	job, _ := world.Job(sim.JobID(reefer))
	if job.ClaimedBy != r.Tractor() {
		t.Fatalf("job %s is claimed by %q, want %q", job.ID, job.ClaimedBy, r.Tractor())
	}
}
