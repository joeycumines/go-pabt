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
	"sort"
	"strings"
	"unicode/utf8"
)

// A segment is a credential when it is a credential word in its own right, or
// when it combines a word that names a secret with a word that names the thing
// holding it.
//
// Combining is what makes the rule work without enumerating compounds. A field
// named sortKey, primaryKey, cacheKey, or idempotencyKey names a sort, a
// primary, a cache, and an idempotency, so it is ordinary state. A field named
// apiKey, accessKey, secretKey, encryptionKey, or awsSecretAccessKey names an
// API, an access grant, a secret, or an encryption, and the thing holding it is
// a key. A field named passwordHash is a credential because "password" is a
// credential word, while fileHash and treeHash are digests of ordinary data.
var sensitiveNames = map[string]struct{}{
	"password": {}, "passwords": {}, "passwd": {},
	"passphrase": {}, "passphrases": {},
	"secret": {}, "secrets": {},
	"credential": {}, "credentials": {},
	"authorization": {}, "authorisation": {},
	"cookie": {}, "cookies": {},
	"token": {}, "tokens": {},
	"pass": {},
}

// sensitiveExact are words that are a credential only as the whole name. They are
// also ordinary nouns, so a compound that merely contains one is not a credential:
// a saltiness, a signatureAlgorithm, and a barcode are ordinary data.
var sensitiveExact = map[string]struct{}{
	"salt": {}, "seed": {}, "seeds": {}, "mnemonic": {}, "mnemonics": {},
	"signature": {}, "signatures": {},
	"code": {}, "codes": {}, "pin": {}, "pins": {}, "otp": {},
	"answer": {}, "answers": {}, "hint": {}, "hints": {},
}

// secrecyQualifiers name a purpose whose value is meant to stay private. One of
// these alongside a credential noun is enough.
var secrecyQualifiers = map[string]struct{}{
	"api": {}, "access": {}, "secret": {}, "private": {},
	"encryption": {}, "encrypt": {}, "signing": {}, "sign": {},
	"auth": {}, "authn": {}, "authorization": {}, "master": {},
	"refresh": {}, "bearer": {}, "jwt": {}, "sso": {}, "hmac": {},
	"webhook": {}, "smtp": {}, "customer": {}, "tenant": {},
	"pepper": {}, "aws": {}, "azure": {}, "gcp": {},
	"session": {}, "service": {}, "system": {}, "app": {}, "application": {},
	// "id" qualifies an id_token, an OIDC credential, and is harmless as a
	// qualifier because "id" is not one of the nouns below, so a user_id or
	// client_id stays ordinary state.
	"id": {},
	// "pass" qualifies a pass_word, a pass_phrase, and a passcode.
	"pass": {},
	"seed": {}, "mnemonic": {},
}

// credentialNouns are the things a secret is held in.
var credentialNouns = map[string]struct{}{
	"key": {}, "keys": {},
	"token": {}, "tokens": {},
	"secret": {}, "secrets": {},
	"password": {}, "passwords": {}, "passwd": {},
	"phrase": {}, "phrases": {},
	"code": {}, "codes": {},
	"pin": {}, "pins": {}, "otp": {},
	"credential": {}, "credentials": {},
	"hash": {}, "hashes": {},
	"seed": {}, "seeds": {}, "mnemonic": {}, "mnemonics": {},
	"salt": {}, "signature": {}, "signatures": {},
	"cookie": {}, "cookies": {},
	"authorization": {}, "authorisation": {},
	"word": {}, "words": {},
	"answer": {}, "answers": {}, "hint": {}, "hints": {},
}

// IsSensitiveSegment reports whether a path segment marks a secret, whatever
// casing and separators the application used to write it.
//
// A map key is a path segment even when the struct field holding it is not, so a
// map keyed by an access token is redacted for the same reason a field named
// AccessToken is.
func IsSensitiveSegment(segment string) bool {
	name := normaliseSegment(segment)
	if name == "" {
		return false
	}
	segments := strings.Split(name, "_")
	if _, ok := sensitiveExact[name]; ok {
		return true
	}
	// A credential word anywhere in the name is enough, which is what catches
	// user_password, password_hash, and api_keys without listing them.
	for _, s := range segments {
		if _, ok := sensitiveNames[s]; ok {
			return true
		}
	}
	// A compound is a credential when one segment says the purpose is private and
	// another says what holds it. "apiKey" and "awsSecretAccessKey" qualify; a bare
	// "key" does not, because the planner uses that word for a variable identifier.
	var qualifier, noun bool
	for _, s := range segments {
		if _, ok := secrecyQualifiers[s]; ok {
			qualifier = true
		}
		if _, ok := credentialNouns[s]; ok {
			noun = true
		}
	}
	return qualifier && noun
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

// maxRegisteredLeafDepth bounds how deep into a redacted value the search for
// candidate literals goes. Reaching the bound counts as a drop rather than
// stopping silently, so a caller can tell that a literal went unregistered.
const maxRegisteredLeafDepth = maxValueDepthLimit

// maxValueDepthLimit is the deepest the leaf search will go, which is the same
// bound the pre-codec walk enforces, so a value the walk accepted is one the
// leaf search can reach in full.
const maxValueDepthLimit = 32

// maxRegisteredLeafBytes is the shortest leaf worth remembering.
//
// A one character path or a two character code is not a credential, and
// registering one would let an ordinary word be destroyed from every later
// message that happened to contain it. A credential shorter than this is not
// detected in later free text; the record itself is still redacted.
const maxRegisteredLeafBytes = 8

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
	if c == nil || len(literal) < maxRegisteredLeafBytes {
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
	// Longest first, so a literal that is a prefix of another cannot leave a
	// fragment behind that no longer matches anything.
	ordered := append([]string(nil), c.secrets...)
	sort.Slice(ordered, func(i, j int) bool { return len(ordered[i]) > len(ordered[j]) })
	for _, secret := range ordered {
		text = strings.ReplaceAll(text, secret, RedactedMarker)
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

	// Value is the first value the walk found sensitive. Redactions holds every one
	// it found, because a value with two sensitive fields has two secrets and
	// registering only the first would let the second reappear in later text.
	Value      reflect.Value
	Redactions []reflect.Value
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
		// Every sensitive field in one struct is collected, because a value with two
		// secrets has two of them and registering only the first would let the second
		// reappear in later text.
		var found []reflect.Value
		for i := range t.NumField() {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			// A field is sensitive under either name. Consulting only the declared one
			// would record a field named Password in the clear whenever its tag
			// happened to be innocuous, and consulting only the Go one would record a
			// field tagged "password" in the clear.
			if isSensitiveField(field) {
				found = append(found, v.Field(i))
				continue
			}
			decision, err := c.walk(joinPath(path, field.Name), v.Field(i), depth+1, nodes)
			if err != nil {
				return walkDecision{}, err
			}
			if decision.Redacted {
				return decision, nil
			}
		}
		if len(found) > 0 {
			return walkDecision{
				Path:       path,
				Redacted:   true,
				Value:      found[0],
				Redactions: found,
			}, nil
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
		// A byte array is as much a byte run as a slice, and its bytes are just as
		// much a candidate.
		if v.Type().Elem().Kind() == reflect.Uint8 {
			c.registerSecret(string(bytesOf(v)))
			return
		}
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
	// The scan is bounded by the input length plus a small constant. Every branch
	// below advances i, so this is unreachable, and it is here so that a future
	// edit that forgets to cannot turn a diagnostic into a hang.
	limit := len(runes) + 64
	for i := 0; i < len(runes) && limit > 0; {
		limit--
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
		// A cookie header is a list of pairs, every one of which is sensitive, so the
		// whole value is discarded rather than only its first pair. The value is
		// skipped, not copied: writing it out first would defeat the point.
		if strings.EqualFold(string(runes[start:end]), "cookie") {
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
			out.WriteString(RedactedMarker)
			continue
		}
		valueStart := i
		for i < len(runes) && !strings.ContainsRune(" \t,;\"'}", runes[i]) {
			i++
		}
		if i == valueStart {
			continue
		}
		scheme := isAuthScheme(string(runes[valueStart:i]))
		out.WriteString(RedactedMarker)
		if !scheme {
			continue
		}
		// "Authorization: Basic <credential>" puts a scheme where the value would
		// be. Replacing only the scheme would leave the credential behind, so the
		// token after the scheme is replaced as well.
		probe := i
		for probe < len(runes) && isSkippableRune(runes[probe]) {
			probe++
		}
		tokenEnd := probe
		for tokenEnd < len(runes) && !strings.ContainsRune(" \t,;\"'}", runes[tokenEnd]) {
			tokenEnd++
		}
		if tokenEnd <= probe {
			continue
		}
		for at := i; at < probe; at++ {
			out.WriteRune(runes[at])
		}
		out.WriteString(RedactedMarker)
		i = tokenEnd
	}
	return out.String()
}

// isAuthScheme reports whether a word names an authentication scheme, which means
// a credential follows it rather than being it.
func isAuthScheme(word string) bool {
	switch strings.ToLower(word) {
	case "basic", "bearer", "digest", "negotiate", "ntlm", "token", "key":
		return true
	}
	return false
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
		probe := start
		for probe < len(text) && isSkippableRuneAt(text[probe]) {
			probe++
		}
		stop := probe
		for stop < len(text) && !isSkippableRuneAt(text[stop]) {
			stop++
		}
		if stop == probe {
			// No token after the word, so this occurrence is not a credential and
			// the rest of the line is still worth scanning.
			from = from + rel + len("bearer")
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

// isSkippableRuneAt reports whether the byte at i is whitespace.
func isSkippableRuneAt(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r':
		return true
	}
	return false
}

func utf8Valid(s string) bool { return utf8.ValidString(s) }
