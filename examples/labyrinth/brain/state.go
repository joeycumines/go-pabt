//go:build example

package brain

import (
	"fmt"

	"github.com/joeycumines/go-pabt"
	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
)

// WorldView is the actor's model of the player and itself. It is the single
// source of truth the planner reads through the state view. The brain updates
// it each tick from the world snapshot; state injection (S5) mutates it
// directly to redirect the hunter.
type WorldView struct {
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

	// Stunned is true while the actor is recovering from a pulse.
	Stunned bool

	// SearchExhausted is true after searching last-known yielded nothing.
	SearchExhausted bool

	// Caught is true when the actor caught the player.
	Caught bool

	// PlayerElement is the player's active element (0=fire 1=ice 2=lightning 3=void).
	PlayerElement int

	// SelfElement is this actor's innate element.
	SelfElement int

	// ReinforceTarget is the position of the nearest friendly same-element
	// ally, set by UpdateWorldView when one exists within sight range.
	// Zero-value means no ally found.
	ReinforceTarget    maze.Vec2
	HasReinforceTarget bool

	// AtDisadvantage is true when the player's element beats ours in the cycle.
	AtDisadvantage bool

	// AtAdvantage is true when our element beats the player's in the cycle.
	AtAdvantage bool
}

// State is the pabt.State implementation over the world view. It reads variables
// from the world view and generates actions for failed conditions.
type State struct {
	world *WorldView
	maze  *maze.Maze
	// selfPos is a pointer to the actor's live position so actions can
	// move it.
	selfPos *maze.Vec2
	// MoveSpeed is the per-tick movement distance for this actor's move
	// actions. Zero falls back to the default moveSpeed, which keeps the
	// brain's standalone tests unchanged.
	MoveSpeed float64

	// movedThisTick is set by move actions that actually step selfPos
	// during a planner tick. The brain snapshots and clears it each Tick
	// so strategy movement can yield exactly when the brain owns movement.
	movedThisTick bool
}

// NewState builds a State over the given world view, maze, and a pointer to the
// actor's live position.
func NewState(w *WorldView, m *maze.Maze, selfPos *maze.Vec2) *State {
	return &State{world: w, maze: m, selfPos: selfPos}
}

// Variable implements pabt.State. It returns the current value for key.
func (s *State) Variable(key any) (any, error) {
	k, ok := key.(VarKey)
	if !ok {
		return nil, fmt.Errorf("brain: unknown key type %T", key)
	}
	switch k {
	case KeyPlayerDetected:
		return s.world.PlayerDetected, nil
	case KeyPlayerPos:
		return s.world.PlayerPos, nil
	case KeyLastKnownPos:
		return s.world.LastKnownPos, nil
	case KeyDistanceBand:
		return s.world.DistanceBand, nil
	case KeyStunned:
		return s.world.Stunned, nil
	case KeySearchExhausted:
		return s.world.SearchExhausted, nil
	case KeyCaught:
		return s.world.Caught, nil
	case KeyPlayerElement:
		return s.world.PlayerElement, nil
	case KeySelfElement:
		return s.world.SelfElement, nil
	case KeyAtDisadvantage:
		return s.world.AtDisadvantage, nil
	case KeyAtAdvantage:
		return s.world.AtAdvantage, nil
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
	case KeyAtDisadvantage:
		// To resolve elemental disadvantage, retreat or reinforce.
		return []pabt.Action[Condition]{
			NewRetreatAction(s),
			NewReinforceAction(s),
		}, nil
	default:
		return nil, nil
	}
}
