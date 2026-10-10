//go:build example

package world

import (
	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
)

// Phase is the high-level game state.
type Phase int

const (
	PhaseRunning Phase = iota
	PhaseWon
	PhaseLost
)

// MaxHP is the player's starting health.
const MaxHP = 100

// Runner is the player entity.
type Runner struct {
	Pos      maze.Vec2
	Vel      maze.Vec2
	Yaw      float64
	Pitch    float64
	Grounded bool
	HP       int
	// Element is the runner's active element: 0=fire 1=ice 2=lightning
	// 3=void. Power-ups change it; the pulse uses it for elemental
	// advantage against actors.
	Element int
}

// StunDuration is the number of ticks a minotaur stays stunned.
// At 30 Hz, 45 ticks = 1.5 seconds.
const StunDuration = 45

// Minotaur is a PA-BT-driven hunter.
type Minotaur struct {
	ID             string
	Pos            maze.Vec2
	Stunned        bool
	StunTicks      int // remaining stun ticks; decremented each tick, clears Stunned at 0
	Type           int // actor archetype: 0=brute, 1=swarm, 2=caster, 3=stalker
	Element        int // 0=fire 1=ice 2=lightning 3=void; innate faction element
	HP             int
	Strategy       int // current faction strategy: 0=patrol 1=raid 2=retreat
	AttackCooldown int // ticks until this minotaur may damage the runner again
}

// PowerUp is an elemental pickup in the maze. Stepping on one changes the
// runner's active element.
type PowerUp struct {
	Pos     maze.Vec2
	Element int
	Active  bool // false once picked up
}

// Hazard is a temporary elemental rift left by faction crossfire. It damages
// anything standing in it until its TTL expires, making active war zones
// dynamic obstacles the player must route around.
type Hazard struct {
	Pos     maze.Vec2
	Element int
	TTL     int
	// DamageCooldown ticks until the rift may damage whatever stands in it
	// again, bounding rift DPS so hazards obstruct rather than delete.
	DamageCooldown int
}

// World is the authoritative, mutable simulation state. It is owned by the
// single-writer loop goroutine; all other access goes through Snapshot.
type World struct {
	Maze      *maze.Maze
	Runner    Runner
	Minotaurs []*Minotaur
	PowerUps  []*PowerUp
	Hazards   []*Hazard
	Round     int
	Phase     Phase
	// TerritoryControl maps each quadrant (0=NE, 1=NW, 2=SE, 3=SW) to the
	// element currently controlling it. Refreshed every tick by the game
	// logic from faction presence.
	TerritoryControl [4]int
	// Intensity escalates on a schedule: hunter speed multiplier, extra
	// spawns, tighter fog. Driven by Round thresholds.
	Intensity int
}

// New builds a world on the given maze with the runner at the spawn cell.
func New(m *maze.Maze) *World {
	return &World{
		Maze:   m,
		Runner: Runner{Pos: m.Start, Grounded: true, HP: MaxHP},
		Phase:  PhaseRunning,
	}
}

// Step advances the authoritative simulation by dt seconds. It is called once
// per loop tick by the single writer.
func (w *World) Step(dt float64) {
	w.Round++
	// Integrate runner horizontal movement and resolve collision. Full
	// input-driven physics (gravity, jump, prediction reconciliation) lands
	// with the netcode task; here we integrate whatever velocity is set.
	w.Runner.Pos.X += w.Runner.Vel.X * dt
	w.Runner.Pos.Y += w.Runner.Vel.Y * dt
	w.Runner.Pos = w.Maze.Resolve(w.Runner.Pos, maze.Radius)

	// Escalate intensity on a round schedule. Every 300 rounds (~10s at 30Hz)
	// bumps intensity by one level. Capped at 5.
	if w.Round%300 == 0 && w.Intensity < 5 {
		w.Intensity++
	}
}

// Snapshot is an immutable, defensive copy of the world for readers.
type Snapshot struct {
	Round     int
	Phase     Phase
	Runner    Runner
	Minotaurs []Minotaur
	PowerUps  []PowerUp
	Hazards   []Hazard
	Intensity int
	Gate      maze.Vec2
	// TerritoryControl maps each quadrant (0=NE, 1=NW, 2=SE, 3=SW) to the
	// element controlling it. Updated each tick based on faction presence.
	TerritoryControl [4]int
}

// Copy returns a defensive-copy snapshot of the current world state. The
// result shares no mutable references with the world: later writes to the
// world do not affect a previously returned snapshot.
func (w *World) Copy() *Snapshot {
	s := &Snapshot{
		Round:            w.Round,
		Phase:            w.Phase,
		Runner:           w.Runner,
		Intensity:        w.Intensity,
		Gate:             w.Maze.Gate,
		TerritoryControl: w.TerritoryControl,
	}
	s.Minotaurs = make([]Minotaur, len(w.Minotaurs))
	for i, m := range w.Minotaurs {
		s.Minotaurs[i] = *m
	}
	s.PowerUps = make([]PowerUp, len(w.PowerUps))
	for i, p := range w.PowerUps {
		s.PowerUps[i] = *p
	}
	s.Hazards = make([]Hazard, len(w.Hazards))
	for i, h := range w.Hazards {
		s.Hazards[i] = *h
	}
	return s
}
