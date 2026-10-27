//go:build example

package game

import (
	"testing"
	"time"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
	"github.com/joeycumines/go-pabt/examples/labyrinth/netcode"
	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

func TestPresenceGateFreezesUntilPlaying(t *testing.T) {
	m := maze.Generate(42, 21, 21)
	g := NewGameWithOptions(m, 8, 42, Options{PauseWithoutPlayer: true})

	g.Loop.TickOnce()
	if g.World.Round != 0 {
		t.Fatalf("world advanced before any session: round %d", g.World.Round)
	}

	sess, _, gen := g.AddSession("")
	g.Loop.TickOnce()
	if g.World.Round != 0 {
		t.Fatalf("world advanced while the session was not playing: round %d", g.World.Round)
	}

	g.SetPlaying(sess.ID, true)
	g.Loop.TickOnce()
	if g.World.Round != 1 {
		t.Fatalf("world did not advance while playing: round %d", g.World.Round)
	}

	g.SetPlaying(sess.ID, false)
	g.Loop.TickOnce()
	if g.World.Round != 1 {
		t.Fatalf("world advanced after play stopped: round %d", g.World.Round)
	}

	// A stale play-on from an unknown session must not open the gate.
	g.SetPlaying(SessionID("removed-session"), true)
	g.Loop.TickOnce()
	if g.World.Round != 1 {
		t.Fatalf("unknown session opened the gate: round %d", g.World.Round)
	}

	// Disconnect teardown must clear the flag even if the client never sent
	// play off (tab crash).
	g.SetPlaying(sess.ID, true)
	g.ReleaseConnection(sess.ID, gen, nil, nil, nil)
	g.Loop.TickOnce()
	if g.World.Round != 1 {
		t.Fatalf("release left the gate open: round %d", g.World.Round)
	}
}

func TestPresenceGateDisabledByDefault(t *testing.T) {
	m := maze.Generate(42, 21, 21)
	g := NewGame(m, 8, 42)
	g.Loop.TickOnce()
	if g.World.Round != 1 {
		t.Fatalf("legacy NewGame should advance without a player: round %d", g.World.Round)
	}
}

func TestCleanupSessionsClearsPlaying(t *testing.T) {
	m := maze.Generate(42, 21, 21)
	g := NewGameWithOptions(m, 8, 42, Options{PauseWithoutPlayer: true})
	sess, _, _ := g.AddSession("")
	g.SetPlaying(sess.ID, true)
	if !g.anyPlaying() {
		t.Fatal("playing flag not set")
	}

	// Simulate a retained session whose disconnect teardown never ran.
	g.sessMu.Lock()
	sess.disconnectedAt = time.Now().Add(-sessionGracePeriod - time.Second)
	g.sessMu.Unlock()
	g.CleanupSessions()

	if g.anyPlaying() {
		t.Fatal("cleanup removed the session but left the gate open")
	}
}

func TestTerminalPhaseIgnoresInputAndFire(t *testing.T) {
	m := maze.Generate(42, 21, 21)
	g := NewGame(m, 8, 42)
	sess, _, _ := g.AddSession("")
	// Drain the AddSession world-reset command while the world is running;
	// otherwise it would legitimately reset the terminal phase below.
	g.Loop.TickOnce()
	frozenRound := g.World.Round

	g.World.Phase = world.PhaseLost
	before := g.World.Runner.Pos

	g.ApplyInput(sess.ID, netcode.InputCmd{Seq: 1, Forward: 1, Yaw: 0})
	g.FirePulse(sess.ID, 1, netcode.RayHitOrigin{Pos: g.World.Runner.Pos})
	g.Loop.TickOnce()

	if g.World.Round != frozenRound {
		t.Fatalf("terminal world advanced: round %d -> %d", frozenRound, g.World.Round)
	}
	if g.World.Runner.Pos != before {
		t.Fatalf("terminal world moved runner: %v -> %v", before, g.World.Runner.Pos)
	}
	if got := g.PulsesFired(); got != 0 {
		t.Fatalf("terminal world counted a shot: %d", got)
	}
}
