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
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

// sensitiveSegments are the normalised path segments that mark a value secret.
//
// A map key is a path segment even when the struct field holding it is not, so a
// map keyed by an access token is redacted for the same reason a field named
// AccessToken is.
// The keys are segments with every separator removed, so one entry covers
// Password, passWord, pass_word, and PASS-WORD alike. Splitting camel case first
// would have made passWord compare as pass_word and miss.
var sensitiveSegments = map[string]struct{}{
	"password": {}, "passwords": {},
	"passwd": {},
	"secret": {}, "secrets": {},
	"token": {}, "tokens": {},
	"apikey": {}, "apikeys": {},
	"authorization": {}, "authorisation": {},
	"cookie": {}, "cookies": {},
	"privatekey": {}, "privatekeys": {},
	"credential": {}, "credentials": {},
	"sessionkey": {}, "sessionkeys": {},
	"accesskey": {}, "accesskeys": {},
	"secretkey": {}, "secretkeys": {},
}

// IsSensitiveSegment reports whether a path segment marks a secret, whatever
// casing and separators the application used to write it.
func IsSensitiveSegment(segment string) bool {
	_, ok := sensitiveSegments[compactSegment(segment)]
	return ok
}

// compactSegment lowercases a segment and keeps only its letters and digits.
func compactSegment(segment string) string {
	var b strings.Builder
	b.Grow(len(segment))
	for _, r := range segment {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r - 'A' + 'a')
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		}
	}
	return b.String()
}

// DiagnosticSanitizer scrubs text that came from the application.
//
// It is the only path by which an error, a panic, a stack, or a rendered value
// reaches a record, a digest, a log, or a response.
type DiagnosticSanitizer interface {
	// Sanitize returns text safe to persist and to show.
	Sanitize(string) string

	// SanitizeStack returns bounded, sanitized frame descriptions.
	SanitizeStack([]string) []string
}

// redactionContext is the per-record scrubber. It is carried on the context so
// that a value encoded deep inside a record is scrubbed with the same registered
// secrets as one encoded at the top.
//
// It holds the secret literals it has seen. That is deliberate and it is why the
// type must never be serialised: the literals exist only to be searched for.
type redactionContext struct {
	limits         ValueLimits
	secrets        []string
	droppedSecrets int
	san            DiagnosticSanitizer
}

// redactionFor returns the scrubber for this record, creating one when the caller
// supplied none. A missing scrubber is filled rather than trusted, because the
// alternative is recording whatever the application happened to return.
func redactionFor(ctx context.Context) (*redactionContext, DiagnosticSanitizer) {
	if c, ok := ctx.Value(redactionKey{}).(*redactionContext); ok && c != nil {
		return c, c.san
	}
	c := &redactionContext{limits: DefaultValueLimits(), san: &textSanitizer{}}
	return c, c.san
}

// WithRedaction runs fn with a scrubber that shares one set of registered secrets
// across every value encoded inside it.
func WithRedaction(ctx context.Context, limits ValueLimits, fn func(context.Context) error) error {
	if err := limits.Validate(); err != nil {
		limits = DefaultValueLimits()
	}
	c := &redactionContext{limits: limits, san: &textSanitizer{limits: limits}}
	return fn(context.WithValue(ctx, redactionKey{}, c))
}

type redactionKey struct{}

// maxRegisteredSecrets bounds the per-record literal list. A record that somehow
// needs more has a value too tangled to record safely, and the walk is told
// rather than the excess being dropped without trace.
const maxRegisteredSecrets = 256

// registerSecret remembers a literal so later text carrying it is scrubbed.
//
// A literal that does not fit is not dropped silently. Redaction still happens;
// what is lost is the ability to scrub that one literal from later free text,
// which is a smaller loss than an unbounded list, so it is counted instead.
func (c *redactionContext) registerSecret(literal string) {
	if c == nil || literal == "" {
		return
	}
	if len(c.secrets) >= maxRegisteredSecrets {
		c.droppedSecrets++
		return
	}
	for _, existing := range c.secrets {
		if existing == literal {
			return
		}
	}
	c.secrets = append(c.secrets, literal)
}

// scrub removes every registered secret literal from text.
func (c *redactionContext) scrub(text string) string {
	if c == nil || text == "" {
		return text
	}
	for _, secret := range c.secrets {
		if secret != "" {
			text = strings.ReplaceAll(text, secret, RedactedMarker)
		}
	}
	return text
}

// walkDecision is the outcome of the pre-codec walk for one value.
type walkDecision struct {
	// Path is the normalised path the value was found at.
	Path string

	// Redacted reports that a segment of the path marked the value secret.
	Redacted bool
}

// walk inspects a value before any codec sees it.
//
// The walk is deliberately before codec selection: a codec that has already
// produced bytes cannot be un-produced, so a sensitive value that reached a codec
// first would already have been hashed.
func (c *redactionContext) walk(path string, v reflect.Value, depth, nodes uint32) (walkDecision, error) {
	if depth > c.limits.MaxValueDepth {
		return walkDecision{}, fmt.Errorf("%w: value nested deeper than %d", ErrValueUnencodable, c.limits.MaxValueDepth)
	}
	nodes++
	if nodes > c.limits.MaxValueNodes {
		return walkDecision{}, fmt.Errorf("%w: value has more than %d values", ErrValueUnencodable, c.limits.MaxValueNodes)
	}
	if !v.IsValid() {
		return walkDecision{Path: path}, nil
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return walkDecision{Path: path}, nil
		}
		return c.walk(path, v.Elem(), depth+1, nodes)
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			key := iter.Key()
			// A non-string key is refused rather than rendered: the int 1 and the
			// string "1" render identically, so projecting them would silently drop
			// one of the entries.
			if key.Kind() != reflect.String {
				return walkDecision{}, fmt.Errorf("%w: map key of kind %s at %s",
					ErrValueUnencodable, key.Kind(), path)
			}
			segment := key.String()
			child := joinPath(path, segment)
			// A map key is a path segment even when the field holding the map is
			// not, so a map keyed by a secret is redacted for the same reason a
			// field named for one is.
			if IsSensitiveSegment(segment) {
				return walkDecision{Path: child, Redacted: true}, nil
			}
			decision, err := c.walk(child, iter.Value(), depth+1, nodes)
			if err != nil || decision.Redacted {
				return decision, err
			}
		}
		return walkDecision{Path: path}, nil
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8 {
			return walkDecision{Path: path}, nil
		}
		for i := range v.Len() {
			decision, err := c.walk(joinPath(path, fmt.Sprint(i)), v.Index(i), depth+1, nodes)
			if err != nil || decision.Redacted {
				return decision, err
			}
		}
		return walkDecision{Path: path}, nil
	case reflect.Struct:
		t := v.Type()
		for i := range t.NumField() {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			child := joinPath(path, field.Name)
			if IsSensitiveSegment(field.Name) {
				return walkDecision{Path: child, Redacted: true}, nil
			}
			decision, err := c.walk(child, v.Field(i), depth+1, nodes)
			if err != nil || decision.Redacted {
				return decision, err
			}
		}
		return walkDecision{Path: path}, nil
	case reflect.String:
		if !utf8Valid(v.String()) {
			return walkDecision{}, fmt.Errorf("%w: string at %s is not valid UTF-8", ErrValueUnencodable, path)
		}
		return walkDecision{Path: path}, nil
	case reflect.Float32, reflect.Float64:
		f := v.Float()
		if f != f {
			return walkDecision{}, fmt.Errorf("%w: float at %s is NaN", ErrValueUnencodable, path)
		}
		return walkDecision{Path: path}, nil
	default:
		return walkDecision{Path: path}, nil
	}
}

func joinPath(parent, segment string) string {
	if parent == "" {
		return segment
	}
	return parent + "." + segment
}

// secretLiteral returns the text of a value that is about to be redacted, so it
// can be registered for scrubbing without persisting it.
func secretLiteral(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	case fmt.Stringer:
		return t.String()
	}
	return ""
}

// textSanitizer scrubs a string with no codec involvement.
type textSanitizer struct {
	limits ValueLimits
}

// Sanitize removes registered credential shapes, control characters, and
// anything past the length bound, and never returns the original bytes.
func (s *textSanitizer) Sanitize(text string) string {
	limits := s.limits
	if err := limits.Validate(); err != nil {
		limits = DefaultValueLimits()
	}
	text = redactCredentialShapes(text)
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		if r < 0x20 && r != '\t' {
			continue
		}
		b.WriteRune(r)
	}
	out := b.String()
	if uint32(len([]rune(out))) > limits.MaxDisplayBytes {
		// Truncating by byte could split a rune and leave invalid text behind.
		out = string([]rune(out)[:limits.MaxDisplayBytes]) + "…"
	}
	return out
}

// SanitizeStack returns bounded, sanitized frame descriptions.
func (s *textSanitizer) SanitizeStack(frames []string) []string {
	limits := s.limits
	if err := limits.Validate(); err != nil {
		limits = DefaultValueLimits()
	}
	out := make([]string, 0, len(frames))
	for _, frame := range frames {
		out = append(out, s.Sanitize(frame))
		// Bounded by a frame count, which is what this is, rather than by the
		// nesting limit of a value.
		if uint32(len(out)) >= limits.MaxStackFrames {
			break
		}
	}
	return out
}

// redactCredentialShapes removes the forms that carry a secret in text the walk
// never saw, such as an error message: an explicit assignment and a bearer
// credential.
//
// The scan advances past each region it rewrites. Re-scanning from the start
// would find the marker it had just inserted and loop forever, because the marker
// follows the very marker being searched for.
func redactCredentialShapes(text string) string {
	markers := []string{
		"password", "passwd", "secret", "token", "apikey", "api_key", "api-key",
		"authorization", "cookie", "private_key", "privatekey", "credential",
	}
	// The bearer form is handled first. "Authorization: Bearer x" also looks like
	// an assignment, and an assignment scrub would replace the word "Bearer" and
	// leave the credential behind it.
	text = redactBearer(text)
	for _, marker := range markers {
		text = redactAssignments(text, marker)
	}
	return text
}

// redactAssignments replaces the value of every "marker=value" or "marker: value"
// occurrence of one marker.
//
// The offsets are absolute. Treating the tail after the marker as if it were the
// whole string would drop the message prefix and shift the tail left, which both
// loses the text a human needed and lets a later occurrence slip past.
func redactAssignments(text, marker string) string {
	lower := strings.ToLower(text)
	for from := 0; ; {
		rel := strings.Index(lower[from:], marker)
		if rel < 0 {
			return text
		}
		at := from + rel
		// The marker must be a whole word, or "tokenizer=1" would be redacted as
		// though it were a credential.
		if at > 0 && isWordByte(text[at-1]) {
			from = at + len(marker)
			continue
		}
		if at+len(marker) < len(text) && isWordByte(text[at+len(marker)]) {
			from = at + len(marker)
			continue
		}
		// Skip spaces and quotes, require exactly one separator, then skip the
		// spaces and quotes that may follow it. The quotes matter for a JSON-shaped
		// message such as {"api_key":"value"}.
		sep := at + len(marker)
		for sep < len(text) && isSkippable(text[sep]) {
			sep++
		}
		if sep >= len(text) || (text[sep] != '=' && text[sep] != ':') {
			from = at + len(marker)
			continue
		}
		start := sep + 1
		for start < len(text) && isSkippable(text[start]) {
			start++
		}
		stop := start
		for stop < len(text) && !strings.ContainsRune(" \t,;\"'}", rune(text[stop])) {
			stop++
		}
		if stop == start {
			from = at + len(marker)
			continue
		}
		text = text[:start] + RedactedMarker + text[stop:]
		lower = strings.ToLower(text)
		from = start + len(RedactedMarker)
		if from >= len(text) {
			return text
		}
	}
}

// isSkippable reports whether a byte is whitespace or a quote that may sit
// between a marker and its value.
func isSkippable(b byte) bool {
	switch b {
	case ' ', '\t', '"', '\'':
		return true
	}
	return false
}

func isWordByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9', b == '_':
		return true
	}
	return false
}

// redactBearer replaces the credential in every "Bearer <token>" occurrence.
func redactBearer(text string) string {
	const bearer = "bearer "
	lower := strings.ToLower(text)
	for from := 0; ; {
		rel := strings.Index(lower[from:], bearer)
		if rel < 0 {
			return text
		}
		start := from + rel + len(bearer)
		stop := start
		for stop < len(text) && !unicode.IsSpace(rune(text[stop])) {
			stop++
		}
		if stop == start {
			return text
		}
		text = text[:start] + RedactedMarker + text[stop:]
		lower = strings.ToLower(text)
		from = start + len(RedactedMarker)
		if from >= len(text) {
			return text
		}
	}
}

// utf8Valid reports whether s is valid UTF-8. A replacement character that the
// application supplied is legitimate text; only an invalid byte sequence is not.
func utf8Valid(s string) bool { return utf8.ValidString(s) }
