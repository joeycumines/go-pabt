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

// +build example

package main

import (
	"context"
	"flag"
	"github.com/gdamore/tcell/v2"
	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt/examples/tcell-pick-and-place/logic"
	"github.com/joeycumines/go-pabt/examples/tcell-pick-and-place/sim"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"time"
)

func main() {
	rand.Seed(time.Now().UnixNano())
	os.Exit(run(os.Args[0], os.Args[1:]))
}

func run(cmd string, args []string) int {
	var (
		flags   = flag.NewFlagSet(cmd, flag.ContinueOnError)
		logfile stringFlag
	)
	flags.Var(&logfile, `logfile`, `write log output to file`)
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
		Screen: screen,
	})
	if err != nil {
		if logfile == `` {
			log.SetOutput(os.Stderr)
		}
		log.Printf(`sim init error: %s`, err)
		return 1
	}

	manager := bt.NewManager()
	go func() {
		<-manager.Done()
		if err := manager.Err(); err != nil {
			if logfile == `` {
				log.SetOutput(os.Stderr)
			}
			log.Printf("bt error: %s\n", err)
			cancel()
		}
	}()
	for _, actor := range simulation.State().PlanConfig.Actors {
		plan := logic.PickAndPlace(ctx, simulation, actor)
		if err := manager.Add(bt.NewTicker(ctx, time.Millisecond*10, bt.New(
			bt.All,
			//bt.New(func([]bt.Node) (bt.Status, error) {
			//	_, _ = fmt.Fprintf(os.Stderr, "\n\nSTART TREE\n%s\nEND TREE\n\n", plan.String())
			//	return bt.Success, nil
			//}),
			bt.New(
				bt.Selector,
				bt.New(
					bt.Sequence,
					plan,
					bt.New(func([]bt.Node) (bt.Status, error) {
						log.Println(`tick success`)
						return bt.Success, nil
					}),
				),
				bt.New(func([]bt.Node) (bt.Status, error) {
					log.Println(`tick failure`)
					return bt.Failure, nil
				}),
			),
			//bt.New(func([]bt.Node) (bt.Status, error) {
			//	_, _ = fmt.Fprintf(os.Stderr, "\n\nSTART TREE\n%s\nEND TREE\n\n", plan.String())
			//	return bt.Success, nil
			//}),
		))); err != nil {
			panic(err)
		}
	}
	defer func() {
		cancel()
		manager.Stop()
		<-manager.Done()
	}()

	if err := simulation.Run(ctx); err != nil {
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
