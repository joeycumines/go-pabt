package pabtdebug

import (
	"bytes"
	"strings"
	"testing"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt"
)

// TestDotExport verifies graphviz output is valid for a non-trivial tree.
// Task 66: after 100 ticks, DOT output starts with "digraph", contains correct node count, valid edge syntax.
func TestDotExport(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{{&testCondition{key: "x", value: true}}})
	if err != nil {
		t.Fatal(err)
	}
	tracker := NewTracker(plan)
	tracker.WithMaxEvents(200)
	tracker.WithMaxTrees(200)

	for i := 0; i < 100; i++ {
		plan.Node().Tick()
		tracker.Track(bt.Running, nil)
	}

	tree := tracker.BuildTree()
	if tree == nil {
		t.Fatal("BuildTree returned nil")
	}

	dot := treeToDot(tree)

	if !strings.HasPrefix(dot, "digraph pabt {") {
		t.Errorf("DOT output missing digraph header: %q", dot[:min(len(dot), 50)])
	}
	if !strings.HasSuffix(strings.TrimSpace(dot), "}") {
		t.Error("DOT output missing closing brace")
	}

	// Count nodes in tree manually
	expectedNodes := countNodes(tree)
	// Each node produces one quoted ID declaration line
	declCount := strings.Count(dot, " [label=")
	if declCount != expectedNodes {
		t.Errorf("DOT node declarations %d != tree node count %d", declCount, expectedNodes)
	}

	// Each parent-child relationship produces one edge line
	edgeCount := strings.Count(dot, " -> ")
	// Edges = total nodes - 1 (root has no incoming edge) for a tree
	expectedEdges := expectedNodes - 1
	if expectedEdges < 0 {
		expectedEdges = 0
	}
	if edgeCount != expectedEdges {
		t.Errorf("DOT edge count %d != expected %d", edgeCount, expectedEdges)
	}
}

// TestReadJSONLFaultTolerance verifies malformed JSONL lines are handled gracefully.
// Task 69: feed ReadJSONL a mix of valid and invalid JSON lines, assert it returns error
// with count of successfully parsed events, does not panic.
func TestReadJSONLFaultTolerance(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{{&testCondition{key: "x", value: true}}})
	if err != nil {
		t.Fatal(err)
	}

	// First, generate valid JSONL from a real tracker
	tracker := NewTracker(plan)
	tracker.WithMaxEvents(10)
	tracker.WithMaxTrees(10)
	for i := 0; i < 3; i++ {
		plan.Node().Tick()
		tracker.Track(bt.Success, nil)
	}
	var validBuf bytes.Buffer
	if err := tracker.WriteJSONL(&validBuf); err != nil {
		t.Fatal(err)
	}
	validLines := strings.Split(strings.TrimSpace(validBuf.String()), "\n")
	if len(validLines) < 3 {
		t.Fatalf("expected at least 3 valid JSONL lines, got %d", len(validLines))
	}

	// Construct mixed input: valid, garbage, valid, partial JSON, valid
	var mixed bytes.Buffer
	mixed.WriteString(validLines[0] + "\n")
	mixed.WriteString("this is not json\n")
	mixed.WriteString(validLines[1] + "\n")
	mixed.WriteString(`{"iteration": 99, "status": ` + "\n") // truncated JSON
	mixed.WriteString(validLines[2] + "\n")

	tracker2 := NewTracker(plan)
	tracker2.WithMaxEvents(100)
	tracker2.WithMaxTrees(100)

	// Must not panic
	n, err := tracker2.ReadJSONL(&mixed)

	// Should return an error due to malformed lines
	if err == nil {
		t.Error("expected error from malformed JSONL, got nil")
	}

	// ReadJSONL returns len(events) parsed before the error but does NOT commit
	// them to tracker state (the atomic reset at the end never runs on error path).
	// So n >= 1 (parsed first valid line) but Events() remains empty.
	if n < 1 {
		t.Errorf("expected at least 1 successfully parsed event before error, got %d", n)
	}
	t.Logf("ReadJSONL parsed %d events before error: %v", n, err)

	// Tracker state should be unmodified (no atomic commit on error path)
	events := tracker2.Events()
	if len(events) != 0 {
		t.Errorf("Events() should be empty after failed import, got %d", len(events))
	}
}

// TestNilTrackerSafety verifies all public methods handle nil receiver gracefully.
// Task 70: var t *Tracker; calling public methods must not panic.
func TestNilTrackerSafety(t *testing.T) {
	var tr *Tracker

	t.Run("ID", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("ID panicked on nil: %v", r)
			}
		}()
		id := tr.ID()
		if id != "" {
			t.Errorf("expected empty string from ID(), got %q", id)
		}
	})

	t.Run("WithID", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("WithID panicked on nil: %v", r)
			}
		}()
		ret := tr.WithID("test")
		if ret != nil {
			t.Error("expected nil from WithID()")
		}
	})

	t.Run("Track", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Track panicked on nil: %v", r)
			}
		}()
		tr.Track(bt.Success, nil)
	})

	t.Run("Events", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Events panicked on nil: %v", r)
			}
		}()
		ev := tr.Events()
		if ev != nil {
			t.Errorf("expected nil from Events(), got %v", ev)
		}
	})

	t.Run("Hub", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Hub panicked on nil: %v", r)
			}
		}()
		h := tr.Hub()
		if h != nil {
			t.Error("expected nil from Hub()")
		}
	})

	t.Run("BuildTree", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("BuildTree panicked on nil: %v", r)
			}
		}()
		tree := tr.BuildTree()
		if tree != nil {
			t.Error("expected nil from BuildTree()")
		}
	})

	t.Run("Timeline", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Timeline panicked on nil: %v", r)
			}
		}()
		tl := tr.Timeline()
		if tl != nil {
			t.Error("expected nil from Timeline()")
		}
	})

	t.Run("EventAt", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("EventAt panicked on nil: %v", r)
			}
		}()
		ev, ok := tr.EventAt(1)
		if ev != nil || ok {
			t.Error("expected nil,false from EventAt()")
		}
	})

	t.Run("Profile", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Profile panicked on nil: %v", r)
			}
		}()
		p := tr.Profile()
		if p != nil {
			t.Error("expected nil from Profile()")
		}
	})

	t.Run("Search", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Search panicked on nil: %v", r)
			}
		}()
		sr := tr.Search("x")
		if sr != nil {
			t.Error("expected nil from Search()")
		}
	})

	t.Run("Diff", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Diff panicked on nil: %v", r)
			}
		}()
		d, err := tr.Diff(1, 2)
		if d != nil || err == nil {
			t.Error("expected nil,error from Diff()")
		}
	})

	t.Run("SetBreakpoint", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SetBreakpoint panicked on nil: %v", r)
			}
		}()
		tr.SetBreakpoint(Breakpoint{})
	})

	t.Run("RemoveBreakpoint", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("RemoveBreakpoint panicked on nil: %v", r)
			}
		}()
		tr.RemoveBreakpoint("0")
	})

	t.Run("Breakpoints", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Breakpoints panicked on nil: %v", r)
			}
		}()
		bp := tr.Breakpoints()
		if bp != nil {
			t.Error("expected nil from Breakpoints()")
		}
	})

	t.Run("WithMaxEvents", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("WithMaxEvents panicked on nil: %v", r)
			}
		}()
		ret := tr.WithMaxEvents(100)
		if ret != nil {
			t.Error("expected nil from WithMaxEvents()")
		}
	})

	t.Run("WithMaxTrees", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("WithMaxTrees panicked on nil: %v", r)
			}
		}()
		ret := tr.WithMaxTrees(100)
		if ret != nil {
			t.Error("expected nil from WithMaxTrees()")
		}
	})

	t.Run("WithMaxTreeNodes", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("WithMaxTreeNodes panicked on nil: %v", r)
			}
		}()
		ret := tr.WithMaxTreeNodes(100)
		if ret != nil {
			t.Error("expected nil from WithMaxTreeNodes()")
		}
	})

	t.Run("WithMaxDeltaNodes", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("WithMaxDeltaNodes panicked on nil: %v", r)
			}
		}()
		ret := tr.WithMaxDeltaNodes(100)
		if ret != nil {
			t.Error("expected nil from WithMaxDeltaNodes()")
		}
	})

	t.Run("WriteJSONL", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("WriteJSONL panicked on nil: %v", r)
			}
		}()
		var buf bytes.Buffer
		err := tr.WriteJSONL(&buf)
		if err == nil {
			t.Error("expected error from WriteJSONL()")
		}
	})

	t.Run("ReadJSONL", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("ReadJSONL panicked on nil: %v", r)
			}
		}()
		var buf bytes.Buffer
		n, err := tr.ReadJSONL(&buf)
		if n != 0 || err == nil {
			t.Error("expected 0,error from ReadJSONL()")
		}
	})

	t.Run("DeltaForIteration", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("DeltaForIteration panicked on nil: %v", r)
			}
		}()
		d, ok := tr.DeltaForIteration(1)
		if d != nil || ok {
			t.Error("expected nil,false from DeltaForIteration()")
		}
	})

	t.Run("WithKeyframeInterval", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("WithKeyframeInterval panicked on nil: %v", r)
			}
		}()
		ret := tr.WithKeyframeInterval(10)
		if ret != nil {
			t.Error("expected nil from WithKeyframeInterval()")
		}
	})

	t.Run("KeyframeInterval", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("KeyframeInterval panicked on nil: %v", r)
			}
		}()
		ki := tr.KeyframeInterval()
		if ki != 0 {
			t.Errorf("expected 0 from KeyframeInterval(), got %d", ki)
		}
	})

	t.Run("SetOverflowFile", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("SetOverflowFile panicked on nil: %v", r)
			}
		}()
		err := tr.SetOverflowFile("/dev/null")
		if err == nil {
			t.Error("expected error from SetOverflowFile()")
		}
	})

	t.Run("CloseOverflow", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("CloseOverflow panicked on nil: %v", r)
			}
		}()
		_ = tr.CloseOverflow()
	})

	t.Run("OverflowPath", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("OverflowPath panicked on nil: %v", r)
			}
		}()
		p := tr.OverflowPath()
		if p != "" {
			t.Error("expected empty string from OverflowPath()")
		}
	})
}
