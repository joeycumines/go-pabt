//go:build example

package game

import (
	"testing"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
	"github.com/joeycumines/go-pabt/examples/labyrinth/netcode"
	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

// rayHitAt records a single-position history for an actor at pos and
// returns the history so FirePulse can validate against it.
func rayHitAt(round int, pos maze.Vec2) *netcode.MinotaurHistory {
	h := netcode.NewMinotaurHistory()
	h.Record(round, pos)
	return h
}

// TestFirePulseRayOccludedByWall verifies a wall cell between the shooter
// and the target along the ray blocks the pulse, matching the client's own
// visibility rules (walls render opaque, so the crosshair cannot legitimately
// rest on an occluded actor). This is the missing wall-occlusion behavior
// the old point-distance ValidateHit never had.
func TestFirePulseRayOccludedByWall(t *testing.T) {
	m := maze.Generate(42, 21, 21)
	g := NewGame(m, 8, 42)

	// Place a session so FirePulse has a runner position/element to use.
	sess, _, _ := g.AddSession("")
	g.World.Runner.Pos = maze.Vec2{X: 5.5, Y: 5.5}
	g.World.Runner.Element = 0
	g.sessMu.RLock()
	sess.Net.Pos = g.World.Runner.Pos
	g.sessMu.RUnlock()

	// Put a single actor dead ahead on +Y at range 3 from the shooter.
	// Force the roster/history to a single controllable actor.
	g.World.Minotaurs = g.World.Minotaurs[:1]
	m0 := g.World.Minotaurs[0]
	m0.Pos = maze.Vec2{X: 5.5, Y: 8.5}
	m0.HP = 50
	g.Histories = g.Histories[:1]
	g.Histories[0] = rayHitAt(1, m0.Pos)

	// Carve a clear corridor from shooter (5.5,5.5) through to the target's
	// cell (5,8) so the ray has line of sight over open floor, then verify.
	for _, cellY := range []int{6, 7, 8} {
		g.Maze.Grid[cellY*m.W+5] = maze.Cell{Kind: maze.CellFloor, Height: 0}
	}
	// Sanity: with no wall on a straight line, the ray is not occluded.
	if g.rayOccluded(g.World.Runner.Pos, 3.0, 0, 0) {
		t.Fatal("expected clear line of sight on open floor, got occluded")
	}

	// Now force a wall cell at (5, 7) - between shooter at y=5.5 and target
	// at y=8.5 - by overwriting the maze grid directly (test-only).
	wallIdx := 7*m.W + 5
	g.Maze.Grid[wallIdx] = maze.Cell{Kind: maze.CellWall, Height: 0}
	if !g.rayOccluded(g.World.Runner.Pos, 3.0, 0, 0) {
		t.Fatal("expected wall to occlude the ray, got clear")
	}

	// FirePulse with the occluding wall must NOT stun the actor. Fire at
	// fireRound=1 against the history recorded at round 1; the wall check
	// runs before any world tick moves the actor, so no re-record is needed.
	aim := netcode.RayHitOrigin{Pos: g.World.Runner.Pos, Yaw: 0, Pitch: 0}
	g.FirePulse(sess.ID, 1, aim)
	g.Loop.TickOnce()

	if m0.Stunned {
		t.Fatal("actor was stunned despite an occluding wall between shooter and target")
	}

	// Remove the wall; the same shot must now land. Re-record history at the
	// actor's post-tick position so the fireRound we use next actually exists
	// in the buffer at a position the ray still reaches.
	g.Maze.Grid[wallIdx] = maze.Cell{Kind: maze.CellFloor, Height: 0}
	if g.rayOccluded(g.World.Runner.Pos, 3.0, 0, 0) {
		t.Fatal("expected clear line of sight after removing wall, got occluded")
	}
	g.Histories[0].Record(2, m0.Pos)
	g.FirePulse(sess.ID, 2, aim)
	g.Loop.TickOnce()

	if !m0.Stunned {
		t.Fatal("actor was not stunned on an unobstructed straight shot")
	}
}

// TestFirePulseNearestAlongRay verifies that when two actors both lie within
// the ray's hit cylinder, the NEAREST one along the ray takes the hit - not
// whichever happens to sit first in the roster slice. The old
// ValidateHit-based loop returned on the FIRST roster match, which was not
// guaranteed to be the closest.
func TestFirePulseNearestAlongRay(t *testing.T) {
	m := maze.Generate(42, 21, 21)
	g := NewGame(m, 8, 42)

	sess, _, _ := g.AddSession("")
	g.World.Runner.Pos = maze.Vec2{X: 5.5, Y: 5.5}
	g.World.Runner.Element = 0
	g.sessMu.RLock()
	sess.Net.Pos = g.World.Runner.Pos
	g.sessMu.RUnlock()

	// Two actors on the same axis; the FAR one is placed first in the slice
	// to prove ordering, not roster position, decides. Carve a clear
	// corridor between shooter and both actors first.
	for _, cellY := range []int{6, 7, 8, 9, 10, 11} {
		g.Maze.Grid[cellY*m.W+5] = maze.Cell{Kind: maze.CellFloor, Height: 0}
	}
	g.World.Minotaurs = g.World.Minotaurs[:2]
	far := g.World.Minotaurs[0]
	near := g.World.Minotaurs[1]
	far.Pos = maze.Vec2{X: 5.5, Y: 12.5} // range 7
	near.Pos = maze.Vec2{X: 5.5, Y: 8.5} // range 3
	far.HP = 50
	near.HP = 50
	far.Stunned = false
	near.Stunned = false
	g.Histories = g.Histories[:2]
	g.Histories[0] = rayHitAt(1, far.Pos)
	g.Histories[1] = rayHitAt(1, near.Pos)

	aim := netcode.RayHitOrigin{Pos: g.World.Runner.Pos, Yaw: 0, Pitch: 0}
	g.FirePulse(sess.ID, 1, aim)
	g.Loop.TickOnce()

	if !near.Stunned {
		t.Fatal("nearest actor was not stunned")
	}
	if far.Stunned {
		t.Fatal("farther actor was stunned instead of the nearest - order-sensitive, not range-sensitive")
	}
}

// TestApplyInputStoresPitch verifies a client input carrying a non-zero
// pitch propagates through ApplyInput into the authoritative world's Runner
// state, so the snapshot the client receives reflects its own aim.
func TestApplyInputStoresPitch(t *testing.T) {
	m := maze.Generate(42, 21, 21)
	g := NewGame(m, 8, 42)

	sess, _, _ := g.AddSession("")
	_ = sess

	const wantPitch = 0.42
	cmd := netcode.InputCmd{
		Seq:     1,
		Forward: 0,
		Strafe:  0,
		Yaw:     0,
		Pitch:   wantPitch,
		Sprint:  false,
	}
	g.ApplyInput(sess.ID, cmd)
	g.Loop.TickOnce()

	if g.World.Runner.Pitch != wantPitch {
		t.Fatalf("runner pitch = %v, want %v", g.World.Runner.Pitch, wantPitch)
	}
	if g.World.Runner.Yaw != 0 {
		t.Fatalf("runner yaw = %v, want 0", g.World.Runner.Yaw)
	}
	_ = world.PhaseRunning // keep world import for future phase asserts
}
