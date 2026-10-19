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
// zero displacement before it reports bt.Failure to the planner. At 30 Hz
// this is one second of being wall-wedged or otherwise stuck — long enough to
// absorb momentary gridlocks, short enough to prevent an actor from freezing
// in place for the rest of the round.
const stuckLimit = 30

// moveAction is shared state for an action that moves the actor along an
// A* path. It is stored on the action so that successive ticks of the same
// action node continue advancing the same path.
type moveAction struct {
	state *State
	path  []maze.Vec2 // remaining path (front = next waypoint)
	done  bool
	// stuck counts consecutive ticks where the action produced no displacement
	// (advance consumed nothing, or every retreat heading was blocked);
	// reaching stuckLimit fails the action so the planner re-instances it.
	stuck int
}

// advance moves the actor along the path by the state's per-tick speed.
// It returns bt.Running while the path has more waypoints and bt.Success when
// the endpoint is reached.
func (a *moveAction) advance() (bt.Status, error) {
	if a.done {
		return bt.Success, nil
	}
	if len(a.path) == 0 {
		a.done = true
		return bt.Success, nil
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
			// A zero-distance waypoint (the path's start cell equals the
			// actor's position) must not claim movement: it would make the
			// strategy layer yield to a planner that stood still.
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
	if len(a.path) == 0 {
		a.done = true
		return bt.Success, nil
	}
	if a.state.selfPos.X == startX && a.state.selfPos.Y == startY {
		a.stuck++
		if a.stuck >= stuckLimit {
			return bt.Failure, nil
		}
	} else {
		a.stuck = 0
	}
	return bt.Running, nil
}

// PursueAction moves the actor toward the player's position.
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
		// Compute the path to the player's position on first tick.
		if a.path == nil && !a.done {
			target := a.state.world.LastKnownPos
			if a.state.world.PlayerDetected {
				target = a.state.world.PlayerPos
			}
			// I4: allied yielding — offset target laterally so same-element
			// actors step aside instead of blocking the player's path.
			if a.state.world.AllyYielding {
				shifted := yieldOffset(target, *a.state.selfPos)
				a.path = a.state.maze.Path(*a.state.selfPos, shifted)
			}
			// Fall back to the unshifted target if the offset path is nil
			// (wall blocks the offset) or if yielding is off.
			if a.path == nil {
				a.path = a.state.maze.Path(*a.state.selfPos, target)
			}
			if a.path == nil {
				return bt.Failure, nil
			}
		}
		status, err := a.advance()
		if err != nil {
			return bt.Failure, err
		}
		if status == bt.Success {
			// On arrival, check if we caught the player.
			d := math.Hypot(a.state.world.PlayerPos.X-a.state.selfPos.X,
				a.state.world.PlayerPos.Y-a.state.selfPos.Y)
			if a.state.world.PlayerDetected && d < 0.6 {
				a.state.world.Caught = true
			}
		}
		return status, nil
	})
}

// SearchAction moves the actor to the last-known position and searches.
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
		if a.path == nil && !a.done {
			target := a.state.world.LastKnownPos
			// I4: allied yielding — offset search target laterally.
			if a.state.world.AllyYielding {
				shifted := yieldOffset(target, *a.state.selfPos)
				a.path = a.state.maze.Path(*a.state.selfPos, shifted)
			}
			// Fall back to the unshifted target if the offset path is nil.
			if a.path == nil {
				a.path = a.state.maze.Path(*a.state.selfPos, target)
			}
			if a.path == nil {
				// Cannot reach last-known: the search is exhausted.
				a.done = true
				a.state.world.SearchExhausted = true
				return bt.Success, nil
			}
		}
		status, err := a.advance()
		if err != nil {
			return bt.Failure, err
		}
		if status == bt.Success {
			// Arrived at last-known and the player is not here (they would
			// have been re-detected by UpdateWorldView otherwise): the search
			// is exhausted.
			a.state.world.SearchExhausted = true
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
		a.ticksLeft--
		if a.ticksLeft <= 0 {
			a.state.world.Stunned = false
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
		// Move away from the player.
		playerPos := a.state.world.PlayerPos
		dx := a.state.selfPos.X - playerPos.X
		dy := a.state.selfPos.Y - playerPos.Y
		dist2 := dx*dx + dy*dy
		if dist2 > 8.0*8.0 {
			// Far enough - disadvantage resolved by distance.
			a.state.world.AtDisadvantage = false
			return bt.Success, nil
		}
		// Retreat: step away from the player. If the direct away heading is
		// blocked, fan out through alternate headings and take the passable
		// one that gains the most distance from the player.
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
		} else {
			// Wall-wedged in every heading: fail so the planner re-instances.
			a.stuck++
			if a.stuck >= stuckLimit {
				return bt.Failure, nil
			}
		}
		return bt.Running, nil
	})
}

// ReinforceAction seeks a same-element ally when at elemental disadvantage.
// This is the second PA-BT elemental planning behavior: instead of fleeing,
// the actor groups up with allies to overcome the disadvantage.
type ReinforceAction struct {
	*moveAction
}

func NewReinforceAction(s *State) *ReinforceAction {
	return &ReinforceAction{moveAction: &moveAction{state: s}}
}

func (a *ReinforceAction) Conditions() []Conditions {
	// Reinforce requires: at disadvantage AND not stunned.
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
		if a.state.world.Stunned {
			return bt.Failure, nil
		}
		// If we have a reinforce target (nearest friendly ally), move
		// toward it. This makes a disadvantaged actor visibly fall back
		// to reinforce rather than idling. Uses maze.Path so walls are
		// respected, matching PursueAction/SearchAction.
		if a.state.world.HasReinforceTarget {
			// Compute path once (or when stale), then follow it across
			// ticks. Recomputing every tick causes the actor to oscillate
			// between cell centers without ever advancing.
			if a.path == nil && !a.done {
				a.path = a.state.maze.Path(*a.state.selfPos, a.state.world.ReinforceTarget)
			}
			status, err := a.advance()
			if err != nil {
				return bt.Failure, nil
			}
			if status == bt.Running {
				return bt.Running, nil
			}
			// Reached the ally: mark reinforce satisfied.
			return bt.Success, nil
		}
		// No ally nearby: succeed without moving (matches old behavior).
		return bt.Success, nil
	})
}
