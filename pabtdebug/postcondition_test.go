package pabtdebug

import "testing"

func TestPropagatePostConditions(t *testing.T) {
	root := &TreeNode{
		ID: "0", Name: "PPARoot", NodeType: "PPARoot", StructureHash: "PPARoot:2",
		Children: []TreeNode{
			{ID: "0.0", Name: "PPAPost", NodeType: "PPAPost", Condition: "held(CRANE-0)", StructureHash: "PPAPost:0"},
			{ID: "0.1", Name: "ActionSelector", NodeType: "ActionSelector", StructureHash: "ActionSelector:1"},
		},
	}
	propagatePostConditions(root)
	if root.PostCondition != "held(CRANE-0)" {
		t.Fatalf("PostCondition = %q want %q", root.PostCondition, "held(CRANE-0)")
	}

	root.PostCondition = "existing"
	propagatePostConditions(root)
	if root.PostCondition != "existing" {
		t.Fatalf("propagation overwrote existing PostCondition: %q", root.PostCondition)
	}

	noPost := &TreeNode{ID: "0", Name: "GoalSelector", NodeType: "GoalSelector", StructureHash: "GoalSelector:1",
		Children: []TreeNode{{ID: "0.0", Name: "PPARoot", NodeType: "PPARoot", StructureHash: "PPARoot:0"}}}
	propagatePostConditions(noPost)
	if noPost.PostCondition != "" {
		t.Fatalf("PostCondition = %q want empty without a PPAPost child", noPost.PostCondition)
	}

	fallback := &TreeNode{ID: "0", Name: "PPARoot", NodeType: "PPARoot", StructureHash: "PPARoot:1",
		Children: []TreeNode{{ID: "0.0", Name: "at(CRANE-0, berth-1)", NodeType: "PPAPost", StructureHash: "PPAPost:0"}}}
	propagatePostConditions(fallback)
	if fallback.PostCondition != "at(CRANE-0, berth-1)" {
		t.Fatalf("PostCondition fallback = %q want child name", fallback.PostCondition)
	}
}
