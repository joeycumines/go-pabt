//go:build example

package brain

import (
	"math"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
)

// effect is the planner's effect type: a key and its new value.
type effect struct {
	K VarKey
	V any
}

func (e *effect) Key() any   { return e.K }
func (e *effect) Value() any { return e.V }

// moveSpeed is cells per tick the actor advances along its path.
const moveSpeed = 0.35

// moveAction is shared state for an action that moves the actor along an
// A* path. It is stored on the action so that successive ticks of the same
// action node continue advancing the same path.
type moveAction struct {
	state *State
	path  []maze.Vec2 // remaining path (front = next waypoint)
	done  bool
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
			a.state.movedThisTick = true
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
			a.path = a.state.maze.Path(*a.state.selfPos, a.state.world.LastKnownPos)
			if a.state.world.PlayerDetected {
				a.path = a.state.maze.Path(*a.state.selfPos, a.state.world.PlayerPos)
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
			a.path = a.state.maze.Path(*a.state.selfPos, a.state.world.LastKnownPos)
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
		// Retreat: move one step in the direction away from the player.
		step := moveSpeed
		if a.state.MoveSpeed > 0 {
			step = a.state.MoveSpeed
		}
		dist := math.Sqrt(dist2)
		if dist < 1e-6 {
			dist = 1e-6
		}
		retreatTarget := maze.Vec2{
			X: a.state.selfPos.X + dx/dist*step,
			Y: a.state.selfPos.Y + dy/dist*step,
		}
		resolved := a.state.maze.Resolve(retreatTarget, maze.Radius)
		if resolved.X != a.state.selfPos.X || resolved.Y != a.state.selfPos.Y {
			a.state.selfPos.X = resolved.X
			a.state.selfPos.Y = resolved.Y
			a.state.movedThisTick = true
		}
		return bt.Running, nil
	})
}

// ReinforceAction seeks a same-element ally when at elemental disadvantage.
// This is the second PA-BT elemental planning behavior: instead of fleeing,
// the actor groups up with allies to overcome the disadvantage.
type ReinforceAction struct {
	state *State
}

func NewReinforceAction(s *State) *ReinforceAction {
	return &ReinforceAction{state: s}
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
		// For now, reinforce is a no-op that succeeds after a delay.
		// The actual ally-seeking behavior requires access to other actors'
		// positions, which the brain doesn't have. The game layer handles
		// faction grouping via strategy movement.
		return bt.Success, nil
	})
}
