// Copyright 2021 Joseph Cumines
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build example
// +build example

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt/examples/harbor-panic/logic"
	hsim "github.com/joeycumines/go-pabt/examples/harbor-panic/sim"
	"github.com/joeycumines/go-pabt/examples/harbor-panic/tui"
	"github.com/joeycumines/go-pabt/pabtdebug"
)

func main() {
	var (
		debugAddr     string
		tickMs        int
		overflowPath  string
		stormEvery    int
		humanSpeed    float64
		headless      bool
		burst         int
		seedBP        string
		ticks         int
		useTUI        bool
		scenario      string
		rankByCost    bool
		beliefFlag    bool
		snapshotEvery int
		fleetFlag     bool
	)
	flag.StringVar(&debugAddr, "debug", "", "start debug server at address (e.g. :8080)")
	flag.IntVar(&tickMs, "tick-ms", 80, "milliseconds between ticks")
	flag.StringVar(&overflowPath, "overflow", "", "path for overflow JSONL persistence file")
	flag.IntVar(&stormEvery, "storm-every", 120, "ticks between goal relocations (0=disabled)")
	flag.Float64Var(&humanSpeed, "human-speed", 3.1, "human forklift speed per tick")
	flag.BoolVar(&headless, "headless", false, "run without tcell UI")
	flag.IntVar(&burst, "burst", 0, "rapidly tick N times then stop (implies --headless)")
	flag.StringVar(&seedBP, "seed-breakpoint", "", "pre-seed breakpoint node path (e.g. 0.0.1)")
	flag.IntVar(&ticks, "ticks", 0, "max ticks to run (0=infinite)")
	flag.BoolVar(&useTUI, "tui", false, "run interactive tcell TUI (auto-disables with --headless/--burst or missing TERM)")
	flag.StringVar(&scenario, "scenario", "", "harbor scenario: calm|stormy|maze (overrides storm/human defaults)")
	flag.BoolVar(&rankByCost, "rank-by-cost", false, "rank harbor Actions by explicit cost model (demo of accidental ordering)")
	flag.BoolVar(&beliefFlag, "belief", false, "enable belief partial-observability (hidden containers with sensing)")
	flag.IntVar(&snapshotEvery, "snapshot-every", 1, "record a debug snapshot every Nth tick (1=every tick; >1 cuts per-tick Track CPU/RSS for sustained UI sessions)")
	flag.BoolVar(&fleetFlag, "fleet", false, "spawn 3 harbors x 4 actors = 12 concurrent plans for fleet stress")
	flag.Parse()

	if burst > 0 {
		headless = true
		if tickMs == 80 {
			tickMs = 1
		}
	}
	if rankByCost {
		// propagated via logic global
		logic.SetRankByCost(true)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, os.Kill)
	go func() {
		<-signals
		cancel()
	}()

	var harbors []*hsim.Harbor
	if fleetFlag {
		for _, m := range hsim.NewFleet(3, time.Now().UnixNano()) {
			harbors = append(harbors, m.Harbor)
		}
	} else {
		harbor := hsim.NewHarbor(stormEvery, humanSpeed, time.Now().UnixNano())
		if scenario != "" {
			if err := hsim.ApplyScenario(harbor, scenario); err != nil {
				log.Fatalf("%v", err)
			}
		}
		if beliefFlag {
			harbor.EnableBelief(true)
		}
		harbors = append(harbors, harbor)
	}
	for _, h := range harbors {
		if n := len(h.State().Actors()); n < 4 {
			log.Fatalf("expected 4 actors, got %d", n)
		}
	}

	if debugAddr != "" {
		// Cap RSS for sustained debug sessions: trade GC CPU for a bounded heap.
		debug.SetGCPercent(50)
		debug.SetMemoryLimit(256 << 20)
	}

	var debugServer *pabtdebug.Server
	var trackers []*pabtdebug.Tracker
	var planNodes []bt.Node

	planIdx := 0
	for harborIdx, h := range harbors {
		for i, actor := range h.State().Actors() {
			var planID string
			if fleetFlag {
				planID = hsim.FleetPlanID(harborIdx, i)
			} else {
				planID = fmt.Sprintf("harbor-bot-%d", i)
			}
			result := logic.HarborPlan(ctx, h, actor.ID)
			planNode := result.Node

			tracker := pabtdebug.NewTrackerWithID(result.Plan, planID)
			if overflowPath != "" {
				tracker.SetOverflowFile(fmt.Sprintf("%s.%s", overflowPath, planID))
				tracker.WithKeyframeInterval(20)
				tracker.WithMaxEvents(1000)
				tracker.WithMaxTrees(100)
			}
			if seedBP != "" && planIdx == 0 {
				tracker.SetBreakpoint(pabtdebug.Breakpoint{
					NodePath:  seedBP,
					Condition: "Running",
					Enabled:   true,
				})
			}
			trackers = append(trackers, tracker)
			planNodes = append(planNodes, planNode)

			if debugAddr != "" {
				if planIdx == 0 {
					debugServer = pabtdebug.NewServer(tracker, debugAddr)
					go func() {
						log.Printf("debug server: %s", debugServer.Start())
					}()
				} else {
					debugServer.RegisterTracker(tracker)
				}
			}
			planIdx++
		}
	}

	// Safety endpoint: exposes live safety counters for verify_safety.sh
	if debugAddr != "" && debugServer != nil {
		debugServer.HandleFunc("/debug/pabt/safety", func(w http.ResponseWriter, r *http.Request) {
			var sv, cy, de int
			for _, h := range harbors {
				sv += h.SafetyViolations()
				cy += h.Cycles()
				de += h.DeadEnds()
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"safetyViolations": sv,
				"cycles":           cy,
				"deadEnds":         de,
			})
		})
	}

	// Manager + tickers for non-burst mode; burst mode uses linearized synchronous pipeline
	var manager bt.Manager
	managerDone := make(chan struct{})
	var wg sync.WaitGroup
	wgDone := make(chan struct{})
	var simPaused atomic.Bool
	stepCh := make(chan struct{}, 1)
	var simSpeed atomic.Value
	simSpeed.Store(1.0)

	if burst > 0 {
		// Linearized burst pipeline: exactly burst iterations, each steps all harbors + Track per plan
		// No manager/wg in burst mode; channels remain open until tail handles them
		go func() {
			ticked := 0
			for ticked < burst {
				select {
				case <-ctx.Done():
					return
				default:
				}
				if ticks > 0 && ticked >= ticks {
					log.Printf("tick limit reached: %d", ticked)
					cancel()
					return
				}
				for _, h := range harbors {
					if err := h.Step(ctx); err != nil {
						if ctx.Err() != nil {
							return
						}
						log.Printf("harbor step error: %v", err)
					}
				}
				for idx, pn := range planNodes {
					status, err := pn.Tick()
					trackers[idx].Track(status, err)
				}
				ticked++
				if ticked%500 == 0 {
					log.Printf("harbor tick %d/%d", ticked, burst)
				}
			}
			log.Printf("burst complete: %d ticks", ticked)
			cancel()
		}()
	} else {
		manager = bt.NewManager()
		go func() {
			defer close(managerDone)
			<-manager.Done()
			if err := manager.Err(); err != nil && ctx.Err() == nil {
				log.Printf("bt manager error: %v", err)
			}
		}()
		wg.Add(len(planNodes))
		go func() {
			wg.Wait()
			close(wgDone)
		}()
		for idx := range planNodes {
			pID := trackers[idx].ID()
			tr := trackers[idx]
			pn := planNodes[idx]
			origNode := pn
			var snapCount int64
			wrappedNode := bt.New(func(children []bt.Node) (bt.Status, error) {
				status, err := origNode.Tick()
				snapCount++
				if snapshotEvery <= 1 || snapCount%int64(snapshotEvery) == 0 {
					tr.Track(status, err)
				}
				return status, err
			})
			ticker := bt.NewTicker(ctx, time.Duration(tickMs)*time.Millisecond, bt.New(
				bt.Not(bt.All),
				bt.New(
					bt.Selector,
					bt.New(
						bt.Sequence,
						wrappedNode,
						bt.New(func([]bt.Node) (bt.Status, error) {
							return bt.Success, nil
						}),
					),
					bt.New(func([]bt.Node) (bt.Status, error) {
						return bt.Failure, nil
					}),
				),
			))
			if err := manager.Add(ticker); err != nil {
				log.Fatalf("manager add %s: %v", pID, err)
			}
			go func(name string) {
				defer wg.Done()
				<-ticker.Done()
				if err := ticker.Err(); err != nil && ctx.Err() == nil {
					log.Printf("plan error %s: %v", name, err)
				}
			}(pID)
		}
		go func() {
			baseInterval := time.Duration(tickMs) * time.Millisecond
			ticked := 0
			for {
				if ticks > 0 && ticked >= ticks {
					log.Printf("tick limit reached: %d", ticked)
					cancel()
					return
				}
				if simPaused.Load() {
					select {
					case <-ctx.Done():
						return
					case <-stepCh:
						for _, h := range harbors {
							if err := h.Step(ctx); err != nil {
								if ctx.Err() != nil {
									return
								}
								log.Printf("harbor step error: %v", err)
							}
						}
						ticked++
						continue
					case <-time.After(50 * time.Millisecond):
						continue
					}
				}
				select {
				case <-ctx.Done():
					return
				default:
				}
				for _, h := range harbors {
					if err := h.Step(ctx); err != nil {
						if ctx.Err() != nil {
							return
						}
						log.Printf("harbor step error: %v", err)
					}
				}
				ticked++
				if headless && ticked%100 == 0 {
					log.Printf("harbor tick %d", ticked)
				}
				if baseInterval > 0 {
					speed := simSpeed.Load().(float64)
					adjusted := time.Duration(float64(baseInterval) / speed)
					if adjusted < time.Millisecond {
						adjusted = time.Millisecond
					}
					time.Sleep(adjusted)
				}
			}
		}()
	}

	useTUIEffective := useTUI && !headless && burst == 0 && ticks == 0
	if useTUI && headless {
		log.Printf("ignoring --tui (headless/burst active)")
		useTUIEffective = false
	}
	if useTUIEffective && os.Getenv("TERM") == "" {
		log.Printf("ignoring --tui (TERM not set)")
		useTUIEffective = false
	}
	if !headless && !useTUIEffective {
		fmt.Println("Harbor Panic running. Press Ctrl+C to stop.")
		fmt.Printf("Plans: %d, Debug: %q\n", len(planNodes), debugAddr)
	}

	if useTUIEffective {
		fmt.Println("Harbor Panic TUI mode: press q to quit, p pause, s step, +/- speed")
		tuiCtx, tuiCancel := context.WithCancel(ctx)
		defer tuiCancel()
		if err := tui.Run(tuiCtx, harbors[0], trackers, tui.Options{TickMs: tickMs, Paused: &simPaused, StepCh: stepCh, SpeedMultiplier: &simSpeed, DebugAddr: debugAddr, OverflowPath: overflowPath, Scenario: scenario}); err != nil {
			log.Printf("tui error: %v, falling back to headless wait", err)
			useTUIEffective = false
		} else {
			cancel()
		}
	}

	if burst > 0 {
		<-ctx.Done()
		if debugServer != nil {
			debugServer.Close()
		}
		for _, tr := range trackers {
			tr.CloseOverflow()
		}
		return
	}

	if useTUIEffective {
		<-ctx.Done()
	} else {
		select {
		case <-ctx.Done():
		case <-managerDone:
		case <-wgDone:
		}
	}
	cancel()
	if manager != nil {
		manager.Stop()
	}
	<-managerDone
	<-wgDone
	if debugServer != nil {
		debugServer.Close()
	}
	for _, tr := range trackers {
		tr.CloseOverflow()
	}
}
