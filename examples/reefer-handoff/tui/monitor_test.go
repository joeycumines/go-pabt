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

package tui

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/joeycumines/go-pabt/examples/reefer-handoff/sim"
)

// stubController records what the TUI sent it. It is safe for concurrent use:
// the TUI goroutine calls Send* methods while the test goroutine reads counts.
type stubController struct {
	mu      sync.Mutex
	pauses  int
	resumes int
	steps   int
	speeds  []float64
	quits   int
	epoch   uint64
	paused  bool
	speed   float64
}

func (s *stubController) SendPause() {
	s.mu.Lock()
	s.pauses++
	s.paused = true
	s.epoch++
	s.mu.Unlock()
}
func (s *stubController) SendResume() {
	s.mu.Lock()
	s.resumes++
	s.paused = false
	s.epoch++
	s.mu.Unlock()
}
func (s *stubController) SendStep() {
	s.mu.Lock()
	s.steps++
	s.epoch++
	s.mu.Unlock()
}
func (s *stubController) SendSpeed(d float64) {
	s.mu.Lock()
	s.speeds = append(s.speeds, d)
	s.speed += d
	s.epoch++
	s.mu.Unlock()
}
func (s *stubController) SendQuit() {
	s.mu.Lock()
	s.quits++
	s.epoch++
	s.mu.Unlock()
}
func (s *stubController) Epoch() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.epoch
}
func (s *stubController) Paused() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.paused
}
func (s *stubController) Speed() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.speed
}

// Read helpers for test assertions (called from the test goroutine).
func (s *stubController) Pauses() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pauses
}
func (s *stubController) Steps() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.steps
}
func (s *stubController) Speeds() []float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]float64, len(s.speeds))
	copy(out, s.speeds)
	return out
}
func (s *stubController) Quits() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.quits
}

func TestMonitorHeadlessRendering(t *testing.T) {
	world := sim.NewWorld()
	ctrl := &stubController{speed: 1.0}
	views := []TractorView{
		{ID: sim.Tug1, Addr: "127.0.0.1:0", Cap: "test"},
	}
	mon := NewMonitor(nil, views, ctrl)
	_ = mon // monitor holds state; renderAll is the function under test

	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(80, 24)

	records := map[string][]RecordSummary{
		string(sim.Tug1): {{Sequence: 1, Kind: "tick", Outcome: "Applied"}},
	}
	renderAll(screen, world, ctrl, records)
	screen.Show()

	// Assert title bar contains REEFER HANDOFF.
	found := false
	for x := 0; x < 80; x++ {
		r, _, _, _ := screen.GetContent(x, 0)
		if r == 'R' {
			// Check next chars spell "REEFER"
			r2, _, _, _ := screen.GetContent(x+1, 0)
			r3, _, _, _ := screen.GetContent(x+2, 0)
			r4, _, _, _ := screen.GetContent(x+3, 0)
			r5, _, _, _ := screen.GetContent(x+4, 0)
			r6, _, _, _ := screen.GetContent(x+5, 0)
			if r2 == 'E' && r3 == 'E' && r4 == 'F' && r5 == 'E' && r6 == 'R' {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("title bar does not contain REEFER HANDOFF")
	}

	// Assert reefer table header exists on row 1.
	headerFound := false
	for x := 0; x < 80; x++ {
		r, _, _, _ := screen.GetContent(x, 1)
		if r == 'R' {
			r2, _, _, _ := screen.GetContent(x+1, 1)
			if r2 == 'E' {
				headerFound = true
				break
			}
		}
	}
	if !headerFound {
		t.Fatal("reefer table header not found on row 1")
	}

	// Assert committed records section renders the tractor ID.
	recordFound := false
	for y := 0; y < 24; y++ {
		for x := 0; x < 80; x++ {
			r, _, _, _ := screen.GetContent(x, y)
			if r == 'T' {
				r2, _, _, _ := screen.GetContent(x+1, y)
				r3, _, _, _ := screen.GetContent(x+2, y)
				if r2 == 'U' && r3 == 'G' {
					recordFound = true
					break
				}
			}
		}
		if recordFound {
			break
		}
	}
	if !recordFound {
		t.Fatal("committed records section does not show tractor ID")
	}
}

func TestMonitorKeyboardSendsControlEvents(t *testing.T) {
	ctrl := &stubController{speed: 1.0}
	views := []TractorView{{ID: sim.Tug1, Addr: "127.0.0.1:0", Cap: "test"}}
	mon := NewMonitor(nil, views, ctrl)

	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	defer screen.Fini()
	screen.SetSize(80, 24)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- mon.RunWithScreen(ctx, screen) }()

	// Give the event loop time to start.
	time.Sleep(50 * time.Millisecond)

	// Press 'p' to pause.
	screen.InjectKey(tcell.KeyRune, 'p', tcell.ModNone)
	time.Sleep(50 * time.Millisecond)
	if ctrl.Pauses() != 1 {
		t.Fatalf("expected 1 pause, got %d", ctrl.Pauses())
	}

	// Press 's' to step.
	screen.InjectKey(tcell.KeyRune, 's', tcell.ModNone)
	time.Sleep(50 * time.Millisecond)
	if ctrl.Steps() != 1 {
		t.Fatalf("expected 1 step, got %d", ctrl.Steps())
	}

	// Press '+' to speed up.
	screen.InjectKey(tcell.KeyRune, '+', tcell.ModNone)
	time.Sleep(50 * time.Millisecond)
	spds := ctrl.Speeds()
	if len(spds) != 1 || spds[0] != 0.5 {
		t.Fatalf("expected speed delta 0.5, got %v", spds)
	}

	// Press 'q' to quit.
	screen.InjectKey(tcell.KeyRune, 'q', tcell.ModNone)

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("RunWithScreen returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RunWithScreen did not exit after quit")
	}
	if ctrl.Quits() != 1 {
		t.Fatalf("expected 1 quit, got %d", ctrl.Quits())
	}
}
