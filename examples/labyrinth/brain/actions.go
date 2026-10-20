//go:build example

package brain

import (
	"math"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
)

// I4: yieldOffset shifts a target position laterally (perpendicular to the
// actor→target direction) by 1.5 cells so same-element actors path around
// the player instead of blocking them. The offset direction is deterministic
// (always +Y perpendicular) to avoid oscillation.
func yieldOffset(target, selfPos maze.Vec2) maze.Vec2 {
	dx := target.X - selfPos.X
	dy := target.Y - selfPos.Y
	d := math.Hypot(dx, dy)
	if d < 1e-9 {
		return maze.Vec2{X: target.X + 1.5, Y: target.Y}
	}
	// Perpendicular: (-dy, dx) normalized, scaled by yield distance.
	const yieldDist = 1.5
	px := -dy / d * yieldDist
	py := dx / d * yieldDist
	return maze.Vec2{X: target.X + px, Y: target.Y + py}
}

// flankOffset shifts a target laterally by 1.0 cell for Stalker ambush approaches
// when outside strike distance.
func flankOffset(target, selfPos maze.Vec2) maze.Vec2 {
	dx := target.X - selfPos.X
	dy := target.Y - selfPos.Y
	d := math.Hypot(dx, dy)
	if d < 2.2 {
		return target
	}
	px := dy / d * 1.0
	py := -dx / d * 1.0
	return maze.Vec2{X: target.X + px, Y: target.Y + py}
}

// effect is the planner's effect type: a key and its new value.
type effect struct {
	K VarKey
	V any
}

func (e *effect) Key() any   { return e.K }
func (e *effect) Value() any { return e.V }

// moveSpeed is cells per tick the actor advances along its path.
const moveSpeed = 0.35

// stuckLimit is the number of consecutive ticks a movement action may produce
// zero displacement before it reports bt.Failure to the planner.
const stuckLimit = 30

// moveAction is shared state for an action that moves the actor along an
// A* path. It dynamically replans when its target shifts significantly.
type moveAction struct {
	state      *State
	path       []maze.Vec2 // remaining path (front = next waypoint)
	lastTarget maze.Vec2
	hasTarget  bool
	done       bool
	stuck      int
}

// ensurePath computes or refreshes the walkable A* path toward target (or shifted
// if non-nil and reachable) whenever the target has moved by more than 0.45u
// or the existing path is empty.
func (a *moveAction) ensurePath(target maze.Vec2, shifted *maze.Vec2) bool {
	goal := target
	if shifted != nil {
		goal = *shifted
	}
	targetMoved := !a.hasTarget || math.Hypot(goal.X-a.lastTarget.X, goal.Y-a.lastTarget.Y) > 0.45
	if !targetMoved && len(a.path) > 0 {
		return true
	}
	a.done = false
	a.lastTarget = goal
	a.hasTarget = true
	a.path = nil
	if shifted != nil {
		a.path = a.state.maze.PathWalkable(*a.state.selfPos, *shifted)
	}
	if len(a.path) == 0 {
		a.path = a.state.maze.PathWalkable(*a.state.selfPos, target)
	}
	if len(a.path) == 0 {
		// Same-cell sub-step when already in the target cell.
		if a.state.maze.Pathable(int(math.Floor(target.X)), int(math.Floor(target.Y))) &&
			math.Hypot(target.X-a.state.selfPos.X, target.Y-a.state.selfPos.Y) > 1e-3 {
			a.path = []maze.Vec2{target}
		}
	}
	return len(a.path) > 0
}

// advance moves the actor along the path by the state's per-tick speed.
// It returns bt.Running while the path has more waypoints and bt.Success when
// the current path's endpoint is reached.
func (a *moveAction) advance() (bt.Status, error) {
	if len(a.path) == 0 {
		return bt.Failure, nil
	}
	startX, startY := a.state.selfPos.X, a.state.selfPos.Y
	remaining := moveSpeed
	if a.state.MoveSpeed > 0 {
		remaining = a.state.MoveSpeed
	}
	for remaining > 0 && len(a.path) > 0 {
		target := a.path[0]
		dx := target.X - a.state.selfPos.X
		dy := target.Y - a.state.selfPos.Y
		dist := math.Hypot(dx, dy)
		if dist <= remaining {
			a.state.selfPos.X = target.X
			a.state.selfPos.Y = target.Y
			if dist > 1e-9 {
				a.state.movedThisTick = true
			}
			a.path = a.path[1:]
			remaining -= dist
		} else {
			a.state.selfPos.X += dx / dist * remaining
			a.state.selfPos.Y += dy / dist * remaining
			a.state.movedThisTick = true
			remaining = 0
		}
	}
	if a.state.selfPos.X == startX && a.state.selfPos.Y == startY {
		a.stuck++
		if a.stuck >= stuckLimit {
			a.path = nil
			a.hasTarget = false
			return bt.Failure, nil
		}
	} else {
		a.stuck = 0
	}
	if len(a.path) == 0 {
		a.done = true
		return bt.Success, nil
	}
	return bt.Running, nil
}

// PursueAction moves the actor toward the player's position with archetype-specific tactics.
type PursueAction struct {
	*moveAction
}

func NewPursueAction(s *State) *PursueAction {
	return &PursueAction{moveAction: &moveAction{state: s}}
}

func (a *PursueAction) Conditions() []Conditions {
	// Pursue requires the player to be detected AND the actor not stunned
	// AND the actor not at elemental disadvantage.
	return []Conditions{
		{&BoolCond{K: KeyPlayerDetected, Want: true}, &BoolCond{K: KeyStunned, Want: false}, &BoolCond{K: KeyAtDisadvantage, Want: false}},
	}
}

func (a *PursueAction) Effects() Effects {
	return Effects{
		&effect{K: KeyCaught, V: true},
	}
}

func (a *PursueAction) Node() bt.Node {
	return bt.New(func(children []bt.Node) (bt.Status, error) {
		if !a.state.world.PlayerDetected || a.state.world.Stunned {
			return bt.Failure, nil
		}
		target := a.state.world.PlayerPos
		dBefore := math.Hypot(target.X-a.state.selfPos.X, target.Y-a.state.selfPos.Y)
		if dBefore < 0.6 {
			a.state.world.Caught = true
			return bt.Success, nil
		}

		// Record archetype-specific tactical behavior.
		origSpeed := a.state.MoveSpeed
		defer func() { a.state.MoveSpeed = origSpeed }()

		switch a.state.world.SelfArchetype {
		case 1:
			a.state.world.ArchetypeAction = "SwarmPackRush"
			if a.state.world.HasReinforceTarget {
				base := origSpeed
				if base == 0 {
					base = moveSpeed
				}
				a.state.MoveSpeed = base * 1.20
			}
		case 2:
			hasLOS := HasClearLOS(a.state.maze, *a.state.selfPos, target)
			if hasLOS && dBefore <= 7.0 {
				a.state.world.CastBoltThisTick = true
			}
			if dBefore < 2.8 && hasLOS {
				// Player is too close: kite backward to maintain standoff distance.
				a.state.world.ArchetypeAction = "CasterKiteBack"
				dx := a.state.selfPos.X - target.X
				dy := a.state.selfPos.Y - target.Y
				dist := math.Hypot(dx, dy)
				if dist < 1e-6 {
					dist = 1e-6
				}
				step := moveSpeed
				if a.state.MoveSpeed > 0 {
					step = a.state.MoveSpeed
				}
				baseX, baseY := dx/dist, dy/dist
				best, bestDist2 := maze.Vec2{}, -1.0
				for _, eighth := range []int{0, 1, -1, 2, -2, 3, -3} {
					angle := float64(eighth) * math.Pi / 4
					dirX := baseX*math.Cos(angle) - baseY*math.Sin(angle)
					dirY := baseX*math.Sin(angle) + baseY*math.Cos(angle)
					resolved := a.state.maze.Resolve(maze.Vec2{
						X: a.state.selfPos.X + dirX*step,
						Y: a.state.selfPos.Y + dirY*step,
					}, maze.Radius)
					if resolved.X == a.state.selfPos.X && resolved.Y == a.state.selfPos.Y {
						continue
					}
					gx, gy := resolved.X-target.X, resolved.Y-target.Y
					if d2 := gx*gx + gy*gy; d2 > bestDist2 {
						bestDist2, best = d2, resolved
					}
				}
				if bestDist2 > 0 {
					a.state.selfPos.X = best.X
					a.state.selfPos.Y = best.Y
					a.state.movedThisTick = true
				}
				return bt.Running, nil
			} else if dBefore >= 2.0 && dBefore <= 5.5 && hasLOS {
				// Sweet spot: hold standoff position and cast ranged elemental bolt.
				a.state.world.ArchetypeAction = "CasterRangedBolt"
				a.state.world.CastBoltThisTick = true
				a.state.movedThisTick = true
				return bt.Running, nil
			} else {
				a.state.world.ArchetypeAction = "CasterReposition"
			}
		case 3:
			a.state.world.ArchetypeAction = "StalkerFlank"
		default:
			a.state.world.ArchetypeAction = "BruteCharge"
			if dBefore <= 4.0 && HasClearLOS(a.state.maze, *a.state.selfPos, target) {
				base := origSpeed
				if base == 0 {
					base = moveSpeed
				}
				a.state.MoveSpeed = base * 1.15
			}
		}

		var shifted *maze.Vec2
		if a.state.world.AllyYielding {
			s := yieldOffset(target, *a.state.selfPos)
			shifted = &s
		} else if a.state.world.SelfArchetype == 3 && dBefore > 2.2 {
			s := flankOffset(target, *a.state.selfPos)
			shifted = &s
		}

		if !a.ensurePath(target, shifted) {
			return bt.Failure, nil
		}
		status, err := a.advance()
		if err != nil {
			return bt.Failure, err
		}
		if status == bt.Failure {
			return bt.Failure, nil
		}
		dAfter := math.Hypot(a.state.world.PlayerPos.X-a.state.selfPos.X,
			a.state.world.PlayerPos.Y-a.state.selfPos.Y)
		if dAfter < 0.6 {
			a.state.world.Caught = true
			return bt.Success, nil
		}
		// Keep returning Running while actively pursuing a moving target so the
		// planner never experiences a false Success with Caught==false.
		return bt.Running, nil
	})
}

// SearchAction moves the actor to the last-known position (or patrol waypoint) and searches.
type SearchAction struct {
	*moveAction
}

func NewSearchAction(s *State) *SearchAction {
	return &SearchAction{moveAction: &moveAction{state: s}}
}

func (a *SearchAction) Conditions() []Conditions {
	return []Conditions{
		{&BoolCond{K: KeyStunned, Want: false}},
	}
}

func (a *SearchAction) Effects() Effects {
	return Effects{
		&effect{K: KeySearchExhausted, V: true},
	}
}

func (a *SearchAction) Node() bt.Node {
	return bt.New(func(children []bt.Node) (bt.Status, error) {
		if a.state.world.Stunned || a.state.world.PlayerDetected {
			return bt.Failure, nil
		}
		target := a.state.world.LastKnownPos
		if !a.state.world.HasLastKnown && !a.state.world.HasPatrolTarget {
			a.state.world.SearchExhausted = true
			a.path = nil
			a.hasTarget = false
			return bt.Failure, nil
		}
		a.state.world.ArchetypeAction = "PatrolSearch"
		dBefore := math.Hypot(target.X-a.state.selfPos.X, target.Y-a.state.selfPos.Y)
		if dBefore < 0.45 {
			a.state.world.HasLastKnown = false
			a.state.world.SearchExhausted = true
			a.path = nil
			a.hasTarget = false
			return bt.Success, nil
		}

		var shifted *maze.Vec2
		if a.state.world.AllyYielding {
			s := yieldOffset(target, *a.state.selfPos)
			shifted = &s
		}
		if !a.ensurePath(target, shifted) {
			a.state.world.HasLastKnown = false
			a.state.world.SearchExhausted = true
			a.path = nil
			a.hasTarget = false
			return bt.Failure, nil
		}
		status, err := a.advance()
		if err != nil {
			return bt.Failure, err
		}
		dAfter := math.Hypot(target.X-a.state.selfPos.X, target.Y-a.state.selfPos.Y)
		if status == bt.Success || dAfter < 0.45 {
			a.state.world.HasLastKnown = false
			a.state.world.SearchExhausted = true
			a.path = nil
			a.hasTarget = false
			return bt.Success, nil
		}
		return status, nil
	})
}

// RecoverAction is the actor recovering from a stun.
type RecoverAction struct {
	state     *State
	ticksLeft int
}

func NewRecoverAction(s *State) *RecoverAction {
	return &RecoverAction{state: s, ticksLeft: 3}
}

func (a *RecoverAction) Conditions() []Conditions {
	return []Conditions{
		{&BoolCond{K: KeyStunned, Want: true}},
	}
}

func (a *RecoverAction) Effects() Effects {
	return Effects{
		&effect{K: KeyStunned, V: false},
	}
}

func (a *RecoverAction) Node() bt.Node {
	return bt.New(func(children []bt.Node) (bt.Status, error) {
		a.state.world.ArchetypeAction = "RecoverStun"
		if a.state.world.StunTicks > 0 {
			a.state.world.StunTicks--
			if a.state.world.StunTicks <= 0 {
				a.state.world.Stunned = false
				return bt.Success, nil
			}
			return bt.Running, nil
		}
		if a.ticksLeft <= 0 {
			a.ticksLeft = 3
		}
		a.ticksLeft--
		if a.ticksLeft <= 0 {
			a.state.world.Stunned = false
			a.ticksLeft = 3
			return bt.Success, nil
		}
		return bt.Running, nil
	})
}

// RetreatAction moves the actor away from the player when at elemental
// disadvantage. This is the core PA-BT elemental planning behavior: a fire
// mob facing an ice player retreats to avoid the disadvantage.
type RetreatAction struct {
	*moveAction
}

func NewRetreatAction(s *State) *RetreatAction {
	return &RetreatAction{moveAction: &moveAction{state: s}}
}

func (a *RetreatAction) Conditions() []Conditions {
	// Retreat requires: at disadvantage AND player detected AND not stunned.
	return []Conditions{
		{&BoolCond{K: KeyAtDisadvantage, Want: true}, &BoolCond{K: KeyPlayerDetected, Want: true}, &BoolCond{K: KeyStunned, Want: false}},
	}
}

func (a *RetreatAction) Effects() Effects {
	return Effects{
		&effect{K: KeyAtDisadvantage, V: false},
	}
}

func (a *RetreatAction) Node() bt.Node {
	return bt.New(func(children []bt.Node) (bt.Status, error) {
		if !a.state.world.PlayerDetected || a.state.world.Stunned {
			return bt.Failure, nil
		}
		a.state.world.ArchetypeAction = "TacticalRetreat"
		playerPos := a.state.world.PlayerPos
		dx := a.state.selfPos.X - playerPos.X
		dy := a.state.selfPos.Y - playerPos.Y
		dist2 := dx*dx + dy*dy
		// Safe distance threshold is 5.0u (inside the 8.0u sight radius so Retreat
		// succeeds cleanly before PlayerDetected drops).
		if dist2 >= 5.0*5.0 {
			a.state.world.AtDisadvantage = false
			a.state.world.DisadvantageResolvedTicks = 60
			return bt.Success, nil
		}
		step := moveSpeed
		if a.state.MoveSpeed > 0 {
			step = a.state.MoveSpeed
		}
		dist := math.Sqrt(dist2)
		if dist < 1e-6 {
			dist = 1e-6
		}
		baseX, baseY := dx/dist, dy/dist
		best, bestDist2 := maze.Vec2{}, -1.0
		for _, eighth := range []int{0, 1, -1, 2, -2, 3, -3, 4} {
			angle := float64(eighth) * math.Pi / 4
			dirX := baseX*math.Cos(angle) - baseY*math.Sin(angle)
			dirY := baseX*math.Sin(angle) + baseY*math.Cos(angle)
			resolved := a.state.maze.Resolve(maze.Vec2{
				X: a.state.selfPos.X + dirX*step,
				Y: a.state.selfPos.Y + dirY*step,
			}, maze.Radius)
			if resolved.X == a.state.selfPos.X && resolved.Y == a.state.selfPos.Y {
				continue // heading fully blocked from here
			}
			gx, gy := resolved.X-playerPos.X, resolved.Y-playerPos.Y
			if d2 := gx*gx + gy*gy; d2 > bestDist2 {
				bestDist2, best = d2, resolved
			}
		}
		if bestDist2 >= 0 {
			a.state.selfPos.X = best.X
			a.state.selfPos.Y = best.Y
			a.state.movedThisTick = true
			a.stuck = 0
			if bestDist2 >= 5.0*5.0 {
				a.state.world.AtDisadvantage = false
				a.state.world.DisadvantageResolvedTicks = 60
				return bt.Success, nil
			}
		} else {
			a.stuck++
			if a.stuck >= stuckLimit {
				return bt.Failure, nil
			}
		}
		return bt.Running, nil
	})
}

// ReinforceAction seeks a same-element ally when at elemental disadvantage.
type ReinforceAction struct {
	*moveAction
}

func NewReinforceAction(s *State) *ReinforceAction {
	return &ReinforceAction{moveAction: &moveAction{state: s}}
}

func (a *ReinforceAction) Conditions() []Conditions {
	return []Conditions{
		{&BoolCond{K: KeyAtDisadvantage, Want: true}, &BoolCond{K: KeyStunned, Want: false}},
	}
}

func (a *ReinforceAction) Effects() Effects {
	return Effects{
		&effect{K: KeyAtDisadvantage, V: false},
	}
}

func (a *ReinforceAction) Node() bt.Node {
	return bt.New(func(children []bt.Node) (bt.Status, error) {
		if a.state.world.Stunned || !a.state.world.HasReinforceTarget {
			return bt.Failure, nil
		}
		a.state.world.ArchetypeAction = "RallyReinforce"
		target := a.state.world.ReinforceTarget
		if math.Hypot(target.X-a.state.selfPos.X, target.Y-a.state.selfPos.Y) < 0.65 {
			a.state.world.AtDisadvantage = false
			a.state.world.DisadvantageResolvedTicks = 60
			return bt.Success, nil
		}
		if !a.ensurePath(target, nil) {
			return bt.Failure, nil
		}
		status, err := a.advance()
		if err != nil {
			return bt.Failure, nil
		}
		if status == bt.Running {
			return bt.Running, nil
		}
		a.state.world.AtDisadvantage = false
		a.state.world.DisadvantageResolvedTicks = 60
		return bt.Success, nil
	})
}
