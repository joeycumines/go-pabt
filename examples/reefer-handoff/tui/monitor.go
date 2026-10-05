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

// Package tui provides an operations monitor for the Reefer Handoff scenario.
// It renders the same world/plan trace the browser shows — cutoff, reefer
// states, transfer bay, lane state, tractor jobs, world events, pending
// commands, and committed records — reading from WorldLoop.Snapshot() and the
// per-tractor pabtdebug.Server HTTP routes with the capability header.
//
// The architecture follows harbor-panic's proven pattern: Run enters the
// screen, dispatches PollEvent on its own goroutine, and renders on a ticker.
// Keyboard input routes through the host-level Controller rather than
// manipulating atomics, keeping pause/step/speed as typed control events.
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/joeycumines/go-pabt/examples/reefer-handoff/sim"
)

// Monitor renders the Reefer Handoff operations view. It is safe for one
// goroutine to call Run while another sends control events through the
// controller it was given.
type Monitor struct {
	loop     *sim.WorldLoop
	tractors []TractorView
	ctrl     Controller
	runners  []RunnerBeliefSource
	mu       sync.Mutex
	records  map[string][]RecordSummary   // keyed by tractor ID
	beliefs  map[string]map[string]string // tractor -> key -> status string
}

// RunnerBeliefSource provides read access to a runner's belief store.
// It matches logic.Runner's Beliefs() method without importing the logic
// package, preventing circular dependencies.
type RunnerBeliefSource interface {
	Beliefs() map[sim.VarKey]sim.Belief
	Tractor() sim.TractorID
}

// TractorView binds one tractor to its debugger server address and capability.
type TractorView struct {
	ID   sim.TractorID
	Addr string
	Cap  string
}

// Controller is the interface the monitor uses to send operator input to the
// drive loop. It matches main.Controller but lives here so the TUI package
// does not import main.
type Controller interface {
	SendPause()
	SendResume()
	SendStep()
	SendSpeed(delta float64)
	SendQuit()
	Epoch() uint64
	Paused() bool
	Speed() float64
}

// RecordSummary is what the monitor extracts from a committed record for
// display. It carries just enough to show sequence, kind, outcome, and the
// causality link without pulling the full wire shape into the TUI.
type RecordSummary struct {
	Sequence    uint64
	Kind        string
	Outcome     string
	CommandID   string
	CauseCmd    string
	WorldSeq    uint64
	PlanTickSeq uint64
}

// NewMonitor builds a ready monitor. The controller may be nil for headless
// tests that do not need keyboard routing. The runners slice provides belief
// data for the TUI belief panel; it may be nil or empty.
func NewMonitor(loop *sim.WorldLoop, views []TractorView, ctrl Controller) *Monitor {
	return &Monitor{
		loop:     loop,
		tractors: views,
		ctrl:     ctrl,
		records:  make(map[string][]RecordSummary),
		beliefs:  make(map[string]map[string]string),
	}
}

// AttachRunners wires belief sources into the monitor. Call before Run().
func (m *Monitor) AttachRunners(runners []RunnerBeliefSource) {
	m.runners = runners
}

// Run starts the TUI loop, blocking until quit or context cancel. If screen
// init fails it returns the error so the caller can fall back to headless.
func (m *Monitor) Run(ctx context.Context) error {
	if os.Getenv("TERM") == "" {
		return fmt.Errorf("TERM not set")
	}
	screen, err := tcell.NewScreen()
	if err != nil {
		return err
	}
	if err := screen.Init(); err != nil {
		return err
	}
	defer screen.Fini()
	screen.Clear()
	return m.runWithScreen(ctx, screen)
}

// RunWithScreen is exported for SimulationScreen tests.
func (m *Monitor) RunWithScreen(ctx context.Context, screen tcell.Screen) error {
	return m.runWithScreen(ctx, screen)
}

func (m *Monitor) runWithScreen(ctx context.Context, screen tcell.Screen) error {
	if screen == nil {
		return fmt.Errorf("nil screen")
	}
	screen.Clear()

	evCh := make(chan tcell.Event, 16)
	go func() {
		for {
			ev := screen.PollEvent()
			if ev == nil {
				close(evCh)
				return
			}
			select {
			case evCh <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	// Start background record fetchers for each tractor.
	fetchCtx, fetchCancel := context.WithCancel(ctx)
	defer fetchCancel()
	for _, tv := range m.tractors {
		go m.fetchRecords(fetchCtx, tv)
	}

	// snapshot safely reads the world; returns an empty world if no loop.
	snapshot := func() sim.World {
		if m.loop != nil {
			return m.loop.Snapshot()
		}
		return sim.NewWorld()
	}

	renderAll(screen, snapshot(), m.ctrl, m.records, m.snapshotBeliefs())
	screen.Show()

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-evCh:
			if ev == nil {
				return nil
			}
			if key, ok := ev.(*tcell.EventKey); ok {
				if m.handleKey(key) {
					return nil
				}
			}
		case <-ticker.C:
			renderAll(screen, snapshot(), m.ctrl, m.records, m.snapshotBeliefs())
			screen.Show()
		}
	}
}

// handleKey dispatches a key event to the controller. Returns true if the
// monitor should exit.
func (m *Monitor) handleKey(ev *tcell.EventKey) bool {
	if m.ctrl == nil {
		return false
	}
	switch ev.Key() {
	case tcell.KeyCtrlC, tcell.KeyEscape:
		m.ctrl.SendQuit()
		return true
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'q', 'Q':
			m.ctrl.SendQuit()
			return true
		case 'p', 'P':
			if m.ctrl.Paused() {
				m.ctrl.SendResume()
			} else {
				m.ctrl.SendPause()
			}
		case 's', 'S':
			m.ctrl.SendStep()
		case '+', '=':
			m.ctrl.SendSpeed(0.5)
		case '-', '_':
			m.ctrl.SendSpeed(-0.5)
		}
	}
	return false
}

// fetchRecords polls one tractor's /debug/pabt/records endpoint and updates
// the monitor's record summary. It runs until the context cancels.
func (m *Monitor) fetchRecords(ctx context.Context, tv TractorView) {
	url := fmt.Sprintf("http://%s/debug/pabt/records", tv.Addr)
	client := &http.Client{Timeout: 2 * time.Second}
	var cursor string
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
		reqURL := url
		if cursor != "" {
			reqURL += "?cursor=" + cursor
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("X-PABT-Capability", tv.Cap)
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			continue
		}
		var page struct {
			Cursor struct {
				Sequence json.Number `json:"sequence"`
			} `json:"cursor"`
			Page struct {
				HasMore bool              `json:"hasMore"`
				Records []json.RawMessage `json:"records"`
			} `json:"page"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			continue
		}
		summaries := parseRecordSummaries(page.Page.Records)
		if len(summaries) > 0 {
			m.mu.Lock()
			existing := m.records[string(tv.ID)]
			merged := mergeRecords(existing, summaries)
			if len(merged) > 50 {
				merged = merged[len(merged)-50:]
			}
			m.records[string(tv.ID)] = merged
			m.mu.Unlock()
		}
		if seqStr := page.Cursor.Sequence; seqStr != "" {
			cursor = string(seqStr)
		}
	}
}

func parseRecordSummaries(raw []json.RawMessage) []RecordSummary {
	out := make([]RecordSummary, 0, len(raw))
	for _, r := range raw {
		var rec struct {
			Sequence  json.Number `json:"sequence"`
			Kind      string      `json:"kind"`
			Causality *struct {
				Settled []struct {
					Outcome   string `json:"outcome"`
					CommandID string `json:"commandId"`
				} `json:"settled"`
				Submitted []struct {
					CommandID string `json:"commandId"`
				} `json:"submitted"`
			} `json:"causality"`
		}
		if err := json.Unmarshal(r, &rec); err != nil {
			continue
		}
		seq, _ := rec.Sequence.Int64()
		rs := RecordSummary{
			Sequence: uint64(seq),
			Kind:     rec.Kind,
		}
		if rec.Causality != nil {
			if len(rec.Causality.Settled) > 0 {
				s := rec.Causality.Settled[0]
				rs.Outcome = s.Outcome
				rs.CommandID = s.CommandID
			}
			if len(rec.Causality.Submitted) > 0 {
				rs.CommandID = rec.Causality.Submitted[0].CommandID
			}
		}
		out = append(out, rs)
	}
	return out
}

func mergeRecords(existing, incoming []RecordSummary) []RecordSummary {
	seen := make(map[uint64]bool, len(existing))
	for _, r := range existing {
		seen[r.Sequence] = true
	}
	out := make([]RecordSummary, len(existing))
	copy(out, existing)
	for _, r := range incoming {
		if !seen[r.Sequence] {
			out = append(out, r)
			seen[r.Sequence] = true
		}
	}
	return out
}

// snapshotBeliefs reads every attached runner's belief store and returns a
// display-ready map keyed by tractor ID. Each value maps a variable key
// string to its belief status string (Unknown/Known/Stale).
func (m *Monitor) snapshotBeliefs() map[string]map[string]string {
	out := make(map[string]map[string]string, len(m.runners))
	for _, r := range m.runners {
		tug := string(r.Tractor())
		beliefs := r.Beliefs()
		statusMap := make(map[string]string, len(beliefs))
		for k, b := range beliefs {
			statusMap[k.String()] = b.Status.String()
		}
		out[tug] = statusMap
	}
	return out
}
