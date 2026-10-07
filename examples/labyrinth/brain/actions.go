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

// moveSpeed is cells per tick the minotaur advances along its path.
const moveSpeed = 0.35

// moveAction is shared state for an action that moves the minotaur along an
// A* path. It is stored on the action so that successive ticks of the same
// action node continue advancing the same path.
type moveAction struct {
	state *State
	path  []maze.Vec2 // remaining path (front = next waypoint)
	done  bool
}

// advance moves the minotaur along the path by moveSpeed cells. It returns
// bt.Running while the path has more waypoints and bt.Success when the
// endpoint is reached.
func (a *moveAction) advance() (bt.Status, error) {
	if a.done {
		return bt.Success, nil
	}
	if len(a.path) == 0 {
		a.done = true
		return bt.Success, nil
	}
	remaining := moveSpeed
	for remaining > 0 && len(a.path) > 0 {
		target := a.path[0]
		dx := target.X - a.state.selfPos.X
		dy := target.Y - a.state.selfPos.Y
		dist := math.Hypot(dx, dy)
		if dist <= remaining {
			a.state.selfPos.X = target.X
			a.state.selfPos.Y = target.Y
			a.path = a.path[1:]
			remaining -= dist
		} else {
			a.state.selfPos.X += dx / dist * remaining
			a.state.selfPos.Y += dy / dist * remaining
			remaining = 0
		}
	}
	if len(a.path) == 0 {
		a.done = true
		return bt.Success, nil
	}
	return bt.Running, nil
}

// PursueAction moves the minotaur toward the player's believed position.
type PursueAction struct {
	*moveAction
}

func NewPursueAction(s *State) *PursueAction {
	return &PursueAction{moveAction: &moveAction{state: s}}
}

func (a *PursueAction) Conditions() []Conditions {
	// Pursue requires the player to be detected AND the minotaur not stunned.
	// Both conditions are in one AND group (a single Conditions slice).
	return []Conditions{
		{&BoolCond{K: KeyPlayerDetected, Want: true}, &BoolCond{K: KeyStunned, Want: false}},
	}
}

func (a *PursueAction) Effects() Effects {
	return Effects{
		&effect{K: KeyCaught, V: true},
	}
}

func (a *PursueAction) Node() bt.Node {
	return bt.New(func(children []bt.Node) (bt.Status, error) {
		// Compute the path to the player's believed position on first tick.
		if a.path == nil && !a.done {
			a.path = a.state.maze.Path(*a.state.selfPos, a.state.belief.LastKnownPos)
			if a.state.belief.PlayerDetected {
				a.path = a.state.maze.Path(*a.state.selfPos, a.state.belief.PlayerPos)
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
			d := math.Hypot(a.state.belief.PlayerPos.X-a.state.selfPos.X,
				a.state.belief.PlayerPos.Y-a.state.selfPos.Y)
			if a.state.belief.PlayerDetected && d < 0.6 {
				a.state.belief.Caught = true
			}
		}
		return status, nil
	})
}

// SearchAction moves the minotaur to the last-known position and searches.
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
			a.path = a.state.maze.Path(*a.state.selfPos, a.state.belief.LastKnownPos)
			if a.path == nil {
				// Cannot reach last-known: the search is exhausted.
				a.done = true
				a.state.belief.SearchExhausted = true
				return bt.Success, nil
			}
		}
		status, err := a.advance()
		if err != nil {
			return bt.Failure, err
		}
		if status == bt.Success {
			// Arrived at last-known and the player is not here (they would
			// have been re-detected by UpdateBelief otherwise): the search is
			// exhausted.
			a.state.belief.SearchExhausted = true
		}
		return status, nil
	})
}

// RecoverAction is the minotaur recovering from a stun.
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
			a.state.belief.Stunned = false
			return bt.Success, nil
		}
		return bt.Running, nil
	})
}
