//go:build example

package netcode

import (
	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
)

// InterpBufferRounds is how many rounds of stamped snapshots the buffer
// retains. At 30 Hz with a 100 ms (3-round) interpolation delay, 16 rounds
// gives ample headroom for jitter.
const InterpBufferRounds = 16

// StampEntry is one stamped minotaur position at a server round.
type StampEntry struct {
	Round int
	Pos   maze.Vec2
}

// InterpBuffer holds recent stamped positions for one minotaur and answers
// interpolation queries. The client renders the minotaur InterpDelay rounds
// behind the latest received snapshot, lerping between the two entries that
// bracket the render time. This is the "100 ms behind" interpolation target
// required by the netcode design.
type InterpBuffer struct {
	entries []StampEntry
}

// NewInterpBuffer creates an empty buffer.
func NewInterpBuffer() *InterpBuffer {
	return &InterpBuffer{}
}

// Add records a stamped position, evicting old entries beyond
// InterpBufferRounds.
func (b *InterpBuffer) Add(round int, pos maze.Vec2) {
	b.entries = append(b.entries, StampEntry{Round: round, Pos: pos})
	if len(b.entries) > InterpBufferRounds {
		b.entries = b.entries[len(b.entries)-InterpBufferRounds:]
	}
}

// Sample returns the interpolated position for the given fractional round.
// renderRound is expressed in rounds (e.g. 42.5 means halfway between round
// 42 and 43). It lerps between the two entries bracketing renderRound, or
// clamps to the nearest entry if renderRound is outside the buffer range.
// Returns false if the buffer is empty.
func (b *InterpBuffer) Sample(renderRound float64) (maze.Vec2, bool) {
	if len(b.entries) == 0 {
		return maze.Vec2{}, false
	}
	if len(b.entries) == 1 {
		return b.entries[0].Pos, true
	}

	// Clamp to buffer range.
	first := b.entries[0]
	last := b.entries[len(b.entries)-1]
	if renderRound <= float64(first.Round) {
		return first.Pos, true
	}
	if renderRound >= float64(last.Round) {
		return last.Pos, true
	}

	// Find the two entries bracketing renderRound.
	for i := 0; i < len(b.entries)-1; i++ {
		e0, e1 := b.entries[i], b.entries[i+1]
		if float64(e0.Round) <= renderRound && renderRound <= float64(e1.Round) {
			span := float64(e1.Round - e0.Round)
			if span == 0 {
				return e0.Pos, true
			}
			t := (renderRound - float64(e0.Round)) / span
			return maze.Vec2{
				X: e0.Pos.X + (e1.Pos.X-e0.Pos.X)*t,
				Y: e0.Pos.Y + (e1.Pos.Y-e0.Pos.Y)*t,
			}, true
		}
	}
	return last.Pos, true
}

// InterpDelayRounds is the interpolation delay expressed in server rounds.
// At 30 Hz, 100 ms is 3 rounds.
const InterpDelayRounds = 3

// RenderRound computes the render round for a client that has received up to
// latestReceivedRound. The client renders InterpDelayRounds behind the
// latest received round.
func RenderRound(latestReceivedRound int) float64 {
	return float64(latestReceivedRound) - InterpDelayRounds
}
