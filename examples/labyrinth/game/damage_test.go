//go:build example

package game

import (
	"testing"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

// hitRecords returns the 'hit' events the server has broadcast.
func hitRecords(g *Game) []EventRecord {
	var out []EventRecord
	for _, rec := range g.EventRecords() {
		if rec.Type == "hit" {
			out = append(out, rec)
		}
	}
	return out
}

// TestHazardDamageEmitsHitEvent: a rift that hurts the player must say so.
// Without an event the client has no cue, so a player loses HP with nothing on
// screen to explain it (player-eye D6).
func TestHazardDamageEmitsHitEvent(t *testing.T) {
	m := maze.Generate(42, 21, 21)
	g := NewGame(m, 0, 42)
	w := g.World
	w.Runner.Pos = maze.Vec2{X: 10.5, Y: 10.5}
	w.Runner.HP = 100
	w.Hazards = []*world.Hazard{{
		Pos:            w.Runner.Pos,
		Element:        0,
		TTL:            60,
		DamageCooldown: 0,
	}}

	g.tickHazards(w)

	if w.Runner.HP != 100-HazardDamage {
		t.Fatalf("hazard damage: HP = %d, want %d", w.Runner.HP, 100-HazardDamage)
	}
	hits := hitRecords(g)
	if len(hits) != 1 {
		t.Fatalf("hazard damage broadcast %d hit events, want 1", len(hits))
	}
	data, ok := hits[0].Data.(map[string]interface{})
	if !ok {
		t.Fatalf("hit event data type = %T", hits[0].Data)
	}
	if data["source"] != "hazard" {
		t.Fatalf("hit source = %v, want hazard", data["source"])
	}
	if data["hp"] != w.Runner.HP {
		t.Fatalf("hit hp = %v, want %d", data["hp"], w.Runner.HP)
	}
	if _, ok := data["x"]; !ok {
		t.Fatalf("hit event carries no attacker position: %v", data)
	}
}
