//go:build example

package brain

import (
	"fmt"
	"sync/atomic"

	"github.com/joeycumines/go-pabt"
	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

// Brain is one minotaur's PA-BT planner and belief. It is driven by the
// authoritative loop: each tick, the brain updates its belief from the world
// snapshot, ticks the planner, and encodes the BT frame before the tick
// returns.
type Brain struct {
	ID   string
	maze *maze.Maze

	// belief is the minotaur's model of the world. It is the single source of
	// truth the planner reads.
	belief Belief

	// state and planner.
	state *State
	plan  *pabt.Plan[Condition]

	// beliefRevision increments on every belief mutation so the state view can
	// report a CoherenceRevisioned revision.
	beliefRevision atomic.Uint64

	// paused skips TickCaptured when true. The minotaur's decisions freeze but
	// the game does not pause.
	paused bool

	// movedLastTick reports whether the planner's move actions stepped the
	// actor during the most recent Tick. Strategy movement yields on it so
	// an actor is never moved twice per tick, and never frozen by a stale
	// belief flag when the planner is idle.
	movedLastTick bool

	// lastFrame is the most recent encoded BT frame, for the explorer.
	lastFrame *BTFrame

	// selfPos points to the minotaur's live position in the world.
	selfPos *maze.Vec2
}

// NewBrain builds a brain for a minotaur at the given position.
func NewBrain(id string, m *maze.Maze, selfPos *maze.Vec2) (*Brain, error) {
	b := &Brain{
		ID:      id,
		maze:    m,
		selfPos: selfPos,
	}
	b.state = NewState(&b.belief, m, selfPos)
	if err := b.buildPlan(); err != nil {
		return nil, err
	}
	return b, nil
}

// buildPlan (re)constructs the captured planner over the current belief.
func (b *Brain) buildPlan() error {
	// Goal DNF: caught-player OR search-exhausted.
	goal := []Conditions{
		{&BoolCond{K: KeyCaught, Want: true}},
		{&BoolCond{K: KeySearchExhausted, Want: true}},
	}
	plan, err := pabt.NewCaptured(
		b.state,
		goal,
		pabt.WithStateViews[Condition](NewViewProvider(b.state, b)),
		pabt.WithExecutionObserver[Condition](NewObserver(b.state)),
		pabt.WithTransitionVerifier[Condition](NewVerifier(b.state)),
	)
	if err != nil {
		return err
	}
	b.plan = plan
	return nil
}

// BeliefRevision returns the current belief revision.
func (b *Brain) BeliefRevision() uint64 {
	return b.beliefRevision.Load()
}

// SetMoveSpeed sets the per-tick movement distance this actor's move actions
// use. Distinct actor archetypes move at distinct speeds; the default (zero)
// keeps the library's historical speed.
func (b *Brain) SetMoveSpeed(cellsPerTick float64) {
	b.state.MoveSpeed = cellsPerTick
}

// UpdateBelief refreshes the brain's belief from the world snapshot and the
// minotaur's own position. It is called by the loop before Tick.
func (b *Brain) UpdateBelief(snap *world.Snapshot, self *world.Minotaur) {
	b.belief.Stunned = self.Stunned
	// Line of sight: the player is detected if within sight range and not
	// blocked. For the brain's purposes we use a simple distance heuristic;
	// the authoritative LOS check lands with the netcode task.
	dx := snap.Runner.Pos.X - self.Pos.X
	dy := snap.Runner.Pos.Y - self.Pos.Y
	dist2 := dx*dx + dy*dy
	const sightRange2 = 6.0 * 6.0
	detected := dist2 <= sightRange2 && !self.Stunned

	b.belief.PlayerDetected = detected
	if detected {
		b.belief.PlayerPos = snap.Runner.Pos
		b.belief.LastKnownPos = snap.Runner.Pos
		b.belief.HasLastKnown = true
		b.belief.SearchExhausted = false
		// Distance band.
		switch {
		case dist2 < 1.5*1.5:
			b.belief.DistanceBand = 0
		case dist2 < 4.0*4.0:
			b.belief.DistanceBand = 1
		default:
			b.belief.DistanceBand = 2
		}
	} else {
		b.belief.DistanceBand = 3
		// Caught is an edge, not a level: once line of sight is lost the
		// catch is over. Leaving it latched true satisfies the planner's
		// goal forever, idling the brain while strategy movement yields.
		b.belief.Caught = false
	}
	b.beliefRevision.Add(1)
}

// Tick runs one planner tick and encodes the BT frame. It returns the encoded
// frame, or nil if the brain is paused. The frame is encoded INSIDE the tick
// boundary because the snapshot's values alias live planner state.
func (b *Brain) Tick(attemptID string) *BTFrame {
	if b.paused {
		b.movedLastTick = false
		return nil
	}
	result := b.plan.TickCaptured(pabt.TickEnvironment{AttemptID: attemptID})
	b.movedLastTick = b.state.movedThisTick
	b.state.movedThisTick = false
	path := pabt.GetPath(b.plan)
	frame := EncodeFrame(result.Snapshot, path)
	b.lastFrame = frame
	return frame
}

// TickDebug runs one planner tick and returns the frame plus a human-readable
// diagnostic of the tick outcome. For debugging only.
func (b *Brain) TickDebug(attemptID string) (*BTFrame, string) {
	if b.paused {
		b.movedLastTick = false
		return nil, "paused"
	}
	result := b.plan.TickCaptured(pabt.TickEnvironment{AttemptID: attemptID})
	b.movedLastTick = b.state.movedThisTick
	b.state.movedThisTick = false
	path := pabt.GetPath(b.plan)
	frame := EncodeFrame(result.Snapshot, path)
	b.lastFrame = frame

	diag := fmt.Sprintf("status=%v statusKnown=%v", result.Status, result.StatusKnown)
	if result.PlannerError != nil {
		diag += " plannerErr=" + result.PlannerError.Message
	}
	if result.CaptureError != nil {
		diag += " captureErr=" + result.CaptureError.Message
	}
	diag += fmt.Sprintf(" rev=%d nodes=%d conds=%d actions=%d execs=%d path=%q",
		result.Snapshot.PlanRevision, len(result.Snapshot.Nodes),
		len(result.Snapshot.Conditions), len(result.Snapshot.Actions),
		len(result.Snapshot.Executions), frame.Path)
	for _, c := range result.Snapshot.Conditions {
		diag += fmt.Sprintf(" cond[%v truth=%v known=%v]", c.Key, c.Truth, c.TruthKnown)
	}
	return frame, diag
}

// LastFrame returns the most recent encoded BT frame.
func (b *Brain) LastFrame() *BTFrame {
	return b.lastFrame
}

// Replan rebuilds the planner, bumping the plan revision. This is the T8
// "force replan" operation.
func (b *Brain) Replan() error {
	return b.buildPlan()
}

// Pause freezes the brain's decisions. The game does not pause.
func (b *Brain) Pause() { b.paused = true }

// Resume unfreezes the brain.
func (b *Brain) Resume() { b.paused = false }

// InjectBelief redirects the hunter by mutating its belief. This is the T8
// "inject false belief" operation.
func (b *Brain) InjectBelief(playerPos maze.Vec2) {
	b.belief.PlayerDetected = true
	b.belief.PlayerPos = playerPos
	b.belief.LastKnownPos = playerPos
	b.belief.HasLastKnown = true
	b.belief.SearchExhausted = false
	b.beliefRevision.Add(1)
}

// Belief returns a copy of the current belief, for inspection.
func (b *Brain) BeliefCopy() Belief { return b.belief }

// MovedLastTick reports whether the planner's move actions stepped the
// actor during the most recent Tick.
func (b *Brain) MovedLastTick() bool { return b.movedLastTick }
