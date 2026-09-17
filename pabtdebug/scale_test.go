package pabtdebug

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt"
)

// TestTimelineWindowing verifies O(1) append + sliding window at 5000 events.
// Task 63: after 5000 ticks, len(entries)==1000 (maxEvents cap), oldest==4001, newest==5000, EventAt O(1).
func TestTimelineWindowing(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{{&testCondition{key: "x", value: true}}})
	if err != nil {
		t.Fatal(err)
	}
	tracker := NewTracker(plan)
	tracker.WithMaxEvents(1000)
	tracker.WithMaxTrees(1000)

	const totalTicks = 5000
	for i := 0; i < totalTicks; i++ {
		plan.Node().Tick()
		tracker.Track(bt.Running, nil)
	}

	events := tracker.Events()
	if len(events) != 1000 {
		t.Fatalf("expected 1000 windowed events, got %d", len(events))
	}
	oldest := events[0].Iteration
	newest := events[len(events)-1].Iteration
	if oldest != 4001 {
		t.Errorf("expected oldest iteration 4001, got %d", oldest)
	}
	if newest != 5000 {
		t.Errorf("expected newest iteration 5000, got %d", newest)
	}

	// EventAt must be O(1) — verify it works for first, last, and mid-window
	for _, iter := range []int{4001, 4500, 5000} {
		ev, ok := tracker.EventAt(iter)
		if !ok {
			t.Fatalf("EventAt(%d) not found", iter)
		}
		if ev.Iteration != iter {
			t.Errorf("EventAt(%d) returned iteration %d", iter, ev.Iteration)
		}
	}

	// Evicted iteration must not be found
	_, ok := tracker.EventAt(4000)
	if ok {
		t.Error("EventAt(4000) should have been evicted")
	}

	tl := tracker.Timeline()
	if len(tl) != 1000 {
		t.Fatalf("Timeline() length %d want 1000", len(tl))
	}
}

// TestExportImportScale verifies 5000-event JSONL lossless round-trip.
// Task 64: burst 5000, WriteJSONL, ReadJSONL into fresh tracker, Events deep equal, BuildTree structurally equal, Profile same entries.
func TestExportImportScale(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{{&testCondition{key: "x", value: true}}})
	if err != nil {
		t.Fatal(err)
	}
	tracker := NewTracker(plan)
	tracker.WithMaxEvents(5000)
	tracker.WithMaxTrees(5000)
	tracker.WithMaxTreeNodes(0) // disable content budget for lossless test
	tracker.WithMaxDeltaNodes(0)

	const totalTicks = 5000
	for i := 0; i < totalTicks; i++ {
		plan.Node().Tick()
		tracker.Track(bt.Running, nil)
	}

	events := tracker.Events()
	if len(events) != totalTicks {
		t.Fatalf("expected %d events, got %d", totalTicks, len(events))
	}

	var buf bytes.Buffer
	if err := tracker.WriteJSONL(&buf); err != nil {
		t.Fatalf("WriteJSONL: %v", err)
	}

	tracker2 := NewTracker(plan)
	tracker2.WithMaxEvents(10000)
	tracker2.WithMaxTrees(10000)
	tracker2.WithMaxTreeNodes(0)
	tracker2.WithMaxDeltaNodes(0)
	n, err := tracker2.ReadJSONL(&buf)
	if err != nil {
		t.Fatalf("ReadJSONL: %v", err)
	}
	if n != totalTicks {
		t.Fatalf("imported %d events, want %d", n, totalTicks)
	}

	events2 := tracker2.Events()
	if len(events2) != len(events) {
		t.Fatalf("imported events length %d != original %d", len(events2), len(events))
	}
	for i := range events {
		if events[i].Iteration != events2[i].Iteration {
			t.Fatalf("iteration mismatch at index %d: %d vs %d", i, events[i].Iteration, events2[i].Iteration)
		}
		if events[i].Status != events2[i].Status {
			t.Fatalf("status mismatch at iteration %d", events[i].Iteration)
		}
		if !TreesEqual(events[i].Tree, events2[i].Tree) {
			t.Fatalf("tree mismatch at iteration %d", events[i].Iteration)
		}
	}

	// BuildTree structural equality
	tree1 := tracker.BuildTree()
	tree2 := tracker2.BuildTree()
	if !TreesEqual(tree1, tree2) {
		t.Fatal("BuildTree() differs between original and imported tracker")
	}

	// Profile entry count match
	p1 := tracker.Profile()
	p2 := tracker2.Profile()
	if len(p1) != len(p2) {
		t.Fatalf("Profile() length mismatch: %d vs %d", len(p1), len(p2))
	}
}

// TestSearchPerformance verifies substring search over a ~24k-node tree completes under 500ms.
// Task 67: build a synthetic large tree, time searchTree calls.
func TestSearchPerformance(t *testing.T) {
	// Build a synthetic tree with ~24k nodes for realistic performance measurement.
	// Structure: root -> 10 selectors -> 10 PPA roots -> 10 action selectors -> 24 action nodes each
	// = 1 + 10 + 100 + 1000 + 24000 ≈ 25111 nodes
	root := &TreeNode{ID: "0", Name: "GoalRoot", NodeType: "GoalRoot", StructureHash: "GoalRoot:10"}
	for i := 0; i < 10; i++ {
		sel := TreeNode{
			ID: fmt.Sprintf("0.%d", i), Name: "GoalSelector",
			NodeType: "GoalSelector", StructureHash: "GoalSelector:10",
		}
		for j := 0; j < 10; j++ {
			ppa := TreeNode{
				ID: fmt.Sprintf("0.%d.%d", i, j), Name: "PPARoot",
				NodeType: "PPARoot", StructureHash: "PPARoot:10",
			}
			for k := 0; k < 10; k++ {
				actSel := TreeNode{
					ID: fmt.Sprintf("0.%d.%d.%d", i, j, k), Name: "ActionSelector",
					NodeType: "ActionSelector", StructureHash: "ActionSelector:24",
				}
				for l := 0; l < 24; l++ {
					leaf := TreeNode{
						ID:       fmt.Sprintf("0.%d.%d.%d.%d", i, j, k, l),
						Name:     fmt.Sprintf("action-%d-%d-%d-%d", i, j, k, l),
						NodeType: "ActionNode", StructureHash: "ActionNode:0",
					}
					actSel.Children = append(actSel.Children, leaf)
				}
				ppa.Children = append(ppa.Children, actSel)
			}
			sel.Children = append(sel.Children, ppa)
		}
		root.Children = append(root.Children, sel)
	}

	nodeCount := countNodes(root)
	t.Logf("synthetic tree node count: %d", nodeCount)
	if nodeCount < 20000 {
		t.Fatalf("expected >=20k nodes, got %d", nodeCount)
	}

	queries := []string{"Selector", "Unknown", "Goal", "Action", "action-5-5"}
	for _, q := range queries {
		lowerQ := strings.ToLower(q)
		var results []SearchResult
		start := time.Now()
		searchTree(root, "", lowerQ, &results)
		elapsed := time.Since(start)
		if elapsed > 500*time.Millisecond {
			t.Errorf("searchTree(%q) took %v (>500ms), returned %d results", q, elapsed, len(results))
		}
		t.Logf("searchTree(%q): %v, %d results", q, elapsed, len(results))
	}
}

// TestDiffIntegrity verifies added/changed/removed counts match actual tree delta.
// Task 68: capture trees at two iterations, compute Diff, manually walk and compare.
func TestDiffIntegrity(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{{&testCondition{key: "x", value: true}}})
	if err != nil {
		t.Fatal(err)
	}
	tracker := NewTracker(plan)
	tracker.WithMaxEvents(200)
	tracker.WithMaxTrees(200)

	// Tick 50 times to establish baseline tree
	for i := 0; i < 50; i++ {
		plan.Node().Tick()
		tracker.Track(bt.Running, nil)
	}

	// Tick 50 more times so status tick counts change
	for i := 0; i < 50; i++ {
		plan.Node().Tick()
		tracker.Track(bt.Running, nil)
	}

	diff, err := tracker.Diff(50, 100)
	if err != nil {
		t.Fatalf("Diff(50, 100): %v", err)
	}

	// Manually compute expected diff by flattening both trees
	ev50, _ := tracker.EventAt(50)
	ev100, _ := tracker.EventAt(100)
	if ev50.Tree == nil || ev100.Tree == nil {
		t.Fatal("tree snapshots missing for diff verification")
	}

	fromNodes := flattenTree(ev50.Tree)
	toNodes := flattenTree(ev100.Tree)

	var expectedAdded, expectedRemoved, expectedChanged int
	for id := range toNodes {
		if _, exists := fromNodes[id]; !exists {
			expectedAdded++
		} else {
			fn := fromNodes[id]
			tn := toNodes[id]
			oldStatus := nodeStatusString(fn.Status)
			newStatus := nodeStatusString(tn.Status)
			if oldStatus != newStatus || effectsDiffer(fn.Effects, tn.Effects) {
				expectedChanged++
			}
		}
	}
	for id := range fromNodes {
		if _, exists := toNodes[id]; !exists {
			expectedRemoved++
		}
	}

	if len(diff.Added) != expectedAdded {
		t.Errorf("Added count %d != expected %d", len(diff.Added), expectedAdded)
	}
	if len(diff.Removed) != expectedRemoved {
		t.Errorf("Removed count %d != expected %d", len(diff.Removed), expectedRemoved)
	}
	if len(diff.Changed) != expectedChanged {
		t.Errorf("Changed count %d != expected %d", len(diff.Changed), expectedChanged)
	}
}
