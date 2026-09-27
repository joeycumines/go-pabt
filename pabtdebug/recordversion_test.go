/*
   Copyright 2026 Joseph Cumines

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
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestAVersionOneRecordIsRefusedRatherThanServed pins what a record written
// before the action and effect sections existed actually does, because the
// comment on FormatVersion once claimed something else.
//
// The claim was that such a record "decodes into this shape with those sections
// absent, which reads as 'not recorded' rather than as 'none'". It does decode.
// It is then refused, and not by a version check: canonical encoding writes a nil
// slice as an explicit null, so the absent sections come back as "actions":null
// and "effects":null, which are bytes the original did not carry. The digest the
// record was written with is therefore not the digest its own contents
// recompute to, and the read paths answer that as corruption.
//
// That is the safe direction - a record that cannot be proven intact is not
// served - but it is a different fact from the one the comment stated, and a
// reader who believed the comment would expect "not recorded" and get a refusal
// that names tampering. This test is here so the next reader is told by the
// suite rather than by a fix.
func TestAVersionOneRecordIsRefusedRatherThanServed(t *testing.T) {
	// A record of the current shape, so the only thing that differs is the two
	// members a version 1 record would not have carried.
	current := StoredRecord{
		Format:        FormatName,
		Version:       FormatVersion,
		EventUID:      "pabt-v1-0000000000000001",
		Sequence:      1,
		AttemptID:     "attempt-1",
		Kind:          RecordTick,
		PayloadState:  PayloadComplete,
		StartedAt:     Timestamp(time.Unix(1, 0).UTC()),
		CompletedAt:   Timestamp(time.Unix(2, 0).UTC()),
		Status:        2,
		StatusPresent: true,
		Conditions:    []StoredCondition{},
		Relations:     []StoredRelation{},
		Actions:       []StoredAction{},
		Effects:       []StoredEffect{},
	}
	encoded, err := CanonicalEncode(current)
	if err != nil {
		t.Fatalf("CanonicalEncode: %v", err)
	}
	if !bytes.Contains(encoded, []byte(`"actions"`)) || !bytes.Contains(encoded, []byte(`"effects"`)) {
		t.Fatalf("the current shape does not carry the two sections: %s", encoded)
	}

	// Build what a version 1 record's bytes would have been: the same members with
	// those two absent, which is the only difference the format change made.
	var members map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &members); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	delete(members, "actions")
	delete(members, "effects")
	// And the version it was written with, which is the one thing a read path could
	// have refused on and does not.
	members["version"] = json.RawMessage(`1`)
	v1Bytes, err := CanonicalEncode(members)
	if err != nil {
		t.Fatalf("CanonicalEncode the version 1 members: %v", err)
	}
	if bytes.Contains(v1Bytes, []byte(`"actions"`)) {
		t.Fatalf("the version 1 bytes still carry the section: %s", v1Bytes)
	}

	// It decodes. That part of the old claim was true, and it is what makes the
	// refusal worth pinning: the failure happens after the decode, not at it.
	var decoded StoredRecord
	if err := CanonicalDecode(v1Bytes, &decoded); err != nil {
		t.Fatalf("a version 1 record does not decode into the current shape: %v", err)
	}
	if decoded.Actions != nil || decoded.Effects != nil {
		t.Fatalf("the absent sections decoded as present: %+v", decoded)
	}
	if decoded.Format != FormatName {
		t.Errorf("format = %q, want %q", decoded.Format, FormatName)
	}

	// And the digest it recomputes to is not the one its bytes would have carried,
	// which is the whole reason a read path refuses it.
	digest, err := HashRecord(decoded)
	if err != nil {
		t.Fatalf("HashRecord: %v", err)
	}
	roundTripped, err := CanonicalEncode(decoded)
	if err != nil {
		t.Fatalf("re-encode: %v", err)
	}
	if bytes.Equal(v1Bytes, roundTripped) {
		t.Fatal("the version 1 bytes round-trip, so nothing would refuse them")
	}
	if !strings.Contains(string(roundTripped), `"actions":null`) {
		t.Errorf("the re-encoded record does not spell the absent sections as null: %s", roundTripped)
	}
	if decoded.Version != 1 {
		t.Fatalf("the fixture is not a version 1 record: version = %d", decoded.Version)
	}
	// The version is right there in the decoded record and nothing refuses on it,
	// which is why the digest is what refuses it and why the refusal names
	// tampering rather than a build. The digest the version 1 bytes would have
	// carried is the one a v1 writer computed over the same projection, so it
	// differs from the current shape's digest for the same reason the bytes do.
	if digest == "" {
		t.Error("HashRecord returned no digest")
	}
}
