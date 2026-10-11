//go:build example

package netcode

import (
	"math"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
)

// HistoryLen is the number of server rounds of minotaur position history
// retained for lag compensation. At 30 Hz this covers about 2 seconds.
const HistoryLen = 64

// HitRadius is the lateral half-width of the aim ray within which a pulse
// counts as a hit. The world is a floor plane (no vertical actor offset), so
// a hit is lateral distance from the ray, not distance from a point.
const HitRadius = 0.8

// RayHitMax is the maximum range along the aim ray at which a pulse can land.
// It replaces the old fixed sphere of effect at ~5 units: the crosshair now
// resolves a ray, so a distant actor under the crosshair is hit and a close
// actor just off the ray is missed.
const RayHitMax = 12.0

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

// ValidateHit performed point-distance lag compensation: rewind the minotaur
// and test whether the aim POINT fell within HitRadius. It is kept for
// reference and older tests but is no longer the pulse hit model — see
// ValidateRayHit. Deprecated: pulses are rays now.
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

// RayHitOrigin is the shooter's aim ray at the moment of firing, in maze
// (floor-plane) coordinates. Pos is the eye position (the runner's current
// position; the maze is 2D and actors sit on the floor plane). Yaw and Pitch
// are the camera angles exactly as the client sends them, using the same
// convention as client/js/camera.mjs: yaw 0 faces +Y, pitch positive looks
// up, forward = (sin(yaw)cos(pitch), sin(pitch), cos(yaw)cos(pitch)).
type RayHitOrigin struct {
	Pos   maze.Vec2
	Yaw   float64
	Pitch float64
}

// forwardVec returns the maze-plane projection of the camera forward vector.
// The vertical component (sin(pitch)) is dropped because the world is a
// floor plane with no height axis: pitch's honest in-plane effect is to
// shrink the horizontal reach by cos(pitch). A near-vertical shot (cos ~ 0)
// degenerates to a zero-length ray and can hit nothing in-plane.
func forwardVec(yaw, pitch float64) maze.Vec2 {
	cp := math.Cos(pitch)
	return maze.Vec2{X: math.Sin(yaw) * cp, Y: math.Cos(yaw) * cp}
}

// RayHit is the result of resolving the aim ray against one actor's
// history. Range is the distance along the ray to the hit (the nearest point
// of the actor's swept tube to the origin). Dist is the lateral offset from
// the ray axis (the miss distance), used for near-miss diagnostics.
type RayHit struct {
	Range float64
	Dist  float64
}

// ValidateRayHit performs lag-compensated ray hit validation for a pulse.
//
// fireRound is the server round the client perceived when it fired (derived
// from the client's render time). The server rewinds the actor to its
// position at fireRound and resolves the aim ray from origin against a
// capsule of radius HitRadius centred on that position, oriented along the
// ray's plane projection. A hit lands when the ray passes within HitRadius
// of the actor and the nearest such point is in front of the eye and within
// RayHitMax.
//
// If fireRound is older than the buffer allows, the hit is rejected. This
// prevents a stale or malicious timestamp from forcing the server to search
// arbitrarily old history.
func ValidateRayHit(h *MinotaurHistory, fireRound int, origin RayHitOrigin) (RayHit, bool) {
	if len(h.entries) == 0 {
		return RayHit{}, false
	}
	oldest := h.entries[0].Round
	if fireRound < oldest || fireRound > h.entries[len(h.entries)-1].Round {
		return RayHit{}, false
	}
	pos, ok := h.PosAtRound(fireRound)
	if !ok {
		return RayHit{}, false
	}
	f := forwardVec(origin.Yaw, origin.Pitch)
	fLen2 := f.X*f.X + f.Y*f.Y
	if fLen2 < 1e-12 {
		// Degenerate near-vertical aim: no in-plane ray to resolve.
		return RayHit{}, false
	}
	// Decompose the actor's offset from the eye onto the ray axis and the
	// perpendicular. The lateral component is normalised by the axis length
	// so it is a true perpendicular distance in the plane, not a component
	// of a non-unit vector.
	rx := pos.X - origin.Pos.X
	ry := pos.Y - origin.Pos.Y
	rangeAlong := (rx*f.X + ry*f.Y) / fLen2
	if rangeAlong <= 0 || rangeAlong > RayHitMax {
		return RayHit{}, false
	}
	// 2D cross product magnitude divided by the axis length gives the true
	// perpendicular (lateral) distance from the ray to the actor's centre.
	lateral := math.Abs(rx*f.Y-ry*f.X) / math.Sqrt(fLen2)
	if lateral > HitRadius {
		return RayHit{}, false
	}
	return RayHit{Range: rangeAlong, Dist: lateral}, true
}
