//go:build example

package brain

import "github.com/joeycumines/go-pabt"

// VarKey is a comparable state variable key for the minotaur planner.
type VarKey int

const (
	// KeyPlayerDetected is true when the minotaur currently has line of sight
	// to the player.
	KeyPlayerDetected VarKey = iota

	// KeyPlayerPos is the player's current world position (maze.Vec2). It is
	// only meaningful when KeyPlayerDetected is true.
	KeyPlayerPos

	// KeyLastKnownPos is the last world position where the player was seen.
	KeyLastKnownPos

	// KeyDistanceBand is an int: 0=adjacent, 1=near, 2=far, 3=unknown.
	KeyDistanceBand

	// KeyStunned is true when the minotaur is stunned by a pulse.
	KeyStunned

	// KeySearchExhausted is true when the minotaur has searched its
	// last-known-position neighborhood and found nothing.
	KeySearchExhausted

	// KeyCaught is true when the minotaur has caught the player.
	KeyCaught
)

// String returns a human-readable name for a VarKey.
func (k VarKey) String() string {
	switch k {
	case KeyPlayerDetected:
		return "playerDetected"
	case KeyPlayerPos:
		return "playerPos"
	case KeyLastKnownPos:
		return "lastKnownPos"
	case KeyDistanceBand:
		return "distanceBand"
	case KeyStunned:
		return "stunned"
	case KeySearchExhausted:
		return "searchExhausted"
	case KeyCaught:
		return "caught"
	default:
		return "unknown"
	}
}

// Condition is the planner's condition type. It is an alias for the library's
// Condition interface so the plan can be built over it.
type Condition = pabt.Condition

// Conditions is an AND group of conditions.
type Conditions = pabt.Conditions[Condition]

// Effects is the library's effect list type.
type Effects = pabt.Effects

// BoolCond is a Condition that requires a bool variable to equal Want. It
// implements pabt.Condition and pabt.Expecting.
type BoolCond struct {
	K    VarKey
	Want bool
}

func (c *BoolCond) Key() any      { return c.K }
func (c *BoolCond) Expected() any { return c.Want }
func (c *BoolCond) Match(v any) bool {
	b, ok := v.(bool)
	return ok && b == c.Want
}

// IntCond is a Condition that requires an int variable to equal Want.
type IntCond struct {
	K    VarKey
	Want int
}

func (c *IntCond) Key() any      { return c.K }
func (c *IntCond) Expected() any { return c.Want }
func (c *IntCond) Match(v any) bool {
	i, ok := v.(int)
	return ok && i == c.Want
}
