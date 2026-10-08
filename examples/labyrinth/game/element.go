//go:build example

package game

// Element is one of the four elemental types in the circular advantage
// cycle: Fire > Ice > Lightning > Void > Fire.
type Element int

const (
	ElementFire      Element = iota // 0
	ElementIce                      // 1
	ElementLightning                // 2
	ElementVoid                     // 3
)

// elementCount is the number of elements.
const elementCount = 4

// advantage returns the damage multiplier when attacker hits defender.
// Advantage (attacker beats defender in the cycle) = 2x.
// Disadvantage (defender beats attacker) = 0.5x.
// Neutral = 1x.
func advantage(attacker, defender Element) float64 {
	// Fire > Ice > Lightning > Void > Fire
	// attacker beats defender if (attacker + 1) % 4 == defender
	if (attacker+1)%elementCount == defender {
		return 2.0
	}
	// defender beats attacker if (defender + 1) % 4 == attacker
	if (defender+1)%elementCount == attacker {
		return 0.5
	}
	return 1.0
}

// actorElement returns the innate element for each actor archetype.
// This maps actor types to factions:
//   - Brute (0) -> Fire (Ember Legion)
//   - Swarm (1) -> Ice (Frost Covenant)
//   - Caster (2) -> Lightning (Storm Pact)
//   - Stalker (3) -> Void (Void Cult)
func actorElement(t ActorType) Element {
	switch t {
	case ActorBrute:
		return ElementFire
	case ActorSwarm:
		return ElementIce
	case ActorCaster:
		return ElementLightning
	case ActorStalker:
		return ElementVoid
	default:
		return ElementFire
	}
}

// elementName returns a human-readable name for an element.
func elementName(e Element) string {
	switch e {
	case ElementFire:
		return "Fire"
	case ElementIce:
		return "Ice"
	case ElementLightning:
		return "Lightning"
	case ElementVoid:
		return "Void"
	default:
		return "Unknown"
	}
}
