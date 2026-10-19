//go:build example

package game

import (
	"testing"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
)

// TestReleaseConnectionDoesNotClobberRejoinedConnection is the regression for
// the reload race observed in E2E: the browser closes the old WebSocket and
// opens a new one that rejoins the same session by token; when the OLD
// socket's close is observed AFTER the new socket has subscribed, the old
// handler's teardown used to delete the new connection's subscriber
// channels unconditionally, silently stopping all snapshots for the live
// client (observed as wsOpen=true with no snapshot for 20s). It also marked
// the reconnected session disconnected, which would let CleanupSessions
// garbage-collect a live session 60s later.
func TestReleaseConnectionDoesNotClobberRejoinedConnection(t *testing.T) {
	m := maze.Generate(42, 21, 21)
	g := NewGame(m, 8, 42)

	// Connection 1: fresh session, subscribes its channels.
	sess, token, gen1 := g.AddSession("")
	snap1 := make(chan []byte, 32)
	event1 := make(chan Event, 64)
	tree1 := make(chan map[string][]byte, 4)
	g.Subscribe(sess.ID, gen1, snap1)
	g.SubscribeEvents(sess.ID, gen1, event1)
	g.SubscribeTrees(sess.ID, gen1, tree1)

	// Connection 2 (reload race): the new socket joins with the same token
	// BEFORE connection 1's handler has observed its close.
	got, gotToken, gen2 := g.AddSession(token)
	if gotToken != token || got.ID != sess.ID {
		t.Fatalf("rejoin must reuse the same session, got id=%q token=%q", got.ID, gotToken)
	}
	if gen2 <= gen1 {
		t.Fatalf("connGen after rejoin = %d, want > %d", gen2, gen1)
	}
	snap2 := make(chan []byte, 32)
	event2 := make(chan Event, 64)
	tree2 := make(chan map[string][]byte, 4)
	g.Subscribe(sess.ID, gen2, snap2)
	g.SubscribeEvents(sess.ID, gen2, event2)
	g.SubscribeTrees(sess.ID, gen2, tree2)

	// Connection 1's handler now exits. Its scoped release must not touch
	// connection 2's live subscription nor disconnect-mark the session.
	g.ReleaseConnection(sess.ID, gen1, snap1, event1, tree1)

	g.subMu.RLock()
	curSnap, okSnap := g.subscribers[sess.ID]
	g.subMu.RUnlock()
	if !okSnap || curSnap.ch != snap2 || curSnap.gen != gen2 {
		t.Fatal("old connection's release deleted the rejoined connection's snapshot subscription")
	}
	g.eventMu.RLock()
	curEvent, okEvent := g.eventSubs[sess.ID]
	g.eventMu.RUnlock()
	if !okEvent || curEvent.ch != event2 || curEvent.gen != gen2 {
		t.Fatal("old connection's release deleted the rejoined connection's event subscription")
	}
	g.treeMu.RLock()
	curTree, okTree := g.treeSubs[sess.ID]
	g.treeMu.RUnlock()
	if !okTree || curTree.ch != tree2 || curTree.gen != gen2 {
		t.Fatal("old connection's release deleted the rejoined connection's tree subscription")
	}
	g.sessMu.RLock()
	stillDisconnected := !g.sessions[sess.ID].disconnectedAt.IsZero()
	g.sessMu.RUnlock()
	if stillDisconnected {
		t.Fatal("old connection's release marked the reconnected session disconnected")
	}

	// The current connection's own release must still tear down cleanly.
	g.ReleaseConnection(sess.ID, gen2, snap2, event2, tree2)
	g.subMu.RLock()
	_, stillSubbed := g.subscribers[sess.ID]
	g.subMu.RUnlock()
	if stillSubbed {
		t.Fatal("current connection's release left its snapshot subscription behind")
	}
	g.sessMu.RLock()
	markedDisconnected := !g.sessions[sess.ID].disconnectedAt.IsZero()
	g.sessMu.RUnlock()
	if !markedDisconnected {
		t.Fatal("current connection's release must mark the session disconnected")
	}
}

// TestSubscribeRejectsOlderGeneration pins the double-reload inversion fix:
// a dying connection's LATE Subscribe (delayed, e.g., in WaitForRunningPhase
// while the world was terminal, up to 2s) must never displace the newer
// connection's registration — otherwise the live client is orphaned with no
// snapshots until it reconnects.
func TestSubscribeRejectsOlderGeneration(t *testing.T) {
	m := maze.Generate(42, 21, 21)
	g := NewGame(m, 8, 42)

	sess, token, gen1 := g.AddSession("")
	_, _, gen2 := g.AddSession(token) // double reload: session superseded to gen2

	// Newer connection subscribes first.
	snapNew := make(chan []byte, 32)
	g.Subscribe(sess.ID, gen2, snapNew)

	// Older connection's late Subscribe (gen1 < gen2) must be rejected.
	snapOld := make(chan []byte, 32)
	g.Subscribe(sess.ID, gen1, snapOld)

	g.subMu.RLock()
	cur, ok := g.subscribers[sess.ID]
	g.subMu.RUnlock()
	if !ok || cur.ch != snapNew || cur.gen != gen2 {
		t.Fatal("older-generation Subscribe displaced the live connection's snapshot subscription")
	}

	// Same inversion for events and trees.
	evNew := make(chan Event, 8)
	g.SubscribeEvents(sess.ID, gen2, evNew)
	evOld := make(chan Event, 8)
	g.SubscribeEvents(sess.ID, gen1, evOld)
	g.eventMu.RLock()
	curEv := g.eventSubs[sess.ID]
	g.eventMu.RUnlock()
	if curEv.ch != evNew || curEv.gen != gen2 {
		t.Fatal("older-generation SubscribeEvents displaced the live connection's subscription")
	}
	trNew := make(chan map[string][]byte, 4)
	g.SubscribeTrees(sess.ID, gen2, trNew)
	trOld := make(chan map[string][]byte, 4)
	g.SubscribeTrees(sess.ID, gen1, trOld)
	g.treeMu.RLock()
	curTr := g.treeSubs[sess.ID]
	g.treeMu.RUnlock()
	if curTr.ch != trNew || curTr.gen != gen2 {
		t.Fatal("older-generation SubscribeTrees displaced the live connection's subscription")
	}

	// The rejected old generation's ReleaseConnection must also leave the
	// live registration intact (channel identity check).
	g.ReleaseConnection(sess.ID, gen1, snapOld, evOld, trOld)
	g.subMu.RLock()
	_, still := g.subscribers[sess.ID]
	g.subMu.RUnlock()
	if !still {
		t.Fatal("rejected old generation's release removed the live subscription")
	}
}

// TestReleaseConnectionUnsubscribedSession: a handler that exits before it
// ever subscribed (e.g. maze-encode failure) must still mark the session
// disconnected for its own generation without panicking on nil channels.
func TestReleaseConnectionUnsubscribedSession(t *testing.T) {
	m := maze.Generate(42, 21, 21)
	g := NewGame(m, 8, 42)

	sess, _, gen := g.AddSession("")
	g.ReleaseConnection(sess.ID, gen, nil, nil, nil)

	g.sessMu.RLock()
	marked := !g.sessions[sess.ID].disconnectedAt.IsZero()
	g.sessMu.RUnlock()
	if !marked {
		t.Fatal("release of an unsubscribed connection must still mark the session disconnected")
	}
}
