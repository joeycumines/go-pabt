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

// Sensitive is the optional interface an application implements on one of its own
// types to declare that the type's values are secrets.
//
// The name-based rule below is a fallback, and it has to be a guess: a field
// named Password must be redacted even when the application never said anything,
// because a debugger cannot ask. A guess is also imprecise, and six review
// passes established it cannot be both complete and precise. A declaration
// replaces the guess, so an application whose field names say nothing, such as a
// database source named Field, still gets a guarantee rather than a heuristic.
//
// The record says which mechanism decided, so a reader can tell a declared
// secret from a merely suspicious name.
type Sensitive interface {
	// PabtSensitive reports whether this value is a secret. It runs before any codec
	// and before any digest, so it must be cheap, must not have side effects, and
	// must not reveal the value it is protecting.
	PabtSensitive() bool
}

// Redaction mechanisms, recorded on a redacted value so a consumer can tell an
// application that declared a secret from a name the recorder happened to
// recognise.
const (
	// RedactionDeclared means the value's own type declared it a secret.
	RedactionDeclared = "declared_by_application"

	// RedactionInferred means the recorder recognised the name and nothing else.
	RedactionInferred = "inferred_from_name"
)

// declaredSensitive reports whether a value declares itself a secret.
//
// Both the value and its address are tried, because a method with a pointer
// receiver is only in the method set of the pointer, and a field of a struct
// reached through an interface is commonly a pointer.
func declaredSensitive(v reflect.Value) bool {
	if !v.IsValid() {
		return false
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return false
		}
		if declared, ok := v.Interface().(Sensitive); ok {
			return declared.PabtSensitive()
		}
		return declaredSensitive(v.Elem())
	default:
		if v.CanInterface() {
			if declared, ok := v.Interface().(Sensitive); ok {
				return declared.PabtSensitive()
			}
		}
		if v.CanAddr() {
			if declared, ok := v.Addr().Interface().(Sensitive); ok {
				return declared.PabtSensitive()
			}
		}
		return false
	}
}

// The sensitivity rule below is the fallback, in one place.
//
// A segment is a credential when a credential word appears in the name, when the
// name combines something that says the purpose is private with something that
// says what holds it, or when it is a credential noun qualified by a purpose that
// is not on the ordinary list.
//
// Deciding this from one hand-written list of compounds does not work. The first
// four attempts at this rule each fixed one class of name and lost another, so it
// is now two conservative rules plus a short list of the ordinary words that would
// otherwise be swallowed.

// sensitiveNames are words that are a credential wherever they appear in a name.
// Matching them in any position is what covers user_password, password_hash, and
// api_keys without listing those.
var sensitiveNames = map[string]struct{}{
	"password": {}, "passwords": {}, "passwd": {},
	"passphrase": {}, "passphrases": {},
	"secret": {}, "secrets": {},
	"credential": {}, "credentials": {},
	"authorization": {}, "authorisation": {},
	"cookie": {}, "cookies": {},
	"token": {}, "tokens": {},
	"pass": {},
	// Credential classes with no separable purpose and holder. A data source name
	// carries a database password, a session id is a bearer credential when it is
	// not an id, and a PKCE verifier is live until it is exchanged.
	"dsn": {}, "jwt": {}, "totp": {},
	"setcookie": {}, "proxyauthorization": {}, "proxyauthorizationheader": {},
	"codeverifier": {}, "codechallenge": {},
	"authheader": {}, "basicauth": {},
	"sessionid": {}, "connectionstring": {},
	"keymaterial": {}, "keybytes": {},
	"mnemonicbackup": {}, "seedbackup": {},
	"serviceaccountkey": {},
}

// sensitiveExact are words that are a credential only as the whole name, because
// they are also ordinary nouns: a saltiness, a signatureAlgorithm, and a barcode
// are ordinary data.
var sensitiveExact = map[string]struct{}{
	"salt": {}, "seed": {}, "seeds": {}, "mnemonic": {}, "mnemonics": {},
	"signature": {}, "signatures": {},
	"pin": {}, "pins": {}, "otp": {},
	"answer": {}, "answers": {}, "hint": {}, "hints": {},
}

// secrecyQualifiers say a purpose whose value is meant to stay private. One of
// these alongside a credential noun is enough.
var secrecyQualifiers = map[string]struct{}{
	"api": {}, "access": {}, "secret": {}, "private": {},
	"encryption": {}, "encrypt": {}, "signing": {}, "sign": {},
	"auth": {}, "authn": {}, "authorization": {}, "master": {},
	"refresh": {}, "bearer": {}, "jwt": {}, "sso": {}, "hmac": {},
	"webhook": {}, "smtp": {}, "customer": {}, "tenant": {},
	"pepper": {}, "aws": {}, "azure": {}, "gcp": {},
	"session": {}, "service": {}, "system": {}, "app": {}, "application": {},
	"ssh": {}, "gpg": {}, "kms": {}, "pgp": {}, "deploy": {}, "deployment": {},
	"license": {}, "client": {}, "backup": {}, "wrapped": {},
	"aes": {}, "rsa": {}, "ecdsa": {}, "ed25519": {}, "x25519": {},
	"id": {}, "pass": {}, "seed": {}, "mnemonic": {}, "vault": {}, "keystore": {},
	"csrf": {}, "xsrf": {}, "mfa": {}, "recovery": {}, "magic": {},
	"db": {}, "salt": {}, "root": {}, "admin": {}, "ops": {}, "ci": {}, "env": {},
	"pin": {}, "otp": {},
	// "dsn" and "connection" carry a database password inside a URI.
	"dsn": {}, "connection": {},
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
	"nonce": {}, "material": {}, "string": {}, "bytes": {}, "data": {},
	// N3: a BIP39 backup phrase is the highest-value secret in a crypto system.
	"backup": {}, "list": {}, "copy": {},
}

// ordinaryKeyQualifiers are purposes that end in "key" without being private. A
// sort key, a primary key, and a cache key are ordinary state, and redacting them
// would destroy the record a debugger exists to produce.
var ordinaryKeyQualifiers = map[string]struct{}{
	"sort": {}, "primary": {}, "partition": {}, "cache": {}, "object": {},
	"row": {}, "idempotency": {}, "pubsub": {}, "foreign": {}, "sharding": {},
	"index": {}, "group": {}, "public": {}, "answer": {}, "field": {},
	"column": {}, "config": {}, "setting": {}, "map": {}, "registry": {},
	"alternate": {}, "surrogate": {}, "composite": {}, "cluster": {},
	"hash": {}, "digest": {}, "checksum": {}, "value": {},
}

// IsSensitiveSegment reports whether a path segment marks a secret, whatever
// casing, separators, or trailing digits the application used to write it.
//
// A map key is a path segment even when the struct field holding it is not, so a
// map keyed by an access token is redacted for the same reason a field named
// AccessToken is.
func IsSensitiveSegment(segment string) bool {
	name := normaliseSegment(segment)
	if name == "" {
		return false
	}
	// A trailing index is not part of a whole name either, so mnemonic1 is still a
	// mnemonic while seedCount is not a seed.
	if _, ok := sensitiveExact[name]; ok {
		return true
	}
	// The same applies to a whole name that is only an index away from one.
	if trimmed := strings.TrimRight(lastSegment(name), "0123456789"); trimmed != "" {
		if _, ok := sensitiveExact[trimmed]; ok {
			return true
		}
	}
	// A holder in the last position is nearly always private; the ordinary
	// exceptions are the qualifiers that do not make it so.
	if strings.HasSuffix(name, "_key") || strings.HasSuffix(name, "_keys") {
		head := strings.TrimSuffix(strings.TrimSuffix(name, "_keys"), "_key")
		if _, ordinary := ordinaryKeyQualifiers[lastSegment(head)]; !ordinary {
			return true
		}
	}
	if _, ok := sensitiveNames[strings.ReplaceAll(name, "_", "")]; ok {
		return true
	}
	words := segmentWords(name)
	for _, w := range words {
		if _, ok := sensitiveNames[w]; ok {
			return true
		}
	}
	// The purpose and the holder must be two different words. Letting one word fill
	// both roles would make seedCount and secretPlant credentials, and would miss
	// seed_phrase, where "seed" is both a purpose and a holder.
	qualifierAt := -1
	for i, w := range words {
		if _, ok := secrecyQualifiers[w]; ok {
			qualifierAt = i
			break
		}
	}
	if qualifierAt < 0 {
		return false
	}
	for i, w := range words {
		if i == qualifierAt {
			continue
		}
		if _, ok := credentialNouns[w]; ok {
			return true
		}
	}
	return false
}

func lastSegment(name string) string {
	if i := strings.LastIndexByte(name, '_'); i >= 0 {
		return name[i+1:]
	}
	return name
}

// segmentWords breaks a normalised name into the words to look up.
//
// Three things are handled here rather than in the rule, because each of them made
// a real name invisible: a trailing index, as in password1 or apiKey2; a run of
// capitals, which normaliseSegment already splits; and a qualifier glued to a
// holder with no separator at all, as in apikey.
func segmentWords(name string) []string {
	parts := strings.Split(name, "_")
	out := make([]string, 0, len(parts)*2)
	for _, part := range parts {
		if part == "" {
			continue
		}
		// A trailing index is not part of the word: password1 is a password.
		trimmed := strings.TrimRight(part, "0123456789")
		if trimmed == "" {
			trimmed = part
		}
		out = append(out, trimmed)
		out = append(out, splitQualifierFromNoun(trimmed)...)
	}
	return out
}

// maxSplittableWord is the longest word the glued split will consider. The
// longest purpose in the table is "authorization" and the longest holder is
// "mnemonics", so nothing longer can be a purpose/holder pair.
const maxSplittableWord = 24

// splitQualifierFromNoun separates a glued pair such as "apikey" into its two
// words, so a name written without any separator is still recognised.
//
// The purpose may itself carry a prefix, which is how an env-style xapikey is
// read: the split is taken where the remainder is a credential noun, and the
// purpose is whatever the head ends with.
func splitQualifierFromNoun(word string) []string {
	// A word long enough to be application data rather than a name is not split.
	// Every purpose and holder in the tables above is short, so a word beyond this
	// cannot be one of them, and trying every position of an unbounded word is
	// quadratic on a path that takes application-controlled text.
	if len(word) > maxSplittableWord {
		return nil
	}
	for i := 1; i < len(word); i++ {
		head, tail := word[:i], word[i:]
		if !isCredentialNoun(tail) {
			continue
		}
		if _, ok := secrecyQualifiers[head]; ok {
			return []string{head, tail}
		}
		for qualifier := range secrecyQualifiers {
			if strings.HasSuffix(head, qualifier) {
				return []string{qualifier, tail}
			}
		}
		// The purpose may itself be two words written together, as in
		// serviceaccountkey and proxyauthorization. Each purpose of at least two
		// characters is tried against the head, which keeps this bounded by the
		// cap above rather than by the length of the word.
		for j := 2; j < len(head); j++ {
			if _, ok := secrecyQualifiers[head[j:]]; ok {
				return []string{head[j:], tail}
			}
		}
	}
	return nil
}

// isCredentialNoun reports whether a word is something a secret is held in,
// allowing for a plural that the noun table does not list.
func isCredentialNoun(word string) bool {
	if _, ok := credentialNouns[word]; ok {
		return true
	}
	return strings.HasSuffix(word, "s") && isCredentialNoun(strings.TrimSuffix(word, "s"))
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

// maxLeafSearchDepth bounds how deep into a redacted value the search for
// candidate literals goes.
//
// It is deliberately half the walk's default limit of 64, because the search
// visits every leaf of a value rather than one path through it. A sensitive field
// whose value is a container deeper than this is still fully redacted in the
// record; only its deepest literals go unregistered, so they could survive in
// later free text. That is the stated cost, not a claim that the record leaks.
const maxLeafSearchDepth = 32

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

	// Redacted reports that some part of the value was found sensitive.
	Redacted bool

	// Redactions holds every sensitive value the walk found, at any depth, in any
	// order.
	//
	// The walk does not stop at the first one. A value with a direct Password and a
	// nested secret has two of them, and returning early discarded the outer one,
	// which then reappeared verbatim in a later diagnostic and was signed by that
	// record's digest. Map iteration made the survivor vary per run.
	Redactions []redaction
}

// redaction is one sensitive value and the mechanism that found it.
type redaction struct {
	Value  reflect.Value
	Reason string
}

// reason reports the mechanism to record, preferring a declaration because it is
// the stronger statement.
func (d walkDecision) reason() string {
	for _, r := range d.Redactions {
		if r.Reason == RedactionDeclared {
			return RedactionDeclared
		}
	}
	if len(d.Redactions) > 0 {
		return RedactionInferred
	}
	return ""
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
	// A declaration outranks any name, and it applies to a value that is not a field
	// at all: a slice of a declared type is declared wherever it appears.
	if declaredSensitive(v) {
		return walkDecision{
			Path:       path,
			Redacted:   true,
			Redactions: []redaction{{Value: v, Reason: RedactionDeclared}},
		}, nil
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return walkDecision{Path: path}, nil
		}
		return c.walk(path, v.Elem(), depth+1, nodes)
	case reflect.Map:
		// Every key is examined, so two sensitive keys both contribute. Returning
		// on the first made the survivor depend on Go's map iteration order.
		var found []redaction
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
				found = append(found, redaction{Value: iter.Value(), Reason: RedactionInferred})
				continue
			}
			decision, err := c.walk(child, iter.Value(), depth+1, nodes)
			if err != nil {
				return walkDecision{}, err
			}
			found = append(found, decision.Redactions...)
		}
		return walkDecision{Path: path, Redacted: len(found) > 0, Redactions: found}, nil
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Uint8 {
			return walkDecision{Path: path}, nil
		}
		// Every element is visited, so a second secret in a slice is found even
		// though the first one already made the value sensitive.
		var found []redaction
		for i := range v.Len() {
			decision, err := c.walk(joinPath(path, fmt.Sprint(i)), v.Index(i), depth+1, nodes)
			if err != nil {
				return walkDecision{}, err
			}
			found = append(found, decision.Redactions...)
		}
		return walkDecision{Path: path, Redacted: len(found) > 0, Redactions: found}, nil
	case reflect.Struct:
		t := v.Type()
		// Every sensitive field in one struct is collected, because a value with two
		// secrets has two of them and registering only the first would let the second
		// reappear in later text.
		var found []redaction
		for i := range t.NumField() {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			// A field is sensitive under either name. Consulting only the declared one
			// would record a field named Password in the clear whenever its tag
			// happened to be innocuous, and consulting only the Go one would record a
			// field tagged "password" in the clear.
			if reason := fieldRedactionReason(field, v.Field(i)); reason != "" {
				found = append(found, redaction{Value: v.Field(i), Reason: reason})
				continue
			}
			decision, err := c.walk(joinPath(path, field.Name), v.Field(i), depth+1, nodes)
			if err != nil {
				return walkDecision{}, err
			}
			// A nested secret is kept alongside the outer ones. Returning it on its
			// own discarded whatever had already been found, so a value with a
			// direct Password and a nested secret registered only the nested one.
			found = append(found, decision.Redactions...)
		}
		if len(found) > 0 {
			return walkDecision{Path: path, Redacted: true, Redactions: found}, nil
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

// registerLeaves remembers the text of every scalar leaf under a redacted value.
//
// A redacted value is often a whole struct or map rather than a single string.
// Registering only its top would leave its leaves able to reappear in a later
// diagnostic, so every leaf is registered.
func (c *redactionContext) registerLeaves(v reflect.Value) {
	c.registerValueLeaves(v, 0)
}

func (c *redactionContext) registerValueLeaves(v reflect.Value, depth uint32) {
	if c == nil || !v.IsValid() {
		return
	}
	if depth > maxLeafSearchDepth {
		// Crossing the bound is counted rather than stopping silently, and the count
		// is readable, so a caller can tell a literal went unregistered.
		c.droppedSecrets++
		return
	}
	if c.droppedSecrets >= maxRegisteredSecrets {
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
