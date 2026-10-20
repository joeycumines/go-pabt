//go:build example

package playtest

import (
	"fmt"
	"math"

	"github.com/joeycumines/go-pabt/examples/labyrinth/game"
	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
)

// OracleVerdict represents the outcome of a behavioral oracle check.
type OracleVerdict string

const (
	VerdictPass OracleVerdict = "PASS"
	VerdictWarn OracleVerdict = "WARN"
	VerdictFail OracleVerdict = "FAIL"
)

// OracleResult is a single falsifiable behavioral or gameplay invariant check.
type OracleResult struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Verdict    OracleVerdict `json:"verdict"`
	Metric     string        `json:"metric"`
	Summary    string        `json:"summary"`
	RootCause  string        `json:"rootCause,omitempty"`
	SampleTick int           `json:"sampleTick,omitempty"`
	SampleID   string        `json:"sampleActorId,omitempty"`
}

// CausalSummary aggregates movement attribution and PA-BT health across an episode.
type CausalSummary struct {
	TotalActorTicks        int                `json:"totalActorTicks"`
	BrainDistance          float64            `json:"brainDistance"`
	StrategyDistance       float64            `json:"strategyDistance"`
	SeparationDistance     float64            `json:"separationDistance"`
	KnockbackDistance      float64            `json:"knockbackDistance"`
	TotalDistance          float64            `json:"totalDistance"`
	BrainSharePct          float64            `json:"brainSharePct"`
	GoalShortCircuitTicks  int                `json:"goalShortCircuitTicks"`
	ZeroMoveSuccessTicks   int                `json:"zeroMoveSuccessTicks"`
	RelationBreaksCount    int                `json:"relationBreaksCount"`
	RelationPreservesCount int                `json:"relationPreservesCount"`
	UnexpandedTicks        int                `json:"unexpandedTicks"`
	DetectedTicks          int                `json:"detectedTicks"`
	DetectedThroughWall    int                `json:"detectedThroughWall"`
	SpawnCount             int                `json:"spawnCount"`
	PlayerKills            int                `json:"playerKills"`
	FactionDeaths          int                `json:"factionDeaths"`
	CasterBoltTicks        int                `json:"casterBoltTicks"`
	ArchetypeActions       map[string]int     `json:"archetypeActions"`
	ActionTicks            map[string]int     `json:"actionTicks"`
	ActionDistance         map[string]float64 `json:"actionDistance"`
}

// SummarizeCausality computes CausalSummary from recorded world ticks.
func SummarizeCausality(recs []game.WorldTickRecord) CausalSummary {
	cs := CausalSummary{
		ArchetypeActions: make(map[string]int),
		ActionTicks:      make(map[string]int),
		ActionDistance:   make(map[string]float64),
	}
	for _, w := range recs {
		for _, a := range w.Actors {
			if a.HP <= 0 {
				continue
			}
			cs.TotalActorTicks++
			cs.BrainDistance += a.BrainDelta
			cs.StrategyDistance += a.StrategyDelta
			cs.SeparationDistance += a.SeparationDelta
			cs.KnockbackDistance += a.KnockbackDelta
			if a.SpawnedThisTick && w.Round > 1 {
				cs.SpawnCount++
				if a.KilledByPlayer {
					cs.PlayerKills++
				} else {
					cs.FactionDeaths++
				}
			}
			if a.Brain.GoalShortCircuited {
				cs.GoalShortCircuitTicks++
			}
			if a.Brain.ZeroMoveActionSuccess {
				cs.ZeroMoveSuccessTicks++
			}
			cs.RelationBreaksCount += a.Brain.BreaksCount
			cs.RelationPreservesCount += a.Brain.PreservesCount
			if a.Brain.UnexpandedCount > 0 {
				cs.UnexpandedTicks++
			}
			if a.Brain.PlayerDetected {
				cs.DetectedTicks++
				if !a.Brain.HasLineOfSight {
					cs.DetectedThroughWall++
				}
			}
			if a.Brain.CastBolt {
				cs.CasterBoltTicks++
			}
			if a.Brain.ArchetypeAction != "" {
				cs.ArchetypeActions[a.Brain.ArchetypeAction]++
			}
			act := a.Brain.ActiveAction
			if act == "" {
				act = "(Idle/GoalShortCircuit)"
			}
			cs.ActionTicks[act]++
			cs.ActionDistance[act] += a.BrainDelta
		}
	}
	cs.TotalDistance = cs.BrainDistance + cs.StrategyDistance + cs.SeparationDistance + cs.KnockbackDistance
	if cs.TotalDistance > 1e-6 {
		cs.BrainSharePct = (cs.BrainDistance / cs.TotalDistance) * 100.0
	}
	return cs
}

// EvaluateOracles runs all 10 behavioral and gameplay oracles on ep.
func EvaluateOracles(ep *EpisodeResult) []OracleResult {
	return []OracleResult{
		evalO1BrainCausality(ep),
		evalO2NoLatchSpin(ep),
		evalO3PlannerWiring(ep),
		evalO4DynamicPursuit(ep),
		evalO5SearchFollowThrough(ep),
		evalO6HonestPerception(ep),
		evalO7DisadvantageResolution(ep),
		evalO8RecoveryAndStunSync(ep),
		evalO9ArchetypeAndFactionHealth(ep),
		evalO10OrganicPlayability(ep),
	}
}

// O1: Verifies PA-BT brain actions drive meaningful displacement rather than short-circuiting.
func evalO1BrainCausality(ep *EpisodeResult) OracleResult {
	cs := ep.Causality
	if len(ep.Records) < 60 || cs.TotalActorTicks < 100 {
		return OracleResult{
			ID:      "O1",
			Name:    "OracleBrainCausality",
			Verdict: VerdictFail,
			Metric:  fmt.Sprintf("records=%d, totalActorTicks=%d", len(ep.Records), cs.TotalActorTicks),
			Summary: "Insufficient ticks to evaluate PA-BT causality (vacuous run).",
		}
	}
	idlePct := 0.0
	if cs.TotalActorTicks > 0 {
		idlePct = float64(cs.GoalShortCircuitTicks) / float64(cs.TotalActorTicks) * 100.0
	}
	metric := fmt.Sprintf("brainShare=%.1f%% (%.1fu/%.1fu), goalShortCircuit=%.1f%% (%d/%d ticks)",
		cs.BrainSharePct, cs.BrainDistance, cs.TotalDistance, idlePct, cs.GoalShortCircuitTicks, cs.TotalActorTicks)
	if cs.BrainSharePct < 45.0 || idlePct > 35.0 || cs.BrainDistance < 5.0 {
		return OracleResult{
			ID:        "O1",
			Name:      "OracleBrainCausality",
			Verdict:   VerdictFail,
			Metric:    metric,
			Summary:   "PA-BT Brain contributes insufficient movement; actors are mostly idle in PA-BT and moved by non-PA-BT fallback strategy or respawn.",
			RootCause: "brain/brain.go & brain/actions.go: SearchAction short-circuits when LastKnownPos is unset or unwalkable.",
		}
	}
	return OracleResult{ID: "O1", Name: "OracleBrainCausality", Verdict: VerdictPass, Metric: metric, Summary: "PA-BT Brain actively drives actor movement across the maze."}
}

// O2: Detects moveAction.done permanent latch causing 0-displacement Success spin loops or action thrashing.
func evalO2NoLatchSpin(ep *EpisodeResult) OracleResult {
	cs := ep.Causality
	if cs.TotalActorTicks < 100 {
		return OracleResult{
			ID:      "O2",
			Name:    "OracleNoLatchSpin",
			Verdict: VerdictFail,
			Metric:  fmt.Sprintf("totalActorTicks=%d", cs.TotalActorTicks),
			Summary: "Insufficient ticks to evaluate latch spin (vacuous run).",
		}
	}
	thrashTicks := 0
	var sampleTick int
	var sampleActor, sampleAction string
	for i := 1; i < len(ep.Records); i++ {
		prevActors := make(map[string]game.ActorTickRecord, len(ep.Records[i-1].Actors))
		for _, a := range ep.Records[i-1].Actors {
			prevActors[a.ActorID] = a
		}
		for _, a := range ep.Records[i].Actors {
			if a.Brain.ZeroMoveActionSuccess {
				if sampleTick == 0 {
					sampleTick = ep.Records[i].Round
					sampleActor = a.ActorID
					sampleAction = a.Brain.ActiveAction
				}
			}
			if prev, ok := prevActors[a.ActorID]; ok {
				if a.BrainDelta < 0.001 && a.Brain.ActiveAction != prev.Brain.ActiveAction &&
					a.Brain.ActiveAction != "Recover" && prev.Brain.ActiveAction != "Recover" {
					thrashTicks++
				}
			}
		}
	}
	thrashRate := 0.0
	if cs.TotalActorTicks > 0 {
		thrashRate = float64(thrashTicks) / float64(cs.TotalActorTicks)
	}
	metric := fmt.Sprintf("zeroMoveSuccess=%d ticks, thrashRate=%.3f (%d ticks), relationBreaks=%d",
		cs.ZeroMoveSuccessTicks, thrashRate, thrashTicks, cs.RelationBreaksCount)
	if cs.ZeroMoveSuccessTicks > 0 || thrashRate > 0.05 {
		return OracleResult{
			ID:         "O2",
			Name:       "OracleNoLatchSpin",
			Verdict:    VerdictFail,
			Metric:     metric,
			Summary:    fmt.Sprintf("Action latch spin or thrashing detected (zeroMove=%d, thrashRate=%.3f, sample: %s on %s).", cs.ZeroMoveSuccessTicks, thrashRate, sampleAction, sampleActor),
			RootCause:  "brain/actions.go: action returned bt.Success with 0 displacement without reaching target or thrashed between actions.",
			SampleTick: sampleTick,
			SampleID:   sampleActor,
		}
	}
	return OracleResult{ID: "O2", Name: "OracleNoLatchSpin", Verdict: VerdictPass, Metric: metric, Summary: "No zero-displacement action latch spins or plan thrashing detected."}
}

// O3: Detects failed preconditions whose State.Actions(key) candidates are rejected due to Effect key mismatch.
func evalO3PlannerWiring(ep *EpisodeResult) OracleResult {
	cs := ep.Causality
	if cs.TotalActorTicks < 100 {
		return OracleResult{
			ID:      "O3",
			Name:    "OraclePlannerWiring",
			Verdict: VerdictFail,
			Metric:  fmt.Sprintf("totalActorTicks=%d", cs.TotalActorTicks),
			Summary: "Insufficient ticks to evaluate planner wiring (vacuous run).",
		}
	}
	metric := fmt.Sprintf("unexpandedConditionTicks=%d/%d", cs.UnexpandedTicks, cs.TotalActorTicks)
	if cs.UnexpandedTicks > 0 {
		return OracleResult{
			ID:        "O3",
			Name:      "OraclePlannerWiring",
			Verdict:   VerdictFail,
			Metric:    metric,
			Summary:   "Planner left failed preconditions unexpanded because candidate action Effects() did not match the failed Condition key.",
			RootCause: "brain/state.go: State.Actions(KeyPlayerDetected) returns an action whose effect does not match KeyPlayerDetected.",
		}
	}
	return OracleResult{ID: "O3", Name: "OraclePlannerWiring", Verdict: VerdictPass, Metric: metric, Summary: "All failed preconditions have matching action effects."}
}

// O4: Verifies PursueAction dynamically tracks a moving player rather than halting at the initial detection point.
func evalO4DynamicPursuit(ep *EpisodeResult) OracleResult {
	pursueRunning := 0
	pursueStuckSuccess := 0
	var sampleActor string
	var sampleTick int
	for _, w := range ep.Records {
		for _, a := range w.Actors {
			if a.Brain.ActiveAction == "Pursue" && a.Brain.PlayerDetected {
				if a.BrainDelta > 1e-4 {
					pursueRunning++
				} else if a.Brain.ZeroMoveActionSuccess && a.Brain.PlayerDist > 1.2 {
					pursueStuckSuccess++
					if sampleActor == "" {
						sampleActor = a.ActorID
						sampleTick = w.Round
					}
				}
			}
		}
	}
	metric := fmt.Sprintf("pursueMovingTicks=%d, pursueHaltedWhileDetectedTicks=%d", pursueRunning, pursueStuckSuccess)
	if pursueMovingTicksFloor := 25; pursueRunning < pursueMovingTicksFloor {
		return OracleResult{
			ID:        "O4",
			Name:      "OracleDynamicPursuit",
			Verdict:   VerdictFail,
			Metric:    metric,
			Summary:   fmt.Sprintf("PursueAction was not exercised with sufficient displacement (%d ticks < %d floor; vacuous run).", pursueRunning, pursueMovingTicksFloor),
			RootCause: "Actors never detected or dynamically pursued the runner.",
		}
	}
	if pursueStuckSuccess > 0 {
		return OracleResult{
			ID:         "O4",
			Name:       "OracleDynamicPursuit",
			Verdict:    VerdictFail,
			Metric:     metric,
			Summary:    "Pursuing actor reached the initial detection point and halted permanently (0.00u/tick) while the detected player kept moving.",
			RootCause:  "brain/actions.go: PursueAction computes a.path only once and never updates the path as PlayerPos changes.",
			SampleTick: sampleTick,
			SampleID:   sampleActor,
		}
	}
	return OracleResult{ID: "O4", Name: "OracleDynamicPursuit", Verdict: VerdictPass, Metric: metric, Summary: "Pursuing actors dynamically track moving targets."}
}

// O5: Verifies SearchAction actually moves toward LastKnownPos after LOS is lost before setting SearchExhausted.
func evalO5SearchFollowThrough(ep *EpisodeResult) OracleResult {
	searchTicks := ep.Causality.ActionTicks["Search"]
	searchDist := ep.Causality.ActionDistance["Search"]
	zeroSearch := 0
	for _, w := range ep.Records {
		for _, a := range w.Actors {
			if a.Brain.ActiveAction == "Search" && a.Brain.ZeroMoveActionSuccess {
				zeroSearch++
			}
		}
	}
	metric := fmt.Sprintf("searchTicks=%d, searchDistance=%.2fu, zeroMoveSearchSuccess=%d", searchTicks, searchDist, zeroSearch)
	if searchTicks < 20 || searchDist < 2.0 {
		return OracleResult{
			ID:        "O5",
			Name:      "OracleSearchFollowThrough",
			Verdict:   VerdictFail,
			Metric:    metric,
			Summary:   fmt.Sprintf("SearchAction was not exercised with sufficient follow-through (ticks=%d < 20, dist=%.2fu < 2.0u; vacuous run).", searchTicks, searchDist),
			RootCause: "brain/actions.go: SearchAction must actively traverse paths toward last-known or patrol waypoints.",
		}
	}
	if zeroSearch > 0 {
		return OracleResult{
			ID:        "O5",
			Name:      "OracleSearchFollowThrough",
			Verdict:   VerdictFail,
			Metric:    metric,
			Summary:   "SearchAction completed with zero movement without walking to destination.",
			RootCause: "brain/actions.go: SearchAction latches a.done=true with 0 displacement.",
		}
	}
	return OracleResult{ID: "O5", Name: "OracleSearchFollowThrough", Verdict: VerdictPass, Metric: metric, Summary: "SearchAction traverses paths to LastKnownPos."}
}

// O6: Checks whether PlayerDetected respects maze wall occlusion.
func evalO6HonestPerception(ep *EpisodeResult) OracleResult {
	cs := ep.Causality
	wallPct := 0.0
	if cs.DetectedTicks > 0 {
		wallPct = float64(cs.DetectedThroughWall) / float64(cs.DetectedTicks) * 100.0
	}
	metric := fmt.Sprintf("detectedTicks=%d, detectedThroughWalls=%d (%.1f%%)", cs.DetectedTicks, cs.DetectedThroughWall, wallPct)
	if cs.DetectedTicks < 15 {
		return OracleResult{
			ID:        "O6",
			Name:      "OracleHonestPerception",
			Verdict:   VerdictFail,
			Metric:    metric,
			Summary:   fmt.Sprintf("Player was never detected sufficiently during episode (%d ticks < 15 floor; vacuous run).", cs.DetectedTicks),
			RootCause: "Episode ran without adequate player-actor perception engagement.",
		}
	}
	if cs.DetectedThroughWall > 0 {
		return OracleResult{
			ID:        "O6",
			Name:      "OracleHonestPerception",
			Verdict:   VerdictFail,
			Metric:    metric,
			Summary:   fmt.Sprintf("%d ticks of PlayerDetected occurred through solid stone walls without clear line of sight.", cs.DetectedThroughWall),
			RootCause: "brain/brain.go: UpdateWorldView must strictly require HasClearLOS through walkable maze cells.",
		}
	}
	return OracleResult{ID: "O6", Name: "OracleHonestPerception", Verdict: VerdictPass, Metric: metric, Summary: "Actor perception strictly respects maze wall occlusion."}
}

// O7: Verifies RetreatAction / ReinforceAction can resolve elemental disadvantage without boundary contradiction.
func evalO7DisadvantageResolution(ep *EpisodeResult) OracleResult {
	retreatTicks := ep.Causality.ActionTicks["Retreat"]
	reinforceTicks := ep.Causality.ActionTicks["Reinforce"]
	retreatDist := ep.Causality.ActionDistance["Retreat"]
	reinforceDist := ep.Causality.ActionDistance["Reinforce"]
	disadvantageDetectedTicks := 0
	pursueWhileDisadvantaged := 0
	retreatSuccess := 0
	retreatAbortedAtBoundary := 0
	for i := 1; i < len(ep.Records); i++ {
		prevMap := make(map[string]game.ActorTickRecord, len(ep.Records[i-1].Actors))
		for _, a := range ep.Records[i-1].Actors {
			prevMap[a.ActorID] = a
		}
		for _, a := range ep.Records[i].Actors {
			if a.Brain.AtDisadvantage && a.Brain.PlayerDetected {
				disadvantageDetectedTicks++
				if a.Brain.ActiveAction == "Pursue" {
					pursueWhileDisadvantaged++
				}
			}
			prev, ok := prevMap[a.ActorID]
			if !ok {
				continue
			}
			if prev.Brain.ActiveAction == "Retreat" {
				if a.Brain.ActiveAction == "Retreat" && a.Brain.ActionStatus == 2 {
					retreatSuccess++
				} else if !a.Brain.PlayerDetected && a.Brain.AtDisadvantage && prev.Brain.PlayerDist <= 6.0 {
					retreatAbortedAtBoundary++
				}
			}
		}
	}
	metric := fmt.Sprintf("disadvantageTicks=%d, pursueWhileDisadvantaged=%d, retreatTicks=%d (dist=%.2fu), reinforceTicks=%d (dist=%.2fu)",
		disadvantageDetectedTicks, pursueWhileDisadvantaged, retreatTicks, retreatDist, reinforceTicks, reinforceDist)
	if disadvantageDetectedTicks < 10 {
		return OracleResult{
			ID:        "O7",
			Name:      "OracleDisadvantageResolution",
			Verdict:   VerdictFail,
			Metric:    metric,
			Summary:   fmt.Sprintf("Elemental disadvantage was not exercised sufficiently (%d ticks < 10 floor; vacuous run).", disadvantageDetectedTicks),
			RootCause: "Actors at elemental disadvantage against runner never engaged or detected the runner.",
		}
	}
	if pursueWhileDisadvantaged > 0 {
		return OracleResult{
			ID:        "O7",
			Name:      "OracleDisadvantageResolution",
			Verdict:   VerdictFail,
			Metric:    metric,
			Summary:   fmt.Sprintf("Actors at disadvantage pursued the advantaged runner for %d ticks instead of retreating or reinforcing.", pursueWhileDisadvantaged),
			RootCause: "brain/state.go & brain/actions.go: KeyAtDisadvantage precondition must prevent PursueAction when disadvantaged.",
		}
	}
	if (retreatTicks+reinforceTicks) < 10 || (retreatDist+reinforceDist) < 1.0 {
		return OracleResult{
			ID:        "O7",
			Name:      "OracleDisadvantageResolution",
			Verdict:   VerdictFail,
			Metric:    metric,
			Summary:   fmt.Sprintf("Disadvantage actions moved insufficiently (ticks=%d < 10, dist=%.2fu < 1.0u).", retreatTicks+reinforceTicks, retreatDist+reinforceDist),
			RootCause: "brain/actions.go: ReinforceAction or RetreatAction failed to displace the actor away from danger.",
		}
	}
	if retreatAbortedAtBoundary > 0 || (reinforceTicks > 5 && reinforceDist < 0.1) {
		return OracleResult{
			ID:        "O7",
			Name:      "OracleDisadvantageResolution",
			Verdict:   VerdictFail,
			Metric:    metric,
			Summary:   "Disadvantage actions failed to resolve cleanly: RetreatAction aborted at boundary or ReinforceAction latched with zero displacement.",
			RootCause: "brain/actions.go: RetreatAction safe distance exceeds sight radius or ReinforceAction latches moveAction.done.",
		}
	}
	return OracleResult{ID: "O7", Name: "OracleDisadvantageResolution", Verdict: VerdictPass, Metric: metric, Summary: "Disadvantage actions resolve cleanly with genuine displacement."}
}

// O8: Verifies RecoverAction & Stun synchronization between game.tickTimers and brain.Tick.
func evalO8RecoveryAndStunSync(ep *EpisodeResult) OracleResult {
	if len(ep.Records) < 60 {
		return OracleResult{
			ID:        "O8",
			Name:      "OracleRecoveryAndStunSync",
			Verdict:   VerdictFail,
			Metric:    fmt.Sprintf("records=%d", len(ep.Records)),
			Summary:   "Insufficient ticks to evaluate recovery and stun synchronization (vacuous run).",
			RootCause: "Episode too short to observe stun recovery dynamics.",
		}
	}
	stunnedActorTicks := 0
	recoverBrainTicks := ep.Causality.ActionTicks["Recover"]
	for _, w := range ep.Records {
		for _, a := range w.Actors {
			if a.Stunned && a.HP > 0 {
				stunnedActorTicks++
			}
		}
	}
	metric := fmt.Sprintf("pulsesFired=%d, stunnedWorldTicks=%d, recoverActionTicks=%d", ep.PulsesFired, stunnedActorTicks, recoverBrainTicks)
	if ep.PulsesFired > 0 && (stunnedActorTicks < 15 || recoverBrainTicks < 12) {
		return OracleResult{
			ID:        "O8",
			Name:      "OracleRecoveryAndStunSync",
			Verdict:   VerdictFail,
			Metric:    metric,
			Summary:   fmt.Sprintf("Stun and recovery were not exercised despite pulses fired (stunnedTicks=%d < 15, recoverTicks=%d < 12; vacuous run).", stunnedActorTicks, recoverBrainTicks),
			RootCause: "Pulses failed to hit and stun actors, or RecoverAction desynchronized from stun duration.",
		}
	}
	if stunnedActorTicks > 0 && recoverBrainTicks < int(float64(stunnedActorTicks)*0.8) {
		return OracleResult{
			ID:        "O8",
			Name:      "OracleRecoveryAndStunSync",
			Verdict:   VerdictFail,
			Metric:    metric,
			Summary:   fmt.Sprintf("Actors were stunned for %d ticks, but RecoverAction only ran for %d ticks (<80%% sync).", stunnedActorTicks, recoverBrainTicks),
			RootCause: "brain/actions.go: RecoverAction desynchronizes from world.StunDuration.",
		}
	}
	return OracleResult{ID: "O8", Name: "OracleRecoveryAndStunSync", Verdict: VerdictPass, Metric: metric, Summary: "Stun duration and RecoverAction are synchronized."}
}

// O9: Audits archetype differentiation (Brute, Swarm, Caster, Stalker) and faction respawn churn.
func evalO9ArchetypeAndFactionHealth(ep *EpisodeResult) OracleResult {
	factionDeaths := ep.Causality.FactionDeaths
	playerKills := ep.Causality.PlayerKills
	distinctBehaviors := len(ep.Causality.ArchetypeActions)
	bolts := ep.Causality.CasterBoltTicks
	const maxAllowedFactionDeaths = 6
	metric := fmt.Sprintf("factionDeaths=%d (max=%d), playerKills=%d, distinctArchetypeBehaviors=%d, casterRangedTicks=%d",
		factionDeaths, maxAllowedFactionDeaths, playerKills, distinctBehaviors, bolts)
	if factionDeaths > maxAllowedFactionDeaths {
		return OracleResult{
			ID:        "O9",
			Name:      "OracleArchetypeAndFactionHealth",
			Verdict:   VerdictFail,
			Metric:    metric,
			Summary:   fmt.Sprintf("Excessive NPC-on-NPC faction suicide churn (%d faction deaths > %d max cap).", factionDeaths, maxAllowedFactionDeaths),
			RootCause: "game/faction_war.go: Faction raiders stack and suicide-loop rather than retreating when hurt.",
		}
	}
	if distinctBehaviors < 4 {
		return OracleResult{
			ID:        "O9",
			Name:      "OracleArchetypeAndFactionHealth",
			Verdict:   VerdictFail,
			Metric:    metric,
			Summary:   fmt.Sprintf("Missing archetype behavioral differentiation (only %d/4 archetypes active).", distinctBehaviors),
			RootCause: "brain/actions.go: Brute, Swarm, Caster, and Stalker archetypes must execute distinct tactical behaviors.",
		}
	}
	if bolts < 1 {
		return OracleResult{
			ID:        "O9",
			Name:      "OracleArchetypeAndFactionHealth",
			Verdict:   VerdictFail,
			Metric:    metric,
			Summary:   "Casters never fired ranged elemental bolts.",
			RootCause: "brain/actions.go: Caster archetype must maintain standoff range [2.0u, 5.5u] and cast bolts.",
		}
	}
	return OracleResult{ID: "O9", Name: "OracleArchetypeAndFactionHealth", Verdict: VerdictPass, Metric: metric, Summary: "Faction combat pacing and all 4 archetype behaviors are healthy."}
}

// O10: Audits Maze.PathWalkable for CellGap routing & start-cell backward steps, and verifies zero-teleport organic playability.
func evalO10OrganicPlayability(ep *EpisodeResult) OracleResult {
	m := ep.Maze
	gapCellsInMaze := 0
	gapRouted := 0
	startCellIncluded := 0
	if m != nil {
		for y := 1; y < m.H-1; y++ {
			for x := 1; x < m.W-1; x++ {
				if m.At(x, y).Kind == maze.CellGap {
					gapCellsInMaze++
				}
			}
		}
		p := m.PathWalkable(m.Start, m.Gate)
		if len(p) > 0 {
			if math.Hypot(p[0].X-m.Start.X, p[0].Y-m.Start.Y) < 1e-6 {
				startCellIncluded = 1
			}
			for _, wp := range p {
				if m.At(int(math.Floor(wp.X)), int(math.Floor(wp.Y))).Kind == maze.CellGap {
					gapRouted++
				}
			}
		}
	}
	runnerMoved := 0.0
	uniqueCells := make(map[[2]int]bool)
	minGateDist := math.MaxFloat64
	initialGateDist := 0.0
	if len(ep.Records) > 0 && m != nil {
		initialGateDist = math.Hypot(ep.Records[0].RunnerPos.X-m.Gate.X, ep.Records[0].RunnerPos.Y-m.Gate.Y)
		for i := 0; i < len(ep.Records); i++ {
			pos := ep.Records[i].RunnerPos
			cell := [2]int{int(math.Floor(pos.X)), int(math.Floor(pos.Y))}
			uniqueCells[cell] = true
			d := math.Hypot(pos.X-m.Gate.X, pos.Y-m.Gate.Y)
			if d < minGateDist {
				minGateDist = d
			}
			if i > 0 {
				runnerMoved += math.Hypot(pos.X-ep.Records[i-1].RunnerPos.X, pos.Y-ep.Records[i-1].RunnerPos.Y)
			}
		}
	}
	progress := 0.0
	if initialGateDist > 1e-6 {
		progress = (initialGateDist - minGateDist) / initialGateDist
	}
	metric := fmt.Sprintf("teleports=%d, runnerMoved=%.1fu, uniqueCells=%d, gateProgress=%.1f%%, npcPathIncludesStartCell=%d, gapsInMaze=%d (npcRoutedOverGap=%d)",
		ep.TeleportsUsed, runnerMoved, len(uniqueCells), progress*100, startCellIncluded, gapCellsInMaze, gapRouted)
	if ep.TeleportsUsed > 0 {
		return OracleResult{
			ID:        "O10",
			Name:      "OracleOrganicPlayability",
			Verdict:   VerdictFail,
			Metric:    metric,
			Summary:   fmt.Sprintf("Runner used %d teleport cheats.", ep.TeleportsUsed),
			RootCause: "Playtest runner must navigate organically without DebugTeleport.",
		}
	}
	if startCellIncluded > 0 || gapRouted > 0 {
		return OracleResult{
			ID:        "O10",
			Name:      "OracleOrganicPlayability",
			Verdict:   VerdictFail,
			Metric:    metric,
			Summary:   "NPC pathfinder includes actor's current cell center as waypoint[0] or routes non-jumping NPCs across CellGap.",
			RootCause: "maze/astar.go: PathWalkable must avoid CellGap and trim start-cell waypoints.",
		}
	}
	if len(ep.Records) < 60 {
		return OracleResult{
			ID:        "O10",
			Name:      "OracleOrganicPlayability",
			Verdict:   VerdictFail,
			Metric:    metric,
			Summary:   fmt.Sprintf("Insufficient ticks to evaluate organic playability (records=%d < 60; vacuous run).", len(ep.Records)),
			RootCause: "Episode too short to evaluate organic exploration toward Gate.",
		}
	}
	if runnerMoved < 1.0 || len(uniqueCells) < 15 || progress < 0.40 {
		return OracleResult{
			ID:        "O10",
			Name:      "OracleOrganicPlayability",
			Verdict:   VerdictFail,
			Metric:    metric,
			Summary:   fmt.Sprintf("Runner made insufficient exploration progress (moved=%.1fu, uniqueCells=%d < 15, progress=%.1f%% < 40%%).", runnerMoved, len(uniqueCells), progress*100),
			RootCause: "playtest/personas.go: bot persona got stuck in dead end or failed to navigate toward Gate.",
		}
	}
	if ep.Config.Persona == PersonaExplorerRunner && ep.FinalPhase != 1 { // world.PhaseWon = 1
		return OracleResult{
			ID:        "O10",
			Name:      "OracleOrganicPlayability",
			Verdict:   VerdictFail,
			Metric:    metric,
			Summary:   fmt.Sprintf("ExplorerRunner failed to reach Gate and WIN (final phase %s at round %d).", FormatPhase(ep.FinalPhase), ep.FinalRound),
			RootCause: "ExplorerRunner must reach the Gate organically and achieve PhaseWon.",
		}
	}
	return OracleResult{ID: "O10", Name: "OracleOrganicPlayability", Verdict: VerdictPass, Metric: metric, Summary: "Organic navigation and walkable NPC pathfinding invariants satisfied."}
}
