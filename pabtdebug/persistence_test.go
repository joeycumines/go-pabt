package pabtdebug

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt"
)

func TestTracker_Persistence_WriteReadJSONL(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{{&testCondition{key: "x", value: true}}})
	if err != nil {
		t.Fatal(err)
	}
	tracker := NewTracker(plan)
	tracker.WithMaxEvents(3)
	tracker.WithMaxTrees(3)
	for i := 0; i < 5; i++ {
		plan.Node().Tick()
		tracker.Track(bt.Success, nil)
	}
	events := tracker.Events()
	if len(events) != 3 {
		t.Fatalf("expected windowed 3 events, got %d", len(events))
	}
	// Export
	var buf bytes.Buffer
	if err := tracker.WriteJSONL(&buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Fatal("WriteJSONL produced empty output")
	}
	// Verify JSONL lines are valid TickEvent JSON
	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(lines) != 3 {
		t.Fatalf("expected 3 JSONL lines, got %d", len(lines))
	}
	for _, line := range lines {
		var ev TickEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			t.Fatalf("unmarshal jsonl line: %v", err)
		}
		if ev.Tree == nil {
			t.Fatalf("expected tree in exported event %d", ev.Iteration)
		}
	}
	// New tracker import
	tracker2 := NewTracker(plan)
	tracker2.WithMaxEvents(100)
	tracker2.WithMaxTrees(100)
	n, err := tracker2.ReadJSONL(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("imported %d want 3", n)
	}
	ev2 := tracker2.Events()
	if len(ev2) != 3 {
		t.Fatalf("after import events len %d want 3", len(ev2))
	}
	for i := range ev2 {
		if ev2[i].Tree == nil {
			t.Fatalf("imported event %d tree nil", ev2[i].Iteration)
		}
		if !TreesEqual(events[i].Tree, ev2[i].Tree) {
			t.Fatalf("tree %d mismatch after round-trip", ev2[i].Iteration)
		}
	}
	// Diff should work after import
	if _, err := tracker2.Diff(ev2[0].Iteration, ev2[1].Iteration); err != nil {
		t.Fatalf("Diff after import: %v", err)
	}
	if len(tracker2.Profile()) == 0 {
		t.Fatal("Profile after import empty")
	}
	if len(tracker2.Timeline()) != 3 {
		t.Fatalf("Timeline len %d want 3", len(tracker2.Timeline()))
	}
}

func TestServer_ExportImport_HTTP(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{{&testCondition{key: "x", value: true}}})
	if err != nil {
		t.Fatal(err)
	}
	tracker := NewTracker(plan)
	srv := NewServer(tracker, "127.0.0.1:0")
	for i := 0; i < 3; i++ {
		plan.Node().Tick()
		tracker.Track(bt.Success, nil)
	}
	// Export via GET /debug/pabt/export
	req := httptest.NewRequest(http.MethodGet, "/debug/pabt/export", nil)
	w := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("export status %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/jsonl" {
		t.Fatalf("Content-Type %q want application/jsonl", ct)
	}
	body := w.Body.Bytes()
	if len(body) == 0 {
		t.Fatal("empty export body")
	}
	// Import into new server/tracker via POST /debug/pabt/import
	tracker2 := NewTracker(plan)
	srv2 := NewServer(tracker2, "127.0.0.1:0")
	req2 := httptest.NewRequest(http.MethodPost, "/debug/pabt/import", bytes.NewReader(body))
	w2 := httptest.NewRecorder()
	srv2.server.Handler.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("import status %d: %s", w2.Code, w2.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w2.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if int(resp["imported"].(float64)) != 3 {
		t.Fatalf("imported count %v want 3", resp["imported"])
	}
	if len(tracker2.Events()) != 3 {
		t.Fatalf("tracker2 events %d want 3", len(tracker2.Events()))
	}
	// Timeline navigation should work
	req3 := httptest.NewRequest(http.MethodGet, "/debug/pabt/timeline/1", nil)
	w3 := httptest.NewRecorder()
	srv2.server.Handler.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("timeline iter 1 after import %d: %s", w3.Code, w3.Body.String())
	}
}

func TestTracker_OverflowFile_EvictionNotLost(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{{&testCondition{key: "x", value: true}}})
	if err != nil {
		t.Fatal(err)
	}
	tracker := NewTracker(plan)
	tmp, err := os.CreateTemp("", "pabt-overflow-*.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	path := tmp.Name()
	tmp.Close()
	defer os.Remove(path)
	if err := tracker.SetOverflowFile(path); err != nil {
		t.Fatal(err)
	}
	defer tracker.CloseOverflow()
	tracker.WithMaxEvents(5)
	tracker.WithMaxTrees(5)
	for i := 0; i < 10; i++ {
		plan.Node().Tick()
		tracker.Track(bt.Success, nil)
	}
	if len(tracker.Events()) != 5 {
		t.Fatalf("windowed events %d want 5", len(tracker.Events()))
	}
	tracker.CloseOverflow()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("overflow file empty, expected evicted events persisted")
	}
	lines := bytes.Split(bytes.TrimSpace(data), []byte("\n"))
	// First 5 ticks should have been evicted and persisted
	if len(lines) < 5 {
		t.Fatalf("overflow lines %d want >=5, body %q", len(lines), string(data[:500]))
	}
	// Verify overflow content is valid JSONL and contains iteration 1
	found1 := false
	for _, line := range lines {
		var ev TickEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			t.Fatalf("overflow jsonl unmarshal: %v line %q", err, string(line[:200]))
		}
		if ev.Iteration == 1 {
			found1 = true
		}
	}
	if !found1 {
		t.Error("overflow file should contain evicted iteration 1")
	}
	// WriteJSONL should merge overflow + window
	var buf bytes.Buffer
	if err := tracker.WriteJSONL(&buf); err != nil {
		t.Fatal(err)
	}
	// After closing overflow, we re-open for WriteJSONL merge path which reads path
	// Count lines: should be 10 (5 evicted + 5 window)
	mergedLines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	if len(mergedLines) != 10 {
		t.Fatalf("merged WriteJSONL lines %d want 10", len(mergedLines))
	}
}
