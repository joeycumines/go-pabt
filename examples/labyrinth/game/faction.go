//go:build example

package game

import (
	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

// FactionCombatRange is the distance within which rival-faction actors
// attack each other each tick.
const FactionCombatRange = 1.5

// FactionCombatCooldown is the number of ticks between faction attacks.
const FactionCombatCooldown = 15 // 0.5s at 30Hz

// ActorState tracks per-actor combat and strategy state that lives outside
// the brain (which is PA-BT territory for S4).
type ActorState struct {
	// FactionAttackCooldown ticks until this actor may attack a rival again.
	FactionAttackCooldown int
	// Strategy is the current high-level behavior: 0=patrol, 1=raid, 2=retreat.
	Strategy int
	// StrategyTimer ticks until the strategy is re-evaluated.
	StrategyTimer int
	// TargetPos is where the actor is heading under its current strategy.
	TargetPos maze.Vec2
	// HasTarget indicates whether TargetPos is valid.
	HasTarget bool
}

const (
	StrategyPatrol  = 0
	StrategyRaid    = 1
	StrategyRetreat = 2
)

// actorStates is parallel to World.Minotaurs. It is owned by the Game and
// only touched on the writer goroutine.
var _ = (*ActorState)(nil) // keep the type referenced

// factionCombat processes inter-faction combat on the writer goroutine.
// Rival-faction actors within FactionCombatRange attack each other, with
// damage scaled by elemental advantage.
func factionCombat(w *world.World, states []*ActorState) {
	for i := 0; i < len(w.Minotaurs); i++ {
		a := w.Minotaurs[i]
		if a.Stunned || a.HP <= 0 {
			continue
		}
		if states[i].FactionAttackCooldown > 0 {
			continue
		}
		for j := i + 1; j < len(w.Minotaurs); j++ {
			b := w.Minotaurs[j]
			if b.Stunned || b.HP <= 0 {
				continue
			}
			if a.Element == b.Element {
				continue // same faction, no friendly fire
			}
			dx := a.Pos.X - b.Pos.X
			dy := a.Pos.Y - b.Pos.Y
			if dx*dx+dy*dy > FactionCombatRange*FactionCombatRange {
				continue
			}
			// Both attack each other simultaneously.
			dmgA := int(float64(10) * advantage(Element(a.Element), Element(b.Element)))
			dmgB := int(float64(10) * advantage(Element(b.Element), Element(a.Element)))
			b.HP -= dmgA
			a.HP -= dmgB
			states[i].FactionAttackCooldown = FactionCombatCooldown
			states[j].FactionAttackCooldown = FactionCombatCooldown
		}
	}
}

// respawnDeadActors replaces dead actors with fresh spawns, maintaining the
// faction balance. Called on the writer goroutine.
func (g *Game) respawnDeadActors(w *world.World, states *[]*ActorState) {
	for i := len(w.Minotaurs) - 1; i >= 0; i-- {
		if w.Minotaurs[i].HP > 0 {
			continue
		}
		// Remove the dead actor.
		w.Minotaurs = append(w.Minotaurs[:i], w.Minotaurs[i+1:]...)
		*states = append((*states)[:i], (*states)[i+1:]...)
		if i < len(g.Brains) {
			g.Brains = append(g.Brains[:i], g.Brains[i+1:]...)
		}
		if i < len(g.Histories) {
			g.Histories = append(g.Histories[:i], g.Histories[i+1:]...)
		}
	}
	// Respawn to maintain 8 actors.
	for len(w.Minotaurs) < 8 {
		typ := ActorType(len(w.Minotaurs) % 4)
		g.spawnActor(typ)
		*states = append(*states, &ActorState{})
	}
}

// territoryControl returns the element controlling each quadrant (0-3).
// Control is determined by which faction has the most total HP in the
// quadrant. Returns [4]Element indexed by territory (0=NE, 1=NW, 2=SE, 3=SW).
func territoryControl(w *world.World) [4]int {
	// Accumulate HP per element per territory.
	hp := [4][4]float64{} // [territory][element]
	for _, m := range w.Minotaurs {
		if m.HP <= 0 {
			continue
		}
		t := territoryIndex(w.Maze, m.Pos)
		hp[t][m.Element] += float64(m.HP)
	}
	var control [4]int
	for t := 0; t < 4; t++ {
		best := 0
		bestHP := 0.0
		for e := 0; e < 4; e++ {
			if hp[t][e] > bestHP {
				bestHP = hp[t][e]
				best = e
			}
		}
		control[t] = best
	}
	return control
}

// territoryIndex maps a position to a territory index: 0=NE(fire), 1=NW(ice),
// 2=SE(lightning), 3=SW(void).
func territoryIndex(m *maze.Maze, pos maze.Vec2) int {
	east := pos.X >= float64(m.W)/2
	north := pos.Y < float64(m.H)/2
	switch {
	case north && east:
		return 0
	case north && !east:
		return 1
	case !north && east:
		return 2
	default:
		return 3
	}
}
