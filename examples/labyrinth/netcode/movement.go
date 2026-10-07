//go:build example

package netcode

import (
	"math"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
)

// Movement speeds in cells per second.
const (
	RunSpeed    = 4.0
	SprintSpeed = 6.5
	// MaxStep is the farthest a single input may legitimately move the runner
	// in one tick at sprint speed. Input commands that would displace the
	// runner farther than this are impossible and rejected. It bounds the
	// per-tick displacement so a malicious client cannot teleport.
	MaxStep = SprintSpeed / 30.0
)

// Move applies one tick of movement from pos using a yaw-relative
// forward/strafe command, then resolves the result against maze collision.
// The returned position is authoritative: it can never penetrate a wall, so a
// client that predicted a wall-clipping position is always corrected to this.
//
// forward and strafe are clamped to [-1,1]. sprint selects the sprint speed.
// The movement is deterministic: the same (pos, yaw, forward, strafe, sprint)
// always yields the same result, which is what lets a client replay unacked
// inputs during reconciliation and converge on the server's answer.
func Move(m *maze.Maze, pos maze.Vec2, yaw, forward, strafe float64, sprint bool, dt float64) maze.Vec2 {
	forward = clamp1(forward)
	strafe = clamp1(strafe)

	speed := RunSpeed
	if sprint {
		speed = SprintSpeed
	}

	// Yaw-relative basis: forward points along yaw, strafe is 90 degrees to
	// the right. The convention only needs to be consistent between the
	// server and the client predictor.
	fx, fy := math.Cos(yaw), math.Sin(yaw)
	sx, sy := -fy, fx

	dx := fx*forward + sx*strafe
	dy := fy*forward + sy*strafe

	// Normalize so diagonal movement is not faster than straight movement.
	if n := math.Hypot(dx, dy); n > 1 {
		dx /= n
		dy /= n
	}

	next := maze.Vec2{
		X: pos.X + dx*speed*dt,
		Y: pos.Y + dy*speed*dt,
	}
	return m.Resolve(next, maze.Radius)
}

// clamp1 clamps v to [-1,1].
func clamp1(v float64) float64 {
	if v > 1 {
		return 1
	}
	if v < -1 {
		return -1
	}
	return v
}
