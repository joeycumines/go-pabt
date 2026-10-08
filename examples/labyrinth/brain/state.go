//go:build example

package brain

import (
	"fmt"

	"github.com/joeycumines/go-pabt"
	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
)

// Belief is the minotaur's model of the player and itself. It is the single
// source of truth the planner reads through the state view. The brain updates
// it each tick from the world snapshot; belief injection (T8) mutates it
// directly to redirect the hunter.
type Belief struct {
	// PlayerDetected is true when line of sight to the player is held.
	PlayerDetected bool

	// PlayerPos is the player's position, meaningful when PlayerDetected.
	PlayerPos maze.Vec2

	// LastKnownPos is the last position where the player was seen.
	LastKnownPos maze.Vec2

	// HasLastKnown is true once the player has been seen at least once.
	HasLastKnown bool

	// DistanceBand: 0=adjacent, 1=near, 2=far, 3=unknown.
	DistanceBand int

	// Stunned is true while the minotaur is recovering from a pulse.
	Stunned bool

	// SearchExhausted is true after searching last-known yielded nothing.
	SearchExhausted bool

	// Caught is true when the minotaur caught the player.
	Caught bool
}

// State is the pabt.State implementation over the belief. It reads variables
// from the belief and generates actions for failed conditions.
type State struct {
	belief *Belief
	maze   *maze.Maze
	// selfPos is a pointer to the minotaur's live position so actions can
	// move it.
	selfPos *maze.Vec2
	// MoveSpeed is the per-tick movement distance for this actor's move
	// actions. Zero falls back to the default moveSpeed, which keeps the
	// brain's standalone tests unchanged.
	MoveSpeed float64
}

// NewState builds a State over the given belief, maze, and a pointer to the
// minotaur's live position.
func NewState(b *Belief, m *maze.Maze, selfPos *maze.Vec2) *State {
	return &State{belief: b, maze: m, selfPos: selfPos}
}

// Variable implements pabt.State. It returns the current value for key.
func (s *State) Variable(key any) (any, error) {
	k, ok := key.(VarKey)
	if !ok {
		return nil, fmt.Errorf("brain: unknown key type %T", key)
	}
	switch k {
	case KeyPlayerDetected:
		return s.belief.PlayerDetected, nil
	case KeyPlayerPos:
		return s.belief.PlayerPos, nil
	case KeyLastKnownPos:
		return s.belief.LastKnownPos, nil
	case KeyDistanceBand:
		return s.belief.DistanceBand, nil
	case KeyStunned:
		return s.belief.Stunned, nil
	case KeySearchExhausted:
		return s.belief.SearchExhausted, nil
	case KeyCaught:
		return s.belief.Caught, nil
	default:
		return nil, fmt.Errorf("brain: unknown VarKey %v", k)
	}
}

// Actions implements pabt.State. It returns the actions that can achieve the
// given failed condition.
func (s *State) Actions(failed Condition) ([]pabt.Action[Condition], error) {
	k, ok := failed.Key().(VarKey)
	if !ok {
		return nil, fmt.Errorf("brain: failed condition key type %T", failed.Key())
	}
	switch k {
	case KeyCaught:
		// To catch the player we must be adjacent and not stunned.
		return []pabt.Action[Condition]{
			NewPursueAction(s),
		}, nil
	case KeyPlayerDetected:
		// To detect the player we search the last-known position.
		return []pabt.Action[Condition]{
			NewSearchAction(s),
		}, nil
	case KeyStunned:
		// To un-stun we recover.
		return []pabt.Action[Condition]{
			NewRecoverAction(s),
		}, nil
	case KeySearchExhausted:
		// To exhaust the search we complete searching.
		return []pabt.Action[Condition]{
			NewSearchAction(s),
		}, nil
	default:
		return nil, nil
	}
}
