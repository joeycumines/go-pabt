package pabt

import (
	"fmt"
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

func TestFormatEffects(t *testing.T) {
	if got := formatEffects(nil); got != "" {
		t.Errorf("formatEffects(nil) = %q, want empty", got)
	}
	if got := formatEffects(Effects{}); got != "" {
		t.Errorf("formatEffects(empty) = %q, want empty", got)
	}
	if got := formatEffects(Effects{nil}); got != "<nil>" {
		t.Errorf("formatEffects(nil entry) = %q, want <nil>", got)
	}
	effects := Effects{
		&simpleEffect{key: "actor", value: "s5"},
		&simpleEffect{key: "loc", value: "table"},
	}
	got := formatEffects(effects)
	if !strings.Contains(got, "actor=s5") {
		t.Errorf("formatEffects = %q, want actor=s5", got)
	}
	if !strings.Contains(got, "loc=table") {
		t.Errorf("formatEffects = %q, want loc=table", got)
	}
	if !strings.Contains(got, ",") {
		t.Errorf("formatEffects multiple should contain comma, got %q", got)
	}
}

func TestFormatVariableKey(t *testing.T) {
	if got := formatVariableKey(nil); got != "<nil>" {
		t.Errorf("formatVariableKey(nil) = %q, want <nil>", got)
	}
	if got := formatVariableKey("actor"); got != "actor" {
		t.Errorf("formatVariableKey(actor) = %q, want actor", got)
	}
}

func TestFormatConditionMatch_Nil(t *testing.T) {
	if got := formatConditionMatch(nil); got != "<nil>" {
		t.Errorf("formatConditionMatch(nil) = %q, want <nil>", got)
	}
	c := &simpleCondition{key: "x", value: "done"}
	got := formatConditionMatch(c)
	if got == "" || got == "<nil>" {
		t.Errorf("formatConditionMatch(c) = %q, want non-nil non-empty", got)
	}
}

func TestPrinter_WithEffects(t *testing.T) {
	old := bt.DefaultPrinter
	bt.DefaultPrinter = Printer
	defer func() { bt.DefaultPrinter = old }()

	variables := map[any]any{"x": "start"}
	state := &mockState{
		variable: func(key any) (any, error) {
			v, ok := variables[key]
			if !ok {
				return nil, fmt.Errorf("variable not found: %v", key)
			}
			return v, nil
		},
		actions: func(failed Condition) ([]IAction, error) {
			return []IAction{
				&simpleAction{
					effects: Effects{
						&simpleEffect{key: "x", value: "done"},
						&simpleEffect{key: "y", value: 42},
					},
					node: bt.New(func([]bt.Node) (bt.Status, error) {
						variables["x"] = "done"
						return bt.Success, nil
					}),
				},
			}, nil
		},
	}
	cond := &simpleCondition{key: "x", value: "done"}
	plan, err := INew(state, []IConditions{{cond}})
	if err != nil {
		t.Fatalf("INew: %v", err)
	}
	node := plan.Node()
	if _, err := node.Tick(); err != nil {
		t.Fatalf("tick 1 (expand): %v", err)
	}
	// Second tick actually runs the action so tree is fully materialized.
	// Either tick should have ActionNode with effects visible via Printer.
	output := node.String()
	if !strings.Contains(output, "effects:") {
		t.Errorf("Printer output should contain effects: prefix, got:\n%s", output)
	}
	if !strings.Contains(output, "x=done") {
		t.Errorf("Printer output should contain x=done, got:\n%s", output)
	}
}
