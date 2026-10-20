//go:build example

package playtest

import (
	"math"

	"github.com/joeycumines/go-pabt/examples/labyrinth/brain"
	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

// PersonaName identifies an autonomous playtest persona.
type PersonaName string

const (
	PersonaExplorerRunner   PersonaName = "explorer_runner"
	PersonaCombatHunter     PersonaName = "combat_hunter"
	PersonaStealthBaiter    PersonaName = "stealth_baiter"
	PersonaElementTactician PersonaName = "element_tactician"
	PersonaChaosStress      PersonaName = "chaos_stress"
)

// AllPersonas returns the 5 standard autonomous playtesting personas.
func AllPersonas() []PersonaName {
	return []PersonaName{
		PersonaExplorerRunner,
		PersonaCombatHunter,
		PersonaStealthBaiter,
		PersonaElementTactician,
		PersonaChaosStress,
	}
}

// PersonaController computes closed-loop player inputs at 30Hz/60Hz without teleporting.
type PersonaController interface {
	Name() PersonaName
	Step(snap *world.Snapshot, m *maze.Maze) BotAction
}

type basePersona struct {
	name       PersonaName
	baitPhase  int
	baitTarget string
	baitWait   int
	baitStart  maze.Vec2
}

// NewPersona constructs the requested PersonaController.
func NewPersona(name PersonaName, _ *maze.Maze) PersonaController {
	if name == "" {
		name = PersonaExplorerRunner
	}
	return &basePersona{name: name}
}

func (p *basePersona) Name() PersonaName { return p.name }

// walkOnlyPath computes a 4-connected BFS shortest path on Floor/Platform cells only
// (avoiding CellGap so the headless runner never gets stuck on unjumpable server gaps).
func walkOnlyPath(m *maze.Maze, start, goal maze.Vec2) []maze.Vec2 {
	sx, sy := int(math.Floor(start.X)), int(math.Floor(start.Y))
	gx, gy := int(math.Floor(goal.X)), int(math.Floor(goal.Y))
	if sx == gx && sy == gy {
		return []maze.Vec2{goal}
	}
	type pt struct{ x, y int }
	q := []pt{{sx, sy}}
	prev := make(map[pt]pt)
	visited := map[pt]bool{{sx, sy}: true}
	dirs := [4]pt{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}

	found := false
	for len(q) > 0 {
		cur := q[0]
		q = q[1:]
		if cur.x == gx && cur.y == gy {
			found = true
			break
		}
		for _, d := range dirs {
			nx, ny := cur.x+d.x, cur.y+d.y
			np := pt{nx, ny}
			if visited[np] || !m.At(nx, ny).Walkable() {
				continue
			}
			visited[np] = true
			prev[np] = cur
			q = append(q, np)
		}
	}
	if !found {
		// Fall back to m.Path if gaps are the only route.
		return m.Path(start, goal)
	}
	var out []maze.Vec2
	cur := pt{gx, gy}
	for cur != (pt{sx, sy}) {
		out = append(out, maze.Vec2{X: float64(cur.x) + 0.5, Y: float64(cur.y) + 0.5})
		cur = prev[cur]
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func steerToward(self, target maze.Vec2, sprint bool) BotAction {
	dx := target.X - self.X
	dy := target.Y - self.Y
	yaw := math.Atan2(dx, dy)
	return BotAction{
		Forward: 1.0,
		Strafe:  0,
		Yaw:     yaw,
		Pitch:   0,
		Sprint:  sprint,
	}
}

func nextWaypoint(m *maze.Maze, self, goal maze.Vec2) maze.Vec2 {
	if m == nil {
		return goal
	}
	path := m.PathWalkable(self, goal)
	for _, wp := range path {
		if math.Hypot(wp.X-self.X, wp.Y-self.Y) > 0.35 {
			return wp
		}
	}
	if len(path) > 0 {
		return path[len(path)-1]
	}
	return goal
}

func nearestLiveActor(snap *world.Snapshot) (*world.Minotaur, float64) {
	var best *world.Minotaur
	bestDist := math.MaxFloat64
	for i := range snap.Minotaurs {
		a := &snap.Minotaurs[i]
		if a.HP <= 0 {
			continue
		}
		d := math.Hypot(a.Pos.X-snap.Runner.Pos.X, a.Pos.Y-snap.Runner.Pos.Y)
		if d < bestDist {
			bestDist = d
			best = a
		}
	}
	return best, bestDist
}

func nearestVisibleActor(snap *world.Snapshot, m *maze.Maze) (*world.Minotaur, float64) {
	var best *world.Minotaur
	bestDist := math.MaxFloat64
	for i := range snap.Minotaurs {
		a := &snap.Minotaurs[i]
		if a.HP <= 0 {
			continue
		}
		d := math.Hypot(a.Pos.X-snap.Runner.Pos.X, a.Pos.Y-snap.Runner.Pos.Y)
		if d < bestDist && brain.HasClearLOS(m, snap.Runner.Pos, a.Pos) {
			bestDist = d
			best = a
		}
	}
	if best != nil {
		return best, bestDist
	}
	return nearestLiveActor(snap)
}

func (p *basePersona) Step(snap *world.Snapshot, m *maze.Maze) BotAction {
	switch p.name {
	case PersonaCombatHunter:
		return p.stepCombatHunter(snap, m)
	case PersonaStealthBaiter:
		return p.stepStealthBaiter(snap, m)
	case PersonaElementTactician:
		return p.stepElementTactician(snap, m)
	case PersonaChaosStress:
		return p.stepChaosStress(snap, m)
	default:
		return p.stepExplorerRunner(snap, m)
	}
}

func (p *basePersona) stepExplorerRunner(snap *world.Snapshot, m *maze.Maze) BotAction {
	self := snap.Runner.Pos
	nearest, dist := nearestLiveActor(snap)
	wp := nextWaypoint(m, self, snap.Gate)
	wdx := wp.X - self.X
	wdy := wp.Y - self.Y
	wn := math.Hypot(wdx, wdy)
	var vx, vy float64
	if wn > 1e-6 {
		vx, vy = wdx/wn, wdy/wn
	}
	moveYaw := math.Atan2(wdx, wdy)
	act := BotAction{
		Forward: 1.0,
		Strafe:  0.0,
		Yaw:     moveYaw,
		Pitch:   0.0,
		Sprint:  true,
		Note:    "run_gate",
	}
	isDisadvantaged := nearest != nil && (snap.Runner.Element+1)%4 == nearest.Element
	if nearest != nil && !nearest.Stunned && brain.HasClearLOS(m, self, nearest.Pos) {
		shouldFire := (dist < 2.5 && !isDisadvantaged) || (dist < 1.0 && isDisadvantaged)
		if shouldFire {
			aimYaw := math.Atan2(nearest.Pos.X-self.X, nearest.Pos.Y-self.Y)
			act.Yaw = aimYaw
			act.Forward = vx*math.Sin(aimYaw) + vy*math.Cos(aimYaw)
			act.Strafe = -vx*math.Cos(aimYaw) + vy*math.Sin(aimYaw)
			act.Fire = true
			act.Note = "fire_defensive"
		}
	}
	return act
}

func (p *basePersona) stepCombatHunter(snap *world.Snapshot, m *maze.Maze) BotAction {
	self := snap.Runner.Pos
	targetGoal := snap.Gate

	// Hunter prioritizes engaging ranged artillery (Casters) before securing the Gate.
	if p.baitWait < 20 {
		for i := range snap.Minotaurs {
			a := &snap.Minotaurs[i]
			if a.HP > 0 && a.Type == 2 {
				cDist := math.Hypot(a.Pos.X-self.X, a.Pos.Y-self.Y)
				if cDist < 12.0 {
					targetGoal = a.Pos
					break
				}
			}
		}
	}

	wp := nextWaypoint(m, self, targetGoal)
	wdx := wp.X - self.X
	wdy := wp.Y - self.Y
	wn := math.Hypot(wdx, wdy)
	var vx, vy float64
	if wn > 1e-6 {
		vx, vy = wdx/wn, wdy/wn
	}
	moveYaw := math.Atan2(wdx, wdy)

	act := BotAction{
		Forward: 1.0,
		Strafe:  0.0,
		Yaw:     moveYaw,
		Pitch:   0.0,
		Sprint:  true,
		Note:    "hunt_advance",
	}

	nearest, dist := nearestVisibleActor(snap, m)
	if nearest != nil && dist <= 4.5 && brain.HasClearLOS(m, self, nearest.Pos) {
		aimYaw := math.Atan2(nearest.Pos.X-self.X, nearest.Pos.Y-self.Y)
		act.Yaw = aimYaw
		act.Forward = vx*math.Sin(aimYaw) + vy*math.Cos(aimYaw)
		act.Strafe = -vx*math.Cos(aimYaw) + vy*math.Sin(aimYaw)
		isDisadvantaged := (snap.Runner.Element+1)%4 == nearest.Element
		fire := !nearest.Stunned
		if isDisadvantaged && dist >= 1.0 {
			fire = false
		}
		if nearest.Type == 2 && p.baitWait < 20 {
			p.baitWait++
			fire = false
		}
		act.Fire = fire
		act.Note = "hunt_engage"
	}
	return act
}

func (p *basePersona) stepStealthBaiter(snap *world.Snapshot, m *maze.Maze) BotAction {
	self := snap.Runner.Pos
	nearest, dist := nearestLiveActor(snap)
	if nearest == nil {
		return steerToward(self, nextWaypoint(m, self, snap.Gate), false)
	}
	hasLOS := brain.HasClearLOS(m, self, nearest.Pos)

	switch p.baitPhase {
	case 0: // Approach until within 4.5u and clear LOS so actor detects us cleanly.
		if dist <= 4.5 && hasLOS {
			p.baitWait++
			if p.baitWait >= 15 {
				p.baitPhase = 1
				p.baitTarget = nearest.ID
				p.baitStart = self
				p.baitWait = 0
			}
			return BotAction{Yaw: math.Atan2(nearest.Pos.X-self.X, nearest.Pos.Y-self.Y), Note: "observe_detect"}
		}
		return steerToward(self, nextWaypoint(m, self, nearest.Pos), true)
	case 1: // Advance along path toward Gate for 45 ticks while actor pursues to test dynamic tracking.
		p.baitWait++
		if p.baitWait >= 45 {
			p.baitPhase = 2
			p.baitWait = 0
		}
		return steerToward(self, nextWaypoint(m, self, snap.Gate), true)
	default: // Break LOS / hold position for 60 ticks to observe SearchAction follow-through.
		p.baitWait++
		if p.baitWait >= 60 {
			p.baitPhase = 0
			p.baitWait = 0
		}
		if hasLOS || dist < 4.0 {
			return steerToward(self, nextWaypoint(m, self, snap.Gate), true)
		}
		return BotAction{Yaw: math.Atan2(nearest.Pos.X-self.X, nearest.Pos.Y-self.Y), Note: "observe_search"}
	}
}

func (p *basePersona) stepElementTactician(snap *world.Snapshot, m *maze.Maze) BotAction {
	self := snap.Runner.Pos
	// First priority: pick up an active power-up if one is close (< 6u).
	bestPU := -1
	bestPUDist := math.MaxFloat64
	for i, pu := range snap.PowerUps {
		if !pu.Active {
			continue
		}
		d := math.Hypot(pu.Pos.X-self.X, pu.Pos.Y-self.Y)
		if d < bestPUDist {
			bestPUDist = d
			bestPU = i
		}
	}
	if bestPU >= 0 && bestPUDist < 6.0 {
		return steerToward(self, nextWaypoint(m, self, snap.PowerUps[bestPU].Pos), true)
	}
	// Second priority: approach an actor whose element we beat for up to 35 ticks
	if p.baitWait < 35 {
		wantActorElem := (snap.Runner.Element + 1) % 4
		for i := range snap.Minotaurs {
			a := &snap.Minotaurs[i]
			if a.HP <= 0 || a.Element != wantActorElem {
				continue
			}
			d := math.Hypot(a.Pos.X-self.X, a.Pos.Y-self.Y)
			p.baitWait++
			if d > 2.5 {
				return steerToward(self, nextWaypoint(m, self, a.Pos), true)
			}
			return BotAction{
				Forward: 0.5,
				Strafe:  0.0,
				Yaw:     math.Atan2(a.Pos.X-self.X, a.Pos.Y-self.Y),
				Sprint:  false,
				Fire:    false,
				Note:    "press_disadvantaged_actor",
			}
		}
	}
	// Default: advance through the maze toward Gate
	return steerToward(self, nextWaypoint(m, self, snap.Gate), true)
}

func (p *basePersona) stepChaosStress(snap *world.Snapshot, m *maze.Maze) BotAction {
	self := snap.Runner.Pos
	nearest, dist := nearestLiveActor(snap)
	angle := float64(snap.Round) * 0.25
	wp := nextWaypoint(m, self, snap.Gate)
	wdx := wp.X - self.X
	wdy := wp.Y - self.Y
	wn := math.Hypot(wdx, wdy)
	var vx, vy float64
	if wn > 1e-6 {
		vx, vy = wdx/wn, wdy/wn
	}
	moveYaw := math.Atan2(wdx, wdy)
	aimYaw := moveYaw + 0.15*math.Sin(angle)
	fire := false
	if nearest != nil && dist < 5.5 && brain.HasClearLOS(m, self, nearest.Pos) {
		aimYaw = math.Atan2(nearest.Pos.X-self.X, nearest.Pos.Y-self.Y)
		fire = snap.Round%20 == 0
	}
	fwd := vx*math.Sin(aimYaw) + vy*math.Cos(aimYaw)
	strafe := -vx*math.Cos(aimYaw) + vy*math.Sin(aimYaw) + 0.15*math.Sin(angle)
	return BotAction{
		Forward: fwd,
		Strafe:  strafe,
		Yaw:     aimYaw,
		Sprint:  true,
		Fire:    fire,
		Note:    "chaos_fuzz",
	}
}
