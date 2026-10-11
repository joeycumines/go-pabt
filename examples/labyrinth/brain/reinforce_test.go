//go:build example

package brain

import (
	"testing"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
)

// TestReinforceActionMovesTowardAlly: when the state has a reinforce target
// (HasReinforceTarget), the action moves selfPos toward it over ticks.
func TestReinforceActionMovesTowardAlly(t *testing.T) {
	m := maze.Generate(42, 11, 11)
	selfPos := m.Start
	// Ally 3 cells to the east.
	target := maze.Vec2{X: selfPos.X + 3, Y: selfPos.Y}
	w := &WorldView{
		PlayerDetected:     false,
		AtDisadvantage:     true,
		SelfElement:        0,
		PlayerElement:      1,
		HasReinforceTarget: true,
		ReinforceTarget:    target,
	}
	s := NewState(w, m, &selfPos)
	a := NewReinforceAction(s)
	node := a.Node()

	startX, startY := selfPos.X, selfPos.Y
	var moved bool
	for i := 0; i < 20; i++ {
		status, err := node.Tick()
		if err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
		if selfPos.X != startX || selfPos.Y != startY {
			moved = true
			break
		}
		if status == bt.Success {
			break
		}
	}
	if !moved {
		t.Fatalf("reinforce did not move selfPos: start=%v,%v end=%v,%v",
			startX, startY, selfPos.X, selfPos.Y)
	}
	// After moving, the actor should be closer to the target.
	dx := target.X - selfPos.X
	dy := target.Y - selfPos.Y
	dist2 := dx*dx + dy*dy
	if dist2 >= 9.0 {
		t.Fatalf("distance to target = %g, want < 9", dist2)
	}
}

// TestReinforceActionNoAllySucceedsWithoutMove: when no reinforce target
// exists, the action succeeds immediately without moving.
func TestReinforceActionNoAllySucceedsWithoutMove(t *testing.T) {
	m := maze.Generate(42, 11, 11)
	selfPos := m.Start
	w := &WorldView{
		PlayerDetected:     false,
		AtDisadvantage:     true,
		SelfElement:        0,
		PlayerElement:      1,
		HasReinforceTarget: false,
	}
	s := NewState(w, m, &selfPos)
	a := NewReinforceAction(s)
	node := a.Node()

	status, err := node.Tick()
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if status != bt.Success {
		t.Fatalf("status = %v, want Success", status)
	}
	if selfPos != m.Start {
		t.Fatalf("selfPos moved with no ally: %v -> %v", m.Start, selfPos)
	}
}

// TestReinforceActionFailsWhenStunned: a stunned actor cannot reinforce.
func TestReinforceActionFailsWhenStunned(t *testing.T) {
	m := maze.Generate(42, 11, 11)
	selfPos := m.Start
	w := &WorldView{
		Stunned:            true,
		AtDisadvantage:     true,
		HasReinforceTarget: true,
		ReinforceTarget:    maze.Vec2{X: selfPos.X + 3, Y: selfPos.Y},
	}
	s := NewState(w, m, &selfPos)
	a := NewReinforceAction(s)
	node := a.Node()

	status, err := node.Tick()
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	if status != bt.Failure {
		t.Fatalf("status = %v, want Failure", status)
	}
}
