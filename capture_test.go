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
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	bt "github.com/joeycumines/go-behaviortree"
)

// The fixture below models a one-variable world. Every application callback
// panics once the fixture is sealed, so a test can prove that a code path makes
// no application call by sealing the fixture and then running that path.

type (
	sealedCondition struct {
		key      string
		expected any
		sealed   bool
		calls    int
	}
	sealedEffect struct {
		key    string
		value  any
		sealed bool
		calls  int
	}
	sealedAction struct {
		conditions []Conditions[*sealedCondition]
		effects    Effects
		node       bt.Node
	}
	sealedState struct {
		variables map[string]any
		apply     func()
	}
	sealedView struct {
		provider  *sealedProvider
		revision  StateRevision
		coherence StateCoherence
	}
	sealedProvider struct {
		state     *sealedState
		coherence StateCoherence

		mu     sync.Mutex
		opens  int
		closes int
	}
	sealedObserver struct{}
	sealedVerifier struct{}
)

func (c *sealedCondition) Key() any {
	if c.sealed {
		panic(`pabt test: Condition.Key called after sealing`)
	}
	c.calls++
	return c.key
}

func (c *sealedCondition) Match(value any) bool {
	if c.sealed {
		panic(`pabt test: Condition.Match called after sealing`)
	}
	c.calls++
	return value == c.expected
}

// Expected declares the value the condition requires, which is the only way an
// expected value can reach the debugger.
func (c *sealedCondition) Expected() any { return c.expected }

func (e *sealedEffect) Key() any {
	if e.sealed {
		panic(`pabt test: Effect.Key called after sealing`)
	}
	e.calls++
	return e.key
}

func (e *sealedEffect) Value() any {
	if e.sealed {
		panic(`pabt test: Effect.Value called after sealing`)
	}
	e.calls++
	return e.value
}

func (a *sealedAction) Conditions() []Conditions[*sealedCondition] { return a.conditions }
func (a *sealedAction) Effects() Effects                           { return a.effects }
func (a *sealedAction) Node() bt.Node                              { return a.node }

func (s *sealedState) Variable(key any) (any, error) {
	v, ok := s.variables[fmt.Sprint(key)]
	if !ok {
		return nil, fmt.Errorf(`pabt test: no variable %v`, key)
	}
	return v, nil
}

func (s *sealedState) Actions(failed *sealedCondition) ([]Action[*sealedCondition], error) {
	apply := s.apply
	return []Action[*sealedCondition]{&sealedAction{
		effects: Effects{&sealedEffect{key: failed.key, value: failed.expected}},
		node: bt.New(func([]bt.Node) (bt.Status, error) {
			if apply != nil {
				apply()
			}
			return bt.Success, nil
		}),
	}}, nil
}

func (v *sealedView) Variable(key any) (any, error) { return v.provider.state.Variable(key) }

func (v *sealedView) Actions(failed *sealedCondition) ([]Action[*sealedCondition], error) {
	return v.provider.state.Actions(failed)
}

func (v *sealedView) Revision() StateRevision   { return v.revision }
func (v *sealedView) Coherence() StateCoherence { return v.coherence }

func (v *sealedView) Close() error {
	v.provider.mu.Lock()
	v.provider.closes++
	v.provider.mu.Unlock()
	return nil
}

func (p *sealedProvider) OpenStateView(req StateViewRequest) (TypedStateView[*sealedCondition], error) {
	p.mu.Lock()
	p.opens++
	p.mu.Unlock()
	return &sealedView{
		provider:  p,
		revision:  StateRevision{PlanRevision: req.PlanRevision, WorldRevision: "w1"},
		coherence: p.coherence,
	}, nil
}

func (p *sealedProvider) counts() (opens, closes int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.opens, p.closes
}

func (o *sealedObserver) ObserveExecution(req ActionObservationRequest) (ActionObservation, error) {
	out := ActionObservation{}
	for _, eff := range req.Effects {
		value, err := req.View.Variable(eff.Key)
		if err != nil {
			continue
		}
		out.EffectValues = append(out.EffectValues, EffectObservation{
			EffectID: eff.ID,
			Value:    value,
			Known:    true,
		})
	}
	return out, nil
}

func (sealedVerifier) VerifyTransitions(req TransitionRequest) ([]RelationVerdict, error) {
	out := make([]RelationVerdict, 0, len(req.Effects))
	for _, eff := range req.Effects {
		result := RelationDoesNotDecide
		for _, obs := range req.Observation.EffectValues {
			if obs.EffectID == eff.ID && obs.Known && fmt.Sprint(obs.Value) == fmt.Sprint(eff.Value) {
				result = RelationPreserves
			}
		}
		out = append(out, RelationVerdict{
			EffectID:        eff.ID,
			PostconditionID: req.PostconditionID,
			Result:          result,
			Method:          "observed_value_equals_declared_value",
		})
	}
	return out, nil
}

// undeclaredCondition declares no expected value, so its expectation must stay
// unavailable rather than being inferred from its key.
type undeclaredCondition struct {
	key      string
	expected any
	sealed   bool
}

func (c *undeclaredCondition) Key() any {
	if c.sealed {
		panic(`pabt test: Condition.Key called after sealing`)
	}
	return c.key
}

func (c *undeclaredCondition) Match(value any) bool {
	if c.sealed {
		panic(`pabt test: Condition.Match called after sealing`)
	}
	return value == c.expected
}

func newSealedFixture(t *testing.T, coherence StateCoherence) (
	*Plan[*sealedCondition],
	*sealedCondition,
	*sealedProvider,
) {
	t.Helper()
	cond := &sealedCondition{key: "x", expected: "done"}
	state := &sealedState{variables: map[string]any{"x": "start"}}
	state.apply = func() { state.variables["x"] = "done" }
	provider := &sealedProvider{state: state, coherence: coherence}
	plan, err := NewCaptured(
		state,
		[]Conditions[*sealedCondition]{{cond}},
		WithStateViews[*sealedCondition](provider),
		WithExecutionObserver[*sealedCondition](&sealedObserver{}),
		WithTransitionVerifier[*sealedCondition](sealedVerifier{}),
	)
	if err != nil {
		t.Fatalf("NewCaptured: %v", err)
	}
	return plan, cond, provider
}

func tickUntilSettled(t *testing.T, plan *Plan[*sealedCondition]) TickExecution {
	t.Helper()
	var last TickExecution
	for range 4 {
		last = plan.TickCaptured(TickEnvironment{AttemptID: "attempt-1"})
		if last.Status == bt.Success {
			return last
		}
	}
	t.Fatalf("plan did not settle, last status %v", last.Status)
	return last
}

func sealFixture(cond *sealedCondition) {
	cond.sealed = true
}

// TestCapturedPlannerTypedAndErased proves one non-generic boundary drives a
// plan over a concrete condition type as well as the erased alias.
func TestCapturedPlannerTypedAndErased(t *testing.T) {
	var _ CapturedPlanner = (*Plan[Condition])(nil)
	var _ CapturedPlanner = (*Plan[*sealedCondition])(nil)
	var _ TypedStateViewProvider[*sealedCondition] = (*sealedProvider)(nil)
	var _ BorrowedStateView = borrowed[Condition]{}
	if _, ok := any(&Plan[*sealedCondition]{}).(CapturedPlanner); !ok {
		t.Fatal("typed plan does not satisfy CapturedPlanner")
	}
}

func TestCapturedSnapshotHasOccurrencesAndProvenance(t *testing.T) {
	plan, cond, _ := newSealedFixture(t, CoherenceSnapshot)
	result := tickUntilSettled(t, plan)
	snap := result.Snapshot

	if snap.RootID == "" || !ValidOccurrenceID(snap.RootID) {
		t.Fatalf("root id %q is not a valid occurrence id", snap.RootID)
	}
	if len(snap.Nodes) == 0 {
		t.Fatal("snapshot has no nodes")
	}
	if len(snap.Conditions) != 1 {
		t.Fatalf("snapshot conditions = %d, want 1", len(snap.Conditions))
	}
	got := snap.Conditions[0]
	if got.ID == "" || !ValidOccurrenceID(got.ID) {
		t.Fatalf("condition id %q is not a valid occurrence id", got.ID)
	}
	if got.Key != "x" {
		t.Errorf("condition key = %v, want x", got.Key)
	}
	if !got.ExpectedKnown || got.Expected != "done" {
		t.Errorf("expected = %v known=%v, want done known=true", got.Expected, got.ExpectedKnown)
	}
	if !got.TruthKnown {
		t.Error("condition truth is not marked known even though it was evaluated")
	}
	// The post-condition node is the first child of the ppa root, so on the tick
	// that runs the action it is still evaluated before the action's effect has
	// been applied. Its confirmation therefore arrives on the following tick,
	// and saying otherwise would misreport when the effect landed.
	if got.Truth != bt.Failure {
		t.Errorf("condition truth on the action tick = %v, want Failure", got.Truth)
	}
	after := plan.TickCaptured(TickEnvironment{})
	if len(after.Snapshot.Conditions) != 1 {
		t.Fatalf("conditions after the following tick = %d, want 1", len(after.Snapshot.Conditions))
	}
	if confirmed := after.Snapshot.Conditions[0]; confirmed.Truth != bt.Success {
		t.Errorf("condition truth after the action = %v, want Success", confirmed.Truth)
	}
	if got.PostNodeID == "" || !ValidOccurrenceID(got.PostNodeID) {
		t.Fatalf("post node id %q is not a valid occurrence id", got.PostNodeID)
	}
	if got.PostNodeID == got.NodeID {
		t.Error("post node id equals the condition node id, want a distinct derived node")
	}

	if len(snap.Actions) != 1 {
		t.Fatalf("snapshot actions = %d, want 1", len(snap.Actions))
	}
	action := snap.Actions[0]
	if action.ID == "" || !ValidOccurrenceID(action.ID) {
		t.Fatalf("action id %q is not a valid occurrence id", action.ID)
	}
	if action.PostconditionID != got.ID {
		t.Errorf("action postcondition = %v, want the expanded condition %v", action.PostconditionID, got.ID)
	}
	if action.Name != "x→done" {
		t.Errorf("action name = %q, want %q", action.Name, "x→done")
	}
	if len(snap.Effects) != 1 {
		t.Fatalf("snapshot effects = %d, want 1", len(snap.Effects))
	}
	eff := snap.Effects[0]
	if eff.ActionID != action.ID {
		t.Errorf("effect action = %v, want %v", eff.ActionID, action.ID)
	}
	if !eff.ValueKnown || eff.Value != "done" {
		t.Errorf("effect value = %v known=%v, want done", eff.Value, eff.ValueKnown)
	}
	if len(action.EffectIDs) != 1 || action.EffectIDs[0] != eff.ID {
		t.Errorf("action effect ids = %v, want [%v]", action.EffectIDs, eff.ID)
	}
	if cond.calls == 0 {
		t.Error("the fixture condition was never called, so the test proved nothing")
	}
}

// TestCapturedRecordsValueThatFedMatch proves the current value a match saw is
// recorded, and that a support test's value is not mistaken for it.
func TestCapturedRecordsValueThatFedMatch(t *testing.T) {
	plan, _, _ := newSealedFixture(t, CoherenceSnapshot)
	// The first tick expands the failed precondition, so the support test runs
	// there. The second runs the action and the post-condition, so the runtime
	// evaluation runs there. Both worlds are checked because conflating them is
	// exactly the ambiguity the use field exists to prevent.
	expansion := plan.TickCaptured(TickEnvironment{})
	settled := tickUntilSettled(t, plan)
	callbacks := append(append([]CallbackEvidence{}, expansion.Snapshot.Callbacks...),
		settled.Snapshot.Callbacks...)
	if len(callbacks) == 0 {
		t.Fatal("no callbacks recorded")
	}

	var (
		reads     int
		matches   int
		usedNames []MatchUse
	)
	for _, cb := range callbacks {
		if cb.Phase != CallbackPhaseTick {
			t.Errorf("callback %v phase = %q, want tick", cb.Kind, cb.Phase)
		}
		if cb.Ordinal == 0 {
			t.Errorf("callback %v has no ordinal", cb.Kind)
		}
		switch cb.Kind {
		case CallbackVariableRead:
			reads++
			if cb.Key != "x" {
				t.Errorf("variable read key = %v, want x", cb.Key)
			}
			if cb.Result != "start" {
				t.Errorf("variable read result = %v, want start", cb.Result)
			}
		case CallbackMatch:
			matches++
			usedNames = append(usedNames, cb.Use)
			if cb.Use == UseRuntimeEvaluation {
				if cb.Input != "start" {
					t.Errorf("runtime match input = %v, want the value read from state", cb.Input)
				}
				if cb.Result != false {
					t.Errorf("runtime match result = %v, want false for start != done", cb.Result)
				}
			}
			if cb.Use == UseEffectSupport && cb.Input != "done" {
				t.Errorf("support match input = %v, want the effect's declared value", cb.Input)
			}
		}
	}
	if reads == 0 {
		t.Error("no variable read recorded, so no current value has a producer")
	}
	if matches == 0 {
		t.Error("no match recorded")
	}
	var sawRuntime, sawSupport bool
	for _, use := range usedNames {
		switch use {
		case UseRuntimeEvaluation:
			sawRuntime = true
		case UseEffectSupport:
			sawSupport = true
		}
	}
	if !sawRuntime || !sawSupport {
		t.Errorf("match uses recorded = %v, want both runtime_evaluation and effect_support", usedNames)
	}
}

// TestConstructionCallbacksAreSeparateFromTick proves the goal's keys are read
// while the plan is built, not while it ticks.
func TestConstructionCallbacksAreSeparateFromTick(t *testing.T) {
	plan, _, _ := newSealedFixture(t, CoherenceSnapshot)
	if len(plan.construction) == 0 {
		t.Fatal("no construction callbacks recorded")
	}
	for _, cb := range plan.construction {
		if cb.Kind != CallbackKey {
			t.Errorf("construction callback kind = %v, want key", cb.Kind)
		}
		if cb.Phase != CallbackPhaseConstruction {
			t.Errorf("construction callback phase = %q, want construction", cb.Phase)
		}
	}
	result := plan.TickCaptured(TickEnvironment{})
	for _, cb := range result.Snapshot.Callbacks {
		if cb.Phase == CallbackPhaseConstruction {
			t.Error("a construction callback appeared in the tick's callbacks")
		}
	}
	if len(result.Snapshot.Construction) != len(plan.construction) {
		t.Error("snapshot construction differs from the plan's construction record")
	}
}

// TestCapturedInspectionMakesNoApplicationCall seals every application callback
// and then runs each inspection path. A panic here is the failure this test
// exists to prevent.
func TestCapturedInspectionMakesNoApplicationCall(t *testing.T) {
	plan, cond, _ := newSealedFixture(t, CoherenceSnapshot)
	tickUntilSettled(t, plan)
	sealFixture(cond)

	old := bt.DefaultPrinter
	bt.DefaultPrinter = Printer
	t.Cleanup(func() { bt.DefaultPrinter = old })

	// Every one of these must run without touching the sealed fixture.
	t.Run("walk", func(t *testing.T) {
		var seen int
		bt.Walk(plan.Node(), func(bt.Metadata) bool {
			seen++
			return true
		})
		if seen == 0 {
			t.Error("walk visited no nodes")
		}
	})
	t.Run("print", func(t *testing.T) {
		// Once the plan has settled the condition leaf has been replaced by a
		// ppa root, so the post-condition node is what remains to identify.
		s := plan.Node().String()
		if !strings.Contains(s, "PPAPost") || !strings.Contains(s, "ActionNode") {
			t.Errorf("printed tree missing the post-condition or action node: %s", s)
		}
		if !strings.Contains(s, "effects:x") {
			t.Errorf("printed tree missing the captured effect summary: %s", s)
		}
	})
	t.Run("path", func(t *testing.T) {
		if got := GetPath(plan); len(got) == 0 {
			t.Error("path is empty")
		}
	})
	t.Run("node_count", func(t *testing.T) {
		if NodeCount(plan.Node()) == 0 {
			t.Error("node count is zero")
		}
	})
	t.Run("metadata_accessors", func(t *testing.T) {
		bt.Walk(plan.Node(), func(meta bt.Metadata) bool {
			info, ok := meta.(*NodeInfo)
			if !ok {
				return true
			}
			_ = info.Type.String()
			_ = info.EffectsSummary()
			_ = info.ConditionSummary()
			return true
		})
	})
	t.Run("snapshot_rebuild", func(t *testing.T) {
		// Rebuilding a snapshot from the sealed tree must not call the
		// application either.
		if got := plan.snapshot(&tickCapture[*sealedCondition]{}); got.RootID != plan.root.id {
			t.Error("snapshot root id changed between builds")
		}
	})
	t.Run("captured_actions_and_effects", func(t *testing.T) {
		bt.Walk(plan.Node(), func(meta bt.Metadata) bool {
			if info, ok := meta.(*NodeInfo); ok {
				_ = info.Effects
			}
			return true
		})
	})
}

// TestOccurrenceIdentitySurvivesConflictResolution proves an occurrence keeps
// its identity when conflict resolution moves its node, and that the
// post-condition copy records where it came from.
func TestOccurrenceIdentitySurvivesConflictResolution(t *testing.T) {
	// Two goals over disjoint variables, each with an action that breaks the
	// other's condition, forces a conflict and therefore a move.
	variables := map[string]any{"a": "start", "b": "start"}
	condA := &sealedCondition{key: "a", expected: "done"}
	condB := &sealedCondition{key: "b", expected: "done"}
	state := &sealedState{variables: variables}
	state.apply = func() { variables["a"] = "done" }
	plan, err := NewCaptured(
		state,
		[]Conditions[*sealedCondition]{{condA}, {condB}},
		WithStateViews[*sealedCondition](&sealedProvider{state: state, coherence: CoherenceSnapshot}),
	)
	if err != nil {
		t.Fatalf("NewCaptured: %v", err)
	}
	// Record the identities before any conflict resolution can move a node.
	before := map[OccurrenceID]bool{}
	var record func(n *node[*sealedCondition])
	record = func(n *node[*sealedCondition]) {
		before[n.id] = true
		for c := n.first; c != nil; c = c.next {
			record(c)
		}
	}
	record(plan.root)
	if len(before) < 2 {
		t.Fatal("expected the initial tree to have several identified nodes")
	}

	for range 6 {
		plan.TickCaptured(TickEnvironment{})
	}

	after := map[OccurrenceID]bool{}
	var check func(n *node[*sealedCondition])
	check = func(n *node[*sealedCondition]) {
		if !ValidOccurrenceID(n.id) {
			t.Errorf("node id %q is not a valid occurrence id", n.id)
		}
		after[n.id] = true
		for c := n.first; c != nil; c = c.next {
			check(c)
		}
	}
	check(plan.root)

	var survived int
	for id := range before {
		if after[id] {
			survived++
		}
	}
	if survived == 0 {
		t.Fatal("no occurrence identity survived the conflict resolution that reordered the tree")
	}
}

// TestVerdictAuthorityRequiresCoherentRevisions proves the planner, not the
// verifier, decides whether a claim is authoritative, and that two live reads
// cannot support one.
func TestVerdictAuthorityRequiresCoherentRevisions(t *testing.T) {
	for _, test := range []struct {
		name      string
		coherence StateCoherence
		want      bool
	}{
		{name: "snapshot", coherence: CoherenceSnapshot, want: true},
		{name: "revisioned", coherence: CoherenceRevisioned, want: true},
		{name: "live", coherence: CoherenceLive, want: false},
		{name: "unknown", coherence: CoherenceUnknown, want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			plan, _, _ := newSealedFixture(t, test.coherence)
			result := tickUntilSettled(t, plan)
			var verdicts int
			for _, exec := range result.Snapshot.Executions {
				for _, rel := range exec.Relations {
					verdicts++
					if rel.Authoritative != test.want {
						t.Errorf("verdict authoritative = %v, want %v for coherence %q",
							rel.Authoritative, test.want, test.coherence)
					}
					if rel.Result != RelationPreserves {
						t.Errorf("verdict result = %v, want preserves", rel.Result)
					}
					if rel.Method == "" {
						t.Error("verdict has no method, so its provenance is undeclared")
					}
				}
			}
			if verdicts == 0 {
				t.Fatal("no verdicts recorded for the executed action")
			}
		})
	}
}

func TestCapturedObserverIsTheSourceOfObservedValues(t *testing.T) {
	plan, _, _ := newSealedFixture(t, CoherenceSnapshot)
	result := tickUntilSettled(t, plan)
	var executions int
	for _, exec := range result.Snapshot.Executions {
		executions++
		if !exec.Execution.StatusPresent {
			t.Error("action execution has no status")
		}
		if len(exec.Observation.EffectValues) == 0 {
			t.Fatal("observer reported no observed value")
		}
		for _, obs := range exec.Observation.EffectValues {
			if !obs.Known {
				t.Error("observed value is not marked known")
			}
			if obs.Value != "done" {
				t.Errorf("observed value = %v, want the post-action state done", obs.Value)
			}
		}
		if exec.Execution.ActionID == "" || !ValidOccurrenceID(exec.Execution.ActionID) {
			t.Errorf("execution action id = %q is not a valid occurrence id", exec.Execution.ActionID)
		}
	}
	if executions == 0 {
		t.Fatal("no action execution recorded")
	}
}

func TestUndeclaredExpectationStaysUnavailable(t *testing.T) {
	cond := &undeclaredCondition{key: "x", expected: "done"}
	variables := map[string]any{"x": "done"}
	state := &bareState{variables: variables}
	plan, err := NewCaptured(
		state,
		[]Conditions[*undeclaredCondition]{{cond}},
		WithStateViews[*undeclaredCondition](&bareProvider{state: state}),
	)
	if err != nil {
		t.Fatalf("NewCaptured: %v", err)
	}
	result := plan.TickCaptured(TickEnvironment{})
	if len(result.Snapshot.Conditions) != 1 {
		t.Fatalf("conditions = %d, want 1", len(result.Snapshot.Conditions))
	}
	got := result.Snapshot.Conditions[0]
	if got.ExpectedKnown {
		t.Errorf("expected value was declared as %v, but the condition declares none", got.Expected)
	}
	if got.Key != "x" {
		t.Errorf("condition key = %v, want x", got.Key)
	}
	// The rendering must say the declaration is absent rather than standing in a
	// value derived from the key.
	plan.root.first.precondition.captured = true
	info := plan.root.first.buildNodeInfo()
	if got := info.ConditionSummary(); got != "<undeclared>" {
		t.Errorf("condition summary = %q, want %q", got, "<undeclared>")
	}
}

type (
	bareState struct{ variables map[string]any }

	bareView struct{ state *bareState }

	bareProvider struct{ state *bareState }
)

func (s *bareState) Variable(key any) (any, error) {
	v, ok := s.variables[fmt.Sprint(key)]
	if !ok {
		return nil, fmt.Errorf(`pabt test: no variable %v`, key)
	}
	return v, nil
}

func (s *bareState) Actions(failed *undeclaredCondition) ([]Action[*undeclaredCondition], error) {
	return []Action[*undeclaredCondition]{&undeclaredAction{cond: failed}}, nil
}

type undeclaredAction struct{ cond *undeclaredCondition }

func (a *undeclaredAction) Conditions() []Conditions[*undeclaredCondition] { return nil }

func (a *undeclaredAction) Effects() Effects {
	return Effects{&undeclaredEffect{key: a.cond.key, value: a.cond.expected}}
}

func (a *undeclaredAction) Node() bt.Node {
	return bt.New(func([]bt.Node) (bt.Status, error) { return bt.Success, nil })
}

type undeclaredEffect struct {
	key   string
	value any
}

func (e *undeclaredEffect) Key() any   { return e.key }
func (e *undeclaredEffect) Value() any { return e.value }

func (v *bareView) Variable(key any) (any, error) { return v.state.Variable(key) }

func (v *bareView) Actions(failed *undeclaredCondition) ([]Action[*undeclaredCondition], error) {
	return v.state.Actions(failed)
}

func (v *bareView) Revision() StateRevision { return StateRevision{WorldRevision: "w1"} }

func (v *bareView) Coherence() StateCoherence { return CoherenceSnapshot }

func (v *bareView) Close() error { return nil }

func (p *bareProvider) OpenStateView(StateViewRequest) (TypedStateView[*undeclaredCondition], error) {
	return &bareView{state: p.state}, nil
}

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
	provider := &sealedProvider{state: state, coherence: CoherenceSnapshot}
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

func TestCapturedRejectsConcurrentTick(t *testing.T) {
	plan, _, _ := newSealedFixture(t, CoherenceSnapshot)
	entered := make(chan struct{})
	release := make(chan struct{})
	plan.gate.Lock()
	defer plan.gate.Unlock()

	go func() {
		close(entered)
		<-release
	}()
	<-entered
	result := plan.TickCaptured(TickEnvironment{})
	if result.CaptureError == nil {
		t.Fatal("a tick while the gate is held returned no capture error")
	}
	if result.Snapshot.RootID != "" {
		t.Error("a rejected tick still produced a snapshot")
	}
	close(release)
}

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

type failingProvider struct{}

func (failingProvider) OpenStateView(StateViewRequest) (TypedStateView[*sealedCondition], error) {
	return nil, fmt.Errorf(`pabt test: view unavailable`)
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

func TestNewRemainsUncaptured(t *testing.T) {
	cond := &sealedCondition{key: "x", expected: "done"}
	variables := map[string]any{"x": "done"}
	state := &sealedState{variables: variables, apply: func() {}}
	plan, err := New(state, []Conditions[*sealedCondition]{{cond}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if plan.config.views != nil {
		t.Error("a plan built with New configured a state view")
	}
	if len(plan.construction) != 0 {
		t.Error("a plan built with New recorded construction callbacks")
	}
	if plan.capture.Load() != nil {
		t.Error("a plan built with New published a capture")
	}
	// Ticking through the legacy entry point must still work unchanged.
	tick, children := plan.Node()()
	if tick == nil {
		t.Fatal("legacy Node returned no tick")
	}
	if _, err := tick(children); err != nil {
		t.Fatalf("legacy tick: %v", err)
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
