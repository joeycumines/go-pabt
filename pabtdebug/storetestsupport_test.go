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
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite" // the test opens the file directly to corrupt it
)

// alterRecordBytes rewrites a stored record's canonical bytes without touching its
// digests, which is what corruption or tampering looks like from the store's side.
//
// The store is reached through its own connections, so a test that alters a row
// proves the store verifies what it reads rather than trusting its own write.
func (s *SQLiteStore) alterRecordBytes(t *testing.T, key RunKey, sequence uint64, marker string) error {
	t.Helper()
	var raw string
	row := s.writer.QueryRow(
		"SELECT CAST(record_json AS TEXT) FROM records WHERE session_id = ? AND run_id = ? AND seq = ?",
		key.SessionID, key.RunID, sequence)
	if err := row.Scan(&raw); err != nil {
		return err
	}
	altered := strings.Replace(raw, `"kind":"tick"`, `"kind":"`+marker+`"`, 1)
	if altered == raw {
		return errAlterNotApplied
	}
	_, err := s.writer.Exec(
		"UPDATE records SET record_json = ? WHERE session_id = ? AND run_id = ? AND seq = ?",
		[]byte(altered), key.SessionID, key.RunID, sequence)
	return err
}

// errAlterNotApplied reports that the fixture text was not present, so the test did
// not alter anything and must not be trusted.
var errAlterNotApplied = errors.New("pabtdebug test: the record text to alter was not found")

// alterTreeBytes rewrites a stored representation's tree with a different but
// internally consistent tree, which is what a buggy base-and-delta assembly path
// would leave behind.
//
// The forged tree carries its own recomputed node and tree digests, so a check of
// the tree against itself would accept it. Only the chain-covered copy inside the
// record says otherwise.
func (s *SQLiteStore) alterTreeBytes(t *testing.T, key RunKey, sequence uint64, tickCount uint32) error {
	t.Helper()
	forged := smallTree("n")
	forged.Nodes[0].Status.TickCount = uint64(tickCount)
	nodeDigest, err := HashNode(forged.Nodes[0])
	if err != nil {
		return err
	}
	forged.Nodes[0].ContentHash = nodeDigest
	treeDigest, err := HashTree(*forged)
	if err != nil {
		return err
	}
	forged.Digest = treeDigest
	encoded, err := CanonicalEncode(*forged)
	if err != nil {
		return err
	}
	_, err = s.writer.Exec(
		"UPDATE representations SET tree_json = ?, tree_sha256 = ? "+
			"WHERE session_id = ? AND run_id = ? AND seq = ?",
		encoded, mustHex(treeDigest), key.SessionID, key.RunID, sequence)
	return err
}

// pragmaOn reports a pragma's value on a pooled writer connection, which is where
// a pragma issued through a pool would be missing.
func (s *SQLiteStore) pragmaOn(t *testing.T, name string) int {
	t.Helper()
	var got int
	if err := s.writer.QueryRow("PRAGMA " + name).Scan(&got); err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return got
}

// orphanSessionID names a session that does not exist, used to prove a foreign key
// is actually in force rather than only declared.
const orphanSessionID = "no-such-session"

// bumpUserVersion writes a schema version the current build does not know, which
// is what a database from a newer build looks like.
func bumpUserVersion(t *testing.T, path string, version int) error {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	_, err = db.Exec("PRAGMA user_version = " + itoa(version))
	return err
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var digits []byte
	for v > 0 {
		digits = append([]byte{byte('0' + v%10)}, digits...)
		v /= 10
	}
	return string(digits)
}

func makeRecord(tree *StoredTree) StoredRecord {
	record := StoredRecord{
		Kind:          RecordTick,
		Revision:      RevisionScope{PlanRevision: testPlanRevision},
		PayloadState:  PayloadComplete,
		TreeEligible:  tree != nil,
		Tree:          tree,
		StartedAt:     Timestamp(time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)),
		CompletedAt:   Timestamp(time.Date(2026, 9, 25, 12, 0, 0, 1, time.UTC)),
		StatusPresent: true,
		Status:        2,
	}
	return record
}

func smallTree(rootID string) *StoredTree {
	node := StoredNode{
		ID:        rootID,
		Ordinal:   0,
		Type:      "root",
		Name:      "root",
		EffectIDs: []string{},
		Status:    StoredNodeStatus{TickCount: 1, LastStatus: 2, StatusPresent: true, Version: 1},
	}
	tree := &StoredTree{
		RootID:       rootID,
		Nodes:        []StoredNode{node},
		NodeCount:    1,
		PlanRevision: 1,
	}
	nodeDigest, err := HashNode(node)
	if err != nil {
		panic(err)
	}
	tree.Nodes[0].ContentHash = nodeDigest
	digest, err := HashTree(*tree)
	if err != nil {
		panic(err)
	}
	tree.Digest = digest
	return tree
}
