// Copyright 2021 Joseph Cumines
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build example
// +build example

package logic

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt"
	"github.com/joeycumines/go-pabt/examples/reefer-handoff/sim"
	"github.com/joeycumines/go-pabt/pabtdebug"
)

// Publisher is the owner-side published history for one tractor.
//
// It holds one store file and one run, bootstrapped on the first generation and
// reused for every later one (DD-35). The durable server serves exactly the
// newest run of a store and refuses to announce a record naming any other, so
// a history split across per-generation runs would strand every generation but
// the last beyond one server's reach, and a reader could not follow a world
// event to the plan tick that caused it across a replan. Each plan generation
// opens a fresh session on that same store and run; a retired generation's
// session is dropped, never ticked again. The store tolerates successive plan
// objects on one run: the only plan-revision check is intra-attempt, and the
// append lease is taken and released per tick, so generations serialize on the
// runner's single-threaded contract rather than on the store.
//
// The publisher is also the session's CausalityProvider: the commands a tick
// submits, the receipts that settled into it, and the world events the host
// drained buffer here and drain into the next committed record (DD-37). The
// drain happens inside the session's record build, after the planner has run,
// so a submission the tick's own action made rides in the same record as the
// tree it produced, and a receipt that settled between ticks rides in the
// record whose tree shows its effect — cause precedes effect in the chain.
//
// Settlements with no plan tick to ride in — a claim, a walk-home hop, a
// refusal after the world has settled — stay buffered until the next committed
// record; anything still buffered when the publisher flushes commits as one
// final tree-less control record.
//
// A wired example attaches the durable server's Announce to the publisher
// (AttachAnnounce, after an eager Open that bootstraps the store so a server
// can exist before the first generation opens). Every session the publisher
// then opens announces its committed records — the tick records and the
// boundary control record alike — so a live reader sees the walk-home
// causality the moment it lands rather than only when it pages the run.
type Publisher struct {
	tug sim.TractorID
	dir string

	mu       sync.Mutex
	store    *pabtdebug.SQLiteStore
	session  *pabtdebug.Session
	identity pabtdebug.SessionIdentity
	gen      uint64

	// announce is the function every session this publisher opens announces
	// its committed records through — the durable server's Announce in a wired
	// example. It is set once, before the first generation begins: a session
	// that opened without it would publish durably but silently, and a
	// half-announced run is the split-brain a live reader must never see.
	announce func(context.Context, pabtdebug.CommitReceipt, pabtdebug.StoredRecord) error

	// The causal buffers, drained by Causality. They are lists rather than
	// single slots because more than one command can settle between two plan
	// ticks: a delivered tractor walks home over several ticks, and each hop's
	// submission and receipt buffer until the next record carries them. The
	// entries link to each other by idempotency key, never by position.
	settled   []pabtdebug.CausalitySettlement
	submitted []pabtdebug.CausalitySubmission
	exogenous []pabtdebug.CausalityEvent

	// stamp is the frame the tick currently being recorded reasoned over. It
	// is set before each session tick and drained with the buffers, so a
	// control flush carries no frame and no tick of its own.
	stamp frameStamp
}

// frameStamp names the plan tick and the world frame one record belongs to.
type frameStamp struct {
	tick     uint64
	gen      uint64
	worldSeq uint64
	revision string
}

func (f frameStamp) isZero() bool {
	return f.tick == 0 && f.gen == 0 && f.worldSeq == 0 && f.revision == ""
}

// NewPublisher builds the owner-side publisher for one tractor. The store file
// lands under dir, one per tractor.
func NewPublisher(tug sim.TractorID, dir string) *Publisher {
	return &Publisher{tug: tug, dir: dir}
}

// Tractor returns whose history this publisher holds.
func (p *Publisher) Tractor() sim.TractorID { return p.tug }

// Store returns the committed store, for a reader that wants the history the
// publisher wrote. It is nil until the first generation begins.
func (p *Publisher) Store() *pabtdebug.SQLiteStore {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.store
}

// RunKey returns the run every generation of this tractor records into.
func (p *Publisher) RunKey() pabtdebug.RunKey {
	p.mu.Lock()
	defer p.mu.Unlock()
	return pabtdebug.RunKey{
		SessionID: p.identity.SessionID,
		PlanID:    p.identity.PlanID,
		RunID:     p.identity.RunID,
	}
}

// Open bootstraps the tractor's store and run if they do not exist yet, so a
// durable server can be built on the store before the first generation opens.
// It is idempotent: a publisher whose first BeginGeneration has already run
// holds the store and Open does nothing.
func (p *Publisher) Open(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ensureStoreLocked(ctx)
}

// AttachAnnounce sets the function every session this publisher opens
// announces its committed records through. It is set once, before the first
// generation begins — the Announce of a durable server built on this
// publisher's store, following the evidence target's construction order: open
// the store, build the server, attach, then let generations open. A session
// that opened without it publishes durably but silently, and a run whose early
// generations are silent and whose later ones are not is the split-brain a
// live reader must never see.
func (p *Publisher) AttachAnnounce(fn func(context.Context, pabtdebug.CommitReceipt, pabtdebug.StoredRecord) error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.announce = fn
}

// ensureStoreLocked bootstraps the store and run once. The caller holds the lock.
func (p *Publisher) ensureStoreLocked(ctx context.Context) error {
	if p.store != nil {
		return nil
	}
	store, err := pabtdebug.OpenSQLiteStore(ctx, pabtdebug.SQLiteStoreConfig{
		Path: filepath.Join(p.dir, string(p.tug), "history.sqlite"),
	})
	if err != nil {
		return fmt.Errorf("logic: %s opening history: %w", p.tug, err)
	}
	identity, err := store.Bootstrap(ctx, pabtdebug.BootstrapRequest{
		PlanID: string(p.tug), RunID: string(p.tug), Title: "reefer " + string(p.tug),
	})
	if err != nil {
		_ = store.Close()
		return fmt.Errorf("logic: %s bootstrapping history: %w", p.tug, err)
	}
	p.store = store
	p.identity = identity
	return nil
}

// BeginGeneration opens a fresh session for a new plan generation on the
// tractor's one store and run. The first call bootstraps the store; later
// calls reuse it, because the run — not the plan object — is what the durable
// server serves (DD-35).
func (p *Publisher) BeginGeneration(ctx context.Context, gen uint64, plan pabt.CapturedPlanner) error {
	// The outgoing generation's buffered causality commits as one tree-less
	// control record before this plan's first tree is built. Two independent
	// plans share the run-prefixed occurrence-ID space, so a delta chained
	// from one plan's last tree to the other's first does not reconstruct and
	// the commit is refused; the control record is the blocker that forces the
	// new plan's first tree to be a keyframe. This is the same single-keyframe
	// cost DD-37 accepts at end-of-history, paid deliberately at the
	// generation boundary. A first generation buffers nothing, and a flush
	// with an empty envelope commits nothing.
	if err := p.Flush(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.ensureStoreLocked(ctx); err != nil {
		return err
	}
	session, err := pabtdebug.OpenSession(ctx, p.store, plan, pabtdebug.SessionConfig{
		Identity:  p.identity,
		Causality: p,
		Announce:  p.announce,
	})
	if err != nil {
		return fmt.Errorf("logic: %s opening generation %d session: %w", p.tug, gen, err)
	}
	p.session = session
	p.gen = gen
	return nil
}

// PublishTickPlan commits one root tick's plan record. The frame the tick
// reasoned over is stamped first, so the record's causality names it; the
// session then runs the plan's captured tick — the same single execution the
// bare node would run — and the record build drains the stamp and the causal
// buffers into the committed envelope. The returned status feeds the runner's
// existing switch, so the runner cannot disagree with what was published.
//
// The publisher's lock is never held across the session tick: the plan's action
// nodes note submissions into this same publisher during the tick, and
// Causality takes the lock inside the record build. The runner's own lock is
// held across the call, and the order is always runner-then-publisher, so the
// synchronous publish cannot deadlock against the runner.
func (p *Publisher) PublishTickPlan(ctx context.Context, tickSeq, gen uint64, frame sim.Observation) (btStatus, error) {
	p.mu.Lock()
	if p.session == nil {
		p.mu.Unlock()
		return btStatusRunning, fmt.Errorf("logic: %s has no open generation", p.tug)
	}
	p.stamp = frameStamp{tick: tickSeq, gen: gen, worldSeq: frame.WorldSeq, revision: frame.Revision}
	session := p.session
	p.mu.Unlock()

	res := session.Tick(ctx)
	if res.Err != nil {
		return btStatusRunning, fmt.Errorf("logic: %s publishing tick %d: %w", p.tug, tickSeq, res.Err)
	}
	if !res.StatusPresent {
		return btStatusRunning, fmt.Errorf("logic: %s tick %d produced no status", p.tug, tickSeq)
	}
	status := bt.Status(res.Status)
	if err := pabt.ValidateStatus(status); err != nil {
		return btStatusRunning, fmt.Errorf("logic: %s tick %d produced no status: %w", p.tug, tickSeq, err)
	}
	return status, nil
}

// NoteSubmitted buffers the command a tick submitted, at submit time: its kind,
// the expectation versions it was decided against, and the action instance
// that caused it. Recording at submit time matters because the submission
// happens during the session tick, and the in-flight record afterwards names
// only the key — the submitter is the only party that knows what the command
// was.
//
// The caller holds the runner's lock; this takes only the publisher's, which
// the publish path never holds across the session tick, so the ordering is
// safe.
func (p *Publisher) NoteSubmitted(cmd sim.Command, actionID uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.submitted = append(p.submitted, pabtdebug.CausalitySubmission{
		CommandID:      cmd.ID,
		IdempotencyKey: cmd.IdempotencyKey,
		Kind:           string(cmd.Kind),
		Tractor:        string(cmd.Tractor),
		Reefer:         string(cmd.Reefer),
		Slot:           string(cmd.Slot),
		Node:           string(cmd.Node),
		PlanTickSeq:    cmd.PlanTickSeq,
		ActionID:       actionID,
		Generation:     cmd.Generation,
		Expect: pabtdebug.CausalityExpectation{
			WorldSeq:    cmd.Expect.WorldSeq,
			SlotVersion: cmd.Expect.SlotVersion,
			LaneVersion: cmd.Expect.LaneVersion,
			TagVersion:  cmd.Expect.TagVersion,
		},
	})
}

// NoteSettled buffers the answer the world gave to one command, routed by the
// submit key the caller settled under. The receipt, its world sequence, and
// the events it published complete the causal envelope of the next committed
// record — the record whose tree shows the effect names the receipt that
// caused it. A refusal is buffered with its typed reason and holder and no
// events, which is what makes "a rejected command is visible with no world
// effect" a reading rather than an inference. An abandoned command is
// buffered as an abandonment, never as a refusal the world made.
func (p *Publisher) NoteSettled(key string, res sim.Result) {
	p.mu.Lock()
	defer p.mu.Unlock()
	set := pabtdebug.CausalitySettlement{IdempotencyKey: key}
	if res.Abandoned != nil {
		set.CommandID = res.Abandoned.CommandID
		set.Abandoned = &pabtdebug.CausalityAbandoned{
			CommandID: res.Abandoned.CommandID,
			Reason:    string(res.Abandoned.Reason),
		}
	} else {
		r := res.Receipt
		set.CommandID = r.ID
		set.Kind = string(r.Kind)
		set.Outcome = string(r.Outcome)
		set.Reason = string(r.Reason)
		set.Holder = r.Holder
		set.WorldSeq = r.WorldSeq
		set.WorldRevision = r.WorldRevision
		set.CommandSeq = r.CommandSeq
		for _, ev := range res.Events {
			set.Events = append(set.Events, causalityEvent(ev))
		}
	}
	p.settled = append(p.settled, set)
}

// NoteEvents buffers world events no command caused — a lane the wind
// controller closed, a plug the power desk pulled, a round boundary reached.
// They carry the worldSeq and revision that produced them and no cause
// command, which is what tells them apart from a command's effect.
func (p *Publisher) NoteEvents(events []sim.WorldEvent) {
	if len(events) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, ev := range events {
		p.exogenous = append(p.exogenous, causalityEvent(ev))
	}
}

// Causality drains the stamp and the buffers into one envelope for the record
// being built, and clears them so the data rides exactly one record. A tick
// that produces no record (a panic, a capture failure) never consults the
// provider, so its buffers stay parked for the next record.
func (p *Publisher) Causality(ctx context.Context) (*pabtdebug.Causality, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stamp.isZero() && len(p.settled) == 0 && len(p.submitted) == 0 && len(p.exogenous) == 0 {
		return nil, nil
	}
	out := &pabtdebug.Causality{
		Generation:    p.stamp.gen,
		PlanTickSeq:   p.stamp.tick,
		FrameWorldSeq: p.stamp.worldSeq,
		FrameRevision: p.stamp.revision,
		Settled:       p.settled,
		Submitted:     p.submitted,
		Exogenous:     p.exogenous,
	}
	p.settled, p.submitted, p.exogenous = nil, nil, nil
	p.stamp = frameStamp{}
	return out, nil
}

// Flush commits whatever causality is still buffered as one tree-less control
// record, without releasing the store. A walk-home receipt that settled after
// the last plan tick has no tick to ride in; the control record is where it
// lands, and the single keyframe it forces is at the end of a history, where
// it costs nothing (DD-37). A flush with an empty envelope commits nothing.
func (p *Publisher) Flush() error {
	p.mu.Lock()
	session := p.session
	p.mu.Unlock()
	if session == nil {
		return nil
	}
	_, committed, err := session.CommitControl(context.Background())
	if err != nil && !committed {
		return fmt.Errorf("logic: %s flushing causality: %w", p.tug, err)
	}
	// A record that landed but was not announced is the same non-fatal contract
	// a tick's AnnounceErr carries: the causality is durable and a reader that
	// pages the run sees it; only a live subscriber was not told. Failing the
	// flush here would fail a generation open whose boundary record already
	// committed, and strand the runner holding a plan with no session.
	return nil
}

// Close flushes the buffered causality and then releases the store. It is
// idempotent: a second call finds no session and no store and does nothing.
func (p *Publisher) Close() error {
	flushErr := p.Flush()
	p.mu.Lock()
	store := p.store
	p.store, p.session = nil, nil
	p.mu.Unlock()
	var closeErr error
	if store != nil {
		closeErr = store.Close()
	}
	if flushErr != nil {
		return flushErr
	}
	return closeErr
}

// causalityEvent translates one world fact into the neutral envelope shape.
// An empty CauseCommand means no command produced it, which is what tells an
// exogenous change apart from a command's effect.
func causalityEvent(ev sim.WorldEvent) pabtdebug.CausalityEvent {
	return pabtdebug.CausalityEvent{
		Kind:          string(ev.Kind),
		WorldSeq:      ev.WorldSeq,
		WorldRevision: ev.WorldRevision,
		Tractor:       string(ev.Tractor),
		Reefer:        string(ev.Reefer),
		Slot:          string(ev.Slot),
		Node:          string(ev.Node),
		Lane:          string(ev.Lane),
		CauseCommand:  ev.CauseCommand,
	}
}
