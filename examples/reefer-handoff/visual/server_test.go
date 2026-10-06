// Copyright 2026 Joseph Cumines
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

package visual

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joeycumines/go-pabt/examples/reefer-handoff/sim"
)

// TestSimStateMatchesManifestInitial starts the sim server, polls /sim/state,
// and asserts the JSON shape matches the manifest initial state: seven nodes,
// seven lanes, three tractors at STAGING, three reefers at STAGING, three
// powered slots, phase Running at round 1.
func TestSimStateMatchesManifestInitial(t *testing.T) {
	loop, err := sim.NewWorldLoop(sim.ModeDeterministic)
	if err != nil {
		t.Fatal(err)
	}
	if err := loop.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = loop.Shutdown(t.Context()) }()

	srv, err := NewServer("127.0.0.1:0", loop)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Start() }()
	defer func() { _ = srv.Close() }()

	resp := pollState(t, srv.Addr())

	if resp.Round != 1 {
		t.Fatalf("round = %d, want 1", resp.Round)
	}
	if resp.Phase != sim.PhaseRunning {
		t.Fatalf("phase = %q, want %q", resp.Phase, sim.PhaseRunning)
	}
	if resp.Cutoff != sim.CutoffRound {
		t.Fatalf("cutoff = %d, want %d", resp.Cutoff, sim.CutoffRound)
	}
	if resp.WorldSeq != 0 {
		t.Fatalf("worldSeq = %d, want 0", resp.WorldSeq)
	}

	// Seven nodes.
	if len(resp.Nodes) != 7 {
		t.Fatalf("nodes: got %d, want 7", len(resp.Nodes))
	}

	// Seven lanes.
	if len(resp.Lanes) != 7 {
		t.Fatalf("lanes: got %d, want 7", len(resp.Lanes))
	}
	for _, ln := range resp.Lanes {
		if ln.Locked {
			t.Fatalf("lane %s locked at initial state", ln.ID)
		}
	}

	// Three tractors, all at STAGING, no job, no hold.
	if len(resp.Tractors) != 3 {
		t.Fatalf("tractors: got %d, want 3", len(resp.Tractors))
	}
	for _, tr := range resp.Tractors {
		if tr.At != sim.NodeStaging {
			t.Fatalf("tractor %s at %q, want STAGING", tr.ID, tr.At)
		}
		if tr.Job != "" {
			t.Fatalf("tractor %s has job %q at initial state", tr.ID, tr.Job)
		}
		if tr.Hold != "" {
			t.Fatalf("tractor %s holds %q at initial state", tr.ID, tr.Hold)
		}
	}

	// Three reefers, all at STAGING, no custodian.
	if len(resp.Reefers) != 3 {
		t.Fatalf("reefers: got %d, want 3", len(resp.Reefers))
	}
	for _, rf := range resp.Reefers {
		if rf.At != sim.NodeStaging {
			t.Fatalf("reefer %s at %q, want STAGING", rf.ID, rf.At)
		}
		if rf.Custodian != "" {
			t.Fatalf("reefer %s has custodian %q at initial state", rf.ID, rf.Custodian)
		}
	}
	// Specific temperature and tag values from the manifest.
	reeferByID := make(map[sim.ReeferID]ReeferState, len(resp.Reefers))
	for _, rf := range resp.Reefers {
		reeferByID[rf.ID] = rf
	}
	if rf, ok := reeferByID[sim.Reefer1]; !ok {
		t.Fatal("REEFER-1 missing")
	} else {
		if rf.TempC != -22 || !rf.TagReadable {
			t.Fatalf("REEFER-1: tempC=%d tagReadable=%t, want -22 true", rf.TempC, rf.TagReadable)
		}
	}
	if rf, ok := reeferByID[sim.Reefer2]; !ok {
		t.Fatal("REEFER-2 missing")
	} else {
		if rf.TempC != -8 || rf.TagReadable {
			t.Fatalf("REEFER-2: tempC=%d tagReadable=%t, want -8 false", rf.TempC, rf.TagReadable)
		}
	}
	if rf, ok := reeferByID[sim.Reefer3]; !ok {
		t.Fatal("REEFER-3 missing")
	} else {
		if rf.TempC != -8 || !rf.TagReadable {
			t.Fatalf("REEFER-3: tempC=%d tagReadable=%t, want -8 true", rf.TempC, rf.TagReadable)
		}
	}

	// Three slots, all powered, with manifest ratings.
	if len(resp.Slots) != 3 {
		t.Fatalf("slots: got %d, want 3", len(resp.Slots))
	}
	slotByID := make(map[sim.SlotID]SlotState, len(resp.Slots))
	for _, sl := range resp.Slots {
		slotByID[sl.ID] = sl
		if !sl.Plugged {
			t.Fatalf("slot %s not powered at initial state", sl.ID)
		}
		if sl.Occupied != "" {
			t.Fatalf("slot %s occupied by %q at initial state", sl.ID, sl.Occupied)
		}
	}
	if sl, ok := slotByID[sim.Slot1]; !ok {
		t.Fatal("SLOT-1 missing")
	} else if sl.RatedMaxC != -30 {
		t.Fatalf("SLOT-1 ratedMaxC = %d, want -30", sl.RatedMaxC)
	}
	if sl, ok := slotByID[sim.Slot2]; !ok {
		t.Fatal("SLOT-2 missing")
	} else if sl.RatedMaxC != -18 {
		t.Fatalf("SLOT-2 ratedMaxC = %d, want -18", sl.RatedMaxC)
	}
	if sl, ok := slotByID[sim.Slot3]; !ok {
		t.Fatal("SLOT-3 missing")
	} else if sl.RatedMaxC != -30 {
		t.Fatalf("SLOT-3 ratedMaxC = %d, want -30", sl.RatedMaxC)
	}
}

// TestSimStateRejectsNonGet verifies the endpoint refuses non-GET methods.
func TestSimStateRejectsNonGet(t *testing.T) {
	loop, err := sim.NewWorldLoop(sim.ModeDeterministic)
	if err != nil {
		t.Fatal(err)
	}
	if err := loop.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = loop.Shutdown(t.Context()) }()

	srv, err := NewServer("127.0.0.1:0", loop)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Start() }()
	defer func() { _ = srv.Close() }()

	url := fmt.Sprintf("http://%s/sim/state", srv.Addr())
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST /sim/state: status %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

// TestSimUIServesClient asserts the embedded client is served at /sim/ui:
// index.html answers with the page title and an html media type, the
// stylesheet and the module are served with theirs, and an unknown asset is
// a 404. The .mjs media type matters: a browser refuses to execute a module
// that is not served as JavaScript.
func TestSimUIServesClient(t *testing.T) {
	loop, err := sim.NewWorldLoop(sim.ModeDeterministic)
	if err != nil {
		t.Fatal(err)
	}
	if err := loop.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = loop.Shutdown(t.Context()) }()

	srv, err := NewServer("127.0.0.1:0", loop)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Start() }()
	defer func() { _ = srv.Close() }()

	base := "http://" + srv.Addr()
	get := func(t *testing.T, path string) (int, string, string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			resp, err := http.Get(base + path) //nolint:gosec // test-only local poll
			if err != nil {
				if time.Now().After(deadline) {
					t.Fatalf("sim server did not respond at %s: %v", base, err)
				}
				time.Sleep(10 * time.Millisecond)
				continue
			}
			body, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if err != nil {
				t.Fatalf("reading %s: %v", path, err)
			}
			return resp.StatusCode, resp.Header.Get("Content-Type"), string(body)
		}
	}

	status, ct, body := get(t, "/sim/ui")
	if status != http.StatusOK {
		t.Fatalf("GET /sim/ui: status %d, want 200", status)
	}
	if !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("GET /sim/ui: content type %q, want text/html", ct)
	}
	if !strings.Contains(body, "REEFER HANDOFF") {
		t.Fatal("GET /sim/ui: body does not contain the page title")
	}

	// The trailing-slash spelling serves the same document.
	status, _, _ = get(t, "/sim/ui/")
	if status != http.StatusOK {
		t.Fatalf("GET /sim/ui/: status %d, want 200", status)
	}

	for _, asset := range []struct {
		path   string
		wantCT string
	}{
		{"/sim/ui/sim.css", "text/css"},
		{"/sim/ui/js/sim.mjs", "text/javascript"},
	} {
		status, ct, body = get(t, asset.path)
		if status != http.StatusOK {
			t.Fatalf("GET %s: status %d, want 200", asset.path, status)
		}
		if !strings.HasPrefix(ct, asset.wantCT) {
			t.Fatalf("GET %s: content type %q, want prefix %q", asset.path, ct, asset.wantCT)
		}
		if len(body) == 0 {
			t.Fatalf("GET %s: empty body", asset.path)
		}
	}

	status, _, _ = get(t, "/sim/ui/does-not-exist.js")
	if status != http.StatusNotFound {
		t.Fatalf("GET /sim/ui/does-not-exist.js: status %d, want 404", status)
	}
}

// pollState polls /sim/state until it responds or the deadline passes.
func pollState(t *testing.T, addr string) StateResponse {
	t.Helper()
	url := fmt.Sprintf("http://%s/sim/state", addr)
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(url) //nolint:gosec // test-only local poll
		if err != nil {
			if time.Now().After(deadline) {
				t.Fatalf("sim server did not respond at %s: %v", addr, err)
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /sim/state: status %d", resp.StatusCode)
		}
		var state StateResponse
		if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
			t.Fatalf("decoding /sim/state: %v", err)
		}
		return state
	}
}

// TestSimStateReceiptLedger drives the world loop with one applied command
// and one refused command, then asserts /sim/state serves both through the
// receipt ledger with their typed outcomes and reasons. The ledger is world
// state, so this is the data the V4 client animates command outcomes from.
func TestSimStateReceiptLedger(t *testing.T) {
	loop, err := sim.NewWorldLoop(sim.ModeDeterministic)
	if err != nil {
		t.Fatal(err)
	}
	if err := loop.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = loop.Shutdown(t.Context()) }()

	srv, err := NewServer("127.0.0.1:0", loop)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Start() }()
	defer func() { _ = srv.Close() }()

	// An applied ClaimJob: TUG-1 takes one job.
	ch, err := loop.Submit(sim.Command{
		ID: "c1", IdempotencyKey: "k1", Kind: sim.CmdClaimJob, Tractor: sim.Tug1,
	})
	if err != nil {
		t.Fatal(err)
	}
	<-ch

	// A refused MoveTo: STAGING has no lane to the bay, so the world declines
	// it and the refusal lands in the ledger with a typed reason.
	ch, err = loop.Submit(sim.Command{
		ID: "c2", IdempotencyKey: "k2", Kind: sim.CmdMoveTo, Tractor: sim.Tug2, Node: sim.NodeBay,
	})
	if err != nil {
		t.Fatal(err)
	}
	<-ch

	resp := pollState(t, srv.Addr())
	if resp.CommandSeq != 2 {
		t.Fatalf("commandSeq = %d, want 2", resp.CommandSeq)
	}
	if len(resp.Receipts) != 2 {
		t.Fatalf("receipts: got %d, want 2", len(resp.Receipts))
	}
	if resp.Receipts[0].Outcome != sim.OutcomeApplied || resp.Receipts[0].Kind != sim.CmdClaimJob {
		t.Fatalf("receipt[0] = %s/%s, want Applied/ClaimJob", resp.Receipts[0].Outcome, resp.Receipts[0].Kind)
	}
	if resp.Receipts[1].Outcome != sim.OutcomeRefused || resp.Receipts[1].Reason != sim.ReasonNotAdjacent {
		t.Fatalf("receipt[1] = %s/%s, want Refused/NotAdjacent", resp.Receipts[1].Outcome, resp.Receipts[1].Reason)
	}
}

// stubControl is a ControlSource the control-route tests attach. It records
// every call and reports the pause/speed it was told, the way the wired
// Controller does after a control event lands.
type stubControl struct {
	mu     sync.Mutex
	paused bool
	speed  float64
	calls  []string
}

func (s *stubControl) SendPause() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paused = true
	s.calls = append(s.calls, "pause")
}

func (s *stubControl) SendResume() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.paused = false
	s.calls = append(s.calls, "resume")
}

func (s *stubControl) SendStep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, "step")
}

func (s *stubControl) SetSpeed(value float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.speed = value
	s.calls = append(s.calls, fmt.Sprintf("speed=%v", value))
}

func (s *stubControl) Paused() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.paused
}

func (s *stubControl) Speed() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.speed
}

func (s *stubControl) callList() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

// TestSimControlDrivesTheAttachedController asserts POST /sim/control maps
// each action to the attached controller and answers with the gate state
// after the event: pause reports paused, resume reports running, speed
// reports the set multiplier. The endpoint must respond fast enough for a
// UI (the acceptance is 100ms), and it must refuse the unknowns.
func TestSimControlDrivesTheAttachedController(t *testing.T) {
	loop, err := sim.NewWorldLoop(sim.ModeDeterministic)
	if err != nil {
		t.Fatal(err)
	}
	if err := loop.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = loop.Shutdown(t.Context()) }()

	srv, err := NewServer("127.0.0.1:0", loop)
	if err != nil {
		t.Fatal(err)
	}
	ctl := &stubControl{speed: 1.0}
	srv.AttachController(ctl)
	go func() { _ = srv.Start() }()
	defer func() { _ = srv.Close() }()

	post := func(t *testing.T, body string) (int, ControlState) {
		t.Helper()
		resp, err := http.Post(
			fmt.Sprintf("http://%s/sim/control", srv.Addr()),
			"application/json", strings.NewReader(body),
		) //nolint:gosec // test-only local post
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			return resp.StatusCode, ControlState{}
		}
		var state ControlState
		if err := json.NewDecoder(resp.Body).Decode(&state); err != nil {
			t.Fatalf("decoding the control response: %v", err)
		}
		return resp.StatusCode, state
	}

	status, state := post(t, `{"action":"pause"}`)
	if status != http.StatusOK || !state.Paused {
		t.Fatalf("pause: status %d paused=%t, want 200 paused=true", status, state.Paused)
	}

	status, state = post(t, `{"action":"step"}`)
	if status != http.StatusOK {
		t.Fatalf("step: status %d, want 200", status)
	}

	status, state = post(t, `{"action":"speed","value":2.5}`)
	if status != http.StatusOK || state.Speed != 2.5 {
		t.Fatalf("speed: status %d speed=%v, want 200 speed=2.5", status, state.Speed)
	}

	status, state = post(t, `{"action":"resume"}`)
	if status != http.StatusOK || state.Paused {
		t.Fatalf("resume: status %d paused=%t, want 200 paused=false", status, state.Paused)
	}

	want := []string{"pause", "step", "speed=2.5", "resume"}
	if got := ctl.callList(); len(got) != len(want) {
		t.Fatalf("controller calls = %v, want %v", got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("controller call %d = %q, want %q", i, got[i], want[i])
			}
		}
	}

	// An unknown action is a bad request, not a silent nothing.
	status, _ = post(t, `{"action":"rewind"}`)
	if status != http.StatusBadRequest {
		t.Fatalf("unknown action: status %d, want %d", status, http.StatusBadRequest)
	}

	// GET is refused.
	resp, err := http.Get(fmt.Sprintf("http://%s/sim/control", srv.Addr())) //nolint:gosec // test-only local get
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET /sim/control: status %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

// TestSimControlWithoutController asserts the route answers 503 rather than
// pretending to drive a gate that does not exist: a fixture that never
// attached one must say so.
func TestSimControlWithoutController(t *testing.T) {
	loop, err := sim.NewWorldLoop(sim.ModeDeterministic)
	if err != nil {
		t.Fatal(err)
	}
	if err := loop.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = loop.Shutdown(t.Context()) }()

	srv, err := NewServer("127.0.0.1:0", loop)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Start() }()
	defer func() { _ = srv.Close() }()

	resp, err := http.Post(
		fmt.Sprintf("http://%s/sim/control", srv.Addr()),
		"application/json", strings.NewReader(`{"action":"pause"}`),
	) //nolint:gosec // test-only local post
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("control without a controller: status %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

// TestSimDebuggersListsTheAttachedPages asserts GET /sim/debuggers reports
// exactly the pages AddDebugger recorded, in order, and refuses non-GET.
func TestSimDebuggersListsTheAttachedPages(t *testing.T) {
	loop, err := sim.NewWorldLoop(sim.ModeDeterministic)
	if err != nil {
		t.Fatal(err)
	}
	if err := loop.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = loop.Shutdown(t.Context()) }()

	srv, err := NewServer("127.0.0.1:0", loop)
	if err != nil {
		t.Fatal(err)
	}
	srv.AddDebugger("TUG-1", "http://127.0.0.1:1/debug/pabt/ui")
	srv.AddDebugger("TUG-2", "http://127.0.0.1:2/debug/pabt/ui")
	srv.AddDebugger("TUG-3", "http://127.0.0.1:3/debug/pabt/ui")
	go func() { _ = srv.Start() }()
	defer func() { _ = srv.Close() }()

	resp, err := http.Get(fmt.Sprintf("http://%s/sim/debuggers", srv.Addr())) //nolint:gosec // test-only local get
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /sim/debuggers: status %d, want 200", resp.StatusCode)
	}
	var body struct {
		Debuggers []DebuggerLink `json:"debuggers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decoding /sim/debuggers: %v", err)
	}
	want := []DebuggerLink{
		{Tractor: "TUG-1", URL: "http://127.0.0.1:1/debug/pabt/ui"},
		{Tractor: "TUG-2", URL: "http://127.0.0.1:2/debug/pabt/ui"},
		{Tractor: "TUG-3", URL: "http://127.0.0.1:3/debug/pabt/ui"},
	}
	if len(body.Debuggers) != len(want) {
		t.Fatalf("debuggers: got %d, want %d", len(body.Debuggers), len(want))
	}
	for i := range want {
		if body.Debuggers[i] != want[i] {
			t.Fatalf("debugger %d = %+v, want %+v", i, body.Debuggers[i], want[i])
		}
	}
}

// TestSimStateReportsControlAndGeneration asserts /sim/state carries the
// attached controller's gate state and the generation source's per-tractor
// generations, and omits the control block when no controller is attached.
func TestSimStateReportsControlAndGeneration(t *testing.T) {
	loop, err := sim.NewWorldLoop(sim.ModeDeterministic)
	if err != nil {
		t.Fatal(err)
	}
	if err := loop.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = loop.Shutdown(t.Context()) }()

	srv, err := NewServer("127.0.0.1:0", loop)
	if err != nil {
		t.Fatal(err)
	}
	ctl := &stubControl{paused: true, speed: 3.0}
	srv.AttachController(ctl)
	srv.SetGenerationSource(func() map[string]uint64 {
		return map[string]uint64{"TUG-1": 7, "TUG-2": 0, "TUG-3": 2}
	})
	go func() { _ = srv.Start() }()
	defer func() { _ = srv.Close() }()

	resp := pollState(t, srv.Addr())
	if resp.Control == nil {
		t.Fatal("/sim/state has no control block with a controller attached")
	}
	if !resp.Control.Paused || resp.Control.Speed != 3.0 {
		t.Fatalf("control = %+v, want paused=true speed=3.0", resp.Control)
	}
	gens := map[sim.TractorID]uint64{}
	for _, tr := range resp.Tractors {
		gens[tr.ID] = tr.Generation
	}
	if gens[sim.Tug1] != 7 || gens[sim.Tug2] != 0 || gens[sim.Tug3] != 2 {
		t.Fatalf("generations = %v, want TUG-1:7 TUG-2:0 TUG-3:2", gens)
	}
}

// TestSimStateOmitsControlWithoutController asserts the control block is
// absent (omitempty) when nothing is attached, so a page knows pause is not
// available instead of reading a zero gate.
func TestSimStateOmitsControlWithoutController(t *testing.T) {
	loop, err := sim.NewWorldLoop(sim.ModeDeterministic)
	if err != nil {
		t.Fatal(err)
	}
	if err := loop.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = loop.Shutdown(t.Context()) }()

	srv, err := NewServer("127.0.0.1:0", loop)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Start() }()
	defer func() { _ = srv.Close() }()

	url := fmt.Sprintf("http://%s/sim/state", srv.Addr())
	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := http.Get(url) //nolint:gosec // test-only local poll
		if err != nil {
			if time.Now().After(deadline) {
				t.Fatalf("sim server did not respond at %s: %v", url, err)
			}
			time.Sleep(10 * time.Millisecond)
			continue
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatalf("reading /sim/state: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET /sim/state: status %d", resp.StatusCode)
		}
		if strings.Contains(string(body), `"control"`) {
			t.Fatalf("/sim/state carries a control block with no controller attached:\n%s", body)
		}
		return
	}
}
