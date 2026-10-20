//go:build example

package playtest

import (
	"fmt"
	"math"
	"strings"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
)

// RenderMarkdownReport compiles one or more EpisodeResults into a token-efficient,
// LLM-native Markdown report containing the Oracle Scorecard, Causal Attribution Table,
// 2D ASCII Trajectory & Causality Map, and Compressed Semantic Episode Narrative.
func RenderMarkdownReport(episodes []EpisodeResult) string {
	var sb strings.Builder
	sb.WriteString("# AI Playtesting & Behavioral Verification Report\n\n")

	for idx := range episodes {
		ep := &episodes[idx]
		sb.WriteString(fmt.Sprintf("## Episode %d: Persona `%s` (Seed `%d`, Ticks `%d`)\n\n",
			idx+1, ep.Config.Persona, ep.Config.Seed, len(ep.Records)))
		sb.WriteString(fmt.Sprintf("- **Outcome**: `%s` at Round `%d` | **Runner HP**: `%d` | **Pulses Fired**: `%d` | **Stuns Inflicted**: `%d` | **Teleports Used**: `%d`\n",
			FormatPhase(ep.FinalPhase), ep.FinalRound, ep.FinalRunnerHP, ep.PulsesFired, ep.StunsInflicted, ep.TeleportsUsed))
		sb.WriteString(fmt.Sprintf("- **Movement Causality**: Total `%.1fu` | **PA-BT Brain**: `%.1fu (%.1f%%)` | **Fallback Strategy**: `%.1fu` | **Separation**: `%.1fu` | **Knockback**: `%.1fu` | **Mid-Episode Respawns**: `%d`\n",
			ep.Causality.TotalDistance, ep.Causality.BrainDistance, ep.Causality.BrainSharePct,
			ep.Causality.StrategyDistance, ep.Causality.SeparationDistance, ep.Causality.KnockbackDistance, ep.Causality.SpawnCount))
		sb.WriteString(fmt.Sprintf("- **Mortality & Archetypes**: **Faction Attrition Deaths**: `%d` | **Player Pulse Kills**: `%d` | **Caster Bolts Fired**: `%d` | **Disadvantage Ticks**: `%d`\n\n",
			ep.Causality.FactionDeaths, ep.Causality.PlayerKills, ep.Causality.CasterBoltTicks, ep.Causality.ActionTicks["Retreat"]+ep.Causality.ActionTicks["Reinforce"]))

		sb.WriteString("### 1. Behavioral & Gameplay Oracle Scorecard\n\n")
		sb.WriteString("| ID | Oracle | Verdict | Metric | Diagnostic Summary & Root Cause |\n")
		sb.WriteString("| :--- | :--- | :---: | :--- | :--- |\n")
		for _, o := range ep.Oracles {
			diag := o.Summary
			if o.RootCause != "" {
				diag += " **Root Cause:** `" + o.RootCause + "`"
			}
			sb.WriteString(fmt.Sprintf("| **%s** | `%s` | **%s** | `%s` | %s |\n",
				o.ID, o.Name, o.Verdict, o.Metric, diag))
		}
		sb.WriteString("\n")

		sb.WriteString("### 2. 2D ASCII Spatial Trajectory & Causality Map\n\n")
		sb.WriteString("Legend: `#`=Wall, `~`=Gap, `S`=Start, `G`=Gate, `@`=Runner Path, `B`=Moved by PA-BT Brain, `f`=Moved by Fallback Strategy, `!`=Zero-Move Action Latch Spin, `X`=Respawn Site\n\n")
		sb.WriteString("```text\n")
		sb.WriteString(RenderASCIIMap(ep))
		sb.WriteString("```\n\n")

		sb.WriteString("### 3. Compressed Semantic Episode Narrative (Sampled Actor Trace)\n\n")
		sb.WriteString("```text\n")
		sb.WriteString(RenderActorNarrative(ep, ""))
		sb.WriteString("```\n\n")
	}
	return sb.String()
}

// RenderASCIIMap projects the 21x21 maze and causal movement events onto a 2D character grid.
func RenderASCIIMap(ep *EpisodeResult) string {
	m := ep.Maze
	if m == nil {
		return "(no maze)\n"
	}
	grid := make([][]rune, m.H)
	for y := 0; y < m.H; y++ {
		grid[y] = make([]rune, m.W)
		for x := 0; x < m.W; x++ {
			switch m.At(x, y).Kind {
			case maze.CellWall:
				grid[y][x] = '#'
			case maze.CellGap:
				grid[y][x] = '~'
			case maze.CellPlatform:
				grid[y][x] = '^'
			default:
				grid[y][x] = ' '
			}
		}
	}

	setCell := func(p maze.Vec2, ch rune) {
		cx, cy := int(math.Floor(p.X)), int(math.Floor(p.Y))
		if cx >= 0 && cy >= 0 && cx < m.W && cy < m.H && grid[cy][cx] != '#' {
			grid[cy][cx] = ch
		}
	}

	for _, w := range ep.Records {
		for _, a := range w.Actors {
			if a.StrategyDelta > 1e-3 && a.BrainDelta < 1e-4 {
				setCell(a.EndPos, 'f')
			}
			if a.BrainDelta > 1e-3 {
				setCell(a.EndPos, 'B')
			}
			if a.Brain.ZeroMoveActionSuccess {
				setCell(a.EndPos, '!')
			}
			if a.SpawnedThisTick && w.Round > 1 {
				setCell(a.EndPos, 'X')
			}
		}
		setCell(w.RunnerPos, '@')
	}
	setCell(m.Start, 'S')
	setCell(m.Gate, 'G')

	var sb strings.Builder
	for y := 0; y < m.H; y++ {
		sb.WriteString(string(grid[y]))
		sb.WriteByte('\n')
	}
	return sb.String()
}

// RenderActorNarrative compresses 30Hz ticks for a representative actor into contiguous semantic spans.
func RenderActorNarrative(ep *EpisodeResult, targetActorID string) string {
	if len(ep.Records) == 0 {
		return "(no records)\n"
	}
	if targetActorID == "" && len(ep.Records[0].Actors) > 0 {
		bestScore := -1
		for _, a := range ep.Records[0].Actors {
			targetActorID = a.ActorID
			break
		}
		scoreByID := make(map[string]int)
		for _, w := range ep.Records {
			for _, a := range w.Actors {
				if a.Brain.PlayerDetected {
					scoreByID[a.ActorID] += 2
				}
				if a.Brain.BreaksCount > 0 || a.Brain.ZeroMoveActionSuccess {
					scoreByID[a.ActorID] += 5
				}
			}
		}
		for id, sc := range scoreByID {
			if sc > bestScore {
				bestScore = sc
				targetActorID = id
			}
		}
	}

	type spanKey struct {
		action       string
		shortCircuit bool
		detected     bool
		hasLOS       bool
		zeroSuccess  bool
		breaks       bool
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Causal Narrative for Actor %s:\n", targetActorID))

	var curKey spanKey
	var hasSpan bool
	var startRound, endRound int
	var startPos, endPos maze.Vec2
	var brainDist, stratDist float64
	var breaksTotal, zeroSuccessTotal int

	flush := func() {
		if !hasSpan {
			return
		}
		t0 := float64(startRound) / 30.0
		t1 := float64(endRound) / 30.0
		sb.WriteString(fmt.Sprintf("[Ticks %d..%d | %.2fs..%.2fs] pos=(%.1f,%.1f)->(%.1f,%.1f) | Action=%q | ShortCircuit=%v | Detected=%v (LOS=%v) | BrainMove=%.2fu | StrategyMove=%.2fu | ZeroMoveSuccess=%d | RelationBreaks=%d\n",
			startRound, endRound, t0, t1, startPos.X, startPos.Y, endPos.X, endPos.Y,
			curKey.action, curKey.shortCircuit, curKey.detected, curKey.hasLOS,
			brainDist, stratDist, zeroSuccessTotal, breaksTotal))
	}

	for _, w := range ep.Records {
		for _, a := range w.Actors {
			if a.ActorID != targetActorID {
				continue
			}
			act := a.Brain.ActiveAction
			if act == "" {
				act = "NONE"
			}
			k := spanKey{
				action:       act,
				shortCircuit: a.Brain.GoalShortCircuited,
				detected:     a.Brain.PlayerDetected,
				hasLOS:       a.Brain.HasLineOfSight,
				zeroSuccess:  a.Brain.ZeroMoveActionSuccess,
				breaks:       a.Brain.BreaksCount > 0,
			}
			if !hasSpan || k != curKey {
				flush()
				hasSpan = true
				curKey = k
				startRound = w.Round
				endRound = w.Round
				startPos = a.StartPos
				endPos = a.EndPos
				brainDist = a.BrainDelta
				stratDist = a.StrategyDelta
				breaksTotal = a.Brain.BreaksCount
				zeroSuccessTotal = 0
				if a.Brain.ZeroMoveActionSuccess {
					zeroSuccessTotal = 1
				}
			} else {
				endRound = w.Round
				endPos = a.EndPos
				brainDist += a.BrainDelta
				stratDist += a.StrategyDelta
				breaksTotal += a.Brain.BreaksCount
				if a.Brain.ZeroMoveActionSuccess {
					zeroSuccessTotal++
				}
			}
		}
	}
	flush()
	return sb.String()
}

// RenderSVGMap generates a standalone SVG top-down trajectory & causality visualization.
func RenderSVGMap(ep *EpisodeResult) string {
	m := ep.Maze
	if m == nil {
		return "<svg xmlns=\"http://www.w3.org/2000/svg\"/>"
	}
	const cellPx = 28
	width, height := m.W*cellPx, m.H*cellPx
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" style="background:#0d1117;font-family:monospace">`, width, height, width, height))
	for y := 0; y < m.H; y++ {
		for x := 0; x < m.W; x++ {
			fill := "#161b22"
			switch m.At(x, y).Kind {
			case maze.CellWall:
				fill = "#30363d"
			case maze.CellGap:
				fill = "#3d1e24"
			case maze.CellPlatform:
				fill = "#1f2937"
			}
			sb.WriteString(fmt.Sprintf(`<rect x="%d" y="%d" width="%d" height="%d" fill="%s" stroke="#21262d" stroke-width="1"/>`,
				x*cellPx, y*cellPx, cellPx, cellPx, fill))
		}
	}
	// Draw actor points color-coded by cause (red=LatchSpin, purple=Disadvantage/Recover, cyan=Brain, orange=Strategy).
	for _, w := range ep.Records {
		for _, a := range w.Actors {
			px := a.EndPos.X * float64(cellPx)
			py := a.EndPos.Y * float64(cellPx)
			if a.Brain.ZeroMoveActionSuccess {
				sb.WriteString(fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="4.0" fill="#ff4444" opacity="0.9"/>`, px, py))
			} else if a.Brain.ActiveAction == "Retreat" || a.Brain.ActiveAction == "Reinforce" || a.Brain.ActiveAction == "Recover" {
				sb.WriteString(fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="2.4" fill="#c084fc" opacity="0.75"/>`, px, py))
			} else if a.BrainDelta > 1e-3 {
				sb.WriteString(fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="2.2" fill="#00e5ff" opacity="0.7"/>`, px, py))
			} else if a.StrategyDelta > 1e-3 {
				sb.WriteString(fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="1.8" fill="#ff9900" opacity="0.5"/>`, px, py))
			}
		}
	}
	// Draw runner trajectory path
	if len(ep.Records) > 1 {
		sb.WriteString(`<polyline points="`)
		for i, w := range ep.Records {
			rx := w.RunnerPos.X * float64(cellPx)
			ry := w.RunnerPos.Y * float64(cellPx)
			if i > 0 {
				sb.WriteByte(' ')
			}
			sb.WriteString(fmt.Sprintf("%.1f,%.1f", rx, ry))
		}
		sb.WriteString(`" fill="none" stroke="#44ff66" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" opacity="0.85"/>`)
	}
	// Start (S) and Gate (G) markers
	sx := m.Start.X * float64(cellPx)
	sy := m.Start.Y * float64(cellPx)
	gx := m.Gate.X * float64(cellPx)
	gy := m.Gate.Y * float64(cellPx)
	sb.WriteString(fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="6" fill="#3fb950" stroke="#fff" stroke-width="1.5"/>`, sx, sy))
	sb.WriteString(fmt.Sprintf(`<text x="%.1f" y="%.1f" fill="#fff" font-size="10" font-weight="bold" text-anchor="middle" dominant-baseline="central">S</text>`, sx, sy))
	sb.WriteString(fmt.Sprintf(`<circle cx="%.1f" cy="%.1f" r="7" fill="#d29922" stroke="#fff" stroke-width="1.5"/>`, gx, gy))
	sb.WriteString(fmt.Sprintf(`<text x="%.1f" y="%.1f" fill="#fff" font-size="10" font-weight="bold" text-anchor="middle" dominant-baseline="central">G</text>`, gx, gy))
	sb.WriteString("</svg>")
	return sb.String()
}
