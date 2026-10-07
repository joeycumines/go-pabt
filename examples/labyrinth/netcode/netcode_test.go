//go:build example

package netcode

import (
	"math"
	"testing"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
)

// openMaze builds a 9x9 all-floor maze so movement tests have no walls to
// collide with except the ones explicitly added.
func openMaze(w, h int) *maze.Maze {
	m := &maze.Maze{W: w, H: h, Grid: make([]maze.Cell, w*h)}
	for i := range m.Grid {
		m.Grid[i] = maze.Cell{Kind: maze.CellFloor}
	}
	m.Start = maze.Vec2{X: 1.5, Y: 1.5}
	m.Gate = maze.Vec2{X: float64(w) - 1.5, Y: float64(h) - 1.5}
	return m
}

func setWall(m *maze.Maze, cx, cy int) {
	if cx >= 0 && cy >= 0 && cx < m.W && cy < m.H {
		m.Grid[cy*m.W+cx] = maze.Cell{Kind: maze.CellWall}
	}
}

// TestWallClippingRejectedAndCorrected proves a wall-clipping input sequence
// is rejected and corrected: the client predictor (which applies inputs
// locally without a second collision pass beyond Move) and the server both
// run the same inputs, and the server's authoritative position never enters
// the wall. A divergent client that predicted into the wall is corrected by
// reconciliation to the server position, which is outside the wall.
func TestWallClippingRejectedAndCorrected(t *testing.T) {
	m := openMaze(9, 9)
	// Wall column at x=4.
	for y := 0; y < 9; y++ {
		setWall(m, 4, y)
	}

	const dt = 1.0 / 30.0
	start := maze.Vec2{X: 3.5, Y: 4.5}

	// Client predicts: sprint straight east (yaw=0) toward the wall.
	client := NewClientPredictor(m, start, 0)
	sess := NewSession("tok", start, 0)

	for i := 0; i < 60; i++ {
		cmd := client.ApplyInput(1, 0, 0, true, dt)
		sess.ApplyInput(m, InputCmd{
			Seq: cmd.Seq, Forward: cmd.Forward, Strafe: cmd.Strafe,
			Yaw: cmd.Yaw, Sprint: cmd.Sprint,
		}, dt)
	}

	// Server position must not overlap the wall cell column [4,5)x[0,9).
	// Tangent contact (center at 4.0-radius) is valid; strictly inside is not.
	if sess.Pos.X > 4.0-maze.Radius && sess.Pos.X < 5.0+maze.Radius {
		t.Fatalf("server position %v overlaps the wall column [4,5)", sess.Pos)
	}

	// Reconcile the client to the server: after reconciliation the client
	// must equal the server position (all inputs acked).
	got := client.Reconcile(sess.Pos, sess.AckSeq(), dt)
	if got != sess.Pos {
		t.Fatalf("reconciled client pos %v != server pos %v", got, sess.Pos)
	}
	if client.PendingCount() != 0 {
		t.Fatalf("pending inputs after full ack = %d, want 0", client.PendingCount())
	}
}

// TestDivergentClientConvergesInOneWindow proves a divergent client state
// converges within one reconciliation window: after the client receives one
// snapshot carrying the server position and ack, its reconciled position
// equals what the server computes for the same unacked inputs.
func TestDivergentClientConvergesInOneWindow(t *testing.T) {
	m := openMaze(11, 11)
	const dt = 1.0 / 30.0
	start := maze.Vec2{X: 2.5, Y: 2.5}

	client := NewClientPredictor(m, start, 0)
	sess := NewSession("tok", start, 0)

	// Apply 10 inputs on both sides.
	for i := 0; i < 10; i++ {
		cmd := client.ApplyInput(1, 0.3, 0.2, false, dt)
		sess.ApplyInput(m, InputCmd{
			Seq: cmd.Seq, Forward: cmd.Forward, Strafe: cmd.Strafe,
			Yaw: cmd.Yaw, Sprint: cmd.Sprint,
		}, dt)
	}

	// Simulate divergence: corrupt the client's local position (packet loss,
	// bug, or a stale snapshot).
	client.pos = maze.Vec2{X: 99, Y: 99}

	// Now 5 more inputs are applied client-side (still unacked). Collect them
	// so the expected server-replay position can be computed independently.
	var inflight []ClientInput
	for i := 0; i < 5; i++ {
		cmd := client.ApplyInput(0.5, -0.5, 0.4, true, dt)
		// Server does NOT receive these yet (they are in flight).
		inflight = append(inflight, cmd)
	}

	// One snapshot arrives: server position after the first 10 inputs,
	// ack = 10.
	serverPos := sess.Pos
	ack := sess.AckSeq()
	if ack != 10 {
		t.Fatalf("server ack after 10 inputs = %d, want 10", ack)
	}

	// Apply the same 5 in-flight inputs to a fresh server session to get the
	// expected converged position.
	expected := NewSession("exp", serverPos, client.Yaw())
	for _, in := range inflight {
		expected.ApplyInput(m, InputCmd{
			Seq: in.Seq, Forward: in.Forward, Strafe: in.Strafe,
			Yaw: in.Yaw, Sprint: in.Sprint,
		}, dt)
	}

	// One reconciliation window: the client must converge.
	got := client.Reconcile(serverPos, ack, dt)
	if got != expected.Pos {
		t.Fatalf("converged client pos %v != expected server-replay pos %v", got, expected.Pos)
	}
	if client.PendingCount() != 5 {
		t.Fatalf("pending after reconcile = %d, want 5", client.PendingCount())
	}
}

// TestAckMonotonic proves ack numbers advance monotonically per session,
// including under duplicate and out-of-order delivery.
func TestAckMonotonic(t *testing.T) {
	m := openMaze(9, 9)
	const dt = 1.0 / 30.0
	sess := NewSession("tok", maze.Vec2{X: 2.5, Y: 2.5}, 0)

	apply := func(seq uint64) {
		sess.ApplyInput(m, InputCmd{Seq: seq, Forward: 1}, dt)
	}

	apply(1)
	apply(2)
	apply(3)
	if sess.AckSeq() != 3 {
		t.Fatalf("ack = %d, want 3", sess.AckSeq())
	}

	// Duplicate and stale retransmissions must not move the ack backward.
	apply(3)
	apply(2)
	apply(1)
	if sess.AckSeq() != 3 {
		t.Fatalf("ack moved backward to %d on duplicate/stale inputs", sess.AckSeq())
	}

	// Out-of-order: a later seq arriving early still advances; the gap is
	// simply never filled, which is correct (the inputs are independent
	// movement commands, not a reliable stream).
	apply(7)
	if sess.AckSeq() != 7 {
		t.Fatalf("ack = %d after seq 7, want 7", sess.AckSeq())
	}
	apply(5) // stale relative to ack 7
	if sess.AckSeq() != 7 {
		t.Fatalf("ack = %d after stale seq 5, want 7", sess.AckSeq())
	}
	apply(8)
	if sess.AckSeq() != 8 {
		t.Fatalf("ack = %d after seq 8, want 8", sess.AckSeq())
	}
}

// TestLagCompHitAndMiss proves lag-compensated pulse validation: a pulse
// fired at a minotaur's client-perceived position stuns it even when the
// server's present position differs, and misses when it should.
func TestLagCompHitAndMiss(t *testing.T) {
	h := NewMinotaurHistory()

	// The minotaur moved east over 10 rounds: at round r it was at (r, 5).
	for r := 1; r <= 10; r++ {
		h.Record(r, maze.Vec2{X: float64(r), Y: 5})
	}

	// The client perceived round 4 (the server is now at round 10). At round
	// 4 the minotaur was at (4,5). The client aims at (4,5): even though the
	// minotaur is now at (10,5), lag compensation rewinds and the hit lands.
	if !ValidateHit(h, 4, maze.Vec2{X: 4, Y: 5}) {
		t.Fatal("lag-comp hit at perceived round 4 rejected; want hit")
	}

	// Aiming at the minotaur's CURRENT position while perceiving round 4
	// misses, because at round 4 the minotaur was not at (10,5).
	if ValidateHit(h, 4, maze.Vec2{X: 10, Y: 5}) {
		t.Fatal("aim at present position validated against perceived round 4; want miss")
	}

	// A near miss just outside HitRadius.
	if ValidateHit(h, 4, maze.Vec2{X: 4 + HitRadius + 0.1, Y: 5}) {
		t.Fatal("aim outside hit radius counted as a hit")
	}

	// A perceived round older than the history window is rejected outright.
	if ValidateHit(h, 0, maze.Vec2{X: 4, Y: 5}) {
		t.Fatal("perceived round older than history window validated; want reject")
	}
	if ValidateHit(h, 999, maze.Vec2{X: 4, Y: 5}) {
		t.Fatal("perceived round newer than history validated; want reject")
	}

	// Empty history never hits.
	if ValidateHit(NewMinotaurHistory(), 4, maze.Vec2{X: 4, Y: 5}) {
		t.Fatal("empty history validated a hit")
	}
}

// TestHistoryRingBounds proves the position history ring buffer retains
// HistoryLen entries and evicts the oldest.
func TestHistoryRingBounds(t *testing.T) {
	h := NewMinotaurHistory()
	for r := 1; r <= HistoryLen+20; r++ {
		h.Record(r, maze.Vec2{X: float64(r), Y: 0})
	}
	if got := h.OldestRound(); got != 21 {
		t.Fatalf("oldest round = %d, want 21 after %d records", got, HistoryLen+20)
	}
	if _, ok := h.PosAtRound(20); ok {
		t.Fatal("evicted round 20 still present")
	}
	if pos, ok := h.PosAtRound(21); !ok || pos.X != 21 {
		t.Fatalf("round 21 = %v/%v, want (21,0)/true", pos, ok)
	}
}

// TestInterpBufferSampling proves snapshot interpolation: Sample lerps
// between bracketing stamps and clamps outside the buffer, and RenderRound
// renders InterpDelayRounds (100 ms at 30 Hz) behind the latest snapshot.
func TestInterpBufferSampling(t *testing.T) {
	b := NewInterpBuffer()
	b.Add(10, maze.Vec2{X: 0, Y: 0})
	b.Add(13, maze.Vec2{X: 3, Y: 0})

	// Exactly on stamps.
	p, ok := b.Sample(10)
	if !ok || p.X != 0 {
		t.Fatalf("Sample(10) = %v/%v, want (0,0)/true", p, ok)
	}
	p, ok = b.Sample(13)
	if !ok || p.X != 3 {
		t.Fatalf("Sample(13) = %v/%v, want (3,0)/true", p, ok)
	}

	// Halfway between round 10 and 13.
	p, ok = b.Sample(11.5)
	if !ok || math.Abs(p.X-1.5) > 1e-9 {
		t.Fatalf("Sample(11.5) = %v/%v, want (1.5,0)/true", p, ok)
	}

	// Clamping outside the buffer range.
	p, _ = b.Sample(5)
	if p.X != 0 {
		t.Fatalf("Sample(5) clamped = %v, want x=0", p)
	}
	p, _ = b.Sample(99)
	if p.X != 3 {
		t.Fatalf("Sample(99) clamped = %v, want x=3", p)
	}

	// Empty buffer reports false.
	if _, ok := NewInterpBuffer().Sample(1); ok {
		t.Fatal("empty buffer Sample returned ok")
	}

	// The render round is InterpDelayRounds behind the latest received.
	if got := RenderRound(43); got != 43-InterpDelayRounds {
		t.Fatalf("RenderRound(43) = %v, want %v", got, 43-InterpDelayRounds)
	}
	// 100 ms at 30 Hz is 3 rounds.
	if InterpDelayRounds != 3 {
		t.Fatalf("InterpDelayRounds = %d, want 3 (100 ms at 30 Hz)", InterpDelayRounds)
	}
}

// TestMoveDeterministicAndClamped proves Move is deterministic (the
// foundation of reconciliation) and clamps oversized input components.
func TestMoveDeterministicAndClamped(t *testing.T) {
	m := openMaze(9, 9)
	pos := maze.Vec2{X: 4.5, Y: 4.5}
	a := Move(m, pos, 0, 1, 0, false, 1.0/30.0)
	b := Move(m, pos, 0, 1, 0, false, 1.0/30.0)
	if a != b {
		t.Fatalf("Move not deterministic: %v vs %v", a, b)
	}

	// Oversized forward/strafe are clamped to [-1,1]; result must equal the
	// clamped-input result, and per-tick displacement stays bounded.
	big := Move(m, pos, 0, 99, -99, true, 1.0/30.0)
	clamped := Move(m, pos, 0, 1, -1, true, 1.0/30.0)
	if big != clamped {
		t.Fatalf("oversized input not clamped: %v vs %v", big, clamped)
	}
	d := math.Hypot(big.X-pos.X, big.Y-pos.Y)
	if d > SprintSpeed/30.0+1e-9 {
		t.Fatalf("per-tick displacement %v exceeds sprint bound %v", d, SprintSpeed/30.0)
	}

	// Diagonal is not faster than straight (normalization).
	straight := Move(m, pos, 0, 1, 0, true, 1.0/30.0)
	dd := math.Hypot(straight.X-pos.X, straight.Y-pos.Y)
	if d > dd+1e-9 {
		t.Fatalf("diagonal displacement %v exceeds straight %v", d, dd)
	}
}
