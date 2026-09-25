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
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrCanonicalEncoding reports a value that cannot be encoded canonically.
	ErrCanonicalEncoding = errors.New(`pabtdebug: value cannot be encoded canonically`)

	// ErrCanonicalDecoding reports bytes that are not a single canonical value.
	ErrCanonicalDecoding = errors.New(`pabtdebug: malformed canonical value`)
)

// Bytes encodes as unpadded standard base64.
//
// encoding/json renders a []byte with padding, so a digest computed over a
// record would depend on the padding. This type removes that degree of freedom.
type Bytes []byte

// MarshalJSON implements json.Marshaler.
//
// A nil Bytes and an empty one are different facts, and they are written
// differently so they cannot share a digest. encoding/json would render both as
// the empty string.
func (b Bytes) MarshalJSON() ([]byte, error) {
	if b == nil {
		return []byte("null"), nil
	}
	return json.Marshal(base64.RawStdEncoding.EncodeToString(b))
}

// UnmarshalJSON implements json.Unmarshaler.
func (b *Bytes) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*b = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("%w: byte field is not a string: %v", ErrCanonicalDecoding, err)
	}
	raw, err := base64.RawStdEncoding.DecodeString(s)
	if err != nil {
		return fmt.Errorf("%w: byte field is not unpadded base64: %v", ErrCanonicalDecoding, err)
	}
	*b = raw
	return nil
}

// Timestamp encodes in UTC with nanosecond precision.
//
// A recorded instant is the same fact whatever offset it was captured in, so
// the wire form is normalised. Two captures of the same instant must produce the
// same bytes or their digests would differ for no reason.
type Timestamp time.Time

// MarshalJSON implements json.Marshaler.
func (t Timestamp) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Time(t).UTC().Format(time.RFC3339Nano))
}

// UnmarshalJSON implements json.Unmarshaler.
func (t *Timestamp) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("%w: timestamp is not a string: %v", ErrCanonicalDecoding, err)
	}
	parsed, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return fmt.Errorf("%w: timestamp %q: %v", ErrCanonicalDecoding, s, err)
	}
	*t = Timestamp(parsed.UTC())
	return nil
}

// Time returns the instant as a time.Time.
func (t Timestamp) Time() time.Time { return time.Time(t) }

// CanonicalEncode renders v as canonical JSON.
//
// Canonical means: object keys sorted by their bytes, no HTML escaping, no
// insignificant whitespace, integers kept as written with no float round trip,
// absent nullable values as an explicit null, and a nil slice or map as null
// while a non-nil empty one stays empty. Every property exists so that a digest
// depends only on the fact being recorded, not on how it happened to be written.
func CanonicalEncode(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCanonicalEncoding, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCanonicalEncoding, err)
	}
	var b strings.Builder
	if err := writeCanonical(&b, generic); err != nil {
		return nil, err
	}
	return []byte(b.String()), nil
}

// CanonicalDecode parses canonical JSON into target, strictly.
//
// Unknown fields, a second value after the first, and a non-object or non-array
// where a struct is expected are all rejected, because accepting them would let
// two different byte strings decode to the same value and break a digest.
func CanonicalDecode(data []byte, target any) error {
	// A duplicate key is refused before decoding. encoding/json applies members in
	// order and lets the last one win, so two different byte strings would decode
	// to the same fact and produce the same digest.
	if err := rejectDuplicateKeys(data); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(target); err != nil {
		return fmt.Errorf("%w: %v", ErrCanonicalDecoding, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: trailing data after the value", ErrCanonicalDecoding)
	}
	return nil
}

// rejectDuplicateKeys walks the raw bytes and refuses a repeated object key at any
// depth.
func rejectDuplicateKeys(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return walkForDuplicateKeys(dec)
}

func walkForDuplicateKeys(dec *json.Decoder) error {
	token, err := dec.Token()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCanonicalDecoding, err)
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]bool)
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return fmt.Errorf("%w: %v", ErrCanonicalDecoding, err)
			}
			name, ok := key.(string)
			if !ok {
				return fmt.Errorf("%w: object key is not a string", ErrCanonicalDecoding)
			}
			if seen[name] {
				return fmt.Errorf("%w: duplicate key %q", ErrCanonicalDecoding, name)
			}
			seen[name] = true
			if err := walkForDuplicateKeys(dec); err != nil {
				return err
			}
		}
	case '[':
		for dec.More() {
			if err := walkForDuplicateKeys(dec); err != nil {
				return err
			}
		}
	}
	if _, err := dec.Token(); err != nil {
		return fmt.Errorf("%w: %v", ErrCanonicalDecoding, err)
	}
	return nil
}

// CanonicalLine renders v as canonical JSON followed by one newline, which is the
// framing every newline-delimited store uses.
func CanonicalLine(v any) ([]byte, error) {
	b, err := CanonicalEncode(v)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func writeCanonical(b *strings.Builder, v any) error {
	switch t := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if t {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case string:
		writeCanonicalString(b, t)
	case json.Number:
		if err := writeCanonicalNumber(b, t); err != nil {
			return err
		}
	case []any:
		b.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			if err := writeCanonical(b, item); err != nil {
				return err
			}
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			writeCanonicalString(b, k)
			b.WriteByte(':')
			if err := writeCanonical(b, t[k]); err != nil {
				return err
			}
		}
		b.WriteByte('}')
	default:
		return fmt.Errorf("%w: unexpected decoded type %T", ErrCanonicalEncoding, v)
	}
	return nil
}

// writeCanonicalNumber keeps the number exactly as it was written. Round tripping
// through float64 would lose precision beyond 2^53, which a tick count or a
// generation can reach.
func writeCanonicalNumber(b *strings.Builder, n json.Number) error {
	s := n.String()
	// An unsigned value above MaxInt64 is still an exact integer, so it must not
	// be forced through a float just because ParseInt would overflow.
	if _, err := strconv.ParseInt(s, 10, 64); err == nil {
		return writeCanonicalInt(b, s)
	}
	if _, err := strconv.ParseUint(s, 10, 64); err == nil {
		return writeCanonicalInt(b, s)
	}
	// A float is written exactly as it was read. The requirement is that it
	// round trips, not that it matches one particular spelling: encoding/json and
	// strconv disagree on ordinary values such as 1e-5, and demanding one
	// spelling would refuse values that are perfectly representable.
	if _, err := strconv.ParseFloat(s, 64); err != nil {
		return fmt.Errorf("%w: number %q: %v", ErrCanonicalEncoding, s, err)
	}
	b.WriteString(s)
	return nil
}

// writeCanonicalInt normalises an integer's spelling so that two spellings of the
// same integer cannot produce two digests.
//
// The sign is kept and the zeros are stripped only after it. Trimming zeros from
// the whole string would leave a negative number as "-0" plus digits and write a
// second minus in front, producing bytes that are not JSON at all.
func writeCanonicalInt(b *strings.Builder, s string) error {
	negative := strings.HasPrefix(s, "-")
	digits := strings.TrimLeft(strings.TrimLeft(s, "+-"), "0")
	if digits == "" {
		digits = "0"
		// A negative zero is still zero, and writing "-0" would make it a
		// different fact from "0".
		negative = false
	}
	if negative {
		b.WriteByte('-')
	}
	b.WriteString(digits)
	return nil
}

// writeCanonicalString escapes exactly what JSON requires and nothing else.
// In particular it leaves <, >, and & alone, so a recorded value cannot change
// meaning when it is embedded in a page.
func writeCanonicalString(b *strings.Builder, s string) {
	// encoding/json has already replaced any invalid byte with U+FFFD, so the
	// text here is valid. That replacement is written as a raw rune, which is
	// what the decoder reads back, so the two encoders cannot disagree.
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				b.WriteString(fmt.Sprintf(`\u%04x`, r))
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
}
