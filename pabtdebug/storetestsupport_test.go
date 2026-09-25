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
