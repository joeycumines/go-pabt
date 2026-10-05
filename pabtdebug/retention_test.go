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
