//go:build example

package game

import (
	"math"
	"testing"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
)

// TestActorsKeepMovingOverTicks is the stalemate regression: across a long
// headless run every survivor must accumulate real displacement. A gang war
// where nobody moves is a frozen diorama, not a living ecosystem.
func TestActorsKeepMovingOverTicks(t *testing.T) {
	m := maze.Generate(42, 21, 21)
	g := NewGame(m, 8, 42)
	w := g.World

	prev := make(map[string]maze.Vec2, len(w.Minotaurs))
	for _, mm := range w.Minotaurs {
		prev[mm.ID] = mm.Pos
	}

	// 40 windows of 500 ticks (~16s of game time each): a living gang war
	// must keep moving in every window after the opening brawl settles.
	frozen := 0
	for window := 0; window < 40; window++ {
		cum := make(map[string]float64, len(w.Minotaurs))
		for tick := 0; tick < 500; tick++ {
			g.tickLogic(w)
			if tick%30 != 29 {
				continue
			}
			for _, mm := range w.Minotaurs {
				if p, ok := prev[mm.ID]; ok {
					cum[mm.ID] += math.Hypot(mm.Pos.X-p.X, mm.Pos.Y-p.Y)
				}
				prev[mm.ID] = mm.Pos
			}
		}
		movers := 0
		for _, mm := range w.Minotaurs {
			if cum[mm.ID] > 1.0 {
				movers++
			}
		}
		t.Logf("window %2d (tick %6d): movers=%d roster=%d", window, (window+1)*500, movers, len(w.Minotaurs))
		if window >= 2 && movers == 0 {
			frozen++
			if frozen == 1 {
				for i, mm := range w.Minotaurs {
					st := g.actorStates[i]
					b := g.Brains[i].BeliefCopy()
					t.Logf("FROZEN %s pos=%v strat=%d hasTarget=%v target=%v path=%d detected=%v lastKnown=%v exhausted=%v caught=%v stunned=%v hp=%d",
						mm.ID, mm.Pos, st.Strategy, st.HasTarget, st.TargetPos, len(st.Path),
						b.PlayerDetected, b.HasLastKnown, b.SearchExhausted, b.Caught, mm.Stunned, mm.HP)
				}
			}
		} else {
			frozen = 0
		}
	}
	if frozen > 0 {
		t.Fatalf("war froze: %d consecutive zero-mover windows", frozen)
	}
}
