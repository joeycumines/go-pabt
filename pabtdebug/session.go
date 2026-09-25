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
	"strings"
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
	store   *SQLiteStore
	planner pabt.CapturedPlanner
	key     RunKey
	codecs  *CodecRegistry
	limits  ValueLimits

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
}

// TickResult is what one tick produced.
type TickResult struct {
	// Sequence is the committed record's position, or zero when nothing was
	// committed.
	Sequence uint64

	// Receipt proves the commit. Its zero proof means nothing was committed.
	Receipt CommitReceipt

	// Kind is the terminal record's kind.
	Kind RecordKind

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
		store:   store,
		planner: planner,
		key:     key,
		codecs:  cfg.Codecs,
		limits:  limits,
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
func (s *Session) Tick(ctx context.Context) TickResult {
	out := TickResult{StartedAt: time.Now()}
	if !s.gate.TryLock() {
		out.Err = fmt.Errorf(`pabtdebug: a tick is already in progress`)
		out.CompletedAt = time.Now()
		return out
	}
	defer s.gate.Unlock()

	head, err := s.store.Head(ctx, s.key)
	if err != nil {
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
	_ = head

	if err := s.store.MarkExecutionStarted(ctx, ticket); err != nil {
		// The attempt is open but execution never began, so it is released rather
		// than recorded: a record claiming a tick that never ran would be a claim
		// about something that did not happen.
		out.Err = joinErrors(err, s.store.Abort(ctx, ticket))
		out.CompletedAt = time.Now()
		return out
	}

	outcome := s.runPlanner(ctx, ticket)
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
func (s *Session) runPlanner(ctx context.Context, ticket IntentTicket) (out tickOutcome) {
	defer func() {
		if r := recover(); r != nil {
			out = tickOutcome{kind: RecordPanicked, err: fmt.Errorf("pabtdebug: the planner panicked: %v", r)}
		}
	}()
	exec := s.planner.TickCaptured(pabt.TickEnvironment{AttemptID: ticket.AttemptID})
	switch {
	case exec.CaptureError != nil && outcomeIsPanic(exec.CaptureError):
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
		// rather than a success the store cannot represent.
		record = s.terminalRecord(ctx, ticket, RecordCaptureFailed, outcome.exec, err)
		tree = nil
		out.Kind = RecordCaptureFailed
	}
	var delta *ExactTreeDelta
	if tree != nil {
		if base, found, baseErr := s.store.DeltaBase(ctx, s.key, ticket.Sequence); baseErr == nil && found {
			if computed, deltaErr := ComputeExactDelta(base, *tree); deltaErr == nil {
				delta = computed
			}
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
	return out
}

// buildRecord turns a captured tick into the record the store commits.
func (s *Session) buildRecord(
	ctx context.Context,
	ticket IntentTicket,
	outcome tickOutcome,
) (StoredRecord, *StoredTree, error) {
	exec := outcome.exec
	snapshot := exec.Snapshot
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
			PlanRevision: snapshot.PlanRevision,
			State:        snapshot.StateRevision,
			Coherence:    snapshot.Coherence,
		},
	}
	if !exec.StatusKnown {
		record.Status = 0
	}
	if outcome.kind != RecordTick {
		// A failure has no tree to claim, so it is not tree eligible and its
		// payload says so rather than carrying a partial one.
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
		record.PayloadState = PayloadNone
	}

	conditions, err := s.storedConditions(ctx, snapshot)
	if err != nil {
		return StoredRecord{}, nil, err
	}
	record.Conditions = conditions
	relations := s.storedRelations(ctx, snapshot)

	if outcome.kind == RecordTick && len(snapshot.Nodes) > 0 {
		tree, err := s.storedTree(snapshot)
		if err != nil {
			return StoredRecord{}, nil, err
		}
		record.Tree = tree
		record.TreeEligible = true
		record.Relations = relations
	} else {
		// A failure still records what the capture boundary managed to report, so
		// the reason a tick failed is visible even when the tree is not.
		record.Conditions = conditions
		record.Relations = relations
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

// storedTree turns the capture boundary's nodes into the store's tree.
//
// The nodes are already in preorder with their parents, so the tree is the same
// sequence with each node's content digest sealed. A node's ordinal follows from
// its position, which is what makes a later delta able to express a move.
func (s *Session) storedTree(snapshot pabt.TickSnapshot) (*StoredTree, error) {
	if len(snapshot.Nodes) == 0 {
		return nil, fmt.Errorf(`pabtdebug: the capture reported no nodes`)
	}
	tree := &StoredTree{
		RootID:       string(snapshot.RootID),
		Nodes:        make([]StoredNode, 0, len(snapshot.Nodes)),
		NodeCount:    uint32(len(snapshot.Nodes)),
		PlanRevision: snapshot.PlanRevision,
	}
	for _, occurrence := range snapshot.Nodes {
		status, err := NodeStatusOf(occurrence.Status)
		if err != nil {
			return nil, err
		}
		node := StoredNode{
			ID:        string(occurrence.ID),
			Lineage:   string(occurrence.Lineage),
			Type:      occurrence.Type.String(),
			Name:      occurrence.Name,
			Status:    status,
			Ordinal:   occurrence.Ordinal,
			EffectIDs: []string{},
			Frame:     occurrence.Frame,
		}
		if occurrence.Parent != "" {
			parent := string(occurrence.Parent)
			node.ParentID = &parent
		}
		digest, err := HashNode(node)
		if err != nil {
			return nil, err
		}
		node.ContentHash = digest
		tree.Nodes = append(tree.Nodes, node)
	}
	digest, err := HashTree(*tree)
	if err != nil {
		return nil, err
	}
	tree.Digest = digest
	return tree, nil
}

// storedConditions turns the capture boundary's conditions into the store's
// four-field evidence, keeping expected, current, observed, and uncertainty apart.
//
// A value the application did not declare stays unknown. It is never replaced by
// one derived from the key, because a key names a variable and not a
// requirement, and a consumer shown a derived value would be reading a claim
// nothing supports.
func (s *Session) storedConditions(
	ctx context.Context,
	snapshot pabt.TickSnapshot,
) ([]StoredCondition, error) {
	observed, uncertainties := observedByCondition(ctx, s, snapshot)
	out := make([]StoredCondition, 0, len(snapshot.Conditions))
	for _, occurrence := range snapshot.Conditions {
		source := "condition." + string(occurrence.ID)
		condition := StoredCondition{
			ConditionID: string(occurrence.ID),
			Kind:        "precondition",
			Revision: RevisionScope{
				PlanRevision: snapshot.PlanRevision,
				State:        snapshot.StateRevision,
				Coherence:    snapshot.Coherence,
			},
		}
		if occurrence.ExpectedKnown {
			encoded, err := s.codecs.Encode(ctx, source+".expected", occurrence.Expected)
			if err != nil {
				return nil, err
			}
			condition.Expected = encoded
		} else {
			condition.Expected = StoredValue{
				Visibility: ValueUnknown,
				Source:     source + ".expected",
				Error:      string(UncertaintyUndeclared),
			}
			condition.Uncertainty = StoredUncertainty{
				State:   UncertaintyUndeclared,
				Code:    "no_declaration",
				Message: "the application declares no expected value for this condition",
			}
		}
		// The current value is the one the planner read and matched. A condition the
		// planner never evaluated has none, which is not the same as having none.
		current := StoredValue{
			Visibility: ValueUnknown,
			Source:     source + ".current",
		}
		if occurrence.TruthKnown {
			current.Visibility = ValueExact
		}
		condition.Current = current
		if occurrence.TruthKnown {
			text, err := StatusText(occurrence.Truth, true)
			if err != nil {
				return nil, err
			}
			condition.Truth = text
		}
		if seen, ok := observed[string(occurrence.ID)]; ok {
			condition.Observed = &seen
		}
		if reason, ok := uncertainties[string(occurrence.ID)]; ok {
			condition.Uncertainty = reason
		} else if condition.Uncertainty.State == "" {
			condition.Uncertainty = StoredUncertainty{State: UncertaintyNone}
		}
		out = append(out, condition)
	}
	return out, nil
}

// observedByCondition collects the post-action values an observer reported, keyed
// by the condition whose post-condition node they were read at.
func observedByCondition(
	ctx context.Context,
	s *Session,
	snapshot pabt.TickSnapshot,
) (map[string]StoredValue, map[string]StoredUncertainty) {
	values := map[string]StoredValue{}
	unknown := map[string]StoredUncertainty{}
	postOf := map[string]string{}
	for _, occurrence := range snapshot.Conditions {
		if occurrence.PostNodeID != "" {
			postOf[string(occurrence.PostNodeID)] = string(occurrence.ID)
		}
	}
	for _, evidence := range snapshot.Executions {
		// An action's evidence is attached to the post-condition it re-evaluates,
		// which is how an observed value reaches the condition it explains.
		for _, observation := range evidence.Observation.EffectValues {
			if observation.Err != nil {
				unknown[string(observation.EffectID)] = StoredUncertainty{
					State:   UncertaintyError,
					Code:    "observer_error",
					Message: s.codecs.sanitizeText(errorText(observation.Err)),
				}
				continue
			}
			if !observation.Known {
				continue
			}
			encoded, err := s.codecs.Encode(ctx, "observed."+string(observation.EffectID), observation.Value)
			if err != nil {
				continue
			}
			values[string(observation.EffectID)] = encoded
		}
	}
	// The values a post-condition node reported attach to the condition that node
	// re-evaluates, which is what a reader of a precondition wants to see.
	if len(postOf) > 0 {
		for postNode, conditionID := range postOf {
			if seen, ok := values[postNode]; ok {
				values[conditionID] = seen
			}
		}
	}
	return values, unknown
}

// storedRelations turns the capture boundary's verdicts into stored relations,
// recording whether the planner considered a claim authoritative.
//
// The authority decision is the planner's, made in the capture boundary from the
// coherence of the revisions bracketing the action. It is recorded rather than
// recomputed, because recomputing it here would be a second opinion about a fact
// the planner already had.
func (s *Session) storedRelations(
	_ context.Context,
	snapshot pabt.TickSnapshot,
) []StoredRelation {
	var out []StoredRelation
	for _, evidence := range snapshot.Executions {
		for _, verdict := range evidence.Relations {
			relation := StoredRelation{
				RelationID:      string(verdict.EffectID) + "->" + string(verdict.PostconditionID),
				EffectID:        string(verdict.EffectID),
				PostconditionID: string(verdict.PostconditionID),
				Result:          string(verdict.Result),
				Method:          verdict.Method,
				Authoritative:   verdict.Authoritative,
				Revision:        RevisionScope{Coherence: snapshot.Coherence},
			}
			// The authority is recorded as its own word, so a reader can tell a
			// claim the planner stood behind from one it did not.
			if verdict.Authoritative {
				relation.Authority = "transition_verifier"
			} else {
				relation.Authority = "reported_not_authoritative"
			}
			if verdict.Err != nil {
				relation.Error = &StoredError{
					Code:    ErrorCodeVerifier,
					Type:    verdict.Err.Type,
					Message: s.codecs.sanitizeText(errorText(verdict.Err)),
				}
			}
			out = append(out, relation)
		}
	}
	return out
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

// outcomeIsPanic reports a capture failure that was really a panic.
//
// The capture boundary reports a panic through its capture error, because a panic
// is a failure to capture rather than a capture of a failure. The two are told
// apart by the error type the boundary records, which is what makes a panicked
// record distinguishable from a failed one.
func outcomeIsPanic(recorded *pabt.RecordedError) bool {
	return recorded != nil && strings.Contains(recorded.Type, "panic")
}

func errorText(recorded *pabt.RecordedError) string {
	if recorded == nil {
		return ""
	}
	return recorded.Message
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
