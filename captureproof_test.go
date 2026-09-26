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
	"strings"
	"testing"

	bt "github.com/joeycumines/go-behaviortree"
)

// The properties the capture boundary has to hold once a tick is over.
//
// Split out of capture_test.go for the same reason as the others. What lives
// here is not what a tick does but what must be true afterwards: that an
// occurrence identity survives conflict resolution and a move, that a verdict
// is authoritative only when its revisions can support one, that an undeclared
// expectation stays unavailable, and that recorded evidence is stable for two
// keys that render alike.

// TestOccurrenceIdentitySurvivesConflictResolution proves an occurrence keeps
// its identity when conflict resolution moves its node, and that the
// post-condition copy records where it came from.
//
// Conflict resolution only ever compares sibling ppa roots, and a goal slice
// holds one preconditions root, so two goal slices never meet. The fixture
// therefore puts both conditions in one slice, which makes their leaves siblings
// and both eligible to become ppa roots. The later expansion then finds that the
// earlier subtree's effects break its own guard, and the subtree moves leftward.
//
// The test fails if no conflict test was ever recorded or if no node changed
// parent, because a fixture that does not conflict would prove nothing about
// surviving a move.
func TestOccurrenceIdentitySurvivesConflictResolution(t *testing.T) {
	variables := map[string]any{"a": "start", "b": "start", "g1": "ok", "g2": "ok"}
	state := &conflictState{variables: variables}
	condA := &sealedCondition{key: "a", expected: "done"}
	condB := &sealedCondition{key: "b", expected: "done"}
	plan, err := NewCaptured(
		state,
		[]Conditions[*sealedCondition]{{condA, condB}},
		WithStateViews[*sealedCondition](&sealedProvider{
			coherence: CoherenceSnapshot, variable: state.Variable, actions: state.Actions,
		}),
	)
	if err != nil {
		t.Fatalf("NewCaptured: %v", err)
	}

	survey := func() (ids map[OccurrenceID]bool, parents map[OccurrenceID]OccurrenceID) {
		ids, parents = map[OccurrenceID]bool{}, map[OccurrenceID]OccurrenceID{}
		var walk func(n *node[*sealedCondition], parent OccurrenceID)
		walk = func(n *node[*sealedCondition], parent OccurrenceID) {
			ids[n.id] = true
			parents[n.id] = parent
			for c := n.first; c != nil; c = c.next {
				walk(c, n.id)
			}
		}
		walk(plan.root, "")
		return
	}
	before, parentsBefore := survey()
	// One goal slice makes the plan root its own preconditions root, so it holds
	// the root plus one leaf per condition.
	if len(before) < 3 {
		t.Fatalf("the plan has only %d identified nodes, too few for this to mean anything", len(before))
	}

	var conflictTests int
	for range 6 {
		result := plan.TickCaptured(TickEnvironment{})
		for _, cb := range result.Snapshot.Callbacks {
			if cb.Use == UseConflictTest {
				conflictTests++
			}
		}
	}
	if conflictTests == 0 {
		t.Fatal("no conflict test was ever recorded, so the fixture never conflicted")
	}

	after, parentsAfter := survey()
	var moved int
	for id := range after {
		if parentsBefore[id] != parentsAfter[id] {
			moved++
		}
	}
	if moved == 0 {
		t.Error("no node changed parent, so no subtree was actually moved")
	}
	for id := range before {
		if !after[id] {
			t.Errorf("occurrence identity %q did not survive conflict resolution", id)
		}
		if !ValidOccurrenceID(id) {
			t.Errorf("node id %q is not a valid occurrence id", id)
		}
	}
	// The one derivation the planner performs must name its source.
	var lineage int
	var check func(n *node[*sealedCondition])
	check = func(n *node[*sealedCondition]) {
		if ValidLineageID(n.lineage) {
			lineage++
		}
		for c := n.first; c != nil; c = c.next {
			check(c)
		}
	}
	check(plan.root)
	if lineage == 0 {
		t.Error("no node recorded a lineage, so the post-condition derivation is unproven")
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

func TestPostExpansionNodesReportTheConditionTicks(t *testing.T) {
	plan, _, _ := newSealedFixture(t, CoherenceSnapshot)
	for range 3 {
		plan.TickCaptured(TickEnvironment{})
	}
	snap := plan.snapshot(&tickCapture[*sealedCondition]{})
	var post, postCondition bool
	for _, n := range snap.Nodes {
		switch n.Type {
		case NodeTypePPAPost:
			post = true
			if n.Status.TickCount == 0 {
				t.Error("the post-condition node reports no ticks, but it is the node that evaluates the condition")
			}
			if !n.Status.StatusPresent {
				t.Error("the post-condition node reports no status after evaluating its condition")
			}
		case NodeTypePPARoot:
			postCondition = true
		}
	}
	if !post {
		t.Fatal("the plan was not expanded, so there is no post-condition node to inspect")
	}
	if !postCondition {
		t.Error("the expanded tree has no ppa root")
	}
}

func TestCapturedEvidenceIsStableForCollidingKeyRendering(t *testing.T) {
	// Two distinct keys render identically under %v. The order of recorded
	// effects must still be the same on every run, or two identical runs would
	// produce different evidence.
	var names []string
	for range 40 {
		state := &collidingState{variables: map[string]any{"k": "start"}}
		plan, err := NewCaptured(
			state, []Conditions[*collidingCondition]{{&collidingCondition{key: "k", expected: "done"}}},
			WithStateViews[*collidingCondition](&collidingProvider{state: state}),
		)
		if err != nil {
			t.Fatalf("NewCaptured: %v", err)
		}
		plan.TickCaptured(TickEnvironment{})
		snap := plan.snapshot(&tickCapture[*collidingCondition]{})
		for _, a := range snap.Actions {
			names = append(names, a.Name)
		}
	}
	if len(names) == 0 {
		t.Fatal("no action was generated, so there was no name to compare")
	}
	for _, n := range names {
		if n != names[0] {
			t.Fatalf("action name is not stable across runs: %q then %q", names[0], n)
		}
	}
}

// secretValue is a value whose type declares it secret. Nothing about the name
// would tell a reader so, which is the point: the declaration is the only reason
// it is protected, and nothing downstream of the declaration can protect it once
// the value has been flattened into a name.
type secretValue struct{ text string }

func (secretValue) PabtSensitive() bool { return true }

// TestAnEffectValueNeverReachesAName is the property that a name is not a place to
// keep a value.
//
// An action's name is built from its effects, and it used to format each effect's
// value into that name. A name is plain text: it passes through no codec, no
// redaction walk, and no diagnostic sanitizer, so a declared secret formatted into
// one was written into every record mentioning the node, into every digest over
// those records, and into every export and screenshot containing them - with no
// mechanism recorded, because by the time the value was text there was nothing
// left to recognise. The evidence run that found this was looking for a planted
// secret in an exported bundle and found it in a node's name.
//
// The effect itself must still carry the value. Hiding it from a name is not a
// reason to stop recording it.
func TestAnEffectValueNeverReachesAName(t *testing.T) {
	const declared = "a-declared-secret-value"
	condition := &sealedCondition{key: "x", expected: "done"}
	state := &sealedState{variables: map[string]any{"x": "start"}}
	state.apply = func() { state.variables["x"] = "done" }
	plan, err := NewCaptured(
		state,
		[]Conditions[*sealedCondition]{{condition}},
		WithStateViews[*sealedCondition]((&sealedProvider{coherence: CoherenceSnapshot}).with(
			state.Variable,
			func(*sealedCondition) ([]Action[*sealedCondition], error) {
				// The action declares the condition's own effect and one more whose
				// value the application has declared secret.
				return []Action[*sealedCondition]{&sealedAction{
					effects: Effects{
						&sealedEffect{key: "x", value: "done"},
						&sealedEffect{key: "audit.last", value: secretValue{text: declared}},
					},
					node: bt.New(func([]bt.Node) (bt.Status, error) {
						state.apply()
						return bt.Success, nil
					}),
				}}, nil
			},
		)),
	)
	if err != nil {
		t.Fatalf("NewCaptured: %v", err)
	}
	tickUntilSettled(t, plan)
	snapshot := plan.snapshot(&tickCapture[*sealedCondition]{})
	if len(snapshot.Actions) == 0 {
		t.Fatal("the settled plan reported no action to name")
	}
	for _, action := range snapshot.Actions {
		if strings.Contains(action.Name, declared) {
			t.Errorf("the action's name carries its effect's value: %q", action.Name)
		}
		if !strings.Contains(action.Name, "audit.last") {
			t.Errorf("the action's name = %q, want it to name the effect it touches", action.Name)
		}
	}
	for _, node := range snapshot.Nodes {
		if strings.Contains(node.Name, declared) {
			t.Errorf("node %s is named with the value its action declared: %q", node.ID, node.Name)
		}
	}
	var recorded bool
	for _, effect := range snapshot.Effects {
		if _, ok := effect.Value.(secretValue); ok && effect.ValueKnown {
			recorded = true
		}
	}
	if !recorded {
		t.Error("the effect's value is no longer recorded at all, which is not what hiding it from a name means")
	}
}
