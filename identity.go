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

package pabt

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync/atomic"
)

const (
	// occurrencePrefix and lineagePrefix make a stored identity string
	// self-describing, so a canonical record identifies its own class.
	occurrencePrefix = "pabt-v1-"
	lineagePrefix    = "pabt-lineage-v1-"

	// identityBytes is the random namespace width shared by every occurrence
	// of one plan. 16 bytes is wide enough that two plans in one process, or
	// one plan before and after a restart, cannot mint the same identity.
	identityBytes = 16
)

// OccurrenceID identifies one planner object instance within a plan's identity
// namespace. An occurrence keeps its identity while the object it names is
// moved, which is what conflict resolution does when it reorders a subtree.
//
// An OccurrenceID names a live object. It is not a durable event identity and
// carries no meaning across plans.
type OccurrenceID string

// LineageID names the occurrence a derived occurrence was copied from. A
// post-condition copy created during expansion is the only derivation the
// planner performs, so this is currently a single-valued relation.
type LineageID string

// Expecting is an optional interface a [Condition] or [Effect] may implement to
// declare the value it expects.
//
// The planner calls only [Condition.Key], [Condition.Match], [Effect.Key], and
// [Effect.Value], so an expected value cannot be recovered from the key without
// guessing. A type that implements Expecting supplies the declaration
// explicitly; a type that does not has an unavailable expected value, which the
// capture boundary reports as such rather than inferring one.
type Expecting interface {
	// Expected returns the value the receiver requires or intends, if it declares
	// one. Returning a nil value with a true second result is not supported; use
	// the single-result form and treat nil as declared.
	Expected() any
}

// ExpectedValue reports the expected value declared by v, if v declares one.
// The second result is false when v does not implement [Expecting], which is
// distinct from a declaration of nil.
func ExpectedValue(v any) (any, bool) {
	e, ok := v.(Expecting)
	if !ok {
		return nil, false
	}
	return e.Expected(), true
}

// identity allocates occurrence identities for one plan.
type identity struct {
	namespace string
	next      atomic.Uint64
}

// newIdentity returns an identity allocator with a fresh random namespace.
func newIdentity() (*identity, error) {
	var b [identityBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, fmt.Errorf(`pabt: identity entropy: %w`, err)
	}
	return &identity{namespace: hex.EncodeToString(b[:])}, nil
}

// mint allocates the next occurrence identity in this namespace.
func (i *identity) mint() OccurrenceID {
	if i == nil {
		return ""
	}
	return OccurrenceID(occurrencePrefix + i.namespace + "-" +
		fmt.Sprintf("%016x", i.next.Add(1)-1))
}

// lineageOf returns the lineage identity for an occurrence derived from src.
func lineageOf(src OccurrenceID) LineageID {
	if src == "" {
		return ""
	}
	return LineageID(lineagePrefix + string(src))
}

// ValidOccurrenceID reports whether id has the form this package mints. The
// same check runs at capture, persistence, and query boundaries so one identity
// class has exactly one validator.
func ValidOccurrenceID(id OccurrenceID) bool {
	rest, ok := strings.CutPrefix(string(id), occurrencePrefix)
	if !ok {
		return false
	}
	ns, ordinal, ok := strings.Cut(rest, "-")
	if !ok || len(ns) != identityBytes*2 || len(ordinal) != 16 {
		return false
	}
	return isLowerHex(ns) && isLowerHex(ordinal)
}

// ValidLineageID reports whether id names a derivation of a valid occurrence.
func ValidLineageID(id LineageID) bool {
	rest, ok := strings.CutPrefix(string(id), lineagePrefix)
	return ok && ValidOccurrenceID(OccurrenceID(rest))
}

func isLowerHex(s string) bool {
	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
