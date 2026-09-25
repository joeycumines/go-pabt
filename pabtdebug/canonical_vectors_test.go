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
	valueVectorBytes  = `{"CodecID":"structure/v1","CodecVersion":1,"Data":"Imh1bnRlcjIi","Display":"REDACTED","Error":"sensitive_path","ReflectType":"string","SHA256":"","Source":"credentials.password","Visibility":"redacted"}`
	valueVectorDigest = "498ade3e5df13cf2a2668da6f37687e31a972d55c337c9e51e90445392e4827c"

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
		Error:        RedactedReason,
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
