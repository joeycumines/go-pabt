//go:build example

package netcode

import (
	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
)

// ClientPredictor is the client-side prediction and reconciliation reference.
// It mirrors the server's Move function so that a client can predict locally
// and then converge on the server's authoritative position during
// reconciliation.
//
// The contract (rendered in T6/T7):
//  1. Prediction: each input is applied locally immediately via ApplyInput,
//     giving instant feedback.
//  2. Reconciliation: when a snapshot arrives with ackSeq, the client drops
//     all inputs with seq <= ackSeq, resets its position to the server's
//     authoritative position, and replays the remaining unacked inputs.
//     Because Move is deterministic, this converges on the server's answer
//     within one round trip.
type ClientPredictor struct {
	m       *maze.Maze
	pos     maze.Vec2
	yaw     float64
	pending []ClientInput
	nextSeq uint64
}

// ClientInput is one input applied by the client, retained until acked.
type ClientInput struct {
	Seq     uint64
	Forward float64
	Strafe  float64
	Yaw     float64
	Sprint  bool
}

// NewClientPredictor creates a predictor starting at the given position.
func NewClientPredictor(m *maze.Maze, start maze.Vec2, startYaw float64) *ClientPredictor {
	return &ClientPredictor{
		m:       m,
		pos:     start,
		yaw:     startYaw,
		nextSeq: 1,
	}
}

// Pos returns the client's current predicted position.
func (c *ClientPredictor) Pos() maze.Vec2 {
	return c.pos
}

// Yaw returns the client's current yaw.
func (c *ClientPredictor) Yaw() float64 {
	return c.yaw
}

// PendingCount returns the number of unacked inputs.
func (c *ClientPredictor) PendingCount() int {
	return len(c.pending)
}

// ApplyInput applies one input locally (prediction) and returns the input
// record for transmission to the server.
func (c *ClientPredictor) ApplyInput(forward, strafe, yaw float64, sprint bool, dt float64) ClientInput {
	cmd := ClientInput{
		Seq:     c.nextSeq,
		Forward: forward,
		Strafe:  strafe,
		Yaw:     yaw,
		Sprint:  sprint,
	}
	c.nextSeq++

	c.pos = Move(c.m, c.pos, yaw, forward, strafe, sprint, dt)
	c.yaw = yaw
	c.pending = append(c.pending, cmd)
	return cmd
}

// Reconcile processes a server snapshot. It drops all inputs with seq <=
// ackSeq, resets the predicted position to the server's authoritative
// position, and replays the remaining unacked inputs. The returned position
// is the reconciled client position.
//
// This is the core of the reconciliation contract: because Move is
// deterministic and identical to the server's movement, replaying unacked
// inputs from the server's acked position converges on the server's answer
// within one round trip.
func (c *ClientPredictor) Reconcile(serverPos maze.Vec2, ackSeq uint64, dt float64) maze.Vec2 {
	// Drop acked inputs.
	var unacked []ClientInput
	for _, in := range c.pending {
		if in.Seq > ackSeq {
			unacked = append(unacked, in)
		}
	}
	c.pending = unacked

	// Reset to server position.
	c.pos = serverPos

	// Replay unacked inputs.
	for _, in := range c.pending {
		c.pos = Move(c.m, c.pos, in.Yaw, in.Forward, in.Strafe, in.Sprint, dt)
		c.yaw = in.Yaw
	}

	return c.pos
}
