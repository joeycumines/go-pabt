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
