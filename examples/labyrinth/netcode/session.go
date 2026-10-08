//go:build example

package netcode

import (
	"sync/atomic"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
)

// InputCmd is one client input command as received by the server.
type InputCmd struct {
	Seq     uint64
	Forward float64
	Strafe  float64
	Yaw     float64
	Sprint  bool
}

// Session is one connected player's server-side state. It owns the
// authoritative runner position and tracks which inputs have been processed
// so that every outgoing snapshot can carry the correct ack.
type Session struct {
	Token string

	// Pos is the server-authoritative runner position. It is only ever set
	// by ApplyInput, which runs Move and resolves collision, so it can never
	// penetrate a wall.
	Pos maze.Vec2
	Yaw float64

	// lastAckSeq is the sequence number of the most recently processed
	// input. It is included in every snapshot sent to this session. It is
	// atomic because ApplyInput runs on the loop writer goroutine while the
	// snapshot fan-out reads AckSeq from its own goroutine.
	lastAckSeq atomic.Uint64

	// history retains recent inputs for audit and replay diagnostics.
	history []InputCmd
}

// historyCap bounds the input history retained per session.
const historyCap = 128

// NewSession creates a session starting at the given position.
func NewSession(token string, start maze.Vec2, startYaw float64) *Session {
	return &Session{
		Token: token,
		Pos:   start,
		Yaw:   startYaw,
	}
}

// ApplyInput processes one input command against the authoritative position.
// It advances LastAckSeq to the command's sequence number and records the
// command in the input history.
//
// Stale and duplicate commands (Seq <= LastAckSeq) are dropped, so the ack
// number only ever advances monotonically per session and retransmitted
// inputs are not applied twice.
func (s *Session) ApplyInput(m *maze.Maze, cmd InputCmd, dt float64) {
	if cmd.Seq <= s.lastAckSeq.Load() {
		return
	}
	s.Pos = Move(m, s.Pos, cmd.Yaw, cmd.Forward, cmd.Strafe, cmd.Sprint, dt)
	s.Yaw = cmd.Yaw
	s.lastAckSeq.Store(cmd.Seq)

	s.history = append(s.history, cmd)
	if len(s.history) > historyCap {
		s.history = s.history[len(s.history)-historyCap:]
	}
}

// AckSeq returns the last processed input sequence number, for inclusion in
// outgoing snapshots.
func (s *Session) AckSeq() uint64 {
	return s.lastAckSeq.Load()
}

// History returns a copy of the retained input history, oldest first.
func (s *Session) History() []InputCmd {
	out := make([]InputCmd, len(s.history))
	copy(out, s.history)
	return out
}
