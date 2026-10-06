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
