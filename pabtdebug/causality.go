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
	"context"
	"fmt"
)

// The causal lane: what a tick submitted, what settled into it, and what the
// world changed on its own since the last record.
//
// Causality is recorded, not inferred. A reader that joins a receipt to a tree
// change by position is guessing; the envelope below carries the explicit IDs —
// command, idempotency key, world sequence, revision, plan tick, action
// instance — so "this tree change happened because the world reduced this
// command" is a reading of one record, never a correlation across two lanes.
//
// The neutral types (Causality*, below) are what an application hands the
// session. The session encodes every string in them through its codec
// registry, so an ID the application minted is scrubbed and redacted exactly
// like a condition key or an action name: it is application text, and
// application text is what the redaction walk exists for. The stored types
// (StoredCausality*) are what the record carries and the digest covers.
//
// Settled and Submitted are lists rather than single slots because more than
// one command can settle between two plan ticks: a delivered tractor walks
// home over several ticks, and each hop's submission and receipt buffer until
// the next plan tick carries them. A singular slot would silently drop the
// earlier hop, and a dropped command is exactly what this lane exists to make
// impossible. The entries link to each other by idempotency key, never by
// position in a list.

// CausalityExpectation is the set of resource versions one command was decided
// against. A command whose expectation names a version the world has since
// moved is visibly stale, which is what lets a reader tell a stale command
// from a current one without replaying the world. Every field is a number, so
// the same type serves as the wire shape: there is no application text here
// to scrub.
type CausalityExpectation struct {
	WorldSeq    uint64 `json:"worldSeq"`
	SlotVersion uint64 `json:"slotVersion"`
	LaneVersion uint64 `json:"laneVersion"`
	TagVersion  uint64 `json:"tagVersion"`
}

// CausalitySubmission is one command a tick submitted, in application terms.
// An empty entity field (Reefer, Slot, Node) means the command named no such
// entity, and is recorded as an absent link rather than as an empty string.
type CausalitySubmission struct {
	CommandID      string
	IdempotencyKey string
	Kind           string
	Tractor        string
	Reefer         string
	Slot           string
	Node           string
	PlanTickSeq    uint64
	ActionID       uint64
	Generation     uint64
	Expect         CausalityExpectation
}

// CausalityEvent is one world fact: either published by a settled result, or
// an exogenous change the host drained. CauseCommand names the command that
// produced it; an absent CauseCommand means no command caused it, which is
// what tells an exogenous change apart from a command's effect.
type CausalityEvent struct {
	Kind          string
	WorldSeq      uint64
	WorldRevision string
	Tractor       string
	Reefer        string
	Slot          string
	Node          string
	Lane          string
	CauseCommand  string
}

// CausalityAbandoned marks a command the loop withdrew without reducing: no
// outcome, no sequence, nothing in the world's ledger. It is deliberately not
// a settlement — a reader that treated an abandonment as a refusal would be
// reporting a decision the world never made.
type CausalityAbandoned struct {
	CommandID string
	Reason    string
}

// CausalitySettlement is the answer the world gave to one command. A refusal
// carries its typed reason and holder and publishes no events, which is what
// makes "a rejected command is visible with no world effect" a reading rather
// than an inference.
type CausalitySettlement struct {
	CommandID      string
	IdempotencyKey string
	Kind           string
	Outcome        string
	Reason         string
	Holder         string
	WorldSeq       uint64
	WorldRevision  string
	CommandSeq     uint64
	Events         []CausalityEvent
	Abandoned      *CausalityAbandoned
}

// Causality is one record's causal envelope: everything that happened between
// the last committed record and this one. Every field is optional; an envelope
// with nothing in it is recorded as no section at all.
type Causality struct {
	// Generation and PlanTickSeq identify the plan tick this record belongs
	// to. Both are zero on a control record, which carries buffered causality
	// but no tick of its own.
	Generation  uint64
	PlanTickSeq uint64

	// FrameWorldSeq and FrameRevision name the world observation the tick
	// reasoned over, so a reader can follow a tree change back to the frame
	// that justified it. Zero and empty when the record pins no frame.
	FrameWorldSeq uint64
	FrameRevision string

	Settled   []CausalitySettlement
	Submitted []CausalitySubmission
	Exogenous []CausalityEvent
}

// CausalityProvider supplies the envelope for the record one tick is about to
// commit. The session calls it inside the record build, after the planner has
// run, so a command the tick's action submitted is already visible.
//
// A provider that returns a nil envelope records no causality section. An
// error fails the record build, and the tick commits as a capture failure —
// the same terminalization the session already applies when a record cannot
// be built, and loud rather than silent. A provider is expected to drain its
// buffer when it returns an envelope, so the data rides exactly one record;
// on an error it should keep the buffer for the next tick.
type CausalityProvider interface {
	Causality(ctx context.Context) (*Causality, error)
}

// StoredCausality is the causal envelope a record carries. It is an additive
// omitempty section, and the omitempty is load-bearing: canonical encoding
// writes an absent nullable as an explicit null, so an always-rendered nil
// would add "causality":null to every record's bytes and move every digest —
// the exact failure TestAVersionOneRecordIsRefusedRatherThanServed pins. With
// omitempty the section is byte-absent when unset, the frozen golden vectors
// hold, and FormatVersion stays 2.
type StoredCausality struct {
	Generation    uint64             `json:"generation,omitempty"`
	PlanTickSeq   uint64             `json:"planTickSeq,omitempty"`
	FrameWorldSeq uint64             `json:"frameWorldSeq,omitempty"`
	FrameRevision *StoredValue       `json:"frameRevision,omitempty"`
	Settled       []StoredSettlement `json:"settled,omitempty"`
	Submitted     []StoredSubmission `json:"submitted,omitempty"`
	Exogenous     []StoredWorldEvent `json:"exogenous,omitempty"`
}

// StoredSubmission is one recorded command. The strings are StoredValues
// because they are application text — IDs and kinds the application minted —
// and application text is scrubbed and redacted before it can reach a digest.
// The numbers are the causal spine a reader joins on, and they are not text.
type StoredSubmission struct {
	CommandID      StoredValue          `json:"commandId"`
	IdempotencyKey StoredValue          `json:"idempotencyKey"`
	Kind           StoredValue          `json:"kind"`
	Tractor        StoredValue          `json:"tractor"`
	Reefer         *StoredValue         `json:"reefer,omitempty"`
	Slot           *StoredValue         `json:"slot,omitempty"`
	Node           *StoredValue         `json:"node,omitempty"`
	PlanTickSeq    uint64               `json:"planTickSeq"`
	ActionID       uint64               `json:"actionId"`
	Generation     uint64               `json:"generation"`
	Expect         CausalityExpectation `json:"expect"`
}

// StoredSettlement is one recorded answer. Abandoned and the receipt fields
// are mutually exclusive by construction: an abandonment carries no outcome,
// and a settlement carries no abandonment marker.
type StoredSettlement struct {
	CommandID      StoredValue        `json:"commandId"`
	IdempotencyKey StoredValue        `json:"idempotencyKey"`
	Kind           StoredValue        `json:"kind"`
	Outcome        StoredValue        `json:"outcome"`
	Reason         *StoredValue       `json:"reason,omitempty"`
	Holder         *StoredValue       `json:"holder,omitempty"`
	WorldSeq       uint64             `json:"worldSeq"`
	WorldRevision  StoredValue        `json:"worldRevision"`
	CommandSeq     uint64             `json:"commandSeq"`
	Events         []StoredWorldEvent `json:"events,omitempty"`
	Abandoned      *StoredAbandonment `json:"abandoned,omitempty"`
}

// StoredAbandonment is one recorded withdrawal: the command never happened,
// and the reason says which transport fault took it.
type StoredAbandonment struct {
	CommandID StoredValue `json:"commandId"`
	Reason    StoredValue `json:"reason"`
}

// StoredWorldEvent is one recorded world fact, with the command that caused
// it named explicitly and absent when no command caused it.
type StoredWorldEvent struct {
	Kind          StoredValue  `json:"kind"`
	WorldSeq      uint64       `json:"worldSeq"`
	WorldRevision StoredValue  `json:"worldRevision"`
	Tractor       *StoredValue `json:"tractor,omitempty"`
	Reefer        *StoredValue `json:"reefer,omitempty"`
	Slot          *StoredValue `json:"slot,omitempty"`
	Node          *StoredValue `json:"node,omitempty"`
	Lane          *StoredValue `json:"lane,omitempty"`
	CauseCommand  *StoredValue `json:"causeCommand,omitempty"`
}

// storedCausality translates one neutral envelope into the wire shape,
// encoding every application string through the session's codec registry. It
// returns a nil envelope for a nil or empty input, so a tick with nothing to
// say omits the section entirely.
func (s *Session) storedCausality(ctx context.Context, c *Causality) (*StoredCausality, error) {
	if c == nil {
		return nil, nil
	}
	out := &StoredCausality{
		Generation:    c.Generation,
		PlanTickSeq:   c.PlanTickSeq,
		FrameWorldSeq: c.FrameWorldSeq,
	}
	if c.FrameRevision != "" {
		encoded, err := s.codecs.Encode(ctx, "causality.frameRevision", c.FrameRevision)
		if err != nil {
			return nil, err
		}
		out.FrameRevision = &encoded
	}
	for i, sub := range c.Submitted {
		stored, err := s.storedSubmission(ctx, fmt.Sprintf("causality.submitted.%d", i), sub)
		if err != nil {
			return nil, err
		}
		out.Submitted = append(out.Submitted, stored)
	}
	for i, st := range c.Settled {
		stored, err := s.storedSettlement(ctx, fmt.Sprintf("causality.settled.%d", i), st)
		if err != nil {
			return nil, err
		}
		out.Settled = append(out.Settled, stored)
	}
	for i, ev := range c.Exogenous {
		stored, err := s.storedEvent(ctx, fmt.Sprintf("causality.exogenous.%d", i), ev)
		if err != nil {
			return nil, err
		}
		out.Exogenous = append(out.Exogenous, stored)
	}
	if out.Generation == 0 && out.PlanTickSeq == 0 && out.FrameWorldSeq == 0 &&
		out.FrameRevision == nil &&
		len(out.Submitted) == 0 && len(out.Settled) == 0 && len(out.Exogenous) == 0 {
		return nil, nil
	}
	return out, nil
}

func (s *Session) storedSubmission(ctx context.Context, source string, sub CausalitySubmission) (StoredSubmission, error) {
	out := StoredSubmission{
		PlanTickSeq: sub.PlanTickSeq,
		ActionID:    sub.ActionID,
		Generation:  sub.Generation,
		Expect:      sub.Expect,
	}
	var err error
	if out.CommandID, err = s.codecs.Encode(ctx, source+".commandId", sub.CommandID); err != nil {
		return StoredSubmission{}, err
	}
	if out.IdempotencyKey, err = s.codecs.Encode(ctx, source+".idempotencyKey", sub.IdempotencyKey); err != nil {
		return StoredSubmission{}, err
	}
	if out.Kind, err = s.codecs.Encode(ctx, source+".kind", sub.Kind); err != nil {
		return StoredSubmission{}, err
	}
	if out.Tractor, err = s.codecs.Encode(ctx, source+".tractor", sub.Tractor); err != nil {
		return StoredSubmission{}, err
	}
	if out.Reefer, err = s.optionalStoredValue(ctx, source+".reefer", sub.Reefer); err != nil {
		return StoredSubmission{}, err
	}
	if out.Slot, err = s.optionalStoredValue(ctx, source+".slot", sub.Slot); err != nil {
		return StoredSubmission{}, err
	}
	if out.Node, err = s.optionalStoredValue(ctx, source+".node", sub.Node); err != nil {
		return StoredSubmission{}, err
	}
	return out, nil
}

func (s *Session) storedSettlement(ctx context.Context, source string, st CausalitySettlement) (StoredSettlement, error) {
	out := StoredSettlement{
		WorldSeq:   st.WorldSeq,
		CommandSeq: st.CommandSeq,
	}
	var err error
	if out.CommandID, err = s.codecs.Encode(ctx, source+".commandId", st.CommandID); err != nil {
		return StoredSettlement{}, err
	}
	if out.IdempotencyKey, err = s.codecs.Encode(ctx, source+".idempotencyKey", st.IdempotencyKey); err != nil {
		return StoredSettlement{}, err
	}
	if out.Kind, err = s.codecs.Encode(ctx, source+".kind", st.Kind); err != nil {
		return StoredSettlement{}, err
	}
	if out.Outcome, err = s.codecs.Encode(ctx, source+".outcome", st.Outcome); err != nil {
		return StoredSettlement{}, err
	}
	if out.WorldRevision, err = s.codecs.Encode(ctx, source+".worldRevision", st.WorldRevision); err != nil {
		return StoredSettlement{}, err
	}
	if out.Reason, err = s.optionalStoredValue(ctx, source+".reason", st.Reason); err != nil {
		return StoredSettlement{}, err
	}
	if out.Holder, err = s.optionalStoredValue(ctx, source+".holder", st.Holder); err != nil {
		return StoredSettlement{}, err
	}
	for i, ev := range st.Events {
		stored, err := s.storedEvent(ctx, fmt.Sprintf("%s.events.%d", source, i), ev)
		if err != nil {
			return StoredSettlement{}, err
		}
		out.Events = append(out.Events, stored)
	}
	if st.Abandoned != nil {
		out.Abandoned = &StoredAbandonment{}
		if out.Abandoned.CommandID, err = s.codecs.Encode(ctx, source+".abandoned.commandId", st.Abandoned.CommandID); err != nil {
			return StoredSettlement{}, err
		}
		if out.Abandoned.Reason, err = s.codecs.Encode(ctx, source+".abandoned.reason", st.Abandoned.Reason); err != nil {
			return StoredSettlement{}, err
		}
	}
	return out, nil
}

func (s *Session) storedEvent(ctx context.Context, source string, ev CausalityEvent) (StoredWorldEvent, error) {
	out := StoredWorldEvent{WorldSeq: ev.WorldSeq}
	var err error
	if out.Kind, err = s.codecs.Encode(ctx, source+".kind", ev.Kind); err != nil {
		return StoredWorldEvent{}, err
	}
	if out.WorldRevision, err = s.codecs.Encode(ctx, source+".worldRevision", ev.WorldRevision); err != nil {
		return StoredWorldEvent{}, err
	}
	if out.Tractor, err = s.optionalStoredValue(ctx, source+".tractor", ev.Tractor); err != nil {
		return StoredWorldEvent{}, err
	}
	if out.Reefer, err = s.optionalStoredValue(ctx, source+".reefer", ev.Reefer); err != nil {
		return StoredWorldEvent{}, err
	}
	if out.Slot, err = s.optionalStoredValue(ctx, source+".slot", ev.Slot); err != nil {
		return StoredWorldEvent{}, err
	}
	if out.Node, err = s.optionalStoredValue(ctx, source+".node", ev.Node); err != nil {
		return StoredWorldEvent{}, err
	}
	if out.Lane, err = s.optionalStoredValue(ctx, source+".lane", ev.Lane); err != nil {
		return StoredWorldEvent{}, err
	}
	if out.CauseCommand, err = s.optionalStoredValue(ctx, source+".causeCommand", ev.CauseCommand); err != nil {
		return StoredWorldEvent{}, err
	}
	return out, nil
}

// optionalStoredValue records an entity field the application may leave
// unnamed. An empty string means no such entity was named, and is recorded as
// an absent link — a statement — rather than as an empty value a reader would
// have to distinguish from one the application never set.
func (s *Session) optionalStoredValue(ctx context.Context, source, v string) (*StoredValue, error) {
	if v == "" {
		return nil, nil
	}
	encoded, err := s.codecs.Encode(ctx, source, v)
	if err != nil {
		return nil, err
	}
	return &encoded, nil
}
