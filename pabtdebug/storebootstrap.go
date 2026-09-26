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
	"fmt"
	"strings"
)

// Bootstrap creates the installation, session, plan, and run of a fresh store.
//
// All four are written in one transaction. A store is either fully bootstrapped
// or not bootstrapped at all, because a half-created history has no run to record
// into and no way to be told apart from a complete one.
func (s *SQLiteStore) Bootstrap(ctx context.Context, req BootstrapRequest) (SessionIdentity, error) {
	if err := s.checkUsable(true); err != nil {
		return SessionIdentity{}, err
	}
	if strings.TrimSpace(req.PlanID) == "" || strings.TrimSpace(req.RunID) == "" {
		return SessionIdentity{}, fmt.Errorf(`pabtdebug: a plan id and a run id are required`)
	}
	storeID, err := randomID("store")
	if err != nil {
		return SessionIdentity{}, err
	}
	sessionID, err := randomID("session")
	if err != nil {
		return SessionIdentity{}, err
	}
	now := s.clock().UTC()

	plan := StoredPlan{
		PlanID:    req.PlanID,
		Title:     req.Title,
		Metadata:  req.Metadata,
		CreatedAt: Timestamp(now),
	}
	// The metadata is stored as its own column as well as inside the plan's
	// canonical bytes, so a reader can fetch it without decoding the whole plan.
	metadataJSON, err := CanonicalEncode(req.Metadata)
	if err != nil {
		return SessionIdentity{}, err
	}
	// The digest covers the plan as stored, so the field is stamped first and the
	// stored bytes are the ones the digest was taken over.
	plan.RecordSHA256 = ""
	planDigest, err := HashCanonical(DomainPlan, plan)
	if err != nil {
		return SessionIdentity{}, err
	}
	plan.RecordSHA256 = planDigest
	if _, err := CanonicalEncode(plan); err != nil {
		return SessionIdentity{}, err
	}

	tx, err := s.writer.BeginTx(ctx, nil)
	if err != nil {
		return SessionIdentity{}, fmt.Errorf("pabtdebug: bootstrap: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := tx.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM installation WHERE singleton = 1").Scan(&exists); err != nil {
		return SessionIdentity{}, fmt.Errorf("pabtdebug: bootstrap: %w", err)
	}
	if exists > 0 {
		return SessionIdentity{}, fmt.Errorf("%w: the store is already bootstrapped", ErrHeadMismatch)
	}
	statements := []struct {
		query string
		args  []any
	}{
		{"INSERT INTO installation (singleton, store_id, generation, active_session_id, recovery, updated_at) " +
			"VALUES (1, ?, 1, ?, ?, ?)", []any{storeID, sessionID, string(RecoveryReady), stamp(now)}},
		{"INSERT INTO sessions (session_id, store_id, generation, read_only, created_at, record_count) " +
			"VALUES (?, ?, 1, 0, ?, 0)", []any{sessionID, storeID, stamp(now)}},
		{"INSERT INTO plans (session_id, plan_id, title, created_at, metadata_json, record_sha256) " +
			"VALUES (?, ?, ?, ?, ?, ?)",
			[]any{sessionID, req.PlanID, planTitle(req.Title), stamp(now), metadataJSON, mustHex(planDigest)}},
		{"INSERT INTO runs (session_id, plan_id, run_id, state, started_at, initial_plan_revision, " +
			"head_seq, head_tree_seq, head_plan_revision, append_epoch) " +
			"VALUES (?, ?, ?, 'active', ?, 0, 0, 0, 0, 0)", []any{sessionID, req.PlanID, req.RunID, stamp(now)}},
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement.query, statement.args...); err != nil {
			return SessionIdentity{}, fmt.Errorf("pabtdebug: bootstrap: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return SessionIdentity{}, fmt.Errorf("pabtdebug: bootstrap: %w", err)
	}
	s.mu.Lock()
	s.storeID = StoreID(storeID)
	s.mu.Unlock()
	return SessionIdentity{
		StoreID:    StoreID(storeID),
		SessionID:  sessionID,
		PlanID:     req.PlanID,
		RunID:      req.RunID,
		Generation: 1,
	}, nil
}

func planTitle(title string) string {
	if strings.TrimSpace(title) == "" {
		return "pabtdebug"
	}
	return title
}
