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
	"encoding/json"
	"testing"
)

// The browser client reads these routes, and it reads them as bytes. Four of the
// types on the wire carry no JSON tags, so the member names are the Go field
// names - Visibility, CodecID, HeadSequence, HasMore - while every tagged type
// beside them is lowerCamel. A client written from the struct definitions rather
// than from the bytes gets that wrong silently: the member is simply absent, and
// the field reads as undefined.
//
// The agreement between the two halves used to rest on a one-time manual wire
// dump. This test is the dump, as an assertion. A tag added to StoredValue, or a
// field renamed on StoredRecord or RunHead, fails here rather than in a browser.

// TestTheWireSpellingIsWhatTheClientReads pins the member names for the types
// the client depends on. The client spells them out in fourteen places across
// pabtdebug/client/js/, and none of those places is type-checked against this
// package - a mismatch is a field that is quietly undefined at run time.
func TestTheWireSpellingIsWhatTheClientReads(t *testing.T) {
	// The exact spellings the client reads, gathered here rather than spread across
	// the module: any type below whose members stop matching fails this.
	cases := []struct {
		name    string
		value   any
		members []string
	}{
		{"StoredValue", StoredValue{
			Visibility: ValueRedacted, ReflectType: "string", CodecID: "string",
			Data: Bytes("x"), Display: "x", Source: "declared",
		}, []string{
			"Visibility", "ReflectType", "CodecID", "CodecVersion", "Data",
			"Display", "Source", "Error", "Redaction",
		}},
		{"RunHead", RunHead{
			RunKey: RunKey{SessionID: "s", RunID: "r"}, Generation: 1,
			HeadSequence: 2, HeadTreeSequence: 1, HeadTreeEventUID: "e",
			HeadTreeDigest: "d", HeadRecordSHA256: "h", HeadPlanRevision: 3,
		}, []string{
			"RunKey", "Generation", "HeadSequence", "HeadTreeSequence",
			"HeadTreeEventUID", "HeadTreeDigest", "HeadRecordSHA256",
			"HeadPlanRevision", "LastCommittedAt",
		}},
		{"RecordPage", RecordPage{
			Records: []StoredRecord{}, Through: 1, HasMore: true,
		}, []string{"Records", "Through", "HasMore"}},
		{"Installation", Installation{
			StoreID: "store", Generation: 1,
		}, []string{"StoreID", "Generation", "ActiveSessionID", "Recovery", "UpdatedAt"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw, err := json.Marshal(c.value)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var members map[string]json.RawMessage
			if err := json.Unmarshal(raw, &members); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			for _, want := range c.members {
				if _, ok := members[want]; !ok {
					t.Errorf("%s has no member %q; the wire spells %s", c.name, want, memberNames(members))
				}
			}
		})
	}

	// And the other direction: the tagged types must NOT be spelled in Go's
	// casing, because the client reads the lowerCamel form and a tag silently
	// added to one of them would satisfy the test above and break the client.
	raw, err := json.Marshal(StateResponse{StoreID: "s", Generation: 1})
	if err != nil {
		t.Fatalf("marshal StateResponse: %v", err)
	}
	var state map[string]json.RawMessage
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("unmarshal StateResponse: %v", err)
	}
	for _, want := range []string{"storeId", "generation", "activeSessionId", "run", "runState", "head"} {
		if _, ok := state[want]; !ok {
			t.Errorf("StateResponse has no member %q; the wire spells %s", want, memberNames(state))
		}
	}
	if _, ok := state["StoreID"]; ok {
		t.Error("StateResponse spells StoreID on the wire; the client reads storeId")
	}
}

func memberNames(m map[string]json.RawMessage) string {
	out := ""
	for k := range m {
		if out != "" {
			out += ", "
		}
		out += k
	}
	return "{" + out + "}"
}
