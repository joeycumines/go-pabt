//go:build example

package playtest

import (
	"strings"
	"testing"

	"github.com/joeycumines/go-pabt/examples/labyrinth/brain"
	"github.com/joeycumines/go-pabt/examples/labyrinth/game"
	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

func TestAllOraclesPassAcrossAllPersonas(t *testing.T) {
	for _, p := range AllPersonas() {
		cfg := Config{
			Seed:    444004,
			Persona: p,
			Ticks:   450,
			Actors:  game.TargetActorCount,
		}
		ep := RunEpisode(cfg)
		t.Logf("persona %s ActionTicks: %+v", p, ep.Causality.ActionTicks)
		if len(ep.Records) == 0 {
			t.Fatalf("persona %s produced 0 telemetry records", p)
		}
		if ep.TeleportsUsed != 0 {
			t.Fatalf("persona %s used teleports: %d", p, ep.TeleportsUsed)
		}
		if len(ep.Oracles) != 10 {
			t.Fatalf("persona %s: expected 10 behavioral oracles, got %d", p, len(ep.Oracles))
		}
		for _, o := range ep.Oracles {
			if o.Verdict != VerdictPass {
				t.Errorf("persona %s: expected oracle %s (%s) to PASS, got %s (metric: %s | summary: %s)",
					p, o.ID, o.Name, o.Verdict, o.Metric, o.Summary)
			}
		}

		md := RenderMarkdownReport([]EpisodeResult{ep})
		if !strings.Contains(md, "OracleBrainCausality") || !strings.Contains(md, "Causal Narrative for Actor") {
			t.Errorf("persona %s: markdown report missing expected sections", p)
		}
		svg := RenderSVGMap(&ep)
		if !strings.HasPrefix(svg, "<svg") {
			t.Errorf("persona %s: expected valid SVG map", p)
		}
	}
}

func TestExplorerRunnerWinsGameOrganicallyWithoutTeleports(t *testing.T) {
	ep := RunEpisode(Config{
		Seed:    444004,
		Persona: PersonaExplorerRunner,
		Ticks:   600,
		Actors:  8,
	})
	if ep.TeleportsUsed != 0 {
		t.Fatalf("expected 0 teleports, got %d", ep.TeleportsUsed)
	}
	if ep.FinalPhase != world.PhaseWon {
		t.Fatalf("expected explorer_runner to organically reach Gate and WIN (PhaseWon), got %s at round %d",
			FormatPhase(ep.FinalPhase), ep.FinalRound)
	}
}

func TestOraclesCatchInjectedRegressions(t *testing.T) {
	// Construct a synthetic EpisodeResult exhibiting the legacy pathologies to prove
	// the 10 Oracles remain sharp falsifiers and cannot pass vacuously.
	recs := make([]game.WorldTickRecord, 20)
	for i := range recs {
		recs[i] = game.WorldTickRecord{
			Round: i + 1,
			Actors: []game.ActorTickRecord{
				{
					Round:           i + 1,
					ActorID:         "B-1",
					HP:              100,
					Stunned:         true,
					StrategyDelta:   0.15,
					SpawnedThisTick: i > 1 && i <= 10,
					Brain: brain.TickTelemetry{
						ActorID:               "B-1",
						ActiveAction:          "Pursue",
						GoalShortCircuited:    true,
						ZeroMoveActionSuccess: true,
						BreaksCount:           2,
						UnexpandedCount:       1,
						PlayerDetected:        true,
						PlayerDist:            3.5,
						HasLineOfSight:        false,
					},
				},
			},
		}
	}
	ep := EpisodeResult{
		TeleportsUsed: 1,
		Records:       recs,
		Causality:     SummarizeCausality(recs),
	}
	oracles := EvaluateOracles(&ep)
	byID := make(map[string]OracleResult, len(oracles))
	for _, o := range oracles {
		byID[o.ID] = o
	}
	for _, id := range []string{"O1", "O2", "O3", "O4", "O5", "O6", "O7", "O8", "O9", "O10"} {
		if byID[id].Verdict != VerdictFail {
			t.Errorf("expected injected regression to trigger FAIL on oracle %s, got %s (%s)",
				id, byID[id].Verdict, byID[id].Metric)
		}
	}
}
