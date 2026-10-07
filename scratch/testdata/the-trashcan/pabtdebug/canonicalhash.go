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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Hash domains. A domain is part of the digest input, so two different kinds of
// object can never collide even if their canonical bytes happened to match, and
// a change of format is a change of domain rather than a silent reinterpretation.
const (
	DomainValue  = "pabtdebug/value/v1"
	DomainNode   = "pabtdebug/node/v1"
	DomainTree   = "pabtdebug/tree/v1"
	DomainRecord = "pabtdebug/record/v1"
	DomainDelta  = "pabtdebug/delta/v1"
	DomainPlan   = "pabtdebug/plan/v1"
	DomainRun    = "pabtdebug/run/v1"
	DomainCodec  = "pabtdebug/codec/v1"
)

// HashCanonical returns the hex SHA-256 of a domain, a NUL byte, and the
// canonical encoding of v.
//
// The domain and the separator are both inside the digest so that a value cannot
// be moved between domains by reinterpreting its bytes, and so that a domain
// cannot be confused with a value that happens to begin with the same text.
func HashCanonical(domain string, v any) (string, error) {
	body, err := CanonicalEncode(v)
	if err != nil {
		return "", err
	}
	return hashBytes(domain, body), nil
}

func hashBytes(domain string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(domain))
	h.Write([]byte{0})
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// HashValue digests a stored value.
//
// The value's own digest field is cleared first, because a digest that covered
// itself would be unsolvable. The projection is a copy, so the caller's value is
// untouched.
func HashValue(v StoredValue) (string, error) {
	projected := v
	projected.SHA256 = ""
	return HashCanonical(DomainValue, projected)
}

// HashNode digests one node, clearing the node's own content digest first.
func HashNode(v StoredNode) (string, error) {
	projected := v
	projected.ContentHash = ""
	return HashCanonical(DomainNode, projected)
}

// HashTree digests a tree, clearing the tree's own digest first. Every node's
// content hash must already be verified by the caller, so a changed child changes
// the parent's digest.
func HashTree(v StoredTree) (string, error) {
	projected := v
	projected.Digest = ""
	return HashCanonical(DomainTree, projected)
}

// HashRun digests a run, clearing the run's own record digest first.
func HashRun(v StoredRun) (string, error) {
	projected := v
	projected.RecordSHA256 = ""
	return HashCanonical(DomainRun, projected)
}

// VerifyNestedHashes reports the first nested digest that does not match its
// object.
//
// A parent digest that covered only its children's digests would let an altered
// child hide behind an unaltered-looking parent, so the children are checked
// before the parent is trusted.
func VerifyNestedHashes(value StoredValue, nodes []StoredNode, tree *StoredTree) error {
	want, err := HashValue(value)
	if err != nil {
		return err
	}
	if value.SHA256 != want {
		return fmt.Errorf("pabtdebug: value digest is %q, recomputes to %q", value.SHA256, want)
	}
	for _, n := range nodes {
		got, err := HashNode(n)
		if err != nil {
			return err
		}
		if n.ContentHash != got {
			return fmt.Errorf("pabtdebug: node %s digest is %q, recomputes to %q", n.ID, n.ContentHash, got)
		}
	}
	if tree != nil {
		got, err := HashTree(*tree)
		if err != nil {
			return err
		}
		if tree.Digest != got {
			return fmt.Errorf("pabtdebug: tree digest is %q, recomputes to %q", tree.Digest, got)
		}
	}
	return nil
}
