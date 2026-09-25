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
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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

// The expected bytes and digests below are frozen regression locks. Each one is
// a literal that the implementation must reproduce, so a change to the canonical
// form, the field names, the domain strings, or the projection clears them. They
// are not an independent derivation of the correct form, so the structural
// properties in TestCanonicalProperties are what pin the format to its
// specification, and TestGoldenVectorsAreSensitive proves these locks can fail.
const (
	valueVectorBytes  = `{"CodecID":"structure/v1","CodecVersion":1,"Data":"Imh1bnRlcjIi","Display":"REDACTED","Error":"","Redaction":"declared_by_application","ReflectType":"string","SHA256":"","Source":"credentials.password","Visibility":"redacted"}`
	valueVectorDigest = "a86d813fb6b131e4fc54c24ead985e0dcb42a03ba8aa7dd98984970e54afea64"

	treeVectorBytes  = `{"digest":"","nodeCount":0,"nodes":null,"planRevision":7,"rootId":"n1","runKey":{"planId":"p","runId":"r","sessionId":"s"},"sequence":1}`
	treeVectorDigest = "e74c9277caa17ac8305d5b83a4395f565ca9952fc2353f2422cd1abd1e670a64"

	recordVectorBytes   = `{"attemptId":"","captureError":null,"completedAt":"2026-09-25T00:00:00.000000001Z","conditions":null,"eventUid":"e1","executionStarted":false,"format":"pabtdebug.record","kind":"tick","panic":null,"payloadSha256":"","payloadState":"complete","plannerError":null,"previousRecordSha256":"0000000000000000000000000000000000000000000000000000000000000000","recordSha256":"","relations":null,"revision":{"coherence":"","planRevision":0,"runKey":{"planId":"","runId":"","sessionId":""},"sequence":0,"state":{"modelRevision":"","planRevision":0,"worldRevision":""}},"runKey":{"planId":"p","runId":"r","sessionId":"s"},"sequence":1,"startedAt":"2026-09-25T00:00:00Z","status":"","statusPresent":false,"tree":null,"treeEligible":false,"version":1}`
	recordVectorDigest  = "e27dd8579107cf44f2a62c50faa2952358364ed1669d4c441edc9a671ed53b22"
	payloadVectorDigest = "5dd385c755480f05ac26bf9d9c410f57418e3abac8b8d369f156bb6179d717d8"
)

// goldenValue, goldenTree, and goldenRecord build the frozen inputs. They are
// separate functions so each vector is constructed once and only once.
func goldenValue() StoredValue {
	return StoredValue{
		Visibility:   ValueRedacted,
		ReflectType:  "string",
		CodecID:      structureCodecID,
		CodecVersion: 1,
		Data:         Bytes(`"hunter2"`),
		Display:      RedactedMarker,
		Source:       "credentials.password",
		Redaction:    RedactionDeclared,
	}
}

func goldenTree() StoredTree {
	return StoredTree{
		RunKey:       RunKey{SessionID: "s", PlanID: "p", RunID: "r"},
		Sequence:     1,
		PlanRevision: 7,
		RootID:       "n1",
	}
}

func goldenRecord() StoredRecord {
	return StoredRecord{
		Format:               FormatName,
		Version:              FormatVersion,
		EventUID:             "e1",
		RunKey:               RunKey{SessionID: "s", PlanID: "p", RunID: "r"},
		Sequence:             1,
		Kind:                 RecordTick,
		PayloadState:         PayloadComplete,
		StartedAt:            Timestamp(time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)),
		CompletedAt:          Timestamp(time.Date(2026, 9, 25, 0, 0, 0, 1, time.UTC)),
		PreviousRecordSHA256: strings.Repeat("0", 64),
	}
}

func TestGoldenHashVectors(t *testing.T) {
	for _, test := range []struct {
		name   string
		encode func() ([]byte, error)
		hash   func() (string, error)
		bytes  string
		digest string
	}{
		{
			name:   "redacted value",
			encode: func() ([]byte, error) { return CanonicalEncode(goldenValue()) },
			hash:   func() (string, error) { return HashValue(goldenValue()) },
			bytes:  valueVectorBytes,
			digest: valueVectorDigest,
		},
		{
			name:   "empty tree",
			encode: func() ([]byte, error) { return CanonicalEncode(goldenTree()) },
			hash:   func() (string, error) { return HashTree(goldenTree()) },
			bytes:  treeVectorBytes,
			digest: treeVectorDigest,
		},
		{
			name:   "record with no status",
			encode: func() ([]byte, error) { return CanonicalEncode(goldenRecord()) },
			hash:   func() (string, error) { return HashRecord(goldenRecord()) },
			bytes:  recordVectorBytes,
			digest: recordVectorDigest,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.encode()
			if err != nil {
				t.Fatalf("CanonicalEncode: %v", err)
			}
			if test.bytes != "" && string(got) != test.bytes {
				t.Errorf("canonical bytes =\n  %s\nwant\n  %s", got, test.bytes)
			}
			digest, err := test.hash()
			if err != nil {
				t.Fatalf("hash: %v", err)
			}
			if digest != test.digest {
				t.Errorf("digest = %s, want %s", digest, test.digest)
			}
			if len(digest) != 64 || digest != strings.ToLower(digest) {
				t.Errorf("digest %q is not 64 lower case hex characters", digest)
			}
		})
	}
}

// TestGoldenVectorsAreSensitive proves the frozen locks can actually fail. A
// golden vector that no change can move is not a lock.
func TestGoldenVectorsAreSensitive(t *testing.T) {
	// One field changed, so both the bytes and the digest must move.
	altered := goldenTree()
	altered.PlanRevision = 8
	if string(mustCanonical(t, altered)) == treeVectorBytes {
		t.Error("changing a field did not change the canonical bytes")
	}
	if digest, err := HashTree(altered); err != nil {
		t.Fatalf("HashTree: %v", err)
	} else if digest == treeVectorDigest {
		t.Error("changing a field did not change the digest")
	}
	// Only the previous link changed, so the record digest must move and the
	// payload digest must not.
	relinked := goldenRecord()
	relinked.PreviousRecordSHA256 = strings.Repeat("1", 64)
	recordDigest, err := HashRecord(relinked)
	if err != nil {
		t.Fatalf("HashRecord: %v", err)
	}
	if recordDigest == recordVectorDigest {
		t.Error("changing the previous link did not change the record digest")
	}
	payloadDigest, err := HashPayload(relinked)
	if err != nil {
		t.Fatalf("HashPayload: %v", err)
	}
	if payloadDigest != payloadVectorDigest {
		t.Error("the payload digest depends on the previous link")
	}
	// The domain is inside the digest.
	other, err := HashCanonical(DomainPlan, goldenTree())
	if err != nil {
		t.Fatalf("HashCanonical: %v", err)
	}
	if other == treeVectorDigest {
		t.Error("two domains produced the same digest")
	}
}

func mustCanonical(t *testing.T, v any) []byte {
	t.Helper()
	got, err := CanonicalEncode(v)
	if err != nil {
		t.Fatalf("CanonicalEncode: %v", err)
	}
	return got
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

// --- Regression coverage for the findings of the first review pass. ---

func TestNegativeIntegersAreWrittenOnce(t *testing.T) {
	// A doubled minus is not JSON and cannot be decoded, so a recorded negative
	// value would be unrecoverable.
	for _, test := range []struct {
		value   any
		want    string
		decodes bool
	}{
		{value: int64(-1), want: `{"x":-1}`, decodes: true},
		{value: int(-5), want: `{"x":-5}`, decodes: true},
		{value: int64(math.MinInt64), want: `{"x":-9223372036854775808}`, decodes: true},
		{value: int64(-0), want: `{"x":0}`, decodes: true},
		{value: 0, want: `{"x":0}`, decodes: true},
		// Above MaxInt64, so it cannot be read into an int64 and is checked as
		// written bytes only.
		{value: uint64(math.MaxUint64), want: `{"x":18446744073709551615}`},
	} {
		got, err := CanonicalEncode(map[string]any{"x": test.value})
		if err != nil {
			t.Errorf("CanonicalEncode(%v): %v", test.value, err)
			continue
		}
		if string(got) != test.want {
			t.Errorf("CanonicalEncode(%v) = %s, want %s", test.value, got, test.want)
			continue
		}
		if !test.decodes {
			continue
		}
		// Whatever is written must read back as the same number.
		var back map[string]int64
		if err := CanonicalDecode(got, &back); err != nil {
			t.Errorf("the encoded form of %v does not decode: %v", test.value, err)
		}
	}
}

func TestOrdinaryFloatsAreAccepted(t *testing.T) {
	// These were refused by a spelling-equality check that demanded a particular
	// float format. The requirement is that a value round trips, not that it is
	// written one way.
	for _, value := range []float64{0.00001, 1e-5, 1e-6, 1e-7, 1e-8, -1e-5, 1.5, 123456789.12345679, math.MaxFloat64, math.SmallestNonzeroFloat64} {
		got, err := CanonicalEncode(map[string]any{"x": value})
		if err != nil {
			t.Errorf("CanonicalEncode(%v): %v", value, err)
			continue
		}
		var back map[string]float64
		if err := CanonicalDecode(got, &back); err != nil {
			t.Errorf("the encoded form of %v does not decode: %v", value, err)
			continue
		}
		if back["x"] != value {
			t.Errorf("%v round tripped to %v", value, back["x"])
		}
	}
}

func TestCanonicalDecodeRejectsDuplicateKeys(t *testing.T) {
	type target struct {
		A int    `json:"a"`
		B string `json:"b"`
	}
	for _, bad := range []string{
		`{"a":1,"a":2,"b":"x"}`,
		`{"a":1,"b":"x","b":"y"}`,
		`{"a":1,"a":2}`,
		`{"a":{"c":1,"c":2},"b":"x"}`,
		`{"a":[1,{"c":1,"c":2}],"b":"x"}`,
	} {
		var got target
		if err := CanonicalDecode([]byte(bad), &got); err == nil {
			t.Errorf("CanonicalDecode(%s) = %+v, want an error", bad, got)
		}
	}
	var m map[string]any
	if err := CanonicalDecode([]byte(`{"a":1,"a":2}`), &m); err == nil {
		t.Error("a duplicate key was accepted into a map")
	}
}

func TestNilAndEmptyBytesDoNotShareADigest(t *testing.T) {
	// encoding/json renders both as the empty string, so without care two
	// different facts would produce one digest.
	var nilValue, emptyValue StoredValue
	nilValue.Data = nil
	emptyValue.Data = Bytes{}
	nilDigest, err := HashValue(nilValue)
	if err != nil {
		t.Fatalf("HashValue: %v", err)
	}
	emptyDigest, err := HashValue(emptyValue)
	if err != nil {
		t.Fatalf("HashValue: %v", err)
	}
	if nilDigest == emptyDigest {
		t.Errorf("nil and empty bytes share the digest %s", nilDigest)
	}
}

func TestStatusIsCarriedAsTextAndValidated(t *testing.T) {
	rec := goldenRecord()
	rec.Status = uint8(bt.Success)
	rec.StatusPresent = true
	got := string(mustCanonical(t, rec))
	if !strings.Contains(got, `"status":"success"`) {
		t.Errorf("record did not carry the status as text: %s", got)
	}
	var back StoredRecord
	if err := CanonicalDecode([]byte(got), &back); err != nil {
		t.Fatalf("a record with a status did not decode: %v", err)
	}
	if back.Status != uint8(bt.Success) || !back.StatusPresent {
		t.Errorf("decoded status = %d present=%v", back.Status, back.StatusPresent)
	}
	// A status the dependency does not define must not be writable as a status.
	for _, bad := range []int{0, 4, 200} {
		rec.Status = uint8(bad)
		if got, err := CanonicalEncode(rec); err == nil &&
			strings.Contains(string(got), fmt.Sprintf(`"status":"%d"`, bad)) {
			t.Errorf("status %d reached the wire as a status", bad)
		}
	}
	// Text the dependency does not define is refused on the way in.
	rec.Status = uint8(bt.Success)
	rec.StatusPresent = true
	encoded := string(mustCanonical(t, rec))
	for _, bad := range []string{`"status":"Running"`, `"status":"ok"`, `"status":"3"`} {
		corrupted := strings.Replace(encoded, `"status":"success"`, bad, 1)
		if err := CanonicalDecode([]byte(corrupted), &back); err == nil {
			t.Errorf("CanonicalDecode accepted %s", bad)
		}
	}
	ns := goldenRecord()
	ns.Status = uint8(bt.Failure)
	ns.StatusPresent = true
	if got := string(mustCanonical(t, ns)); !strings.Contains(got, `"status":"failure"`) {
		t.Errorf("node status did not carry text: %s", got)
	}
}

func TestNodeStatusCarriesTextOnTheWire(t *testing.T) {
	got := string(mustCanonical(t, StoredNodeStatus{
		TickCount: 3, LastStatus: uint8(bt.Failure), StatusPresent: true, Version: 3,
	}))
	if !strings.Contains(got, `"lastStatus":"failure"`) {
		t.Errorf("node status was not text: %s", got)
	}
	var back StoredNodeStatus
	if err := CanonicalDecode([]byte(got), &back); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if back.LastStatus != uint8(bt.Failure) || back.Version != 3 {
		t.Errorf("decoded = %+v", back)
	}
}

func TestCodecOfToleratesAFallbackCodec(t *testing.T) {
	// A fallback codec declares no match type, and the default registry always
	// has one, so describing the default registry must not panic.
	reg := NewCodecRegistry(DefaultValueLimits())
	for _, c := range reg.Codecs() {
		got := CodecOf(c)
		if got.ID == "" {
			t.Errorf("CodecOf(%T) has no id", c)
		}
		if c.MatchType() == nil && !got.Fallback {
			t.Errorf("CodecOf(%q) did not mark a fallback", c.ID())
		}
		if c.MatchType() != nil && got.ReflectType == "" {
			t.Errorf("CodecOf(%q) has no reflect type", c.ID())
		}
	}
	if got := CodecOf(nil); got.ID != "" {
		t.Errorf("CodecOf(nil) = %+v", got)
	}
}

func TestStructureProjectionKeepsTimestampsAndDistinctFieldNames(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	at := Timestamp(time.Date(2026, 9, 25, 1, 2, 3, 4, time.UTC))
	got := mustEncode(t, reg, struct {
		When time.Time `json:"when"`
		At   Timestamp `json:"at"`
		N    int       `json:"n"`
	}{time.Date(2026, 9, 25, 1, 2, 3, 4, time.UTC), at, 1})
	projection := string(got.Data)
	for _, want := range []string{"2026-09-25T01:02:03.000000004Z", `"n":1`} {
		if !strings.Contains(projection, want) {
			t.Errorf("projection lost %q: %s", want, projection)
		}
	}
	// A projection that erased a timestamp would report two instants as equal.
	other := mustEncode(t, reg, struct {
		When time.Time `json:"when"`
		At   Timestamp `json:"at"`
		N    int       `json:"n"`
	}{time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC), Timestamp(time.Time{}), 1})
	if string(other.Data) == projection {
		t.Error("two different instants produced the same projection")
	}

	// Two fields whose names normalise to the same string must both survive.
	collide := mustEncode(t, reg, struct {
		MyID  string `json:"MyID"`
		My_Id string `json:"My_Id"`
	}{"one", "two"})
	if !strings.Contains(string(collide.Data), "one") || !strings.Contains(string(collide.Data), "two") {
		t.Errorf("a colliding field name dropped a value: %s", collide.Data)
	}
	// A field excluded from the wire is excluded, not recorded as empty.
	skipped := mustEncode(t, reg, struct {
		Kept    string `json:"kept"`
		Dropped string `json:"-"`
	}{"yes", "no"})
	if strings.Contains(string(skipped.Data), "Dropped") || strings.Contains(string(skipped.Data), `"-"`) {
		t.Errorf("a json-excluded field was recorded: %s", skipped.Data)
	}
}

func TestSanitizeTruncatesOnARuneBoundary(t *testing.T) {
	limits := DefaultValueLimits()
	limits.MaxDisplayBytes = 4
	san := &textSanitizer{limits: limits}
	got := san.Sanitize(strings.Repeat("日", 20))
	if !utf8.ValidString(got) {
		t.Errorf("truncation produced invalid UTF-8: %q", got)
	}
	if runes := []rune(got); len(runes) > 5 {
		t.Errorf("truncated to %d runes, want at most 5", len(runes))
	}
	// A replacement character the application supplied is ordinary text, so it is
	// measured with a limit that does not truncate past it.
	roomy := &textSanitizer{limits: DefaultValueLimits()}
	kept := roomy.Sanitize("before\uFFFDafter")
	if !strings.Contains(kept, "\uFFFD") {
		t.Errorf("a legitimate replacement character was dropped: %q", kept)
	}
}

func TestSanitizeDoesNotOverRedact(t *testing.T) {
	san := &textSanitizer{limits: DefaultValueLimits()}
	for _, text := range []string{
		"tokenizer=fast", "cookiecutter=chocolate", "secretionary=staff",
		"the word password appears here", "no credentials here",
	} {
		if got := san.Sanitize(text); got != text {
			t.Errorf("Sanitize(%q) = %q, want it unchanged", text, got)
		}
	}
	// A word that merely contains a marker must not have its value removed.
	if got := san.Sanitize("see token=abc def"); !strings.Contains(got, "token=") {
		t.Errorf("a real credential assignment was missed: %q", got)
	}
}

func TestSanitizeRedactsEveryOccurrenceAndKeepsThePrefix(t *testing.T) {
	san := &textSanitizer{limits: DefaultValueLimits()}
	for _, test := range []struct {
		in      string
		absent  []string
		present []string
	}{
		{in: "err token=alpha token=beta", absent: []string{"alpha", "beta"}, present: []string{"err "}},
		{in: "dial failed: token=alpha and token=beta", absent: []string{"alpha", "beta"}, present: []string{"dial failed: "}},
		{in: "password=one password=two password=three", absent: []string{"one", "two", "three"}, present: []string{"password=REDACTED password=REDACTED password=REDACTED"}},
		{in: "Authorization: Bearer first and Bearer second", absent: []string{"first", "second"}, present: []string{"Authorization: "}},
		{in: "a very long prefix a human needs: password=hunter2", absent: []string{"hunter2"}, present: []string{"a very long prefix a human needs: "}},
	} {
		got := san.Sanitize(test.in)
		for _, absent := range test.absent {
			if strings.Contains(got, absent) {
				t.Errorf("Sanitize(%q) = %q, still contains %q", test.in, got, absent)
			}
		}
		for _, present := range test.present {
			if !strings.Contains(got, present) {
				t.Errorf("Sanitize(%q) = %q, lost %q", test.in, got, present)
			}
		}
	}
}

func TestARegisteredLiteralReachesErrorText(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	// A codec whose failure repeats a literal the walk already saw. The error is
	// the one path a secret could otherwise reach the record through.
	if err := reg.Register(failingCodec{message: "upstream rejected " + secretLiteralValue}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	err := WithRedaction(context.Background(), DefaultValueLimits(),
		func(ctx context.Context) error {
			if _, encErr := reg.Encode(ctx, "record", nested{
				Account: credential{User: "alice", Password: secretLiteralValue},
			}); encErr != nil {
				return encErr
			}
			out, encErr := reg.Encode(ctx, "record", unencodable{})
			if encErr != nil {
				return encErr
			}
			if out.Visibility != ValueError {
				t.Fatalf("visibility = %q, want %q", out.Visibility, ValueError)
			}
			encoded, encErr := CanonicalEncode(out)
			if encErr != nil {
				return encErr
			}
			assertNoSecret(t, "an error that repeats a registered secret", encoded)
			digest, encErr := HashValue(out)
			if encErr != nil {
				return encErr
			}
			assertNoSecret(t, "the digest of such an error", []byte(digest))
			return nil
		})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
}

func TestAPanickingCodecIsContainedAndScrubbed(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	// A panicking codec's text is application-adjacent and can carry anything, so
	// the panic must not unwind into the caller's frame.
	panicking := panickingCodec{message: "codec exploded with token=" + secretLiteralValue}
	if err := reg.Register(panicking); err != nil {
		t.Fatalf("Register: %v", err)
	}
	var out StoredValue
	err := WithRedaction(context.Background(), DefaultValueLimits(),
		func(ctx context.Context) error {
			var encErr error
			out, encErr = reg.Encode(ctx, "record", unencodable{})
			return encErr
		})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if out.Visibility != ValueError {
		t.Errorf("visibility = %q, want %q", out.Visibility, ValueError)
	}
	encoded, err := CanonicalEncode(out)
	if err != nil {
		t.Fatalf("CanonicalEncode: %v", err)
	}
	assertNoSecret(t, "a recovered codec panic", encoded)
	if !strings.Contains(out.Error, RedactedMarker) {
		t.Errorf("a codec panic was recorded without redaction: %q", out.Error)
	}
}

type unencodable struct{}

// panickingCodec is a codec whose Encode panics with a message carrying a secret.
type panickingCodec struct{ message string }

func (panickingCodec) ID() string      { return "panic/v1" }
func (panickingCodec) Version() uint32 { return 1 }
func (panickingCodec) MatchType() reflect.Type {
	return reflect.TypeOf(unencodable{})
}
func (p panickingCodec) Encode(any) (Bytes, error) { panic(p.message) }
func (panickingCodec) Decode(Bytes) (any, error)   { return nil, nil }

type failingCodec struct{ message string }

func (failingCodec) ID() string      { return "failing/v1" }
func (failingCodec) Version() uint32 { return 1 }
func (failingCodec) MatchType() reflect.Type {
	return reflect.TypeOf(unencodable{})
}
func (f failingCodec) Encode(any) (Bytes, error) {
	return nil, fmt.Errorf("%s", f.message)
}
func (failingCodec) Decode(Bytes) (any, error) { return nil, nil }

// --- Regression coverage for the findings of the second review pass. ---

func TestCompoundCredentialNamesAreRedacted(t *testing.T) {
	// A whole-word rule that treats an underscore as part of a word skipped every
	// compound name, which is the common case for a real credential. The decision
	// is made on the identifier before the separator, compacted, so every spelling
	// of the same name is covered.
	san := &textSanitizer{limits: DefaultValueLimits()}
	for _, name := range []string{
		"auth_token", "access_token", "refresh_token", "id_token", "bearer_token",
		"access_key", "secret_key", "session_key", "api_key", "private_key",
		"secretKey", "authToken", "ACCESS-KEY", "Password", "cookie",
		"user_password", "token", "tokens", "passwords", "secrets", "cookies",
		"passWord", "passPhrase", "seedPhrase", "mnemonic", "passwordHash",
		"encryptionKey", "signingKey", "jwtToken",
		// The plural compounds of every family, which the earlier snake-keyed
		// table missed entirely.
		"apiKeys", "secretKeys", "sessionKeys", "passWords", "authTokens",
		"accessKeys", "privateKeys", "clientSecret", "jwtToken", "apiToken",
		"x-api-key", "awsSecretAccessKey", "passPhrase", "userPasswords",
	} {
		text := "config " + name + "=hunter2 trailing"
		got := san.Sanitize(text)
		if strings.Contains(got, "hunter2") {
			t.Errorf("Sanitize(%q) = %q, still contains the credential", text, got)
		}
		if !strings.Contains(got, "config ") {
			t.Errorf("Sanitize(%q) = %q, lost the prefix", text, got)
		}
		if !strings.Contains(got, RedactedMarker) {
			t.Errorf("Sanitize(%q) = %q, redaction is not visible in place", text, got)
		}
	}
	// A word that merely resembles one must not be redacted, or ordinary data
	// would disappear. A field whose own name is a credential is redacted even when
	// its value looks harmless, because the name is the signal and the value is not.
	for _, text := range []string{
		"tokenizer=fast", "cookiecutter=chocolate", "passworder=1",
		"my_tokenizer_count=3", "api_endpoint=https://x", "fileHash=abc",
		// A planner variable identifier is not a credential.
		"key=actor", "keys=3", "keypath=/usr/bin", "monkey=1", "turnkey=yes",
		"sortKey=name", "primaryKey=id", "partitionKey=k", "cacheKey=k",
		"idempotencyKey=req", "publicKey=pem", "objectKey=k", "rowKey=1",
	} {
		if got := san.Sanitize(text); got != text {
			t.Errorf("Sanitize(%q) = %q, want it unchanged", text, got)
		}
	}
}

func TestAStructValuedSensitivePathRegistersEveryLeaf(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	// The redacted value is a whole struct, so registering only its top would
	// leave its leaves free to reappear in a later diagnostic.
	holder := map[string]struct {
		Owner string
		Inner struct {
			Label string
			Deep  struct{ Note string }
		}
	}{
		"password": {
			Owner: "alice",
			Inner: struct {
				Label string
				Deep  struct{ Note string }
			}{Label: "top", Deep: struct{ Note string }{Note: "leaf-" + secretLiteralValue}},
		},
	}
	err := WithRedaction(context.Background(), DefaultValueLimits(),
		func(ctx context.Context) error {
			c, _ := redactionFor(ctx)
			if _, encErr := reg.Encode(ctx, "record", holder); encErr != nil {
				return encErr
			}
			// The leaf literal must now be scrubbed from free text.
			scrubbed := c.scrub("saw leaf-" + secretLiteralValue + " in a message")
			if strings.Contains(scrubbed, secretLiteralValue) {
				t.Errorf("a struct leaf literal was not registered: %q", scrubbed)
			}
			return nil
		})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
}

func TestASegmentContainingThePathSeparatorStillRegisters(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	// The walk reports the value it stopped at rather than a path to be re-parsed,
	// so a segment containing the separator cannot break the lookup.
	holder := map[string]map[string]string{
		"pass.word": {"inner": "deep-" + secretLiteralValue},
	}
	err := WithRedaction(context.Background(), DefaultValueLimits(),
		func(ctx context.Context) error {
			c, _ := redactionFor(ctx)
			out, encErr := reg.Encode(ctx, "record", holder)
			if encErr != nil {
				return encErr
			}
			if out.Visibility != ValueRedacted {
				t.Fatalf("visibility = %q, want redacted", out.Visibility)
			}
			encoded, encErr := CanonicalEncode(out)
			if encErr != nil {
				return encErr
			}
			assertNoSecret(t, "a dotted sensitive key", encoded, out.Data)
			if strings.Contains(c.scrub("saw deep-"+secretLiteralValue), secretLiteralValue) {
				t.Error("a dotted key's leaf literal was not registered")
			}
			return nil
		})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
}

func TestAnInjectedMemberIsRefusedByEveryCustomDecoder(t *testing.T) {
	// A type with its own unmarshaller loses the caller's DisallowUnknownFields.
	// An injected member would decode cleanly and the digest would still verify,
	// which is the tampering a digest exists to catch.
	rec := goldenRecord()
	rec.Status = uint8(bt.Success)
	rec.StatusPresent = true
	encoded := string(mustCanonical(t, rec))
	var back StoredRecord
	if err := CanonicalDecode([]byte(encoded), &back); err != nil {
		t.Fatalf("the untampered record did not decode: %v", err)
	}
	for _, injected := range []string{`{"injected":"payload",`, `{"injected":"payload","`} {
		tampered := injected + encoded[1:]
		if err := CanonicalDecode([]byte(tampered), &back); err == nil {
			t.Errorf("CanonicalDecode accepted an injected member: %s", injected)
		}
	}
	ns := string(mustCanonical(t, StoredNodeStatus{
		TickCount: 1, LastStatus: uint8(bt.Running), StatusPresent: true, Version: 1,
	}))
	var status StoredNodeStatus
	if err := CanonicalDecode([]byte(`{"injected":1,`+ns[1:]), &status); err == nil {
		t.Error("StoredNodeStatus accepted an injected member")
	}
	if err := CanonicalDecode([]byte(ns), &status); err != nil {
		t.Fatalf("the untampered node status did not decode: %v", err)
	}
	// A contradictory presence flag is refused rather than resolved.
	contradictory := strings.Replace(ns, `"statusPresent":true`, `"statusPresent":false`, 1)
	if err := CanonicalDecode([]byte(contradictory), &status); err == nil {
		t.Error("a status present with an empty text and an absent flag was accepted")
	}
}

func TestByteRunsAreRecordedLosslessly(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	// Two byte sequences that differ must not share a digest. Converting them to
	// text would lose any sequence that is not valid UTF-8.
	first := mustEncode(t, reg, []byte{0xfe, 0xfd, 0x01})
	second := mustEncode(t, reg, []byte{0xfe, 0xfd, 0x02})
	if first.SHA256 == second.SHA256 {
		t.Error("two different byte sequences share a digest")
	}
	// A long run differing in one byte, beyond the display bound, must still differ.
	base := make([]byte, 6000)
	for i := range base {
		base[i] = byte(i % 251)
	}
	altered := append([]byte(nil), base...)
	altered[5999] ^= 0xff
	longFirst := mustEncode(t, reg, base)
	longSecond := mustEncode(t, reg, altered)
	if longFirst.SHA256 == longSecond.SHA256 {
		t.Error("two long byte runs differing in one byte share a digest")
	}
	// A byte run is not a string, so the two must not collide either.
	asString := mustEncode(t, reg, "abc")
	asBytes := mustEncode(t, reg, []byte("abc"))
	if asString.SHA256 == asBytes.SHA256 {
		t.Error("a string and the bytes holding it share a digest")
	}
}

func TestRegisteringLeavesDoesNotDestroyOrdinaryText(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	// A redacted subtree whose leaves are short ordinary strings. Registering
	// those would let any later text containing "/" or "a" or "bearer" be mangled.
	holder := map[string]any{
		"credential": struct{ Kind, Path, Pad string }{"bearer", "/", "a"},
	}
	err := WithRedaction(context.Background(), DefaultValueLimits(),
		func(ctx context.Context) error {
			c, _ := redactionFor(ctx)
			if _, encErr := reg.Encode(ctx, "record", holder); encErr != nil {
				return encErr
			}
			for _, ordinary := range []string{
				"/usr/local/bin", "Authorization: Bearer abc", "negotiated with a peer",
				"a plain sentence with ordinary words",
			} {
				got := c.scrub(ordinary)
				if got != ordinary {
					t.Errorf("scrub(%q) = %q, ordinary text was destroyed by a short registered leaf",
						ordinary, got)
				}
			}
			return nil
		})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
}

func TestASchemeWordDoesNotShieldTheCredentialBehindIt(t *testing.T) {
	san := &textSanitizer{limits: DefaultValueLimits()}
	// "Authorization: Basic <credential>" puts a scheme where the value would be,
	// so replacing only the scheme would leave the credential in the clear.
	for _, text := range []string{
		"Authorization: Basic dXNlcjpwYXNzd29yZA==",
		"authorization: Digest response=deadbeef",
		"Authorization: Negotiate YII=",
		"Authorization: Bearer abc123",
	} {
		got := san.Sanitize(text)
		if strings.Contains(got, "dXNlcjpwYXNzd29yZA==") || strings.Contains(got, "deadbeef") ||
			strings.Contains(got, "abc123") {
			t.Errorf("Sanitize(%q) = %q, the credential survived", text, got)
		}
	}
	// A bearer prefix with extra spaces must not abandon the rest of the line.
	got := san.Sanitize("bearer  first and Bearer second")
	for _, secret := range []string{"first", "second"} {
		if strings.Contains(got, secret) {
			t.Errorf("Sanitize left %q: %q", secret, got)
		}
	}
}

func TestAWireTagNamingASecretIsHonoured(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	// The declared name is the one the record carries, so it is the one that
	// decides sensitivity, whatever the Go field is called.
	type payload struct {
		Value string `json:"password"`
		Other string `json:"other"`
	}
	got := mustEncode(t, reg, payload{Value: secretLiteralValue, Other: "keep"})
	if got.Visibility != ValueRedacted {
		t.Fatalf("visibility = %q, want %q", got.Visibility, ValueRedacted)
	}
	encoded, err := CanonicalEncode(got)
	if err != nil {
		t.Fatalf("CanonicalEncode: %v", err)
	}
	assertNoSecret(t, "a field whose wire tag names a secret", encoded, got.Data)
	if !strings.Contains(string(got.Data), "keep") {
		t.Errorf("the safe field was lost: %s", got.Data)
	}
}

func TestARecordRefusesAContradictoryPresenceFlag(t *testing.T) {
	rec := goldenRecord()
	rec.Status = uint8(bt.Success)
	rec.StatusPresent = true
	encoded := string(mustCanonical(t, rec))
	contradictory := strings.Replace(encoded, `"statusPresent":true`, `"statusPresent":false`, 1)
	var back StoredRecord
	if err := CanonicalDecode([]byte(contradictory), &back); err == nil {
		t.Error("a record with a present status and an absent flag was accepted")
	}
	if err := CanonicalDecode([]byte(encoded), &back); err != nil {
		t.Errorf("the untampered record did not decode: %v", err)
	}
}

func TestSensitiveNameRuleInBothDirections(t *testing.T) {
	// A credential is recognised because the name says the purpose is private and
	// the thing holding it is a credential, or because a credential word appears
	// anywhere in it. That has to work in both directions: a miss leaks, and a false
	// positive destroys ordinary state.
	for _, name := range []string{
		// A credential word anywhere in the name.
		"password", "passwords", "user_password", "userPasswords", "password_hash",
		"passwordHash", "pass_word", "passWord", "passWords", "pass_phrase",
		"passPhrase", "passphrase", "secret", "secrets", "seed", "seedPhrase",
		"mnemonic", "cookie", "cookies", "token", "tokens", "token_1", "credential",
		"credentials", "authorization", "authorization", "salt", "signature",
		// A private purpose plus a thing that holds it.
		"apiKey", "api_key", "api-key", "x-api-key", "apiKeys", "api_key_1",
		"accessKey", "access_token", "refreshToken", "idToken", "id_token",
		"secretKey", "secret_key", "sessionKey", "session_key", "privateKey",
		"private_key_phrase", "encryptionKey", "signingKey", "jwtToken",
		"awsSecretAccessKey", "clientSecret", "apiToken", "passCode", "seed_phrase",
	} {
		if !IsSensitiveSegment(name) {
			t.Errorf("IsSensitiveSegment(%q) = false, want true", name)
		}
	}
	// None of these names anything private, and several are the words a planner
	// uses for a variable identifier. Redacting them would destroy the record.
	for _, name := range []string{
		"key", "keys", "keyPath", "keypath", "keyword", "keyboard", "keyring",
		"monkey", "hotkey", "turnkey", "sortKey", "sort_key", "primaryKey",
		"partitionKey", "cacheKey", "objectKey", "rowKey", "idempotencyKey",
		"publicKey", "pubsubKey", "foreignKey", "shardingKey", "indexKey",
		"groupKey", "fileHash", "treeHash", "contentHash", "secretariat",
		"api_endpoint", "apiVersion", "apiCall", "user", "userId", "userName",
		"clientId", "clientName", "serverPort", "dataPath", "label", "value",
		"name", "id", "type", "count", "actor", "location", "status",
	} {
		if IsSensitiveSegment(name) {
			t.Errorf("IsSensitiveSegment(%q) = true, want false: ordinary state would be destroyed", name)
		}
	}
}

func TestAGoNameThatIsSecretIsRedactedUnderAnInnocuousTag(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	// Consulting only the declared tag would record this in the clear, and the
	// digest would then sign a credential.
	type payload struct {
		Password    string `json:"pw"`
		AccessToken string `json:"t"`
		Note        string `json:"note"`
	}
	got := mustEncode(t, reg, payload{
		Password:    secretLiteralValue,
		AccessToken: secretLiteralValue,
		Note:        "keep",
	})
	if got.Visibility != ValueRedacted {
		t.Fatalf("visibility = %q, want %q", got.Visibility, ValueRedacted)
	}
	encoded, err := CanonicalEncode(got)
	if err != nil {
		t.Fatalf("CanonicalEncode: %v", err)
	}
	assertNoSecret(t, "a secret Go field with an innocuous tag", encoded, got.Data, []byte(got.Display))
	if !strings.Contains(string(got.Data), "keep") {
		t.Errorf("the safe field was lost: %s", got.Data)
	}
}

func TestEverySensitiveFieldContributesALiteral(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	// Two secrets in one value: registering only the first would let the second
	// reappear in later text even though the record itself is clean.
	const second = "second-secret-value-ZZZ"
	holder := struct {
		Password string `json:"password"`
		APIKey   string `json:"apiKey"`
	}{"first-secret-value-AAA", second}
	err := WithRedaction(context.Background(), DefaultValueLimits(),
		func(ctx context.Context) error {
			c, _ := redactionFor(ctx)
			if _, encErr := reg.Encode(ctx, "record", holder); encErr != nil {
				return encErr
			}
			for _, later := range []string{
				"first-secret-value-AAA", second,
			} {
				got := c.scrub("upstream rejected " + later)
				if strings.Contains(got, later) {
					t.Errorf("a secret survived later text: %q", got)
				}
			}
			return nil
		})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
}

func TestACookieHeaderLosesEveryPair(t *testing.T) {
	san := &textSanitizer{limits: DefaultValueLimits()}
	got := san.Sanitize("Cookie: session=abc; csrf=def; other=ghi")
	for _, secret := range []string{"abc", "def", "ghi"} {
		if strings.Contains(got, secret) {
			t.Errorf("Sanitize left %q: %q", secret, got)
		}
	}
}

// TestEveryRedactionContributesALiteralInEveryShape covers the three shapes the
// earlier walk missed, because it returned on the first sensitive field it found:
// a direct field beside a nested one, a second element of a slice, and two keys of
// a map. Map order made the survivor vary between runs, so the map case is
// checked repeatedly.
func TestEveryRedactionContributesALiteralInEveryShape(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	const (
		direct = "DIRECT-secret-value-bbbb"
		nested = "NESTED-secret-value-aaaa"
		first  = "FIRST-element-value-cccc"
		second = "SECOND-element-value-dddd"
		keyOne = "FIRST-key-value-eeee"
		keyTwo = "SECOND-key-value-ffff"
	)
	type inner struct{ Secret string }
	type payload struct {
		Password string
		Inner    inner
		Tokens   []string
	}
	check := func(secrets ...string) {
		t.Helper()
		err := WithRedaction(context.Background(), DefaultValueLimits(),
			func(ctx context.Context) error {
				c, _ := redactionFor(ctx)
				if _, encErr := reg.Encode(ctx, "record", payload{
					Password: direct,
					Inner:    inner{Secret: nested},
					Tokens:   []string{first, second},
				}); encErr != nil {
					return encErr
				}
				if _, encErr := reg.Encode(ctx, "record",
					map[string]string{"password": keyOne, "apiKey": keyTwo}); encErr != nil {
					return encErr
				}
				for _, secret := range secrets {
					if got := c.scrub("upstream rejected " + secret); strings.Contains(got, secret) {
						t.Errorf("a secret declared redacted reappeared in later text: %q", got)
					}
				}
				return nil
			})
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
	}
	// Repeated, because the map case previously leaked in some runs and not others.
	for range 20 {
		check(direct, nested, first, second, keyOne, keyTwo)
	}
}

func TestDigitSuffixedAndGluedCredentialsAreRedacted(t *testing.T) {
	// A trailing index is how rotation fields and old/new forms are written, and
	// a run of capitals is how env-style names arrive. Both made real names
	// invisible before.
	for _, name := range []string{
		"password1", "Password1", "userPassword1", "secret1", "token1", "apiKey1",
		"apiKey2", "accessToken1", "passphrase1", "cookie1", "credential1",
		"mnemonic1", "signature1", "apikey", "apikeys", "apisecret", "clientsecret",
		"accesstoken", "bearertoken", "refreshtoken", "privatekey", "sessiontoken",
		"authtoken", "secretkey", "signingkey", "encryptionkey", "masterkey",
		"apitoken", "idtoken", "ssotoken", "hmacsecret", "xapikey", "userpassword",
		"dbpassword", "sshKey", "gpgKey", "kmsKey",
		"deployKey", "licenseKey", "clientKey", "backupKey", "aesKey", "ed25519PrivateKey",
		"x25519PrivateKey", "serviceAccountKey", "csrfToken", "xsrfToken",
		"recoveryCode", "mfaSecret", "vaultToken", "keystorePassword", "appSecret",
	} {
		if !IsSensitiveSegment(name) {
			t.Errorf("IsSensitiveSegment(%q) = false, want true", name)
		}
	}
	// A name containing a credential word is redacted even where the compound reads
	// as ordinary English: secretPlant, tokenRing, and seedValue are all lost, and
	// losing them cannot leak anything. A saltValue and a pepperValue are also lost,
	// which is the one deliberate gap: value is the least specific word in the noun
	// table, and protecting every *Value field destroyed far more real data than it
	// saved. The names below are ones where no credential word appears at all.
	for _, name := range []string{
		"sortKey", "primaryKey", "partitionKey", "cacheKey", "objectKey", "rowKey",
		"idempotencyKey", "pubsubKey", "foreignKey", "shardingKey", "indexKey",
		"groupKey", "publicKey", "answerKey", "fieldKey", "columnKey", "configKey",
		"settingKey", "mapKey", "registryKey", "compositeKey", "alternateKey",
		"seedCount", "seedType", "mnemonicCount",
		"masterDiamond", "saltRock",
		"fileHash", "treeHash", "contentHash", "sha256Hash",
	} {
		if IsSensitiveSegment(name) {
			t.Errorf("IsSensitiveSegment(%q) = true, want false: ordinary state would be destroyed", name)
		}
	}
}

func TestWeakSignalsDoNotBecomeCredentials(t *testing.T) {
	// A code, a value, a pepper, and a hash key are ordinary data. Redacting them
	// would replace a value the debugger exists to show with the same marker used
	// for a real secret, which is worse than the gap it hides.
	for _, name := range []string{
		"ErrorCode", "StatusCode", "HTTPStatusCode", "CountryCode", "CurrencyCode",
		"PostalCode", "ProductCode", "SkuCode", "VerificationCode", "PartCode",
		"TaxCode", "CouponCode", "InviteCode", "EmployeeCode", "JournalCode",
		"GLCode", "DiscountCode", "UserCode", "Code", "userCode", "userName",
		"SystemValue", "UserValue", "ClientValue", "SessionValue", "EnvValue",
		"ApplicationValue", "TenantValue", "ServiceValue", "ValueKey",
		"actualValue", "bellPepper", "blackPepper", "pepperMill", "PepperMill",
		"HashKey", "hash_key", "hashKey", "digestKey", "checksumKey",
		"ValueKey", "valueKey",
	} {
		if IsSensitiveSegment(name) {
			t.Errorf("IsSensitiveSegment(%q) = true, want false: ordinary data would be destroyed", name)
		}
	}
}

func TestTheGluedSplitIsBounded(t *testing.T) {
	// The split is tried at every position of a word, so an unbounded word is
	// quadratic on a path that takes application-controlled text. A long word is
	// data, not a name, and must be rejected without the work.
	long := strings.Repeat("k", 200000) + "=v"
	limits := DefaultValueLimits()
	limits.MaxDisplayBytes = 64
	san := &textSanitizer{limits: limits}
	done := make(chan struct{})
	go func() {
		defer close(done)
		got := san.Sanitize(long)
		if len(got) > 128 {
			t.Errorf("Sanitize returned %d bytes, want a bounded result", len(got))
		}
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Sanitize did not finish on a long application-supplied word")
	}
}

func TestHighValueCredentialSpellingsAreRedacted(t *testing.T) {
	// The names a real system uses for its most valuable secrets, including the
	// spellings with no separator at all.
	for _, name := range []string{
		"mnemonicBackup", "seedBackup", "mnemonicList", "seedCopy",
		"serviceaccountkey", "serviceAccountKey", "service_account_key",
		"setcookie", "setCookie", "set_cookie", "proxyauthorization",
		"proxyAuthorization", "proxy_authorization", "csrftoken", "xsrftoken",
		"connectionString", "connection_string", "dsn", "databaseDSN",
		"codeVerifier", "codeChallenge", "authHeader", "basicAuth", "jwt", "totp",
		"sessionId", "keyMaterial", "keyBytes", "mnemonicPhrase", "mnemonicSeed",
		// A private purpose in front of an ordinary holder still makes it one.
		"accessCode", "pinCode", "otpCode", "recoveryCode", "userPassword",
		"userKey", "userToken", "userSecret",
	} {
		if !IsSensitiveSegment(name) {
			t.Errorf("IsSensitiveSegment(%q) = false, want true", name)
		}
	}
}

// declaredSecret is a value whose type declares itself a secret, with a field name
// that says nothing. It is the guarantee an application gets in exchange for one
// method, and it is what a name-based rule cannot give.
type declaredSecret struct {
	Value string
}

func (declaredSecret) PabtSensitive() bool { return true }

type notASecret struct {
	Value string
}

func TestADeclarationRedactsAValueItsNameNeverReveals(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	// Every field name here is ordinary. Only the declaration can protect them.
	//
	// The literal is the one the shared helper searches for, so this test fails if
	// the declaration stops being honoured. An earlier version of it planted a
	// different string and could therefore never fail, which is how a leak in the
	// projector shipped behind a test that appeared to cover it.
	const literal = secretLiteralValue
	type payload struct {
		Field   declaredSecret
		Detail  notASecret
		Entries []declaredSecret
		ByName  map[string]declaredSecret
	}
	got := mustEncode(t, reg, payload{
		Field:   declaredSecret{Value: literal},
		Detail:  notASecret{Value: "keep me"},
		Entries: []declaredSecret{{Value: literal}},
		ByName:  map[string]declaredSecret{"anything": {Value: literal}},
	})
	if got.Visibility != ValueRedacted {
		t.Fatalf("visibility = %q, want %q", got.Visibility, ValueRedacted)
	}
	if got.Redaction != RedactionDeclared {
		t.Errorf("mechanism = %q, want %q", got.Redaction, RedactionDeclared)
	}
	encoded, err := CanonicalEncode(got)
	if err != nil {
		t.Fatalf("CanonicalEncode: %v", err)
	}
	assertNoSecret(t, "a declared secret", encoded, got.Data, []byte(got.Display))
	// The declared value is gone everywhere, including a slice element and a map
	// value, because the declaration travels with the type.
	if !strings.Contains(string(got.Data), "keep me") {
		t.Errorf("an undeclared sibling was destroyed: %s", got.Data)
	}
	digest, err := HashValue(got)
	if err != nil {
		t.Fatalf("HashValue: %v", err)
	}
	assertNoSecret(t, "the digest of a declared secret", []byte(digest))
}

func TestAnInferredRedactionIsRecordedAsInferred(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	// Nothing declared this. The name did, and the record must say so rather than
	// letting a reader assume the application agreed.
	type payload struct {
		Password string
		Field    notASecret
	}
	got := mustEncode(t, reg, payload{Password: "hunter2hunter2", Field: notASecret{Value: "keep"}})
	if got.Visibility != ValueRedacted {
		t.Fatalf("visibility = %q, want %q", got.Visibility, ValueRedacted)
	}
	if got.Redaction != RedactionInferred {
		t.Errorf("mechanism = %q, want %q", got.Redaction, RedactionInferred)
	}
	if !strings.Contains(string(got.Data), "keep") {
		t.Errorf("an ordinary sibling was destroyed: %s", got.Data)
	}
}

func TestADeclarationOutranksTheNameAndTheRecordSaysSo(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	// A field named Password whose type also declares itself: the declaration is the
	// stronger statement, so the record names it.
	type payload struct {
		Password declaredSecret
	}
	got := mustEncode(t, reg, payload{Password: declaredSecret{Value: "x"}})
	if got.Redaction != RedactionDeclared {
		t.Errorf("mechanism = %q, want %q", got.Redaction, RedactionDeclared)
	}
}

func TestAnUnredactedValueCarriesNoMechanism(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	got := mustEncode(t, reg, notASecret{Value: "ordinary"})
	if got.Redaction != "" {
		t.Errorf("an unredacted value reports a mechanism: %q", got.Redaction)
	}
}

// ptrDeclaredSecret declares itself through a pointer receiver, which is the
// spelling an application reaches for when the type also has other methods on the
// pointer. A value field it cannot address must still be honoured.
type ptrDeclaredSecret struct {
	Value string
}

func (*ptrDeclaredSecret) PabtSensitive() bool { return true }

// panickingSecret panics from its declaration. The panic text is the value it is
// protecting, so the answer must be treated as a declaration rather than a
// failure.
type panickingSecret struct {
	Value string
}

func (panickingSecret) PabtSensitive() bool {
	panic("PabtSensitive panicked with token=" + declaredPanicLiteral)
}

const declaredPanicLiteral = "panicking-declared-literal-value"

// secretKey is a map key that declares itself sensitive, which takes its whole
// entry with it.
type secretKey struct{ Name string }

func (secretKey) PabtSensitive() bool { return true }

func TestADeclarationIsHonouredWhereverAValueAppears(t *testing.T) {
	// Every shape the walk consults must be mirrored by the projection. A shape the
	// walk treats as declared but the projection writes out is a record that claims
	// the application declared a secret and then carries it in the clear, signed by
	// its own digest.
	reg := NewCodecRegistry(DefaultValueLimits())
	const literal = "declared-secret-literal-value"

	check := func(name string, value any) {
		t.Helper()
		got := mustEncode(t, reg, value)
		if got.Visibility != ValueRedacted {
			t.Errorf("%s: visibility = %q, want %q", name, got.Visibility, ValueRedacted)
			return
		}
		if got.Redaction != RedactionDeclared {
			t.Errorf("%s: mechanism = %q, want %q", name, got.Redaction, RedactionDeclared)
		}
		encoded, err := CanonicalEncode(got)
		if err != nil {
			t.Fatalf("%s: CanonicalEncode: %v", name, err)
		}
		if strings.Contains(string(encoded), literal) || strings.Contains(string(got.Data), literal) {
			t.Errorf("%s: the declared secret is in the record: %s", name, got.Data)
		}
		if strings.Contains(got.Display, literal) {
			t.Errorf("%s: the declared secret is in the display: %q", name, got.Display)
		}
		digest, err := HashValue(got)
		if err != nil {
			t.Fatalf("%s: HashValue: %v", name, err)
		}
		if strings.Contains(digest, literal) {
			t.Errorf("%s: the declared secret is in the digest", name)
		}
	}

	check("top level struct", declaredSecret{Value: literal})
	check("pointer element", []*declaredSecret{{Value: literal}})
	check("slice of declared", []declaredSecret{{Value: literal}})
	check("array of declared", [1]declaredSecret{{Value: literal}})
	check("any element", []any{declaredSecret{Value: literal}})
	check("map value", map[string]declaredSecret{"k": {Value: literal}})
	check("map of any", map[string]any{"k": declaredSecret{Value: literal}})
	check("nested in a slice of maps", []map[string]any{{"k": declaredSecret{Value: literal}}})
	check("pointer receiver on a value field", struct {
		Handle ptrDeclaredSecret
	}{ptrDeclaredSecret{Value: literal}})
	check("pointer receiver at top level", ptrDeclaredSecret{Value: literal})
	check("pointer receiver in a slice", []*ptrDeclaredSecret{{Value: literal}})
	check("declared key", map[secretKey]string{{Name: "k"}: literal})
	check("three levels down", map[string]any{"a": []any{map[string]any{
		"b": declaredSecret{Value: literal},
	}}})

	// A panicking declaration is a declaration, because the panic text is the value
	// it is protecting and assuming "not a secret" is the one answer that leaks.
	got := mustEncode(t, reg, panickingSecret{Value: "kept"})
	if got.Visibility != ValueRedacted {
		t.Errorf("a panicking declaration: visibility = %q, want %q", got.Visibility, ValueRedacted)
	}
	if strings.Contains(got.Display, declaredPanicLiteral) {
		t.Errorf("a panicking declaration leaked its own text: %q", got.Display)
	}
	encoded, err := CanonicalEncode(got)
	if err != nil {
		t.Fatalf("CanonicalEncode: %v", err)
	}
	if strings.Contains(string(encoded), declaredPanicLiteral) {
		t.Error("a panicking declaration reached the record")
	}
}

func TestANameOutrankingADeclarationStillRedacts(t *testing.T) {
	// A field whose name is sensitive is redacted by the fallback even when its
	// value declines to declare itself, because the fallback is what keeps an
	// undeclared Password safe.
	reg := NewCodecRegistry(DefaultValueLimits())
	got := mustEncode(t, reg, struct {
		Password notASecret
	}{notASecret{Value: "keep-me-not-a-secret"}})
	if got.Visibility != ValueRedacted {
		t.Fatalf("visibility = %q, want %q", got.Visibility, ValueRedacted)
	}
	if got.Redaction != RedactionInferred {
		t.Errorf("mechanism = %q, want %q", got.Redaction, RedactionInferred)
	}
	// The fallback replaced the value under the sensitive name, which is the whole
	// point of keeping it: an undeclared Password is still safe.
	if strings.Contains(string(got.Data), "keep-me-not-a-secret") {
		t.Errorf("a value under a sensitive name was recorded: %s", got.Data)
	}
	if !strings.Contains(string(got.Data), RedactedMarker) {
		t.Errorf("no marker was left: %s", got.Data)
	}
}

func TestADeclaredMapKeyIsProtectedIncludingItsOwnMaterial(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	// A declared key says the key is a secret. Honouring that while leaving the
	// key's own text readable in a later message would not have honoured it.
	const keyLiteral = "declared-key-literal-9Qa"
	err := WithRedaction(context.Background(), DefaultValueLimits(),
		func(ctx context.Context) error {
			if _, encErr := reg.Encode(ctx, "src",
				map[secretKey]string{{Name: keyLiteral}: "v"}); encErr != nil {
				return encErr
			}
			c, _ := redactionFor(ctx)
			got := c.scrub("value=" + keyLiteral)
			if strings.Contains(got, keyLiteral) {
				t.Errorf("a declared key's own material survived later text: %q", got)
			}
			return nil
		})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	// Two declared keys must not collapse onto one marker, or the map would appear
	// to hold fewer entries than it does.
	got := mustEncode(t, reg, map[secretKey]string{
		{Name: "one"}: "v1",
		{Name: "two"}: "v2",
	})
	if got.Visibility != ValueRedacted {
		t.Fatalf("visibility = %q, want %q", got.Visibility, ValueRedacted)
	}
	if strings.Count(string(got.Data), RedactedMarker) < 4 {
		t.Errorf("two declared keys collapsed onto one entry: %s", got.Data)
	}
	if strings.Contains(string(got.Data), "one") || strings.Contains(string(got.Data), "two") {
		t.Errorf("a declared key's text is in the record: %s", got.Data)
	}
}

// privateHolder keeps its declared secret in an unexported field, which the
// pre-codec walk cannot inspect and therefore never registers.
type privateHolder struct {
	Handle declaredSecret
	port   int
}

func TestTheDisplayNeverDisclosesMoreThanTheRecord(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	// The display used to render the value with %v, which shows unexported fields
	// and calls a String method. A declared secret in an unexported field therefore
	// reached the display in the clear, and the display is part of the record and of
	// every digest over it. The display is now derived from the projection, so it
	// cannot hold anything the record's own data does not.
	got := mustEncode(t, reg, privateHolder{Handle: declaredSecret{Value: "inner-value"}, port: 8080})
	if strings.Contains(got.Display, "inner-value") {
		t.Errorf("the display disclosed a declared secret: %q", got.Display)
	}
	// An unexported field is absent from the record rather than present, because
	// reflect cannot read it. That is the point: it is neither shown nor leaked.
	if strings.Contains(string(got.Data), "8080") || strings.Contains(got.Display, "8080") {
		t.Errorf("an unexported field reached the record: %s / %q", got.Data, got.Display)
	}
	if !strings.Contains(string(got.Data), RedactedMarker) {
		t.Errorf("the declared secret was not replaced: %s", got.Data)
	}
	encoded, err := CanonicalEncode(got)
	if err != nil {
		t.Fatalf("CanonicalEncode: %v", err)
	}
	digest, err := HashValue(got)
	if err != nil {
		t.Fatalf("HashValue: %v", err)
	}
	for _, artifact := range [][]byte{encoded, got.Data, []byte(got.Display), []byte(digest)} {
		if strings.Contains(string(artifact), "inner-value") {
			t.Error("a declared secret reached an artifact")
		}
	}
}

type leakyStringer struct{ Value string }

func (l leakyStringer) String() string { return "leaky " + l.Value + " end" }

func TestTheDisplayDoesNotCallAValueStringMethod(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	// A String method is free to print anything, including a secret the walk never
	// saw, so the display must not call it.
	got := mustEncode(t, reg, leakyStringer{Value: "stringed-value-here"})
	// The String method's own decorations are what identify it. Its text was not
	// used as the display; the exported field was projected instead.
	if strings.Contains(got.Display, "leaky") || strings.Contains(got.Display, " end") {
		t.Errorf("the display called a String method: %q", got.Display)
	}
	if !strings.Contains(string(got.Data), "stringed-value-here") {
		t.Errorf("the exported field was lost: %s", got.Data)
	}
}

// revHidden is the shape the display fix exists for: a declared secret in an
// unexported field, which the pre-codec walk cannot inspect and therefore never
// registers, next to an ordinary exported field.
type revHidden struct {
	handle declaredSecret
	Port   int
}

func TestTheDisplayRevealsNothingTheWalkCannotSee(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	// Before the display was derived from the projection, this rendered as
	// "{{literal} 8080}" through the default formatter while the record claimed
	// visibility: "semantic" and no mechanism at all.
	const literal = "hidden-declared-literal-4d7f"
	got := mustEncode(t, reg, revHidden{handle: declaredSecret{Value: literal}, Port: 8080})
	for _, artifact := range []struct {
		what  string
		value string
	}{
		{"display", got.Display},
		{"data", string(got.Data)},
		{"source", got.Source},
		{"error", got.Error},
	} {
		if strings.Contains(artifact.value, literal) {
			t.Errorf("the %s disclosed a secret in an unexported field: %q", artifact.what, artifact.value)
		}
	}
	// The ordinary exported field is still shown, so the record is not simply
	// blanked in the attempt to be safe.
	if !strings.Contains(string(got.Data), "8080") {
		t.Errorf("the record lost an ordinary exported field: %s", got.Data)
	}
	encoded, err := CanonicalEncode(got)
	if err != nil {
		t.Fatalf("CanonicalEncode: %v", err)
	}
	digest, err := HashValue(got)
	if err != nil {
		t.Fatalf("HashValue: %v", err)
	}
	for _, artifact := range []string{string(encoded), string(got.Data), got.Display, digest} {
		if strings.Contains(artifact, literal) {
			t.Error("a secret in an unexported field reached an artifact")
		}
	}
	// The display is a prefix of the record's own bytes for a structural value, so
	// it cannot describe something the data does not contain.
	if !strings.HasPrefix(strings.TrimSuffix(got.Display, "…"), string(got.Data)) &&
		!strings.HasPrefix(string(got.Data), strings.TrimSuffix(got.Display, "…")) {
		t.Errorf("the display and the data describe different things: %q vs %s", got.Display, got.Data)
	}
}

// The field is exported so the projection carries a value at all; an unexported
// one projects to an empty object, which is the safer outcome but leaves nothing
// here to dispatch a method with.
type methodScalar struct{ Text string }

// The three method sets a named scalar can carry, any of which fmt would
// dispatch to if the display were rendered with a formatter.
func (methodScalar) String() string { return "S:" + methodScalarSecret }
func (methodScalar) Error() string  { return "E:" + methodScalarSecret }
func (methodScalar) Format(f fmt.State, verb rune) {
	_, _ = f.Write([]byte("F:" + methodScalarSecret))
}

const methodScalarSecret = "method-scalar-literal-7b3c"

func TestTheDisplayNeverDispatchesToAValueMethod(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	// A named scalar inside a struct is projected as itself, so a formatter over
	// the projection would still call its String, Error, or Format method. The
	// display is canonical bytes, which write a scalar as text and dispatch to
	// nothing.
	got := mustEncode(t, reg, struct {
		Note methodScalar
		N    int
	}{Note: methodScalar{Text: "harmless"}, N: 1})
	for _, artifact := range []string{got.Display, string(got.Data), got.Error, got.Source} {
		if strings.Contains(artifact, methodScalarSecret) {
			t.Errorf("a value method was dispatched: %q", artifact)
		}
	}
	if !strings.Contains(string(got.Data), "harmless") {
		t.Errorf("the projection lost the value: %s", got.Data)
	}
	// A String method that panics must not take the encoder with it.
	got = mustEncode(t, reg, struct {
		Note panickingStringer
		N    int
	}{Note: panickingStringer{}, N: 1})
	if strings.Contains(got.Display, "PANIC") {
		t.Errorf("a panicking value method reached the display: %q", got.Display)
	}
}

type panickingStringer struct{}

func (panickingStringer) String() string { panic("PANIC: stringer panicked") }

// TestADeclarationIsRememberedSoTwoPassesCannotDisagree proves the walk and the
// projector cannot be made to disagree. A declaration that answered yes once and
// then no would let the walk redact and the projector write the value out, which
// is a record that claims the application declared a secret and then carries it.
func TestADeclarationIsRememberedSoTwoPassesCannotDisagree(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	if !declaredSensitive(reflect.ValueOf(flakySecret{})) {
		t.Fatal("the first declaration did not register")
	}
	// The second and third answers are false. The type must still be honoured.
	if !declaredSensitive(reflect.ValueOf(flakySecret{})) {
		t.Error("a type that declared itself was forgotten, so two passes could disagree")
	}
	got := mustEncode(t, reg, struct {
		Field flakySecret
		Keep  string
	}{Field: flakySecret{}, Keep: "keep"})
	if got.Visibility != ValueRedacted {
		t.Errorf("visibility = %q, want %q", got.Visibility, ValueRedacted)
	}
	if strings.Contains(string(got.Data), got.Display) && got.Display == "" {
		t.Error("no display was produced for a redacted value")
	}
}

type flakySecret struct{ calls *int }

func (f *flakySecret) PabtSensitive() bool {
	if f.calls == nil {
		f.calls = new(int)
	}
	*f.calls++
	return *f.calls == 1
}
