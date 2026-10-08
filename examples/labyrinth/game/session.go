//go:build example

package game

import (
	"crypto/rand"
	"encoding/hex"
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

	escalationRounds []int

	// rng places actor spawns. It is only touched on the loop writer
	// goroutine (NewGame completes before the loop starts), so it needs no
	// lock. Seeding it from the maze seed makes the whole world
	// reproducible.
	rng *mrand.Rand
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

	id := actorID(typ, len(g.World.Minotaurs))
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
}

func actorID(typ ActorType, idx int) string {
	prefixes := [4]string{"B", "S", "C", "K"}
	return prefixes[typ%4] + "-" + strconv.Itoa(idx+1)
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

	// Decrement stun timers.
	for _, m := range w.Minotaurs {
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
	}

	// Update brains and move actors.
	for i, b := range g.Brains {
		if i >= len(w.Minotaurs) {
			break
		}
		m := w.Minotaurs[i]
		snap := w.Copy()
		b.UpdateBelief(snap, m)
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
}

// AddSession registers a new player session.
func (g *Game) AddSession() (*PlayerSession, string) {
	g.sessMu.Lock()
	defer g.sessMu.Unlock()

	id := generateSessionID()
	token := string(id)
	sess := &PlayerSession{
		ID:    id,
		Net:   netcode.NewSession(token, g.Maze.Start, 0),
		Token: token,
	}
	g.sessions[id] = sess
	return sess, token
}

// RemoveSession unregisters a player session.
func (g *Game) RemoveSession(id SessionID) {
	g.sessMu.Lock()
	delete(g.sessions, id)
	g.sessMu.Unlock()
	g.subMu.Lock()
	delete(g.subscribers, id)
	g.subMu.Unlock()
	g.eventMu.Lock()
	delete(g.eventSubs, id)
	g.eventMu.Unlock()
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
