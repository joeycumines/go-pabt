/*
   Copyright 2026 Joseph Cumines

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
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// ErrCursorStale reports a resume position that belongs to a history this store is no
// longer serving.
//
// It is its own error rather than ErrImportStale because the remedy is different: an
// import that has been superseded is staged again, while a cursor from another
// generation is a client that has to start from a keyframe.
var ErrCursorStale = errors.New(`pabtdebug: the cursor belongs to another history`)

// Cursor is a client's position in a run.
//
// It carries everything the server needs to refuse a resume against a history other
// than the one the client was reading: the store, the session, the plan, the run, the
// generation, and the sequence the client has already seen. A client that reconnects
// with it resumes at the exact next sequence, after a restart as much as after a
// dropped connection, because every part of the resume is read from the store and
// nothing is held in the transport.
//
// It is not a secret. The capability token is the secret, and it is presented in a
// header; a forged cursor only chooses a position in a history the client could
// already have asked for by name, so signing it would add a key to defend something
// that is not a capability.
type Cursor struct {
	StoreID    StoreID `json:"storeId"`
	RunKey     RunKey  `json:"runKey"`
	Generation uint64  `json:"generation"`
	Sequence   uint64  `json:"sequence"`
	// Keyframe is the newest tree the client is known to hold, so a resume can be told
	// what to fetch to be whole again. It is a convenience, not a claim the server
	// trusts: the client applies what it fetches and the store verifies the result.
	KeyframeSequence uint64 `json:"keyframeSequence"`
	KeyframeDigest   string `json:"keyframeDigest"`
}

// Encode renders the cursor as one opaque token.
func (c Cursor) Encode() (string, error) {
	encoded, err := CanonicalEncode(c)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

// DecodeCursor reads a token back.
//
// A token that is not one of ours is refused rather than interpreted: a cursor is
// either a position this server issued or it is nothing, and guessing at a partial
// match would resume a client at a position nobody chose.
func DecodeCursor(token string) (Cursor, error) {
	var out Cursor
	if strings.TrimSpace(token) == "" {
		return out, fmt.Errorf("%w: the cursor is empty", ErrCursorStale)
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return out, fmt.Errorf("%w: the cursor is not a token: %v", ErrCursorStale, err)
	}
	if err := CanonicalDecode(raw, &out); err != nil {
		return out, fmt.Errorf("%w: the cursor does not decode: %v", ErrCursorStale, err)
	}
	if out.RunKey.IsZero() || out.StoreID == "" || out.Generation == 0 {
		return out, fmt.Errorf("%w: the cursor names no history", ErrCursorStale)
	}
	return out, nil
}

// CheckRefuses reports why this cursor may not resume against what the store is
// serving now, or nil when it may.
//
// A cursor from another store, another run, or another generation is refused. The
// generation is the part that matters most and the part a sequence alone cannot catch:
// an import replaces the active session and moves the generation, and a client that
// resumed by sequence alone would apply a delta from one history to a tree from
// another.
// A refusal names what the store is serving and never quotes what the cursor carried.
// The cursor is untrusted text: it comes back from a client, possibly from a browser
// address bar, and a refusal is the one place it would be reflected. Saying which run
// and which generation are expected loses the client nothing it did not already send.
func (c Cursor) CheckRefuses(store StoreID, key RunKey, generation uint64) error {
	switch {
	case c.StoreID != store:
		return fmt.Errorf("%w: it names another store, and this is %q", ErrCursorStale, store)
	case c.RunKey != key:
		return fmt.Errorf("%w: it names another run, and the request named %s",
			ErrCursorStale, key.RunID)
	case c.Generation != generation:
		return fmt.Errorf("%w: it names another generation, and the store is at %d",
			ErrCursorStale, generation)
	}
	return nil
}
