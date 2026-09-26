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
)

// The state view a tick is handed, and how it is closed.
//
// Split out of capture_test.go for the same reason as the others. What lives
// here is about the view itself: that one is opened and closed exactly once per
// tick, that it is closed even when the planner panics, that a configured
// provider is used and never silently replaced, and that the default is an
// explicit live view rather than a fallback.

func TestStateViewOpenedAndClosedExactlyOncePerTick(t *testing.T) {
	plan, _, provider := newSealedFixture(t, CoherenceSnapshot)
	tickUntilSettled(t, plan)
	opens, closes := provider.counts()
	if opens == 0 {
		t.Fatal("no state view was opened")
	}
	// One pre-tick view per tick, plus one post-action view per action
	// execution, must all be closed.
	if closes != opens {
		t.Errorf("state views opened = %d, closed = %d", opens, closes)
	}
}

func TestCapturedViewClosesExactlyOnceWhenThePlannerPanics(t *testing.T) {
	cond := &sealedCondition{key: "x", expected: "done"}
	variables := map[string]any{"x": "start"}
	state := &sealedState{variables: variables}
	state.apply = func() { panic(`pabt test: action panicked`) }
	provider := (&sealedProvider{coherence: CoherenceSnapshot}).forState(state)
	plan, err := NewCaptured(
		state,
		[]Conditions[*sealedCondition]{{cond}},
		WithStateViews[*sealedCondition](provider),
	)
	if err != nil {
		t.Fatalf("NewCaptured: %v", err)
	}
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("a planner panic did not propagate to the caller")
			}
		}()
		// The first tick expands; the second runs the action and panics.
		plan.TickCaptured(TickEnvironment{})
		plan.TickCaptured(TickEnvironment{})
	}()
	opens, closes := provider.counts()
	if opens != closes {
		t.Errorf("after a panic, state views opened = %d, closed = %d", opens, closes)
	}
}

func TestCapturedStateViewsAreAuthoritativeAndNeverFallBack(t *testing.T) {
	cond := &sealedCondition{key: "x", expected: "done"}
	variables := map[string]any{"x": "start"}
	state := &sealedState{
		variables: variables,
		apply:     func() { variables["x"] = "done" },
	}
	// A provider that fails must not silently fall back to the raw state.
	plan, err := NewCaptured(
		state,
		[]Conditions[*sealedCondition]{{cond}},
		WithStateViews[*sealedCondition](failingProvider{}),
	)
	if err != nil {
		t.Fatalf("NewCaptured: %v", err)
	}
	result := plan.TickCaptured(TickEnvironment{})
	if result.CaptureError == nil {
		t.Fatal("a failing state view produced no capture error")
	}
	if len(result.Snapshot.Nodes) != 0 {
		t.Error("a failing state view still produced a tree")
	}
}

func TestCapturedDefaultsToAnExplicitLiveView(t *testing.T) {
	cond := &sealedCondition{key: "x", expected: "done"}
	variables := map[string]any{"x": "done"}
	state := &sealedState{variables: variables, apply: func() {}}
	plan, err := NewCaptured(state, []Conditions[*sealedCondition]{{cond}})
	if err != nil {
		t.Fatalf("NewCaptured: %v", err)
	}
	result := plan.TickCaptured(TickEnvironment{})
	if result.Snapshot.Coherence != CoherenceLive {
		t.Errorf("coherence = %q, want %q for the default provider", result.Snapshot.Coherence, CoherenceLive)
	}
}

func TestCapturedProviderRejectsNilConfiguration(t *testing.T) {
	cond := &sealedCondition{key: "x", expected: "done"}
	state := &sealedState{variables: map[string]any{"x": "done"}, apply: func() {}}
	if _, err := NewCaptured(state, []Conditions[*sealedCondition]{{cond}},
		WithStateViews[*sealedCondition](nil)); err == nil {
		t.Error("a nil state view provider was accepted")
	}
	if _, err := NewCaptured(state, []Conditions[*sealedCondition]{{cond}},
		WithExecutionObserver[*sealedCondition](nil)); err == nil {
		t.Error("a nil execution observer was accepted")
	}
	if _, err := NewCaptured(state, []Conditions[*sealedCondition]{{cond}},
		WithTransitionVerifier[*sealedCondition](nil)); err == nil {
		t.Error("a nil transition verifier was accepted")
	}
	if _, err := NewCaptured[*sealedCondition](nil, nil); err == nil {
		t.Error("a nil state was accepted")
	}
}
