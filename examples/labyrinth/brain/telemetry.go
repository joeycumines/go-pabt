//go:build example

package brain

import (
	"math"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
)

// TickTelemetry captures causal PA-BT diagnostics for a single Brain.Tick call.
type TickTelemetry struct {
	ActorID               string    `json:"actorId"`
	PlanRevision          uint64    `json:"planRevision"`
	PrePos                maze.Vec2 `json:"prePos"`
	PostPos               maze.Vec2 `json:"postPos"`
	Displacement          float64   `json:"displacement"`
	MovedByBrain          bool      `json:"movedByBrain"`
	ActiveAction          string    `json:"activeAction"`
	ArchetypeAction       string    `json:"archetypeAction,omitempty"`
	CastBolt              bool      `json:"castBolt,omitempty"`
	ActionStatus          int       `json:"actionStatus"`
	GoalShortCircuited    bool      `json:"goalShortCircuited"`
	PreservesCount        int       `json:"preservesCount"`
	BreaksCount           int       `json:"breaksCount"`
	UnexpandedCount       int       `json:"unexpandedCount"`
	PlayerDetected        bool      `json:"playerDetected"`
	PlayerDist            float64   `json:"playerDist"`
	HasLineOfSight        bool      `json:"hasLineOfSight"`
	AtDisadvantage        bool      `json:"atDisadvantage"`
	AtAdvantage           bool      `json:"atAdvantage"`
	SearchExhausted       bool      `json:"searchExhausted"`
	Caught                bool      `json:"caught"`
	Stunned               bool      `json:"stunned"`
	HasLastKnown          bool      `json:"hasLastKnown"`
	LastKnownPos          maze.Vec2 `json:"lastKnownPos"`
	LastKnownWalkable     bool      `json:"lastKnownWalkable"`
	ZeroMoveActionSuccess bool      `json:"zeroMoveActionSuccess"`
}

// HasClearLOS checks whether the straight segment between a and b in m crosses
// only walkable cells (CellFloor or CellPlatform), matching server rayOccluded.
func HasClearLOS(m *maze.Maze, a, b maze.Vec2) bool {
	if m == nil {
		return false
	}
	dx := b.X - a.X
	dy := b.Y - a.Y
	dist := math.Hypot(dx, dy)
	if dist < 1e-6 {
		cx, cy := int(math.Floor(a.X)), int(math.Floor(a.Y))
		return m.At(cx, cy).Walkable()
	}
	steps := int(math.Ceil(dist / 0.2))
	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps)
		x := a.X + dx*t
		y := a.Y + dy*t
		if !m.At(int(math.Floor(x)), int(math.Floor(y))).Walkable() {
			return false
		}
	}
	return true
}

// auditPlannerWiring verifies that for every boolean condition key, any candidate
// action returned by State.Actions(cond) declares an Effect matching cond.Key().
func auditPlannerWiring(s *State) int {
	if s == nil {
		return 0
	}
	keys := []VarKey{
		KeyPlayerDetected,
		KeyStunned,
		KeySearchExhausted,
		KeyCaught,
		KeyAtDisadvantage,
		KeyAtAdvantage,
	}
	mismatches := 0
	for _, k := range keys {
		cond := &BoolCond{K: k, Want: true}
		acts, err := s.Actions(cond)
		if err != nil {
			mismatches++
			continue
		}
		for _, act := range acts {
			matched := false
			for _, eff := range act.Effects() {
				if eff.Key() == k {
					matched = true
					break
				}
			}
			if !matched {
				mismatches++
			}
		}
	}
	return mismatches
}

// buildTickTelemetry constructs TickTelemetry from pre/post positions and the encoded BTFrame.
func (b *Brain) buildTickTelemetry(prePos, postPos maze.Vec2, moved bool, frame *BTFrame) TickTelemetry {
	disp := math.Hypot(postPos.X-prePos.X, postPos.Y-prePos.Y)
	lkCellX := int(math.Floor(b.world.LastKnownPos.X))
	lkCellY := int(math.Floor(b.world.LastKnownPos.Y))
	lkWalkable := b.maze != nil && b.maze.At(lkCellX, lkCellY).Walkable()

	playerDist := math.Hypot(b.world.PlayerPos.X-postPos.X, b.world.PlayerPos.Y-postPos.Y)
	hasLOS := false
	if b.world.PlayerDetected {
		hasLOS = b.maze == nil || HasClearLOS(b.maze, prePos, b.world.PlayerPos) || HasClearLOS(b.maze, postPos, b.world.PlayerPos)
	}

	t := TickTelemetry{
		ActorID:           b.ID,
		PrePos:            prePos,
		PostPos:           postPos,
		Displacement:      disp,
		MovedByBrain:      moved,
		ArchetypeAction:   b.world.ArchetypeAction,
		CastBolt:          b.world.CastBoltThisTick,
		PlayerDetected:    b.world.PlayerDetected,
		PlayerDist:        playerDist,
		HasLineOfSight:    hasLOS,
		AtDisadvantage:    b.world.AtDisadvantage,
		AtAdvantage:       b.world.AtAdvantage,
		SearchExhausted:   b.world.SearchExhausted,
		Caught:            b.world.Caught,
		Stunned:           b.world.Stunned,
		HasLastKnown:      b.world.HasLastKnown,
		LastKnownPos:      b.world.LastKnownPos,
		LastKnownWalkable: lkWalkable,
		UnexpandedCount:   auditPlannerWiring(b.state),
	}
	if frame == nil {
		return t
	}
	t.PlanRevision = frame.PlanRevision

	// Map ActionID -> semantic name from CandidatesWithCost / Actions.
	actionNames := make(map[string]string, len(frame.Actions))
	for i, a := range frame.Actions {
		if i < len(frame.CandidatesWithCost) && frame.CandidatesWithCost[i].ActionName != "" {
			actionNames[a.ID] = frame.CandidatesWithCost[i].ActionName
		} else {
			actionNames[a.ID] = a.Name
		}
	}

	if len(frame.Executions) == 0 {
		if b.world.Caught || b.world.SearchExhausted {
			t.GoalShortCircuited = true
		}
		return t
	}

	lastExec := frame.Executions[len(frame.Executions)-1]
	t.ActiveAction = actionNames[lastExec.ActionID]
	if t.ActiveAction == "" {
		t.ActiveAction = lastExec.ActionID
	}
	t.ActionStatus = lastExec.Status

	for _, ex := range frame.Executions {
		for _, rel := range ex.Relations {
			switch rel.Result {
			case "preserves":
				t.PreservesCount++
			case "breaks":
				t.BreaksCount++
			}
		}
	}

	// Detect movement actions returning Success with 0 displacement while far from their target.
	if t.ActionStatus == int(bt.Success) && disp < 1e-6 {
		switch t.ActiveAction {
		case "Pursue":
			if playerDist > 0.65 {
				t.ZeroMoveActionSuccess = true
			}
		case "Search":
			lkDist := math.Hypot(b.world.LastKnownPos.X-postPos.X, b.world.LastKnownPos.Y-postPos.Y)
			if lkDist > 0.65 {
				t.ZeroMoveActionSuccess = true
			}
		case "Retreat":
			if playerDist < 4.9 {
				t.ZeroMoveActionSuccess = true
			}
		case "Reinforce":
			if b.world.HasReinforceTarget {
				rfDist := math.Hypot(b.world.ReinforceTarget.X-postPos.X, b.world.ReinforceTarget.Y-postPos.Y)
				if rfDist > 0.70 {
					t.ZeroMoveActionSuccess = true
				}
			}
		}
	}

	return t
}

// LastTelemetry returns the most recent TickTelemetry recorded by Tick.
func (b *Brain) LastTelemetry() TickTelemetry {
	return b.lastTelemetry
}
