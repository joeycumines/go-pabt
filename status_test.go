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

package pabt

import (
	"testing"

	"strings"
	"sync"
	"sync/atomic"

	bt "github.com/joeycumines/go-behaviortree"
)

// The status and identity primitives, and the capture proofs that are really
// statements about them.
//
// Split out of capture_test.go, which had outgrown the point where a change to
// one of these could be read without scrolling past the others. What lives here
// is about what a status and an occurrence identity *are*: that they use the
// dependency's numbering, that a snapshot of one is coherent, that an absent
// status is an absence rather than a zero, and that a declared expectation is
// distinguishable from an inferred one. What stayed in capture_test.go is the
// capture boundary itself - what a tick records and what it must not call.

func TestValidateStatusUsesTheDependencyValues(t *testing.T) {
	// The dependency derives its statuses from a blank iota, so the zero value
	// is not a status. Pinning the numbers here is what stops the wrong set from
	// propagating into storage and wire formats again.
	if bt.Running != 1 || bt.Success != 2 || bt.Failure != 3 {
		t.Fatalf("dependency statuses changed: Running=%d Success=%d Failure=%d",
			bt.Running, bt.Success, bt.Failure)
	}
	for _, ok := range []bt.Status{bt.Running, bt.Success, bt.Failure} {
		if err := ValidateStatus(ok); err != nil {
			t.Errorf("ValidateStatus(%d) = %v, want nil", ok, err)
		}
	}
	for _, bad := range []bt.Status{0, 4, -1, 99} {
		if err := ValidateStatus(bad); err == nil {
			t.Errorf("ValidateStatus(%d) = nil, want an error", bad)
		}
	}
}

func TestNodeStatusSnapshotIsCoherent(t *testing.T) {
	var s NodeStatus
	first := s.Snapshot()
	if first.StatusPresent {
		t.Error("a node that never ticked reports a status")
	}
	if first.TickCount != 0 || first.Version != 0 {
		t.Errorf("fresh snapshot = %+v, want zero", first)
	}

	s.record(bt.Running)
	s.record(bt.Success)
	got := s.Snapshot()
	if got.TickCount != 2 {
		t.Errorf("tick count = %d, want 2", got.TickCount)
	}
	if got.LastStatus != bt.Success {
		t.Errorf("last status = %v, want Success", got.LastStatus)
	}
	if !got.StatusPresent {
		t.Error("a recorded status is not marked present")
	}
	if got.Version != 2 {
		t.Errorf("version = %d, want 2", got.Version)
	}
	if s.TickCount() != got.TickCount || s.LastStatus() != got.LastStatus {
		t.Error("the accessors disagree with the coherent snapshot")
	}
}

func TestNodeStatusRejectsAnAbsentStatusRatherThanInventingOne(t *testing.T) {
	var s NodeStatus
	// A status the dependency does not define is recorded verbatim but never
	// claimed as present, so a consumer shows it as unavailable.
	s.record(bt.Status(0))
	got := s.Snapshot()
	if got.StatusPresent {
		t.Error("an undefined status was reported as present")
	}
	if got.LastStatus != 0 {
		t.Errorf("last status = %d, want the recorded value", got.LastStatus)
	}
}

func TestNodeStatusSnapshotUnderConcurrentReaders(t *testing.T) {
	// Version and TickCount advance together, so a single Snapshot read must
	// always agree on them. Independent counters could not make that guarantee,
	// which is the property this test exists to hold.
	var (
		s     NodeStatus
		start = make(chan struct{})
		wg    sync.WaitGroup
		torn  atomic.Int64
		reads atomic.Int64
	)
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for range 500 {
				got := s.Snapshot()
				reads.Add(1)
				if got.Version != uint64(got.TickCount) {
					torn.Add(1)
				}
				if got.StatusPresent != (ValidateStatus(got.LastStatus) == nil) {
					torn.Add(1)
				}
			}
		}()
	}
	close(start)
	for i := range 2000 {
		if i%2 == 0 {
			s.record(bt.Running)
		} else {
			s.record(bt.Success)
		}
	}
	wg.Wait()
	if reads.Load() != 2000 {
		t.Fatalf("reads = %d, want 2000", reads.Load())
	}
	if n := torn.Load(); n != 0 {
		t.Errorf("observed %d inconsistent status snapshots", n)
	}
}

func TestValidOccurrenceID(t *testing.T) {
	id := &identity{namespace: strings.Repeat("a", 32)}
	minted := id.mint()
	if !ValidOccurrenceID(minted) {
		t.Errorf("minted id %q rejected by its own validator", minted)
	}
	second := id.mint()
	if minted == second {
		t.Error("mint returned the same identity twice")
	}
	for _, bad := range []string{
		"",
		"nope",
		"pabt-v1-",
		"pabt-v1-aaaaaaaa-0000000000000000", // namespace too short
		"pabt-v1-" + strings.Repeat("a", 32) + "-0000",             // ordinal too short
		"pabt-v1-" + strings.Repeat("A", 32) + "-0000000000000000", // uppercase
		"pabt-v2-" + strings.Repeat("a", 32) + "-0000000000000000", // wrong version
	} {
		if ValidOccurrenceID(OccurrenceID(bad)) {
			t.Errorf("ValidOccurrenceID(%q) = true, want false", bad)
		}
	}
	lineage := lineageOf(minted)
	if !ValidLineageID(lineage) {
		t.Errorf("lineage %q rejected by its own validator", lineage)
	}
	if ValidLineageID(LineageID(minted)) {
		t.Error("a plain occurrence id was accepted as a lineage id")
	}
	if lineageOf("") != "" {
		t.Error("lineage of an empty source is not empty")
	}
}

func TestExpectedValueOnlyWhenDeclared(t *testing.T) {
	if _, ok := ExpectedValue(&undeclaredCondition{key: "x"}); ok {
		t.Error("a condition that declares nothing reported an expected value")
	}
	value, ok := ExpectedValue(&sealedCondition{key: "x", expected: "done"})
	if !ok || value != "done" {
		t.Errorf("ExpectedValue = %v, %v, want done, true", value, ok)
	}
	if _, ok := ExpectedValue(nil); ok {
		t.Error("nil reported a declared expected value")
	}
}

// TestEveryApplicationCallIsRecorded proves the recorded evidence covers the
// calls an audit would ask about, including the ones that return a value the
// planner then caches.
func TestEveryApplicationCallIsRecorded(t *testing.T) {
	plan, _, _ := newSealedFixture(t, CoherenceSnapshot)
	// Expansion records the action's own calls, which happen on the first tick.
	first := plan.TickCaptured(TickEnvironment{})
	tickUntilSettled(t, plan)
	all := append([]CallbackEvidence{}, plan.construction...)
	all = append(all, first.Snapshot.Callbacks...)

	seen := map[CallbackKind]int{}
	for _, cb := range all {
		seen[cb.Kind]++
	}
	for _, want := range []CallbackKind{
		CallbackKey, CallbackExpected, CallbackStateActions,
		CallbackActionEffects, CallbackActionNode, CallbackActionConditions,
		CallbackEffectValue, CallbackVariableRead, CallbackMatch,
	} {
		if seen[want] == 0 {
			t.Errorf("no %v callback was recorded, so the audit of application calls has a hole", want)
		}
	}
	// An expectation the application does not declare is still a call, and must
	// be visible as such rather than absent.
	bare := &bareState{variables: map[string]any{"x": "done"}}
	barePlan, err := NewCaptured(
		bare, []Conditions[*undeclaredCondition]{{&undeclaredCondition{key: "x", expected: "done"}}},
		WithStateViews[*undeclaredCondition](&bareProvider{state: bare}),
	)
	if err != nil {
		t.Fatalf("NewCaptured: %v", err)
	}
	var asked int
	for _, cb := range barePlan.construction {
		if cb.Kind == CallbackExpected {
			asked++
			if cb.Known {
				t.Error("a condition that declares nothing was recorded as declaring one")
			}
		}
	}
	if asked == 0 {
		t.Error("an undeclared expectation produced no recorded call")
	}
}

// TestRootStatusIsPublishedInOneStep applies the same tear oracle the status
// snapshot guarantees to the plan root, which the tick wrapper publishes.
func TestRootStatusIsPublishedInOneStep(t *testing.T) {
	var (
		cond   = &sealedCondition{key: "x", expected: "done"}
		vars   = map[string]any{"x": "start"}
		state  *sealedState
		plan   *Plan[*sealedCondition]
		torn   atomic.Int64
		reads  atomic.Int64
		stop   = make(chan struct{})
		guard  sync.WaitGroup
		status *NodeStatus
	)
	state = &sealedState{variables: vars}
	state.apply = func() { vars["x"] = "done" }
	var err error
	plan, err = NewCaptured(state, []Conditions[*sealedCondition]{{cond}},
		WithStateViews[*sealedCondition](&sealedProvider{
			coherence: CoherenceSnapshot, variable: state.Variable, actions: state.Actions,
		}))
	if err != nil {
		t.Fatalf("NewCaptured: %v", err)
	}
	for range 6 {
		plan.TickCaptured(TickEnvironment{})
	}
	status = plan.root.status
	for range 4 {
		guard.Add(1)
		go func() {
			defer guard.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				got := status.Snapshot()
				reads.Add(1)
				if got.Version != uint64(got.TickCount) {
					torn.Add(1)
				}
			}
		}()
	}
	for range 3000 {
		plan.TickCaptured(TickEnvironment{})
	}
	close(stop)
	guard.Wait()
	if reads.Load() == 0 {
		t.Fatal("no reads happened")
	}
	if n := torn.Load(); n != 0 {
		t.Errorf("observed %d torn root status reads", n)
	}
	final := status.Snapshot()
	if final.Version != uint64(final.TickCount) {
		t.Errorf("root status = %+v, want version equal to tick count", final)
	}
}

// TestSecondActionInATickIsNotAuthoritative proves a verdict is refused when the
// window also contains another action's effects.
func TestSecondActionInATickIsNotAuthoritative(t *testing.T) {
	state := &pairState{variables: map[string]any{"x": "start"}}
	cond := &sealedCondition{key: "x", expected: "done"}
	plan, err := NewCaptured(
		state, []Conditions[*sealedCondition]{{cond}},
		WithStateViews[*sealedCondition](&sealedProvider{
			coherence: CoherenceSnapshot, variable: state.Variable, actions: state.Actions,
		}),
		WithExecutionObserver[*sealedCondition](&sealedObserver{}),
		WithTransitionVerifier[*sealedCondition](sealedVerifier{}),
	)
	if err != nil {
		t.Fatalf("NewCaptured: %v", err)
	}
	var found bool
	for range 4 {
		result := plan.TickCaptured(TickEnvironment{})
		if len(result.Snapshot.Executions) < 2 {
			continue
		}
		found = true
		for i, exec := range result.Snapshot.Executions {
			verdicts := 0
			for _, rel := range exec.Relations {
				verdicts++
				want := i == 0
				if rel.Authoritative != want {
					t.Errorf("execution %d verdict authoritative = %v, want %v", i, rel.Authoritative, want)
				}
			}
			if verdicts == 0 {
				t.Errorf("execution %d has no verdict", i)
			}
			if exec.Isolated != (i == 0) {
				t.Errorf("execution %d isolated = %v, want %v", i, exec.Isolated, i == 0)
			}
		}
		break
	}
	if !found {
		t.Fatal("no tick ran two actions, so the isolation rule was not exercised")
	}
}

func TestUnmarshalKeepsAPresentStatusFromTheOlderFormat(t *testing.T) {
	var s NodeStatus
	// The older format had no StatusPresent field, so its absence must be derived
	// rather than decoded as "no status", which would silently lose the record.
	if err := s.UnmarshalJSON([]byte(`{"TickCount":5,"LastStatus":2}`)); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	got := s.Snapshot()
	if !got.StatusPresent {
		t.Error("a recorded status decoded as an absence")
	}
	if got.LastStatus != bt.Success || got.TickCount != 5 {
		t.Errorf("snapshot = %+v, want the recorded pair", got)
	}
	// An explicit false is still honoured.
	if err := s.UnmarshalJSON([]byte(
		`{"TickCount":5,"LastStatus":2,"StatusPresent":false}`)); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if s.Snapshot().StatusPresent {
		t.Error("an explicit absent status was read as present")
	}
	// A status the dependency does not define derives to absent.
	if err := s.UnmarshalJSON([]byte(`{"TickCount":1,"LastStatus":7}`)); err != nil {
		t.Fatalf("UnmarshalJSON: %v", err)
	}
	if s.Snapshot().StatusPresent {
		t.Error("an undefined status was read as present")
	}
}

func TestMergeErrorKeepsBothTypes(t *testing.T) {
	got := mergeError(
		&RecordedError{Type: "pabt.ErrOne", Message: "one"},
		&RecordedError{Type: "pabt.ErrTwo", Message: "two"},
	)
	if got == nil {
		t.Fatal("mergeError(nil, nil) is not nil")
	}
	if got.Type != "pabt.ErrOne|pabt.ErrTwo" {
		t.Errorf("merged type = %q, want both types", got.Type)
	}
	if mergeError(nil, nil) != nil {
		t.Error("merging two nil errors produced an error")
	}
}
