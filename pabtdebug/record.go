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
	"fmt"

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
	LastStatus    uint8  `json:"lastStatus"`
	StatusPresent bool   `json:"statusPresent"`
	Version       uint64 `json:"version"`
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
type StoredTree struct {
	RunKey       RunKey       `json:"runKey"`
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
func CodecOf(c ValueCodec) StoredCodec {
	t := c.MatchType()
	nilable := t != nil && (t.Kind() == 0 || t.Kind() == 20) // invalid or interface
	return StoredCodec{
		ID:          c.ID(),
		Version:     c.Version(),
		ReflectType: t.String(),
		Nilable:     nilable,
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
type StoredCondition struct {
	ConditionID string            `json:"conditionId"`
	Kind        string            `json:"kind"`
	Expected    StoredValue       `json:"expected"`
	Current     StoredValue       `json:"current"`
	Observed    *StoredValue      `json:"observed"`
	Uncertainty StoredUncertainty `json:"uncertainty"`
	Truth       string            `json:"truth"`
	Revision    RevisionScope     `json:"revision"`
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
const (
	FormatName    = "pabtdebug.record"
	FormatVersion = uint32(1)
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
	Tree                 *StoredTree       `json:"tree"`
	PayloadSHA256        string            `json:"payloadSha256"`
	PreviousRecordSHA256 string            `json:"previousRecordSha256"`
	RecordSHA256         string            `json:"recordSha256"`
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
