//go:build example

package brain

import (
	"fmt"
	"math"
	"sync/atomic"

	"github.com/joeycumines/go-pabt"
	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

// Brain is one actor's PA-BT planner and world view. It is driven by the
// authoritative loop: each tick, the brain updates its world view from the
// world snapshot, ticks the planner, and encodes the BT frame before the tick
// returns.
type Brain struct {
	ID   string
	maze *maze.Maze

	// world is the actor's model of the world. It is the single source of
	// truth the planner reads.
	world WorldView

	// state and planner.
	state *State
	plan  *pabt.Plan[Condition]

	// worldRevision increments on every world view mutation so the state view
	// can report a CoherenceRevisioned revision.
	worldRevision atomic.Uint64

	// paused skips TickCaptured when true. The actor's decisions freeze but
	// the game does not pause.
	paused bool

	// movedLastTick reports whether the planner's move actions stepped the
	// actor during the most recent Tick. Strategy movement yields on it so
	// an actor is never moved twice per tick, and never frozen by a stale
	// world flag when the planner is idle.
	movedLastTick bool

	// lastFrame is the most recent encoded BT frame, for the explorer.
	lastFrame *BTFrame

	// replanLog records recent plan modifications for the I5 reasoning
	// overlay: subtree expansions and conflict relocations (repair) and
	// full rebuilds (replan). Capped; most recent last.
	replanLog []ReplanEvent

	// lastPlanRevision is the plan revision seen by the previous Tick, used
	// to detect revision bumps (subtree expansion / conflict resolution).
	lastPlanRevision uint64

	// selfPos points to the actor's live position in the world.
	selfPos *maze.Vec2

	// lastTelemetry stores the causal diagnostics from the most recent Tick.
	lastTelemetry TickTelemetry
}

// NewBrain builds a brain for an actor at the given position.
func NewBrain(id string, m *maze.Maze, selfPos *maze.Vec2) (*Brain, error) {
	b := &Brain{
		ID:      id,
		maze:    m,
		selfPos: selfPos,
	}
	b.state = NewState(&b.world, m, selfPos)
	if err := b.buildPlan(); err != nil {
		return nil, err
	}
	return b, nil
}

// buildPlan (re)constructs the captured planner over the current world view.
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
	b.lastPlanRevision = plan.PlannerRevision()
	b.logReplanEvent(ReplanEvent{Type: "replan", Reason: "full rebuild", Revision: plan.PlannerRevision()})
	return nil
}

// logReplanEvent appends an event to the replan log, keeping the most recent
// five. The brain is driven by the single-writer loop, so no lock is needed.
func (b *Brain) logReplanEvent(evt ReplanEvent) {
	b.replanLog = append(b.replanLog, evt)
	if len(b.replanLog) > 5 {
		b.replanLog = b.replanLog[len(b.replanLog)-5:]
	}
}

// detectPlanChange compares the plan revision before and after a tick. A
// bump means the planner inserted a subtree (condition expansion) or
// relocated a conflicting one — a local repair, not a full rebuild.
func (b *Brain) detectPlanChange(rev uint64) {
	if rev > b.lastPlanRevision {
		b.logReplanEvent(ReplanEvent{Type: "repair", Reason: "subtree expanded", Revision: rev})
	}
	b.lastPlanRevision = rev
}

// WorldRevision returns the current world view revision.
func (b *Brain) WorldRevision() uint64 {
	return b.worldRevision.Load()
}

// SetMoveSpeed sets the per-tick movement distance this actor's move actions
// use. Distinct actor archetypes move at distinct speeds; the default (zero)
// keeps the library's historical speed.
func (b *Brain) SetMoveSpeed(cellsPerTick float64) {
	b.state.MoveSpeed = cellsPerTick
}

// GetMoveSpeed returns the per-tick movement distance currently set on this
// brain's state. It is zero when SetMoveSpeed has never been called.
func (b *Brain) GetMoveSpeed() float64 {
	return b.state.MoveSpeed
}

// SetArchetype records the actor's archetype (0=Brute, 1=Swarm, 2=Caster, 3=Stalker)
// so PA-BT actions can express archetype-specific pursuit and ranged behavior.
func (b *Brain) SetArchetype(typ int) {
	b.world.SelfArchetype = typ
}

// SetPatrolTarget provides the PA-BT brain with a patrol/strategy waypoint to
// search when the player is not currently detected or tracked.
func (b *Brain) SetPatrolTarget(pos maze.Vec2) {
	b.world.PatrolTarget = pos
	b.world.HasPatrolTarget = true
}

// UpdateWorldView refreshes the brain's world view from the world snapshot
// and the actor's own position. It is called by the loop before Tick.
func (b *Brain) UpdateWorldView(snap *world.Snapshot, self *world.Minotaur) {
	b.world.Stunned = self.Stunned
	b.world.StunTicks = self.StunTicks
	if self.Stunned {
		b.world.SearchExhausted = false
		b.world.Caught = false
	}
	b.world.SelfElement = self.Element
	b.world.SelfArchetype = self.Type
	b.world.PlayerElement = snap.Runner.Element
	b.world.CastBoltThisTick = false
	b.world.ArchetypeAction = ""

	if b.world.DisadvantageResolvedTicks > 0 {
		b.world.DisadvantageResolvedTicks--
	}

	dx := snap.Runner.Pos.X - self.Pos.X
	dy := snap.Runner.Pos.Y - self.Pos.Y
	dist2 := dx*dx + dy*dy
	const sightRange2 = 8.0 * 8.0

	runnerCellWalkable := b.maze == nil || b.maze.At(int(math.Floor(snap.Runner.Pos.X)), int(math.Floor(snap.Runner.Pos.Y))).Walkable()
	hasLOS := b.maze == nil || HasClearLOS(b.maze, self.Pos, snap.Runner.Pos)
	detected := dist2 <= sightRange2 && !self.Stunned && hasLOS

	b.world.PlayerDetected = detected
	if detected {
		b.world.PlayerPos = snap.Runner.Pos
		b.world.LastKnownPos = snap.Runner.Pos
		b.world.HasLastKnown = true
		b.world.SearchExhausted = false
		if dist2 >= 0.65*0.65 {
			b.world.Caught = false
		}
		switch {
		case dist2 < 1.5*1.5:
			b.world.DistanceBand = 0
		case dist2 < 4.0*4.0:
			b.world.DistanceBand = 1
		default:
			b.world.DistanceBand = 2
		}
	} else {
		b.world.DistanceBand = 3
		b.world.Caught = false
		// Acoustic proximity clue: nearby footsteps within 3.5u behind a wall
		// update LastKnownPos so SearchAction investigates around corners
		// without falsely claiming line-of-sight PlayerDetected.
		if !self.Stunned && dist2 <= 3.5*3.5 && runnerCellWalkable {
			b.world.LastKnownPos = snap.Runner.Pos
			b.world.HasLastKnown = true
			b.world.SearchExhausted = false
		} else if !self.Stunned && !b.world.HasLastKnown && b.world.HasPatrolTarget {
			if math.Hypot(b.world.PatrolTarget.X-self.Pos.X, b.world.PatrolTarget.Y-self.Pos.Y) >= 0.5 {
				b.world.LastKnownPos = b.world.PatrolTarget
				b.world.SearchExhausted = false
			}
		}
	}

	// Elemental advantage: fire > ice > lightning > void > fire.
	// advantage(attacker, defender) = 2x if (attacker+1)%4 == defender.
	playerBeatsSelf := (b.world.PlayerElement+1)%4 == b.world.SelfElement
	selfBeatsPlayer := (b.world.SelfElement+1)%4 == b.world.PlayerElement
	dist := math.Sqrt(dist2)
	b.world.AtDisadvantage = detected && playerBeatsSelf && b.world.DisadvantageResolvedTicks == 0 && dist >= 0.6
	b.world.AtAdvantage = selfBeatsPlayer

	// Find the nearest friendly same-element ally for ReinforceAction.
	b.world.HasReinforceTarget = false
	const allySightRange2 = 8.0 * 8.0
	bestDist2 := allySightRange2
	for _, other := range snap.Minotaurs {
		if other.Element != self.Element || other.HP <= 0 || other.Stunned {
			continue
		}
		adx := other.Pos.X - self.Pos.X
		ady := other.Pos.Y - self.Pos.Y
		ad2 := adx*adx + ady*ady
		if ad2 < bestDist2 && ad2 > 1e-6 {
			bestDist2 = ad2
			b.world.ReinforceTarget = other.Pos
			b.world.HasReinforceTarget = true
		}
	}

	// I4: Allied yielding — same-element actors path around the player.
	b.world.AllyYielding = false
	if b.world.PlayerDetected && b.world.SelfElement == b.world.PlayerElement {
		rdx := snap.Runner.Pos.X - self.Pos.X
		rdy := snap.Runner.Pos.Y - self.Pos.Y
		if rdx*rdx+rdy*rdy < 3.0*3.0 {
			b.world.AllyYielding = true
		}
	}

	b.worldRevision.Add(1)
}

// Tick runs one planner tick and encodes the BT frame. It returns the encoded
// frame, or nil if the brain is paused. The frame is encoded INSIDE the tick
// boundary because the snapshot's values alias live planner state.
func (b *Brain) Tick(attemptID string) *BTFrame {
	if b.paused {
		b.movedLastTick = false
		return nil
	}
	var prePos maze.Vec2
	if b.selfPos != nil {
		prePos = *b.selfPos
	}
	result := b.plan.TickCaptured(pabt.TickEnvironment{AttemptID: attemptID})
	var postPos maze.Vec2
	if b.selfPos != nil {
		postPos = *b.selfPos
	}
	b.movedLastTick = b.state.movedThisTick
	b.state.movedThisTick = false
	b.detectPlanChange(result.Snapshot.PlanRevision)
	path := pabt.GetPath(b.plan)
	frame := EncodeFrame(result.Snapshot, path)
	frame.WorldView = b.worldViewFrame()
	frame.ReplanEvents = append(frame.ReplanEvents, b.replanLog...)
	b.lastFrame = frame
	b.lastTelemetry = b.buildTickTelemetry(prePos, postPos, b.movedLastTick, frame)
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
	b.detectPlanChange(result.Snapshot.PlanRevision)
	path := pabt.GetPath(b.plan)
	frame := EncodeFrame(result.Snapshot, path)
	frame.WorldView = b.worldViewFrame()
	frame.ReplanEvents = append(frame.ReplanEvents, b.replanLog...)
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

// Replan rebuilds the planner, bumping the plan revision. This is the
// "force replan" operation.
func (b *Brain) Replan() error {
	return b.buildPlan()
}

// Pause freezes the brain's decisions. The game does not pause.
func (b *Brain) Pause() { b.paused = true }

// Resume unfreezes the brain.
func (b *Brain) Resume() { b.paused = false }

// Paused returns whether the brain is paused.
func (b *Brain) Paused() bool { return b.paused }

// InjectWorldView redirects the hunter by mutating its world view. This is
// the "inject false state" operation.
func (b *Brain) InjectWorldView(playerPos maze.Vec2) {
	b.world.PlayerDetected = true
	b.world.PlayerPos = playerPos
	b.world.LastKnownPos = playerPos
	b.world.HasLastKnown = true
	b.world.SearchExhausted = false
	b.worldRevision.Add(1)
}

// WorldViewCopy returns a copy of the current world view, for inspection.
func (b *Brain) WorldViewCopy() WorldView { return b.world }

// worldViewFrame encodes the current world view for the debugging GUI.
func (b *Brain) worldViewFrame() BTWorldView {
	return BTWorldView{
		PlayerElement:  b.world.PlayerElement,
		SelfElement:    b.world.SelfElement,
		AtDisadvantage: b.world.AtDisadvantage,
		AtAdvantage:    b.world.AtAdvantage,
		PlayerDetected: b.world.PlayerDetected,
		Stunned:        b.world.Stunned,
		DistanceBand:   b.world.DistanceBand,
		LastKnownX:     b.world.LastKnownPos.X,
		LastKnownY:     b.world.LastKnownPos.Y,
		HasLastKnown:   b.world.HasLastKnown,
	}
}

// MovedLastTick reports whether the planner's move actions stepped the
// actor during the most recent Tick.
func (b *Brain) MovedLastTick() bool { return b.movedLastTick }
