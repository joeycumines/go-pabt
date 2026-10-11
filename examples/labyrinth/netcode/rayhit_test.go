//go:build example

package netcode

import (
	"testing"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
)

func mustHistory(t *testing.T, round int, pos maze.Vec2) *MinotaurHistory {
	t.Helper()
	h := NewMinotaurHistory()
	h.Record(round, pos)
	return h
}

// TestRayHitAxisDistances verifies a level (pitch 0) shot at yaw 0 (facing
// +Y) hits a target dead-center on the axis at every in-range distance, and
// misses beyond the max range. This is the core regression the old
// point-distance ValidateHit had: a target closer than ~4.2u or farther than
// ~5.8u from a fixed aim point missed even when dead-center on screen.
func TestRayHitAxisDistances(t *testing.T) {
	origin := RayHitOrigin{Pos: maze.Vec2{X: 0, Y: 0}, Yaw: 0, Pitch: 0}
	for _, dist := range []float64{1, 3, 5, 8} {
		h := mustHistory(t, 1, maze.Vec2{X: 0, Y: dist})
		if _, ok := ValidateRayHit(h, 1, origin); !ok {
			t.Errorf("expected hit at distance %v, got miss", dist)
		}
	}
	// Beyond RayHitMax: miss.
	h := mustHistory(t, 1, maze.Vec2{X: 0, Y: RayHitMax + 1})
	if _, ok := ValidateRayHit(h, 1, origin); ok {
		t.Errorf("expected miss beyond RayHitMax (%v), got hit", RayHitMax)
	}
	// Exactly at RayHitMax: hit (inclusive upper bound).
	h = mustHistory(t, 1, maze.Vec2{X: 0, Y: RayHitMax})
	if _, ok := ValidateRayHit(h, 1, origin); !ok {
		t.Errorf("expected hit at exactly RayHitMax (%v), got miss", RayHitMax)
	}
}

// TestRayHitBehindOriginMisses verifies a target behind the shooter along
// the aim direction is never hit.
func TestRayHitBehindOriginMisses(t *testing.T) {
	origin := RayHitOrigin{Pos: maze.Vec2{X: 0, Y: 0}, Yaw: 0, Pitch: 0}
	h := mustHistory(t, 1, maze.Vec2{X: 0, Y: -3})
	if _, ok := ValidateRayHit(h, 1, origin); ok {
		t.Fatal("expected miss for target behind origin, got hit")
	}
}

// TestRayHitLateralOffset verifies a target off the ray axis is hit only
// within HitRadius and missed outside it.
func TestRayHitLateralOffset(t *testing.T) {
	origin := RayHitOrigin{Pos: maze.Vec2{X: 0, Y: 0}, Yaw: 0, Pitch: 0}
	// Within radius: hit.
	h := mustHistory(t, 1, maze.Vec2{X: HitRadius - 0.1, Y: 5})
	if _, ok := ValidateRayHit(h, 1, origin); !ok {
		t.Fatal("expected hit within HitRadius lateral offset, got miss")
	}
	// Outside radius: miss.
	h = mustHistory(t, 1, maze.Vec2{X: HitRadius + 0.2, Y: 5})
	if _, ok := ValidateRayHit(h, 1, origin); ok {
		t.Fatal("expected miss beyond HitRadius lateral offset, got hit")
	}
}

// TestRayHitPitchShrinksForward verifies the forward vector's horizontal
// component shrinks by cos(pitch), which is the mathematically honest
// effect of pitch in a floor-plane world: a target dead-ahead at yaw 0
// always has lateral offset 0 regardless of pitch, so pitch cannot miss a
// floor target that is within horizontal range - what it changes is the
// ray's horizontal reach (and therefore which walls occlude and how far
// off-axis targets resolve). This test pins the projection math.
func TestRayHitPitchShrinksForward(t *testing.T) {
	fwd := forwardVec(0, 0)
	if fwd.Y < 0.999 {
		t.Fatalf("pitch 0 forward Y = %v, want ~1", fwd.Y)
	}
	const deg60 = 1.04719755 // 60 degrees in radians
	fwd60 := forwardVec(0, deg60)
	if fwd60.Y > 0.55 || fwd60.Y < 0.45 {
		t.Fatalf("pitch 60 forward Y = %v, want ~0.5 (cos 60)", fwd60.Y)
	}
	// A near-max-range target is hit at pitch 0; at pitch 60 the same target
	// is still hit (lateral offset 0) but with a shorter ground track,
	// confirming the projection shrank without spuriously missing.
	const near = RayHitMax - 0.1
	origin := RayHitOrigin{Pos: maze.Vec2{X: 0, Y: 0}, Yaw: 0, Pitch: 0}
	h := mustHistory(t, 1, maze.Vec2{X: 0, Y: near})
	hit, ok := ValidateRayHit(h, 1, origin)
	if !ok {
		t.Fatal("expected hit at pitch 0 near max range, got miss")
	}
	if hit.Range < 0.99*near || hit.Range > 1.01*near {
		t.Fatalf("pitch 0 rangeAlong = %v, want ~%v (full horizontal reach)", hit.Range, near)
	}
}

// TestRayHitYawRotates verifies the aim direction rotates with yaw, matching
// the client camera convention (yaw 0 faces +Y, positive yaw rotates toward
// +X).
func TestRayHitYawRotates(t *testing.T) {
	const yaw90 = 1.57079633
	origin := RayHitOrigin{Pos: maze.Vec2{X: 0, Y: 0}, Yaw: yaw90, Pitch: 0}
	h := mustHistory(t, 1, maze.Vec2{X: 5, Y: 0})
	if _, ok := ValidateRayHit(h, 1, origin); !ok {
		t.Fatal("expected hit on +X axis at yaw 90, got miss")
	}
	// Target straight ahead at yaw 0 should now be lateral to the yaw-90 aim.
	h = mustHistory(t, 1, maze.Vec2{X: 0, Y: 5})
	if _, ok := ValidateRayHit(h, 1, origin); ok {
		t.Fatal("expected miss for target on +Y axis at yaw 90, got hit")
	}
}

// TestRayHitStaleRoundRejected verifies a fireRound older than the history
// buffer rejects, mirroring the anti-cheat behavior of the point-based
// ValidateHit.
func TestRayHitStaleRoundRejected(t *testing.T) {
	origin := RayHitOrigin{Pos: maze.Vec2{X: 0, Y: 0}, Yaw: 0, Pitch: 0}
	h := NewMinotaurHistory()
	for r := 10; r <= 20; r++ {
		h.Record(r, maze.Vec2{X: 0, Y: 5})
	}
	// fireRound before oldest recorded round: reject.
	if _, ok := ValidateRayHit(h, 5, origin); ok {
		t.Fatal("expected reject for fireRound before history, got hit")
	}
	// fireRound after newest: reject.
	if _, ok := ValidateRayHit(h, 999, origin); ok {
		t.Fatal("expected reject for fireRound after history, got hit")
	}
	// Empty history: reject.
	if _, ok := ValidateRayHit(NewMinotaurHistory(), 1, origin); ok {
		t.Fatal("expected reject for empty history, got hit")
	}
}

// TestRayHitNearestAlongRay verifies range ordering so FirePulse can pick
// the nearest actor along the ray when multiple actors qualify.
func TestRayHitNearestAlongRay(t *testing.T) {
	origin := RayHitOrigin{Pos: maze.Vec2{X: 0, Y: 0}, Yaw: 0, Pitch: 0}
	near := mustHistory(t, 1, maze.Vec2{X: 0, Y: 3})
	far := mustHistory(t, 1, maze.Vec2{X: 0, Y: 7})
	hitNear, okNear := ValidateRayHit(near, 1, origin)
	hitFar, okFar := ValidateRayHit(far, 1, origin)
	if !okNear || !okFar {
		t.Fatalf("expected both to hit, got okNear=%v okFar=%v", okNear, okFar)
	}
	if hitNear.Range >= hitFar.Range {
		t.Fatalf("expected near range %v < far range %v", hitNear.Range, hitFar.Range)
	}
}
