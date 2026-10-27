//go:build example

package game

import (
	"testing"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
)

func TestEscalationStopsAtRosterCap(t *testing.T) {
	m := maze.Generate(42, 21, 21)
	g := NewGame(m, TargetActorCount, 42)

	// Below the cap an escalation round still adds one actor.
	g.World.Round = 300
	g.tickLogic(g.World)
	if got := len(g.World.Minotaurs); got != TargetActorCount+1 {
		t.Fatalf("roster after first escalation = %d, want %d", got, TargetActorCount+1)
	}

	// Fill to the cap. Respawns hold the roster at TargetActorCount without
	// shrinking it, so escalation must refuse to grow it past the cap.
	for len(g.World.Minotaurs) < TargetActorCount+2 {
		before := len(g.World.Minotaurs)
		g.spawnActor(ActorType(0))
		if len(g.World.Minotaurs) == before {
			t.Fatal("could not fill the roster to the cap")
		}
	}
	g.World.Round = 600
	g.tickLogic(g.World)
	if got := len(g.World.Minotaurs); got > TargetActorCount+2 {
		t.Fatalf("roster exceeded the cap: %d > %d", got, TargetActorCount+2)
	}
}
