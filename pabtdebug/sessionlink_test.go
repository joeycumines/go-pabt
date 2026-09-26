/*
   Copyright 2026 Joseph Cumines

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
	"net/http"
	"strconv"
	"testing"

	bt "github.com/joeycumines/go-behaviortree"
)

// settledRecord ticks a fixture until it settles and returns the record it
// committed, which is the only shape in which the linkage is complete: the first
// tick expands the plan and the action has not run yet.
func settledRecord(t *testing.T) StoredRecord {
	t.Helper()
	f := newSessionFixture(t)
	session, store, key := newSession(t, f)
	var result TickResult
	for range 4 {
		result = session.Tick(context.Background())
		if result.Err != nil {
			t.Fatalf("Tick: %v", result.Err)
		}
		if result.Kind == RecordTick && result.StatusPresent && bt.Status(result.Status) == bt.Success {
			break
		}
	}
	if result.Kind != RecordTick {
		t.Fatalf("the settled tick is %q, want a tick", result.Kind)
	}
	record, err := store.RecordAt(context.Background(), key, result.Sequence)
	if err != nil {
		t.Fatalf("RecordAt: %v", err)
	}
	return record
}

// TestARecordNamesTheOccurrenceEachConditionBelongsTo is the property the
// browser client is built on: a selected occurrence can be told which
// preconditions govern it, because the record says which nodes evaluate which
// condition. Before this existed every one of those links was null and the
// question had no answer in the data.
func TestARecordNamesTheOccurrenceEachConditionBelongsTo(t *testing.T) {
	record := settledRecord(t)
	if len(record.Conditions) == 0 {
		t.Fatal("the settled tick recorded no condition")
	}
	nodes := map[string]StoredNode{}
	for _, node := range record.Tree.Nodes {
		nodes[node.ID] = node
	}
	for _, condition := range record.Conditions {
		if condition.NodeID == nil {
			t.Errorf("condition %s names no node that evaluates it", condition.ConditionID)
			continue
		}
		if _, ok := nodes[*condition.NodeID]; !ok {
			t.Errorf("condition %s names node %s, which the tree does not hold", condition.ConditionID, *condition.NodeID)
		}
		if condition.PostNodeID == nil {
			t.Errorf("condition %s names no post-condition node", condition.ConditionID)
			continue
		}
		if _, ok := nodes[*condition.PostNodeID]; !ok {
			t.Errorf("condition %s names post node %s, which the tree does not hold",
				condition.ConditionID, *condition.PostNodeID)
		}
		if *condition.PostNodeID == *condition.NodeID {
			t.Errorf("condition %s names the same node twice, as the condition and its post",
				condition.ConditionID)
		}
	}
}

// TestARecordCarriesTheConditionKeyAsAValue checks the field a reader needs to
// know which variable a condition is about, and that it arrived the way every
// other piece of application data arrives: through the codec, so it is scrubbed
// and can be redacted.
func TestARecordCarriesTheConditionKeyAsAValue(t *testing.T) {
	record := settledRecord(t)
	condition := record.Conditions[0]
	if condition.Key.Display == "" {
		t.Fatalf("condition %s carries no key: %+v", condition.ConditionID, condition.Key)
	}
	if condition.Key.Visibility == ValueUnknown || condition.Key.Visibility == ValueError {
		t.Errorf("the key is %q, want a readable value", condition.Key.Visibility)
	}
	if condition.Key.Source == "" {
		t.Error("the key records no source, so a reader cannot tell which condition it belongs to")
	}
	// The key is the subject of the condition. It must not be presented as the
	// requirement, which is the guess the four evidence fields exist to prevent.
	if condition.Key.Display == condition.Expected.Display && condition.Expected.Visibility != ValueUnknown {
		t.Log("the key and the expected value render alike here; the client labels them separately")
	}
}

// TestARecordCarriesTheActionsAndEffectsAroundACondition is the relation a
// consumer cannot infer: which effect an action declared, and which condition
// that action was there to make.
func TestARecordCarriesTheActionsAndEffectsAroundACondition(t *testing.T) {
	record := settledRecord(t)
	if len(record.Actions) == 0 {
		t.Fatal("the settled tick recorded no action")
	}
	if len(record.Effects) == 0 {
		t.Fatal("the settled tick recorded no effect")
	}
	nodes := map[string]StoredNode{}
	for _, node := range record.Tree.Nodes {
		nodes[node.ID] = node
	}
	conditions := map[string]StoredCondition{}
	for _, condition := range record.Conditions {
		conditions[condition.ConditionID] = condition
	}
	effects := map[string]StoredEffect{}
	for _, effect := range record.Effects {
		effects[effect.EffectID] = effect
	}
	actions := map[string]bool{}
	for _, action := range record.Actions {
		actions[action.ActionID] = true
		if action.NodeID == nil {
			t.Errorf("action %s names no node", action.ActionID)
		} else if _, ok := nodes[*action.NodeID]; !ok {
			t.Errorf("action %s names node %s, which the tree does not hold", action.ActionID, *action.NodeID)
		}
		if action.PostconditionID == nil {
			t.Errorf("action %s names no postcondition, so a reader cannot say what it was for", action.ActionID)
			continue
		}
		if _, ok := conditions[*action.PostconditionID]; !ok {
			t.Errorf("action %s names postcondition %s, which the record does not hold",
				action.ActionID, *action.PostconditionID)
		}
		for _, id := range action.ConditionIDs {
			if _, ok := conditions[id]; !ok {
				t.Errorf("action %s is guarded by %s, which the record does not hold", action.ActionID, id)
			}
		}
		for _, id := range action.EffectIDs {
			if _, ok := effects[id]; !ok {
				t.Errorf("action %s declares effect %s, which the record does not hold", action.ActionID, id)
			}
		}
	}
	for _, effect := range record.Effects {
		if !actions[effect.ActionID] {
			t.Errorf("effect %s names action %s, which the record does not hold", effect.EffectID, effect.ActionID)
		}
	}
}

// TestATreeNodeSaysWhatItIs checks the node's own links, which are what let a
// tree be read without joining the sections above.
func TestATreeNodeSaysWhatItIs(t *testing.T) {
	record := settledRecord(t)
	conditions := map[string]bool{}
	for _, condition := range record.Conditions {
		conditions[condition.ConditionID] = true
	}
	effects := map[string]bool{}
	for _, effect := range record.Effects {
		effects[effect.EffectID] = true
	}
	actions := map[string]bool{}
	for _, action := range record.Actions {
		actions[action.ActionID] = true
	}
	nodes := map[string]bool{}
	for _, node := range record.Tree.Nodes {
		nodes[node.ID] = true
	}
	var withCondition, withPost, withAction int
	for _, node := range record.Tree.Nodes {
		if node.ConditionID != nil {
			withCondition++
			if !conditions[*node.ConditionID] {
				t.Errorf("node %s names condition %s, which the record does not hold", node.ID, *node.ConditionID)
			}
		}
		if node.PostNodeID != nil {
			withPost++
			if !nodes[*node.PostNodeID] {
				t.Errorf("node %s names post node %s, which the tree does not hold", node.ID, *node.PostNodeID)
			}
		}
		if node.ActionID != nil {
			withAction++
			if !actions[*node.ActionID] {
				t.Errorf("node %s names action %s, which the record does not hold", node.ID, *node.ActionID)
			}
			for _, id := range node.EffectIDs {
				if !effects[id] {
					t.Errorf("node %s names effect %s, which the record does not hold", node.ID, id)
				}
			}
		}
	}
	// A settled tick that linked nothing would still pass every check above, so
	// the count is what says the population actually happened.
	if withCondition == 0 {
		t.Error("no node names a condition")
	}
	if withPost == 0 {
		t.Error("no node names a post-condition")
	}
	if withAction == 0 {
		t.Error("no node names an action")
	}
}

// TestTheLinkageIsInsideWhatTheDigestCovers checks that a link is part of what
// the record commits to. A link outside the digest could be rewritten without the
// record's hash noticing, which would make the whole structure unevidenced.
func TestTheLinkageIsInsideWhatTheDigestCovers(t *testing.T) {
	record := settledRecord(t)
	altered := record
	altered.Actions = append([]StoredAction(nil), record.Actions...)
	if len(altered.Actions) == 0 {
		t.Skip("no action to alter")
	}
	altered.Actions[0].PostconditionID = nil
	before, err := HashRecord(record)
	if err != nil {
		t.Fatalf("HashRecord: %v", err)
	}
	after, err := HashRecord(altered)
	if err != nil {
		t.Fatalf("HashRecord: %v", err)
	}
	if before == after {
		t.Error("dropping an action's postcondition did not change the record digest")
	}
	altered = record
	altered.Conditions = append([]StoredCondition(nil), record.Conditions...)
	altered.Conditions[0].NodeID = nil
	after, err = HashRecord(altered)
	if err != nil {
		t.Fatalf("HashRecord: %v", err)
	}
	if before == after {
		t.Error("dropping a condition's node did not change the record digest")
	}
}

// TestTheRecordWithItsLinkageRoundTrips proves the new sections survive the
// canonical encoding the store and the wire both use.
func TestTheRecordWithItsLinkageRoundTrips(t *testing.T) {
	record := settledRecord(t)
	encoded, err := CanonicalEncode(record)
	if err != nil {
		t.Fatalf("CanonicalEncode: %v", err)
	}
	var decoded StoredRecord
	if err := CanonicalDecode(encoded, &decoded); err != nil {
		t.Fatalf("CanonicalDecode: %v", err)
	}
	if len(decoded.Actions) != len(record.Actions) {
		t.Errorf("actions round-tripped to %d, want %d", len(decoded.Actions), len(record.Actions))
	}
	if len(decoded.Effects) != len(record.Effects) {
		t.Errorf("effects round-tripped to %d, want %d", len(decoded.Effects), len(record.Effects))
	}
	if len(decoded.Conditions) != len(record.Conditions) {
		t.Fatalf("conditions round-tripped to %d, want %d", len(decoded.Conditions), len(record.Conditions))
	}
	if decoded.Conditions[0].Key.Display != record.Conditions[0].Key.Display {
		t.Errorf("the key round-tripped to %q, want %q",
			decoded.Conditions[0].Key.Display, record.Conditions[0].Key.Display)
	}
	if decoded.Version != FormatVersion {
		t.Errorf("the record round-tripped at version %d, want %d", decoded.Version, FormatVersion)
	}
}

// TestTheRepresentationNamesItsBaseExactlyWhenItCarriesADelta is the contract the
// client's keyframe recovery depends on: a base reference with no delta is not a
// keyframe, it is a bug, and a base whose digest disagrees with the tree the base
// route serves would send the client to apply a delta to the wrong tree.
func TestTheRepresentationNamesItsBaseExactlyWhenItCarriesADelta(t *testing.T) {
	f := newDurableFixture(t, 4)
	f.waitReady(t)
	sawDelta, sawKeyframe := false, false
	for sequence := uint64(1); sequence <= 4; sequence++ {
		resp, body := f.get(t, "/debug/pabt/v2/representations/"+strconv.FormatUint(sequence, 10))
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("representation %d = %d: %s", sequence, resp.StatusCode, body)
		}
		var out RepresentationResponse
		if err := CanonicalDecode(body, &out); err != nil {
			t.Fatalf("decode the representation: %v", err)
		}
		if (out.Delta == nil) != (out.Base == nil) {
			t.Fatalf("tick %d has delta=%v base=%v: a base with no delta is not a keyframe",
				sequence, out.Delta != nil, out.Base != nil)
		}
		if out.Delta == nil {
			sawKeyframe = true
			continue
		}
		sawDelta = true
		if out.Base.Sequence != out.Delta.BaseSequence ||
			out.Base.Digest != out.Delta.BaseDigest ||
			out.Base.EventUID != out.Delta.BaseEventUID {
			t.Errorf("tick %d names a base that disagrees with its delta: %+v against %+v",
				sequence, out.Base, out.Delta)
		}
		// The base the delta names must be the tree the base route actually
		// serves, or the client reconstructs from something else.
		base, err := f.store.TreeAt(context.Background(), f.key, out.Delta.BaseSequence)
		if err != nil {
			t.Fatalf("the base of tick %d is not readable: %v", sequence, err)
		}
		if base.Digest != out.Base.Digest {
			t.Errorf("tick %d names base digest %s, the store serves %s",
				sequence, out.Base.Digest, base.Digest)
		}
		if base.EventUID != out.Base.EventUID {
			t.Errorf("tick %d names base event %s, the store serves %s",
				sequence, out.Base.EventUID, base.EventUID)
		}
	}
	if !sawDelta {
		t.Error("no representation carried a delta, so the delta path was not exercised")
	}
	if !sawKeyframe {
		t.Error("no representation was a keyframe, so the first record was not exercised")
	}
}
