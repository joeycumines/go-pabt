/*
   Copyright 2021 Joseph Cumines

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package pabt

import (
	"encoding/json"
	"testing"

	bt "github.com/joeycumines/go-behaviortree"
)

func TestNodeType_String(t *testing.T) {
	tests := []struct {
		input    NodeType
		expected string
	}{
		{NodeTypeUnknown, "Unknown"},
		{NodeTypeGoalRoot, "GoalRoot"},
		{NodeTypeGoalSelector, "GoalSelector"},
		{NodeTypePPARoot, "PPARoot"},
		{NodeTypePPAPost, "PPAPost"},
		{NodeTypeActionSelector, "ActionSelector"},
		{NodeTypeActionRoot, "ActionRoot"},
		{NodeTypeActionNode, "ActionNode"},
		{NodeTypePreconditionsRoot, "PreconditionsRoot"},
		{NodeTypePreconditionLeaf, "PreconditionLeaf"},
		{NodeType(999), "Unknown"}, // out of bounds
		{NodeType(-1), "Unknown"},  // negative
	}
	for _, tc := range tests {
		if got := tc.input.String(); got != tc.expected {
			t.Errorf("NodeType(%d).String() = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestNodeInfo_Implements_Metadata(t *testing.T) {
	// Compile-time check that NodeInfo implements bt.Metadata
	var _ bt.Metadata = (*NodeInfo)(nil)
}

func TestNodeInfo_Value(t *testing.T) {
	info := &NodeInfo{Type: NodeTypeGoalRoot}
	if info.Value("anything") != nil {
		t.Error("NodeInfo.Value should return nil for all keys")
	}
	if info.Value(nodeInfoKey{}) != nil {
		t.Error("NodeInfo.Value should return nil for nodeInfoKey")
	}
}

func TestNodeInfo_Children_Nil(t *testing.T) {
	info := &NodeInfo{}
	called := false
	info.Children(func(m bt.Metadata) bool {
		called = true
		return true
	})
	if called {
		t.Error("Children should not yield when children is nil")
	}
}

func TestNodeInfo_Children_Early_Exit(t *testing.T) {
	child1 := &NodeInfo{Type: NodeTypeActionNode}
	child2 := &NodeInfo{Type: NodeTypePreconditionLeaf}

	info := &NodeInfo{
		Type: NodeTypeGoalRoot,
		children: func(yield func(bt.Metadata) bool) {
			if !yield(child1) {
				return
			}
			yield(child2)
		},
	}

	var count int
	info.Children(func(m bt.Metadata) bool {
		count++
		return false // stop after first
	})
	if count != 1 {
		t.Errorf("Children early exit: got %d calls, want 1", count)
	}
}

func TestGetNodeInfo_Nil(t *testing.T) {
	if GetNodeInfo(nil) != nil {
		t.Error("GetNodeInfo(nil) should return nil")
	}
}

func TestGetNodeInfo_NonPabtNode(t *testing.T) {
	node := bt.New(func([]bt.Node) (bt.Status, error) { return bt.Success, nil })
	// Expand the node to get a proper bt.Node
	expanded := node
	_, _ = expanded.Tick() // Force expansion
	if GetNodeInfo(node) != nil {
		t.Error("GetNodeInfo on non-pabt node should return nil")
	}
}

func TestGetNodeType_Nil(t *testing.T) {
	if got := GetNodeType(nil); got != NodeTypeUnknown {
		t.Errorf("GetNodeType(nil) = %v, want NodeTypeUnknown", got)
	}
}

func TestGetPreconditionKey_Nil(t *testing.T) {
	key, ok := GetPreconditionKey(nil)
	if ok || key != nil {
		t.Errorf("GetPreconditionKey(nil) = (%v, %v), want (nil, false)", key, ok)
	}
}

func TestGetCondition_Nil(t *testing.T) {
	cond, ok := GetCondition(nil)
	if ok || cond != nil {
		t.Errorf("GetCondition(nil) = (%v, %v), want (nil, false)", cond, ok)
	}
}

func TestGetEffects_Nil(t *testing.T) {
	effects, ok := GetEffects(nil)
	if ok || effects != nil {
		t.Errorf("GetEffects(nil) = (%v, %v), want (nil, false)", effects, ok)
	}
}

func TestUseNodeInfo_Nil(t *testing.T) {
	if UseNodeInfo(nil) != nil {
		t.Error("UseNodeInfo(nil) should return nil")
	}
}

func TestUseNodeInfo_Provider(t *testing.T) {
	info := &NodeInfo{Type: NodeTypeGoalRoot}
	provider := UseNodeInfo(info)
	if provider == nil {
		t.Fatal("UseNodeInfo should return non-nil provider")
	}

	// Test that it provides the info for the correct key
	val, ok := provider.Value(nodeInfoKey{})
	if !ok {
		t.Error("provider.Value(nodeInfoKey{}) should return (info, true)")
	}
	if val != info {
		t.Errorf("provider.Value(nodeInfoKey{}) = %v, want %v", val, info)
	}

	// Test that it returns (nil, false) for other keys
	val, ok = provider.Value("other")
	if ok {
		t.Error("provider.Value(other) should return (nil, false)")
	}
	if val != nil {
		t.Errorf("provider.Value(other) = %v, want nil", val)
	}
}

func TestNodeValueProvider(t *testing.T) {
	// This tests the internal nodeValueProvider type indirectly
	// by using a pabt plan and checking metadata WITHOUT triggering expansion
	state := &mockState{
		variable: func(key any) (value any, err error) {
			// Return a value that matches the condition so we don't trigger expansion
			return "expected", nil
		},
		actions: func(failed Condition) ([]IAction, error) {
			t.Fatal("Actions should not be called in this test")
			return nil, nil
		},
	}
	cond := &mockCondition{
		key:   func() any { return "test_key" },
		match: func(value any) bool { return value == "expected" },
	}

	plan, err := INew(state, []IConditions{{cond}})
	if err != nil {
		t.Fatal(err)
	}

	// Get the internal node
	node := plan.Node()
	// Tick once - should succeed immediately without expansion because condition matches
	status, err := node.Tick()
	if err != nil {
		t.Fatal(err)
	}
	if status != bt.Success {
		t.Errorf("Expected success, got %v", status)
	}

	// Check that we can get node info from the root
	info := GetNodeInfo(node)
	if info == nil {
		t.Fatal("GetNodeInfo should return non-nil for pabt node")
	}

	// The root node should be GoalRoot type
	if info.Type != NodeTypeGoalRoot {
		t.Errorf("root node type = %v, want NodeTypeGoalRoot", info.Type)
	}
}

func TestMetadata_WalkIntegration(t *testing.T) {
	// Test that bt.Walk works with pabt nodes
	state := &graphState{
		nodes: []*graphNode{
			{name: "s0"},
			{name: "sg"},
		},
		actor: nil,
		goal:  nil,
	}
	state.nodes[0].links = []*graphNode{state.nodes[1]}
	state.nodes[1].links = []*graphNode{state.nodes[0]}
	state.actor = state.nodes[0]
	state.goal = []*graphNode{state.nodes[1]}

	plan, err := INew(state, state.Goal())
	if err != nil {
		t.Fatal(err)
	}

	node := plan.Node()
	// Force expansion
	node.Tick()
	node.Tick()

	// Walk the tree and collect node types
	var nodeTypes []NodeType
	bt.Walk(node, func(n bt.Metadata) bool {
		if info := GetNodeInfo(n); info != nil {
			nodeTypes = append(nodeTypes, info.Type)
		}
		return true
	})

	// We should have found at least one node with metadata
	if len(nodeTypes) == 0 {
		t.Error("bt.Walk should have found nodes with metadata")
	}

	// Log the types found for debugging
	t.Logf("Found %d nodes with metadata: %v", len(nodeTypes), nodeTypes)
}

func TestWalk(t *testing.T) {
	state := &mockState{
		variable: func(key any) (any, error) {
			return "expected", nil
		},
		actions: func(failed Condition) ([]IAction, error) {
			return nil, nil
		},
	}
	cond := &mockCondition{
		key:   func() any { return "test_key" },
		match: func(value any) bool { return value == "expected" },
	}

	plan, err := INew(state, []IConditions{{cond}})
	if err != nil {
		t.Fatal(err)
	}

	var count int
	Walk(plan.Node(), func(info *NodeInfo) bool {
		count++
		return true
	})
	if count == 0 {
		t.Error("Walk should find at least one node with metadata")
	}
}

func TestWalk_EarlyExit(t *testing.T) {
	state := &mockState{
		variable: func(key any) (any, error) {
			return "expected", nil
		},
		actions: func(failed Condition) ([]IAction, error) {
			return nil, nil
		},
	}
	cond := &mockCondition{
		key:   func() any { return "test_key" },
		match: func(value any) bool { return value == "expected" },
	}

	plan, err := INew(state, []IConditions{{cond}})
	if err != nil {
		t.Fatal(err)
	}

	var count int
	Walk(plan.Node(), func(info *NodeInfo) bool {
		count++
		return false // stop after first
	})
	if count != 1 {
		t.Errorf("Walk early exit: got %d visits, want 1", count)
	}
}

func TestGetNodeStatus(t *testing.T) {
	state := &mockState{
		variable: func(key any) (any, error) {
			return "expected", nil
		},
		actions: func(failed Condition) ([]IAction, error) {
			return nil, nil
		},
	}
	cond := &mockCondition{
		key:   func() any { return "test_key" },
		match: func(value any) bool { return value == "expected" },
	}

	plan, err := INew(state, []IConditions{{cond}})
	if err != nil {
		t.Fatal(err)
	}

	node := plan.Node()
	status, err := node.Tick()
	if err != nil {
		t.Fatal(err)
	}
	_ = status

	rootStatus, ok := GetNodeStatus(node)
	if !ok {
		t.Fatal("expected root node to have status")
	}
	if rootStatus.TickCount() == 0 {
		t.Error("expected TickCount > 0 after tick")
	}

	// Test with nil valuer
	_, ok = GetNodeStatus(nil)
	if ok {
		t.Error("expected false for nil valuer")
	}
}

func TestGetFrameInfo(t *testing.T) {
	state := &mockState{
		variable: func(key any) (any, error) {
			return "expected", nil
		},
		actions: func(failed Condition) ([]IAction, error) {
			return nil, nil
		},
	}
	cond := &mockCondition{
		key:   func() any { return "test_key" },
		match: func(value any) bool { return value == "expected" },
	}

	plan, err := INew(state, []IConditions{{cond}})
	if err != nil {
		t.Fatal(err)
	}

	node := plan.Node()
	_, _ = node.Tick()

	frameInfo := GetFrameInfo(node)
	// Frame info may or may not be available depending on how bt.New captures it.
	// The important thing is it doesn't panic and returns nil or a valid FrameInfo.
	if frameInfo != nil {
		if frameInfo.File == "" && frameInfo.Line == 0 && frameInfo.Function == "" {
			t.Error("non-nil FrameInfo should have at least one populated field")
		}
	}
}

func TestGetFrameInfo_Nil(t *testing.T) {
	if GetFrameInfo(nil) != nil {
		t.Error("GetFrameInfo(nil) should return nil")
	}
}

func TestNodeCount(t *testing.T) {
	state := &mockState{
		variable: func(key any) (any, error) {
			return "expected", nil
		},
		actions: func(failed Condition) ([]IAction, error) {
			return nil, nil
		},
	}
	cond := &mockCondition{
		key:   func() any { return "test_key" },
		match: func(value any) bool { return value == "expected" },
	}

	plan, err := INew(state, []IConditions{{cond}})
	if err != nil {
		t.Fatal(err)
	}

	count := NodeCount(plan.Node())
	if count <= 0 {
		t.Errorf("NodeCount should be > 0, got %d", count)
	}
}

func TestNodeCount_Nil(t *testing.T) {
	count := NodeCount(nil)
	if count != 0 {
		t.Errorf("NodeCount(nil) = %d, want 0", count)
	}
}

func TestWalk_NodeTypes(t *testing.T) {
	state := newGraphState()
	plan, err := INew(state, state.Goal())
	if err != nil {
		t.Fatal(err)
	}

	node := plan.Node()
	for i := 0; i < 10; i++ {
		status, err := node.Tick()
		if err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
		if status != bt.Running {
			break
		}
	}

	var types []NodeType
	Walk(node, func(info *NodeInfo) bool {
		types = append(types, info.Type)
		return true
	})

	if len(types) == 0 {
		t.Fatal("Walk should find at least one node with metadata")
	}

	hasGoalRoot := false
	for _, nt := range types {
		if nt == NodeTypeGoalRoot {
			hasGoalRoot = true
		}
	}
	if !hasGoalRoot {
		t.Errorf("expected NodeTypeGoalRoot in walked types, got %v", types)
	}

	for _, nt := range types {
		if nt == NodeTypeUnknown {
			t.Errorf("planner-created nodes should not have NodeTypeUnknown, got types %v", types)
		}
	}
}

func TestNodeStatus_MarshalJSON(t *testing.T) {
	t.Run("zero value", func(t *testing.T) {
		var s NodeStatus
		data, err := s.MarshalJSON()
		if err != nil {
			t.Fatalf("MarshalJSON error: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("unmarshal: %v data=%s", err, data)
		}
		if v, ok := m["TickCount"]; !ok || int(v.(float64)) != 0 {
			t.Errorf("TickCount = %v, want 0", m["TickCount"])
		}
		if v, ok := m["LastStatus"]; !ok || int(v.(float64)) != 0 {
			t.Errorf("LastStatus = %v, want 0", m["LastStatus"])
		}
	})

	t.Run("with values", func(t *testing.T) {
		var s NodeStatus
		s.SetTickCount(5)
		s.SetLastStatus(bt.Success)
		data, err := s.MarshalJSON()
		if err != nil {
			t.Fatalf("MarshalJSON error: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("unmarshal: %v data=%s", err, data)
		}
		if got := int(m["TickCount"].(float64)); got != 5 {
			t.Errorf("TickCount = %d, want 5", got)
		}
		if got := int(m["LastStatus"].(float64)); got != int(bt.Success) {
			t.Errorf("LastStatus = %d, want %d", got, int(bt.Success))
		}
	})

	t.Run("round trip via json Marshal", func(t *testing.T) {
		var s NodeStatus
		s.SetTickCount(42)
		s.SetLastStatus(bt.Running)
		data, err := json.Marshal(&s)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if int(m["TickCount"].(float64)) != 42 {
			t.Errorf("TickCount round-trip failed: %v", m)
		}
		if int(m["LastStatus"].(float64)) != int(bt.Running) {
			t.Errorf("LastStatus round-trip failed: %v", m)
		}
	})
}
