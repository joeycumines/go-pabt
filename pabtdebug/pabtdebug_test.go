package pabtdebug

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt"
)

func TestTracker_NewTracker(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{
		{&testCondition{key: "x", value: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tracker := NewTracker(plan)
	if tracker == nil {
		t.Fatal("expected non-nil tracker")
	}
	if tracker.plan != plan {
		t.Error("plan mismatch")
	}
}

func TestTracker_Track(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{
		{&testCondition{key: "x", value: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tracker := NewTracker(plan)
	tracker.Track(bt.Success, nil)

	events := tracker.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Iteration != 1 {
		t.Errorf("expected iteration 1, got %d", events[0].Iteration)
	}
	if events[0].Status != bt.Success {
		t.Errorf("expected status Success, got %v", events[0].Status)
	}
	if events[0].Tree == nil {
		t.Error("expected non-nil tree")
	}
}

func TestTracker_BuildTree(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{
		{&testCondition{key: "x", value: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tracker := NewTracker(plan)
	tree := tracker.BuildTree()

	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
	if tree.ID != "0" {
		t.Errorf("expected root ID '0', got %q", tree.ID)
	}
	if tree.NodeType == "" {
		t.Error("expected non-empty node type")
	}
}

func TestServer_PlansEndpoint(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{
		{&testCondition{key: "x", value: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tracker := NewTracker(plan)
	srv := NewServer(tracker, "127.0.0.1:0")

	req := httptest.NewRequest(http.MethodGet, "/debug/pabt/plans", nil)
	w := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var result []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 plan, got %d", len(result))
	}
}

func TestServer_PlanDetailEndpoint(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{
		{&testCondition{key: "x", value: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tracker := NewTracker(plan)
	srv := NewServer(tracker, "127.0.0.1:0")

	req := httptest.NewRequest(http.MethodGet, "/debug/pabt/plans/0", nil)
	w := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var tree TreeNode
	if err := json.Unmarshal(w.Body.Bytes(), &tree); err != nil {
		t.Fatal(err)
	}
	if tree.ID != "0" {
		t.Errorf("expected root ID '0', got %q", tree.ID)
	}
}

func TestHub_Broadcast(t *testing.T) {
	hub := NewHub()
	event := SSEEvent{
		Iteration: 1,
		Status:    bt.Running,
		Timestamp: time.Now(),
	}

	hub.Broadcast(event)
}

func TestHub_ServeHTTP(t *testing.T) {
	hub := NewHub()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/debug/pabt/plans/0/events", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		hub.ServeHTTP(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)

	hub.Broadcast(SSEEvent{
		Iteration: 1,
		Status:    bt.Running,
		Timestamp: time.Now(),
	})

	time.Sleep(50 * time.Millisecond)

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit after context cancellation")
	}

	body := w.Body.String()
	if !strings.Contains(body, "data:") {
		t.Errorf("expected SSE data in response, got: %s", body)
	}
}

func TestUIEndpoint(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{
		{&testCondition{key: "x", value: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tracker := NewTracker(plan)
	srv := NewServer(tracker, "127.0.0.1:0")

	req := httptest.NewRequest(http.MethodGet, "/debug/pabt/ui", nil)
	w := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "PA-BT Debug") {
		t.Error("expected HTML with PA-BT Debug title")
	}
}

func TestTracker_Timeline(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{
		{&testCondition{key: "x", value: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tracker := NewTracker(plan)

	entries := tracker.Timeline()
	if len(entries) != 0 {
		t.Fatalf("expected 0 timeline entries for empty tracker, got %d", len(entries))
	}

	tracker.Track(bt.Success, nil)
	tracker.Track(bt.Running, nil)

	entries = tracker.Timeline()
	if len(entries) != 2 {
		t.Fatalf("expected 2 timeline entries, got %d", len(entries))
	}
	if entries[0].Iteration != 1 {
		t.Errorf("expected iteration 1, got %d", entries[0].Iteration)
	}
	if entries[0].Status != "Success" {
		t.Errorf("expected status 'Success', got %q", entries[0].Status)
	}
	if entries[1].Iteration != 2 {
		t.Errorf("expected iteration 2, got %d", entries[1].Iteration)
	}
	if entries[1].Status != "Running" {
		t.Errorf("expected status 'Running', got %q", entries[1].Status)
	}
}

func TestTracker_EventAt(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{
		{&testCondition{key: "x", value: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tracker := NewTracker(plan)
	tracker.Track(bt.Success, nil)
	tracker.Track(bt.Failure, nil)

	event, ok := tracker.EventAt(1)
	if !ok {
		t.Fatal("expected to find event at iteration 1")
	}
	if event.Iteration != 1 {
		t.Errorf("expected iteration 1, got %d", event.Iteration)
	}
	if event.Status != bt.Success {
		t.Errorf("expected status Success, got %v", event.Status)
	}

	event, ok = tracker.EventAt(2)
	if !ok {
		t.Fatal("expected to find event at iteration 2")
	}
	if event.Status != bt.Failure {
		t.Errorf("expected status Failure, got %v", event.Status)
	}

	event, ok = tracker.EventAt(999)
	if ok {
		t.Error("expected no event for non-existent iteration")
	}
	if event != nil {
		t.Error("expected nil event for non-existent iteration")
	}
}

func TestTracker_Profile(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{
		{&testCondition{key: "x", value: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tracker := NewTracker(plan)

	profiles := tracker.Profile()
	if profiles != nil {
		t.Fatalf("expected nil profile for empty tracker, got %v", profiles)
	}

	tracker.Track(bt.Success, nil)
	tracker.Track(bt.Running, nil)

	profiles = tracker.Profile()
	if len(profiles) == 0 {
		t.Fatal("expected non-empty profile slice after tracking events")
	}

	for _, p := range profiles {
		if p.TickCount <= 0 {
			t.Errorf("expected TickCount > 0 for profile %s, got %d", p.ID, p.TickCount)
		}
	}
}

func TestTracker_Search(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{
		{&testCondition{key: "x", value: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tracker := NewTracker(plan)

	results := tracker.Search("GoalRoot")
	if len(results) == 0 {
		t.Error("expected at least one result for 'GoalRoot' search")
	}

	results = tracker.Search("goalroot")
	if len(results) == 0 {
		t.Error("expected at least one result for case-insensitive 'goalroot' search")
	}

	results = tracker.Search("zzzznonexistentnode12345")
	if len(results) != 0 {
		t.Errorf("expected no results for nonsense query, got %d", len(results))
	}
}

func TestTracker_Diff(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{
		{&testCondition{key: "x", value: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tracker := NewTracker(plan)
	tracker.Track(bt.Success, nil)
	tracker.Track(bt.Running, nil)

	result, err := tracker.Diff(1, 2)
	if err != nil {
		t.Fatalf("unexpected error from Diff(1, 2): %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil DiffResult")
	}
	if result.FromIteration != 1 {
		t.Errorf("expected FromIteration 1, got %d", result.FromIteration)
	}
	if result.ToIteration != 2 {
		t.Errorf("expected ToIteration 2, got %d", result.ToIteration)
	}

	_, err = tracker.Diff(1, 999)
	if err == nil {
		t.Error("expected error for Diff with non-existent iteration 999")
	}

	_, err = tracker.Diff(999, 1)
	if err == nil {
		t.Error("expected error for Diff with non-existent iteration 999")
	}
}

func TestTracker_Breakpoints(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{
		{&testCondition{key: "x", value: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tracker := NewTracker(plan)

	bps := tracker.Breakpoints()
	if len(bps) != 0 {
		t.Fatalf("expected 0 breakpoints initially, got %d", len(bps))
	}

	bp := Breakpoint{
		ID:       "bp1",
		NodePath: "0",
		NodeType: "GoalRoot",
		Enabled:  true,
	}
	tracker.SetBreakpoint(bp)

	bps = tracker.Breakpoints()
	if len(bps) != 1 {
		t.Fatalf("expected 1 breakpoint after SetBreakpoint, got %d", len(bps))
	}
	if bps[0].NodePath != "0" {
		t.Errorf("expected NodePath '0', got %q", bps[0].NodePath)
	}
	if bps[0].NodeType != "GoalRoot" {
		t.Errorf("expected NodeType 'GoalRoot', got %q", bps[0].NodeType)
	}

	tracker.RemoveBreakpoint("0")
	bps = tracker.Breakpoints()
	if len(bps) != 0 {
		t.Fatalf("expected 0 breakpoints after RemoveBreakpoint, got %d", len(bps))
	}
}

func TestTracker_BreakpointHit(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{
		{&testCondition{key: "x", value: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tracker := NewTracker(plan)

	tracker.SetBreakpoint(Breakpoint{
		ID:       "bp1",
		NodePath: "0",
		NodeType: "GoalRoot",
		Enabled:  true,
	})

	tracker.Track(bt.Success, nil)

	events := tracker.Events()
	if len(events) == 0 {
		t.Fatal("expected at least one event")
	}
	lastEvent := events[len(events)-1]
	if lastEvent.BreakpointHit == nil {
		t.Error("expected BreakpointHit to be set when breakpoint matches")
	} else if lastEvent.BreakpointHit.NodePath != "0" {
		t.Errorf("expected BreakpointHit.NodePath '0', got %q", lastEvent.BreakpointHit.NodePath)
	}

	tracker.RemoveBreakpoint("0")
	tracker.SetBreakpoint(Breakpoint{
		ID:        "bp2",
		NodePath:  "0",
		NodeType:  "GoalRoot",
		Condition: "Failure",
		Enabled:   true,
	})

	tracker.Track(bt.Success, nil)

	events = tracker.Events()
	lastEvent = events[len(events)-1]
	if lastEvent.BreakpointHit != nil {
		t.Error("expected no BreakpointHit when condition doesn't match")
	}

	tracker.RemoveBreakpoint("0")
	tracker.SetBreakpoint(Breakpoint{
		ID:       "bp3",
		NodePath: "0",
		NodeType: "GoalRoot",
		Enabled:  false,
	})

	tracker.Track(bt.Success, nil)

	events = tracker.Events()
	lastEvent = events[len(events)-1]
	if lastEvent.BreakpointHit != nil {
		t.Error("expected no BreakpointHit for disabled breakpoint")
	}
}

func TestServer_TimelineEndpoint(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{
		{&testCondition{key: "x", value: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tracker := NewTracker(plan)
	tracker.Track(bt.Success, nil)
	srv := NewServer(tracker, "127.0.0.1:0")

	req := httptest.NewRequest(http.MethodGet, "/debug/pabt/timeline", nil)
	w := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var entries []TimelineEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 timeline entry, got %d", len(entries))
	}
}

func TestServer_ProfileEndpoint(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{
		{&testCondition{key: "x", value: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tracker := NewTracker(plan)
	tracker.Track(bt.Success, nil)
	srv := NewServer(tracker, "127.0.0.1:0")

	req := httptest.NewRequest(http.MethodGet, "/debug/pabt/profile", nil)
	w := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var profiles []NodeProfile
	if err := json.Unmarshal(w.Body.Bytes(), &profiles); err != nil {
		t.Fatal(err)
	}
	if len(profiles) == 0 {
		t.Fatal("expected at least one profile entry")
	}
}

func TestServer_SearchEndpoint(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{
		{&testCondition{key: "x", value: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tracker := NewTracker(plan)
	srv := NewServer(tracker, "127.0.0.1:0")

	req := httptest.NewRequest(http.MethodGet, "/debug/pabt/search?q=GoalRoot", nil)
	w := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var results []SearchResult
	if err := json.Unmarshal(w.Body.Bytes(), &results); err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Error("expected at least one search result for 'GoalRoot'")
	}
}

func TestServer_DotEndpoint(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{
		{&testCondition{key: "x", value: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tracker := NewTracker(plan)
	srv := NewServer(tracker, "127.0.0.1:0")

	req := httptest.NewRequest(http.MethodGet, "/debug/pabt/dot", nil)
	w := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/vnd.graphviz") {
		t.Errorf("expected content type containing 'text/vnd.graphviz', got %q", ct)
	}

	body := w.Body.String()
	if !strings.Contains(body, "digraph pabt") {
		t.Error("expected DOT output to contain 'digraph pabt' header")
	}
}

func TestServer_BreakpointsPostEndpoint(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{
		{&testCondition{key: "x", value: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tracker := NewTracker(plan)
	srv := NewServer(tracker, "127.0.0.1:0")

	bp := Breakpoint{
		ID:       "bp1",
		NodePath: "0",
		NodeType: "GoalRoot",
		Enabled:  true,
	}
	body, _ := json.Marshal(bp)
	req := httptest.NewRequest(http.MethodPost, "/debug/pabt/breakpoints", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	bps := tracker.Breakpoints()
	if len(bps) != 1 {
		t.Fatalf("expected 1 breakpoint after POST, got %d", len(bps))
	}
}

func TestServer_BreakpointsGetEndpoint(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{
		{&testCondition{key: "x", value: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tracker := NewTracker(plan)
	tracker.SetBreakpoint(Breakpoint{
		ID:       "bp1",
		NodePath: "0",
		NodeType: "GoalRoot",
		Enabled:  true,
	})
	srv := NewServer(tracker, "127.0.0.1:0")

	req := httptest.NewRequest(http.MethodGet, "/debug/pabt/breakpoints", nil)
	w := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var bps []Breakpoint
	if err := json.Unmarshal(w.Body.Bytes(), &bps); err != nil {
		t.Fatal(err)
	}
	if len(bps) != 1 {
		t.Fatalf("expected 1 breakpoint, got %d", len(bps))
	}
	if bps[0].NodePath != "0" {
		t.Errorf("expected NodePath '0', got %q", bps[0].NodePath)
	}
}

func TestServer_BreakpointDeleteEndpoint(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{
		{&testCondition{key: "x", value: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tracker := NewTracker(plan)
	tracker.SetBreakpoint(Breakpoint{
		ID:       "bp1",
		NodePath: "0",
		NodeType: "GoalRoot",
		Enabled:  true,
	})
	srv := NewServer(tracker, "127.0.0.1:0")

	req := httptest.NewRequest(http.MethodDelete, "/debug/pabt/breakpoints/0", nil)
	w := httptest.NewRecorder()
	srv.server.Handler.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", w.Code)
	}

	bps := tracker.Breakpoints()
	if len(bps) != 0 {
		t.Fatalf("expected 0 breakpoints after DELETE, got %d", len(bps))
	}
}

func TestTreeToDot(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{
		{&testCondition{key: "x", value: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tracker := NewTracker(plan)
	tree := tracker.BuildTree()

	dot := treeToDot(tree)

	if !strings.Contains(dot, "digraph pabt") {
		t.Error("expected DOT output to contain 'digraph pabt' header")
	}
	if !strings.Contains(dot, "GoalRoot") {
		t.Error("expected DOT output to contain 'GoalRoot' node type")
	}
	if !strings.Contains(dot, "->") {
		t.Error("expected DOT output to contain edge declarations")
	}

	dot = treeToDot(nil)
	if !strings.Contains(dot, "digraph pabt") {
		t.Error("expected DOT output for nil tree to still contain 'digraph pabt' header")
	}
}

func TestTreeNode_StructureHash(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{
		{&testCondition{key: "x", value: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tracker := NewTracker(plan)
	tree := tracker.BuildTree()

	if tree == nil {
		t.Fatal("expected non-nil tree")
	}
	if tree.StructureHash == "" {
		t.Error("expected non-empty StructureHash for root node")
	}
	if !strings.Contains(tree.StructureHash, ":") {
		t.Errorf("expected StructureHash to contain ':', got %q", tree.StructureHash)
	}
}

func TestTickEvent_DurationMs(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{
		{&testCondition{key: "x", value: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tracker := NewTracker(plan)

	tracker.Track(bt.Success, nil)
	events := tracker.Events()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].DurationMs != 0 {
		t.Errorf("expected DurationMs 0 for first event, got %f", events[0].DurationMs)
	}

	tracker.Track(bt.Running, nil)
	events = tracker.Events()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[1].DurationMs <= 0 {
		t.Errorf("expected DurationMs > 0 for second event, got %f", events[1].DurationMs)
	}
}

func TestTracker_TreeStoreEviction(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{
		{&testCondition{key: "x", value: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tracker := NewTracker(plan)
	tracker.WithMaxTrees(3)

	for i := 0; i < 5; i++ {
		tracker.Track(bt.Success, nil)
	}

	events := tracker.Events()
	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %d", len(events))
	}

	if events[0].Tree != nil {
		t.Error("expected tree for iteration 1 to be evicted (nil), but it was present")
	}
	if events[1].Tree != nil {
		t.Error("expected tree for iteration 2 to be evicted (nil), but it was present")
	}
	if events[2].Tree == nil {
		t.Error("expected tree for iteration 3 to be retained, but it was nil")
	}
	if events[3].Tree == nil {
		t.Error("expected tree for iteration 4 to be retained, but it was nil")
	}
	if events[4].Tree == nil {
		t.Error("expected tree for iteration 5 to be retained, but it was nil")
	}

	_, err = tracker.Diff(1, 3)
	if err == nil {
		t.Error("expected error for Diff with evicted tree at iteration 1")
	}

	result, err := tracker.Diff(3, 5)
	if err != nil {
		t.Fatalf("unexpected error for Diff with retained trees: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil DiffResult")
	}
}

func TestTracker_EventIndexLookup(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{
		{&testCondition{key: "x", value: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tracker := NewTracker(plan)

	for i := 0; i < 10; i++ {
		tracker.Track(bt.Success, nil)
	}

	event, ok := tracker.EventAt(5)
	if !ok {
		t.Fatal("expected to find event at iteration 5")
	}
	if event.Iteration != 5 {
		t.Errorf("expected iteration 5, got %d", event.Iteration)
	}
	if event.Tree == nil {
		t.Error("expected non-nil tree for iteration 5")
	}

	event, ok = tracker.EventAt(1)
	if !ok {
		t.Fatal("expected to find event at iteration 1")
	}
	if event.Iteration != 1 {
		t.Errorf("expected iteration 1, got %d", event.Iteration)
	}

	event, ok = tracker.EventAt(999)
	if ok {
		t.Error("expected no event for non-existent iteration 999")
	}
	if event != nil {
		t.Error("expected nil event for non-existent iteration 999")
	}

	tracker.WithMaxEvents(5)
	for i := 0; i < 5; i++ {
		tracker.Track(bt.Running, nil)
	}

	_, ok = tracker.EventAt(1)
	if ok {
		t.Error("expected iteration 1 to be evicted after maxEvents trim")
	}
	_, ok = tracker.EventAt(10)
	if ok {
		t.Error("expected iteration 10 to be evicted after maxEvents trim")
	}

	event, ok = tracker.EventAt(11)
	if !ok {
		t.Fatal("expected to find event at iteration 11 after trim")
	}
	if event.Iteration != 11 {
		t.Errorf("expected iteration 11, got %d", event.Iteration)
	}
}

func TestTracker_ProfileCaching(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{
		{&testCondition{key: "x", value: true}},
	})
	if err != nil {
		t.Fatal(err)
	}

	tracker := NewTracker(plan)

	profiles := tracker.Profile()
	if profiles != nil {
		t.Fatalf("expected nil profile for empty tracker, got %v", profiles)
	}

	tracker.Track(bt.Success, nil)
	profiles = tracker.Profile()
	if len(profiles) == 0 {
		t.Fatal("expected non-empty profile after first track")
	}

	firstTickCount := profiles[0].TickCount

	tracker.Track(bt.Running, nil)
	profiles = tracker.Profile()
	if len(profiles) == 0 {
		t.Fatal("expected non-empty profile after second track")
	}

	for _, p := range profiles {
		if p.TickCount < firstTickCount {
			t.Errorf("expected TickCount >= %d after second track, got %d for %s", firstTickCount, p.TickCount, p.ID)
		}
		if p.AvgDurationMs <= 0 && p.TotalDurationMs > 0 {
			t.Errorf("expected AvgDurationMs > 0 when TotalDurationMs > 0 for %s", p.ID)
		}
	}

	tracker.WithMaxTrees(0)
	tracker.Track(bt.Failure, nil)
	profiles = tracker.Profile()
	if len(profiles) == 0 {
		t.Fatal("expected non-empty profile even after tree eviction")
	}

	for _, p := range profiles {
		if p.TickCount <= 0 {
			t.Errorf("expected TickCount > 0 for profile %s, got %d", p.ID, p.TickCount)
		}
	}
}

type testState struct {
	vars map[any]any
}

func (s *testState) Variable(key any) (any, error) {
	v, ok := s.vars[key]
	if !ok {
		return nil, nil
	}
	return v, nil
}

func (s *testState) Actions(failed pabt.Condition) ([]pabt.IAction, error) {
	return nil, nil
}

type testCondition struct {
	key   any
	value any
}

func (c *testCondition) Key() any             { return c.key }
func (c *testCondition) Match(value any) bool { return value == c.value }

func TestHub_SequenceNumbers(t *testing.T) {
	hub := NewHub()
	hub.minInterval = 0

	ch := make(chan string, 64)
	hub.mu.Lock()
	hub.clients[ch] = struct{}{}
	hub.mu.Unlock()
	defer func() {
		hub.mu.Lock()
		delete(hub.clients, ch)
		hub.mu.Unlock()
	}()

	for i := 0; i < 5; i++ {
		hub.Broadcast(SSEEvent{
			Iteration: i + 1,
			Status:    bt.Running,
			Timestamp: time.Now(),
		})
	}

	var lastSeq int64
	for i := 0; i < 5; i++ {
		msg := <-ch
		if !strings.Contains(msg, "id:") {
			t.Errorf("expected SSE id field in message %d: %s", i, msg)
		}
		if !strings.Contains(msg, `"seq"`) {
			t.Errorf("expected seq field in JSON data in message %d: %s", i, msg)
		}

		var event SSEEvent
		dataStart := strings.Index(msg, "data: ")
		if dataStart == -1 {
			t.Fatalf("no data line in message %d", i)
		}
		dataEnd := strings.Index(msg[dataStart:], "\n\n")
		if dataEnd == -1 {
			dataEnd = len(msg) - dataStart
		}
		jsonData := msg[dataStart+6 : dataStart+dataEnd]
		if err := json.Unmarshal([]byte(jsonData), &event); err != nil {
			t.Fatalf("failed to unmarshal event %d: %v", i, err)
		}
		if event.Seq <= lastSeq {
			t.Errorf("expected seq > %d, got %d for event %d", lastSeq, event.Seq, i)
		}
		lastSeq = event.Seq
	}
}

func TestHub_ReplayOnReconnect(t *testing.T) {
	hub := NewHub()
	hub.minInterval = 0

	for i := 0; i < 3; i++ {
		hub.Broadcast(SSEEvent{
			Iteration: i + 1,
			Status:    bt.Running,
			Timestamp: time.Now(),
		})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/debug/pabt/plans/0/events", nil)
	req.Header.Set("Last-Event-ID", "2")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		hub.ServeHTTP(w, req)
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit after context cancellation")
	}

	body := w.Body.String()
	dataLines := strings.Count(body, "data: ")
	if dataLines != 3 {
		t.Errorf("expected 3 replayed data lines for reconnecting client, got %d", dataLines)
	}
}

func TestHub_GracefulClose(t *testing.T) {
	hub := NewHub()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/debug/pabt/plans/0/events", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		hub.ServeHTTP(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)

	hub.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit after Hub.Close()")
	}

	hub.mu.Lock()
	remaining := len(hub.clients)
	hub.mu.Unlock()
	if remaining != 0 {
		t.Errorf("expected 0 clients after Close, got %d", remaining)
	}
}
