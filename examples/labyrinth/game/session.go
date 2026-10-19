//go:build example

package game

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"math"
	mrand "math/rand"
	"strconv"
	"sync"
	"time"

	"github.com/joeycumines/go-pabt/examples/labyrinth/brain"
	"github.com/joeycumines/go-pabt/examples/labyrinth/loop"
	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
	"github.com/joeycumines/go-pabt/examples/labyrinth/net/proto"
	"github.com/joeycumines/go-pabt/examples/labyrinth/netcode"
	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

// SessionID is a unique token for a connected player.
type SessionID string

func generateSessionID() SessionID {
	var b [16]byte
	rand.Read(b[:])
	return SessionID(hex.EncodeToString(b[:]))
}

// PlayerSession is one connected player's server-side state.
type PlayerSession struct {
	ID    SessionID
	Net   *netcode.Session
	Token string

	// disconnectedAt records when the session's WebSocket closed. Zero means
	// currently connected. Sessions are retained for a grace period after
	// disconnect so the same token can rejoin without losing prediction state.
	disconnectedAt time.Time

	// connGen counts (re)connections for this session. A connection handler
	// captures the generation after AddSession and passes it to
	// ReleaseConnection on exit, so a slow-dying old socket can never tear
	// down the subscriber channels or disconnect-mark of a newer connection
	// that has already rejoined the same session (the reload race).
	connGen uint64
}

// Event is a typed game event sent to clients.
type Event struct {
	Type string      `json:"type"`
	Data interface{} `json:"data,omitempty"`
}

// Subscriber entries carry the connection generation that registered them.
// Subscribe keeps the highest-generation registration per session, so an
// older connection's late Subscribe (the double-reload inversion, where the
// old handler is delayed in WaitForRunningPhase and subscribes after the
// new one) can never overwrite the live connection's channels.
type snapSubEntry struct {
	ch  chan []byte
	gen uint64
}

type eventSubEntry struct {
	ch  chan Event
	gen uint64
}

type treeSubEntry struct {
	ch  chan map[string][]byte
	gen uint64
}

// ActorType defines distinct actor archetypes.
type ActorType int

const (
	ActorBrute   ActorType = iota // heavy melee, slow, high damage
	ActorSwarm                    // fast, weak, pack behavior
	ActorCaster                   // ranged elemental attacker
	ActorStalker                  // ambush, stealthy
)

// actorHP returns starting HP for each archetype.
func actorHP(t ActorType) int {
	switch t {
	case ActorBrute:
		return 40
	case ActorSwarm:
		return 15
	case ActorCaster:
		return 25
	case ActorStalker:
		return 20
	default:
		return 30
	}
}

// actorDamage returns contact damage per hit for each archetype.
func actorDamage(t ActorType) int {
	switch t {
	case ActorBrute:
		return 20
	case ActorSwarm:
		return 8
	case ActorCaster:
		return 12
	case ActorStalker:
		return 15
	default:
		return 10
	}
}

// I3: actorMass returns the relative mass of each archetype used for
// player-actor collision knockback ratios. Player mass is 1.0; brute actors
// are heavy (3.0), swarm actors are light (0.5), so the lighter body is
// pushed farther by the same impulse.
func actorMass(t ActorType) float64 {
	switch t {
	case ActorBrute:
		return 3.0
	case ActorSwarm:
		return 0.5
	case ActorCaster:
		return 1.0
	case ActorStalker:
		return 0.8
	default:
		return 1.0
	}
}

// RunnerMass is the player's mass for knockback ratios.
const RunnerMass = 1.0

// RunnerActorSeparation is the minimum distance between the runner and an
// actor cylinder center. When closer, both bodies are pushed apart by a
// mass-ratio impulse so the player cannot stand inside an enemy.
const RunnerActorSeparation = 0.7

// ContactKnockback is the impulse applied to both bodies on a contact
// damage event. Scaled by mass ratio so brutes barely budge and swarm
// actors fly away from the player.
const ContactKnockback = 1.5

// PulseKnockback is the impulse applied to a hit actor along the shot
// direction on a successful pulse fire. Makes hits feel physical.
const PulseKnockback = 1.2

// actorSpeed returns cells-per-tick movement speed for each archetype. At
// 30 Hz these are 3.0/4.8/2.7/4.2 cells per second: every archetype is
// slower than the player's sprint (6.5) but several beat the walk speed
// (4.0), so escape depends on sprinting, stunning, and maze knowledge.
//
// intensity is the world's Intensity level (0-5). It scales the base speed
// by (1 + 0.1*intensity), so a level-5 round makes every actor 1.5x faster.
func actorSpeed(t ActorType, intensity int) float64 {
	var base float64
	switch t {
	case ActorBrute:
		base = 0.10
	case ActorSwarm:
		base = 0.16
	case ActorCaster:
		base = 0.09
	case ActorStalker:
		base = 0.14
	default:
		base = 0.12
	}
	// Intensity scales speed: level 5 gives 1.5x base. This makes
	// escalating rounds genuinely harder, not just fog-tinted.
	return base * (1.0 + 0.1*float64(intensity))
}

// Game is the authoritative game server.
type Game struct {
	Maze  *maze.Maze
	World *world.World
	Loop  *loop.Loop

	Brains    []*brain.Brain
	Histories []*netcode.MinotaurHistory

	sessions    map[SessionID]*PlayerSession
	sessMu      sync.RWMutex
	subscribers map[SessionID]snapSubEntry
	subMu       sync.RWMutex
	eventSubs   map[SessionID]eventSubEntry
	eventMu     sync.RWMutex
	treeSubs    map[SessionID]treeSubEntry
	treeMu      sync.RWMutex

	escalationRounds []int

	// rng places actor spawns. It is only touched on the loop writer
	// goroutine (NewGame completes before the loop starts), so it needs no
	// lock. Seeding it from the maze seed makes the whole world
	// reproducible.
	rng *mrand.Rand

	// nextActorID is a monotonic actor ID counter. IDs are never reused,
	// because clients may still hold interpolation buffers for a removed
	// actor when its old ID would otherwise be handed to a respawn.
	nextActorID int

	// actorStates is parallel to World.Minotaurs (faction combat cooldowns
	// and strategy state). Only touched on the writer goroutine.
	actorStates []*ActorState
}

// NewGame creates a game with the given maze and initial actor count. The
// seed drives actor spawn placement, so a given (maze seed, actor count)
// pair always produces the same starting world.
func NewGame(m *maze.Maze, initialActors int, seed uint64) *Game {
	w := world.New(m)
	l := loop.New(w)

	g := &Game{
		Maze:             m,
		World:            w,
		Loop:             l,
		sessions:         make(map[SessionID]*PlayerSession),
		subscribers:      make(map[SessionID]snapSubEntry),
		eventSubs:        make(map[SessionID]eventSubEntry),
		treeSubs:         make(map[SessionID]treeSubEntry),
		escalationRounds: []int{300, 600, 900},
		rng:              mrand.New(mrand.NewSource(int64(seed))),
	}

	for i := 0; i < initialActors; i++ {
		g.spawnActor(ActorType(i % 4))
	}

	// Spawn elemental power-ups into the authoritative world. The runner
	// starts as Fire (the zero value of Runner.Element).
	spawnPowerUps(w, g.rng)

	// Register the tick hook so all game logic runs on the writer goroutine.
	l.SetTickHook(func(w *world.World) {
		g.tickLogic(w)
	})

	return g
}

// MazePayload returns the authoritative maze for sending to clients on connect.
func (g *Game) MazePayload() proto.MazePayload {
	p := proto.MazePayload{
		W:      g.Maze.W,
		H:      g.Maze.H,
		Grid:   make([]int, len(g.Maze.Grid)),
		StartX: g.Maze.Start.X,
		StartY: g.Maze.Start.Y,
		GateX:  g.Maze.Gate.X,
		GateY:  g.Maze.Gate.Y,
	}
	for i, c := range g.Maze.Grid {
		p.Grid[i] = int(c.Kind)
	}
	return p
}

func (g *Game) spawnActor(typ ActorType) {
	elem := actorElement(typ)
	var pos maze.Vec2
	var found bool
	// Prefer the faction's home territory so the four factions visibly hold
	// ground; fall back to an unbiased spawn if the quadrant has no room.
	if p, ok := g.homeTerritoryPos(elem); ok {
		pos, found = p, true
	}
	if !found {
		for attempts := 0; attempts < 200; attempts++ {
			cx := 1 + g.rng.Intn(g.Maze.W-2)
			cy := 1 + g.rng.Intn(g.Maze.H-2)
			if !g.Maze.At(cx, cy).Walkable() {
				continue
			}
			p := maze.Vec2{X: float64(cx) + 0.5, Y: float64(cy) + 0.5}
			if distSq(p, g.Maze.Start) < 81 || distSq(p, g.Maze.Gate) < 81 {
				continue
			}
			pos, found = p, true
			break
		}
	}
	if !found {
		return
	}

	g.nextActorID++
	id := actorID(typ, g.nextActorID)
	minotaur := &world.Minotaur{
		ID:      id,
		Pos:     pos,
		Type:    int(typ),
		Element: int(elem),
		HP:      actorHP(typ),
	}

	b, err := brain.NewBrain(id, g.Maze, &minotaur.Pos)
	if err != nil {
		// Do not add the minotaur without its brain: Brains and Histories
		// are indexed against Minotaurs and must stay aligned.
		return
	}
	g.World.Minotaurs = append(g.World.Minotaurs, minotaur)
	b.SetMoveSpeed(actorSpeed(typ, g.World.Intensity))
	g.Brains = append(g.Brains, b)
	g.Histories = append(g.Histories, netcode.NewMinotaurHistory())
	g.actorStates = append(g.actorStates, &ActorState{StrategyTimer: StrategyReevalTicks})
}

func actorID(typ ActorType, n int) string {
	prefixes := [4]string{"B", "S", "C", "K"}
	return prefixes[typ%4] + "-" + strconv.Itoa(n)
}

func distSq(a, b maze.Vec2) float64 {
	dx := a.X - b.X
	dy := a.Y - b.Y
	return dx*dx + dy*dy
}

// tickLogic runs on the single-writer goroutine every tick. It drives stun
// timers, actor AI, contact damage, win/lose detection, and escalation.
func (g *Game) tickLogic(w *world.World) {
	if w.Phase != world.PhaseRunning {
		return
	}

	// Gang war ecosystem: inter-faction combat, rule-based strategies, and
	// crossfire hazards. All on the writer goroutine.
	g.factionCombat(w)
	g.tickStrategies(w)
	g.tickHazards(w)

	// Decrement stun and per-actor cooldown timers.
	g.tickTimers(w)

	// Update brains and move actors.
	for i, b := range g.Brains {
		if i >= len(w.Minotaurs) {
			break
		}
		m := w.Minotaurs[i]
		if m.HP <= 0 {
			continue
		}
		// Refresh brain move speed from current Intensity so escalation
		// takes effect without waiting for a respawn.
		b.SetMoveSpeed(actorSpeed(ActorType(m.Type), w.Intensity))
		snap := w.Copy()
		b.UpdateWorldView(snap, m)
		b.Tick("tick")

		// Record position for lag compensation.
		if i < len(g.Histories) {
			g.Histories[i].Record(w.Round, m.Pos)
		}
	}

	// I3: contact damage + mass-ratio knockback between runner and actors.
	// Runs outside the brain loop so it fires regardless of brain state.
	g.tickContactDamage(w)

	// Broadcast tree frames every 10 ticks (~3Hz) for the debug panel.
	if w.Round%10 == 0 {
		g.BroadcastTrees(g.TreeFrames(w))
	}

	// Strategy-driven movement for actors whose brain is not pursuing.
	g.applyStrategyMovement(w)

	// Physical separation: actors are bodies, not ghosts.
	g.separateActors(w)

	// I3: separate runner from actors (mass-ratio knockback) and sync
	// runner position back to the authoritative netcode session so the
	// server-side reconciliation stays coherent with the knockback.
	g.separateRunnerFromActors(w)

	// Check lose condition.
	if w.Runner.HP <= 0 {
		w.Phase = world.PhaseLost
		g.broadcastEvent(Event{Type: "caught"})
		return
	}

	// Check win condition.
	dx := w.Runner.Pos.X - g.Maze.Gate.X
	dy := w.Runner.Pos.Y - g.Maze.Gate.Y
	if dx*dx+dy*dy < 0.5*0.5 {
		w.Phase = world.PhaseWon
		g.broadcastEvent(Event{Type: "win"})
		return
	}

	// Check power-up pickup.
	if pu := checkPowerUpPickup(w); pu != nil {
		g.broadcastEvent(Event{
			Type: "powerup",
			Data: map[string]interface{}{
				"element": pu.Element,
				"name":    elementName(Element(pu.Element)),
			},
		})
	}

	// Escalation schedule.
	for _, round := range g.escalationRounds {
		if w.Round == round {
			g.spawnActor(ActorType(w.Round / 300 % 4))
			g.broadcastEvent(Event{
				Type: "spawn",
				Data: map[string]interface{}{
					"count":     len(w.Minotaurs),
					"intensity": w.Intensity,
				},
			})
		}
	}

	// Remove actors killed by the gang war (or the player) and respawn to
	// hold the roster; then refresh territory control so every snapshot
	// carries the current front line.
	g.respawnDeadActors(w)
	w.TerritoryControl = territoryControl(w)
}

// tickTimers decrements stun and per-actor cooldown timers for the roster.
// Every timer that gates repeated behavior decays here, on the writer
// goroutine, so none can latch closed forever.
func (g *Game) tickTimers(w *world.World) {
	for i, m := range w.Minotaurs {
		if m.Stunned {
			m.StunTicks--
			if m.StunTicks <= 0 {
				m.Stunned = false
				m.StunTicks = 0
			}
		}
		if m.AttackCooldown > 0 {
			m.AttackCooldown--
		}
		if i < len(g.actorStates) && g.actorStates[i].FactionAttackCooldown > 0 {
			g.actorStates[i].FactionAttackCooldown--
		}
	}
}

// AddSession registers a new player session. If token is non-empty and
// matches a retained session, that session is reactivated (rejoin) instead
// of creating a fresh one, preserving prediction state.
//
// The token itself is the session ID, so a reconnect presenting the same
// token deterministically finds the same session even if the prior
// connection's disconnect (RemoveSession, driven by the read pump observing
// the close) has not landed yet - the browser closes the old WebSocket and
// opens the new one during a page reload, and the two events race.
//
// When the world is in a terminal phase (won or lost), AddSession submits a
// reset command so the rejoined or fresh session starts a new game. Without
// this, a page reload after win/lose rejoins the terminal world and the
// client's terminal-phase fallback instantly re-fires the win/lose overlay.
func (g *Game) AddSession(token string) (*PlayerSession, string, uint64) {
	g.sessMu.Lock()
	defer g.sessMu.Unlock()

	// Attempt rejoin: the token IS the session ID, so a matching session is
	// the same session regardless of whether it is currently connected or
	// retained-disconnected. This closes the reload race where the old
	// socket's close had not yet been observed when the new socket arrives.
	if token != "" {
		if sess, ok := g.sessions[SessionID(token)]; ok {
			sess.disconnectedAt = time.Time{} // mark (re)connected
			sess.connGen++                    // supersede any dying connection
			sess.Net.ResetAckSeq()            // client restarts at seq 1
			g.submitWorldResetIfTerminal(sess)
			return sess, sess.Token, sess.connGen
		}
	}

	// Fresh session. The ID doubles as the token the client will present on
	// reconnect, so it must be unguessable (crypto/rand) - not derived from
	// any guessable counter.
	id := generateSessionID()
	tok := string(id)
	sess := &PlayerSession{
		ID:      id,
		Net:     netcode.NewSession(tok, g.Maze.Start, 0),
		Token:   tok,
		connGen: 1,
	}
	g.sessions[id] = sess
	g.submitWorldResetIfTerminal(sess)
	return sess, tok, sess.connGen
}

// ReleaseConnection tears down the per-connection state owned by a handler
// that is exiting. It is safe under the reload race: the browser closes the
// old WebSocket and opens a new one (rejoining the same session by token),
// and the old socket's close can be observed AFTER the new socket has
// already subscribed. In that case the old handler must not delete the new
// connection's subscriber channels (which would silently stop all
// snapshots for the live client) nor mark the reconnected session
// disconnected (which would let CleanupSessions garbage-collect it).
//
// gen must be the value AddSession returned to THIS handler; the channels
// must be the ones THIS handler created and subscribed.
func (g *Game) ReleaseConnection(id SessionID, gen uint64, snapCh chan []byte, eventCh chan Event, treeCh chan map[string][]byte) {
	g.sessMu.Lock()
	if sess, ok := g.sessions[id]; ok && sess.connGen == gen {
		sess.disconnectedAt = time.Now()
	}
	g.sessMu.Unlock()
	// Channel identity scopes the deletes: a rejoin replaces the map
	// entries with the new connection's channels, so the old connection's
	// compare fails and the live subscription survives.
	g.subMu.Lock()
	if cur, ok := g.subscribers[id]; ok && cur.ch == snapCh {
		delete(g.subscribers, id)
	}
	g.subMu.Unlock()
	g.eventMu.Lock()
	if cur, ok := g.eventSubs[id]; ok && cur.ch == eventCh {
		delete(g.eventSubs, id)
	}
	g.eventMu.Unlock()
	g.treeMu.Lock()
	if cur, ok := g.treeSubs[id]; ok && cur.ch == treeCh {
		delete(g.treeSubs, id)
	}
	g.treeMu.Unlock()
}

// submitWorldResetIfTerminal queues a loop command that resets the world to
// a fresh running game if the phase is terminal when the command runs. The
// phase is read only on the writer goroutine (the sole writer of
// World.Phase), so AddSession stays race-free. The command is a cheap no-op
// while the world is already running.
func (g *Game) submitWorldResetIfTerminal(sess *PlayerSession) {
	g.Loop.Submit(func(w *world.World) {
		if w.Phase == world.PhaseRunning {
			return // already reset by a concurrent session
		}
		w.Phase = world.PhaseRunning
		w.Round = 0
		w.Intensity = 0
		w.Runner.Pos = g.Maze.Start
		w.Runner.Vel = maze.Vec2{}
		w.Runner.HP = world.MaxHP
		w.Runner.Element = 0
		w.Runner.Grounded = true
		// Rebuild the actor roster from scratch: fresh HP, no stun, full
		// faction balance. Clear brains/histories/states so spawnActor
		// rebuilds them aligned with the new Minotaurs slice.
		w.Minotaurs = nil
		g.Brains = nil
		g.Histories = nil
		g.actorStates = nil
		for i := 0; i < TargetActorCount; i++ {
			g.spawnActor(ActorType(i % 4))
		}
		// Reset hazards and re-spawn power-ups.
		w.Hazards = nil
		w.PowerUps = nil
		spawnPowerUps(w, g.rng)
		// Sync the session's authoritative position so the client's
		// prediction reconciles to the spawn point.
		if sess != nil && sess.Net != nil {
			sess.Net.Pos = g.Maze.Start
		}
	})
}

// WaitForRunningPhase blocks until the loop snapshot reports a running world
// or the timeout expires, returning whether the world is running. The
// connect path calls this after AddSession: when the world was terminal,
// AddSession queued a reset, and waiting for it to land ensures the new
// subscriber's first snapshot is the fresh game rather than a stale terminal
// phase that the client's terminal-phase fallback would turn into an instant
// game-over overlay.
func (g *Game) WaitForRunningPhase(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if snap := g.Loop.Snapshot(); snap != nil && snap.Phase == world.PhaseRunning {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// RemoveSession marks a player session as disconnected and drops ALL of its
// subscriber registrations unconditionally. It is TEST-ONLY: production
// connection handlers must use ReleaseConnection, which scopes both the
// disconnect-mark and the subscription deletes to their own connection
// generation and channels. RemoveSession is unsafe when a session may have
// a newer live connection (the reload race), because it would delete that
// connection's subscriptions.
func (g *Game) RemoveSession(id SessionID) {
	g.sessMu.Lock()
	if sess, ok := g.sessions[id]; ok {
		sess.disconnectedAt = time.Now()
	}
	g.sessMu.Unlock()
	g.subMu.Lock()
	delete(g.subscribers, id)
	g.subMu.Unlock()
	g.eventMu.Lock()
	delete(g.eventSubs, id)
	g.eventMu.Unlock()
	g.treeMu.Lock()
	delete(g.treeSubs, id)
	g.treeMu.Unlock()
}

// sessionGracePeriod is how long a disconnected session is retained before
// being garbage-collected.
const sessionGracePeriod = 60 * time.Second

// CleanupSessions removes sessions that have been disconnected longer than
// the grace period. Call periodically (e.g. from a ticker goroutine).
func (g *Game) CleanupSessions() {
	now := time.Now()
	g.sessMu.Lock()
	for id, sess := range g.sessions {
		if !sess.disconnectedAt.IsZero() && now.Sub(sess.disconnectedAt) > sessionGracePeriod {
			delete(g.sessions, id)
		}
	}
	g.sessMu.Unlock()
}

// SubscribeTrees registers a channel to receive tree frames. gen is the
// connection generation from AddSession; a registration from an older
// generation than the current entry is rejected so a dying connection can
// never displace the live one (double-reload inversion).
func (g *Game) SubscribeTrees(id SessionID, gen uint64, ch chan map[string][]byte) {
	g.treeMu.Lock()
	if cur, ok := g.treeSubs[id]; ok && cur.gen > gen {
		g.treeMu.Unlock()
		return
	}
	g.treeSubs[id] = treeSubEntry{ch: ch, gen: gen}
	g.treeMu.Unlock()
}

// BroadcastTrees sends tree frames to all tree subscribers. Must be called
// from the writer goroutine only.
func (g *Game) BroadcastTrees(frames map[string][]byte) {
	g.treeMu.RLock()
	defer g.treeMu.RUnlock()
	for _, e := range g.treeSubs {
		select {
		case e.ch <- frames:
		default:
		}
	}
}

// Subscribe registers a channel to receive encoded snapshots. gen is the
// connection generation from AddSession; a registration from an older
// generation than the current entry is rejected so a dying connection can
// never displace the live one (double-reload inversion).
func (g *Game) Subscribe(id SessionID, gen uint64, ch chan []byte) {
	g.subMu.Lock()
	if cur, ok := g.subscribers[id]; ok && cur.gen > gen {
		g.subMu.Unlock()
		return
	}
	g.subscribers[id] = snapSubEntry{ch: ch, gen: gen}
	g.subMu.Unlock()
}

// SubscribeEvents registers a channel to receive game events. gen is the
// connection generation from AddSession; a registration from an older
// generation than the current entry is rejected so a dying connection can
// never displace the live one (double-reload inversion).
func (g *Game) SubscribeEvents(id SessionID, gen uint64, ch chan Event) {
	g.eventMu.Lock()
	if cur, ok := g.eventSubs[id]; ok && cur.gen > gen {
		g.eventMu.Unlock()
		return
	}
	g.eventSubs[id] = eventSubEntry{ch: ch, gen: gen}
	g.eventMu.Unlock()
}

// ApplyInput processes a player input command via the authoritative loop.
func (g *Game) ApplyInput(id SessionID, cmd netcode.InputCmd) {
	g.sessMu.RLock()
	sess, ok := g.sessions[id]
	g.sessMu.RUnlock()
	if !ok {
		return
	}

	g.Loop.Submit(func(w *world.World) {
		sess.Net.ApplyInput(g.Maze, cmd, loop.TickInterval.Seconds())
		w.Runner.Pos = sess.Net.Pos
		w.Runner.Yaw = sess.Net.Yaw
		w.Runner.Pitch = sess.Net.Pitch
	})
}

// tickContactDamage applies contact damage and mass-ratio knockback between
// the runner and every non-stunned actor within 0.7u. Pulled out of the
// brain loop so separation/damage applies to the world state regardless of
// brain iteration state (keeps the logic testable without a live brain).
func (g *Game) tickContactDamage(w *world.World) {
	for _, m := range w.Minotaurs {
		if m.HP <= 0 || m.Stunned || m.AttackCooldown > 0 {
			continue
		}
		dx := m.Pos.X - w.Runner.Pos.X
		dy := m.Pos.Y - w.Runner.Pos.Y
		if dx*dx+dy*dy >= 0.7*0.7 {
			continue
		}
		dmg := actorDamage(ActorType(m.Type))
		w.Runner.HP -= dmg
		m.AttackCooldown = 30 // 1 second cooldown at 30Hz
		// I3: knockback on contact — both bodies pushed apart by the
		// mass-ratio impulse. Runner flies away from the actor; the actor
		// is nudged in the opposite direction.
		d := math.Hypot(dx, dy)
		if d < 1e-9 {
			d = 1e-9
			dx, dy = 1e-9, 0
		}
		ux, uy := dx/d, dy/d
		aMass := actorMass(ActorType(m.Type))
		totalMass := RunnerMass + aMass
		runnerPush := ContactKnockback * (aMass / totalMass)
		w.Runner.Pos.X -= ux * runnerPush
		w.Runner.Pos.Y -= uy * runnerPush
		actorPush := ContactKnockback * (RunnerMass / totalMass)
		m.Pos.X += ux * actorPush
		m.Pos.Y += uy * actorPush
		// Resolve both against the maze so knockback never pushes either
		// body into a wall. Without this, contact near a corridor edge
		// shoves entities through solid geometry.
		w.Runner.Pos = g.Maze.Resolve(w.Runner.Pos, maze.Radius)
		m.Pos = g.Maze.Resolve(m.Pos, maze.Radius)
		g.broadcastEvent(Event{
			Type: "hit",
			Data: map[string]interface{}{
				"damage":    dmg,
				"hp":        w.Runner.HP,
				"knockback": true,
			},
		})
	}
}

// PulseBaseDamage is the damage a neutral pulse deals. Elemental advantage
// (attacker beats defender in the cycle) doubles it; disadvantage halves it.
const PulseBaseDamage = 15

// FirePulse processes a pulse fire with lag-compensated hit validation.
// Damage scales by elemental advantage between the runner's active element
// and the struck actor's innate element.
func (g *Game) FirePulse(id SessionID, fireRound int, aim netcode.RayHitOrigin) {
	g.Loop.Submit(func(w *world.World) {
		bestIdx := -1
		bestRange := math.MaxFloat64
		for i, hist := range g.Histories {
			if i >= len(w.Minotaurs) {
				break
			}
			if w.Minotaurs[i].HP <= 0 {
				continue
			}
			hit, ok := netcode.ValidateRayHit(hist, fireRound, aim)
			if !ok {
				continue
			}
			// Occlusion: a wall cell between the shooter and the actor
			// along the ray blocks the pulse, matching the client's own
			// visibility rules (walls render opaque, so the crosshair cannot
			// legitimately rest on an occluded actor).
			if g.rayOccluded(aim.Pos, hit.Range, aim.Yaw, aim.Pitch) {
				continue
			}
			if hit.Range < bestRange {
				bestRange = hit.Range
				bestIdx = i
			}
		}
		if bestIdx < 0 {
			return
		}
		m := w.Minotaurs[bestIdx]
		mult := advantage(Element(w.Runner.Element), Element(m.Element))
		dmg := int(float64(PulseBaseDamage) * mult)
		m.HP -= dmg
		m.Stunned = true
		m.StunTicks = world.StunDuration
		// I3: pulse knockback — hit actor pushed along the shot direction.
		// Heavier actors resist more (mass ratio). Makes shots feel physical.
		g.applyPulseKnockback(m, aim)
		g.broadcastEvent(Event{
			Type: "stunned",
			Data: map[string]interface{}{
				"minotaur":  m.ID,
				"hp":        m.HP,
				"damage":    dmg,
				"advantage": mult,
				"pos":       []float64{m.Pos.X, m.Pos.Y},
				"element":   m.Element,
			},
		})
	})
}

// applyPulseKnockback pushes a hit actor along the shot direction by a
// mass-ratio impulse. Extracted from FirePulse so unit tests can exercise
// the knockback math without a live loop.
func (g *Game) applyPulseKnockback(m *world.Minotaur, aim netcode.RayHitOrigin) {
	fx := math.Sin(aim.Yaw) * math.Cos(aim.Pitch)
	fy := math.Cos(aim.Yaw) * math.Cos(aim.Pitch)
	aMass := actorMass(ActorType(m.Type))
	kick := PulseKnockback * (RunnerMass / (RunnerMass + aMass))
	m.Pos.X += fx * kick
	m.Pos.Y += fy * kick
	m.Pos = g.Maze.Resolve(m.Pos, maze.Radius)
}

// rayOccluded samples the maze grid along a ray from origin toward
// (yaw, pitch) for up to range units and reports whether any solid cell
// lies between the shooter and a target at that range. The sampling
// resolution is sub-cell (0.25 units) so a thin wall sliver cannot slip
// between samples. Only the horizontal (maze-plane) projection is traced,
// matching how the maze is authored and rendered (walls are full-height
// columns, not floating obstacles).
func (g *Game) rayOccluded(origin maze.Vec2, rangeAlong, yaw, pitch float64) bool {
	cp := math.Cos(pitch)
	if cp < 1e-6 {
		return false // degenerate near-vertical aim; nothing to occlude against in-plane
	}
	fx := math.Sin(yaw) * cp
	fy := math.Cos(yaw) * cp
	const step = 0.25
	for d := step; d < rangeAlong; d += step {
		x := origin.X + fx*d
		y := origin.Y + fy*d
		cx := int(math.Floor(x))
		cy := int(math.Floor(y))
		if !g.Maze.At(cx, cy).Walkable() {
			return true
		}
	}
	return false
}

// DebugTeleport relocates the runner authoritatively, for the integrated
// debugging tooling and end-to-end verification. It runs on the writer
// goroutine and resolves the destination against the maze so a bad
// coordinate can never push the runner into a wall.
func (g *Game) DebugTeleport(id SessionID, pos maze.Vec2) {
	g.sessMu.RLock()
	sess, ok := g.sessions[id]
	g.sessMu.RUnlock()
	if !ok {
		return
	}
	g.Loop.Submit(func(w *world.World) {
		p := g.Maze.Resolve(pos, maze.Radius)
		sess.Net.Pos = p
		w.Runner.Pos = p
	})
}

func (g *Game) broadcastEvent(evt Event) {
	g.eventMu.RLock()
	defer g.eventMu.RUnlock()
	for _, e := range g.eventSubs {
		select {
		case e.ch <- evt:
		default:
		}
	}
}

// DebugReplan forces a specific actor's brain to rebuild its plan.
func (g *Game) DebugReplan(actorID string) {
	g.Loop.Submit(func(w *world.World) {
		for i, m := range w.Minotaurs {
			if m.ID == actorID && i < len(g.Brains) {
				_ = g.Brains[i].Replan()
				return
			}
		}
	})
}

// DebugPause freezes a specific actor's brain decisions.
func (g *Game) DebugPause(actorID string) {
	g.Loop.Submit(func(w *world.World) {
		for i, m := range w.Minotaurs {
			if m.ID == actorID && i < len(g.Brains) {
				g.Brains[i].Pause()
				return
			}
		}
	})
}

// DebugResume unfreezes a specific actor's brain.
func (g *Game) DebugResume(actorID string) {
	g.Loop.Submit(func(w *world.World) {
		for i, m := range w.Minotaurs {
			if m.ID == actorID && i < len(g.Brains) {
				g.Brains[i].Resume()
				return
			}
		}
	})
}

// DebugInject redirects a specific actor's world view to a position.
func (g *Game) DebugInject(actorID string, pos maze.Vec2) {
	g.Loop.Submit(func(w *world.World) {
		for i, m := range w.Minotaurs {
			if m.ID == actorID && i < len(g.Brains) {
				g.Brains[i].InjectWorldView(g.Maze.Resolve(pos, maze.Radius))
				return
			}
		}
	})
}

// TreeFrames returns the latest PA-BT frames for all actors. Called from
// the writer goroutine via BroadcastTrees.
func (g *Game) TreeFrames(w *world.World) map[string][]byte {
	out := make(map[string][]byte)
	for i, m := range w.Minotaurs {
		if i < len(g.Brains) {
			frame := g.Brains[i].LastFrame()
			if frame != nil {
				data, err := json.Marshal(frame)
				if err == nil {
					out[m.ID] = data
				}
			}
		}
	}
	return out
}

// SnapshotFanOut sends encoded snapshots to all subscribers at 20Hz.
func (g *Game) SnapshotFanOut(encode func(snap *world.Snapshot, ackSeq uint64) ([]byte, error)) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		snap := g.Loop.Snapshot()
		if snap == nil {
			continue
		}

		g.subMu.RLock()
		for id, e := range g.subscribers {
			g.sessMu.RLock()
			sess, ok := g.sessions[id]
			g.sessMu.RUnlock()
			if !ok {
				continue
			}

			data, err := encode(snap, sess.Net.AckSeq())
			if err != nil {
				continue
			}
			select {
			case e.ch <- data:
			default:
			}
		}
		g.subMu.RUnlock()
	}
}
