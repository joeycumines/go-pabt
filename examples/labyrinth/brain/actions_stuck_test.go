//go:build example

package brain

import (
	"testing"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
)

// TestRetreatActionStuckFailsAfterLimit verifies that a wall-wedged retreat
// (all headings resolve back to the current position) fails after stuckLimit
// ticks instead of returning bt.Running forever. This is the B1 actor-freeze
// regression: an action node stuck in Running with zero displacement froze
// the entire war because the planner never re-instanced it.
func TestRetreatActionStuckFailsAfterLimit(t *testing.T) {
	m := maze.Generate(42, 11, 11)
	// Place the player at the actor's exact position: dx=dy=0, so every
	// retreat heading computes a zero direction vector and Resolve returns
	// the current position unchanged for all 8 sampled headings.
	w := &WorldView{
		PlayerDetected: true,
		PlayerPos:      m.Start,
		AtDisadvantage: true,
		SelfElement:    0, // fire
		PlayerElement:  1, // ice beats fire
	}
	selfPos := m.Start
	s := NewState(w, m, &selfPos)
	a := NewRetreatAction(s)
	node := a.Node()

	var status bt.Status
	for i := 0; i < stuckLimit+1; i++ {
		status, _ = node.Tick()
		if status == bt.Failure {
			if i < stuckLimit-1 {
				t.Fatalf("retreat failed too early at tick %d, want >= %d", i, stuckLimit)
			}
			return
		}
		if status != bt.Running {
			t.Fatalf("tick %d: status = %v, want Running", i, status)
		}
	}
	t.Fatalf("retreat action did not fail after %d stuck ticks; last status %v", stuckLimit, status)
}

// TestRetreatActionMovesWhenUnblocked verifies that a retreat with a clear
// away-from-player heading moves the actor and does not trip the stuck
// counter.
func TestRetreatActionMovesWhenUnblocked(t *testing.T) {
	m := maze.Generate(42, 11, 11)
	// Place the player 3 cells to the east of the actor so the away heading
	// points west, which is open floor in this seed's start neighborhood.
	selfPos := m.Start
	w := &WorldView{
		PlayerDetected: true,
		PlayerPos:      maze.Vec2{X: selfPos.X + 3, Y: selfPos.Y},
		AtDisadvantage: true,
		SelfElement:    0,
		PlayerElement:  1,
	}
	s := NewState(w, m, &selfPos)
	a := NewRetreatAction(s)
	node := a.Node()

	startX, startY := selfPos.X, selfPos.Y
	var moved bool
	for i := 0; i < 10; i++ {
		status, err := node.Tick()
		if err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
		if status == bt.Failure {
			t.Fatalf("retreat failed at tick %d despite open heading", i)
		}
		if selfPos.X != startX || selfPos.Y != startY {
			moved = true
			break
		}
	}
	if !moved {
		t.Fatalf("retreat did not move the actor in 10 ticks")
	}
}
