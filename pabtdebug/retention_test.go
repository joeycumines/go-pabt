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

func openRetentionStore(t *testing.T) (*SQLiteStore, RunKey) {
	t.Helper()
	path := testStorePath(t)
	store := openTestStore(t, path)
	identity := bootstrapTestStore(t, store)
	return store, RunKey{
		SessionID: identity.SessionID,
		PlanID:    identity.PlanID,
		RunID:     identity.RunID,
	}
}

func commitRetentionRecord(t *testing.T, store *SQLiteStore, key RunKey) {
	t.Helper()
	ctx := context.Background()
	ticket, err := store.Begin(ctx, BeginRequest{RunKey: key})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	record := makeRecord(nil)
	if _, err := store.Commit(ctx, ticket, record); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

func TestEvictRemovesOldestRecords(t *testing.T) {
	store, key := openRetentionStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		commitRetentionRecord(t, store, key)
	}

	result, err := store.Evict(ctx, key, RetentionPolicy{MaxRecords: 3})
	if err != nil {
		t.Fatalf("Evict: %v", err)
	}
	if result.RemovedCount != 2 {
		t.Errorf("RemovedCount = %d, want 2", result.RemovedCount)
	}
	if result.RetainedFrom != 3 {
		t.Errorf("RetainedFrom = %d, want 3", result.RetainedFrom)
	}

	// Sequences 1 and 2 should be evicted.
	_, err = store.RecordAt(ctx, key, 1)
	if !errors.Is(err, ErrEvicted) {
		t.Errorf("RecordAt(1) = %v, want ErrEvicted", err)
	}
	_, err = store.RecordAt(ctx, key, 2)
	if !errors.Is(err, ErrEvicted) {
		t.Errorf("RecordAt(2) = %v, want ErrEvicted", err)
	}

	// Sequence 3 should still be readable.
	_, err = store.RecordAt(ctx, key, 3)
	if err != nil {
		t.Errorf("RecordAt(3) = %v, want nil", err)
	}
}

func TestEvictZeroPolicyIsNoop(t *testing.T) {
	store, key := openRetentionStore(t)
	ctx := context.Background()

	commitRetentionRecord(t, store, key)
	commitRetentionRecord(t, store, key)

	result, err := store.Evict(ctx, key, RetentionPolicy{MaxRecords: 0})
	if err != nil {
		t.Fatalf("Evict: %v", err)
	}
	if result.RemovedCount != 0 {
		t.Errorf("RemovedCount = %d, want 0", result.RemovedCount)
	}

	_, err = store.RecordAt(ctx, key, 1)
	if err != nil {
		t.Errorf("RecordAt(1) after no-op evict: %v", err)
	}
}

func TestEvictBeyondHeadIsNoop(t *testing.T) {
	store, key := openRetentionStore(t)
	ctx := context.Background()

	commitRetentionRecord(t, store, key)

	result, err := store.Evict(ctx, key, RetentionPolicy{MaxRecords: 100})
	if err != nil {
		t.Fatalf("Evict: %v", err)
	}
	if result.RemovedCount != 0 {
		t.Errorf("RemovedCount = %d, want 0", result.RemovedCount)
	}
}

func TestRetainedFromReportsBoundary(t *testing.T) {
	store, key := openRetentionStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		commitRetentionRecord(t, store, key)
	}

	_, err := store.Evict(ctx, key, RetentionPolicy{MaxRecords: 2})
	if err != nil {
		t.Fatalf("Evict: %v", err)
	}

	from, err := store.RetainedFrom(ctx, key)
	if err != nil {
		t.Fatalf("RetainedFrom: %v", err)
	}
	if from != 4 {
		t.Errorf("RetainedFrom = %d, want 4", from)
	}
}

func TestEvictedRecordReturnsErrEvictedNotErrNotFound(t *testing.T) {
	store, key := openRetentionStore(t)
	ctx := context.Background()

	commitRetentionRecord(t, store, key)
	commitRetentionRecord(t, store, key)

	_, err := store.Evict(ctx, key, RetentionPolicy{MaxRecords: 1})
	if err != nil {
		t.Fatalf("Evict: %v", err)
	}

	_, err = store.RecordAt(ctx, key, 1)
	if errors.Is(err, ErrNotFound) {
		t.Error("RecordAt(1) returned ErrNotFound for an evicted record; should be ErrEvicted")
	}
	if !errors.Is(err, ErrEvicted) {
		t.Errorf("RecordAt(1) = %v, want ErrEvicted", err)
	}
}

// commitTreeRecord commits a record with a tree, creating a representation row.
// The root node's ID is always "root" (from smallTree); rootName sets only the
// Name field, matching bundleFixture's pattern so deltas between records are
// well-formed (same node IDs, different names).
func commitTreeRecord(t *testing.T, store *SQLiteStore, key RunKey, rootName string) {
	t.Helper()
	ctx := context.Background()
	ticket, err := store.Begin(ctx, BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	tree := smallTree("root")
	tree.RunKey = key
	tree.Nodes[0].Name = rootName
	sealed := reseal(t, *tree)
	record := makeRecord(&sealed)
	if _, err := store.Commit(ctx, ticket, record); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

// commitDeltaRecord commits a record whose representation is stored as a delta
// against a base sequence. It follows the same pattern as bundleFixture:
// read the base via DeltaBase, create a modified target tree (same node IDs,
// different Name), compute the exact delta, and commit with CommitWithDelta.
func commitDeltaRecord(t *testing.T, store *SQLiteStore, key RunKey, rootName string) {
	t.Helper()
	ctx := context.Background()

	ticket, err := store.Begin(ctx, BeginRequest{RunKey: key, ExpectedPlanRevision: testPlanRevision})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	// Build the target tree: same root ID as the base, different Name.
	target := smallTree("root")
	target.RunKey = key
	target.Nodes[0].Name = rootName
	sealed := reseal(t, *target)

	record := makeRecord(&sealed)

	// Try to compute a delta against the eligible base.
	var delta *ExactTreeDelta
	if base, found, baseErr := store.DeltaBase(ctx, key, ticket.Sequence); baseErr == nil && found {
		if computed, compErr := ComputeExactDelta(base, sealed); compErr == nil {
			delta = computed
		}
	}

	if _, err := store.CommitWithDelta(ctx, ticket, record, delta); err != nil {
		t.Fatalf("CommitWithDelta: %v", err)
	}
}

func TestEvictPromotesDeltaToKeyframe(t *testing.T) {
	store, key := openRetentionStore(t)
	ctx := context.Background()

	// Seq 1: keyframe (base).
	commitTreeRecord(t, store, key, "base-root")

	// Seq 2: delta based on seq 1.
	commitDeltaRecord(t, store, key, "delta-root")

	// Verify seq 2 has a delta before eviction.
	tree2Before, base2Before, delta2Before, err := store.Representation(ctx, key, 2)
	if err != nil {
		t.Fatalf("Representation(2) before evict: %v", err)
	}
	if delta2Before == nil {
		t.Fatal("precondition: seq 2 should have a delta before eviction")
	}
	_ = tree2Before
	_ = base2Before

	// Evict seq 1 (keep only 1 record = seq 2).
	result, err := store.Evict(ctx, key, RetentionPolicy{MaxRecords: 1})
	if err != nil {
		t.Fatalf("Evict: %v", err)
	}
	if result.PromotedCount != 1 {
		t.Errorf("PromotedCount = %d, want 1", result.PromotedCount)
	}
	if result.RemovedCount != 1 {
		t.Errorf("RemovedCount = %d, want 1", result.RemovedCount)
	}

	// Seq 1 should be evicted.
	_, err = store.RecordAt(ctx, key, 1)
	if !errors.Is(err, ErrEvicted) {
		t.Errorf("RecordAt(1) = %v, want ErrEvicted", err)
	}

	// Seq 2's representation should now be a keyframe (no delta, no base).
	tree2, base2, delta2, err := store.Representation(ctx, key, 2)
	if err != nil {
		t.Fatalf("Representation(2) after evict: %v", err)
	}
	if delta2 != nil {
		t.Error("seq 2 still has a delta after promotion; should be a keyframe")
	}
	if base2 != nil {
		t.Error("seq 2 still has a base reference after promotion; should be nil")
	}

	// The promoted tree must be readable. RootID is "root" because smallTree
	// uses the same node ID for delta compatibility; the Name field carries
	// the differentiation.
	if tree2.RootID != "root" {
		t.Errorf("promoted tree RootID = %q, want %q", tree2.RootID, "root")
	}
	if len(tree2.Nodes) == 0 || tree2.Nodes[0].Name != "delta-root" {
		t.Errorf("promoted tree root node Name = %q, want %q", tree2.Nodes[0].Name, "delta-root")
	}

	// TreeAt must succeed on the promoted representation — this is the
	// critical assertion proving the tree was reconstructed, not left empty.
	readTree, err := store.TreeAt(ctx, key, 2)
	if err != nil {
		t.Fatalf("TreeAt(2) after promotion: %v (tree was not reconstructed)", err)
	}
	if readTree.RootID != "root" {
		t.Errorf("TreeAt(2).RootID = %q, want %q", readTree.RootID, "root")
	}
	if len(readTree.Nodes) == 0 || readTree.Nodes[0].Name != "delta-root" {
		t.Errorf("TreeAt(2) root node Name = %q, want %q", readTree.Nodes[0].Name, "delta-root")
	}
}
