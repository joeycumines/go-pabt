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
		// Check whether this sequence was evicted rather than never committed.
		var exists int
		checkErr := s.readers.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM records WHERE session_id = ? AND run_id = ?",
			key.SessionID, key.RunID).Scan(&exists)
		if checkErr == nil && exists > 0 {
			// The run has records but not this one — check if it's below the minimum.
			var minSeq sql.NullInt64
			minErr := s.readers.QueryRowContext(ctx,
				"SELECT MIN(seq) FROM records WHERE session_id = ? AND run_id = ?",
				key.SessionID, key.RunID).Scan(&minSeq)
			if minErr == nil && minSeq.Valid && sequence < uint64(minSeq.Int64) {
				return StoredRecord{}, fmt.Errorf("%w: sequence %d of run %s", ErrEvicted, sequence, key.RunID)
			}
		}
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
//
// The store keeps the tree twice: once inside the record, where the chain
// covers it, and once in the representation table, where a base and a delta can
// reference it. A check of the representation's tree against its own digest is
// not enough, because a rewrite that recomputed that digest would satisfy it. The
// digest the chain already covers is therefore the one that decides, and a
// representation that disagrees with it is reported rather than served.
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
		// Check whether this sequence was evicted rather than never having a tree.
		var minSeq sql.NullInt64
		minErr := s.readers.QueryRowContext(ctx,
			"SELECT MIN(seq) FROM records WHERE session_id = ? AND run_id = ?",
			key.SessionID, key.RunID).Scan(&minSeq)
		if minErr == nil && minSeq.Valid && sequence < uint64(minSeq.Int64) {
			return StoredTree{}, fmt.Errorf("%w: tree at sequence %d of run %s", ErrEvicted, sequence, key.RunID)
		}
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
	// The record's own copy of the tree is covered by the chain, so it is what
	// says which tree this sequence really committed.
	record, err := s.RecordAt(ctx, key, sequence)
	if err != nil {
		return StoredTree{}, err
	}
	if record.Tree == nil {
		return StoredTree{}, fmt.Errorf("%w: sequence %d has a representation but its record has no tree",
			ErrStoreCorrupt, sequence)
	}
	committed, err := HashTree(*record.Tree)
	if err != nil {
		return StoredTree{}, err
	}
	if committed != digest {
		return StoredTree{}, fmt.Errorf("%w: the tree at %d is %q, the committed record holds %q",
			ErrStoreCorrupt, sequence, digest, committed)
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

	// The filter columns are qualified, because the page query joins the
	// representation table for a record's tree digest.
	var (
		rclauses = []string{"r.session_id = ?", "r.run_id = ?"}
		rargs    = []any{key.SessionID, key.RunID}
	)
	if query.Before != nil {
		rclauses = append(rclauses, "r.seq < ?")
		rargs = append(rargs, *query.Before)
	}
	if query.After != nil {
		rclauses = append(rclauses, "r.seq > ?")
		rargs = append(rargs, *query.After)
	}
	if len(query.Kinds) > 0 {
		placeholders := make([]string, 0, len(query.Kinds))
		for _, kind := range query.Kinds {
			placeholders = append(placeholders, "?")
			rargs = append(rargs, string(kind))
		}
		rclauses = append(rclauses, "r.kind IN ("+strings.Join(placeholders, ",")+")")
	}
	rargs = append(rargs, selectLimit)

	rows, err := s.readers.QueryContext(ctx,
		"SELECT r.seq, r.event_uid, r.kind, r.payload_state, r.tree_eligible, r.recorded_at, "+
			"r.record_sha256, length(r.record_json), t.tree_sha256 "+
			"FROM records AS r "+
			"LEFT JOIN representations AS t "+
			"  ON t.session_id = r.session_id AND t.event_uid = r.event_uid "+
			"WHERE "+strings.Join(rclauses, " AND ")+" ORDER BY r.seq LIMIT ?", rargs...)
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
			treeSHA    []byte
		)
		if err := rows.Scan(&seq, &summary.EventUID, &kind, &payload, &eligible, &recordedAt,
			&recordSHA, &contentLen, &treeSHA); err != nil {
			return EventPage{}, fmt.Errorf("pabtdebug: read events: %w", err)
		}
		if uint32(len(page.Events)) >= limit {
			page.HasMore = true
			break
		}
		if pageBytes+uint64(contentLen) > s.limits.MaxPageBytes && len(page.Events) > 0 {
			// The byte bound is on the content a page names, not on the page: a
			// summary row is small however large the record it points at, which is
			// the actual defence. One row is always returned, so a caller with a very
			// small byte bound still makes progress.
			page.HasMore = true
			break
		}
		summary.RunKey = key
		summary.Sequence = uint64(seq)
		summary.Kind = RecordKind(kind)
		summary.PayloadState = PayloadState(payload)
		summary.TreeEligible = eligible == 1
		summary.RecordSHA256 = hex.EncodeToString(recordSHA)
		summary.TreeSHA256 = hex.EncodeToString(treeSHA)
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

// RecordsSince returns the committed records after a position, in order, bounded.
//
// A reconnecting client reads the gap between where it was and where the run is now
// from here, rather than from anything the transport kept. The bound is on rows and on
// bytes, so a caller walking a long gap pages it instead of materialising a run, and
// HasMore is a fact about the store rather than about the page that was asked for.
func (s *SQLiteStore) RecordsSince(ctx context.Context, key RunKey, after uint64) (RecordPage, error) {
	if err := s.checkUsable(false); err != nil {
		return RecordPage{}, err
	}
	rows, err := s.readers.QueryContext(ctx,
		"SELECT seq, record_json FROM records WHERE session_id = ? AND run_id = ? AND seq > ? "+
			"ORDER BY seq LIMIT ?",
		key.SessionID, key.RunID, after, s.limits.MaxPageRows)
	if err != nil {
		return RecordPage{}, fmt.Errorf("pabtdebug: read records: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var (
		out   RecordPage
		bytes uint64
	)
	for rows.Next() {
		var (
			sequence uint64
			raw      []byte
		)
		if err := rows.Scan(&sequence, &raw); err != nil {
			return RecordPage{}, fmt.Errorf("pabtdebug: read a record: %w", err)
		}
		size := uint64(len(raw))
		if len(out.Records) > 0 && bytes+size > s.limits.MaxPageBytes {
			// The page stops before a record that would exceed the byte bound, and says
			// so, rather than returning a page that silently ignores the bound.
			out.HasMore = true
			break
		}
		var record StoredRecord
		if err := CanonicalDecode(raw, &record); err != nil {
			return RecordPage{}, fmt.Errorf("%w: record %d: %v", ErrStoreCorrupt, sequence, err)
		}
		digest, err := HashRecord(record)
		if err != nil {
			return RecordPage{}, err
		}
		if digest != record.RecordSHA256 {
			return RecordPage{}, fmt.Errorf("%w: record %d digest is %q, recomputes to %q",
				ErrStoreCorrupt, sequence, record.RecordSHA256, digest)
		}
		if len(out.Records) == 0 {
			out.OldestSeq = sequence
		}
		out.Records = append(out.Records, record)
		bytes += size
		out.Through = sequence
	}
	if err := rows.Err(); err != nil {
		return RecordPage{}, fmt.Errorf("pabtdebug: read records: %w", err)
	}
	if uint32(len(out.Records)) == s.limits.MaxPageRows {
		// The page came back full, so there may be more even though the byte bound was
		// never reached. Saying so is what lets a caller page without guessing.
		out.HasMore = true
	}
	return out, nil
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
