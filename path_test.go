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

// TestGetPathWithAConcreteConditionTypeRenders is the shape GetPath is called
// with in real code, and it is not the shape the rest of this file tests.
//
// Every other case here goes through IPath, so T is the Condition interface and the
// zero value is a nil interface, which a comparison against nil happens to catch.
// A plan is normally built over a concrete condition pointer, where the zero value
// is a typed nil: stored in an interface it is not nil, so the root entry - which
// never had a condition at all - read as having one, and String called Key on a nil
// receiver. The documented call, pabt.GetPath(plan).String(), panicked.
func TestGetPathWithAConcreteConditionTypeRenders(t *testing.T) {
	cond := &simpleCondition{key: "actor", value: "s5"}
	variables := map[string]any{"actor": "s4"}
	state := &concretePathState{variables: variables, apply: func() { variables["actor"] = "s5" }}
	plan, err := New(state, []Conditions[*simpleCondition]{{cond}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for range 4 {
		if status, tickErr := plan.Node().Tick(); tickErr == nil && status.String() == "Success" {
			break
		}
	}
	// The whole point: this must not panic, over a concrete T.
	s := GetPath(plan).String()
	if !strings.Contains(s, "GoalRoot") {
		t.Errorf("Path.String() = %q, want it to contain GoalRoot", s)
	}
	// And a root entry with no condition must not claim one.
	if strings.Contains(s, "cond:<nil>") {
		t.Errorf("Path.String() = %q, rendered a nil condition as a declaration", s)
	}
}

// concretePathState is a state for a plan over a concrete condition type.
type concretePathState struct {
	variables map[string]any
	apply     func()
}

func (s *concretePathState) Variable(key any) (any, error) {
	return s.variables[key.(string)], nil
}

func (s *concretePathState) Actions(failed *simpleCondition) ([]Action[*simpleCondition], error) {
	return []Action[*simpleCondition]{&concretePathAction{state: s, failed: failed}}, nil
}

type concretePathEffect struct {
	key   string
	value any
}

func (e concretePathEffect) Key() any   { return e.key }
func (e concretePathEffect) Value() any { return e.value }

type concretePathAction struct {
	state  *concretePathState
	failed *simpleCondition
}

func (a *concretePathAction) Conditions() []Conditions[*simpleCondition] { return nil }

func (a *concretePathAction) Effects() Effects {
	return Effects{
		concretePathEffect{key: "actor", value: "s5"},
		concretePathEffect{key: "loc", value: "table"},
	}
}

func (a *concretePathAction) Node() bt.Node {
	return bt.New(func([]bt.Node) (bt.Status, error) {
		if a.state.apply != nil {
			a.state.apply()
		}
		return bt.Success, nil
	})
}

// sliceFieldCondition is a condition that cannot be compared.
//
// A struct holding a slice is just as uncomparable as a slice is, and far more
// ordinary as a type for an application to write. isUnset used to compare two
// values with ==, so a plan over this type panicked in String() rather than
// rendering. The comparison now goes through sameValue, which reports "not the
// same" for a pair it cannot compare.
type sliceFieldCondition struct {
	key    string
	values []string
}

func (c sliceFieldCondition) Key() any       { return c.key }
func (c sliceFieldCondition) Match(any) bool { return false }

// uncomparablePathState is a state whose condition type cannot be compared.
//
// The condition is held and passed BY VALUE, which is the whole point: a pointer is
// always comparable, so a test over *sliceFieldCondition would exercise the case
// that works and prove nothing about the case that does not.
type uncomparablePathState struct{ variables map[string]any }

func (s *uncomparablePathState) Variable(key any) (any, error) {
	return s.variables[key.(string)], nil
}

func (s *uncomparablePathState) Actions(sliceFieldCondition) (
	[]Action[sliceFieldCondition], error,
) {
	return nil, nil
}

func TestPathStringOverAnUncomparableConditionType(t *testing.T) {
	variables := map[string]any{"actor": "s4"}
	state := &uncomparablePathState{variables: variables}
	plan, err := New[sliceFieldCondition](state, []Conditions[sliceFieldCondition]{
		{sliceFieldCondition{key: "actor", values: []string{"s5"}}},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for range 4 {
		if status, tickErr := plan.Node().Tick(); tickErr == nil && status.String() == "Success" {
			break
		}
	}
	// A comparison here is a run-time panic on an uncomparable type, so before
	// sameValue this call took the process down rather than returning a string.
	s := GetPath(plan).String()
	if s == "" {
		t.Error("Path.String() is empty")
	}
	if !strings.Contains(s, "GoalRoot") {
		t.Errorf("Path.String() = %q, want it to contain GoalRoot", s)
	}
}
