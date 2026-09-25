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
	"unicode/utf8"
)

// sensitiveSegments are the normalised path segments that mark a value secret.
//
// A map key is a path segment even when the struct field holding it is not, so a
// map keyed by an access token is redacted for the same reason a field named
// AccessToken is.
// sensitiveSegments is keyed by a segment normalised to lower snake case, so one
// entry covers Password, passWord, pass_word, and PASS-WORD alike. Compound names
// are listed explicitly, because dropping the separator would make auth_token and
// authtoken the same key while neither spelling is a word anyone writes.
var sensitiveSegments = map[string]struct{}{
	"password": {}, "passwords": {}, "pass_word": {},
	"passwd": {}, "pass_word_phrase": {},
	"secret": {}, "secrets": {}, "secret_key": {},
	"token": {}, "tokens": {},
	"auth_token": {}, "access_token": {}, "refresh_token": {},
	"id_token": {}, "bearer_token": {},
	"api_key": {}, "apikeys": {}, "api_secret": {},
	"authorization": {}, "authorisation": {},
	"cookie": {}, "cookies": {}, "set_cookie": {},
	"private_key": {}, "private_keys": {}, "private_key_phrase": {},
	"credential": {}, "credentials": {},
	"session_key": {}, "session_token": {},
	"access_key": {}, "access_keys": {},
	"user_password": {},
}

// IsSensitiveSegment reports whether a path segment marks a secret, whatever
// casing and separators the application used to write it.
func IsSensitiveSegment(segment string) bool {
	_, ok := sensitiveSegments[normaliseSegment(segment)]
	return ok
}

// normaliseSegment rewrites a segment to lower snake case.
//
// A lower to upper boundary starts a word, as does the last capital of a run
// followed by a lower case letter, so HTTPServer becomes http_server rather than
// hTTPServer. Separators of every kind collapse to a single underscore, and a
// repeated underscore is folded, so a name is recognised however it was spelled.
func normaliseSegment(segment string) string {
	runes := []rune(segment)
	var b strings.Builder
	b.Grow(len(segment) + 4)
	prevUnderscore := true
	for i, r := range runes {
		switch {
		case r >= 'A' && r <= 'Z':
			// A boundary before a capital, unless one was just written.
			if i > 0 && !prevUnderscore {
				lowerBefore := runes[i-1] >= 'a' && runes[i-1] <= 'z' ||
					(runes[i-1] >= 'A' && runes[i-1] <= 'Z' && i+1 < len(runes) &&
						runes[i+1] >= 'a' && runes[i+1] <= 'z')
				if lowerBefore {
					b.WriteByte('_')
				}
			}
			b.WriteRune(r - 'A' + 'a')
			prevUnderscore = false
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevUnderscore = false
		default:
			// Every separator kind collapses to one underscore.
			if !prevUnderscore {
				b.WriteByte('_')
				prevUnderscore = true
			}
		}
	}
	return strings.TrimRight(b.String(), "_")
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
	// Path is the path the value was found at, relative to the value the walk
	// started from.
	Path string

	// Redacted reports that a segment of the path marked the value secret.
	Redacted bool

	// Value is the value the walk stopped at. Carrying it means the caller never
	// has to find the value again by parsing the path, which cannot work for a
	// segment that itself contains the separator.
	Value reflect.Value
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
			return walkDecision{Path: path, Value: v}, nil
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
				return walkDecision{Path: child, Redacted: true, Value: iter.Value()}, nil
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
		return walkDecision{Path: path, Value: v}, nil
	case reflect.Struct:
		t := v.Type()
		for i := range t.NumField() {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			child := joinPath(path, field.Name)
			if IsSensitiveSegment(field.Name) {
				return walkDecision{Path: child, Redacted: true, Value: v.Field(i)}, nil
			}
			decision, err := c.walk(child, v.Field(i), depth+1, nodes)
			if err != nil || decision.Redacted {
				return decision, err
			}
		}
		return walkDecision{Path: path, Value: v}, nil
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

// registerLeaves remembers the text of every scalar leaf under a redacted value.
//
// A redacted value is often a whole struct or map rather than a single string.
// Registering only its top would leave its leaves able to reappear in a later
// diagnostic, so every leaf is registered.
func (c *redactionContext) registerLeaves(v reflect.Value) {
	c.registerValueLeaves(v, 0)
}

func (c *redactionContext) registerValueLeaves(v reflect.Value, depth uint32) {
	if c == nil || !v.IsValid() || depth > 16 || c.droppedSecrets > 0 {
		return
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return
		}
		c.registerValueLeaves(v.Elem(), depth+1)
	case reflect.String:
		c.registerSecret(v.String())
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			c.registerSecret(string(v.Bytes()))
			return
		}
		for i := range v.Len() {
			c.registerValueLeaves(v.Index(i), depth+1)
		}
	case reflect.Array:
		for i := range v.Len() {
			c.registerValueLeaves(v.Index(i), depth+1)
		}
	case reflect.Map:
		iter := v.MapRange()
		for iter.Next() {
			c.registerValueLeaves(iter.Value(), depth+1)
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				c.registerValueLeaves(v.Field(i), depth+1)
			}
		}
	}
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
// never saw, such as an error message.
//
// The decision is made on the identifier immediately before an assignment
// separator, compacted the same way a path segment is. That covers auth_token,
// access_key, secretKey, and api-key without a list of every spelling, and it
// leaves a word that merely contains a marker alone, because tokeniser
// compacts to something that is not a credential name.
func redactCredentialShapes(text string) string {
	// The bearer form is handled first. "Authorization: Bearer x" also looks like an
	// assignment, and an assignment pass would replace the word "Bearer" and leave
	// the credential behind it.
	text = redactBearer(text)

	runes := []rune(text)
	var out strings.Builder
	out.Grow(len(text))
	for i := 0; i < len(runes); {
		if runes[i] != '=' && runes[i] != ':' {
			out.WriteRune(runes[i])
			i++
			continue
		}
		// Walk back over any closing quote and then over the identifier, so that a
		// JSON-shaped message such as {"api_key":"value"} is recognised.
		end := i
		for end > 0 && (runes[end-1] == '"' || runes[end-1] == '\'') {
			end--
		}
		start := end
		for start > 0 && isIdentifierRune(runes[start-1]) {
			start--
		}
		if start == end {
			out.WriteRune(runes[i])
			i++
			continue
		}
		// The identifier is passed as written, because the segment normaliser needs
		// to see the camel-case boundary in "secretKey".
		if !IsSensitiveSegment(string(runes[start:end])) {
			out.WriteRune(runes[i])
			i++
			continue
		}
		// The separator and the spaces or quotes around the value are kept, so the
		// message still reads correctly and the redaction is visible in place.
		out.WriteRune(runes[i])
		i++
		for i < len(runes) && isSkippableRune(runes[i]) {
			out.WriteRune(runes[i])
			i++
		}
		valueStart := i
		for i < len(runes) && !strings.ContainsRune(" \t,;\"'}", runes[i]) {
			i++
		}
		if i == valueStart {
			continue
		}
		out.WriteString(RedactedMarker)
	}
	return out.String()
}

// isIdentifierRune reports whether a rune can be part of an assignment target.
func isIdentifierRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case r == '_', r == '-', r == '.':
		return true
	}
	return false
}

func isSkippableRune(r rune) bool {
	switch r {
	case ' ', '\t', '"', '\'':
		return true
	}
	return false
}

// redactBearer replaces the credential in every "Bearer <token>" occurrence,
// wherever it appears in the line.
func redactBearer(text string) string {
	lower := strings.ToLower(text)
	for from := 0; ; {
		rel := strings.Index(lower[from:], "bearer ")
		if rel < 0 {
			return text
		}
		start := from + rel + len("bearer ")
		stop := start
		for stop < len(text) && !isSkippableRuneAt(text[stop]) {
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

// isSkippableRuneAt reports whether the byte at i is whitespace.
func isSkippableRuneAt(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r':
		return true
	}
	return false
}

func utf8Valid(s string) bool { return utf8.ValidString(s) }
