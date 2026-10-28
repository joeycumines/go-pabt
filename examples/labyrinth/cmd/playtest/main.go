//go:build example

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joeycumines/go-pabt/examples/labyrinth/game"
	"github.com/joeycumines/go-pabt/examples/labyrinth/playtest"
)

func main() {
	personaFlag := flag.String("persona", "all", "persona to run (explorer_runner, combat_hunter, stealth_baiter, element_tactician, chaos_stress, or all)")
	seedFlag := flag.Uint64("seed", 6, "deterministic maze and game seed")
	ticksFlag := flag.Int("ticks", 450, "number of 30Hz ticks to simulate per episode")
	actorsFlag := flag.Int("actors", game.TargetActorCount, "initial actor count")
	outDirFlag := flag.String("out", "", "optional directory to write report.md, report.json, and trajectory.svg")
	failOnOracle := flag.Bool("fail-on-oracle", false, "exit with code 1 if any behavioral oracle fails")
	flag.Parse()

	var personas []playtest.PersonaName
	if *personaFlag == "all" {
		personas = playtest.AllPersonas()
	} else {
		for _, part := range strings.Split(*personaFlag, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				personas = append(personas, playtest.PersonaName(part))
			}
		}
	}

	var episodes []playtest.EpisodeResult
	for _, p := range personas {
		ep := playtest.RunEpisode(playtest.Config{
			Seed:    *seedFlag,
			Persona: p,
			Ticks:   *ticksFlag,
			Actors:  *actorsFlag,
		})
		episodes = append(episodes, ep)
	}

	md := playtest.RenderMarkdownReport(episodes)
	fmt.Print(md)

	if *outDirFlag != "" {
		if err := os.MkdirAll(*outDirFlag, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "playtest: mkdir %s: %v\n", *outDirFlag, err)
			os.Exit(2)
		}
		_ = os.WriteFile(filepath.Join(*outDirFlag, "report.md"), []byte(md), 0o644)
		if data, err := json.MarshalIndent(episodes, "", "  "); err == nil {
			_ = os.WriteFile(filepath.Join(*outDirFlag, "report.json"), data, 0o644)
		}
		if len(episodes) > 0 {
			svg := playtest.RenderSVGMap(&episodes[0])
			_ = os.WriteFile(filepath.Join(*outDirFlag, "trajectory.svg"), []byte(svg), 0o644)
		}
	}

	if *failOnOracle {
		for _, ep := range episodes {
			for _, o := range ep.Oracles {
				if o.Verdict == playtest.VerdictFail {
					os.Exit(1)
				}
			}
		}
	}
}
