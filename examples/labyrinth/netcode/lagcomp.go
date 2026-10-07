//go:build example

package netcode

import (
	"math"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
)

// HistoryLen is the number of server rounds of minotaur position history
// retained for lag compensation. At 30 Hz this covers about 2 seconds.
const HistoryLen = 64

// HitRadius is the distance within which a pulse counts as a hit.
const HitRadius = 0.8

// HistEntry is one recorded position at a server round.
type HistEntry struct {
	Round int
	Pos   maze.Vec2
}

// MinotaurHistory is a ring buffer of a minotaur's recent positions, indexed
// by server round. It is the data structure behind lag-compensated hit
// validation: when a pulse arrives, the server rewinds to the round the
// shooter perceived and checks the minotaur's position at that time.
type MinotaurHistory struct {
	entries []HistEntry
}

// NewMinotaurHistory creates an empty history.
func NewMinotaurHistory() *MinotaurHistory {
	return &MinotaurHistory{}
}

// Record appends the minotaur's position at the given round, evicting the
// oldest entry when the buffer is full.
func (h *MinotaurHistory) Record(round int, pos maze.Vec2) {
	h.entries = append(h.entries, HistEntry{Round: round, Pos: pos})
	if len(h.entries) > HistoryLen {
		h.entries = h.entries[len(h.entries)-HistoryLen:]
	}
}

// PosAtRound returns the recorded position at the given round, or false if
// that round is not in the buffer.
func (h *MinotaurHistory) PosAtRound(round int) (maze.Vec2, bool) {
	for _, e := range h.entries {
		if e.Round == round {
			return e.Pos, true
		}
	}
	return maze.Vec2{}, false
}

// OldestRound returns the oldest round in the buffer, or -1 if empty.
func (h *MinotaurHistory) OldestRound() int {
	if len(h.entries) == 0 {
		return -1
	}
	return h.entries[0].Round
}

// ValidateHit performs lag-compensated hit validation for a pulse.
//
// fireRound is the server round the client perceived when it fired (derived
// from the client's render time). aimPos is the point in world space the
// client aimed at. The server rewinds the minotaur to its position at
// fireRound and checks whether aimPos is within HitRadius of that position.
//
// If fireRound is older than the buffer allows, the hit is rejected. This
// prevents a stale or malicious timestamp from forcing the server to search
// arbitrarily old history.
func ValidateHit(h *MinotaurHistory, fireRound int, aimPos maze.Vec2) bool {
	if len(h.entries) == 0 {
		return false
	}
	oldest := h.entries[0].Round
	if fireRound < oldest || fireRound > h.entries[len(h.entries)-1].Round {
		return false
	}
	pos, ok := h.PosAtRound(fireRound)
	if !ok {
		return false
	}
	d := math.Hypot(aimPos.X-pos.X, aimPos.Y-pos.Y)
	return d <= HitRadius
}
