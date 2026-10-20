//go:build example

package playtest

import (
	"fmt"

	"github.com/joeycumines/go-pabt/examples/labyrinth/game"
	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
	"github.com/joeycumines/go-pabt/examples/labyrinth/netcode"
	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

// Config specifies parameters for a deterministic playtest episode.
type Config struct {
	Seed    uint64
	Persona PersonaName
	Ticks   int
	Actors  int
}

// DefaultConfig returns a standard 600-tick (20s at 30Hz) episode configuration.
func DefaultConfig(persona PersonaName) Config {
	return Config{
		Seed:    444004,
		Persona: persona,
		Ticks:   600,
		Actors:  game.TargetActorCount,
	}
}

// BotAction is the control command emitted by a Persona on a single tick.
type BotAction struct {
	Forward float64
	Strafe  float64
	Yaw     float64
	Pitch   float64
	Sprint  bool
	Fire    bool
	Note    string
}

// EpisodeResult holds the complete causal telemetry and oracle scorecard for one episode.
type EpisodeResult struct {
	Config         Config                 `json:"config"`
	Maze           *maze.Maze             `json:"-"`
	FinalPhase     world.Phase            `json:"finalPhase"`
	FinalRound     int                    `json:"finalRound"`
	FinalRunnerHP  int                    `json:"finalRunnerHp"`
	PulsesFired    int                    `json:"pulsesFired"`
	StunsInflicted int                    `json:"stunsInflicted"`
	TeleportsUsed  int                    `json:"teleportsUsed"`
	Records        []game.WorldTickRecord `json:"records"`
	Oracles        []OracleResult         `json:"oracles"`
	Causality      CausalSummary          `json:"causality"`
}

// Engine drives a deterministic, synchronous Game instance without wall-clock sleeps.
type Engine struct {
	Config    Config
	Game      *game.Game
	SessionID game.SessionID
	Bot       PersonaController
	seq       uint64
	pulses    int
	lastFire  int
}

// NewEngine creates a synchronous headless Game and registers a player session.
// Note: Do NOT start go g.Loop.Run(ctx); Engine steps g.Loop.TickOnce() deterministically.
func NewEngine(cfg Config) *Engine {
	if cfg.Seed == 0 {
		cfg.Seed = 444004
	}
	if cfg.Ticks <= 0 {
		cfg.Ticks = 600
	}
	if cfg.Actors <= 0 {
		cfg.Actors = game.TargetActorCount
	}
	m := maze.Generate(cfg.Seed, 21, 21)
	g := game.NewGame(m, cfg.Actors, cfg.Seed)
	sess, _, _ := g.AddSession("")
	// Drain initial session reset command if any.
	g.Loop.TickOnce()
	g.ResetTelemetry()

	return &Engine{
		Config:    cfg,
		Game:      g,
		SessionID: sess.ID,
		Bot:       NewPersona(cfg.Persona, m),
		lastFire:  -100,
	}
}

// StepTicks advances the simulation by n ticks using the active PersonaController.
func (e *Engine) StepTicks(n int) {
	for i := 0; i < n; i++ {
		snap := e.Game.Loop.Snapshot()
		if snap == nil || snap.Phase != world.PhaseRunning {
			break
		}
		act := e.Bot.Step(snap, e.Game.Maze)
		e.seq++
		e.Game.ApplyInput(e.SessionID, netcode.InputCmd{
			Seq:     e.seq,
			Forward: act.Forward,
			Strafe:  act.Strafe,
			Yaw:     act.Yaw,
			Pitch:   act.Pitch,
			Sprint:  act.Sprint,
		})
		// Enforce 15-tick (0.5s at 30Hz) fire cooldown matching client FIRE_COOLDOWN.
		if act.Fire && snap.Round-e.lastFire >= 15 {
			e.lastFire = snap.Round
			e.pulses++
			e.Game.FirePulse(e.SessionID, snap.Round, netcode.RayHitOrigin{
				Pos:   snap.Runner.Pos,
				Yaw:   act.Yaw,
				Pitch: act.Pitch,
			})
		}
		e.Game.Loop.TickOnce()
	}
}

// RunUntil steps the simulation up to maxTicks until pred returns true on the latest WorldTickRecord.
func (e *Engine) RunUntil(maxTicks int, pred func(rec game.WorldTickRecord) bool) (game.WorldTickRecord, bool) {
	for i := 0; i < maxTicks; i++ {
		e.StepTicks(1)
		recs := e.Game.TelemetryRecords()
		if len(recs) == 0 {
			continue
		}
		last := recs[len(recs)-1]
		if pred(last) {
			return last, true
		}
		if last.Phase != int(world.PhaseRunning) {
			return last, false
		}
	}
	recs := e.Game.TelemetryRecords()
	if len(recs) > 0 {
		return recs[len(recs)-1], false
	}
	return game.WorldTickRecord{}, false
}

// Finish compiles the EpisodeResult, causal summary, and all 10 behavioral oracle evaluations.
func (e *Engine) Finish() EpisodeResult {
	recs := e.Game.TelemetryRecords()
	snap := e.Game.Loop.Snapshot()
	var finalPhase world.Phase
	var finalRound, finalHP int
	if snap != nil {
		finalPhase = snap.Phase
		finalRound = snap.Round
		finalHP = snap.Runner.HP
	}
	stuns := 0
	for i := 1; i < len(recs); i++ {
		prevStun := make(map[string]bool, len(recs[i-1].Actors))
		for _, a := range recs[i-1].Actors {
			prevStun[a.ActorID] = a.Stunned
		}
		for _, a := range recs[i].Actors {
			if a.Stunned && !prevStun[a.ActorID] {
				stuns++
			}
		}
	}
	causality := SummarizeCausality(recs)
	res := EpisodeResult{
		Config:         e.Config,
		Maze:           e.Game.Maze,
		FinalPhase:     finalPhase,
		FinalRound:     finalRound,
		FinalRunnerHP:  finalHP,
		PulsesFired:    e.pulses,
		StunsInflicted: stuns,
		TeleportsUsed:  0, // Engine never calls DebugTeleport
		Records:        recs,
		Causality:      causality,
	}
	res.Oracles = EvaluateOracles(&res)
	return res
}

// RunEpisode executes a full headless episode for cfg and returns its EpisodeResult.
func RunEpisode(cfg Config) EpisodeResult {
	eng := NewEngine(cfg)
	eng.StepTicks(cfg.Ticks)
	return eng.Finish()
}

// FormatPhase returns a human-readable string for world.Phase.
func FormatPhase(p world.Phase) string {
	switch p {
	case world.PhaseRunning:
		return "RUNNING"
	case world.PhaseWon:
		return "WON"
	case world.PhaseLost:
		return "LOST"
	default:
		return fmt.Sprintf("PHASE(%d)", int(p))
	}
}
