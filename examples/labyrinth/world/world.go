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

// Runner is the player entity.
type Runner struct {
	Pos      maze.Vec2
	Vel      maze.Vec2
	Yaw      float64
	Pitch    float64
	Grounded bool
}

// Minotaur is a PA-BT-driven hunter.
type Minotaur struct {
	ID      string
	Pos     maze.Vec2
	Stunned bool
	// Belief is the minotaur's current belief about the player. The BT state
	// view reads this to drive pursuit/search decisions. Belief injection
	// (T8) mutates this field to redirect the hunter.
	Belief Belief
}

// Belief is a minotaur's model of the player's state.
type Belief struct {
	PlayerDetected  bool      // line-of-sight currently held
	LastKnownPos    maze.Vec2 // last position where the player was seen
	DistanceBand    int       // 0=adjacent, 1=near, 2=far, 3=unknown
	SearchExhausted bool      // true when search at last-known yielded nothing
}

// World is the authoritative, mutable simulation state. It is owned by the
// single-writer loop goroutine; all other access goes through Snapshot.
type World struct {
	Maze      *maze.Maze
	Runner    Runner
	Minotaurs []*Minotaur
	Round     int
	Phase     Phase
	// Intensity escalates on a schedule: hunter speed multiplier, extra
	// spawns, tighter fog. Driven by Round thresholds.
	Intensity int
}

// New builds a world on the given maze with the runner at the spawn cell.
func New(m *maze.Maze) *World {
	return &World{
		Maze:   m,
		Runner: Runner{Pos: m.Start, Grounded: true},
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
	Intensity int
}

// Copy returns a defensive-copy snapshot of the current world state. The
// result shares no mutable references with the world: later writes to the
// world do not affect a previously returned snapshot.
func (w *World) Copy() *Snapshot {
	s := &Snapshot{
		Round:     w.Round,
		Phase:     w.Phase,
		Runner:    w.Runner,
		Intensity: w.Intensity,
	}
	s.Minotaurs = make([]Minotaur, len(w.Minotaurs))
	for i, m := range w.Minotaurs {
		s.Minotaurs[i] = *m
	}
	return s
}
