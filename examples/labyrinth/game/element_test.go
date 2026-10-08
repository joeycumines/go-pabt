//go:build example

package game

import (
	"testing"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
)

func TestAdvantageCycle(t *testing.T) {
	// Fire > Ice > Lightning > Void > Fire.
	adv := [][2]Element{
		{ElementFire, ElementIce},
		{ElementIce, ElementLightning},
		{ElementLightning, ElementVoid},
		{ElementVoid, ElementFire},
	}
	for _, p := range adv {
		if got := advantage(p[0], p[1]); got != 2.0 {
			t.Errorf("advantage(%v,%v) = %v, want 2.0", p[0], p[1], got)
		}
		// Reverse is a disadvantage.
		if got := advantage(p[1], p[0]); got != 0.5 {
			t.Errorf("advantage(%v,%v) = %v, want 0.5", p[1], p[0], got)
		}
	}

	// Same element is neutral.
	for e := Element(0); e < elementCount; e++ {
		if got := advantage(e, e); got != 1.0 {
			t.Errorf("advantage(%v,%v) = %v, want 1.0", e, e, got)
		}
	}

	// Non-adjacent pair (fire vs lightning) is neutral both ways.
	if got := advantage(ElementFire, ElementLightning); got != 1.0 {
		t.Errorf("fire vs lightning = %v, want 1.0", got)
	}
	if got := advantage(ElementLightning, ElementFire); got != 1.0 {
		t.Errorf("lightning vs fire = %v, want 1.0", got)
	}
}

func TestActorElementMapping(t *testing.T) {
	want := map[ActorType]Element{
		ActorBrute:   ElementFire,
		ActorSwarm:   ElementIce,
		ActorCaster:  ElementLightning,
		ActorStalker: ElementVoid,
	}
	for typ, elem := range want {
		if got := actorElement(typ); got != elem {
			t.Errorf("actorElement(%v) = %v, want %v", typ, got, elem)
		}
	}
}

func TestElementNames(t *testing.T) {
	names := map[Element]string{
		ElementFire:      "Fire",
		ElementIce:       "Ice",
		ElementLightning: "Lightning",
		ElementVoid:      "Void",
	}
	for e, name := range names {
		if got := elementName(e); got != name {
			t.Errorf("elementName(%v) = %q, want %q", e, got, name)
		}
	}
}

func TestTerritoryQuadrants(t *testing.T) {
	m := maze.Generate(1, 21, 21)
	cases := []struct {
		pos  maze.Vec2
		want Element
	}{
		{maze.Vec2{X: 1.5, Y: 1.5}, ElementIce},         // north-west
		{maze.Vec2{X: 19.5, Y: 1.5}, ElementFire},       // north-east
		{maze.Vec2{X: 1.5, Y: 19.5}, ElementVoid},       // south-west
		{maze.Vec2{X: 19.5, Y: 19.5}, ElementLightning}, // south-east
	}
	for _, c := range cases {
		if got := TerritoryOf(m, c.pos); got != c.want {
			t.Errorf("TerritoryOf(%v) = %v, want %v", c.pos, got, c.want)
		}
	}
}
