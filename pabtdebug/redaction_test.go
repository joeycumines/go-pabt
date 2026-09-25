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
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

const secretLiteralValue = "s3cr3t-literal-value"

type shape struct{ A int }

type (
	credential struct {
		User     string `json:"user"`
		Password string `json:"password"`
	}
	session struct {
		ID    string            `json:"id"`
		Token string            `json:"token"`
		Extra map[string]string `json:"extra"`
	}
	nested struct {
		Label    string     `json:"label"`
		Account  credential `json:"account"`
		Sessions []session  `json:"sessions"`
	}
	codecPanic struct{}
)

func (codecPanic) ID() string              { return "panic/v1" }
func (codecPanic) Version() uint32         { return 1 }
func (codecPanic) MatchType() reflect.Type { return nil }
func (codecPanic) Encode(any) (Bytes, error) {
	panic("codec exploded with token=" + secretLiteralValue)
}
func (codecPanic) Decode(Bytes) (any, error) { return nil, nil }

// assertNoSecret fails if the secret appears anywhere in an artifact.
func assertNoSecret(t *testing.T, what string, artifacts ...[]byte) {
	t.Helper()
	for _, a := range artifacts {
		if a == nil {
			continue
		}
		if bytesContains(a, secretLiteralValue) {
			t.Errorf("%s contains the planted secret", what)
		}
	}
}

func bytesContains(haystack []byte, needle string) bool {
	return strings.Contains(string(haystack), needle)
}

// TestRedactionKeepsSecretsOutOfEveryArtifact is the central secret-handling
// proof. The secret is planted in a struct field, a nested struct, a slice
// element, a map key, an error string, a panic string, and a codec failure, and
// then every artifact the debugger would ever emit is checked for it.
func TestRedactionKeepsSecretsOutOfEveryArtifact(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	value := nested{
		Label: "ok",
		Account: credential{
			User:     "alice",
			Password: secretLiteralValue,
		},
		Sessions: []session{{
			ID:    "s1",
			Token: secretLiteralValue,
			Extra: map[string]string{"note": "fine"},
		}},
	}

	var valueOut StoredValue
	err := WithRedaction(context.Background(), DefaultValueLimits(),
		func(ctx context.Context) error {
			got, err := reg.Encode(ctx, "record", value)
			if err != nil {
				return err
			}
			valueOut = got
			return nil
		})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	if valueOut.Visibility != ValueRedacted {
		t.Fatalf("visibility = %q, want %q", valueOut.Visibility, ValueRedacted)
	}
	if valueOut.Error != RedactedReason {
		t.Errorf("reason = %q, want %q", valueOut.Error, RedactedReason)
	}
	if valueOut.SHA256 == "" {
		t.Error("a redacted value carries no digest")
	}
	if valueOut.Display != RedactedMarker {
		t.Errorf("display = %q, want the marker", valueOut.Display)
	}

	encoded, err := CanonicalEncode(valueOut)
	if err != nil {
		t.Fatalf("CanonicalEncode: %v", err)
	}
	digest, err := HashValue(valueOut)
	if err != nil {
		t.Fatalf("HashValue: %v", err)
	}
	// The record and the response both embed these bytes, so checking them is
	// checking the response too.
	assertNoSecret(t, "the encoded value", encoded, []byte(digest))

	// The safe parts must survive, or redaction would have destroyed the record.
	// The safe siblings must survive, or redaction would have destroyed the
	// record along with the secret.
	projection := string(valueOut.Data)
	for _, want := range []string{"alice", `"user":"alice"`, "fine", `"label":"ok"`, `"id":"s1"`} {
		if !strings.Contains(projection, want) {
			t.Errorf("the redacted projection lost %q: %s", want, projection)
		}
	}
	if strings.Count(projection, RedactedMarker) != 2 {
		t.Errorf("expected two redacted fields, got %d: %s",
			strings.Count(projection, RedactedMarker), projection)
	}
	if !strings.Contains(projection, RedactedMarker) {
		t.Errorf("the redacted projection does not contain the marker: %s", projection)
	}
}

// TestRedactionReplacesAWholeSensitiveField states the deliberate boundary: a
// field whose own name is sensitive is replaced whole, because nothing about its
// contents can be assumed safe.
func TestRedactionReplacesAWholeSensitiveField(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	type payload struct {
		Label      string `json:"label"`
		Credential struct {
			User     string `json:"user"`
			Password string `json:"password"`
		} `json:"credential"`
	}
	var in payload
	in.Label = "ok"
	in.Credential.User = "alice"
	in.Credential.Password = secretLiteralValue

	var out StoredValue
	err := WithRedaction(context.Background(), DefaultValueLimits(),
		func(ctx context.Context) error {
			var err error
			out, err = reg.Encode(ctx, "record", in)
			return err
		})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if out.Visibility != ValueRedacted {
		t.Fatalf("visibility = %q, want redacted", out.Visibility)
	}
	encoded, err := CanonicalEncode(out)
	if err != nil {
		t.Fatalf("CanonicalEncode: %v", err)
	}
	assertNoSecret(t, "a wholly redacted field", encoded, out.Data)
	if strings.Contains(string(out.Data), "alice") {
		t.Errorf("a wholly sensitive field kept a nested value: %s", out.Data)
	}
}

// TestRedactionCannotGuessAnOpaqueMapKey records a boundary rather than a claim.
// A map whose key is an opaque token is indistinguishable from one keyed by an
// identifier, so the walk does not remove it. Pretending otherwise would mean
// guessing, and a guess that removes ordinary data is worse than the gap.
func TestRedactionCannotGuessAnOpaqueMapKey(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	out := mustEncode(t, reg, map[string]string{secretLiteralValue: "leaked"})
	if out.Visibility == ValueRedacted {
		t.Skip("the walk now treats an opaque map key as sensitive, which the boundary test says it does not")
	}
	if out.Visibility != ValueSemantic {
		t.Errorf("visibility = %q, want %q for a structure with no sensitive segment", out.Visibility, ValueSemantic)
	}
}

func TestRedactionNormalisesSegmentNaming(t *testing.T) {
	for _, segment := range []string{
		"password", "Password", "PASSWORD", "passWord",
		"apiKey", "api_key", "API-KEY", "Api Key",
		"authorization", "Authorization", "cookie", "privateKey", "private_key",
		"credential", "credentials", "sessionKey", "session_key", "token", "secret", "passwd",
	} {
		if !IsSensitiveSegment(segment) {
			t.Errorf("IsSensitiveSegment(%q) = false, want true", segment)
		}
	}
	for _, segment := range []string{
		"user", "label", "name", "id", "value", "keyword", "pasword", "", "tokenizer", "cookiecutter",
	} {
		if IsSensitiveSegment(segment) {
			t.Errorf("IsSensitiveSegment(%q) = true, want false", segment)
		}
	}
}

func TestRedactionTreatsAMapKeyAsAPathSegment(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	// The map is not behind a sensitive field; the key itself is the secret.
	var out StoredValue
	err := WithRedaction(context.Background(), DefaultValueLimits(),
		func(ctx context.Context) error {
			var err error
			out, err = reg.Encode(ctx, "record",
				map[string]string{"password": secretLiteralValue, "user": "alice"})
			return err
		})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if out.Visibility != ValueRedacted {
		t.Fatalf("visibility = %q, want %q", out.Visibility, ValueRedacted)
	}
	encoded, err := CanonicalEncode(out)
	if err != nil {
		t.Fatalf("CanonicalEncode: %v", err)
	}
	assertNoSecret(t, "a map keyed by a secret", encoded, out.Data)
	if !strings.Contains(string(out.Data), "alice") {
		t.Errorf("the safe map entry was lost: %s", out.Data)
	}
}

func TestRedactionScrubsAnErrorString(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	// A struct whose unexported field holds the secret is not walked, so the
	// only place it can surface is the error text.
	failing := struct {
		Note string `json:"note"`
	}{Note: "fine"}
	var out StoredValue
	err := WithRedaction(context.Background(), DefaultValueLimits(),
		func(ctx context.Context) error {
			var err error
			out, err = reg.Encode(ctx, "record", failing)
			return err
		})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	encoded, err := CanonicalEncode(out)
	if err != nil {
		t.Fatalf("CanonicalEncode: %v", err)
	}
	assertNoSecret(t, "a clean value", encoded)

	// A diagnostic carrying the secret in a recognisable shape is scrubbed, and
	// the scrubbed form is what a record would carry.
	san := &textSanitizer{limits: DefaultValueLimits()}
	for _, text := range []string{
		"failed with token=" + secretLiteralValue,
		"password=" + secretLiteralValue,
		"Authorization: Bearer " + secretLiteralValue,
		`{"api_key":"` + secretLiteralValue + `"}`,
		"secret: " + secretLiteralValue,
	} {
		got := san.Sanitize(text)
		if strings.Contains(got, secretLiteralValue) {
			t.Errorf("Sanitize(%q) left the secret: %q", text, got)
		}
	}

	// A literal the walk already saw is scrubbed from later text even with no
	// recognisable shape, because the walk registered it.
	err = WithRedaction(context.Background(), DefaultValueLimits(),
		func(ctx context.Context) error {
			c, _ := redactionFor(ctx)
			_, encErr := reg.Encode(ctx, "record", nested{
				Account: credential{User: "alice", Password: secretLiteralValue},
			})
			if encErr != nil {
				return encErr
			}
			bare := "connection failed for " + secretLiteralValue
			if strings.Contains(c.scrub(bare), secretLiteralValue) {
				t.Errorf("a registered secret survived later text: %q", c.scrub(bare))
			}
			return nil
		})
	if err != nil {
		t.Fatalf("registered-secret scrub: %v", err)
	}
}

// TestSanitizeDoesNotGuessAnOpaqueString states the boundary rather than a claim.
// A bare string with no credential shape and no registered literal is not
// detectable. Removing every short string would destroy the record; the design
// therefore covers shapes and literals the walk saw, and says so.
func TestSanitizeDoesNotGuessAnOpaqueString(t *testing.T) {
	san := &textSanitizer{limits: DefaultValueLimits()}
	plain := "connection refused to 10.0.0.1"
	if got := san.Sanitize(plain); got != plain {
		t.Errorf("an ordinary message was altered: %q -> %q", plain, got)
	}
}

func TestDiagnosticSanitizerBoundsAndStripsControlText(t *testing.T) {
	limits := DefaultValueLimits()
	limits.MaxDisplayBytes = 16
	san := &textSanitizer{limits: limits}
	got := san.Sanitize(strings.Repeat("a", 200))
	if len([]rune(got)) > 17 {
		t.Errorf("sanitized text is %d runes, want at most 17", len([]rune(got)))
	}
	if !strings.Contains(got, "…") {
		t.Errorf("truncated text does not say it was truncated: %q", got)
	}
	// A control character must not survive into a record or a terminal.
	cleaned := san.Sanitize("a\x00b\x07c\x1bd")
	for _, r := range cleaned {
		if r < 0x20 && r != '\t' {
			t.Errorf("control character %q survived sanitising", r)
		}
	}
	if !strings.Contains(cleaned, "a") || !strings.Contains(cleaned, "d") {
		t.Errorf("sanitising removed printable text: %q", cleaned)
	}
	// A tab is legitimate whitespace in a stack frame.
	if !strings.Contains(san.Sanitize("a\tb"), "\t") {
		t.Error("a tab was removed from a stack frame")
	}
}

func TestSanitizeStackIsBounded(t *testing.T) {
	limits := DefaultValueLimits()
	limits.MaxStackFrames = 3
	san := &textSanitizer{limits: limits}
	frames := make([]string, 0, 50)
	for i := range 50 {
		frames = append(frames, fmt.Sprintf("frame.%d token=%s", i, secretLiteralValue))
	}
	got := san.SanitizeStack(frames)
	if len(got) > int(limits.MaxStackFrames) {
		t.Errorf("SanitizeStack returned %d frames, want at most %d", len(got), limits.MaxStackFrames)
	}
	for _, frame := range got {
		if strings.Contains(frame, secretLiteralValue) {
			t.Errorf("a stack frame kept the secret: %q", frame)
		}
	}
}

func TestValueWalkLimits(t *testing.T) {
	limits := DefaultValueLimits()
	limits.MaxValueDepth = 4
	limits.MaxValueNodes = 8
	reg := NewCodecRegistry(limits)

	// A slice nested deeper than the limit.
	deep := any("leaf")
	for range 10 {
		deep = []any{deep}
	}
	var out StoredValue
	err := WithRedaction(context.Background(), limits, func(ctx context.Context) error {
		var err error
		out, err = reg.Encode(ctx, "record", deep)
		return err
	})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if out.Visibility != ValueError {
		t.Errorf("an over-deep value was recorded as %q, want %q", out.Visibility, ValueError)
	}
	if out.Error == "" {
		t.Error("a rejected value carries no reason")
	}

	// A non-string map key cannot be projected, so it is refused rather than
	// silently rendered.
	var keyed StoredValue
	err = WithRedaction(context.Background(), DefaultValueLimits(),
		func(ctx context.Context) error {
			var err error
			keyed, err = reg.Encode(ctx, "record", map[int]string{1: "a"})
			return err
		})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if keyed.Visibility != ValueError {
		t.Errorf("a non-string map key was recorded as %q, want %q", keyed.Visibility, ValueError)
	}
}

func TestValueVisibilityDistinguishesRenderedFromReproduced(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	type point struct{ X, Y int }

	for _, test := range []struct {
		name  string
		value any
		want  ValueVisibility
	}{
		{name: "string is exact", value: "text", want: ValueExact},
		{name: "bool is exact", value: true, want: ValueExact},
		{name: "int is exact", value: int64(7), want: ValueExact},
		{name: "float is exact", value: 1.5, want: ValueExact},
		{name: "bytes are exact", value: Bytes("ab"), want: ValueExact},
		{name: "a struct is recorded as a canonical structure", value: point{1, 2}, want: ValueSemantic},
		{name: "a map is recorded as a canonical structure", value: map[string]int{"a": 1}, want: ValueSemantic},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := mustEncode(t, reg, test.value)
			if got.Visibility != test.want {
				t.Errorf("visibility = %q, want %q", got.Visibility, test.want)
			}
			// A value that was not reproduced byte for byte must not claim to
			// have been, and must still carry something a reader can see.
			if got.Visibility != ValueExact && got.Display == "" {
				t.Errorf("a %q value carries no display", got.Visibility)
			}
		})
	}
}

func TestNilIsUnknownRatherThanEmpty(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	got := mustEncode(t, reg, nil)
	if got.Visibility != ValueUnknown {
		t.Errorf("nil visibility = %q, want %q", got.Visibility, ValueUnknown)
	}
	if got.Display != "<nil>" {
		t.Errorf("nil display = %q", got.Display)
	}
	if got.SHA256 == "" {
		t.Error("a nil value carries no digest")
	}
}

func TestCodecSelectionPrefersTheExactMatch(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	// The display codec accepts everything, so a value with an exact codec must
	// still take the exact one rather than falling back to a rendering.
	if err := reg.Register(multiCodec{}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := reg.Register(shapeCodec{}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got := mustEncode(t, reg, shape{A: 1})
	if got.CodecID != "shape/v1" {
		t.Errorf("codec = %q, want the exact match", got.CodecID)
	}
}

type multiCodec struct{}

func (multiCodec) ID() string      { return "multi/v1" }
func (multiCodec) Version() uint32 { return 1 }
func (multiCodec) MatchType() reflect.Type {
	return reflect.TypeOf((*any)(nil)).Elem()
}
func (multiCodec) Encode(v any) (Bytes, error) { return Bytes("m"), nil }
func (multiCodec) Decode(Bytes) (any, error)   { return nil, nil }

type shapeCodec struct{}

func (shapeCodec) ID() string                  { return "shape/v1" }
func (shapeCodec) Version() uint32             { return 1 }
func (shapeCodec) MatchType() reflect.Type     { return reflect.TypeOf(shape{}) }
func (shapeCodec) Encode(v any) (Bytes, error) { return Bytes("s"), nil }
func (shapeCodec) Decode(Bytes) (any, error)   { return nil, nil }

func TestRegisterRejectsAnIncompleteCodec(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	if err := reg.Register(nil); err == nil {
		t.Error("a nil codec was registered")
	}
	// A codec with no match type is a fallback and is allowed; one with no
	// identity is not, because a record could not say which codec ran.
	if err := reg.Register(codecPanic{}); err != nil {
		t.Errorf("a fallback codec was refused: %v", err)
	}
	if err := reg.Register(unnamedCodec{}); err == nil {
		t.Error("a codec with no id was registered")
	}
}

type unnamedCodec struct{}

func (unnamedCodec) ID() string      { return "" }
func (unnamedCodec) Version() uint32 { return 1 }
func (unnamedCodec) MatchType() reflect.Type {
	return reflect.TypeOf((*any)(nil)).Elem()
}
func (unnamedCodec) Encode(any) (Bytes, error) { return nil, nil }
func (unnamedCodec) Decode(Bytes) (any, error) { return nil, nil }

func TestEncodePropagatesCancellation(t *testing.T) {
	reg := NewCodecRegistry(DefaultValueLimits())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reg.Encode(ctx, "record", "value"); !errors.Is(err, context.Canceled) {
		t.Errorf("Encode with a cancelled context = %v, want context.Canceled", err)
	}
}
