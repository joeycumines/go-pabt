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
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sync"
	"time"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt/examples/harbor-panic/logic"
	hsim "github.com/joeycumines/go-pabt/examples/harbor-panic/sim"
	"github.com/joeycumines/go-pabt/pabtdebug"
)

func main() {
	var (
		debugAddr    string
		tickMs       int
		overflowPath string
		stormEvery   int
		humanSpeed   float64
		headless     bool
		burst        int
		seedBP       string
		ticks        int
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
	flag.Parse()

	if burst > 0 {
		headless = true
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, os.Kill)
	go func() {
		<-signals
		cancel()
	}()

	harbor := hsim.NewHarbor(stormEvery, humanSpeed, time.Now().UnixNano())
	st := harbor.State()
	actors := st.Actors()

	if len(actors) < 4 {
		log.Fatalf("expected 4 actors, got %d", len(actors))
	}

	var debugServer *pabtdebug.Server
	var trackers []*pabtdebug.Tracker

	manager := bt.NewManager()
	managerDone := make(chan struct{})
	go func() {
		defer close(managerDone)
		<-manager.Done()
		if err := manager.Err(); err != nil && ctx.Err() == nil {
			log.Printf("bt manager error: %v", err)
		}
	}()

	var wg sync.WaitGroup
	wgDone := make(chan struct{})
	wg.Add(len(actors))
	go func() {
		wg.Wait()
		close(wgDone)
	}()

	for i, actor := range actors {
		planID := fmt.Sprintf("harbor-bot-%d", i)
		result := logic.HarborPlan(ctx, harbor, actor.ID)
		planNode := result.Node

		tracker := pabtdebug.NewTrackerWithID(result.Plan, planID)
		if overflowPath != "" {
			tracker.SetOverflowFile(fmt.Sprintf("%s.%s", overflowPath, planID))
			tracker.WithKeyframeInterval(20)
			tracker.WithMaxEvents(1000)
			tracker.WithMaxTrees(100)
		}
		if seedBP != "" && i == 0 {
			tracker.SetBreakpoint(pabtdebug.Breakpoint{
				NodePath:  seedBP,
				Condition: "Running",
				Enabled:   true,
			})
		}
		trackers = append(trackers, tracker)

		origNode := planNode
		wrappedNode := bt.New(func(children []bt.Node) (bt.Status, error) {
			status, err := origNode.Tick()
			tracker.Track(status, err)
			return status, err
		})

		if debugAddr != "" {
			if i == 0 {
				debugServer = pabtdebug.NewServer(tracker, debugAddr)
				go func() {
					log.Printf("debug server: %s", debugServer.Start())
				}()
			} else {
				debugServer.RegisterTracker(tracker)
			}
		}

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
			log.Fatalf("manager add %s: %v", planID, err)
		}

		go func(name string) {
			defer wg.Done()
			<-ticker.Done()
			if err := ticker.Err(); err != nil && ctx.Err() == nil {
				log.Printf("plan error %s: %v", name, err)
			}
		}(planID)
	}

	// Harbor sim step loop
	go func() {
		interval := time.Duration(tickMs) * time.Millisecond
		if burst > 0 {
			interval = 0 // as fast as possible
		}
		ticked := 0
		for {
			if burst > 0 && ticked >= burst {
				log.Printf("burst complete: %d ticks", ticked)
				<-ctx.Done()
				return
			}
			if ticks > 0 && ticked >= ticks {
				log.Printf("tick limit reached: %d", ticked)
				<-ctx.Done()
				return
			}
			select {
			case <-ctx.Done():
				return
			default:
			}
			if err := harbor.Step(ctx); err != nil {
				if ctx.Err() != nil {
					return
				}
				log.Printf("harbor step error: %v", err)
			}
			ticked++
			if headless && ticked%100 == 0 {
				log.Printf("harbor tick %d", ticked)
			}
			if interval > 0 {
				time.Sleep(interval)
			}
		}
	}()

	if !headless {
		fmt.Println("Harbor Panic running. Press Ctrl+C to stop.")
		fmt.Printf("Actors: %d, Debug: %q\n", len(actors), debugAddr)
	}

	select {
	case <-ctx.Done():
	case <-managerDone:
	case <-wgDone:
	}

	cancel()
	manager.Stop()
	<-managerDone
	<-wgDone

	if debugServer != nil {
		debugServer.Close()
	}
	for _, t := range trackers {
		t.CloseOverflow()
	}
}
