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

package pabtdebug

import (
	"context"
	"errors"
	"testing"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt"
)

// sessionFixture is a small planner whose world the test controls.
type sessionFixture struct {
	condition *fixtureCondition
	state     *fixtureState
	plan      *pabt.Plan[*fixtureCondition]
	observer  *fixtureObserver
	verifier  *fixtureVerifier
	// panicOn makes the action node panic, so a panic can be turned into a
	// terminal record rather than an unwound test.
	panicOn bool
	// closeFails makes the capture boundary report a close failure.
	closeFails bool
}

type fixtureCondition struct {
	key      string
	expected any
	sealed   bool
}

func (c *fixtureCondition) Key() any {
	if c.sealed {
		panic("fixture: the key was read after sealing")
	}
	return c.key
}

func (c *fixtureCondition) Match(value any) bool {
	if c.sealed {
		panic("fixture: the condition was matched after sealing")
	}
	return value == c.expected
}

func (c *fixtureCondition) Expected() any { return c.expected }

type fixtureState struct {
	variables map[string]any
	// panics makes the action node panic, so a panic becomes a terminal record
	// rather than an unwound test.
	panics bool
}

func (s *fixtureState) Variable(key any) (any, error) {
	return s.variables[toString(key)], nil
}

func (s *fixtureState) Actions(failed *fixtureCondition) ([]pabt.Action[*fixtureCondition], error) {
	return []pabt.Action[*fixtureCondition]{&fixtureAction{
		effects: []pabt.Effect{effect{key: failed.key, value: failed.expected}},
		apply:   func() { s.variables[failed.key] = failed.expected },
		panicOn: s.panics,
	}}, nil
}

func toString(v any) string {
	s, _ := v.(string)
	return s
}

type effect struct {
	key   string
	value any
}

func (e effect) Key() any   { return e.key }
func (e effect) Value() any { return e.value }

type fixtureAction struct {
	effects []pabt.Effect
	apply   func()
	panicOn bool
}

func (a *fixtureAction) Conditions() []pabt.Conditions[*fixtureCondition] { return nil }
func (a *fixtureAction) Effects() pabt.Effects                            { return a.effects }

func (a *fixtureAction) Node() bt.Node {
	return bt.New(func([]bt.Node) (bt.Status, error) {
		if a.panicOn {
			panic("fixture: the action panicked")
		}
		a.apply()
		return bt.Success, nil
	})
}

// fixtureObserver reads the post-action world through the view it is handed,
// which is what makes the observed value a real observation rather than a guess.
type fixtureObserver struct{}

func (fixtureObserver) ObserveExecution(
	req pabt.ActionObservationRequest,
) (pabt.ActionObservation, error) {
	out := pabt.ActionObservation{}
	for _, e := range req.Effects {
		value, err := req.View.Variable(e.Key)
		if err != nil {
			continue
		}
		out.EffectValues = append(out.EffectValues, pabt.EffectObservation{
			EffectID: e.ID, Value: value, Known: true,
		})
	}
	return out, nil
}

type fixtureVerifier struct{}

func (fixtureVerifier) VerifyTransitions(
	req pabt.TransitionRequest,
) ([]pabt.RelationVerdict, error) {
	out := make([]pabt.RelationVerdict, 0, len(req.Effects))
	for _, e := range req.Effects {
		result := pabt.RelationDoesNotDecide
		for _, seen := range req.Observation.EffectValues {
			if seen.EffectID == e.ID && seen.Known && seen.Value == e.Value {
				result = pabt.RelationPreserves
			}
		}
		out = append(out, pabt.RelationVerdict{
			EffectID: e.ID, PostconditionID: req.PostconditionID,
			Result: result, Method: "observed_value_equals_declared_value",
		})
	}
	return out, nil
}

func newSessionFixture(t *testing.T) *sessionFixture {
	t.Helper()
	f := &sessionFixture{
		condition: &fixtureCondition{key: "x", expected: "done"},
		state:     &fixtureState{variables: map[string]any{"x": "start"}},
		observer:  &fixtureObserver{},
		verifier:  &fixtureVerifier{},
	}
	plan, err := pabt.NewCaptured(
		f.state,
		[]pabt.Conditions[*fixtureCondition]{{f.condition}},
		pabt.WithExecutionObserver[*fixtureCondition](f.observer),
		pabt.WithTransitionVerifier[*fixtureCondition](f.verifier),
	)
	if err != nil {
		t.Fatalf("NewCaptured: %v", err)
	}
	f.plan = plan
	return f
}

func newSession(t *testing.T, f *sessionFixture) (*Session, *SQLiteStore, RunKey) {
	t.Helper()
	store := openTestStore(t, testStorePath(t))
	identity := bootstrapTestStore(t, store)
	session, err := OpenSession(context.Background(), store, f.plan, SessionConfig{
		Identity:    identity,
		Codecs:      NewCodecRegistry(DefaultValueLimits()),
		ValueLimits: DefaultValueLimits(),
	})
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	return session, store, session.RunKey()
}

func TestSessionRecordsATickThatSettles(t *testing.T) {
	f := newSessionFixture(t)
	session, store, key := newSession(t, f)

	// The first tick expands, the second runs the action and confirms the goal.
	var last TickResult
	for range 4 {
		last = session.Tick(context.Background())
		if last.Kind == RecordTick && last.StatusPresent && bt.Status(last.Status) == bt.Success {
			break
		}
		if last.Err != nil {
			t.Fatalf("Tick: %v", last.Err)
		}
	}
	if last.Kind != RecordTick {
		t.Fatalf("the settled tick is %q, want a tick", last.Kind)
	}
	if last.Sequence == 0 {
		t.Fatal("a settled tick committed nothing")
	}
	record, err := store.RecordAt(context.Background(), key, last.Sequence)
	if err != nil {
		t.Fatalf("RecordAt: %v", err)
	}
	if record.Kind != RecordTick {
		t.Errorf("the stored record kind = %q", record.Kind)
	}
	if !record.TreeEligible || record.Tree == nil {
		t.Fatal("a settled tick stored no tree")
	}
	if len(record.Conditions) == 0 {
		t.Error("a settled tick stored no conditions, so the explanation is empty")
	}
	// The four fields stay apart, and an expected value is the declaration rather
	// than anything derived from the key.
	found := false
	for _, condition := range record.Conditions {
		if condition.Expected.Visibility != ValueExact {
			t.Errorf("condition %s expected = %q, want the declared value",
				condition.ConditionID, condition.Expected.Visibility)
		}
		if condition.Current.Visibility != ValueExact {
			t.Errorf("condition %s current = %q, want an observed current value",
				condition.ConditionID, condition.Current.Visibility)
		}
		// The post-condition is evaluated before the action runs within a tick, so
		// the settling tick still reads failure. The claim is only that the truth is
		// a real status word rather than a guess.
		if condition.Truth != "failure" && condition.Truth != "success" {
			t.Errorf("condition %s truth = %q, want a status word", condition.ConditionID, condition.Truth)
		}
		found = true
	}
	if !found {
		t.Error("no condition was recorded")
	}
	// A verifier's claim is recorded with the authority the planner gave it. The
	// session's default state view is the live one, so the revision bracketing an
	// action cannot support an authoritative claim, and the record says so rather
	// than presenting a conclusion it does not have grounds for.
	if len(record.Relations) == 0 {
		t.Fatal("a settled tick recorded no relation")
	}
	relation := record.Relations[0]
	if relation.Authoritative {
		t.Error("a relation is authoritative under a live view, which cannot support it")
	}
	if relation.Authority != "reported_not_authoritative" {
		t.Errorf("the recorded authority is %q, want the non-authoritative word", relation.Authority)
	}
	if relation.Method == "" {
		t.Error("a relation records no method, so its provenance is undeclared")
	}
	if verified, err := store.VerifyChain(context.Background(), key); err != nil {
		t.Errorf("VerifyChain: %v", err)
	} else if verified != last.Sequence {
		t.Errorf("the chain verified %d records, want %d", verified, last.Sequence)
	}
}

func TestAPlannerPanicNeverSettlesAndLeavesTheRunIntact(t *testing.T) {
	// An action that panics must never be reported as a settled goal, and whatever
	// the tick does become, the run has to stay verifiable.
	f := newSessionFixture(t)
	session, store, key := newSession(t, f)
	f.state.panics = true
	ctx := context.Background()

	committed := uint64(0)
	for range 4 {
		result := session.Tick(ctx)
		if result.Err != nil {
			// A reported failure is acceptable; a settled success is not.
			continue
		}
		if result.StatusPresent && bt.Status(result.Status) == bt.Success {
			t.Fatal("a panicking action was reported as a settled goal")
		}
		if result.Sequence > committed {
			committed = result.Sequence
		}
		if result.Kind == RecordPanicked && result.Sequence > 0 {
			record, err := store.RecordAt(ctx, key, result.Sequence)
			if err != nil {
				t.Fatalf("the panicked record is not there: %v", err)
			}
			if record.Kind != RecordPanicked {
				t.Errorf("the stored kind = %q, want panicked", record.Kind)
			}
			if record.StatusPresent {
				t.Error("a panicked record claims a status")
			}
		}
	}
	if _, err := store.VerifyChain(ctx, key); err != nil {
		t.Errorf("the chain does not verify after a panic: %v", err)
	}
	pending, err := store.PendingIntents(ctx)
	if err != nil {
		t.Fatalf("PendingIntents: %v", err)
	}
	if len(pending) > 0 {
		t.Errorf("%d attempts are still open after a panicking run", len(pending))
	}
}

func TestATickIsSerialisedAndTheHeadNeverSkips(t *testing.T) {
	f := newSessionFixture(t)
	session, store, key := newSession(t, f)
	ctx := context.Background()

	var previous uint64
	for range 6 {
		result := session.Tick(ctx)
		if result.Err != nil {
			t.Fatalf("Tick: %v", result.Err)
		}
		if result.Sequence == 0 {
			continue
		}
		if result.Sequence != previous+1 {
			t.Fatalf("sequence went from %d to %d, so a sequence was skipped or reused",
				previous, result.Sequence)
		}
		previous = result.Sequence
	}
	head, err := store.Head(ctx, key)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.HeadSequence != previous {
		t.Errorf("the head is at %d, the last committed sequence was %d", head.HeadSequence, previous)
	}
	// Every committed sequence is one terminal record, and the run never ended up
	// claiming a tick it does not hold.
	if verified, err := store.VerifyChain(ctx, key); err != nil {
		t.Errorf("VerifyChain: %v", err)
	} else if verified != previous {
		t.Errorf("the chain verified %d records, want %d", verified, previous)
	}
	pending, err := store.PendingIntents(ctx)
	if err != nil {
		t.Fatalf("PendingIntents: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("%d attempts are still open after a settled run", len(pending))
	}
}

func TestASecondTickIsRefusedRatherThanInterleaved(t *testing.T) {
	f := newSessionFixture(t)
	session, _, _ := newSession(t, f)
	if !session.gate.TryLock() {
		t.Fatal("the session gate was already held")
	}
	defer session.gate.Unlock()
	result := session.Tick(context.Background())
	if result.Err == nil {
		t.Error("a concurrent tick was allowed")
	}
	if result.Sequence != 0 {
		t.Error("a refused tick reported a sequence")
	}
}

func TestTheSessionRefusesAnUnusableConfiguration(t *testing.T) {
	f := newSessionFixture(t)
	store := openTestStore(t, testStorePath(t))
	identity := bootstrapTestStore(t, store)
	ctx := context.Background()
	if _, err := OpenSession(ctx, nil, f.plan, SessionConfig{Identity: identity}); err == nil {
		t.Error("a session with no store was opened")
	}
	if _, err := OpenSession(ctx, store, nil, SessionConfig{Identity: identity}); err == nil {
		t.Error("a session with no planner was opened")
	}
	if _, err := OpenSession(ctx, store, f.plan, SessionConfig{}); err == nil {
		t.Error("a session with no run identity was opened")
	}
	if _, err := OpenSession(ctx, store, f.plan, SessionConfig{
		Identity: SessionIdentity{SessionID: "nope", PlanID: "p", RunID: "r"},
	}); err == nil {
		t.Error("a session attached to a run that does not exist was opened")
	} else if !errors.Is(err, ErrNotFound) {
		t.Errorf("attaching to a missing run = %v, want not found", err)
	}
}
