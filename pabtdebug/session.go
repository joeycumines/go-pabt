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
	// The snapshot's revision is the most precise answer, but a recovered panic has
	// no snapshot at all. Taking the higher of the two keeps the record at or above
	// the revision the attempt began at, which is what the store requires, rather
	// than zero, which it refuses.
	planRevision := snapshot.PlanRevision
	if planRevision < exec.PlanRevision {
		planRevision = exec.PlanRevision
	}
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
	if outcome.kind == RecordTick && len(snapshot.Nodes) > 0 {
		tree, err := s.storedTree(snapshot)
		if err != nil {
			return StoredRecord{}, nil, err
		}
		record.Tree = tree
		record.TreeEligible = true
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
			PlanRevision: max64(exec.PlanRevision, 0),
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
		// The run is named here so a delta can be computed against a base; the store
		// stamps the event and sequence when it commits. A tree that did not name
		// its run could not be compared with any base, so no delta would ever be
		// stored from this path.
		RunKey:       s.key,
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
	observed, uncertainties := s.observedByCondition(ctx, snapshot)
	currentByCondition := currentValuesByCondition(ctx, s, snapshot)
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
		// The current value is the one the planner actually read and matched, taken
		// from the variable read recorded for this condition. A condition the
		// planner never evaluated has none, and claiming it is exact while carrying
		// nothing would tell a reader the value is available when it is not.
		current := StoredValue{
			Visibility: ValueUnknown,
			Source:     source + ".current",
		}
		if read, ok := currentByCondition[string(occurrence.ID)]; ok {
			current = read
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

// observedByCondition attaches the post-action values an observer reported to the
// condition each one explains.
//
// The bridge is through the action, not through a node. An observation names the
// effect it read, an effect belongs to an action, and an action's postcondition is
// the condition whose re-evaluation the action was there to make. Looking a
// post-condition *node* id up in a map keyed by *effect* ids finds nothing, because
// the two are minted from one counter but are different identities, and that
// mistake left every condition with no observed value at all.
func (s *Session) observedByCondition(
	ctx context.Context,
	snapshot pabt.TickSnapshot,
) (map[string]StoredValue, map[string]StoredUncertainty) {
	values := map[string]StoredValue{}
	unknown := map[string]StoredUncertainty{}

	postOfAction := make(map[string]string, len(snapshot.Actions))
	for _, action := range snapshot.Actions {
		if action.PostconditionID != "" {
			postOfAction[string(action.ID)] = string(action.PostconditionID)
		}
	}
	for _, evidence := range snapshot.Executions {
		conditionID, ok := postOfAction[string(evidence.Execution.ActionID)]
		if !ok {
			continue
		}
		for _, observation := range evidence.Observation.EffectValues {
			source := "observed." + conditionID + "." + string(observation.EffectID)
			switch {
			case observation.Err != nil:
				unknown[conditionID] = StoredUncertainty{
					State:   UncertaintyError,
					Code:    "observer_error",
					Message: s.codecs.sanitizeText(observation.Err.Message),
				}
			case !observation.Known:
				if _, already := unknown[conditionID]; !already {
					unknown[conditionID] = StoredUncertainty{
						State:   UncertaintyUnavailable,
						Code:    "not_observed",
						Message: "the observer did not read this value",
					}
				}
			default:
				encoded, err := s.codecs.Encode(ctx, source, observation.Value)
				if err != nil {
					unknown[conditionID] = StoredUncertainty{
						State:   UncertaintyError,
						Code:    "encoding_failed",
						Message: s.codecs.sanitizeText(err.Error()),
					}
					continue
				}
				values[conditionID] = encoded
			}
		}
	}
	return values, unknown
}

// currentValuesByCondition collects the variable each condition was matched
// against.
//
// The capture boundary records the read that fed the matcher, so the value is the
// one the planner saw rather than one looked up again afterwards, which could
// have moved.
func currentValuesByCondition(
	ctx context.Context,
	s *Session,
	snapshot pabt.TickSnapshot,
) map[string]StoredValue {
	out := map[string]StoredValue{}
	for _, callback := range snapshot.Callbacks {
		if callback.Kind != pabt.CallbackVariableRead || callback.SubjectID == "" {
			continue
		}
		encoded, err := s.codecs.Encode(ctx, "current."+string(callback.SubjectID), callback.Result)
		if err != nil {
			continue
		}
		out[string(callback.SubjectID)] = encoded
	}
	return out
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

func errorText(recorded *pabt.RecordedError) string {
	if recorded == nil {
		return ""
	}
	return recorded.Message
}

// max64 returns the higher of two revisions, so a record is never stamped below
// the revision its attempt began at.
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
