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
	"time"
)

// zeroDigest is the previous-record link of the first record in a run.
const zeroDigest = "0000000000000000000000000000000000000000000000000000000000000000"

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func mustHex(digest string) []byte {
	raw, err := hex.DecodeString(digest)
	if err != nil {
		// A digest this layer computed is always hex, so a failure here is a bug
		// rather than an input problem and must not be silently written.
		panic("pabtdebug: digest is not hex: " + err.Error())
	}
	return raw
}

// Begin allocates the next sequence for a run under its append lease.
//
// The lease is what makes the chain gap-free: a sequence is allocated here and
// committed by CommitIntent or released by AbortIntent, and nothing else may
// allocate one in between. A caller that already holds the lease is told so
// rather than being allowed to interleave, because two interleaved appends would
// leave the chain with two records claiming one predecessor.
func (s *SQLiteStore) Begin(ctx context.Context, req BeginRequest) (IntentTicket, error) {
	if err := s.checkUsable(true); err != nil {
		return IntentTicket{}, err
	}
	if err := ctx.Err(); err != nil {
		return IntentTicket{}, err
	}
	head, err := s.Head(ctx, req.RunKey)
	if err != nil {
		return IntentTicket{}, err
	}
	attemptID, err := randomID("attempt")
	if err != nil {
		return IntentTicket{}, err
	}
	eventUID, err := randomID("event")
	if err != nil {
		return IntentTicket{}, err
	}
	now := s.clock().UTC()
	sequence := head.HeadSequence + 1

	// The lease and the intent row are written together, so a crash between them
	// cannot leave a sequence allocated with nothing to say it belongs to.
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return IntentTicket{}, fmt.Errorf("pabtdebug: begin intent: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var owner sql.NullString
	var readOnly int
	if err := tx.QueryRowContext(ctx,
		"SELECT r.append_owner, s.read_only FROM runs AS r "+
			"JOIN sessions AS s ON s.session_id = r.session_id "+
			"WHERE r.session_id = ? AND r.run_id = ?",
		req.RunKey.SessionID, req.RunKey.RunID).Scan(&owner, &readOnly); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return IntentTicket{}, fmt.Errorf("%w: run %s", ErrNotFound, req.RunKey.RunID)
		}
		return IntentTicket{}, fmt.Errorf("pabtdebug: begin intent: %w", err)
	}
	// An imported history is marked read-only, and the mark is enforced here rather
	// than only stated: a session that can be appended to is a history the store no
	// longer holds as it was imported.
	if readOnly != 0 {
		return IntentTicket{}, fmt.Errorf("%w: session %s is read-only",
			ErrStoreReadOnly, req.RunKey.SessionID)
	}
	if owner.Valid {
		return IntentTicket{}, fmt.Errorf("%w: held by attempt %s", ErrAppendBusy, owner.String)
	}
	inTxHead, err := headSequenceInTx(ctx, tx, req.RunKey)
	if err != nil {
		return IntentTicket{}, fmt.Errorf("pabtdebug: begin intent: %w", err)
	}
	if head.HeadSequence != inTxHead {
		return IntentTicket{}, fmt.Errorf("%w: the head moved between reads", ErrHeadMismatch)
	}

	update, err := tx.ExecContext(ctx,
		"UPDATE runs SET append_owner = ?, append_attempt_id = ?, append_epoch = "+
			"(append_epoch + 1), head_plan_revision = ? "+
			"WHERE session_id = ? AND run_id = ? AND append_owner IS NULL",
		attemptID, attemptID, req.ExpectedPlanRevision, req.RunKey.SessionID, req.RunKey.RunID)
	if err != nil {
		return IntentTicket{}, fmt.Errorf("pabtdebug: begin intent: %w", err)
	}
	affected, err := update.RowsAffected()
	if err != nil {
		return IntentTicket{}, fmt.Errorf("pabtdebug: begin intent: %w", err)
	}
	if affected == 0 {
		return IntentTicket{}, ErrAppendBusy
	}
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO intents (session_id, event_uid, plan_id, run_id, seq, attempt_id, "+
			"expected_head_seq, expected_tree_seq, expected_plan_revision, expected_generation, "+
			"execution_started, started_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?)",
		req.RunKey.SessionID, eventUID, req.RunKey.PlanID, req.RunKey.RunID, sequence, attemptID,
		head.HeadSequence, head.HeadTreeSequence, req.ExpectedPlanRevision, head.Generation, stamp(now),
	); err != nil {
		return IntentTicket{}, fmt.Errorf("pabtdebug: begin intent: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return IntentTicket{}, fmt.Errorf("pabtdebug: begin intent: %w", err)
	}
	ticket := IntentTicket{
		RunKey:               req.RunKey,
		Sequence:             sequence,
		EventUID:             eventUID,
		AttemptID:            attemptID,
		ExpectedHeadSequence: head.HeadSequence,
		ExpectedTreeSequence: head.HeadTreeSequence,
		ExpectedPlanRevision: req.ExpectedPlanRevision,
		ExpectedGeneration:   head.Generation,
		StartedAt:            now,
	}
	ticket.proof = s.proof(ticket.RunKey, ticket.Sequence, ticket.EventUID, ticket.AttemptID)
	return ticket, nil
}

// headSequenceInTx reads a run's head sequence inside a transaction.
//
// A read that fails is reported rather than treated as a zero head, because a
// zero head would look like a match against the value just read.
func headSequenceInTx(ctx context.Context, tx *sql.Tx, key RunKey) (uint64, error) {
	var head uint64
	err := tx.QueryRowContext(ctx,
		"SELECT head_seq FROM runs WHERE session_id = ? AND run_id = ?",
		key.SessionID, key.RunID).Scan(&head)
	if err != nil {
		return 0, err
	}
	return head, nil
}

// MarkExecutionStarted records that a tick began executing the application.
//
// It is written before the planner runs, so a crash mid-tick is distinguishable
// from a crash before it started, and recovery can say which.
func (s *SQLiteStore) MarkExecutionStarted(ctx context.Context, ticket IntentTicket) error {
	if err := s.checkUsable(true); err != nil {
		return err
	}
	if err := s.verifyTicket(ticket); err != nil {
		return err
	}
	result, err := s.writer.ExecContext(ctx,
		"UPDATE intents SET execution_started = 1 WHERE session_id = ? AND event_uid = ? AND attempt_id = ?",
		ticket.RunKey.SessionID, ticket.EventUID, ticket.AttemptID)
	if err != nil {
		return fmt.Errorf("pabtdebug: mark execution started: %w", err)
	}
	// A ticket whose intent is gone was already resolved. Saying so is the
	// difference between a caller that knows its tick committed and one that
	// believes it was abandoned.
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("pabtdebug: mark execution started: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("%w: attempt %s has no open intent", ErrNotFound, ticket.AttemptID)
	}
	return nil
}

// verifyTicket refuses a ticket that did not come from Begin.
func (s *SQLiteStore) verifyTicket(ticket IntentTicket) error {
	want := s.proof(ticket.RunKey, ticket.Sequence, ticket.EventUID, ticket.AttemptID)
	if ticket.proof != want {
		return fmt.Errorf("%w: the intent ticket was not issued by this store", ErrHeadMismatch)
	}
	return nil
}

// Commit writes a record, its tree, and the run head in one transaction.
//
// Nothing here is separate: the record row, its representation, the chain link,
// and the head advance either all land or none do. A kill between any two steps
// therefore leaves no half-written record, and because the sequence was already
// allocated by Begin, it also leaves no gap: the sequence is either committed or
// still held by an open intent that recovery can resolve.
func (s *SQLiteStore) Commit(ctx context.Context, ticket IntentTicket, record StoredRecord) (CommitReceipt, error) {
	return s.commit(ctx, ticket, record, nil)
}

// CommitWithDelta commits a record together with the exact change from its base.
//
// A nil delta stores a keyframe, which is what a reader needs when no base is
// eligible. A delta is stored beside the complete tree rather than instead of it,
// so a delta is an economy in transfer and never the only copy.
func (s *SQLiteStore) CommitWithDelta(
	ctx context.Context,
	ticket IntentTicket,
	record StoredRecord,
	delta *ExactTreeDelta,
) (CommitReceipt, error) {
	return s.commit(ctx, ticket, record, delta)
}

func (s *SQLiteStore) commit(
	ctx context.Context,
	ticket IntentTicket,
	record StoredRecord,
	delta *ExactTreeDelta,
) (CommitReceipt, error) {
	if err := s.checkUsable(true); err != nil {
		return CommitReceipt{}, err
	}
	if err := ctx.Err(); err != nil {
		return CommitReceipt{}, err
	}
	if err := s.verifyTicket(ticket); err != nil {
		return CommitReceipt{}, err
	}
	// The ticket owns the identity, so a record that names a different one is
	// refused rather than restamped. A record that names none is the normal case:
	// the caller built a payload and the store attaches it to the attempt.
	if record.EventUID != "" && record.EventUID != ticket.EventUID {
		return CommitReceipt{}, fmt.Errorf("%w: the record names event %s, the ticket is %s",
			ErrHeadMismatch, record.EventUID, ticket.EventUID)
	}
	if record.Sequence != 0 && record.Sequence != ticket.Sequence {
		return CommitReceipt{}, fmt.Errorf("%w: the record names sequence %d, the ticket is %d",
			ErrHeadMismatch, record.Sequence, ticket.Sequence)
	}
	if !record.RunKey.IsZero() && record.RunKey != ticket.RunKey {
		return CommitReceipt{}, fmt.Errorf("%w: the record belongs to run %s, the ticket is %s",
			ErrHeadMismatch, record.RunKey.RunID, ticket.RunKey.RunID)
	}

	now := s.clock().UTC()

	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return CommitReceipt{}, fmt.Errorf("pabtdebug: commit: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var (
		headSeq       uint64
		appendOwner   sql.NullString
		appendAttempt sql.NullString
		previous      []byte
	)
	err = tx.QueryRowContext(ctx,
		"SELECT head_seq, append_owner, append_attempt_id, head_record_sha256 "+
			"FROM runs WHERE session_id = ? AND run_id = ?",
		ticket.RunKey.SessionID, ticket.RunKey.RunID).
		Scan(&headSeq, &appendOwner, &appendAttempt, &previous)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return CommitReceipt{}, fmt.Errorf("%w: run %s", ErrNotFound, ticket.RunKey.RunID)
	case err != nil:
		return CommitReceipt{}, fmt.Errorf("pabtdebug: commit: %w", err)
	}
	if !appendOwner.Valid || appendAttempt.String != ticket.AttemptID {
		return CommitReceipt{}, fmt.Errorf("%w: the lease is not held by attempt %s",
			ErrAppendBusy, ticket.AttemptID)
	}
	if headSeq+1 != ticket.Sequence {
		return CommitReceipt{}, fmt.Errorf("%w: expected sequence %d, the run is at %d",
			ErrHeadMismatch, ticket.Sequence, headSeq)
	}

	// The chain link is read from the store, never from the caller, so a record
	// cannot claim a predecessor the run does not have.
	link := zeroDigest
	if headSeq > 0 {
		if len(previous) == 0 {
			return CommitReceipt{}, fmt.Errorf("%w: the run has records but no head digest", ErrStoreCorrupt)
		}
		link = hex.EncodeToString(previous)
	}

	// Everything the store owns is stamped first, and the digests are taken over
	// the record as it will be stored. Hashing before the store-owned fields are
	// set would produce a digest over a record that is not the one being written.
	// The record's plan revision must be at least the one the attempt began at.
	//
	// Equality would be wrong: a tick that expands the plan moves the revision
	// while it runs, so the structure the record describes is the one the tick
	// ended with, not the one it started from. A record below the attempt's
	// revision is the real fault, because that would be a structure the attempt
	// cannot have produced.
	if record.Revision.PlanRevision < ticket.ExpectedPlanRevision {
		return CommitReceipt{}, fmt.Errorf("%w: the record is at plan revision %d, the attempt began at %d",
			ErrHeadMismatch, record.Revision.PlanRevision, ticket.ExpectedPlanRevision)
	}
	// Everything the store owns is stamped before the digests are taken, including
	// the eligibility flag, which is derived rather than believed: a record that
	// claims eligibility with no tree would be a claim the store cannot honour.
	record.TreeEligible = record.Tree != nil
	if record.Tree != nil {
		// A tree belongs to the record that committed it, so its envelope is the
		// record's identity rather than whatever the caller built. The digest is
		// re-sealed because the envelope is inside it.
		record.Tree.RunKey = ticket.RunKey
		record.Tree.EventUID = ticket.EventUID
		record.Tree.Sequence = ticket.Sequence
		digest, err := HashTree(*record.Tree)
		if err != nil {
			return CommitReceipt{}, err
		}
		record.Tree.Digest = digest
	}
	record.PreviousRecordSHA256 = link
	record.Format = FormatName
	record.Version = FormatVersion
	record.EventUID = ticket.EventUID
	record.Sequence = ticket.Sequence
	record.RunKey = ticket.RunKey
	record.AttemptID = ticket.AttemptID
	record.RecordSHA256 = ""

	payloadDigest, err := HashPayload(record)
	if err != nil {
		return CommitReceipt{}, err
	}
	record.PayloadSHA256 = payloadDigest
	record.RecordSHA256 = ""
	recordDigest, err := HashRecord(record)
	if err != nil {
		return CommitReceipt{}, err
	}
	record.RecordSHA256 = recordDigest
	recordBytes, err := CanonicalEncode(record)
	if err != nil {
		return CommitReceipt{}, err
	}
	if uint64(len(recordBytes)) > s.limits.MaxRecordBytes {
		return CommitReceipt{}, fmt.Errorf("%w: the record is %d bytes, over the %d limit",
			ErrValueUnencodable, len(recordBytes), s.limits.MaxRecordBytes)
	}

	var treeDigest string
	var pendingTree []byte
	if record.Tree != nil {
		treeBytes, err := CanonicalEncode(record.Tree)
		if err != nil {
			return CommitReceipt{}, err
		}
		if uint64(len(treeBytes)) > s.limits.MaxTreeBytes {
			return CommitReceipt{}, fmt.Errorf("%w: the tree is %d bytes, over the %d limit",
				ErrValueUnencodable, len(treeBytes), s.limits.MaxTreeBytes)
		}
		treeDigest, err = HashTree(*record.Tree)
		if err != nil {
			return CommitReceipt{}, err
		}
		pendingTree = treeBytes
	}

	// The record comes first: the representation below references it, and the head
	// below is the run's own link to it. All are in one transaction, so they land
	// together or not at all.
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO records (session_id, event_uid, plan_id, run_id, seq, attempt_id, kind, "+
			"payload_state, tree_eligible, execution_started, recorded_at, record_json, "+
			"payload_sha256, previous_record_sha256, record_sha256) "+
			"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		ticket.RunKey.SessionID, ticket.EventUID, ticket.RunKey.PlanID, ticket.RunKey.RunID,
		ticket.Sequence, ticket.AttemptID, string(record.Kind), string(record.PayloadState),
		boolToInt(record.Tree != nil), boolToInt(record.ExecutionStarted), stamp(now), recordBytes,
		mustHex(payloadDigest), mustHex(link), mustHex(recordDigest),
	); err != nil {
		return CommitReceipt{}, fmt.Errorf("pabtdebug: commit record: %w", err)
	}
	if pendingTree != nil {
		var (
			baseUID  any
			baseSeq  any
			deltaRaw any
			deltaHex any
			ops      = 0
		)
		if delta != nil {
			// The store is the authority on what it committed, so it stamps the
			// delta's target envelope from the ticket and the record rather than
			// believing a caller-supplied one. The caller supplied the operations;
			// the identity and the tree digest are this store's to fix.
			sealed := *delta
			sealed.RunKey = ticket.RunKey
			sealed.TargetEventUID = ticket.EventUID
			sealed.TargetSequence = ticket.Sequence
			sealed.TargetPlanRevision = record.Revision.PlanRevision
			sealed.TargetDigest = treeDigest
			digest, err := HashDelta(sealed)
			if err != nil {
				return CommitReceipt{}, err
			}
			sealed.SHA256 = digest
			// The strongest check available: the delta, applied to the base this store
			// actually recorded, must produce the tree this store actually stored.
			// Anything else means the operations do not describe the commit.
			if err := s.verifyDeltaReconstructs(ctx, ticket.RunKey, sealed, treeDigest); err != nil {
				return CommitReceipt{}, err
			}
			encoded, err := CanonicalEncode(sealed)
			if err != nil {
				return CommitReceipt{}, err
			}
			baseUID = sealed.BaseEventUID
			baseSeq = sealed.BaseSequence
			deltaRaw = encoded
			deltaHex = mustHex(digest)
			ops = len(sealed.Operations)
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO representations (session_id, event_uid, plan_id, run_id, seq, "+
				"base_event_uid, base_seq, target_plan_revision, tree_json, tree_sha256, "+
				"delta_json, delta_sha256, operation_count) "+
				"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			ticket.RunKey.SessionID, ticket.EventUID, ticket.RunKey.PlanID, ticket.RunKey.RunID,
			ticket.Sequence, baseUID, baseSeq, record.Revision.PlanRevision, pendingTree,
			mustHex(treeDigest), deltaRaw, deltaHex, ops,
		); err != nil {
			return CommitReceipt{}, fmt.Errorf("pabtdebug: commit tree: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE runs SET head_seq = ?, append_owner = NULL, append_attempt_id = NULL, "+
			"head_record_sha256 = ?, record_sha256 = ?, head_plan_revision = ? "+
			"WHERE session_id = ? AND run_id = ?",
		ticket.Sequence, mustHex(recordDigest), mustHex(recordDigest),
		record.Revision.PlanRevision, ticket.RunKey.SessionID, ticket.RunKey.RunID,
	); err != nil {
		return CommitReceipt{}, fmt.Errorf("pabtdebug: commit head: %w", err)
	}
	if record.Tree != nil {
		if _, err := tx.ExecContext(ctx,
			"UPDATE runs SET head_tree_seq = ?, head_tree_event_uid = ?, head_tree_digest = ? "+
				"WHERE session_id = ? AND run_id = ?",
			ticket.Sequence, ticket.EventUID, treeDigest,
			ticket.RunKey.SessionID, ticket.RunKey.RunID,
		); err != nil {
			return CommitReceipt{}, fmt.Errorf("pabtdebug: commit tree head: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM intents WHERE session_id = ? AND event_uid = ? AND attempt_id = ?",
		ticket.RunKey.SessionID, ticket.EventUID, ticket.AttemptID,
	); err != nil {
		return CommitReceipt{}, fmt.Errorf("pabtdebug: commit intent: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE sessions SET record_count = record_count + 1 WHERE session_id = ?",
		ticket.RunKey.SessionID); err != nil {
		return CommitReceipt{}, fmt.Errorf("pabtdebug: commit session: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return CommitReceipt{}, fmt.Errorf("pabtdebug: commit: %w", err)
	}
	return CommitReceipt{
		RunKey:       ticket.RunKey,
		Sequence:     ticket.Sequence,
		EventUID:     ticket.EventUID,
		RecordSHA256: recordDigest,
		TreeSHA256:   treeDigest,
		CommittedAt:  now,
		proof:        s.proof(ticket.RunKey, ticket.Sequence, ticket.EventUID, recordDigest),
	}, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// Abort releases a sequence without writing a record.
//
// It is used when a tick fails before it can produce a record, and when recovery
// decides an intent should not become one. Releasing rather than committing an
// empty record is what keeps the chain free of records that say nothing happened.
func (s *SQLiteStore) Abort(ctx context.Context, ticket IntentTicket) error {
	if err := s.checkUsable(true); err != nil {
		return err
	}
	if err := s.verifyTicket(ticket); err != nil {
		return err
	}
	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("pabtdebug: abort: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	deleted, err := tx.ExecContext(ctx,
		"DELETE FROM intents WHERE session_id = ? AND event_uid = ? AND attempt_id = ?",
		ticket.RunKey.SessionID, ticket.EventUID, ticket.AttemptID)
	if err != nil {
		return fmt.Errorf("pabtdebug: abort: %w", err)
	}
	if affected, err := deleted.RowsAffected(); err == nil && affected == 0 {
		return fmt.Errorf("%w: attempt %s has no open intent", ErrNotFound, ticket.AttemptID)
	}
	if _, err := tx.ExecContext(ctx,
		"UPDATE runs SET append_owner = NULL, append_attempt_id = NULL "+
			"WHERE session_id = ? AND run_id = ? AND append_attempt_id = ?",
		ticket.RunKey.SessionID, ticket.RunKey.RunID, ticket.AttemptID); err != nil {
		return fmt.Errorf("pabtdebug: abort: %w", err)
	}
	return tx.Commit()
}

// PendingIntents returns the intents left open by an earlier process, in sequence
// order.
//
// An open intent is not a record. It says a sequence was allocated and never
// resolved, and the caller must decide what it becomes before recording more.
func (s *SQLiteStore) PendingIntents(ctx context.Context) ([]IntentTicket, error) {
	if err := s.checkUsable(false); err != nil {
		return nil, err
	}
	rows, err := s.readers.QueryContext(ctx,
		"SELECT session_id, plan_id, run_id, seq, event_uid, attempt_id, "+
			"expected_head_seq, expected_tree_seq, expected_plan_revision, expected_generation, "+
			"execution_started, started_at "+
			"FROM intents ORDER BY session_id, run_id, seq")
	if err != nil {
		return nil, fmt.Errorf("pabtdebug: read intents: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []IntentTicket
	for rows.Next() {
		var (
			ticket                               IntentTicket
			started                              string
			seq, headSeq, treeSeq, revision, gen int
		)
		if err := rows.Scan(&ticket.RunKey.SessionID, &ticket.RunKey.PlanID, &ticket.RunKey.RunID,
			&seq, &ticket.EventUID, &ticket.AttemptID, &headSeq, &treeSeq, &revision, &gen,
			&ticket.ExecutionStarted, &started); err != nil {
			return nil, fmt.Errorf("pabtdebug: read intents: %w", err)
		}
		ticket.Sequence = uint64(seq)
		ticket.ExpectedHeadSequence = uint64(headSeq)
		ticket.ExpectedTreeSequence = uint64(treeSeq)
		ticket.ExpectedPlanRevision = uint64(revision)
		ticket.ExpectedGeneration = uint64(gen)
		if parsed, perr := time.Parse(time.RFC3339Nano, started); perr == nil {
			ticket.StartedAt = parsed.UTC()
		}
		// A recovered ticket gets a fresh proof so it can be resolved. The old one
		// is meaningless in a new process.
		ticket.proof = s.proof(ticket.RunKey, ticket.Sequence, ticket.EventUID, ticket.AttemptID)
		out = append(out, ticket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pabtdebug: read intents: %w", err)
	}
	return out, nil
}
