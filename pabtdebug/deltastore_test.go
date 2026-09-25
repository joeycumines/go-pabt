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
)

// tickWithDelta commits one record for a tree, using a delta from base when the
// store says one is eligible.
func tickWithDelta(t *testing.T, store *SQLiteStore, key RunKey, tree *StoredTree, forceKeyframe bool) (uint64, error) {
	t.Helper()
	ctx := context.Background()
	sequence := uint64(1)
	if head, err := store.Head(ctx, key); err == nil {
		sequence = head.HeadSequence + 1
	}
	ticket, err := store.Begin(ctx, BeginRequest{
		RunKey: key, ExpectedPlanRevision: testPlanRevision,
	})
	if err != nil {
		return 0, err
	}
	record := makeRecord(tree)
	var delta *ExactTreeDelta
	if !forceKeyframe && tree != nil {
		// The tree carries the run it belongs to, which the store stamps on commit;
		// the delta is computed against a base that already carries one.
		tree.RunKey = key
		base, found, err := store.DeltaBase(ctx, key, sequence)
		if err != nil {
			return 0, err
		}
		if found {
			delta, err = ComputeExactDelta(base, *tree)
			if err != nil {
				return 0, err
			}
		}
	}
	receipt, err := store.CommitWithDelta(ctx, ticket, record, delta)
	if err != nil {
		return 0, err
	}
	return receipt.Sequence, nil
}

func TestADeltaIsStoredBesideTheTreeItDescribes(t *testing.T) {
	store := openTestStore(t, testStorePath(t))
	identity := bootstrapTestStore(t, store)
	key := RunKey{SessionID: identity.SessionID, PlanID: identity.PlanID, RunID: identity.RunID}

	first := smallTree("n")
	if _, err := tickWithDelta(t, store, key, first, false); err != nil {
		t.Fatalf("the first tick has no base and must be a keyframe: %v", err)
	}
	// The first tree cannot have a base, so it is a keyframe.
	_, base, delta, err := store.Representation(context.Background(), key, 1)
	if err != nil {
		t.Fatalf("Representation: %v", err)
	}
	if base != nil || delta != nil {
		t.Errorf("the first representation names base %v and carries a delta: %v", base, delta != nil)
	}

	second := smallTree("n")
	second.Nodes[0].Name = "renamed"
	sealedSecond := reseal(t, *second)
	sequence, err := tickWithDelta(t, store, key, &sealedSecond, false)
	if err != nil {
		t.Fatalf("the second tick: %v", err)
	}
	tree, base, delta, err := store.Representation(context.Background(), key, sequence)
	if err != nil {
		t.Fatalf("Representation: %v", err)
	}
	if delta == nil {
		t.Fatal("the second representation carries no delta")
	}
	if base == nil || *base != tree.EventUID && *base == "" {
		t.Errorf("the delta names base %v", base)
	}
	// The delta reconstructs the stored tree, which is what makes the pair useful.
	rebuilt, err := ApplyExactDelta(*mustTree(t, store, key, 1), *delta)
	if err != nil {
		t.Fatalf("ApplyExactDelta: %v", err)
	}
	if rebuilt.Digest != tree.Digest {
		t.Errorf("the stored delta reconstructs to %q, the stored tree is %q", rebuilt.Digest, tree.Digest)
	}
	// And the keyframe is still there, so a reader is never dependent on the delta.
	stored, err := store.TreeAt(context.Background(), key, sequence)
	if err != nil {
		t.Fatalf("TreeAt: %v", err)
	}
	if stored.Digest != tree.Digest {
		t.Errorf("the keyframe and the reconstructed tree disagree: %q and %q", stored.Digest, tree.Digest)
	}
}

func mustTree(t *testing.T, store *SQLiteStore, key RunKey, sequence uint64) *StoredTree {
	t.Helper()
	tree, err := store.TreeAt(context.Background(), key, sequence)
	if err != nil {
		t.Fatalf("TreeAt %d: %v", sequence, err)
	}
	return &tree
}

func TestAKeyframeIsForcedWhenNoBaseIsEligible(t *testing.T) {
	store := openTestStore(t, testStorePath(t))
	identity := bootstrapTestStore(t, store)
	key := RunKey{SessionID: identity.SessionID, PlanID: identity.PlanID, RunID: identity.RunID}
	ctx := context.Background()

	// A run whose records carry no tree cannot be deltified across, so the next
	// tree has no eligible base even though an earlier one exists.
	if _, err := tickWithDelta(t, store, key, smallTree("n"), false); err != nil {
		t.Fatalf("first tick: %v", err)
	}
	if _, err := tickWithDelta(t, store, key, nil, false); err != nil {
		t.Fatalf("a tick with no tree: %v", err)
	}
	third := smallTree("n")
	third.Nodes[0].Name = "after-a-gap"
	sealedThird := reseal(t, *third)
	sequence, err := tickWithDelta(t, store, key, &sealedThird, false)
	if err != nil {
		t.Fatalf("third tick: %v", err)
	}
	_, _, delta, err := store.Representation(ctx, key, sequence)
	if err != nil {
		t.Fatalf("Representation: %v", err)
	}
	if delta != nil {
		t.Error("a delta was stored across a record that carries no tree")
	}
	// The record before the gap is a valid base when nothing blocks it.
	if _, found, err := store.DeltaBase(ctx, key, 1); err != nil {
		t.Fatalf("DeltaBase: %v", err)
	} else if found {
		t.Error("the first sequence reports a base before it")
	}
}

func TestADeltaIsRefusedWhenItDoesNotDescribeTheCommittedTree(t *testing.T) {
	store := openTestStore(t, testStorePath(t))
	identity := bootstrapTestStore(t, store)
	key := RunKey{SessionID: identity.SessionID, PlanID: identity.PlanID, RunID: identity.RunID}
	ctx := context.Background()

	if _, err := tickWithDelta(t, store, key, smallTree("n"), false); err != nil {
		t.Fatalf("first tick: %v", err)
	}
	target := smallTree("n")
	target.Nodes[0].Name = "target"
	sealedTarget := reseal(t, *target)
	base := mustTree(t, store, key, 1)
	sealedTarget.RunKey = key
	delta, err := ComputeExactDelta(*base, sealedTarget)
	if err != nil {
		t.Fatalf("ComputeExactDelta: %v", err)
	}
	ticket, err := store.Begin(ctx, BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := store.CommitWithDelta(ctx, ticket, makeRecord(&sealedTarget), delta); err != nil {
		t.Fatalf("a valid delta was refused: %v", err)
	}

	// The store corrects a delta's target identity and digest, because it is the
	// authority on what it committed. What it will not accept is a set of
	// operations that does not actually produce the tree it stored.
	tampered := *delta
	tampered.Operations = append([]DeltaOp(nil), delta.Operations...)
	tampered.Operations[0].Replace.Node.Name = "a name the target does not have"
	ticket, err = store.Begin(ctx, BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := store.CommitWithDelta(ctx, ticket, makeRecord(&sealedTarget), &tampered); err == nil {
		t.Error("a delta whose operations do not produce the committed tree was stored")
	} else if !errors.Is(err, ErrStoreCorrupt) {
		t.Errorf("CommitWithDelta = %v, want a corruption error", err)
	}

	// A refused commit leaves the attempt's lease held, which is what lets
	// recovery tell a half-written attempt from a finished one. The caller
	// resolves it before the run moves on.
	if err := store.Abort(ctx, ticket); err != nil {
		t.Fatalf("Abort after the refused commit: %v", err)
	}

	// A delta that drops an operation is the same class of claim: the result is
	// not the tree, so it is refused rather than stored beside it.
	incomplete := *delta
	incomplete.Operations = nil
	ticket, err = store.Begin(ctx, BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := store.CommitWithDelta(ctx, ticket, makeRecord(&sealedTarget), &incomplete); err == nil {
		t.Error("a delta that does not fully describe the change was stored")
	}
	if err := store.Abort(ctx, ticket); err != nil {
		t.Fatalf("Abort: %v", err)
	}

	// The head did not move for any refused attempt, so nothing is half written.
	head, err := store.Head(ctx, key)
	if err != nil {
		t.Fatalf("Head: %v", err)
	}
	if head.HeadSequence != 2 {
		t.Errorf("the head is at %d after refused commits, want the 2 committed records", head.HeadSequence)
	}
	if verified, err := store.VerifyChain(ctx, key); err != nil {
		t.Errorf("VerifyChain: %v", err)
	} else if verified != 2 {
		t.Errorf("the chain verified %d records, want 2", verified)
	}
}

func TestADeltaNamingAnUnrecordedBaseIsRefused(t *testing.T) {
	store := openTestStore(t, testStorePath(t))
	identity := bootstrapTestStore(t, store)
	key := RunKey{SessionID: identity.SessionID, PlanID: identity.PlanID, RunID: identity.RunID}
	ctx := context.Background()

	target := smallTree("n")
	ticket, err := store.Begin(ctx, BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	// A delta whose base was never committed describes a continuity the run does
	// not have, so it is refused rather than stored.
	phantom := ExactTreeDelta{
		Format: ExactDeltaFormatName, Version: ExactDeltaFormatVersion, RunKey: key,
		BaseEventUID: "never-committed", BaseSequence: 1,
		TargetEventUID: ticket.EventUID, TargetSequence: ticket.Sequence,
	}
	digest, err := HashDelta(phantom)
	if err != nil {
		t.Fatalf("HashDelta: %v", err)
	}
	phantom.SHA256 = digest
	if _, err := store.CommitWithDelta(ctx, ticket, makeRecord(target), &phantom); err == nil {
		t.Error("a delta naming an unrecorded base was stored")
	} else if !errors.Is(err, ErrHeadMismatch) && !errors.Is(err, ErrNotFound) {
		t.Errorf("CommitWithDelta = %v, want a mismatch or not-found", err)
	}
}
