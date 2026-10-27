//go:build example

package game

import (
	"encoding/json"
	"math"
	"sync"
	"time"

	"github.com/joeycumines/go-pabt/examples/labyrinth/brain"
	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
	"github.com/joeycumines/go-pabt/examples/labyrinth/netcode"
	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

// MaxTelemetryTicks caps the rolling tick history retained when telemetry is enabled.
const MaxTelemetryTicks = 3600

// ActorTickRecord attributes one actor's displacement and brain state on a single server tick.
type ActorTickRecord struct {
	Round           int                 `json:"round"`
	ActorID         string              `json:"actorId"`
	Type            int                 `json:"type"`
	Element         int                 `json:"element"`
	Strategy        int                 `json:"strategy"`
	HP              int                 `json:"hp"`
	Stunned         bool                `json:"stunned"`
	StunTicks       int                 `json:"stunTicks"`
	StartPos        maze.Vec2           `json:"startPos"`
	EndPos          maze.Vec2           `json:"endPos"`
	BrainDelta      float64             `json:"brainDelta"`
	StrategyDelta   float64             `json:"strategyDelta"`
	SeparationDelta float64             `json:"separationDelta"`
	KnockbackDelta  float64             `json:"knockbackDelta"`
	SpawnedThisTick bool                `json:"spawnedThisTick"`
	KilledByPlayer  bool                `json:"killedByPlayer"`
	KilledByFaction bool                `json:"killedByFaction"`
	Brain           brain.TickTelemetry `json:"brain"`
}

// WorldTickRecord captures the runner and all actors on one authoritative tick.
type WorldTickRecord struct {
	Round            int               `json:"round"`
	Phase            int               `json:"phase"`
	Intensity        int               `json:"intensity"`
	RunnerPos        maze.Vec2         `json:"runnerPos"`
	RunnerHP         int               `json:"runnerHp"`
	RunnerElement    int               `json:"runnerElement"`
	TerritoryControl [4]int            `json:"territoryControl"`
	Actors           []ActorTickRecord `json:"actors"`
}

// telemetryLog is a thread-safe rolling buffer of WorldTickRecord entries.
type telemetryLog struct {
	mu      sync.RWMutex
	records []WorldTickRecord
}

func (tl *telemetryLog) append(rec WorldTickRecord) {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	tl.records = append(tl.records, rec)
	if len(tl.records) > MaxTelemetryTicks {
		tl.records = tl.records[len(tl.records)-MaxTelemetryTicks:]
	}
}

func (tl *telemetryLog) snapshot() []WorldTickRecord {
	tl.mu.RLock()
	defer tl.mu.RUnlock()
	out := make([]WorldTickRecord, len(tl.records))
	for i := range tl.records {
		out[i] = tl.records[i]
		out[i].Actors = append([]ActorTickRecord(nil), tl.records[i].Actors...)
	}
	return out
}

func (tl *telemetryLog) reset() {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	tl.records = nil
}

// TelemetryRecords returns a defensive copy of recorded tick telemetry.
func (g *Game) TelemetryRecords() []WorldTickRecord {
	return g.telemetry.snapshot()
}

// ResetTelemetry clears the recorded tick telemetry buffer and counters.
func (g *Game) ResetTelemetry() {
	g.telemetry.reset()
	g.eventHistory.reset()
	g.pulsesFired.Store(0)
	g.teleportsUsed.Store(0)
}

// PulsesFired returns the number of pulses fired since last reset.
func (g *Game) PulsesFired() int {
	return int(g.pulsesFired.Load())
}

// TeleportsUsed returns the number of debug teleports used since last reset.
func (g *Game) TeleportsUsed() int {
	return int(g.teleportsUsed.Load())
}

func vecDist(a, b maze.Vec2) float64 {
	return math.Hypot(b.X-a.X, b.Y-a.Y)
}

// DebugTeleport relocates the runner authoritatively, for the integrated
// debugging tooling and end-to-end verification. It runs on the writer
// goroutine and resolves the destination against the maze so a bad
// coordinate can never push the runner into a wall.
func (g *Game) DebugTeleport(id SessionID, pos maze.Vec2) {
	g.teleportsUsed.Add(1)
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
	// Record the event authoritatively before fanning out: a subscriber
	// draining its channel must not be the only copy that ever existed.
	tick := 0
	if snap := g.Loop.Snapshot(); snap != nil {
		tick = snap.Round
	}
	g.eventHistory.append(tick, evt)

	g.eventMu.RLock()
	defer g.eventMu.RUnlock()
	for _, e := range g.eventSubs {
		select {
		case e.ch <- evt:
		default:
		}
	}
}

// EventRecords returns a defensive copy of the authoritative event log.
func (g *Game) EventRecords() []EventRecord {
	return g.eventHistory.snapshot()
}

// QueryEvents returns the event records matching the given filter.
func (g *Game) QueryEvents(q EventQuery) []EventRecord {
	return g.eventHistory.query(q)
}

// ResetEvents clears the authoritative event log.
func (g *Game) ResetEvents() {
	g.eventHistory.reset()
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

// PulseBaseDamage is the damage a neutral pulse deals. Elemental advantage
// (attacker beats defender in the cycle) doubles it; disadvantage halves it.
const PulseBaseDamage = 15

// FirePulse processes a pulse fire with lag-compensated hit validation.
// Damage scales by elemental advantage between the runner's active element
// and the struck actor's innate element.
func (g *Game) FirePulse(id SessionID, fireRound int, aim netcode.RayHitOrigin) {
	g.Loop.Submit(func(w *world.World) {
		if w.Phase != world.PhaseRunning {
			return
		}
		g.pulsesFired.Add(1)
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
		if bestIdx < len(g.actorStates) {
			g.actorStates[bestIdx].LastDamageSource = "player"
			g.actorStates[bestIdx].PlayerDamageRound = w.Round
		}
		if m.HP > 0 {
			m.Stunned = true
			m.StunTicks = world.StunDuration
		}
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
// lies between the shooter and a target at that range.
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
