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
	"database/sql"
	"errors"
	"fmt"
)

// ErrEvicted is returned when a record or tree has been removed by the
// retention policy. It is distinct from ErrNotFound: a not-found sequence
// was never committed, while an evicted one was committed and then removed.
// Readers that see ErrEvicted should report the data as explicitly unavailable
// rather than as missing or corrupt.
var ErrEvicted = errors.New("pabtdebug: record evicted by retention policy")

// RetentionPolicy defines how many records a run retains. When MaxRecords is
// zero, no eviction occurs and the run keeps its full history. When positive,
// Evict removes records below the retention boundary, promoting any delta
// whose base was evicted into a keyframe so the remaining chain stays
// reconstructable.
type RetentionPolicy struct {
	// MaxRecords is the maximum number of committed records a run retains.
	// Zero means unlimited.
	MaxRecords uint64
}

// EvictResult reports what an eviction removed and what it promoted.
type EvictResult struct {
	// RemovedCount is the number of records deleted.
	RemovedCount uint64
	// PromotedCount is the number of deltas promoted to keyframes because
	// their base was evicted.
	PromotedCount uint64
	// RetainedFrom is the new lower bound of the retained history.
	// Sequences below this value are evicted.
	RetainedFrom uint64
}

// Evict removes records below the retention boundary for a run. It handles
// the delta chain: any representation whose base was evicted is promoted to
// a keyframe (its delta_json is cleared, making it self-contained) before
// the base row is deleted. Records and representations are deleted in one
// transaction, and the run's head metadata is updated to reflect the new
// retained-from boundary.
//
// The policy's MaxRecords determines the boundary: if the run has N records
// and MaxRecords is M < N, the oldest N-M records are evicted. A MaxRecords
// of zero or greater than or equal to the current count is a no-op.
//
// Evict refuses to remove the head record or any record that is the base of
// a non-evicted delta, so the retained history always forms a valid chain.
func (s *SQLiteStore) Evict(ctx context.Context, key RunKey, policy RetentionPolicy) (EvictResult, error) {
	if err := s.checkUsable(true); err != nil {
		return EvictResult{}, err
	}
	if policy.MaxRecords == 0 {
		return EvictResult{}, nil
	}

	head, err := s.Head(ctx, key)
	if err != nil {
		return EvictResult{}, err
	}
	if head.HeadSequence <= policy.MaxRecords {
		return EvictResult{RetainedFrom: 1}, nil
	}

	// The boundary is the first sequence to retain. Everything below it goes.
	boundary := head.HeadSequence - policy.MaxRecords + 1

	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return EvictResult{}, fmt.Errorf("pabtdebug: evict begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Find representations whose base is below the boundary — these need
	// promotion to keyframes before their bases are deleted.
	rows, err := tx.QueryContext(ctx,
		"SELECT event_uid FROM representations "+
			"WHERE session_id = ? AND run_id = ? AND base_seq IS NOT NULL AND base_seq < ?",
		key.SessionID, key.RunID, boundary)
	if err != nil {
		return EvictResult{}, fmt.Errorf("pabtdebug: evict find promotions: %w", err)
	}
	var promoteUIDs []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			rows.Close()
			return EvictResult{}, fmt.Errorf("pabtdebug: evict scan promotion: %w", err)
		}
		promoteUIDs = append(promoteUIDs, uid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return EvictResult{}, fmt.Errorf("pabtdebug: evict promotion iteration: %w", err)
	}

	// Promote: clear the delta fields so the representation becomes a keyframe.
	for _, uid := range promoteUIDs {
		if _, err := tx.ExecContext(ctx,
			"UPDATE representations SET base_event_uid = NULL, base_seq = NULL, "+
				"delta_json = NULL, delta_sha256 = NULL WHERE session_id = ? AND event_uid = ?",
			key.SessionID, uid); err != nil {
			return EvictResult{}, fmt.Errorf("pabtdebug: evict promote %s: %w", uid, err)
		}
	}

	// Delete representations below the boundary.
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM representations WHERE session_id = ? AND run_id = ? AND seq < ?",
		key.SessionID, key.RunID, boundary); err != nil {
		return EvictResult{}, fmt.Errorf("pabtdebug: evict delete representations: %w", err)
	}

	// Delete records below the boundary.
	recResult, err := tx.ExecContext(ctx,
		"DELETE FROM records WHERE session_id = ? AND run_id = ? AND seq < ?",
		key.SessionID, key.RunID, boundary)
	if err != nil {
		return EvictResult{}, fmt.Errorf("pabtdebug: evict delete records: %w", err)
	}

	removedCount, _ := recResult.RowsAffected()

	if err := tx.Commit(); err != nil {
		return EvictResult{}, fmt.Errorf("pabtdebug: evict commit: %w", err)
	}

	return EvictResult{
		RemovedCount:  uint64(removedCount),
		PromotedCount: uint64(len(promoteUIDs)),
		RetainedFrom:  boundary,
	}, nil
}

// RetainedFrom returns the lowest sequence a run still holds. Sequences
// below this value have been evicted and will return ErrEvicted from
// RecordAt and TreeAt. A return value of 1 means nothing has been evicted.
func (s *SQLiteStore) RetainedFrom(ctx context.Context, key RunKey) (uint64, error) {
	if err := s.checkUsable(false); err != nil {
		return 0, err
	}
	var minSeq sql.NullInt64
	err := s.readers.QueryRowContext(ctx,
		"SELECT MIN(seq) FROM records WHERE session_id = ? AND run_id = ?",
		key.SessionID, key.RunID).Scan(&minSeq)
	if err != nil {
		return 0, fmt.Errorf("pabtdebug: retained from: %w", err)
	}
	if !minSeq.Valid {
		return 0, fmt.Errorf("%w: run %s has no records", ErrNotFound, key.RunID)
	}
	return uint64(minSeq.Int64), nil
}
