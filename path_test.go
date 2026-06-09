package pabt

import (
	"strings"
	"testing"

	bt "github.com/joeycumines/go-behaviortree"
)

func TestGetPath_NilPlan(t *testing.T) {
	if path := GetPath((*IPlan)(nil)); path != nil {
		t.Errorf("GetPath(nil) = %v, want nil", path)
	}
}

func TestGetPath_UntickedSingleGoal(t *testing.T) {
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

	path := GetPath(plan)
	if len(path) == 0 {
		t.Fatal("expected non-empty path")
	}
	if path[0].NodeType != NodeTypeGoalRoot {
		t.Errorf("first entry NodeType = %v, want NodeTypeGoalRoot", path[0].NodeType)
	}
}

func TestGetPath_AfterSuccessfulTick(t *testing.T) {
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
	if status != bt.Success {
		t.Fatalf("expected Success, got %v", status)
	}

	path := GetPath(plan)
	if len(path) == 0 {
		t.Fatal("expected non-empty path after tick")
	}
	if path[0].NodeType != NodeTypeGoalRoot {
		t.Errorf("first entry NodeType = %v, want NodeTypeGoalRoot", path[0].NodeType)
	}
}

func TestGetPath_GraphState(t *testing.T) {
	state := newGraphState()
	plan, err := INew(state, state.Goal())
	if err != nil {
		t.Fatal(err)
	}

	path := GetPath(plan)
	if len(path) == 0 {
		t.Fatal("expected non-empty path")
	}
	if path[0].NodeType != NodeTypeGoalRoot {
		t.Errorf("first entry NodeType = %v, want NodeTypeGoalRoot", path[0].NodeType)
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

	pathAfter := GetPath(plan)
	if len(pathAfter) == 0 {
		t.Fatal("expected non-empty path after ticks")
	}
	if pathAfter[0].NodeType != NodeTypeGoalRoot {
		t.Errorf("first entry NodeType = %v, want NodeTypeGoalRoot", pathAfter[0].NodeType)
	}
}

func TestPath_String_Empty(t *testing.T) {
	var path IPath
	if s := path.String(); s != "" {
		t.Errorf("empty Path.String() = %q, want empty string", s)
	}
}

func TestPath_String_SingleEntry(t *testing.T) {
	path := IPath{
		{NodeType: NodeTypeGoalRoot},
	}
	s := path.String()
	if !strings.Contains(s, "GoalRoot") {
		t.Errorf("Path.String() = %q, want to contain GoalRoot", s)
	}
}

func TestPath_String_WithEffects(t *testing.T) {
	path := IPath{
		{NodeType: NodeTypeGoalRoot},
		{NodeType: NodeTypeActionNode, Effects: Effects{
			&simpleEffect{key: "actor", value: "s5"},
			&simpleEffect{key: "loc", value: "table"},
		}},
	}
	s := path.String()
	if !strings.Contains(s, "GoalRoot") {
		t.Errorf("Path.String() = %q, want to contain GoalRoot", s)
	}
	if !strings.Contains(s, "ActionNode") {
		t.Errorf("Path.String() = %q, want to contain ActionNode", s)
	}
	if !strings.Contains(s, "actor=s5") {
		t.Errorf("Path.String() = %q, want to contain actor=s5", s)
	}
	if !strings.Contains(s, "loc=table") {
		t.Errorf("Path.String() = %q, want to contain loc=table", s)
	}
	if !strings.Contains(s, " → ") {
		t.Errorf("Path.String() = %q, want to contain arrow separator", s)
	}
}

func TestPath_String_WithCondition(t *testing.T) {
	path := IPath{
		{NodeType: NodeTypeGoalRoot},
		{NodeType: NodeTypePreconditionLeaf, Condition: &simpleCondition{key: "actor", value: "s5"}},
	}
	s := path.String()
	if !strings.Contains(s, "PreconditionLeaf") {
		t.Errorf("Path.String() = %q, want to contain PreconditionLeaf", s)
	}
	if !strings.Contains(s, "cond:") {
		t.Errorf("Path.String() = %q, want to contain cond: prefix", s)
	}
}

func TestPath_String_WithPostCondition(t *testing.T) {
	path := IPath{
		{NodeType: NodeTypeGoalRoot},
		{NodeType: NodeTypeActionNode, PostCondition: &simpleCondition{key: "actor", value: "s5"}},
	}
	s := path.String()
	if !strings.Contains(s, "post:") {
		t.Errorf("Path.String() = %q, want to contain post: prefix", s)
	}
}

func TestPrinter_AssignableToBtDefaultPrinter(t *testing.T) {
	old := bt.DefaultPrinter
	bt.DefaultPrinter = Printer
	defer func() { bt.DefaultPrinter = old }()

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
	output := node.String()
	if output == "" {
		t.Error("expected non-empty output from node.String() with Printer assigned")
	}
	if !strings.Contains(output, "GoalRoot") {
		t.Errorf("output with Printer should contain GoalRoot, got: %s", output)
	}
}

func TestPrinter_NilNode(t *testing.T) {
	old := bt.DefaultPrinter
	bt.DefaultPrinter = Printer
	defer func() { bt.DefaultPrinter = old }()

	var node bt.Node
	output := node.String()
	if output != "<nil>" {
		t.Errorf("Printer with nil node should return <nil>, got: %q", output)
	}
}

func TestGetIPath(t *testing.T) {
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

	path := GetIPath(plan)
	if len(path) == 0 {
		t.Fatal("expected non-empty path")
	}
	if path[0].NodeType != NodeTypeGoalRoot {
		t.Errorf("first entry NodeType = %v, want NodeTypeGoalRoot", path[0].NodeType)
	}
}
