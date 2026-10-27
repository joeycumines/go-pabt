//go:build example

package game

import "github.com/joeycumines/go-pabt/examples/labyrinth/maze"

// Presence pause: the authoritative world does not advance while nobody is
// actually playing. A browser tab can sit on the tutorial or the click-to-
// enter prompt indefinitely; without this gate the faction war, escalation
// timer and contact damage run behind the menu, so the player enters an
// already-lost game. The gate is opt-in so headless engine tests and legacy
// harnesses (which never take pointer lock) keep their old behaviour.

// Options configures optional server behaviour. The zero value matches the
// historical NewGame behaviour.
type Options struct {
	// PauseWithoutPlayer freezes world.Step and the game tick hook while no
	// connected session has declared itself playing.
	PauseWithoutPlayer bool
}

// NewGameWithOptions is NewGame plus optional presence gating.
func NewGameWithOptions(m *maze.Maze, initialActors int, seed uint64, opts Options) *Game {
	g := NewGame(m, initialActors, seed)
	g.pauseWithoutPlayer = opts.PauseWithoutPlayer
	if opts.PauseWithoutPlayer {
		g.Loop.SetGate(g.anyPlaying)
	}
	return g
}

// SetPlaying records whether a connected session is actually in the game.
// Unknown or already-removed sessions are ignored: a stale "play on" from a
// closed tab must not restart the world by itself.
func (g *Game) SetPlaying(id SessionID, on bool) {
	g.sessMu.RLock()
	defer g.sessMu.RUnlock()
	if _, ok := g.sessions[id]; !ok {
		return
	}
	g.setPlayingLocked(id, on)
}

// setPlayingLocked updates the flag. The caller must hold g.sessMu (read or
// write) so the session cannot be removed between the membership check and
// the update. Lock order is sessMu -> playMu everywhere.
func (g *Game) setPlayingLocked(id SessionID, on bool) {
	g.playMu.Lock()
	if on {
		g.playing[id] = true
	} else {
		delete(g.playing, id)
	}
	g.playMu.Unlock()
}

// clearPlaying forgets a session without checking whether it still exists.
// Used by teardown paths that already removed the session.
func (g *Game) clearPlaying(id SessionID) {
	g.playMu.Lock()
	delete(g.playing, id)
	g.playMu.Unlock()
}

// anyPlaying is the loop gate: true while at least one present session has
// declared itself playing.
func (g *Game) anyPlaying() bool {
	g.playMu.RLock()
	defer g.playMu.RUnlock()
	return len(g.playing) > 0
}
