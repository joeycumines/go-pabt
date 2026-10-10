//go:build example

package game

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
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
}

// Event is a typed game event sent to clients.
type Event struct {
	Type string      `json:"type"`
	Data interface{} `json:"data,omitempty"`
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

// actorSpeed returns cells-per-tick movement speed for each archetype. At
// 30 Hz these are 3.0/4.8/2.7/4.2 cells per second: every archetype is
// slower than the player's sprint (6.5) but several beat the walk speed
// (4.0), so escape depends on sprinting, stunning, and maze knowledge.
func actorSpeed(t ActorType) float64 {
	switch t {
	case ActorBrute:
		return 0.10
	case ActorSwarm:
		return 0.16
	case ActorCaster:
		return 0.09
	case ActorStalker:
		return 0.14
	default:
		return 0.12
	}
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
	subscribers map[SessionID]chan []byte
	subMu       sync.RWMutex
	eventSubs   map[SessionID]chan Event
	eventMu     sync.RWMutex
	treeSubs    map[SessionID]chan map[string][]byte
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
		subscribers:      make(map[SessionID]chan []byte),
		eventSubs:        make(map[SessionID]chan Event),
		treeSubs:         make(map[SessionID]chan map[string][]byte),
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
	b.SetMoveSpeed(actorSpeed(typ))
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
		snap := w.Copy()
		b.UpdateWorldView(snap, m)
		b.Tick("tick")

		// Record position for lag compensation.
		if i < len(g.Histories) {
			g.Histories[i].Record(w.Round, m.Pos)
		}

		// Contact damage: if not stunned and close enough, hurt the runner.
		if !m.Stunned && m.AttackCooldown <= 0 {
			dx := m.Pos.X - w.Runner.Pos.X
			dy := m.Pos.Y - w.Runner.Pos.Y
			if dx*dx+dy*dy < 0.7*0.7 {
				dmg := actorDamage(ActorType(m.Type))
				w.Runner.HP -= dmg
				m.AttackCooldown = 30 // 1 second cooldown at 30Hz
				g.broadcastEvent(Event{
					Type: "hit",
					Data: map[string]interface{}{
						"damage": dmg,
						"hp":     w.Runner.HP,
					},
				})
			}
		}
	}

	// Broadcast tree frames every 10 ticks (~3Hz) for the debug panel.
	if w.Round%10 == 0 {
		g.BroadcastTrees(g.TreeFrames(w))
	}

	// Strategy-driven movement for actors whose brain is not pursuing.
	g.applyStrategyMovement(w)

	// Physical separation: actors are bodies, not ghosts.
	g.separateActors(w)

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
				Data: map[string]interface{}{"count": len(w.Minotaurs)},
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
// matches a recently-disconnected session, that session is reactivated
// (rejoin) instead of creating a fresh one, preserving prediction state.
//
// When the world is in a terminal phase (won or lost), AddSession submits a
// reset command so the rejoined or fresh session starts a new game. Without
// this, a page reload after win/lose rejoins the terminal world and the
// client's terminal-phase fallback instantly re-fires the win/lose overlay.
func (g *Game) AddSession(token string) (*PlayerSession, string) {
	g.sessMu.Lock()
	defer g.sessMu.Unlock()

	// Attempt rejoin: find a disconnected session with this token.
	if token != "" {
		for _, sess := range g.sessions {
			if sess.Token == token && !sess.disconnectedAt.IsZero() {
				sess.disconnectedAt = time.Time{} // mark reconnected
				sess.Net.ResetAckSeq()            // client restarts at seq 1
				g.submitWorldResetIfTerminal(sess)
				return sess, sess.Token
			}
		}
	}

	// Fresh session.
	id := generateSessionID()
	tok := string(id)
	sess := &PlayerSession{
		ID:    id,
		Net:   netcode.NewSession(tok, g.Maze.Start, 0),
		Token: tok,
	}
	g.sessions[id] = sess
	g.submitWorldResetIfTerminal(sess)
	return sess, tok
}

// submitWorldResetIfTerminal submits a loop command that resets the world to
// a fresh running game when the phase is terminal. The command runs on the
// writer goroutine at the next tick boundary, so it is safe to call from any
// goroutine.
func (g *Game) submitWorldResetIfTerminal(sess *PlayerSession) {
	if g.World.Phase == world.PhaseRunning {
		return
	}
	g.Loop.Submit(func(w *world.World) {
		if w.Phase == world.PhaseRunning {
			return // already reset by a concurrent session
		}
		w.Phase = world.PhaseRunning
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

// RemoveSession marks a player session as disconnected. The session is
// retained for a grace period so the same token can rejoin. Subscriptions
// are removed immediately (the socket is gone).
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

// SubscribeTrees registers a channel to receive tree frames.
func (g *Game) SubscribeTrees(id SessionID, ch chan map[string][]byte) {
	g.treeMu.Lock()
	g.treeSubs[id] = ch
	g.treeMu.Unlock()
}

// BroadcastTrees sends tree frames to all tree subscribers. Must be called
// from the writer goroutine only.
func (g *Game) BroadcastTrees(frames map[string][]byte) {
	g.treeMu.RLock()
	defer g.treeMu.RUnlock()
	for _, ch := range g.treeSubs {
		select {
		case ch <- frames:
		default:
		}
	}
}

// Subscribe registers a channel to receive encoded snapshots.
func (g *Game) Subscribe(id SessionID, ch chan []byte) {
	g.subMu.Lock()
	g.subscribers[id] = ch
	g.subMu.Unlock()
}

// SubscribeEvents registers a channel to receive game events.
func (g *Game) SubscribeEvents(id SessionID, ch chan Event) {
	g.eventMu.Lock()
	g.eventSubs[id] = ch
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
	})
}

// PulseBaseDamage is the damage a neutral pulse deals. Elemental advantage
// (attacker beats defender in the cycle) doubles it; disadvantage halves it.
const PulseBaseDamage = 15

// FirePulse processes a pulse fire with lag-compensated hit validation.
// Damage scales by elemental advantage between the runner's active element
// and the struck actor's innate element.
func (g *Game) FirePulse(id SessionID, fireRound int, aimPos maze.Vec2) {
	g.Loop.Submit(func(w *world.World) {
		for i, hist := range g.Histories {
			if i >= len(w.Minotaurs) {
				break
			}
			if w.Minotaurs[i].HP <= 0 {
				continue
			}
			if netcode.ValidateHit(hist, fireRound, aimPos) {
				m := w.Minotaurs[i]
				mult := advantage(Element(w.Runner.Element), Element(m.Element))
				dmg := int(float64(PulseBaseDamage) * mult)
				m.HP -= dmg
				m.Stunned = true
				m.StunTicks = world.StunDuration
				g.broadcastEvent(Event{
					Type: "stunned",
					Data: map[string]interface{}{
						"minotaur":  m.ID,
						"hp":        m.HP,
						"damage":    dmg,
						"advantage": mult,
					},
				})
				return
			}
		}
	})
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
	for _, ch := range g.eventSubs {
		select {
		case ch <- evt:
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
		for id, ch := range g.subscribers {
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
			case ch <- data:
			default:
			}
		}
		g.subMu.RUnlock()
	}
}
