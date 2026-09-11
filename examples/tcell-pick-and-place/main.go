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
	"bytes"
	"context"
	"flag"
	"fmt"
	"github.com/gdamore/tcell/v2"
	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt/examples/tcell-pick-and-place/logic"
	"github.com/joeycumines/go-pabt/examples/tcell-pick-and-place/sim"
	"github.com/joeycumines/go-pabt/pabtdebug"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"sync"
	"time"
)

func main() {
	rand.Seed(time.Now().UnixNano())
	os.Exit(run(os.Args[0], os.Args[1:]))
}

func run(cmd string, args []string) (exitCode int) {
	var (
		flags    = flag.NewFlagSet(cmd, flag.ContinueOnError)
		logfile  stringFlag
		exit     bool
		scenario stringFlag
		debug    stringFlag
	)
	flags.Var(&logfile, `logfile`, `write log output to file`)
	flags.BoolVar(&exit, `exit`, false, `exit once all plans succeed`)
	flags.Var(&scenario, `scenario`, `specify scenario as one of (static, human-vs-robot) [default=static]`)
	flags.Var(&debug, `debug`, `start debug server at given address (e.g. :8080)`)
	if err := flags.Parse(args); err != nil {
		return 1
	}
	if flags.NArg() != 0 {
		log.Printf("expected no args\n")
		flags.Usage()
		return 1
	}

	if logfile != `` {
		f, err := os.OpenFile(string(logfile), os.O_WRONLY|os.O_APPEND|os.O_CREATE, os.ModePerm)
		if err != nil {
			log.Printf("logfile open error: %s\n", err)
			return 1
		}
		defer f.Close()
		log.SetOutput(f)
	} else {
		log.SetOutput(ioutil.Discard)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	{
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, os.Interrupt, os.Kill)
		defer signal.Stop(signals)
		go signalHandler(ctx, cancel, signals)
	}

	screen, err := tcell.NewScreen()
	if err == nil {
		err = screen.Init()
	}
	if err != nil {
		log.Printf(`screen init error: %s`, err)
		return 1
	}
	defer screen.Fini()

	simulation, err := sim.New(sim.Config{
		Screen:   screen,
		Scenario: string(scenario),
	})
	if err != nil {
		if logfile == `` {
			log.SetOutput(os.Stderr)
		}
		log.Printf(`sim init error: %s`, err)
		return 1
	}

	defer func() {
		var b bytes.Buffer
		if _, err := dumpSimSpace(&b, screen, simulation); err == nil {
			log.Printf("dumping final state...\n%s\n", b.Bytes())
		}
	}()

	exitErrs := make(chan error, 5)
	defer func() {
		if exitCode != 0 {
			return
		}
		select {
		case <-exitErrs:
			exitCode = 1
		default:
		}
	}()

	if planConfig := simulation.State().PlanConfig; len(planConfig.Actors) != 0 {
		var newTicker func(ctx context.Context, duration time.Duration, node bt.Node) bt.Ticker
		if exit {
			newTicker = bt.NewTickerStopOnFailure
		} else {
			newTicker = bt.NewTicker
		}

		var (
			manager     = bt.NewManager()
			managerDone = make(chan struct{})
		)
		go func() {
			defer close(managerDone)
			<-manager.Done()
			if err := manager.Err(); err != nil {
				if logfile == `` {
					log.SetOutput(os.Stderr)
				}
				log.Printf("bt error: %s\n", err)
				select {
				case exitErrs <- err:
				default:
				}
				cancel()
				return
			}
		}()

		var (
			wg     sync.WaitGroup
			wgDone = make(chan struct{})
		)
		wg.Add(len(planConfig.Actors))
		go func() {
			wg.Wait()
			close(wgDone)
		}()

		var debugServer *pabtdebug.Server
		for i, actor := range planConfig.Actors {
			var (
				name      = fmt.Sprintf(`actors[%d]`, i)
				pnpResult = logic.PickAndPlace(ctx, simulation, actor)
				planNode  = pnpResult.Node
			)

			if debug != `` {
				tracker := pabtdebug.NewTrackerWithID(pnpResult.Plan, name)
				origNode := planNode
				planNode = bt.New(func(children []bt.Node) (bt.Status, error) {
					status, err := origNode.Tick()
					tracker.Track(status, err)
					return status, err
				})
				if i == 0 {
					debugServer = pabtdebug.NewServer(tracker, string(debug))
					go func() {
						log.Printf("debug server: %s", debugServer.Start())
					}()
				} else {
					debugServer.RegisterTracker(tracker)
				}
			}

			ticker := newTicker(ctx, time.Millisecond*10, bt.New(
				bt.Not(bt.All),
				bt.New(
					bt.Selector,
					bt.New(
						bt.Sequence,
						planNode,
						bt.New(func([]bt.Node) (bt.Status, error) {
							log.Printf("tick success for %s\n", name)
							return bt.Success, nil
						}),
					),
					bt.New(func([]bt.Node) (bt.Status, error) {
						log.Printf("tick failure for %s\n", name)
						return bt.Failure, nil
					}),
				),
			))
			log.Printf("plan started for %s\n", name)
			if err := manager.Add(ticker); err != nil {
				panic(err)
			}
			go func() {
				defer wg.Done()
				<-ticker.Done()
				if err := ticker.Err(); err != nil {
					if logfile == `` {
						log.SetOutput(os.Stderr)
					}
					log.Printf("plan error for %s: %s\n", name, err)
					return
				}
				log.Printf("plan success for %s\n", name)
			}()
		}

		if exit {
			go func() {
				select {
				case <-ctx.Done():
					return
				case <-managerDone:
				case <-wgDone:
				}
				cancel()
			}()
		}

		defer func() {
			cancel()
			manager.Stop()
			<-managerDone
			<-wgDone
			if debugServer != nil {
				debugServer.Close()
			}
		}()
	}

	if err := simulation.Run(ctx); err != nil && err != context.Canceled {
		if logfile == `` {
			log.SetOutput(os.Stderr)
		}
		log.Printf(`sim run error: %s`, err)
		return 1
	}

	return 0
}

func signalHandler(ctx context.Context, cancel context.CancelFunc, signals <-chan os.Signal) {
	select {
	case <-ctx.Done():
	case <-signals:
		cancel()
	}
}

type stringFlag string

func (f stringFlag) String() string { return string(f) }
func (f *stringFlag) Set(s string) error {
	*f = stringFlag(s)
	return nil
}

func logBT(name string, node bt.Node) bt.Tick {
	return func([]bt.Node) (bt.Status, error) {
		var b bytes.Buffer
		b.WriteString(fmt.Sprintf("dumping bt %q start\n", name))
		b.WriteString(node.String())
		if b.Bytes()[b.Len()-1] != '\n' {
			b.WriteRune('\n')
		}
		b.WriteString(fmt.Sprintf("dumping bt %q finish\n", name))
		log.Printf("%s", b.Bytes())
		return bt.Success, nil
	}
}

func dumpSimSpace(w io.Writer, screen tcell.Screen, simulation sim.Simulation) (written int64, err error) {
	var (
		state = simulation.State()
		x, y  int32
		b     bytes.Buffer
		r     rune
		n     int64
	)
	for y = -1; y <= state.SpaceHeight; y++ {
		b.Reset()
		for x = -1; x <= state.SpaceWidth; x++ {
			if x >= 0 && x < state.SpaceWidth && y >= 0 && y < state.SpaceHeight {
				r, _, _, _ = screen.GetContent(state.ScreenPosition(x, y))
			} else {
				r = '#'
			}
			b.WriteRune(r)
		}
		b.WriteRune('\n')
		n, err = io.Copy(w, &b)
		written += n
		if err != nil {
			return
		}
	}
	return
}
