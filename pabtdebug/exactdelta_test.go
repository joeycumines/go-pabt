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
	"strings"
	"testing"
)

// deltaFixtureNode builds a sealed node.
func deltaFixtureNode(id string, parent *string, ordinal uint32, name string) StoredNode {
	node := StoredNode{
		ID:        id,
		ParentID:  parent,
		Ordinal:   ordinal,
		Type:      "node",
		Name:      name,
		Status:    StoredNodeStatus{TickCount: 1, LastStatus: 2, StatusPresent: true, Version: 1},
		EffectIDs: []string{},
	}
	digest, err := HashNode(node)
	if err != nil {
		panic(err)
	}
	node.ContentHash = digest
	return node
}

// sealDeltaTree seals a fixture tree.
func sealDeltaTree(t *testing.T, runKey RunKey, eventUID string, seq uint64, revision uint64, nodes ...StoredNode) StoredTree {
	t.Helper()
	tree := StoredTree{
		RunKey:       runKey,
		EventUID:     eventUID,
		Sequence:     seq,
		PlanRevision: revision,
		RootID:       nodes[0].ID,
		Nodes:        nodes,
		NodeCount:    uint32(len(nodes)),
	}
	digest, err := HashTree(tree)
	if err != nil {
		t.Fatalf("seal tree: %v", err)
	}
	tree.Digest = digest
	return tree
}

func deltaRunKey() RunKey {
	return RunKey{SessionID: "s", PlanID: "p", RunID: "r"}
}

// threeNodeTree is root, first, second.
func threeNodeTree(t *testing.T, seq, revision uint64) StoredTree {
	t.Helper()
	first, second := "n1", "n2"
	return sealDeltaTree(t, deltaRunKey(), "e", seq, revision,
		deltaFixtureNode("root", nil, 0, "root"),
		deltaFixtureNode(first, id("root"), 0, "first"),
		deltaFixtureNode(second, id("root"), 1, "second"),
	)
}

// id returns a stable pointer to a literal, which is how an optional
// reference is expressed in a fixture.
func id(name string) *string { return &name }

// cloneTree copies a tree's node slice, so a case that changes the tree cannot
// change the base every later case is measured against.
func cloneTree(tree StoredTree) StoredTree {
	out := tree
	out.Nodes = append([]StoredNode(nil), tree.Nodes...)
	return out
}

func TestExactDeltaRoundTripsAcrossEveryKindOfChange(t *testing.T) {
	base := threeNodeTree(t, 1, 1)
	// Every case produces a target one sequence after the base, because a delta
	// must move forward.
	target := func(tree StoredTree) StoredTree {
		out := cloneTree(tree)
		out.Sequence = tree.Sequence + 1
		out.EventUID = "target"
		return reseal(t, out)
	}

	for _, test := range []struct {
		name   string
		mutate func(StoredTree) StoredTree
	}{
		{
			name:   "identical",
			mutate: func(tree StoredTree) StoredTree { return target(tree) },
		},
		{
			name: "a reordered child",
			mutate: func(tree StoredTree) StoredTree {
				tree = target(tree)
				tree.Nodes[1], tree.Nodes[2] = tree.Nodes[2], tree.Nodes[1]
				return reseal(t, tree)
			},
		},
		{
			name: "a replaced name",
			mutate: func(tree StoredTree) StoredTree {
				tree = target(tree)
				tree.Nodes[1].Name = "renamed"
				return reseal(t, tree)
			},
		},
		{
			name: "a removed subtree",
			mutate: func(tree StoredTree) StoredTree {
				tree = target(tree)
				tree.Nodes = tree.Nodes[:2]
				tree.NodeCount = 2
				return reseal(t, tree)
			},
		},
		{
			name: "an inserted subtree",
			mutate: func(tree StoredTree) StoredTree {
				tree = target(tree)
				parent := "root"
				tree.Nodes = append(tree.Nodes,
					deltaFixtureNode("n3", &parent, 2, "third"),
					deltaFixtureNode("n4", &parent, 3, "fourth"),
				)
				tree.NodeCount = uint32(len(tree.Nodes))
				return reseal(t, tree)
			},
		},
		{
			name: "a removed and an inserted node together",
			mutate: func(tree StoredTree) StoredTree {
				tree = target(tree)
				parent := "root"
				tree.Nodes = []StoredNode{
					tree.Nodes[0],
					deltaFixtureNode("n9", &parent, 0, "fresh"),
				}
				tree.NodeCount = 2
				return reseal(t, tree)
			},
		},
		{
			name: "a moved and renamed node at once",
			mutate: func(tree StoredTree) StoredTree {
				tree = target(tree)
				tree.Nodes[1].Name = "moved-and-renamed"
				tree.Nodes[1], tree.Nodes[2] = tree.Nodes[2], tree.Nodes[1]
				return reseal(t, tree)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			target := test.mutate(base)
			delta, err := ComputeExactDelta(base, target)
			if err != nil {
				t.Fatalf("ComputeExactDelta: %v", err)
			}
			got, err := ApplyExactDelta(base, *delta)
			if err != nil {
				t.Fatalf("ApplyExactDelta: %v", err)
			}
			if got.Digest != target.Digest {
				t.Errorf("the delta reconstructs to %q, the target is %q", got.Digest, target.Digest)
			}
			if len(got.Nodes) != len(target.Nodes) {
				t.Fatalf("the delta has %d nodes, the target has %d", len(got.Nodes), len(target.Nodes))
			}
			for i := range got.Nodes {
				if got.Nodes[i].ID != target.Nodes[i].ID {
					t.Fatalf("node %d is %q, the target has %q", i, got.Nodes[i].ID, target.Nodes[i].ID)
				}
				if got.Nodes[i].ContentHash != target.Nodes[i].ContentHash {
					t.Errorf("node %s reconstructs with content hash %q, the target has %q",
						got.Nodes[i].ID, got.Nodes[i].ContentHash, target.Nodes[i].ContentHash)
				}
			}
		})
	}
}

// reseal re-derives each node's ordinal from the order it appears in, then seals
// the node and the tree.
//
// A node's ordinal is its position among its siblings, and the canonical order is
// preorder, so the slice order is what says where a node sits. A fixture that
// swapped two nodes and left their ordinals alone would describe two different
// trees at once.
func reseal(t *testing.T, tree StoredTree) StoredTree {
	t.Helper()
	within := map[string]int{}
	for i := range tree.Nodes {
		parent := ""
		if tree.Nodes[i].ParentID != nil {
			parent = *tree.Nodes[i].ParentID
		}
		tree.Nodes[i].Ordinal = uint32(within[parent])
		within[parent]++
	}
	for i := range tree.Nodes {
		node := tree.Nodes[i]
		digest, err := HashNode(node)
		if err != nil {
			t.Fatalf("seal node: %v", err)
		}
		tree.Nodes[i].ContentHash = digest
	}
	tree.NodeCount = uint32(len(tree.Nodes))
	digest, err := HashTree(tree)
	if err != nil {
		t.Fatalf("seal tree: %v", err)
	}
	tree.Digest = digest
	return tree
}

func TestExactDeltaIsDeterministic(t *testing.T) {
	// Two runs that reach the same tree must produce the same bytes, or a
	// re-exported history would not match the one that was imported.
	base := threeNodeTree(t, 1, 1)
	target := sealDeltaTree(t, deltaRunKey(), "e2", 2, 2,
		deltaFixtureNode("root", nil, 0, "root"),
		deltaFixtureNode("b", id("root"), 0, "b"),
		deltaFixtureNode("a", id("root"), 1, "a"),
		deltaFixtureNode("c", id("a"), 0, "c"),
	)
	first, err := ComputeExactDelta(base, target)
	if err != nil {
		t.Fatalf("ComputeExactDelta: %v", err)
	}
	firstBytes, err := CanonicalEncode(*first)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	for range 25 {
		again, err := ComputeExactDelta(base, target)
		if err != nil {
			t.Fatalf("ComputeExactDelta: %v", err)
		}
		againBytes, err := CanonicalEncode(*again)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		if string(againBytes) != string(firstBytes) {
			t.Fatal("the same change produced different delta bytes")
		}
	}
	// The phases are in a fixed order, so a reader can apply a delta without
	// consulting the generator.
	var phases []DeltaOpKind
	for _, op := range first.Operations {
		phases = append(phases, op.Kind)
	}
	if !sortedPhases(phases) {
		t.Errorf("operations are not in phase order: %v", phases)
	}
}

func sortedPhases(kinds []DeltaOpKind) bool {
	phase := map[DeltaOpKind]int{
		DeltaRemove: 0, DeltaInsert: 1, DeltaReorder: 2, DeltaReplace: 3,
	}
	for i := 1; i < len(kinds); i++ {
		if phase[kinds[i]] < phase[kinds[i-1]] {
			return false
		}
	}
	return true
}

func TestARejectedDeltaDoesNotLeakPastItsBase(t *testing.T) {
	// Every identity in the delta is checked in both directions, so a delta
	// aimed at a different base, or claiming a different target, is refused.
	base := threeNodeTree(t, 1, 1)
	target := threeNodeTree(t, 2, 1)
	target.Nodes[1].Name = "changed"
	target = reseal(t, target)
	good, err := ComputeExactDelta(base, target)
	if err != nil {
		t.Fatalf("ComputeExactDelta: %v", err)
	}
	if _, err := ApplyExactDelta(base, *good); err != nil {
		t.Fatalf("a valid delta was refused: %v", err)
	}
	other := threeNodeTree(t, 9, 9)

	for _, test := range []struct {
		name   string
		damage func(d *ExactTreeDelta)
		want   error
	}{
		{
			name:   "a changed digest",
			damage: func(d *ExactTreeDelta) { d.SHA256 = strings.Repeat("f", 64) },
			want:   ErrStoreCorrupt,
		},
		{
			name:   "a wrong base event",
			damage: func(d *ExactTreeDelta) { d.BaseEventUID = "other" },
			want:   ErrHeadMismatch,
		},
		{
			name:   "a wrong base sequence",
			damage: func(d *ExactTreeDelta) { d.BaseSequence = 7 },
			want:   ErrHeadMismatch,
		},
		{
			name:   "a wrong base plan revision",
			damage: func(d *ExactTreeDelta) { d.BasePlanRevision = 7 },
			want:   ErrHeadMismatch,
		},
		{
			name:   "a wrong base digest",
			damage: func(d *ExactTreeDelta) { d.BaseDigest = strings.Repeat("e", 64) },
			want:   ErrHeadMismatch,
		},
		{
			name: "another run",
			damage: func(d *ExactTreeDelta) {
				d.RunKey = RunKey{SessionID: "s", PlanID: "p", RunID: "other"}
			},
			want: ErrHeadMismatch,
		},
		{
			name:   "a re-indexed operation",
			damage: func(d *ExactTreeDelta) { d.Operations[0].Index = 99 },
			want:   ErrStoreCorrupt,
		},
		{
			name: "two payloads on one operation",
			damage: func(d *ExactTreeDelta) {
				d.Operations[0].Insert = &DeltaInsertOp{RootID: "x", Nodes: []StoredNode{
					deltaFixtureNode("x", nil, 0, "x"),
				}}
			},
			want: ErrValueUnencodable,
		},
		{
			// Taking a replace operation's own payload away leaves it carrying
			// none, which the payload count refuses before the kind is even read.
			name:   "no payload on one operation",
			damage: func(d *ExactTreeDelta) { d.Operations[0].Replace = nil },
			want:   ErrValueUnencodable,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			damaged := *good
			damaged.Operations = append([]DeltaOp(nil), good.Operations...)
			test.damage(&damaged)
			if test.want != ErrStoreCorrupt {
				// A damaged delta is caught by its own digest before any identity
				// is read, so the digest is re-stamped to reach the check this
				// case is about.
				digest, err := HashDelta(damaged)
				if err != nil {
					t.Fatalf("re-stamp: %v", err)
				}
				damaged.SHA256 = digest
			}
			if _, err := ApplyExactDelta(base, damaged); !errors.Is(err, test.want) {
				t.Errorf("ApplyExactDelta = %v, want %v", err, test.want)
			}
		})
	}
	// A base that is not the one the delta names is refused, including a base
	// from a different run.
	if _, err := ApplyExactDelta(other, *good); err == nil {
		t.Error("a delta was applied to a base it does not name")
	}
}

func TestAKeyframeIsADeltaFromNothing(t *testing.T) {
	// A keyframe carries the whole tree and no operations, so a reader needs no
	// base to reconstruct it. That is what makes a keyframe a safe fallback when
	// no base is eligible.
	tree := threeNodeTree(t, 1, 1)
	empty, err := ComputeExactDelta(StoredTree{RunKey: deltaRunKey()}, tree)
	if err == nil {
		// A missing base is not a tree, so this must be refused rather than
		// silently producing a keyframe.
		t.Fatalf("a delta from a missing base was accepted: %+v", empty)
	}
	// A keyframe is simply a representation with no delta, and reading it needs no
	// base at all. TreeAt is that path.
	store := openTestStore(t, testStorePath(t))
	identity := bootstrapTestStore(t, store)
	key := RunKey{SessionID: identity.SessionID, PlanID: identity.PlanID, RunID: identity.RunID}
	ticket, err := store.Begin(context.Background(),
		BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	stored := smallTree("n")
	stored.EventUID = "placeholder"
	if _, err := store.Commit(context.Background(), ticket, makeRecord(stored)); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	got, err := store.TreeAt(context.Background(), key, 1)
	if err != nil {
		t.Fatalf("TreeAt: %v", err)
	}
	if got.Digest != stored.Digest {
		t.Errorf("the keyframe read back as %q, stored as %q", got.Digest, stored.Digest)
	}
}

// TestAParentThatLosesItsLastChildRoundTrips covers the case the generator and the
// applier disagreed about.
//
// A parent that keeps no children has no entry in the target's child map, so
// walking the target alone emitted no child order for it and the delta said only
// "remove the child". The applier kept the base's list, which still named the
// removed node, and refused the delta the generator had just produced. In the
// product that is not a rejected round trip but a failed tick: the store refuses
// to commit the delta, so a tick that actually ran is recorded as a storage
// error. The reviewer who found this could not reach it from a live run, which is
// why it is pinned here rather than left to a plan that happens to produce it.
func TestAParentThatLosesItsLastChildRoundTrips(t *testing.T) {
	root, parent, child := "root", "parent", "child-0"
	base := sealDeltaTree(t, deltaRunKey(), "e", 1, 2,
		deltaFixtureNode(root, nil, 0, root),
		deltaFixtureNode(parent, id(root), 0, parent),
		deltaFixtureNode(child, id(parent), 0, child),
	)
	target := sealDeltaTree(t, deltaRunKey(), "e", 2, 2,
		deltaFixtureNode(root, nil, 0, root),
		deltaFixtureNode(parent, id(root), 0, parent),
	)
	delta, err := ComputeExactDelta(base, target)
	if err != nil {
		t.Fatalf("ComputeExactDelta: %v", err)
	}
	var removes, reorders int
	for _, op := range delta.Operations {
		switch op.Kind {
		case DeltaRemove:
			removes++
		case DeltaReorder:
			reorders++
			if len(op.Reorder.ChildOrder) != 0 {
				t.Errorf("the reorder for the emptied parent lists %v, want none",
					op.Reorder.ChildOrder)
			}
		}
	}
	if removes != 1 {
		t.Errorf("the delta removes %d nodes, want 1", removes)
	}
	if reorders == 0 {
		t.Error("the delta says nothing about the parent that lost its child, so it is not exact")
	}
	if err := ValidateExactDelta(base, *delta); err != nil {
		t.Fatalf("the applier refuses the generator's own delta: %v", err)
	}
	applied, err := ApplyExactDelta(base, *delta)
	if err != nil {
		t.Fatalf("ApplyExactDelta: %v", err)
	}
	if applied.Digest != target.Digest {
		t.Errorf("the applied tree is %s, the target is %s", applied.Digest, target.Digest)
	}
	for _, node := range applied.Nodes {
		if node.ID == "child-0" {
			t.Error("the removed node is still in the applied tree")
		}
	}
}

// TestTheRemovalPhaseUnlinksFromTheParent covers the applier half on its own, with
// a delta that says only "remove". The applier must not depend on the generator
// having also said what the parent was left holding, because a delta is applied by
// things other than this package's own generator.
func TestTheRemovalPhaseUnlinksFromTheParent(t *testing.T) {
	root, parent, first, second := "root", "parent", "child-1", "child-2"
	base := sealDeltaTree(t, deltaRunKey(), "e", 1, 2,
		deltaFixtureNode(root, nil, 0, root),
		deltaFixtureNode(parent, id(root), 0, parent),
		deltaFixtureNode(first, id(parent), 0, first),
		deltaFixtureNode(second, id(parent), 1, second),
	)
	target := sealDeltaTree(t, deltaRunKey(), "e", 2, 2,
		deltaFixtureNode(root, nil, 0, root),
		deltaFixtureNode(parent, id(root), 0, parent),
		deltaFixtureNode(first, id(parent), 0, first),
	)
	delta, err := ComputeExactDelta(base, target)
	if err != nil {
		t.Fatalf("ComputeExactDelta: %v", err)
	}
	// Drop the reorder the generator emits, so only the removal is left to apply.
	var kept []DeltaOp
	for _, op := range delta.Operations {
		if op.Kind == DeltaReorder {
			continue
		}
		kept = append(kept, op)
	}
	delta.Operations = kept
	// The delta's own digest covers its operations, so removing one and applying
	// the result means re-sealing it. That is the point: the applier is being
	// handed a delta that is internally consistent and simply does not mention the
	// parent's new child order.
	digest, err := HashDelta(*delta)
	if err != nil {
		t.Fatalf("HashDelta: %v", err)
	}
	delta.SHA256 = digest
	if _, err := ApplyExactDelta(base, *delta); err != nil {
		t.Fatalf("the applier could not remove a child on a remove alone: %v", err)
	}
}
