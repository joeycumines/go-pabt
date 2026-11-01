//go:build example

package game

import (
	mrand "math/rand"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

// SigilHuntDuration is the duration in ticks that a faction hunts the runner
// after their elemental sigil is taken (12 seconds at 30Hz).
const SigilHuntDuration = 12 * 30

// factionName returns the narrative title of the faction for element elem.
func factionName(elem Element) string {
	switch elem {
	case ElementFire:
		return "Ember Legion"
	case ElementIce:
		return "Frost Covenant"
	case ElementLightning:
		return "Storm Pact"
	default:
		return "Void Cult"
	}
}

// spawnSigils places 4 elemental sigils in the maze, one per faction quadrant,
// selecting walkable cells positioned far from Start. Called once per game or
// world reset.
func spawnSigils(w *world.World, m *maze.Maze, rng *mrand.Rand) {
	w.Sigils = nil
	w.SigilsHeld = 0
	w.HuntRemaining = [4]int{}

	for elem := 0; elem < 4; elem++ {
		var bestPos maze.Vec2
		bestDist := -1.0
		found := false

		// Collect and evaluate candidate walkable cells in this quadrant.
		for attempts := 0; attempts < 200; attempts++ {
			cx := 1 + rng.Intn(m.W-2)
			cy := 1 + rng.Intn(m.H-2)
			if !m.At(cx, cy).Walkable() {
				continue
			}
			pos := maze.Vec2{X: float64(cx) + 0.5, Y: float64(cy) + 0.5}
			if TerritoryOf(m, pos) != Element(elem) {
				continue
			}
			dStart := distSq(pos, m.Start)
			dGate := distSq(pos, m.Gate)
			if dStart < 16 || dGate < 16 {
				continue
			}
			if !found || dStart > bestDist {
				bestDist = dStart
				bestPos = pos
				found = true
			}
		}

		// Fallback if random sampling found no cell at least 4u away.
		if !found {
			for cy := 1; cy < m.H-1; cy++ {
				for cx := 1; cx < m.W-1; cx++ {
					if !m.At(cx, cy).Walkable() {
						continue
					}
					pos := maze.Vec2{X: float64(cx) + 0.5, Y: float64(cy) + 0.5}
					if TerritoryOf(m, pos) != Element(elem) {
						continue
					}
					dStart := distSq(pos, m.Start)
					if !found || dStart > bestDist {
						bestDist = dStart
						bestPos = pos
						found = true
					}
				}
			}
		}

		w.Sigils = append(w.Sigils, &world.Sigil{
			Pos:     bestPos,
			Element: elem,
			Taken:   false,
		})
	}
}

// checkSigilPickup runs on the single-writer goroutine. If the runner is within
// 0.6u of an untaken sigil, the runner collects it: SigilsHeld increments,
// intensity increases by 1, and the corresponding faction enters a 12s hunt.
func checkSigilPickup(g *Game, w *world.World) {
	for _, sig := range w.Sigils {
		if sig.Taken {
			continue
		}
		dx := sig.Pos.X - w.Runner.Pos.X
		dy := sig.Pos.Y - w.Runner.Pos.Y
		if dx*dx+dy*dy < 0.6*0.6 {
			sig.Taken = true
			w.SigilsHeld++
			if w.Intensity < 5 {
				w.Intensity++
			}
			w.HuntRemaining[sig.Element] = SigilHuntDuration

			// Alert and retarget all live actors of this faction to Raid the runner.
			for i, m := range w.Minotaurs {
				if m.Element == sig.Element && m.HP > 0 {
					if i < len(g.actorStates) {
						g.actorStates[i].Strategy = StrategyRaid
						m.Strategy = StrategyRaid
						setStrategyTarget(g.actorStates[i], w.Runner.Pos)
					}
					if i < len(g.Brains) {
						g.Brains[i].AlertPlayerPos(w.Runner.Pos)
						g.Brains[i].SetPatrolTarget(w.Runner.Pos)
					}
				}
			}

			// Broadcast hunt announcement banner event.
			g.broadcastEvent(Event{
				Type: "hunt",
				Data: map[string]interface{}{
					"element":  sig.Element,
					"name":     elementName(Element(sig.Element)),
					"faction":  factionName(Element(sig.Element)),
					"duration": 12,
				},
			})

			// Broadcast sigil collection event.
			g.broadcastEvent(Event{
				Type: "sigil",
				Data: map[string]interface{}{
					"element":      sig.Element,
					"held":         w.SigilsHeld,
					"total":        3,
					"gateUnlocked": w.SigilsHeld >= 3,
				},
			})
			return
		}
	}
}

// tickHunts decrements active faction hunt timers and maintains raid pressure
// toward the runner during the 12-second hunt window.
func tickHunts(g *Game, w *world.World) {
	for elem := 0; elem < 4; elem++ {
		if w.HuntRemaining[elem] <= 0 {
			continue
		}
		w.HuntRemaining[elem]--

		// Every 15 ticks (~0.5s), update raiding actors' targeting to the runner's
		// current position so they maintain pursuit during the hunt.
		if w.Round%15 == 0 {
			for i, m := range w.Minotaurs {
				if m.Element == elem && m.HP > 0 && i < len(g.actorStates) {
					if g.actorStates[i].Strategy != StrategyRetreat {
						g.actorStates[i].Strategy = StrategyRaid
						m.Strategy = StrategyRaid
						setStrategyTarget(g.actorStates[i], w.Runner.Pos)
						if i < len(g.Brains) {
							g.Brains[i].AlertPlayerPos(w.Runner.Pos)
							g.Brains[i].SetPatrolTarget(w.Runner.Pos)
						}
					}
				}
			}
		}
	}
}
