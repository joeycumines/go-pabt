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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt"
)

// StatusText returns the wire text for a status.
//
// The text names the status rather than numbering it, so a reader cannot
// misread it and a future dependency cannot renumber it underneath a stored
// history. An absent status is an empty string, never a number the dependency
// happens not to use.
func StatusText(s bt.Status, present bool) (string, error) {
	if !present {
		return "", nil
	}
	switch s {
	case bt.Running:
		return "running", nil
	case bt.Success:
		return "success", nil
	case bt.Failure:
		return "failure", nil
	}
	return "", fmt.Errorf("%w: status %d", pabt.ErrStatusValue, int(s))
}

// DecodeStatusText parses wire text back to a status.
func DecodeStatusText(text string) (bt.Status, bool, error) {
	if text == "" {
		return 0, false, nil
	}
	switch text {
	case "running":
		return bt.Running, true, nil
	case "success":
		return bt.Success, true, nil
	case "failure":
		return bt.Failure, true, nil
	}
	return 0, false, fmt.Errorf("%w: status text %q", pabt.ErrStatusValue, text)
}

// ErrorCode is a stable, machine-readable classification of a recorded failure.
//
// The code is stable across releases so a consumer can branch on it; the message
// is scrubbed free text and may change.
type ErrorCode string

const (
	ErrorCodeCapture     ErrorCode = "capture_failed"
	ErrorCodePlanner     ErrorCode = "planner_error"
	ErrorCodePanic       ErrorCode = "panic"
	ErrorCodeObserver    ErrorCode = "observer_error"
	ErrorCodeVerifier    ErrorCode = "verifier_error"
	ErrorCodeStateView   ErrorCode = "state_view_error"
	ErrorCodeValue       ErrorCode = "value_error"
	ErrorCodeEncoding    ErrorCode = "encoding_error"
	ErrorCodeStorage     ErrorCode = "storage_error"
	ErrorCodeUnavailable ErrorCode = "unavailable"
)

// StoredError is a recorded failure, scrubbed and classified.
type StoredError struct {
	Code    ErrorCode `json:"code"`
	Type    string    `json:"type"`
	Message string    `json:"message"`
}

// StoredPanic is a recorded panic, scrubbed and classified.
type StoredPanic struct {
	Code    ErrorCode `json:"code"`
	Type    string    `json:"type"`
	Message string    `json:"message"`
	Stack   string    `json:"stack"`
}

// StoredNodeStatus is a node's coherent tick accounting.
type StoredNodeStatus struct {
	TickCount     uint64 `json:"tickCount"`
	LastStatus    uint8  `json:"-"`
	StatusPresent bool   `json:"-"`
	Version       uint64 `json:"version"`
}

// MarshalJSON writes the status as text and its presence as a separate flag.
//
// go-behaviortree numbers its statuses 1, 2, and 3, with 0 meaning nothing. A
// stored number would therefore carry a value that looks like a status and is
// not, and a reader of a future record would have to know the numbering.
func (s StoredNodeStatus) MarshalJSON() ([]byte, error) {
	type alias StoredNodeStatus
	text, present := statusFields(s.LastStatus, s.StatusPresent)
	return json.Marshal(struct {
		alias
		LastStatus    string `json:"lastStatus"`
		StatusPresent bool   `json:"statusPresent"`
	}{
		alias:         alias(s),
		LastStatus:    text,
		StatusPresent: present,
	})
}

// UnmarshalJSON reads the status from text and refuses anything undefined.
func (s *StoredNodeStatus) UnmarshalJSON(data []byte) error {
	type alias StoredNodeStatus
	var raw struct {
		alias
		LastStatus    string `json:"lastStatus"`
		StatusPresent *bool  `json:"statusPresent"`
	}
	// A nested decoder is used rather than json.Unmarshal because the caller's
	// DisallowUnknownFields does not reach inside a custom unmarshaller. Without
	// this, an injected member would decode cleanly and the record's digest would
	// still verify, which is exactly the tampering the digest exists to catch.
	if err := decodeStrict(data, &raw); err != nil {
		return err
	}
	status, present, err := DecodeStatusText(raw.LastStatus)
	if err != nil {
		return err
	}
	if raw.StatusPresent != nil && *raw.StatusPresent != present {
		return fmt.Errorf("%w: status %q is present=%v but the flag says %v",
			ErrCanonicalDecoding, raw.LastStatus, present, *raw.StatusPresent)
	}
	*s = StoredNodeStatus(raw.alias)
	s.StatusPresent = present
	if present {
		s.LastStatus = uint8(status)
	} else {
		s.LastStatus = 0
	}
	return nil
}

// decodeStrict decodes one value, refusing an unknown member and a trailing
// value. It exists because a type with its own unmarshaller loses the caller's
// strictness otherwise.
func decodeStrict(data []byte, target any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return fmt.Errorf("%w: %v", ErrCanonicalDecoding, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: trailing data after the value", ErrCanonicalDecoding)
	}
	return nil
}

// statusFields returns the wire text for a status and the presence flag that goes
// with it.
//
// A status the dependency does not define is written as absent, not as an empty
// text with the flag still set. Writing the pair "empty but present" is a
// statement no reader could act on, and it decodes back as an absence anyway.
func statusFields(status uint8, present bool) (string, bool) {
	text, err := StatusText(bt.Status(status), present)
	if err != nil {
		return "", false
	}
	return text, present
}

// NodeStatusOf converts a captured status, rejecting one the dependency does not
// define rather than storing an integer nothing can interpret.
func NodeStatusOf(s pabt.NodeStatusSnapshot) (StoredNodeStatus, error) {
	out := StoredNodeStatus{
		TickCount:     uint64(s.TickCount),
		StatusPresent: s.StatusPresent,
		Version:       s.Version,
	}
	if !s.StatusPresent {
		return out, nil
	}
	if err := pabt.ValidateStatus(s.LastStatus); err != nil {
		return StoredNodeStatus{}, err
	}
	out.LastStatus = uint8(s.LastStatus)
	return out, nil
}

// RunKey identifies one run inside one session of one store.
type RunKey struct {
	SessionID string `json:"sessionId"`
	PlanID    string `json:"planId"`
	RunID     string `json:"runId"`
}

// IsZero reports whether the key names nothing.
func (k RunKey) IsZero() bool {
	return k.SessionID == "" && k.PlanID == "" && k.RunID == ""
}

// StoredNode is one behavior-tree node in a recorded tree.
//
// The four link fields say what this occurrence is, not where it sits. A
// condition node names the condition it evaluates; a post-condition node names
// the condition's original node through PostNodeID; an action node names the
// action instance and the effects that instance declares. A nil link means the
// node is none of those things, which is a statement rather than a gap, so a
// consumer never has to guess whether an absent link was recorded as empty or
// never recorded at all.
type StoredNode struct {
	ID          string           `json:"id"`
	Lineage     string           `json:"lineage"`
	ParentID    *string          `json:"parentId"`
	Ordinal     uint32           `json:"ordinal"`
	Type        string           `json:"type"`
	Name        string           `json:"name"`
	Status      StoredNodeStatus `json:"status"`
	ConditionID *string          `json:"conditionId"`
	PostNodeID  *string          `json:"postNodeId"`
	ActionID    *string          `json:"actionId"`
	EffectIDs   []string         `json:"effectIds"`
	Frame       *pabt.FrameInfo  `json:"frame"`
	ContentHash string           `json:"contentHash"`
}

// StoredTree is a complete occurrence-keyed tree for one record.
//
// EventUID names the record that committed this tree, which is what lets a delta
// name its base without a side table.
type StoredTree struct {
	RunKey       RunKey       `json:"runKey"`
	EventUID     string       `json:"eventUid"`
	Sequence     uint64       `json:"sequence"`
	PlanRevision uint64       `json:"planRevision"`
	RootID       string       `json:"rootId"`
	Nodes        []StoredNode `json:"nodes"`
	NodeCount    uint32       `json:"nodeCount"`
	Digest       string       `json:"digest"`
}

// StoredPlan is the plan a run belongs to.
type StoredPlan struct {
	PlanID       string        `json:"planId"`
	Title        string        `json:"title"`
	Metadata     []StoredValue `json:"metadata"`
	CreatedAt    Timestamp     `json:"createdAt"`
	RecordSHA256 string        `json:"recordSha256"`
}

// StoredRun is one run's identity and head.
type StoredRun struct {
	RunKey              RunKey     `json:"runKey"`
	State               string     `json:"state"`
	StartedAt           Timestamp  `json:"startedAt"`
	EndedAt             *Timestamp `json:"endedAt"`
	InitialPlanRevision uint64     `json:"initialPlanRevision"`
	HeadSequence        uint64     `json:"headSequence"`
	HeadTreeSequence    uint64     `json:"headTreeSequence"`
	HeadTreeEventUID    string     `json:"headTreeEventUid"`
	HeadTreeDigest      string     `json:"headTreeDigest"`
	HeadRecordSHA256    string     `json:"headRecordSha256"`
	RecordSHA256        string     `json:"recordSha256"`
}

// StoredCodec is a registered value codec, as persisted.
type StoredCodec struct {
	ID          string `json:"id"`
	Version     uint32 `json:"version"`
	ReflectType string `json:"reflectType"`
	Nilable     bool   `json:"nilable"`
	Fallback    bool   `json:"fallback"`
	SHA256      string `json:"sha256"`
}

// HashCodec digests a codec descriptor.
//
// The descriptor bytes are part of the projection. A codec whose behaviour
// changed while keeping its identity and version would otherwise keep the digest
// the record already recorded, and a consumer would have no way to notice.
func HashCodec(c StoredCodec) (string, error) {
	projected := c
	projected.SHA256 = ""
	return HashCanonical(DomainCodec, projected)
}

// CodecOf converts a registered codec into its persisted descriptor.
//
// A fallback codec declares no match type, which is how a registry says "this
// codec is reached only when no exact one matched". Its descriptor records that
// rather than dereferencing a type that is not there.
func CodecOf(c ValueCodec) StoredCodec {
	if c == nil {
		return StoredCodec{}
	}
	t := c.MatchType()
	if t == nil {
		return StoredCodec{ID: c.ID(), Version: c.Version(), Fallback: true}
	}
	return StoredCodec{
		ID:          c.ID(),
		Version:     c.Version(),
		ReflectType: t.String(),
		Nilable:     t.Kind() == reflect.Interface,
	}
}

// UncertaintyState says why a value the debugger wanted is not there.
type UncertaintyState string

const (
	// UncertaintyNone means every requested value was available.
	UncertaintyNone UncertaintyState = "none"

	// UncertaintyUnavailable means the application never supplied the value.
	UncertaintyUnavailable UncertaintyState = "unavailable"

	// UncertaintyError means the read failed.
	UncertaintyError UncertaintyState = "error"

	// UncertaintyUndeclared means the application does not declare a value there,
	// which is different from declaring that the value is unknown.
	UncertaintyUndeclared UncertaintyState = "undeclared"
)

// StoredUncertainty explains a missing value without inventing one.
type StoredUncertainty struct {
	State   UncertaintyState `json:"state"`
	Code    string           `json:"code"`
	Message string           `json:"message"`
}

// RevisionScope records the revisions a fact was observed at.
type RevisionScope struct {
	RunKey       RunKey              `json:"runKey"`
	Sequence     uint64              `json:"sequence"`
	PlanRevision uint64              `json:"planRevision"`
	State        pabt.StateRevision  `json:"state"`
	Coherence    pabt.StateCoherence `json:"coherence"`
}

// StoredCondition is one condition with its four evidence fields kept apart.
//
// Expected is the value the application declared. Current is the value the
// planner actually read and matched against. Observed is what a post-action
// observer saw. Uncertainty explains whichever is missing. None of them is
// derived from another: a consumer that shows an expected value which was
// really inferred from a key is stating something false.
//
// Key is the variable the condition reads, carried as a value rather than as
// text so it is scrubbed and redacted like every other piece of application
// data. It is the subject of the condition, never its requirement: a consumer
// that displays it under the expected value's heading is making the key-only
// guess this whole type exists to prevent.
//
// NodeID and PostNodeID are the two nodes that evaluate it. A condition is
// reachable from more than one node, and a consumer asked "which preconditions
// govern this occurrence" cannot answer without being told which occurrences
// carry which conditions.
type StoredCondition struct {
	ConditionID string            `json:"conditionId"`
	Kind        string            `json:"kind"`
	Key         StoredValue       `json:"key"`
	NodeID      *string           `json:"nodeId"`
	PostNodeID  *string           `json:"postNodeId"`
	Expected    StoredValue       `json:"expected"`
	Current     StoredValue       `json:"current"`
	Observed    *StoredValue      `json:"observed"`
	Uncertainty StoredUncertainty `json:"uncertainty"`
	Truth       string            `json:"truth"`
	Revision    RevisionScope     `json:"revision"`
}

// StoredAction is one action instance the planner expanded.
//
// It carries the relation that actually exists between an effect and a
// condition: an action's postcondition is the condition the action was there to
// make, so a consumer can say what governed an action without inferring it
// from a shared variable key (DD-19). ConditionIDs are the conditions the
// action's own alternative branches are guarded by, which is a different
// relation from the postcondition and is kept separate.
type StoredAction struct {
	ActionID        string   `json:"actionId"`
	NodeID          *string  `json:"nodeId"`
	Name            string   `json:"name"`
	PostconditionID *string  `json:"postconditionId"`
	ConditionIDs    []string `json:"conditionIds"`
	EffectIDs       []string `json:"effectIds"`
}

// StoredEffect is one effect an action declares, with the value the application
// gave it.
//
// A declared effect with no readable value says so through Uncertainty rather
// than through an absent section, because "the action declares this effect" and
// "the action declares no effect" are different facts about an action.
type StoredEffect struct {
	EffectID    string            `json:"effectId"`
	ActionID    string            `json:"actionId"`
	Key         StoredValue       `json:"key"`
	Value       StoredValue       `json:"value"`
	Uncertainty StoredUncertainty `json:"uncertainty"`
}

// StoredRelation is one verifier conclusion about an effect's contribution to a
// post-condition.
type StoredRelation struct {
	RelationID      string        `json:"relationId"`
	EffectID        string        `json:"effectId"`
	PostconditionID string        `json:"postconditionId"`
	Result          string        `json:"result"`
	Method          string        `json:"method"`
	Authority       string        `json:"authority"`
	Authoritative   bool          `json:"authoritative"`
	Revision        RevisionScope `json:"revision"`
	Error           *StoredError  `json:"error"`
}

// RecordKind names what a record says happened.
type RecordKind string

const (
	RecordTick          RecordKind = "tick"
	RecordCaptureFailed RecordKind = "capture_failed"
	RecordPanicked      RecordKind = "panicked"
	RecordInterrupted   RecordKind = "interrupted"
	RecordAborted       RecordKind = "aborted"
	RecordControl       RecordKind = "control"
	RecordImport        RecordKind = "import"
)

// PayloadState says how much of a record's payload survived.
type PayloadState string

const (
	PayloadComplete PayloadState = "complete"
	PayloadPartial  PayloadState = "partial"
	PayloadNone     PayloadState = "none"
)

// FormatName and FormatVersion identify a record's on-disk shape.
//
// Version 2 added the action and effect sections and the condition's own node
// links, because a consumer could not otherwise say which preconditions govern
// a selected occurrence. A version 1 record decodes into this shape with those
// sections absent, which reads as "not recorded" rather than as "none": a record
// written before the linkage existed did not say the tree had no actions, it
// said nothing about them.
const (
	FormatName    = "pabtdebug.record"
	FormatVersion = uint32(2)
)

// StoredRecord is one recorded tick, and the unit the history is made of.
//
// The payload digest covers the record except for the digests themselves, and the
// record digest additionally covers the previous record's digest. That asymmetry
// is what makes a record tamper-evident and a whole run chain-verifiable.
type StoredRecord struct {
	Format               string            `json:"format"`
	Version              uint32            `json:"version"`
	EventUID             string            `json:"eventUid"`
	RunKey               RunKey            `json:"runKey"`
	Sequence             uint64            `json:"sequence"`
	AttemptID            string            `json:"attemptId"`
	Kind                 RecordKind        `json:"kind"`
	PayloadState         PayloadState      `json:"payloadState"`
	TreeEligible         bool              `json:"treeEligible"`
	ExecutionStarted     bool              `json:"executionStarted"`
	Revision             RevisionScope     `json:"revision"`
	StartedAt            Timestamp         `json:"startedAt"`
	CompletedAt          Timestamp         `json:"completedAt"`
	Status               uint8             `json:"status"`
	StatusPresent        bool              `json:"statusPresent"`
	PlannerError         *StoredError      `json:"plannerError"`
	Panic                *StoredPanic      `json:"panic"`
	CaptureError         *StoredError      `json:"captureError"`
	Conditions           []StoredCondition `json:"conditions"`
	Relations            []StoredRelation  `json:"relations"`
	Actions              []StoredAction    `json:"actions"`
	Effects              []StoredEffect    `json:"effects"`
	Tree                 *StoredTree       `json:"tree"`
	PayloadSHA256        string            `json:"payloadSha256"`
	PreviousRecordSHA256 string            `json:"previousRecordSha256"`
	RecordSHA256         string            `json:"recordSha256"`
}

// MarshalJSON writes the record's status as text and omits absent optional
// sections, so two records that differ only in a section that was never populated
// do not differ in bytes.
func (r StoredRecord) MarshalJSON() ([]byte, error) {
	type alias StoredRecord
	status, err := StatusText(bt.Status(r.Status), r.StatusPresent)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		alias
		Status        string      `json:"status"`
		StatusPresent bool        `json:"statusPresent"`
		Tree          *StoredTree `json:"tree"`
	}{
		alias:         alias(r),
		Status:        status,
		StatusPresent: r.StatusPresent,
		Tree:          r.Tree,
	})
}

// UnmarshalJSON reads the record's status from text and refuses anything the
// dependency does not define.
func (r *StoredRecord) UnmarshalJSON(data []byte) error {
	type alias StoredRecord
	var raw struct {
		alias
		Status        string `json:"status"`
		StatusPresent *bool  `json:"statusPresent"`
	}
	if err := decodeStrict(data, &raw); err != nil {
		return err
	}
	status, present, err := DecodeStatusText(raw.Status)
	if err != nil {
		return err
	}
	if raw.StatusPresent != nil && *raw.StatusPresent != present {
		return fmt.Errorf("%w: status %q is present=%v but the flag says %v",
			ErrCanonicalDecoding, raw.Status, present, *raw.StatusPresent)
	}
	*r = StoredRecord(raw.alias)
	r.StatusPresent = present
	if present {
		r.Status = uint8(status)
	} else {
		r.Status = 0
	}
	return nil
}

// HashRecord digests a record.
//
// The previous-record link is inside the projection, so altering only the link
// changes this digest, which is what makes the chain verifiable.
func HashRecord(r StoredRecord) (string, error) {
	projected := r
	projected.RecordSHA256 = ""
	return HashCanonical(DomainRecord, projected)
}

// HashPayload digests a record's payload, which excludes both digests and the
// previous-record link.
func HashPayload(r StoredRecord) (string, error) {
	projected := r
	projected.PayloadSHA256 = ""
	projected.RecordSHA256 = ""
	projected.PreviousRecordSHA256 = ""
	return HashCanonical(DomainRecord+"/payload", projected)
}
