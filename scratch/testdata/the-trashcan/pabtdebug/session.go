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
	"sync"
	"time"

	"github.com/joeycumines/go-pabt"
)

// Session drives one planner tick per store append.
//
// It owns the durable order, which is the whole point of this type: a sequence is
// taken, the planner runs, a canonical record is built from what the capture
// boundary reported, and the record is committed. Nothing is recorded that the
// planner did not produce, and nothing the planner produced is discarded
// without a terminal record saying so.
type Session struct {
	store    *SQLiteStore
	planner  pabt.CapturedPlanner
	key      RunKey
	codecs   *CodecRegistry
	limits   ValueLimits
	announce func(context.Context, CommitReceipt, StoredRecord) error

	// causality, when set, supplies the causal envelope each tick record
	// carries. A nil provider records no envelope: the section is absent from
	// the bytes, and every digest written without it still verifies.
	causality CausalityProvider

	// gate serialises ticks. The store's append lease would serialise commits
	// anyway, but serialising here means a second caller is told rather than
	// being allowed to allocate a sequence it will not finish.
	gate sync.Mutex
}

// SessionConfig configures a session.
type SessionConfig struct {
	// Identity names the run the session records into.
	Identity SessionIdentity

	// Codecs records the values the capture boundary reports.
	Codecs *CodecRegistry

	// ValueLimits bounds what one recorded value may cost.
	ValueLimits ValueLimits

	// Announce is called once per committed record, with the receipt that proves the
	// commit. A nil Announce records nothing extra.
	//
	// It is fed from the receipt rather than from the tick, so a subscriber can never
	// be told about a sequence the store does not hold, and a tick that failed to
	// commit is not an event. An Announce that returns an error does not undo the
	// commit: the record is already durable, and the error is the subscriber's to
	// report.
	Announce func(context.Context, CommitReceipt, StoredRecord) error

	// Causality, when set, supplies the causal envelope each tick record
	// carries. See CausalityProvider for when it is consulted and what a nil
	// or an error means.
	Causality CausalityProvider
}

// TickResult is what one tick produced.
type TickResult struct {
	// Sequence is the committed record's position, or zero when nothing was
	// committed.
	Sequence uint64

	// Receipt proves the commit. Its zero proof means nothing was committed.
	Receipt CommitReceipt

	// Kind is the kind the attempt would commit. It describes a record that does
	// not exist when Sequence is zero or Err is set, so a caller must check those
	// before reading it as a committed kind.
	Kind RecordKind

	// DeltaSkipped explains why the record was stored as a keyframe when a delta
	// was expected. It is empty when a delta was stored, when no base was
	// eligible, or when the record carries no tree.
	DeltaSkipped string

	// Status is the planner's status, when the tick produced one.
	Status uint8

	// StatusPresent reports whether there was a status at all. A capture failure
	// has none, which is different from having failed.
	StatusPresent bool

	// StartedAt and CompletedAt bracket the whole attempt, including the commit.
	StartedAt   time.Time
	CompletedAt time.Time

	// Err is why no record was committed, if nothing was.
	Err error

	// AnnounceErr is why a subscriber was not told about a record that was committed.
	//
	// It is separate from Err because the record is durable either way, and a caller
	// that saw Err set would conclude the opposite. A subscriber that cannot be reached
	// is a fact about the subscriber.
	AnnounceErr error
}

// OpenSession attaches a session to a bootstrapped run.
func OpenSession(
	ctx context.Context,
	store *SQLiteStore,
	planner pabt.CapturedPlanner,
	cfg SessionConfig,
) (*Session, error) {
	if store == nil {
		return nil, fmt.Errorf(`pabtdebug: a session needs a store`)
	}
	if planner == nil {
		return nil, fmt.Errorf(`pabtdebug: a session needs a planner`)
	}
	if cfg.Codecs == nil {
		cfg.Codecs = NewCodecRegistry(DefaultValueLimits())
	}
	limits := cfg.ValueLimits
	if err := limits.Validate(); err != nil {
		limits = DefaultValueLimits()
	}
	key := RunKey{
		SessionID: cfg.Identity.SessionID,
		PlanID:    cfg.Identity.PlanID,
		RunID:     cfg.Identity.RunID,
	}
	if key.IsZero() {
		return nil, fmt.Errorf(`pabtdebug: a session needs a run identity`)
	}
	if _, err := store.Head(ctx, key); err != nil {
		return nil, fmt.Errorf("pabtdebug: attach a session: %w", err)
	}
	return &Session{
		store:     store,
		planner:   planner,
		key:       key,
		codecs:    cfg.Codecs,
		limits:    limits,
		announce:  cfg.Announce,
		causality: cfg.Causality,
	}, nil
}

// RunKey returns the run this session records into.
func (s *Session) RunKey() RunKey { return s.key }

// PlannerRevision returns the planner's current structural revision, which is what
// a tick begins at.
func (s *Session) PlannerRevision() uint64 { return s.planner.PlannerRevision() }

// Tick runs one planner tick and records it.
//
// Every path out of here either commits exactly one terminal record or returns an
// error with the sequence released, so a caller can never be left believing a tick
// was recorded when it was not, nor that a sequence was used when it was not.
func (s *Session) Tick(ctx context.Context) (out TickResult) {
	out.StartedAt = time.Now()
	if !s.gate.TryLock() {
		out.Err = fmt.Errorf(`pabtdebug: a tick is already in progress`)
		out.CompletedAt = time.Now()
		return out
	}
	defer s.gate.Unlock()

	// A panic anywhere in the tick is contained here, not only in the planner call,
	// so a failure while building the record cannot unwind with the run's lease and
	// its intent still held and nothing written.
	var ticket IntentTicket
	var began bool
	defer func() {
		if r := recover(); r != nil {
			cause := fmt.Errorf("pabtdebug: the tick panicked: %v", r)
			out = TickResult{
				StartedAt:   out.StartedAt,
				CompletedAt: time.Now(),
				Err:         cause,
			}
			if began {
				out.Err = joinErrors(cause, s.store.Abort(ctx, ticket))
			}
		}
	}()

	if _, err := s.store.Head(ctx, s.key); err != nil {
		out.Err = err
		out.CompletedAt = time.Now()
		return out
	}
	// The revision is taken before the attempt begins, because that is the
	// structure the attempt starts from. The planner may move it during the tick;
	// the record carries the revision the snapshot ended at, and the store
	// refuses a record that disagrees with what the attempt began at.
	ticket, err := s.store.Begin(ctx, BeginRequest{
		RunKey:               s.key,
		ExpectedPlanRevision: s.planner.PlannerRevision(),
	})
	if err != nil {
		out.Err = err
		out.CompletedAt = time.Now()
		return out
	}
	if err := s.store.MarkExecutionStarted(ctx, ticket); err != nil {
		// The attempt is open but execution never began, so it is released rather
		// than recorded: a record claiming a tick that never ran would be a claim
		// about something that did not happen.
		out.Err = joinErrors(err, s.store.Abort(ctx, ticket))
		out.CompletedAt = time.Now()
		return out
	}
	began = true

	outcome := s.runPlanner(ctx, ticket, out.StartedAt)
	return s.finalize(ctx, ticket, outcome)
}

// tickOutcome is what running the planner produced, before it is committed.
type tickOutcome struct {
	kind RecordKind
	exec pabt.TickExecution
	// release reports that the sequence should be released rather than recorded,
	// which is the case only when nothing about the tick is worth a record.
	release bool
	err     error
}

// runPlanner runs the capture boundary and contains a panic.
//
// A panic inside an application callback would otherwise unwind through the
// session and leave the attempt open with no record, which is the one state the
// terminalization rules exist to prevent. It is converted into a panicked record
// with a scrubbed message.
func (s *Session) runPlanner(ctx context.Context, ticket IntentTicket, started time.Time) (out tickOutcome) {
	defer func() {
		if r := recover(); r != nil {
			// The recovered outcome has to be committable. Leaving the execution
			// zero would give the record a plan revision of zero, which the store
			// refuses against an attempt that began at the planner's current
			// revision, and the tick would silently vanish from the history.
			out = tickOutcome{
				kind: RecordPanicked,
				exec: pabt.TickExecution{
					PlanRevision: s.planner.PlannerRevision(),
					StartedAt:    started,
					CompletedAt:  time.Now(),
				},
				err: fmt.Errorf("pabtdebug: the planner panicked: %v", r),
			}
		}
	}()
	exec := s.planner.TickCaptured(pabt.TickEnvironment{AttemptID: ticket.AttemptID})
	switch {
	case exec.CaptureError != nil:
		// The capture boundary could not produce a complete record. The failure is
		// recorded, because the fact that a tick ran and could not be captured is
		// itself part of the history.
		return tickOutcome{kind: RecordCaptureFailed, exec: exec,
			err: fmt.Errorf("pabtdebug: capture failed: %s", exec.CaptureError.Message)}
	case !exec.StatusKnown:
		return tickOutcome{kind: RecordCaptureFailed, exec: exec,
			err: fmt.Errorf("pabtdebug: the tick produced no usable status")}
	default:
		// A planner error is evidence about the world, not a failure to record, so
		// the tick is still recorded as a tick carrying that error.
		return tickOutcome{kind: RecordTick, exec: exec}
	}
}

// finalize turns an outcome into exactly one terminal record, or releases the
// sequence.
func (s *Session) finalize(ctx context.Context, ticket IntentTicket, outcome tickOutcome) TickResult {
	out := TickResult{
		Kind:        outcome.kind,
		StartedAt:   outcome.exec.StartedAt,
		CompletedAt: outcome.exec.CompletedAt,
	}
	if out.StartedAt.IsZero() {
		out.StartedAt = time.Now()
	}
	if outcome.release {
		out.Err = joinErrors(outcome.err, s.store.Abort(ctx, ticket))
		if out.CompletedAt.IsZero() {
			out.CompletedAt = time.Now()
		}
		return out
	}
	if outcome.kind == RecordTick {
		out.Status = uint8(outcome.exec.Status)
		out.StatusPresent = outcome.exec.StatusKnown
	}

	record, tree, err := s.buildRecord(ctx, ticket, outcome)
	if err != nil {
		// The record could not be built, so the attempt becomes a capture failure
		// rather than a success the store cannot represent. A capture failure has
		// no status, so the one the tick reported is dropped with it.
		record = s.terminalRecord(ctx, ticket, RecordCaptureFailed, outcome.exec, err)
		tree = nil
		out.Kind = RecordCaptureFailed
		out.Status = 0
		out.StatusPresent = false
	}
	var delta *ExactTreeDelta
	if tree != nil {
		base, found, baseErr := s.store.DeltaBase(ctx, s.key, ticket.Sequence)
		switch {
		case baseErr != nil:
			// The base could not be read, so the record is a keyframe. The reason
			// travels with the result rather than being swallowed, because a silently
			// keyframed history looks like a delta store that never found a base.
			out.DeltaSkipped = baseErr.Error()
		case found:
			computed, deltaErr := ComputeExactDelta(base, *tree)
			if deltaErr != nil {
				out.DeltaSkipped = deltaErr.Error()
				break
			}
			delta = computed
		}
	}
	receipt, err := s.store.CommitWithDelta(ctx, ticket, record, delta)
	if err != nil {
		// The commit failed, so the attempt is released and the failure reported.
		// A sequence that cannot be written is never left as a silent gap.
		out.Err = joinErrors(err, s.store.Abort(ctx, ticket))
		out.CompletedAt = time.Now()
		return out
	}
	out.Sequence = receipt.Sequence
	out.Receipt = receipt
	if out.CompletedAt.IsZero() {
		out.CompletedAt = time.Now()
	}
	// The announcement happens after the commit, and it announces the record as the
	// store holds it rather than the one this function built. The two differ: the
	// store seals the record it writes, and a subscriber told about the unsealed one
	// would be quoting bytes the chain does not cover. Reading it back costs one
	// indexed read and makes the announcement the store's own statement. Its error is
	// reported alongside the tick's own, never in place of it: the record exists
	// either way.
	if s.announce != nil {
		committed, readErr := s.store.RecordAt(ctx, s.key, receipt.Sequence)
		if readErr != nil {
			out.AnnounceErr = readErr
		} else {
			out.AnnounceErr = s.announce(ctx, receipt, committed)
		}
	}
	return out
}

// CommitControl commits a tree-less control record carrying whatever causality
// the provider still holds. It is the flush a run takes when it ends with
// buffered settlements and no further plan tick to ride in — a walk-home
// receipt, an exogenous change drained after the last tick.
//
// The record is tree-less, so DeltaBase excludes it as a base and the next
// tree after it is a keyframe. At the end of a history that forces nothing;
// mid-history a caller that flushes is accepting the one keyframe.
//
// A provider with an empty envelope commits nothing and reports it: a control
// record that says nothing happened is exactly the filler the terminalization
// rules exist to keep out of the chain.
func (s *Session) CommitControl(ctx context.Context) (CommitReceipt, bool, error) {
	if !s.gate.TryLock() {
		return CommitReceipt{}, false, fmt.Errorf(`pabtdebug: a tick is already in progress`)
	}
	defer s.gate.Unlock()
	if s.causality == nil {
		return CommitReceipt{}, false, nil
	}
	envelope, err := s.causality.Causality(ctx)
	if err != nil {
		return CommitReceipt{}, false, fmt.Errorf("pabtdebug: the causality provider: %w", err)
	}
	stored, err := s.storedCausality(ctx, envelope)
	if err != nil {
		return CommitReceipt{}, false, err
	}
	if stored == nil {
		return CommitReceipt{}, false, nil
	}
	revision := s.planner.PlannerRevision()
	ticket, err := s.store.Begin(ctx, BeginRequest{
		RunKey:               s.key,
		ExpectedPlanRevision: revision,
	})
	if err != nil {
		return CommitReceipt{}, false, err
	}
	now := time.Now().UTC()
	record := StoredRecord{
		Kind:         RecordControl,
		PayloadState: PayloadComplete,
		StartedAt:    Timestamp(now),
		CompletedAt:  Timestamp(now),
		Revision: RevisionScope{
			RunKey:       s.key,
			Sequence:     ticket.Sequence,
			PlanRevision: revision,
		},
		Causality: stored,
	}
	receipt, err := s.store.Commit(ctx, ticket, record)
	if err != nil {
		return CommitReceipt{}, false, joinErrors(err, s.store.Abort(ctx, ticket))
	}
	if s.announce != nil {
		committed, readErr := s.store.RecordAt(ctx, s.key, receipt.Sequence)
		if readErr != nil {
			return receipt, true, readErr
		}
		if err := s.announce(ctx, receipt, committed); err != nil {
			return receipt, true, err
		}
	}
	return receipt, true, nil
}

// buildRecord turns a captured tick into the record the store commits.
func (s *Session) buildRecord(
	ctx context.Context,
	ticket IntentTicket,
	outcome tickOutcome,
) (StoredRecord, *StoredTree, error) {
	exec := outcome.exec
	snapshot := exec.Snapshot
	// The snapshot's revision is the most precise answer, but a recovered panic has
	// no snapshot at all. Taking the higher of the two keeps the record at or above
	// the revision the attempt began at, which is what the store requires, rather
	// than zero, which it refuses.
	planRevision := max(snapshot.PlanRevision, exec.PlanRevision)
	record := StoredRecord{
		Kind:             outcome.kind,
		PayloadState:     PayloadComplete,
		ExecutionStarted: true,
		StartedAt:        Timestamp(exec.StartedAt.UTC()),
		CompletedAt:      Timestamp(exec.CompletedAt.UTC()),
		Status:           uint8(exec.Status),
		StatusPresent:    exec.StatusKnown,
		Revision: RevisionScope{
			RunKey:       s.key,
			Sequence:     ticket.Sequence,
			PlanRevision: planRevision,
			State:        snapshot.StateRevision,
			Coherence:    snapshot.Coherence,
		},
	}
	if !exec.StatusKnown {
		record.Status = 0
	}
	if outcome.kind != RecordTick {
		// A failure has no tree to claim, so it is not tree eligible and its
		// payload says so rather than carrying a partial one. Its conditions and
		// relations are still recorded, so the reason a tick failed is visible.
		record.PayloadState = PayloadNone
	}
	if exec.PlannerError != nil {
		record.PlannerError = s.storedError(ctx, exec.PlannerError)
	}
	if exec.CaptureError != nil {
		record.CaptureError = s.storedError(ctx, exec.CaptureError)
	}
	if outcome.kind == RecordPanicked {
		record.Panic = &StoredPanic{
			Code:    ErrorCodePanic,
			Type:    "panic",
			Message: s.sanitize(outcome.err),
		}
	}

	conditions, err := s.storedConditions(ctx, snapshot)
	if err != nil {
		return StoredRecord{}, nil, err
	}
	record.Conditions = conditions
	relations := s.storedRelations(ctx, snapshot)

	record.Relations = relations
	// The actions and the effects are recorded as sections of their own, because the
	// relation between an effect and the condition it was meant to make is carried
	// by the action and not by anything a consumer could join on. Without them a
	// reader can see that an effect exists and that a condition exists and has to
	// guess that they are the same fact.
	actions, err := s.storedActions(ctx, snapshot)
	if err != nil {
		return StoredRecord{}, nil, err
	}
	record.Actions = actions
	effects, err := s.storedEffects(ctx, snapshot)
	if err != nil {
		return StoredRecord{}, nil, err
	}
	record.Effects = effects
	if outcome.kind == RecordTick && len(snapshot.Nodes) > 0 {
		tree, err := s.storedTree(snapshot)
		if err != nil {
			return StoredRecord{}, nil, err
		}
		record.Tree = tree
		record.TreeEligible = true
	}
	// The envelope is consulted only for a tick that produced a record: a
	// panicked or capture-failed tick leaves the provider's buffer parked for
	// the next record, so buffered causality is never discarded by a failure
	// that carries no payload of its own.
	if outcome.kind == RecordTick && s.causality != nil {
		envelope, err := s.causality.Causality(ctx)
		if err != nil {
			return StoredRecord{}, nil, fmt.Errorf("pabtdebug: the causality provider: %w", err)
		}
		record.Causality, err = s.storedCausality(ctx, envelope)
		if err != nil {
			return StoredRecord{}, nil, err
		}
	}
	return record, record.Tree, nil
}

// terminalRecord is the minimal record a failure commits.
func (s *Session) terminalRecord(
	ctx context.Context,
	ticket IntentTicket,
	kind RecordKind,
	exec pabt.TickExecution,
	cause error,
) StoredRecord {
	return StoredRecord{
		Kind:             kind,
		PayloadState:     PayloadNone,
		ExecutionStarted: true,
		StartedAt:        Timestamp(exec.StartedAt.UTC()),
		CompletedAt:      Timestamp(exec.CompletedAt.UTC()),
		Revision: RevisionScope{
			RunKey:       s.key,
			Sequence:     ticket.Sequence,
			PlanRevision: exec.PlanRevision,
		},
		CaptureError: &StoredError{
			Code:    ErrorCodeCapture,
			Type:    "capture",
			Message: s.sanitize(cause),
		},
	}
}

func (s *Session) storedError(ctx context.Context, recorded *pabt.RecordedError) *StoredError {
	if recorded == nil {
		return nil
	}
	return &StoredError{
		Code:    ErrorCodePlanner,
		Type:    recorded.Type,
		Message: s.codecs.sanitizeText(recorded.Message),
	}
}

// sanitize scrubs text through the same path a value takes, so a diagnostic
// cannot carry something the value layer would have removed.
func (s *Session) sanitize(err error) string {
	if err == nil {
		return ""
	}
	return s.codecs.sanitizeText(err.Error())
}

func errorText(recorded *pabt.RecordedError) string {
	if recorded == nil {
		return ""
	}
	return recorded.Message
}

// max64 returns the higher of two revisions, so a record is never stamped below
// the revision its attempt began at. On a normal tick the snapshot's revision is
// already at or above the execution's, so this only matters when a tick produced
// no snapshot at all.
func max64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

func joinErrors(a, b error) error {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	}
	return fmt.Errorf("%w; %w", a, b)
}
