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
	"strconv"
	"strings"
	"time"
)

var (
	// ErrValueCodecUnavailable reports that no registered codec can represent a
	// value without guessing. Two equally specific codecs are as unresolvable as
	// none, because picking one would silently change what is recorded.
	ErrValueCodecUnavailable = errors.New(`pabtdebug: no unambiguous value codec`)

	// ErrValueRedacted reports a value removed before a codec could see it.
	ErrValueRedacted = errors.New(`pabtdebug: value redacted`)

	// ErrValueUnencodable reports a value the pre-codec walk rejected outright.
	ErrValueUnencodable = errors.New(`pabtdebug: value cannot be encoded`)
)

// ValueVisibility says how faithfully a stored value represents what the
// application actually held.
type ValueVisibility string

const (
	// ValueExact is a value a codec reproduced without loss.
	ValueExact ValueVisibility = "exact"

	// ValueSemantic is a value a codec reproduced in a defined, lossless-for-its-
	// purpose form.
	ValueSemantic ValueVisibility = "semantic"

	// ValueRedacted is a value removed because its path marked it sensitive. The
	// original bytes are not recoverable from the record.
	ValueRedacted ValueVisibility = "redacted"

	// ValueUnknown is a value the application never supplied.
	ValueUnknown ValueVisibility = "unknown"

	// ValueError is a value whose read failed.
	ValueError ValueVisibility = "error"
)

// RedactedMarker is the fixed text that replaces a sensitive value. The stored
// record keeps the digest of this marker and nothing else, so the marker is
// itself not a disclosure.
const RedactedMarker = "REDACTED"

// StoredValue is one recorded value, or the record that one could not be
// recorded.
//
// The digest covers the encoded form, so a consumer can detect a record that has
// been altered after the fact. For a redacted value it covers the marker, which
// proves the value was replaced rather than read.
type StoredValue struct {
	Visibility   ValueVisibility
	ReflectType  string
	CodecID      string
	CodecVersion uint32
	Data         Bytes
	Display      string
	Source       string
	Error        string

	// Redaction names the mechanism that removed a redacted value: a declaration by
	// the application, or a name the recorder recognised. It is empty for a value
	// that was not redacted, so a reader can tell a declared secret from a merely
	// suspicious name rather than having to infer it.
	Redaction string

	SHA256 string
}

// ValueCodec reproduces one kind of value as bytes.
type ValueCodec interface {
	// ID names the codec. It is persisted, so it must be stable across releases.
	ID() string

	// Version distinguishes incompatible encodings of the same type.
	Version() uint32

	// MatchType is the type this codec accepts. A nil MatchType with a nilable
	// codec accepts a nil value of any type.
	MatchType() reflect.Type

	// Encode turns a value into bytes. It must be deterministic.
	Encode(any) (Bytes, error)

	// Decode turns bytes back into a value. It is only called for a known codec.
	Decode(Bytes) (any, error)
}

// ValueLimits bound what a single recorded value may cost.
//
// The limits exist so a hostile or merely careless value cannot make encoding
// unbounded work. They are enforced before a codec is chosen, so a rejected value
// never reaches a codec or a digest.
type ValueLimits struct {
	// MaxValueBytes is the largest encoded value.
	MaxValueBytes uint64

	// MaxValueDepth is the deepest nesting the walk will follow.
	MaxValueDepth uint32

	// MaxValueNodes is the most values and map entries the walk will visit.
	MaxValueNodes uint32

	// MaxDisplayBytes bounds a rendered or diagnostic string.
	MaxDisplayBytes uint32

	// MaxStackFrames bounds the frame descriptions a diagnostic may carry.
	MaxStackFrames uint32
}

// DefaultValueLimits returns limits that suit an interactive local debugger.
func DefaultValueLimits() ValueLimits {
	return ValueLimits{
		MaxValueBytes:   1 << 20,
		MaxValueDepth:   64,
		MaxValueNodes:   10000,
		MaxDisplayBytes: 4096,
		MaxStackFrames:  64,
	}
}

// Validate reports limits that cannot bound anything.
func (l ValueLimits) Validate() error {
	if l.MaxValueBytes == 0 || l.MaxValueDepth == 0 ||
		l.MaxValueNodes == 0 || l.MaxDisplayBytes == 0 || l.MaxStackFrames == 0 {
		return fmt.Errorf(`pabtdebug: incomplete value limits`)
	}
	return nil
}

// CodecRegistry encodes values and records how it did so.
type CodecRegistry struct {
	codecs []ValueCodec
	limits ValueLimits
}

// Register adds a codec.
//
// A later registration of the same ID and version replaces the earlier one, so a
// process that reconfigures itself cannot end up with two codecs claiming the
// same identity.
func (r *CodecRegistry) Register(c ValueCodec) error {
	if r == nil {
		return ErrValueCodecUnavailable
	}
	if c == nil || c.ID() == "" {
		return fmt.Errorf(`pabtdebug: codec needs an id`)
	}
	for i, existing := range r.codecs {
		if existing.ID() == c.ID() && existing.Version() == c.Version() {
			r.codecs[i] = c
			return nil
		}
	}
	r.codecs = append(r.codecs, c)
	return nil
}

// Codecs returns the registered codecs in registration order, for persistence.
func (r *CodecRegistry) Codecs() []ValueCodec {
	if r == nil {
		return nil
	}
	return append([]ValueCodec(nil), r.codecs...)
}

// Encode records one value.
//
// The pre-codec walk runs first, so a sensitive value is removed before any codec
// and before any digest can see it. An exact type match wins; failing that, the
// unique most specific assignable codec wins; a tie is refused rather than
// resolved arbitrarily.
func (r *CodecRegistry) Encode(ctx context.Context, source string, v any) (StoredValue, error) {
	if err := ctx.Err(); err != nil {
		return StoredValue{}, err
	}
	out := StoredValue{Source: source, ReflectType: typeName(v)}
	if v == nil {
		out.Visibility = ValueUnknown
		out.Display = "<nil>"
		return finishValue(&out)
	}
	redactor, san := redactionFor(ctx)
	// The walk starts at an empty path so the path it reports is relative to the
	// value. The origin label is recorded separately; prefixing it would make the
	// path unusable for locating the value again.
	decision, err := redactor.walk("", reflect.ValueOf(v), 0, 0)
	if err != nil {
		if errors.Is(err, ErrValueRedacted) {
			return StoredValue{}, err
		}
		out.Visibility = ValueError
		out.Error = redactor.scrub(san.Sanitize(err.Error()))
		return finishValue(&out)
	}
	if decision.Redacted {
		// A redacted value keeps the shape of everything that was safe. Replacing
		// the whole record with a marker would hide the safe fields too, which is
		// its own kind of dishonesty.
		out.Visibility = ValueRedacted
		out.Source = joinPath(source, decision.Path)
		out.CodecID = structureCodecID
		out.CodecVersion = 1
		out.Redaction = decision.reason()
		out.Display = RedactedMarker
		projection, err := projectRedacted(redactor.limits, reflect.ValueOf(v), 0, 0)
		if err != nil {
			return StoredValue{}, err
		}
		data, err := CanonicalEncode(projection)
		if err != nil {
			return StoredValue{}, err
		}
		if uint64(len(data)) > redactor.limits.MaxValueBytes {
			return StoredValue{}, fmt.Errorf("%w: value is larger than %d bytes",
				ErrValueUnencodable, redactor.limits.MaxValueBytes)
		}
		out.Data = data
		// The literal is registered so a later diagnostic carrying the same text
		// is scrubbed too. It is held in memory only and is never persisted.
		// Every sensitive value the walk found contributes a literal, at any depth,
		// so a value with two secrets cannot leak the second through later text.
		for _, r := range decision.Redactions {
			redactor.registerLeaves(r.Value)
		}
		return finishValue(&out)
	}
	codec, err := r.selectCodec(reflect.TypeOf(v))
	if err != nil {
		return StoredValue{}, err
	}
	data, err := encodeWithCodec(codec, v)
	if err != nil {
		out.Visibility = ValueError
		// The scrubber knows the literals the walk saw, so a failure that repeats
		// one is cleaned here. Without this, an error is the one place a secret
		// could reach the record.
		out.Error = redactor.scrub(san.Sanitize(err.Error()))
		return finishValue(&out)
	}
	out.CodecID = codec.ID()
	out.CodecVersion = codec.Version()
	out.Data = data
	out.Visibility = codecVisibility(codec)
	out.Display = san.Sanitize(displayOf(v))
	// The display rendering is derived from the value, so it can repeat a secret
	// that the walk did not mark. Scrubbing it here means the record and the
	// response carry the same text.
	out.Display = redactor.scrub(out.Display)
	return finishValue(&out)
}

// encodeWithCodec runs a codec, converting a panic into an error.
//
// A panicking codec is application-adjacent code whose panic text can carry
// anything, including a secret. Recovering here is what keeps that text inside
// the scrubber instead of unwinding through the caller's frame.
func encodeWithCodec(c ValueCodec, v any) (data Bytes, err error) {
	defer func() {
		if r := recover(); r != nil {
			data = nil
			err = fmt.Errorf("%w: codec %s panicked: %v", ErrValueUnencodable, c.ID(), r)
		}
	}()
	return c.Encode(v)
}

var (
	timeType      = reflect.TypeOf(time.Time{})
	timestampType = reflect.TypeOf(Timestamp(time.Time{}))
)

// bytesOf returns a byte slice or byte array's bytes.
func bytesOf(v reflect.Value) []byte {
	if v.Kind() == reflect.Slice {
		return v.Bytes()
	}
	out := make([]byte, v.Len())
	for i := range v.Len() {
		out[i] = byte(v.Index(i).Uint())
	}
	return out
}

// selectCodec resolves a codec for t.
//
// An exact type match wins. Failing that the structure codec is used, which
// records the value as canonical JSON. That is a defined form rather than a
// reproduction of the Go type, so it is recorded as semantic and never as exact.
func (r *CodecRegistry) selectCodec(t reflect.Type) (ValueCodec, error) {
	if r == nil {
		return nil, ErrValueCodecUnavailable
	}
	for _, c := range r.codecs {
		if c.MatchType() != nil && c.MatchType() == t {
			return c, nil
		}
	}
	for _, c := range r.codecs {
		if c.MatchType() == nil {
			return c, nil
		}
	}
	return nil, fmt.Errorf("%w: no codec matches %s", ErrValueCodecUnavailable, t)
}

type structureCodec struct{}

func (structureCodec) ID() string      { return structureCodecID }
func (structureCodec) Version() uint32 { return 1 }

// MatchType is nil because this codec accepts no exact type. It is reached only
// after no exact codec matched.
func (structureCodec) MatchType() reflect.Type { return nil }
func (structureCodec) Encode(v any) (Bytes, error) {
	projected, err := projectStructure(reflect.ValueOf(v), 0, 0)
	if err != nil {
		return nil, err
	}
	return CanonicalEncode(projected)
}

func (structureCodec) Decode(b Bytes) (any, error) {
	var out any
	if err := CanonicalDecode(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func codecVisibility(c ValueCodec) ValueVisibility {
	if c.ID() == structureCodecID {
		return ValueSemantic
	}
	return ValueExact
}

func typeName(v any) string {
	if v == nil {
		return ""
	}
	return reflect.TypeOf(v).String()
}

func displayOf(v any) string {
	if s, ok := v.(fmt.Stringer); ok {
		return s.String()
	}
	return fmt.Sprintf("%v", v)
}

// projectedName returns the key a field is recorded under.
//
// The declared wire name is preferred when there is one, because that is the name
// the application already chose and it round trips through the canonical encoder
// unchanged. Otherwise the field name is normalised.
//
// The name is made unique within its container. Two fields can normalise to the
// same string, and letting them collide would drop one of them without trace and
// assert a value the application never held.
func projectedName(f reflect.StructField, taken map[string]bool) string {
	name := declaredName(f)
	if name == "" {
		return ""
	}
	if !taken[name] {
		return name
	}
	normalised := typeNameKey(name)
	if !taken[normalised] {
		return normalised
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s_%d", normalised, i)
		if !taken[candidate] {
			return candidate
		}
	}
}

// isSensitiveField reports whether a field is a credential under either its Go
// name or its declared wire name.
func isSensitiveField(f reflect.StructField) bool {
	return IsSensitiveSegment(f.Name) || IsSensitiveSegment(declaredName(f))
}

// fieldRedactionReason returns why a field is redacted, or an empty string when it
// is not. A declaration outranks the name, so a value that declares itself a
// secret is recorded as declared even when its field name also looks sensitive.
func fieldRedactionReason(f reflect.StructField, v reflect.Value) string {
	if declaredSensitive(v) {
		return RedactionDeclared
	}
	if isSensitiveField(f) {
		return RedactionInferred
	}
	return ""
}

// declaredName returns the name a field is recorded under: the declared wire name
// when there is one, otherwise the field name. An empty result means the field is
// excluded from the record.
func declaredName(f reflect.StructField) string {
	name := f.Name
	tag, ok := f.Tag.Lookup("json")
	if !ok {
		return name
	}
	if comma := strings.IndexByte(tag, ','); comma >= 0 {
		tag = tag[:comma]
	}
	switch tag {
	case "-":
		return ""
	case "":
		return name
	}
	return tag
}

// putProjected stores one projected entry, refusing to overwrite another.
func putProjected(into map[string]any, name string, value any) error {
	if name == "" {
		return nil
	}
	if _, clash := into[name]; clash {
		return fmt.Errorf("%w: two entries project onto the name %q", ErrValueUnencodable, name)
	}
	into[name] = value
	return nil
}

// typeNameKey normalises a path segment to lower snake case so that a field
// named ApiKey, apiKey, or api_key is recognised as the same segment.
func typeNameKey(segment string) string {
	var b strings.Builder
	var prevLower bool
	for i, r := range segment {
		switch {
		case r >= 'A' && r <= 'Z':
			if i > 0 && prevLower {
				b.WriteByte('_')
			}
			b.WriteRune(r - 'A' + 'a')
			prevLower = false
		case r == '-' || r == ' ' || r == '.':
			b.WriteByte('_')
			prevLower = false
		default:
			b.WriteRune(r)
			prevLower = r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		}
	}
	return b.String()
}

// finishValue stamps the digest, which is always over the record as it will be
// stored, with the digest field itself cleared.
func finishValue(out *StoredValue) (StoredValue, error) {
	digest, err := HashValue(*out)
	if err != nil {
		return StoredValue{}, err
	}
	out.SHA256 = digest
	return *out, nil
}

// projectStructure rebuilds a value as plain maps, slices, and scalars, so that
// canonical JSON can carry it.
func projectStructure(v reflect.Value, depth, nodes uint32) (any, error) {
	limits := DefaultValueLimits()
	if depth > limits.MaxValueDepth {
		return nil, fmt.Errorf("%w: value nested deeper than %d", ErrValueUnencodable, limits.MaxValueDepth)
	}
	nodes++
	if nodes > limits.MaxValueNodes {
		return nil, fmt.Errorf("%w: value has more than %d values", ErrValueUnencodable, limits.MaxValueNodes)
	}
	if !v.IsValid() {
		return nil, nil
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil, nil
		}
		return projectStructure(v.Elem(), depth+1, nodes)
	case reflect.Struct:
		// A struct whose every field is unexported is a time or a similar opaque
		// value. Projecting it to an empty object would record total loss while
		// reporting the value as semantic, so it is recorded by its canonical text.
		if v.Type() == timeType {
			return v.Interface().(time.Time).UTC().Format(time.RFC3339Nano), nil
		}
		if v.Type() == timestampType {
			return time.Time(v.Interface().(Timestamp)).UTC().Format(time.RFC3339Nano), nil
		}
		t := v.Type()
		out := make(map[string]any, t.NumField())
		taken := make(map[string]bool, t.NumField())
		for i := range t.NumField() {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			name := projectedName(field, taken)
			if name == "" {
				continue
			}
			taken[name] = true
			child, err := projectStructure(v.Field(i), depth+1, nodes)
			if err != nil {
				return nil, err
			}
			if err := putProjected(out, name, child); err != nil {
				return nil, err
			}
		}
		return out, nil
	case reflect.Map:
		out := make(map[string]any, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			if iter.Key().Kind() != reflect.String {
				return nil, fmt.Errorf("%w: map key of kind %s",
					ErrValueUnencodable, iter.Key().Kind())
			}
			child, err := projectStructure(iter.Value(), depth+1, nodes)
			if err != nil {
				return nil, err
			}
			if err := putProjected(out, iter.Key().String(), child); err != nil {
				return nil, err
			}
		}
		return out, nil
	case reflect.Slice, reflect.Array:
		// A byte run is recorded as bytes, not as text. Converting it to a string
		// would be lossy for any sequence that is not valid UTF-8, and two
		// different sequences would then share a digest.
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return Bytes(bytesOf(v)), nil
		}
		out := make([]any, 0, v.Len())
		for i := range v.Len() {
			child, err := projectStructure(v.Index(i), depth+1, nodes)
			if err != nil {
				return nil, err
			}
			out = append(out, child)
		}
		return out, nil
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Interface(), nil
	case reflect.Float32, reflect.Float64:
		return v.Interface(), nil
	default:
		return nil, fmt.Errorf("%w: kind %s has no canonical form",
			ErrValueUnencodable, v.Kind())
	}
}

// projectRedacted rebuilds a value with every sensitive leaf replaced by the
// marker and everything else preserved.
func projectRedacted(limits ValueLimits, v reflect.Value, depth, nodes uint32) (any, error) {
	if depth > limits.MaxValueDepth {
		return nil, fmt.Errorf("%w: value nested deeper than %d", ErrValueUnencodable, limits.MaxValueDepth)
	}
	nodes++
	if nodes > limits.MaxValueNodes {
		return nil, fmt.Errorf("%w: value has more than %d values", ErrValueUnencodable, limits.MaxValueNodes)
	}
	if !v.IsValid() {
		return nil, nil
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil, nil
		}
		return projectRedacted(limits, v.Elem(), depth+1, nodes)
	case reflect.Struct:
		if v.Type() == timeType {
			return v.Interface().(time.Time).UTC().Format(time.RFC3339Nano), nil
		}
		if v.Type() == timestampType {
			return time.Time(v.Interface().(Timestamp)).UTC().Format(time.RFC3339Nano), nil
		}
		t := v.Type()
		out := make(map[string]any, t.NumField())
		taken := make(map[string]bool, t.NumField())
		for i := range t.NumField() {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			name := projectedName(field, taken)
			if name == "" {
				continue
			}
			taken[name] = true
			// The same rule the walk used, declaration included, so the projection
			// replaces exactly what the walk decided was sensitive.
			if fieldRedactionReason(field, v.Field(i)) != "" {
				if err := putProjected(out, name, RedactedMarker); err != nil {
					return nil, err
				}
				continue
			}
			child, err := projectRedacted(limits, v.Field(i), depth+1, nodes)
			if err != nil {
				return nil, err
			}
			if err := putProjected(out, name, child); err != nil {
				return nil, err
			}
		}
		return out, nil
	case reflect.Map:
		out := make(map[string]any, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			segment := iter.Key().String()
			if IsSensitiveSegment(segment) || declaredSensitive(iter.Value()) {
				if err := putProjected(out, segment, RedactedMarker); err != nil {
					return nil, err
				}
				continue
			}
			child, err := projectRedacted(limits, iter.Value(), depth+1, nodes)
			if err != nil {
				return nil, err
			}
			if err := putProjected(out, segment, child); err != nil {
				return nil, err
			}
		}
		return out, nil
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return Bytes(bytesOf(v)), nil
		}
		out := make([]any, 0, v.Len())
		for i := range v.Len() {
			child, err := projectRedacted(limits, v.Index(i), depth+1, nodes)
			if err != nil {
				return nil, err
			}
			out = append(out, child)
		}
		return out, nil
	default:
		return v.Interface(), nil
	}
}

// --- built-in codecs ---

const (
	structureCodecID = "structure/v1"
	stringCodecID    = "string/v1"
	bytesCodecID     = "bytes/v1"
	boolCodecID      = "bool/v1"
	intCodecID       = "int/v1"
	floatCodecID     = "float/v1"
)

type stringCodec struct{}

func (stringCodec) ID() string              { return stringCodecID }
func (stringCodec) Version() uint32         { return 1 }
func (stringCodec) MatchType() reflect.Type { return reflect.TypeOf("") }
func (stringCodec) Encode(v any) (Bytes, error) {
	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf(`pabtdebug: string codec given %T`, v)
	}
	return Bytes(s), nil
}
func (stringCodec) Decode(b Bytes) (any, error) { return string(b), nil }

type bytesCodec struct{}

func (bytesCodec) ID() string              { return bytesCodecID }
func (bytesCodec) Version() uint32         { return 1 }
func (bytesCodec) MatchType() reflect.Type { return reflect.TypeOf(Bytes(nil)) }
func (bytesCodec) Encode(v any) (Bytes, error) {
	switch t := v.(type) {
	case Bytes:
		return t, nil
	case []byte:
		return Bytes(t), nil
	}
	return nil, fmt.Errorf(`pabtdebug: bytes codec given %T`, v)
}
func (bytesCodec) Decode(b Bytes) (any, error) { return b, nil }

type boolCodec struct{}

func (boolCodec) ID() string              { return boolCodecID }
func (boolCodec) Version() uint32         { return 1 }
func (boolCodec) MatchType() reflect.Type { return reflect.TypeOf(false) }
func (boolCodec) Encode(v any) (Bytes, error) {
	b, ok := v.(bool)
	if !ok {
		return nil, fmt.Errorf(`pabtdebug: bool codec given %T`, v)
	}
	if b {
		return Bytes("true"), nil
	}
	return Bytes("false"), nil
}
func (boolCodec) Decode(b Bytes) (any, error) { return string(b) == "true", nil }

type intCodec struct{}

func (intCodec) ID() string              { return intCodecID }
func (intCodec) Version() uint32         { return 1 }
func (intCodec) MatchType() reflect.Type { return reflect.TypeOf(int64(0)) }
func (intCodec) Encode(v any) (Bytes, error) {
	switch t := v.(type) {
	case int64:
		return Bytes(strconv.FormatInt(t, 10)), nil
	case int:
		return Bytes(strconv.FormatInt(int64(t), 10)), nil
	}
	return nil, fmt.Errorf(`pabtdebug: int codec given %T`, v)
}
func (intCodec) Decode(b Bytes) (any, error) {
	n, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%w: int codec: %v", ErrValueUnencodable, err)
	}
	return n, nil
}

type floatCodec struct{}

func (floatCodec) ID() string              { return floatCodecID }
func (floatCodec) Version() uint32         { return 1 }
func (floatCodec) MatchType() reflect.Type { return reflect.TypeOf(float64(0)) }
func (floatCodec) Encode(v any) (Bytes, error) {
	f, ok := v.(float64)
	if !ok {
		return nil, fmt.Errorf(`pabtdebug: float codec given %T`, v)
	}
	if f != f {
		return nil, fmt.Errorf(`pabtdebug: float codec given NaN`)
	}
	return Bytes(strconv.FormatFloat(f, 'g', -1, 64)), nil
}
func (floatCodec) Decode(b Bytes) (any, error) {
	f, err := strconv.ParseFloat(string(b), 64)
	if err != nil {
		return nil, fmt.Errorf("%w: float codec: %v", ErrValueUnencodable, err)
	}
	return f, nil
}

// NewCodecRegistry returns a registry holding the built-in codecs.
//
// The built-ins cover the types a planner's state is likely to use. A value with
// no exact codec falls back to rendering, which records that fact in the
// visibility rather than pretending the value was reproduced.
func NewCodecRegistry(limits ValueLimits) *CodecRegistry {
	if err := limits.Validate(); err != nil {
		limits = DefaultValueLimits()
	}
	r := &CodecRegistry{limits: limits}
	for _, c := range []ValueCodec{
		stringCodec{}, bytesCodec{}, boolCodec{}, intCodec{}, floatCodec{},
		structureCodec{},
	} {
		// The built-ins cannot fail registration, so the error is not actionable.
		_ = r.Register(c)
	}
	return r
}
