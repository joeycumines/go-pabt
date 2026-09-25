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
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Head returns a run's latest committed position.
//
// It is read from the read pool rather than the write connection, so reading the
// head never queues behind a commit.
func (s *SQLiteStore) Head(ctx context.Context, key RunKey) (RunHead, error) {
	if err := s.checkUsable(false); err != nil {
		return RunHead{}, err
	}
	if key.SessionID == "" || key.PlanID == "" || key.RunID == "" {
		return RunHead{}, fmt.Errorf("%w: a run key needs a session, a plan, and a run", ErrNotFound)
	}
	var (
		out        RunHead
		generation int
		headSeq    int
		treeSeq    int
		headUID    sql.NullString
		treeDigest sql.NullString
		headRecord []byte
		planRev    int
	)
	row := s.readers.QueryRowContext(ctx,
		"SELECT r.session_id, r.plan_id, r.run_id, r.head_seq, r.head_tree_seq, "+
			"r.head_tree_event_uid, r.head_tree_digest, r.head_record_sha256, r.head_plan_revision, "+
			"i.generation FROM runs AS r "+
			"JOIN sessions AS i ON i.session_id = r.session_id "+
			"WHERE r.session_id = ? AND r.plan_id = ? AND r.run_id = ?",
		key.SessionID, key.PlanID, key.RunID)
	err := row.Scan(&out.RunKey.SessionID, &out.RunKey.PlanID, &out.RunKey.RunID, &headSeq, &treeSeq,
		&headUID, &treeDigest, &headRecord, &planRev, &generation)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return RunHead{}, fmt.Errorf("%w: run %s", ErrNotFound, key.RunID)
	case err != nil:
		return RunHead{}, fmt.Errorf("pabtdebug: read head: %w", err)
	}
	out.Generation = uint64(generation)
	out.HeadSequence = uint64(headSeq)
	out.HeadTreeSequence = uint64(treeSeq)
	out.HeadTreeEventUID = headUID.String
	out.HeadTreeDigest = treeDigest.String
	out.HeadPlanRevision = uint64(planRev)
	if len(headRecord) > 0 {
		out.HeadRecordSHA256 = hex.EncodeToString(headRecord)
	}
	return out, nil
}

// RecordAt returns one committed record by sequence.
//
// The record is decoded from the bytes the store committed, so what a reader sees
// is exactly what the chain covers. Its digests are verified before it is
// returned, because a record that does not match its own digest is corrupt rather
// than merely surprising.
func (s *SQLiteStore) RecordAt(ctx context.Context, key RunKey, sequence uint64) (StoredRecord, error) {
	if err := s.checkUsable(false); err != nil {
		return StoredRecord{}, err
	}
	var raw []byte
	row := s.readers.QueryRowContext(ctx,
		"SELECT record_json FROM records WHERE session_id = ? AND run_id = ? AND seq = ?",
		key.SessionID, key.RunID, sequence)
	switch err := row.Scan(&raw); {
	case errors.Is(err, sql.ErrNoRows):
		return StoredRecord{}, fmt.Errorf("%w: sequence %d of run %s", ErrNotFound, sequence, key.RunID)
	case err != nil:
		return StoredRecord{}, fmt.Errorf("pabtdebug: read record: %w", err)
	}
	var record StoredRecord
	if err := CanonicalDecode(raw, &record); err != nil {
		return StoredRecord{}, fmt.Errorf("%w: record %d: %v", ErrStoreCorrupt, sequence, err)
	}
	digest, err := HashRecord(record)
	if err != nil {
		return StoredRecord{}, err
	}
	if digest != record.RecordSHA256 {
		return StoredRecord{}, fmt.Errorf("%w: record %d digest is %q, recomputes to %q",
			ErrStoreCorrupt, sequence, record.RecordSHA256, digest)
	}
	return record, nil
}

// TreeAt returns the tree a record was committed with.
func (s *SQLiteStore) TreeAt(ctx context.Context, key RunKey, sequence uint64) (StoredTree, error) {
	if err := s.checkUsable(false); err != nil {
		return StoredTree{}, err
	}
	var raw []byte
	row := s.readers.QueryRowContext(ctx,
		"SELECT tree_json FROM representations WHERE session_id = ? AND run_id = ? AND seq = ?",
		key.SessionID, key.RunID, sequence)
	switch err := row.Scan(&raw); {
	case errors.Is(err, sql.ErrNoRows):
		return StoredTree{}, fmt.Errorf("%w: no tree at sequence %d of run %s",
			ErrNotFound, sequence, key.RunID)
	case err != nil:
		return StoredTree{}, fmt.Errorf("pabtdebug: read tree: %w", err)
	}
	var tree StoredTree
	if err := CanonicalDecode(raw, &tree); err != nil {
		return StoredTree{}, fmt.Errorf("%w: tree at %d: %v", ErrStoreCorrupt, sequence, err)
	}
	digest, err := HashTree(tree)
	if err != nil {
		return StoredTree{}, err
	}
	if digest != tree.Digest {
		return StoredTree{}, fmt.Errorf("%w: tree at %d digest is %q, recomputes to %q",
			ErrStoreCorrupt, sequence, tree.Digest, digest)
	}
	return tree, nil
}

// Events returns one bounded page of a run's timeline.
//
// The page carries metadata only, so a caller can page a run of any size without
// ever materialising a whole run. The row and byte bounds are both enforced, and
// the caller learns from HasMore that there is more.
func (s *SQLiteStore) Events(ctx context.Context, query EventQuery) (EventPage, error) {
	if err := s.checkUsable(false); err != nil {
		return EventPage{}, err
	}
	key := query.RunKey
	if key.SessionID == "" || key.PlanID == "" || key.RunID == "" {
		return EventPage{}, fmt.Errorf("%w: a run key needs a session, a plan, and a run", ErrNotFound)
	}
	limit := query.Limit
	if limit == 0 || limit > s.limits.MaxPageRows {
		limit = s.limits.MaxPageRows
	}
	// One more row than the page holds, so HasMore is a fact about the store
	// rather than a guess.
	selectLimit := int(limit) + 1

	var (
		clauses = []string{"session_id = ?", "run_id = ?"}
		args    = []any{key.SessionID, key.RunID}
	)
	if query.Before != nil {
		clauses = append(clauses, "seq < ?")
		args = append(args, *query.Before)
	}
	if query.After != nil {
		clauses = append(clauses, "seq > ?")
		args = append(args, *query.After)
	}
	if len(query.Kinds) > 0 {
		placeholders := make([]string, 0, len(query.Kinds))
		for _, kind := range query.Kinds {
			placeholders = append(placeholders, "?")
			args = append(args, string(kind))
		}
		clauses = append(clauses, "kind IN ("+strings.Join(placeholders, ",")+")")
	}
	args = append(args, selectLimit)

	rows, err := s.readers.QueryContext(ctx,
		"SELECT seq, event_uid, kind, payload_state, tree_eligible, recorded_at, "+
			"record_sha256, length(record_json) FROM records WHERE "+strings.Join(clauses, " AND ")+
			" ORDER BY seq LIMIT ?", args...)
	if err != nil {
		return EventPage{}, fmt.Errorf("pabtdebug: read events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	page := EventPage{RunKey: key}
	var pageBytes uint64
	for rows.Next() {
		var (
			summary    EventSummary
			seq        int
			kind       string
			payload    string
			eligible   int
			recordedAt string
			recordSHA  []byte
			contentLen int
		)
		if err := rows.Scan(&seq, &summary.EventUID, &kind, &payload, &eligible, &recordedAt,
			&recordSHA, &contentLen); err != nil {
			return EventPage{}, fmt.Errorf("pabtdebug: read events: %w", err)
		}
		if uint32(len(page.Events)) >= limit {
			page.HasMore = true
			break
		}
		if pageBytes+uint64(contentLen) > s.limits.MaxPageBytes && len(page.Events) > 0 {
			// The byte bound stops the page even when the row bound has not been
			// reached, so one enormous record cannot make the page enormous.
			page.HasMore = true
			break
		}
		summary.RunKey = key
		summary.Sequence = uint64(seq)
		summary.Kind = RecordKind(kind)
		summary.PayloadState = PayloadState(payload)
		summary.TreeEligible = eligible == 1
		summary.RecordSHA256 = hex.EncodeToString(recordSHA)
		summary.ContentBytes = uint64(contentLen)
		if parsed, perr := time.Parse(time.RFC3339Nano, recordedAt); perr == nil {
			summary.RecordedAt = parsed.UTC()
		}
		pageBytes += uint64(contentLen)
		if len(page.Events) == 0 {
			page.OldestSeq = summary.Sequence
		}
		page.NewestSeq = summary.Sequence
		page.Events = append(page.Events, summary)
	}
	if err := rows.Err(); err != nil {
		return EventPage{}, fmt.Errorf("pabtdebug: read events: %w", err)
	}
	return page, nil
}

// RunState returns a run's lifecycle state.
func (s *SQLiteStore) RunState(ctx context.Context, key RunKey) (string, error) {
	if err := s.checkUsable(false); err != nil {
		return "", err
	}
	var state string
	row := s.readers.QueryRowContext(ctx,
		"SELECT state FROM runs WHERE session_id = ? AND run_id = ?", key.SessionID, key.RunID)
	switch err := row.Scan(&state); {
	case errors.Is(err, sql.ErrNoRows):
		return "", fmt.Errorf("%w: run %s", ErrNotFound, key.RunID)
	case err != nil:
		return "", fmt.Errorf("pabtdebug: read run state: %w", err)
	}
	return state, nil
}

// VerifyChain walks a run's records and checks every link and digest.
//
// It is the store's own integrity check, and it runs on open when the store was
// not shut down cleanly. A run whose chain does not verify is reported rather
// than served, because a debugger that shows a plausible but altered history is
// worse than one that refuses.
func (s *SQLiteStore) VerifyChain(ctx context.Context, key RunKey) (uint64, error) {
	if err := s.checkUsable(false); err != nil {
		return 0, err
	}
	rows, err := s.readers.QueryContext(ctx,
		"SELECT seq, record_json FROM records WHERE session_id = ? AND run_id = ? ORDER BY seq",
		key.SessionID, key.RunID)
	if err != nil {
		return 0, fmt.Errorf("pabtdebug: verify chain: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var (
		verified uint64
		expected uint64
		previous = zeroDigest
	)
	for rows.Next() {
		var (
			seq int
			raw []byte
		)
		if err := rows.Scan(&seq, &raw); err != nil {
			return 0, fmt.Errorf("pabtdebug: verify chain: %w", err)
		}
		if uint64(seq) != expected+1 {
			return verified, fmt.Errorf("%w: run %s jumps from %d to %d",
				ErrStoreCorrupt, key.RunID, expected, seq)
		}
		var record StoredRecord
		if err := CanonicalDecode(raw, &record); err != nil {
			return verified, fmt.Errorf("%w: record %d: %v", ErrStoreCorrupt, seq, err)
		}
		if record.PreviousRecordSHA256 != previous {
			return verified, fmt.Errorf("%w: record %d links to %q, the run is at %q",
				ErrStoreCorrupt, seq, record.PreviousRecordSHA256, previous)
		}
		digest, err := HashRecord(record)
		if err != nil {
			return verified, err
		}
		if digest != record.RecordSHA256 {
			return verified, fmt.Errorf("%w: record %d digest is %q, recomputes to %q",
				ErrStoreCorrupt, seq, record.RecordSHA256, digest)
		}
		previous = record.RecordSHA256
		expected = uint64(seq)
		verified++
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("pabtdebug: verify chain: %w", err)
	}
	return verified, nil
}

// close releases the store's resources without the idempotence of Close, so a
// failed open does not report an error the caller has not seen yet.
func (s *SQLiteStore) close() error {
	if s.lease != nil {
		_, _ = s.lease.ExecContext(context.Background(), "ROLLBACK")
		_ = s.lease.Close()
		s.lease = nil
	}
	if s.leaseDB != nil {
		_ = s.leaseDB.Close()
		s.leaseDB = nil
	}
	if s.readers != nil {
		_ = s.readers.Close()
	}
	if s.writer != nil {
		_ = s.writer.Close()
	}
	return nil
}
