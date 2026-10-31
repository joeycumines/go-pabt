//go:build example

package game

import (
	"math"

	"github.com/joeycumines/go-pabt/examples/labyrinth/brain"
	"github.com/joeycumines/go-pabt/examples/labyrinth/loop"
	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

const (
	// CasterWindupTicks is a caster's telegraphed bolt windup: 15 ticks (0.5s).
	CasterWindupTicks = 15
	// CasterCooldownTicks is the per-caster cooldown between ranged bolts: 75 ticks (2.5s).
	CasterCooldownTicks = 75
	// BoltSpeed is the speed of a ranged bolt projectile in cells per second.
	BoltSpeed = 7.0
	// BoltRadius is the collision radius of a ranged bolt projectile.
	BoltRadius = 0.25
	// BoltTTLTicks is the lifetime of a ranged bolt projectile: 60 ticks (2.0s).
	BoltTTLTicks = 60
)

// casterBolt fires one caster's ranged bolt projectile toward the runner.
// Kept as an entry point for direct tests; tickBolts calls it at the end of a
// caster's 15-tick windup.
func (g *Game) casterBolt(w *world.World, m *world.Minotaur) {
	dx := w.Runner.Pos.X - m.Pos.X
	dy := w.Runner.Pos.Y - m.Pos.Y
	d := math.Hypot(dx, dy)
	var vx, vy float64
	if d > 1e-6 {
		vx = dx / d * BoltSpeed
		vy = dy / d * BoltSpeed
	} else {
		vx = BoltSpeed
	}
	g.nextBoltID++
	b := &world.Bolt{
		ID:      g.nextBoltID,
		Pos:     m.Pos,
		Vel:     maze.Vec2{X: vx, Y: vy},
		Element: m.Element,
		TTL:     BoltTTLTicks,
		OwnerID: m.ID,
	}
	w.Bolts = append(w.Bolts, b)
	g.broadcastEvent(Event{
		Type: "bolt",
		Data: map[string]interface{}{
			"id":      m.ID,
			"boltId":  b.ID,
			"element": m.Element,
			"x":       m.Pos.X,
			"y":       m.Pos.Y,
			"vx":      vx,
			"vy":      vy,
		},
	})
}

// tickBolts drives caster ranged windups and advances active bolt projectiles.
// Bolts die on solid walls or after BoltTTLTicks, and damage the runner on
// contact within BoltRadius + maze.Radius.
func (g *Game) tickBolts(w *world.World) {
	for i, m := range w.Minotaurs {
		if ActorType(m.Type) != ActorCaster {
			continue
		}
		if m.HP <= 0 || m.Stunned {
			if m.AtkType == 2 {
				m.Windup = 0
				m.Atk = 0
				m.AtkType = 0
			}
			continue
		}
		if m.AtkType == 2 && m.Windup > 0 {
			m.Windup--
			if m.Windup > 0 {
				m.Atk = float64(CasterWindupTicks-m.Windup+1) / float64(CasterWindupTicks)
				continue
			}
			m.Atk = 0
			m.AtkType = 0
			m.AttackCooldown = CasterCooldownTicks
			if brain.HasClearLOS(g.Maze, m.Pos, w.Runner.Pos) {
				g.casterBolt(w, m)
			}
			continue
		}
		if m.Windup > 0 || m.AttackCooldown > 0 {
			continue
		}
		wantsCast := false
		if i < len(g.Brains) {
			wantsCast = g.Brains[i].LastTelemetry().CastBolt
		}
		if wantsCast && brain.HasClearLOS(g.Maze, m.Pos, w.Runner.Pos) {
			m.Windup = CasterWindupTicks
			m.AtkType = 2
			m.Atk = 1.0 / float64(CasterWindupTicks)
		}
	}

	dt := loop.TickInterval.Seconds()
	hitRadius2 := (BoltRadius + maze.Radius) * (BoltRadius + maze.Radius)
	alive := w.Bolts[:0]
	for _, b := range w.Bolts {
		b.TTL--
		if b.TTL <= 0 {
			continue
		}
		// Advance in 2 substeps per tick so a fast bolt cannot skip a wall or the runner.
		const steps = 2
		subDt := dt / steps
		dropped := false
		for s := 0; s < steps; s++ {
			b.Pos.X += b.Vel.X * subDt
			b.Pos.Y += b.Vel.Y * subDt
			cx := int(math.Floor(b.Pos.X))
			cy := int(math.Floor(b.Pos.Y))
			if g.Maze.Solid(cx, cy) {
				dropped = true
				break
			}
			dx := b.Pos.X - w.Runner.Pos.X
			dy := b.Pos.Y - w.Runner.Pos.Y
			if dx*dx+dy*dy <= hitRadius2 {
				g.damageRunner(w, BoltDamage, "bolt", b.Pos, b.OwnerID)
				dropped = true
				break
			}
		}
		if !dropped {
			alive = append(alive, b)
		}
	}
	w.Bolts = alive
}
