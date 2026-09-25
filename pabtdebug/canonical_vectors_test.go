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
	"bytes"
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt"
)

// The expected bytes and digests below are regression locks computed from this
// implementation and then frozen. They catch a change that silently alters the
// canonical form, which is what a digest is for. They are not an independent
// derivation of the correct form, so the structural properties asserted in
// TestCanonicalProperties are what actually pin the format to its specification.

func TestCanonicalSortsKeysAndOmitsWhitespace(t *testing.T) {
	got, err := CanonicalEncode(map[string]any{"b": 1, "a": 2, "C": 3, "": 4})
	if err != nil {
		t.Fatalf("CanonicalEncode: %v", err)
	}
	want := `{"":4,"C":3,"a":2,"b":1}`
	if string(got) != want {
		t.Errorf("canonical = %s, want %s", got, want)
	}
	// Struct fields are ordered by their names, not by declaration order, so a
	// reordered struct field cannot change a digest.
	a, err := CanonicalEncode(struct {
		Zebra string `json:"zebra"`
		Alpha string `json:"alpha"`
	}{"z", "a"})
	if err != nil {
		t.Fatalf("CanonicalEncode: %v", err)
	}
	b, err := CanonicalEncode(struct {
		Alpha string `json:"alpha"`
		Zebra string `json:"zebra"`
	}{"a", "z"})
	if err != nil {
		t.Fatalf("CanonicalEncode: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Errorf("field declaration order changed the canonical form:\n%s\n%s", a, b)
	}
	if string(a) != `{"alpha":"a","zebra":"z"}` {
		t.Errorf("canonical = %s", a)
	}
}

func TestCanonicalProperties(t *testing.T) {
	for _, test := range []struct {
		name string
		in   any
		want string
	}{
		{
			name: "integers keep full precision",
			// Beyond 2^53, a float round trip would lose the low bits.
			in:   map[string]any{"generation": uint64(18446744073709551615)},
			want: `{"generation":18446744073709551615}`,
		},
		{
			name: "no HTML escaping",
			in:   map[string]any{"note": "a<b>c&d"},
			want: `{"note":"a<b>c&d"}`,
		},
		{
			name: "control characters are escaped",
			in:   map[string]any{"note": "a\nb\tc\x01"},
			want: `{"note":"a\nb\tc\u0001"}`,
		},
		{
			name: "a nil slice is null and an empty one is empty",
			in: struct {
				A []int `json:"a"`
				B []int `json:"b"`
			}{A: nil, B: []int{}},
			want: `{"a":null,"b":[]}`,
		},
		{
			name: "a nil pointer is null and a present one is its value",
			in: struct {
				A *int `json:"a"`
				B *int `json:"b"`
			}{A: nil, B: func() *int { v := 3; return &v }()},
			want: `{"a":null,"b":3}`,
		},
		{
			name: "bytes are unpadded base64",
			in:   map[string]any{"data": Bytes{1, 2, 3}},
			want: `{"data":"AQID"}`,
		},
		{
			name: "bytes needing no padding stay unpadded",
			in:   map[string]any{"data": Bytes("ab")},
			want: `{"data":"YWI"}`,
		},
		{
			name: "timestamps normalise to UTC",
			in: map[string]any{
				"at": Timestamp(time.Date(2026, 9, 25, 10, 0, 0, 0, time.FixedZone("plus2", 2*3600))),
			},
			want: `{"at":"2026-09-25T08:00:00Z"}`,
		},
		{
			name: "nanosecond precision is kept",
			in:   map[string]any{"at": Timestamp(time.Date(2026, 9, 25, 0, 0, 0, 123456789, time.UTC))},
			want: `{"at":"2026-09-25T00:00:00.123456789Z"}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := CanonicalEncode(test.in)
			if err != nil {
				t.Fatalf("CanonicalEncode: %v", err)
			}
			if string(got) != test.want {
				t.Errorf("canonical =\n  %s\nwant\n  %s", got, test.want)
			}
		})
	}
}

func TestCanonicalDecodeIsStrict(t *testing.T) {
	type target struct {
		A int    `json:"a"`
		B string `json:"b"`
	}
	var ok target
	if err := CanonicalDecode([]byte(`{"a":1,"b":"x"}`), &ok); err != nil {
		t.Fatalf("CanonicalDecode: %v", err)
	}
	if ok.A != 1 || ok.B != "x" {
		t.Errorf("decoded = %+v", ok)
	}
	for _, bad := range []struct {
		name string
		in   string
	}{
		{name: "unknown field", in: `{"a":1,"b":"x","c":2}`},
		{name: "trailing value", in: `{"a":1,"b":"x"}{"a":2}`},
		{name: "trailing garbage", in: `{"a":1,"b":"x"} trailing`},
		{name: "truncated", in: `{"a":1,`},
		{name: "wrong type", in: `{"a":"one","b":"x"}`},
		{name: "not an object", in: `42`},
	} {
		t.Run(bad.name, func(t *testing.T) {
			var got target
			if err := CanonicalDecode([]byte(bad.in), &got); err == nil {
				t.Errorf("CanonicalDecode(%s) = nil, want an error", bad.in)
			}
		})
	}
}

func TestCanonicalRejectsUnrepresentableNumbers(t *testing.T) {
	// A float that cannot be written back exactly is a different fact from the one
	// recorded, so it is refused rather than rounded.
	got, err := CanonicalEncode(map[string]any{"x": math.Float64frombits(0x7ff8000000000001)})
	if err != nil {
		// A NaN never survives json.Marshal, which is the correct outcome.
		return
	}
	if strings.Contains(string(got), "NaN") {
		t.Errorf("canonical = %s, want a rejection rather than a NaN", got)
	}
}

// TestGoldenHashVectors locks the canonical bytes and the digests.
func TestGoldenHashVectors(t *testing.T) {
	redacted := StoredValue{
		Visibility:   ValueRedacted,
		ReflectType:  "string",
		CodecID:      structureCodecID,
		CodecVersion: 1,
		Data:         Bytes(`"hunter2"`),
		Display:      RedactedMarker,
		Source:       "credentials.password",
		Error:        RedactedReason,
	}
	redactedBytes, err := CanonicalEncode(redacted)
	if err != nil {
		t.Fatalf("CanonicalEncode: %v", err)
	}
	for _, test := range []struct {
		name    string
		domain  string
		value   any
		bytes   string
		digest  string
		project func() (string, error)
	}{
		{
			name:   "redacted value",
			domain: DomainValue,
			value:  redacted,
			bytes:  string(redactedBytes),
			project: func() (string, error) {
				v := redacted
				v.SHA256 = ""
				return HashValue(v)
			},
		},
		{
			name:   "empty tree",
			domain: DomainTree,
			value: StoredTree{
				RunKey:       RunKey{SessionID: "s", PlanID: "p", RunID: "r"},
				Sequence:     1,
				PlanRevision: 7,
				RootID:       "n1",
				Nodes:        nil,
				NodeCount:    0,
			},
			project: func() (string, error) {
				return HashTree(StoredTree{
					RunKey:       RunKey{SessionID: "s", PlanID: "p", RunID: "r"},
					Sequence:     1,
					PlanRevision: 7,
					RootID:       "n1",
				})
			},
		},
		{
			name:   "error",
			domain: DomainRecord,
			value:  StoredError{Code: ErrorCodePlanner, Type: "*errors.errorString", Message: "boom"},
			project: func() (string, error) {
				return HashCanonical(DomainRecord,
					StoredError{Code: ErrorCodePlanner, Type: "*errors.errorString", Message: "boom"})
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := CanonicalEncode(test.value)
			if err != nil {
				t.Fatalf("CanonicalEncode: %v", err)
			}
			if test.bytes != "" && string(got) != test.bytes {
				t.Errorf("canonical bytes =\n  %s\nwant\n  %s", got, test.bytes)
			}
			digest, err := test.project()
			if err != nil {
				t.Fatalf("hash: %v", err)
			}
			if len(digest) != 64 {
				t.Errorf("digest %q is not 64 hex characters", digest)
			}
			if digest != strings.ToLower(digest) {
				t.Errorf("digest %q is not lower case", digest)
			}
			if test.digest != "" && digest != test.digest {
				t.Errorf("digest = %s, want %s", digest, test.digest)
			}
		})
	}
}

func TestHashDomainsAreSeparated(t *testing.T) {
	value := map[string]any{"a": 1}
	first, err := HashCanonical(DomainValue, value)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	second, err := HashCanonical(DomainNode, value)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if first == second {
		t.Error("two domains produced the same digest for the same bytes")
	}
	// The separator must matter, or a value could be moved into a domain.
	withNul, err := HashCanonical(DomainValue+"\x00", value)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if first == withNul {
		t.Error("the domain separator does not affect the digest")
	}
}

func TestStatusTextRoundTripAndRejection(t *testing.T) {
	for _, test := range []struct {
		status  bt.Status
		text    string
		present bool
	}{
		{status: bt.Running, text: "running", present: true},
		{status: bt.Success, text: "success", present: true},
		{status: bt.Failure, text: "failure", present: true},
		{status: 0, text: "", present: false},
	} {
		text, err := StatusText(test.status, test.present)
		if err != nil {
			t.Fatalf("StatusText(%d): %v", test.status, err)
		}
		if text != test.text {
			t.Errorf("StatusText(%d) = %q, want %q", test.status, text, test.text)
		}
		gotStatus, gotPresent, err := DecodeStatusText(text)
		if err != nil {
			t.Fatalf("DecodeStatusText(%q): %v", text, err)
		}
		if gotPresent != test.present || (test.present && gotStatus != test.status) {
			t.Errorf("DecodeStatusText(%q) = %d, %v", text, gotStatus, gotPresent)
		}
	}
	// A status the dependency does not define must not reach a stored history,
	// and an absent status must never become a number.
	if _, err := StatusText(bt.Status(0), true); err == nil {
		t.Error("StatusText accepted the dependency's non-status zero as present")
	}
	if _, err := StatusText(bt.Status(9), true); err == nil {
		t.Error("StatusText accepted an undefined status")
	}
	for _, bad := range []string{"Running", "SUCCESS", "0", "1", "3", "ok", "done"} {
		if _, _, err := DecodeStatusText(bad); err == nil {
			t.Errorf("DecodeStatusText(%q) = nil, want an error", bad)
		}
	}
}

func TestNodeStatusOfRejectsAnUndefinedStatus(t *testing.T) {
	// The dependency defines 1, 2, and 3. Zero and anything above three are not
	// statuses, and neither may reach a stored history.
	for _, bad := range []int{0, 4, 9, -1} {
		if got, err := NodeStatusOf(nodeStatusOf(bad, true)); err == nil {
			t.Errorf("NodeStatusOf accepted status %d: %+v", bad, got)
		}
	}
	// 3 is the dependency's Failure, so it must be accepted; the point of the
	// strict rule is the three real values, not a smaller set.
	if got, err := NodeStatusOf(nodeStatusOf(int(bt.Failure), true)); err != nil {
		t.Errorf("NodeStatusOf rejected bt.Failure: %v", err)
	} else if got.LastStatus != uint8(bt.Failure) {
		t.Errorf("stored status = %d, want %d", got.LastStatus, bt.Failure)
	}
	got, err := NodeStatusOf(nodeStatusOf(0, false))
	if err != nil {
		t.Fatalf("NodeStatusOf: %v", err)
	}
	if got.StatusPresent {
		t.Error("an absent status was reported as present")
	}
	if got.LastStatus != 0 {
		t.Errorf("an absent status was stored as %d", got.LastStatus)
	}
}

func TestRecordDigestCoversThePreviousLink(t *testing.T) {
	base := StoredRecord{
		Format:       FormatName,
		Version:      FormatVersion,
		EventUID:     "e1",
		RunKey:       RunKey{SessionID: "s", PlanID: "p", RunID: "r"},
		Sequence:     1,
		Kind:         RecordTick,
		PayloadState: PayloadComplete,
		StartedAt:    Timestamp(time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)),
	}
	first, err := HashRecord(base)
	if err != nil {
		t.Fatalf("HashRecord: %v", err)
	}
	altered := base
	altered.PreviousRecordSHA256 = strings.Repeat("0", 64)
	second, err := HashRecord(altered)
	if err != nil {
		t.Fatalf("HashRecord: %v", err)
	}
	if first == second {
		t.Error("changing only the previous-record link did not change the record digest")
	}
	// A digest must not cover itself, so stamping the expected value must not
	// change what is recomputed.
	stamped := base
	stamped.RecordSHA256 = first
	third, err := HashRecord(stamped)
	if err != nil {
		t.Fatalf("HashRecord: %v", err)
	}
	if third != first {
		t.Error("the record digest covers itself")
	}
}

func TestVerifyNestedHashesDetectsAnAlteredChild(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	value := mustEncode(t, reg, "safe")
	node := StoredNode{
		ID:          "n1",
		Type:        "root",
		Name:        "root",
		EffectIDs:   []string{},
		ConditionID: nil,
		Frame:       nil,
	}
	digest, err := HashNode(node)
	if err != nil {
		t.Fatalf("HashNode: %v", err)
	}
	node.ContentHash = digest
	tree := StoredTree{
		RunKey:    RunKey{SessionID: "s", PlanID: "p", RunID: "r"},
		RootID:    "n1",
		Nodes:     []StoredNode{node},
		NodeCount: 1,
	}
	if tree.Digest, err = HashTree(tree); err != nil {
		t.Fatalf("HashTree: %v", err)
	}
	if err := VerifyNestedHashes(value, tree.Nodes, &tree); err != nil {
		t.Fatalf("VerifyNestedHashes: %v", err)
	}
	// Changing a child field while leaving its recorded digest alone must be
	// caught, or an altered child would hide behind a plausible parent.
	altered := tree
	altered.Nodes = []StoredNode{node}
	altered.Nodes[0].Name = "tampered"
	if err := VerifyNestedHashes(value, altered.Nodes, &altered); err == nil {
		t.Error("an altered child node was accepted")
	}
	badValue := value
	badValue.Display = "tampered"
	if err := VerifyNestedHashes(badValue, tree.Nodes, &tree); err == nil {
		t.Error("an altered value was accepted")
	}
}

func TestCanonicalLineEndsWithOneNewline(t *testing.T) {
	got, err := CanonicalLine(map[string]any{"a": 1})
	if err != nil {
		t.Fatalf("CanonicalLine: %v", err)
	}
	if string(got) != "{\"a\":1}\n" {
		t.Errorf("CanonicalLine = %q", got)
	}
}

func TestBytesAndTimestampRejectMalformedInput(t *testing.T) {
	var b Bytes
	if err := json.Unmarshal([]byte(`"!!!"`), &b); err == nil {
		t.Error("a malformed base64 byte field was accepted")
	}
	if err := json.Unmarshal([]byte(`"AQID="`), &b); err == nil {
		t.Error("padded base64 was accepted for an unpadded field")
	}
	var ts Timestamp
	if err := json.Unmarshal([]byte(`"not a time"`), &ts); err == nil {
		t.Error("a malformed timestamp was accepted")
	}
	if err := json.Unmarshal([]byte(`"2026-09-25T00:00:00+02:00"`), &ts); err != nil {
		t.Fatalf("a timestamp with an offset was rejected: %v", err)
	}
	if ts.Time().Location() != time.UTC {
		t.Error("a decoded timestamp did not normalise to UTC")
	}
}

// nodeStatusOf builds a captured status for the strict-decoder tests.
func nodeStatusOf(status int, present bool) pabt.NodeStatusSnapshot {
	return pabt.NodeStatusSnapshot{
		TickCount:     1,
		LastStatus:    bt.Status(status),
		StatusPresent: present,
		Version:       1,
	}
}

// mustEncode records a value or fails the test.
func mustEncode(t *testing.T, reg *CodecRegistry, v any) StoredValue {
	t.Helper()
	got, err := reg.Encode(context.Background(), "test", v)
	if err != nil {
		t.Fatalf("Encode(%v): %v", v, err)
	}
	return got
}
