//go:build example

package game

import (
	"math"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

// Damage pipeline tuning. All runner damage goes through damageRunner so the
// clamp, invulnerability window, and client feedback stay in one place.
const (
	// InvulnTicks is the grace period after a hit, at 30Hz: 0.67s. It bounds
	// the damage rate no matter how many sources overlap.
	InvulnTicks = 20
	// RunnerKnockbackMax caps how far one knockRunner call may move the
	// player: 0.6u, well under the detector's 1.6u "thrown across the maze"
	// threshold and short enough to survive a corridor corner.
	RunnerKnockbackMax = 0.6
	// KnockbackStepSize is the collision substep used by knockRunner.
	KnockbackStepSize = 0.1
	// RegenDelayTicks is the out-of-combat window before health returns:
	// 6s at 30Hz.
	RegenDelayTicks = 180
	// RegenIntervalTicks is how often one HP returns once regen starts.
	RegenIntervalTicks = 15
	// ContactCooldownTicks gates repeat contact damage from one actor.
	ContactCooldownTicks = 30
	// BoltDamage is a caster's ranged hit.
	BoltDamage = 2
)

// actorBodyRadius mirrors client/js/archetypes.mjs BODY_RADIUS so the server's
// body separation agrees with the size a player sees on screen. Keeping the
// models out of the camera is the point: a 0.3u collision cylinder is much
// smaller than the rendered body.
func actorBodyRadius(t ActorType) float64 {
	switch t {
	case ActorBrute:
		return 0.55
	case ActorSwarm:
		return 0.35
	case ActorCaster:
		return 0.4
	case ActorStalker:
		return 0.4
	default:
		return 0.4
	}
}

// damageRunner is the single entry point for damage to the player. It clamps
// HP at zero, honours the invulnerability window, and broadcasts one 'hit'
// event carrying the source and origin so the client can show directional
// feedback. It reports whether the hit landed.
func (g *Game) damageRunner(w *world.World, dmg int, source string, from maze.Vec2, attackerID string) bool {
	if dmg <= 0 || w.Phase != world.PhaseRunning {
		return false
	}
	if w.Runner.InvulnTicks > 0 {
		return false
	}
	w.Runner.HP -= dmg
	if w.Runner.HP < 0 {
		w.Runner.HP = 0
	}
	w.Runner.InvulnTicks = InvulnTicks
	w.Runner.LastDamageRound = w.Round
	w.Runner.LastAttacker = attackerID
	g.broadcastEvent(Event{
		Type: "hit",
		Data: map[string]interface{}{
			"damage":   dmg,
			"hp":       w.Runner.HP,
			"source":   source,
			"x":        from.X,
			"y":        from.Y,
			"attacker": attackerID,
		},
	})
	return true
}

// knockRunner displaces the player along dir by dist, capped and stepped so
// collision is resolved in small increments. A push that would enter a wall
// stops at the last legal position instead of shoving the camera through
// geometry. Session reconciliation targets are synced afterwards.
func (g *Game) knockRunner(w *world.World, dir maze.Vec2, dist float64) {
	d := math.Hypot(dir.X, dir.Y)
	if d < 1e-9 || dist <= 0 {
		return
	}
	if dist > RunnerKnockbackMax {
		dist = RunnerKnockbackMax
	}
	ux, uy := dir.X/d, dir.Y/d

	moved := 0.0
	for moved < dist-1e-9 {
		step := math.Min(KnockbackStepSize, dist-moved)
		next := maze.Vec2{X: w.Runner.Pos.X + ux*step, Y: w.Runner.Pos.Y + uy*step}
		resolved := g.Maze.Resolve(next, maze.Radius)
		// If the destination cell is solid, Resolve pushed the body back out
		// of it; treat that as a wall and stop rather than sliding along it.
		if g.Maze.Solid(int(math.Floor(resolved.X)), int(math.Floor(resolved.Y))) {
			break
		}
		// A resolved step shorter than half the intended one means the body
		// ran into a corner and further substeps cannot make progress.
		if math.Hypot(resolved.X-w.Runner.Pos.X, resolved.Y-w.Runner.Pos.Y) < step*0.5 {
			w.Runner.Pos = resolved
			break
		}
		w.Runner.Pos = resolved
		moved += step
	}
	g.syncRunnerSessions(w)
}

// syncRunnerSessions copies the authoritative runner position back to every
// live session so client prediction reconciles to knockback displacement.
func (g *Game) syncRunnerSessions(w *world.World) {
	g.sessMu.RLock()
	defer g.sessMu.RUnlock()
	for _, sess := range g.sessions {
		if sess != nil && sess.Net != nil {
			sess.Net.Pos = w.Runner.Pos
		}
	}
}

// casterBolt fires one caster's ranged attack: the projectile event the
// client draws, then damage through the shared pipeline.
func (g *Game) casterBolt(w *world.World, m *world.Minotaur) {
	g.broadcastEvent(Event{
		Type: "bolt",
		Data: map[string]interface{}{
			"id":      m.ID,
			"element": m.Element,
			"x":       m.Pos.X,
			"y":       m.Pos.Y,
		},
	})
	g.damageRunner(w, BoltDamage, "bolt", m.Pos, m.ID)
}

// tickContactDamage applies melee contact damage and mass-ratio knockback
// between the runner and every non-stunned actor within 0.7u. Runs outside the
// brain loop so it fires regardless of brain iteration state.
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
		if !g.damageRunner(w, dmg, "contact", m.Pos, m.ID) {
			continue // invulnerable: no damage, no shove
		}
		m.AttackCooldown = ContactCooldownTicks
		// Push both bodies apart by the mass-ratio impulse. The runner's
		// share goes through knockRunner so the maze, not the impulse,
		// decides the final position.
		d := math.Hypot(dx, dy)
		if d < 1e-9 {
			d = 1e-9
			dx, dy = 1e-9, 0
		}
		ux, uy := dx/d, dy/d
		aMass := actorMass(ActorType(m.Type))
		totalMass := RunnerMass + aMass
		g.knockRunner(w, maze.Vec2{X: -dx, Y: -dy}, ContactKnockback*(aMass/totalMass))
		actorPush := ContactKnockback * (RunnerMass / totalMass)
		m.Pos.X += ux * actorPush
		m.Pos.Y += uy * actorPush
		m.Pos = g.Maze.Resolve(m.Pos, maze.Radius)
	}
}

// separateRunnerFromActors pushes the runner and each overlapping actor apart
// so the two bodies can never occupy the same space. The minimum centre
// distance is the runner cylinder plus the actor's rendered body radius,
// matching what the player sees. Pure world-level separation runs regardless
// of whether any session is connected.
//
// The actor takes the separation push so enemies cannot bulldoze the player
// across the maze while walking; if the actor is backed against a solid wall,
// the runner takes whatever separation push remains.
func (g *Game) separateRunnerFromActors(w *world.World) {
	for _, a := range w.Minotaurs {
		if a.HP <= 0 {
			continue
		}
		minSep := maze.Radius + actorBodyRadius(ActorType(a.Type))
		dx := a.Pos.X - w.Runner.Pos.X
		dy := a.Pos.Y - w.Runner.Pos.Y
		d2 := dx*dx + dy*dy
		if d2 >= minSep*minSep {
			continue
		}
		d := math.Sqrt(d2)
		if d < 1e-9 {
			d = 1e-9
			dx, dy = 1e-9, 0
		}
		ux, uy := dx/d, dy/d
		push := minSep - d
		a.Pos.X += ux * push
		a.Pos.Y += uy * push
		a.Pos = g.Maze.Resolve(a.Pos, maze.Radius)

		// If the actor was backed against a wall and could not separate fully,
		// nudge the runner out by the remainder.
		newD := math.Hypot(a.Pos.X-w.Runner.Pos.X, a.Pos.Y-w.Runner.Pos.Y)
		if newD < minSep {
			rem := minSep - newD
			w.Runner.Pos.X -= ux * rem
			w.Runner.Pos.Y -= uy * rem
			w.Runner.Pos = g.Maze.Resolve(w.Runner.Pos, maze.Radius)
		}
	}
	g.syncRunnerSessions(w)
}

// resolveRunnerAgainstActors ensures player movement cannot penetrate or pass
// through living actor bodies. When newPos is closer than minSep to an actor,
// the runner is pushed back out to minSep along the side they approached from.
func (g *Game) resolveRunnerAgainstActors(w *world.World, prevPos, newPos maze.Vec2) maze.Vec2 {
	for _, a := range w.Minotaurs {
		if a.HP <= 0 {
			continue
		}
		minSep := maze.Radius + actorBodyRadius(ActorType(a.Type))
		dx := newPos.X - a.Pos.X
		dy := newPos.Y - a.Pos.Y
		d2 := dx*dx + dy*dy
		if d2 >= minSep*minSep {
			continue
		}
		pdx := prevPos.X - a.Pos.X
		pdy := prevPos.Y - a.Pos.Y
		pd2 := pdx*pdx + pdy*pdy
		var nx, ny float64
		if dx*pdx+dy*pdy < 0 && pd2 > 1e-9 {
			pd := math.Sqrt(pd2)
			nx, ny = pdx/pd, pdy/pd
		} else if d2 > 1e-9 {
			d := math.Sqrt(d2)
			nx, ny = dx/d, dy/d
		} else if pd2 > 1e-9 {
			pd := math.Sqrt(pd2)
			nx, ny = pdx/pd, pdy/pd
		} else {
			nx, ny = 1, 0
		}
		newPos.X = a.Pos.X + nx*minSep
		newPos.Y = a.Pos.Y + ny*minSep
		newPos = g.Maze.Resolve(newPos, maze.Radius)
	}
	return newPos
}
