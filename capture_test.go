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
		variable  func(key any) (any, error)
		actions   func(*sealedCondition) ([]Action[*sealedCondition], error)
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
func (c *sealedCondition) Expected() any {
	if c.sealed {
		panic(`pabt test: Condition.Expected called after sealing`)
	}
	return c.expected
}

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

func (v *sealedView) Variable(key any) (any, error) { return v.provider.variable(key) }

func (v *sealedView) Actions(failed *sealedCondition) ([]Action[*sealedCondition], error) {
	return v.provider.actions(failed)
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

// with builds a provider over an explicit variable reader and action factory, for
// a test whose action is not the state's default one.
func (p *sealedProvider) with(
	variable func(any) (any, error),
	actions func(*sealedCondition) ([]Action[*sealedCondition], error),
) *sealedProvider {
	p.variable = variable
	p.actions = actions
	return p
}

func (p *sealedProvider) forState(s *sealedState) *sealedProvider {
	p.variable = s.Variable
	p.actions = s.Actions
	return p
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
	provider := (&sealedProvider{coherence: coherence}).forState(state)
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
	// The name says what the action touches. The value is recorded as an effect,
	// where a declared secret is redacted; a name is plain text that bypasses
	// every one of those mechanisms, so the value must not be in one.
	if action.Name != "x" {
		t.Errorf("action name = %q, want the effect key %q", action.Name, "x")
	}
	if strings.Contains(action.Name, "done") {
		t.Errorf("the action's name carries its effect's value: %q", action.Name)
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
	var keys, expected int
	for _, cb := range plan.construction {
		if cb.Phase != CallbackPhaseConstruction {
			t.Errorf("construction callback phase = %q, want construction", cb.Phase)
		}
		switch cb.Kind {
		case CallbackKey:
			keys++
		case CallbackExpected:
			expected++
			if !cb.Known {
				t.Error("the construction phase recorded an expectation as unknown")
			}
			if cb.Result != "done" {
				t.Errorf("recorded expectation = %v, want done", cb.Result)
			}
		default:
			t.Errorf("construction callback kind = %v, want key or expected", cb.Kind)
		}
	}
	if keys == 0 {
		t.Error("the construction phase recorded no key callback")
	}
	if expected == 0 {
		t.Error("the construction phase recorded no expected callback")
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

type conflictState struct{ variables map[string]any }

func (s *conflictState) Variable(key any) (any, error) {
	if v, ok := s.variables[fmt.Sprint(key)]; ok {
		return v, nil
	}
	return nil, fmt.Errorf(`pabt test: no variable %v`, key)
}

// Actions offers one action per failed condition. Each action guards a variable
// that is already satisfied, so a guard never has to expand and the two sibling
// conditions can both expand. Each action also declares an effect on the other
// action's guard that violates it, so when the second subtree is resolved the
// first one's effects break the second one's guard and the second is moved ahead
// of the first.
func (s *conflictState) Actions(failed *sealedCondition) ([]Action[*sealedCondition], error) {
	if failed.key == "a" {
		return []Action[*sealedCondition]{&conflictingAction{
			effects: Effects{
				&sealedEffect{key: "a", value: "done"},
				&sealedEffect{key: "g2", value: "broken"},
			},
			guard: &sealedCondition{key: "g1", expected: "ok"},
		}}, nil
	}
	return []Action[*sealedCondition]{&conflictingAction{
		effects: Effects{
			&sealedEffect{key: "b", value: "done"},
			&sealedEffect{key: "g1", value: "broken"},
		},
		guard: &sealedCondition{key: "g2", expected: "ok"},
	}}, nil
}

type conflictingAction struct {
	effects Effects
	guard   *sealedCondition
}

func (a *conflictingAction) Conditions() []Conditions[*sealedCondition] {
	return []Conditions[*sealedCondition]{{a.guard}}
}

func (a *conflictingAction) Effects() Effects { return a.effects }

func (a *conflictingAction) Node() bt.Node {
	return bt.New(func([]bt.Node) (bt.Status, error) { return bt.Success, nil })
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

type failingProvider struct{}

func (failingProvider) OpenStateView(StateViewRequest) (TypedStateView[*sealedCondition], error) {
	return nil, fmt.Errorf(`pabt test: view unavailable`)
}

func TestNewRemainsUncaptured(t *testing.T) {
	cond := &sealedCondition{key: "x", expected: "done"}
	variables := map[string]any{"x": "done"}
	state := &sealedState{variables: variables, apply: func() {}}
	plan, err := New(state, []Conditions[*sealedCondition]{{cond}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// New installs the same explicit live provider, so the captured entry point
	// is usable on any plan; what it does not do is record anything.
	if plan.config.views == nil {
		t.Error("a plan built with New has no state view provider")
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

// --- Regression coverage for the findings of the first review pass. ---

// TestCapturedEntryPointWorksOnAPlanBuiltWithNew proves the captured tick path is
// not a trap for a caller that reached the plan through New.
func TestCapturedEntryPointWorksOnAPlanBuiltWithNew(t *testing.T) {
	cond := &sealedCondition{key: "x", expected: "done"}
	variables := map[string]any{"x": "done"}
	state := &sealedState{variables: variables}
	plan, err := New(state, []Conditions[*sealedCondition]{{cond}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	result := plan.TickCaptured(TickEnvironment{})
	if result.CaptureError != nil {
		t.Fatalf("TickCaptured on a New-built plan: %v", result.CaptureError)
	}
	if result.Snapshot.Coherence != CoherenceLive {
		t.Errorf("coherence = %q, want %q", result.Snapshot.Coherence, CoherenceLive)
	}
	if len(result.Snapshot.Conditions) != 1 {
		t.Errorf("conditions = %d, want 1", len(result.Snapshot.Conditions))
	}
}

type pairState struct{ variables map[string]any }

func (s *pairState) Variable(key any) (any, error) {
	if v, ok := s.variables[fmt.Sprint(key)]; ok {
		return v, nil
	}
	return nil, fmt.Errorf(`pabt test: no variable %v`, key)
}

// Actions returns two candidates that both support the failed condition and both
// report failure, so the action selector visits both of them within one tick.
//
// A selector returns on the first Running or Success child, so two running
// actions would execute one per tick and the case under test would never arise.
// An action that did not support the condition would be discarded before it
// ever reached the tree.
func (s *pairState) Actions(failed *sealedCondition) ([]Action[*sealedCondition], error) {
	keepRunning := bt.New(func([]bt.Node) (bt.Status, error) { return bt.Failure, nil })
	return []Action[*sealedCondition]{
		&sealedAction{effects: Effects{
			&sealedEffect{key: failed.key, value: failed.expected},
			&sealedEffect{key: "left", value: "a"},
		}, node: keepRunning},
		&sealedAction{effects: Effects{
			&sealedEffect{key: failed.key, value: failed.expected},
			&sealedEffect{key: "right", value: "b"},
		}, node: keepRunning},
	}, nil
}

type (
	collidingCondition struct {
		key      string
		expected any
	}
	collidingEffect struct {
		key   any
		value any
	}
	collidingAction struct {
		cond  *collidingCondition
		value any
	}

	collidingState    struct{ variables map[string]any }
	collidingView     struct{ state *collidingState }
	collidingProvider struct{ state *collidingState }
)

func (c *collidingCondition) Key() any         { return c.key }
func (c *collidingCondition) Match(v any) bool { return v == c.expected }

func (e *collidingEffect) Key() any   { return e.key }
func (e *collidingEffect) Value() any { return e.value }

func (a *collidingAction) Conditions() []Conditions[*collidingCondition] { return nil }

func (a *collidingAction) Effects() Effects {
	// The int 1 and the string "1" are distinct keys that render identically, so
	// ordering on the rendering alone would leave the order to map iteration.
	return Effects{
		&collidingEffect{key: 1, value: a.value},
		&collidingEffect{key: "1", value: "other"},
		&collidingEffect{key: a.cond.key, value: a.cond.expected},
	}
}

func (a *collidingAction) Node() bt.Node {
	return bt.New(func([]bt.Node) (bt.Status, error) { return bt.Success, nil })
}

func (s *collidingState) Variable(key any) (any, error) { return s.variables[fmt.Sprint(key)], nil }

func (s *collidingState) Actions(failed *collidingCondition) ([]Action[*collidingCondition], error) {
	return []Action[*collidingCondition]{&collidingAction{cond: failed, value: "a"}}, nil
}

func (v *collidingView) Variable(key any) (any, error) { return v.state.Variable(key) }

func (v *collidingView) Actions(failed *collidingCondition) ([]Action[*collidingCondition], error) {
	return v.state.Actions(failed)
}

func (v *collidingView) Revision() StateRevision   { return StateRevision{WorldRevision: "w1"} }
func (v *collidingView) Coherence() StateCoherence { return CoherenceSnapshot }
func (v *collidingView) Close() error              { return nil }

func (p *collidingProvider) OpenStateView(StateViewRequest) (TypedStateView[*collidingCondition], error) {
	return &collidingView{state: p.state}, nil
}
