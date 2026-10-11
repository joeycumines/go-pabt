//go:build example

package game

import (
	"math"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

// Faction war tuning. Rival-faction actors within FactionCombatRange exchange
// blows when off cooldown, damage scaled by elemental advantage; crossfire
// leaves temporary hazards that make active war zones dynamic obstacles.
// Everything here runs on the single-writer goroutine inside tickLogic.
const (
	FactionCombatRange    = 1.5
	FactionCombatCooldown = 15 // 0.5s at 30Hz
	FactionBaseDamage     = 8
	StrategyReevalTicks   = 90 // re-pick a strategy every 3s at 30Hz
	TargetActorCount      = 9  // was 8; G7 raises the roster so all 4 factions
	// can be represented at once with a spare slot for respawn balancing.
	HazardRadius         = 0.9
	HazardTTL            = 120 // 4s at 30Hz
	HazardDamage         = 2   // per damage tick to anything standing in a rift
	HazardDamageInterval = 15  // 0.5s at 30Hz between rift damage ticks
	MaxHazards           = 10
)

// Strategy is a rule-based high-level behavior. PA-BT-driven faction strategy
// lands in S4; these rules make the gang war observable now: clusters form in
// home territory (patrol), push at rivals (raid), and fall back when hurt
// (retreat).
const (
	StrategyPatrol  = 0
	StrategyRaid    = 1
	StrategyRetreat = 2
)

// ActorState tracks per-actor combat and strategy state that lives outside
// the PA-BT brain. Parallel to World.Minotaurs; owned by Game; touched only
// on the writer goroutine.
type ActorState struct {
	FactionAttackCooldown int
	Strategy              int
	StrategyTimer         int
	TargetPos             maze.Vec2
	HasTarget             bool
	// Path is the remaining A* waypoint list toward TargetPos (front =
	// next waypoint). Empty means replan on the next movement tick.
	Path []maze.Vec2
}

// factionCombat processes inter-faction combat. Rival-faction actors within
// range attack simultaneously, damage scaled by elemental advantage; same-
// faction actors never fire on each other. Each clash leaves a hazard and
// broadcasts a clash event so the client can show the crossfire.
func (g *Game) factionCombat(w *world.World) {
	states := g.actorStates
	for i := 0; i < len(w.Minotaurs); i++ {
		a := w.Minotaurs[i]
		if a.Stunned || a.HP <= 0 || i >= len(states) || states[i].FactionAttackCooldown > 0 {
			continue
		}
		for j := i + 1; j < len(w.Minotaurs); j++ {
			b := w.Minotaurs[j]
			if b.Stunned || b.HP <= 0 || a.Element == b.Element {
				continue
			}
			dx := a.Pos.X - b.Pos.X
			dy := a.Pos.Y - b.Pos.Y
			if dx*dx+dy*dy > FactionCombatRange*FactionCombatRange {
				continue
			}
			dmgA := int(float64(FactionBaseDamage) * advantage(Element(a.Element), Element(b.Element)))
			dmgB := int(float64(FactionBaseDamage) * advantage(Element(b.Element), Element(a.Element)))
			b.HP -= dmgA
			a.HP -= dmgB
			states[i].FactionAttackCooldown = FactionCombatCooldown
			if j < len(states) {
				states[j].FactionAttackCooldown = FactionCombatCooldown
			}
			spawnClashHazard(w, a, b)
			g.broadcastEvent(Event{
				Type: "clash",
				Data: map[string]interface{}{
					"x": (a.Pos.X + b.Pos.X) / 2,
					"y": (a.Pos.Y + b.Pos.Y) / 2,
					"a": a.Element,
					"b": b.Element,
				},
			})
			// A lethal counter-hit ends the actor's tick immediately: a
			// dead actor must not hit a second rival in the same pass.
			if a.HP <= 0 {
				break
			}
		}
	}
}

// spawnClashHazard leaves a temporary elemental rift at the midpoint of a
// clash, capped so a prolonged war cannot fill the maze.
func spawnClashHazard(w *world.World, a, b *world.Minotaur) {
	if len(w.Hazards) >= MaxHazards {
		return
	}
	w.Hazards = append(w.Hazards, &world.Hazard{
		Pos:            maze.Vec2{X: (a.Pos.X + b.Pos.X) / 2, Y: (a.Pos.Y + b.Pos.Y) / 2},
		Element:        a.Element,
		TTL:            HazardTTL,
		DamageCooldown: HazardDamageInterval,
	})
}

// tickHazards ages every rift, damages anything standing in one at a bounded
// rate, and drops expired rifts. Hazards are the gang war's dynamic
// obstacles: the player must route around active crossfire.
func (g *Game) tickHazards(w *world.World) {
	alive := w.Hazards[:0]
	for _, h := range w.Hazards {
		h.TTL--
		if h.TTL <= 0 {
			continue
		}
		h.DamageCooldown--
		if h.DamageCooldown <= 0 {
			h.DamageCooldown = HazardDamageInterval
			if distSq(h.Pos, w.Runner.Pos) < HazardRadius*HazardRadius {
				w.Runner.HP -= HazardDamage
			}
			for _, m := range w.Minotaurs {
				if m.HP > 0 && distSq(h.Pos, m.Pos) < HazardRadius*HazardRadius {
					m.HP -= HazardDamage
				}
			}
		}
		alive = append(alive, h)
	}
	w.Hazards = alive
}

// tickStrategies re-evaluates each actor's rule-based strategy and sets a
// movement target: retreat home when badly hurt, raid the nearest rival when
// healthy, patrol home territory otherwise. The brain still owns pursuit when
// the player is detected.
func (g *Game) tickStrategies(w *world.World) {
	states := g.actorStates
	for i, m := range w.Minotaurs {
		if i >= len(states) || m.Stunned || m.HP <= 0 {
			continue
		}
		st := states[i]
		st.StrategyTimer--
		if st.StrategyTimer > 0 && st.HasTarget {
			continue
		}
		st.StrategyTimer = StrategyReevalTicks
		home := Element(m.Element)
		hpFrac := float64(m.HP) / float64(actorHP(ActorType(m.Type)))

		switch {
		case hpFrac < 0.35:
			st.Strategy = StrategyRetreat
			if p, ok := g.homeTerritoryPos(home); ok {
				setStrategyTarget(st, p)
			}
		case hpFrac > 0.6:
			if target, ok := nearestRival(w, i, m.Element); ok {
				st.Strategy = StrategyRaid
				setStrategyTarget(st, target)
			} else {
				st.Strategy = StrategyPatrol
				if p, ok := g.homeTerritoryPos(home); ok {
					setStrategyTarget(st, p)
				}
			}
		default:
			st.Strategy = StrategyPatrol
			if p, ok := g.homeTerritoryPos(home); ok {
				setStrategyTarget(st, p)
			}
		}
		m.Strategy = st.Strategy
	}
}

// setStrategyTarget retargets an actor and drops any cached path so the
// next movement tick replans toward the new objective.
func setStrategyTarget(st *ActorState, p maze.Vec2) {
	st.TargetPos = p
	st.HasTarget = true
	st.Path = nil
}

// nearestRival returns the position of the closest rival-faction actor to
// actor i, or false if none exists.
func nearestRival(w *world.World, i, elem int) (maze.Vec2, bool) {
	var best maze.Vec2
	bestD := -1.0
	m := w.Minotaurs[i]
	for j, o := range w.Minotaurs {
		if j == i || o.HP <= 0 || o.Element == elem {
			continue
		}
		dx := o.Pos.X - m.Pos.X
		dy := o.Pos.Y - m.Pos.Y
		d := dx*dx + dy*dy
		if bestD < 0 || d < bestD {
			bestD = d
			best = o.Pos
		}
	}
	return best, bestD >= 0
}

// applyStrategyMovement steers each actor along an A* path toward its
// strategy target while the brain owns neither pursuit nor an active
// search, at the actor's archetype speed. Direct steering is not enough:
// strategy targets routinely sit behind walls, and a straight-line step
// just wedges the actor into the wall it is steering at.
func (g *Game) applyStrategyMovement(w *world.World) {
	states := g.actorStates
	for i, m := range w.Minotaurs {
		if i >= len(states) || m.Stunned || m.HP <= 0 {
			continue
		}
		st := states[i]
		if !st.HasTarget {
			continue
		}
		// Yield exactly when the brain's own move actions (pursuit or
		// search) stepped this actor this tick: stepping here too would
		// double its speed toward a different target. Keying off actual
		// movement (not world-view flags) also means an idle planner can
		// never latch strategy movement closed.
		if i < len(g.Brains) && g.Brains[i].MovedLastTick() {
			continue
		}
		dx := st.TargetPos.X - m.Pos.X
		dy := st.TargetPos.Y - m.Pos.Y
		if dx*dx+dy*dy < 0.4*0.4 {
			st.HasTarget = false
			st.Path = nil
			continue
		}
		if len(st.Path) == 0 {
			st.Path = g.Maze.Path(m.Pos, st.TargetPos)
			if len(st.Path) == 0 {
				// Unreachable objective: drop it rather than wedge.
				st.HasTarget = false
				continue
			}
		}
		// Waypoints are open-cell centers on a 4-connected path, so
		// segment travel cannot clip a wall.
		remaining := actorSpeed(ActorType(m.Type), w.Intensity)
		for remaining > 0 && len(st.Path) > 0 {
			wp := st.Path[0]
			wx := wp.X - m.Pos.X
			wy := wp.Y - m.Pos.Y
			d := math.Hypot(wx, wy)
			if d <= remaining {
				m.Pos = wp
				st.Path = st.Path[1:]
				remaining -= d
			} else {
				m.Pos = maze.Vec2{
					X: m.Pos.X + wx/d*remaining,
					Y: m.Pos.Y + wy/d*remaining,
				}
				remaining = 0
			}
		}
	}
}

// ActorSeparation is the minimum distance between actor centers. Overlapping
// cylinders are pushed apart each tick so creatures read as physical bodies
// in a brawl instead of interpenetrating ghosts.
const ActorSeparation = 0.7

// separateActors pushes apart overlapping actor cylinders after all movement
// for the tick (brain pursuit, strategy paths) has been applied, then
// re-resolves both against the maze so a push can never shove an actor into
// a wall.
func (g *Game) separateActors(w *world.World) {
	for i := 0; i < len(w.Minotaurs); i++ {
		a := w.Minotaurs[i]
		if a.HP <= 0 {
			continue
		}
		for j := i + 1; j < len(w.Minotaurs); j++ {
			b := w.Minotaurs[j]
			if b.HP <= 0 {
				continue
			}
			dx := b.Pos.X - a.Pos.X
			dy := b.Pos.Y - a.Pos.Y
			d2 := dx*dx + dy*dy
			if d2 >= ActorSeparation*ActorSeparation {
				continue
			}
			d := math.Sqrt(d2)
			if d < 1e-9 {
				// Perfect overlap: nudge along a fixed axis so the two
				// actors separate even when their positions are identical.
				// A fixed axis keeps the nudge deterministic.
				d = 1e-9
				dx, dy = 1e-9, 0
			}
			push := (ActorSeparation - d) / 2
			ux, uy := dx/d, dy/d
			a.Pos.X -= ux * push
			a.Pos.Y -= uy * push
			b.Pos.X += ux * push
			b.Pos.Y += uy * push
			a.Pos = g.Maze.Resolve(a.Pos, maze.Radius)
			b.Pos = g.Maze.Resolve(b.Pos, maze.Radius)
		}
	}
}

// respawnDeadActors removes actors killed by the war (or the player), keeping
// the brain/history/state arrays aligned, then respawns fresh actors to hold
// TargetActorCount. The failure cap bounds the loop if the maze has no room,
// so a spawn drought can never wedge the writer goroutine.
func (g *Game) respawnDeadActors(w *world.World) {
	for i := len(w.Minotaurs) - 1; i >= 0; i-- {
		if w.Minotaurs[i].HP > 0 {
			continue
		}
		id := w.Minotaurs[i].ID
		elem := w.Minotaurs[i].Element
		w.Minotaurs = append(w.Minotaurs[:i], w.Minotaurs[i+1:]...)
		if i < len(g.actorStates) {
			g.actorStates = append(g.actorStates[:i], g.actorStates[i+1:]...)
		}
		if i < len(g.Brains) {
			g.Brains = append(g.Brains[:i], g.Brains[i+1:]...)
		}
		if i < len(g.Histories) {
			g.Histories = append(g.Histories[:i], g.Histories[i+1:]...)
		}
		g.broadcastEvent(Event{
			Type: "death",
			Data: map[string]interface{}{"id": id, "element": elem},
		})
	}
	for fails := 0; len(w.Minotaurs) < TargetActorCount && fails < TargetActorCount*4; {
		before := len(w.Minotaurs)
		g.spawnActor(ActorType(g.leastPopulatedElement(w)))
		if len(w.Minotaurs) == before {
			fails++
		}
	}
}

// leastPopulatedElement returns the element with the fewest living actors,
// breaking ties by lowest element index for determinism. This keeps all four
// factions represented on the field: the old nextActorID%4 cycle drifted
// when spawn attempts failed, collapsing rosters to one or two elements and
// killing the war (same-element actors never clash).
func (g *Game) leastPopulatedElement(w *world.World) Element {
	counts := [elementCount]int{}
	for _, m := range w.Minotaurs {
		if m.HP > 0 && m.Element >= 0 && m.Element < elementCount {
			counts[m.Element]++
		}
	}
	best := 0
	for e := 1; e < elementCount; e++ {
		if counts[e] < counts[best] {
			best = e
		}
	}
	return Element(best)
}

// territoryControl returns the element controlling each quadrant, indexed
// 0=NE, 1=NW, 2=SE, 3=SW to match the client's territoryAt. Control goes to
// the faction with the most total HP present; an empty quadrant keeps its
// innate element so the map always reads cleanly.
func territoryControl(w *world.World) [4]int {
	hp := [4][4]float64{}
	for _, m := range w.Minotaurs {
		if m.HP <= 0 {
			continue
		}
		t := territoryIndex(w.Maze, m.Pos)
		hp[t][m.Element] += float64(m.HP)
	}
	innate := [4]int{int(ElementFire), int(ElementIce), int(ElementLightning), int(ElementVoid)}
	var control [4]int
	for t := 0; t < 4; t++ {
		best := innate[t]
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
// 2=SE(lightning), 3=SW(void). It classifies by cell index with the same
// float half-width comparison the client's integer-cell territoryAt uses, so
// the center seam of an odd-sized maze reads identically on both sides.
func territoryIndex(m *maze.Maze, pos maze.Vec2) int {
	cx := int(pos.X)
	cy := int(pos.Y)
	east := float64(cx) >= float64(m.W)/2
	north := float64(cy) < float64(m.H)/2
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
