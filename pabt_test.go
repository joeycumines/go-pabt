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
	"errors"
	"fmt"
	bt "github.com/joeycumines/go-behaviortree"
	"regexp"
	"strings"
	"testing"
)

type mockState struct {
	variable func(key any) (value any, err error)
	actions  func(failed Condition) ([]IAction, error)
}

func (m *mockState) Variable(key any) (value any, err error) { return m.variable(key) }
func (m *mockState) Actions(failed Condition) ([]IAction, error) {
	return m.actions(failed)
}

func TestNew_nilState(t *testing.T) {
	p, err := INew(nil, nil)
	if err == nil || p != nil || err.Error() != `pabt: nil state` {
		t.Error(p, err)
	}
}

func replacePointers(b string) string {
	var (
		m = make(map[string]struct{})
		r []string
		n int
	)
	for _, v := range regexp.MustCompile(`(?:[[:^alnum:]]|^)(0x[[:alnum:]]{1,16})(?:[[:^alnum:]]|$)`).FindAllStringSubmatch(b, -1) {
		if v := v[1]; v != `0x0` {
			if _, ok := m[v]; !ok {
				n++
				m[v] = struct{}{}
				r = append(r, v, fmt.Sprintf(`%#x`, n))
			}
		}
	}
	return strings.NewReplacer(r...).Replace(b)
}

type mockCondition struct {
	key   func() any
	match func(value any) bool
}

func (m *mockCondition) Key() any             { return m.key() }
func (m *mockCondition) Match(value any) bool { return m.match(value) }

func TestNew_initialStructure(t *testing.T) {
	var (
		variables map[any]any
		state     = &mockState{variable: func(key any) (value any, err error) {
			var ok bool
			value, ok = variables[key]
			if !ok {
				err = fmt.Errorf(`variable not found: (%T, %v)`, key, key)
			}
			return
		}}
		cond1 = &mockCondition{
			key:   func() any { return 1 },
			match: func(value any) bool { return value == `1` },
		}
		cond2 = &mockCondition{
			key:   func() any { return 2 },
			match: func(value any) bool { return value == `2` },
		}
		cond3 = &mockCondition{
			key: func() any { return 3 },
			match: func(value any) bool {
				if value == nil {
					t.Error(value)
				}
				return value == `3`
			},
		}
	)
	for _, test := range []struct {
		Name   string
		Goal   []IConditions
		Err    error
		String string
		Plan   func(t *testing.T, plan *IPlan)
	}{
		{
			Name:   `nil goal`,
			Goal:   nil,
			String: "[0x1 util.go:140 0x2 sequence.go:21]  github.com/joeycumines/go-pabt.TestNew_initialStructure.func14.(*node[...]).bt.1 | github.com/joeycumines/go-behaviortree.Sequence",
		},
		{
			Name:   `case 0`,
			Goal:   []IConditions{},
			String: "[0x1 util.go:140 0x2 sequence.go:21]  github.com/joeycumines/go-pabt.TestNew_initialStructure.func14.(*node[...]).bt.1 | github.com/joeycumines/go-behaviortree.Sequence",
		},
		{
			Name:   `precondition single condition`,
			Goal:   []IConditions{{cond3}},
			String: "[0x1 util.go:140 0x2 sequence.go:21]  github.com/joeycumines/go-pabt.TestNew_initialStructure.func14.(*node[...]).bt.1 | github.com/joeycumines/go-behaviortree.Sequence\n└── [0x3 util.go:158 0x4 util.go:158]  github.com/joeycumines/go-pabt.newConditionNode[...] | github.com/joeycumines/go-pabt.newConditionNode[...].func1",
			Plan: func(t *testing.T, p *IPlan) {
				if p.root == nil ||
					p.root.parent != nil ||
					p.root.next != nil ||
					p.root.goal == nil ||
					p.root.goal.root != p.root ||
					p.root.goal.state != state ||
					len(p.root.goal.or) != 1 ||
					p.root.preconditions == nil ||
					p.root.goal.or[0] != p.root.preconditions ||
					p.root.preconditions.root != p.root ||
					p.root.first == nil {
					t.Fatal(p.root)
				}
				if p.root.first.goal != p.root.goal ||
					p.root.first.preconditions != p.root.preconditions ||
					p.root.first.precondition == nil ||
					p.root.first.precondition.condition != cond3 {
					t.Fatal(p.root.first)
				}
				if len(p.root.preconditions.and) != 1 ||
					p.root.preconditions.and[3] != p.root.first.precondition {
					t.Fatal(p.root.preconditions.and)
				}
				if p.root.first.first != nil ||
					p.root.first.next != nil ||
					p.root.first.parent != p.root {
					t.Fatal(p.root.first)
				}
				if p.root.first.precondition.status != 0 {
					t.Error(p.root.first.precondition.status)
				}
				if status, err := p.root.bt().Tick(); err == nil || err.Error() != `variable not found: (int, 3)` || status != bt.Failure {
					t.Errorf(`%v: %v`, status, err)
				}
				if p.root.first.precondition.status != bt.Failure {
					t.Errorf(`%s %p`, p.root.first.precondition.status, &p.root.first.precondition.status)
				}
				p.root.first.precondition.status = 0
				variables = map[any]any{
					3: false,
				}
				if status, err := p.root.bt().Tick(); err != nil || status != bt.Failure {
					t.Errorf(`%v: %v`, status, err)
				}
				if p.root.first.precondition.status != bt.Failure {
					t.Errorf(`%s %p`, p.root.first.precondition.status, &p.root.first.precondition.status)
				}
				p.root.first.precondition.status = 0
				variables = map[any]any{
					3: `3`,
				}
				if status, err := p.root.bt().Tick(); err != nil || status != bt.Success {
					t.Errorf(`%v: %v`, status, err)
				}
				if p.root.first.precondition.status != bt.Success {
					t.Errorf(`%s %p`, p.root.first.precondition.status, &p.root.first.precondition.status)
				}
				variables = nil
			},
		},
		{
			Name:   `precondition multiple conditions`,
			Goal:   []IConditions{{cond1, cond2, cond3}},
			String: "[0x1 util.go:140 0x2 sequence.go:21]  github.com/joeycumines/go-pabt.TestNew_initialStructure.func14.(*node[...]).bt.1 | github.com/joeycumines/go-behaviortree.Sequence\n├── [0x3 util.go:158 0x4 util.go:158]  github.com/joeycumines/go-pabt.newConditionNode[...] | github.com/joeycumines/go-pabt.newConditionNode[...].func1\n├── [0x3 util.go:158 0x4 util.go:158]  github.com/joeycumines/go-pabt.newConditionNode[...] | github.com/joeycumines/go-pabt.newConditionNode[...].func1\n└── [0x3 util.go:158 0x4 util.go:158]  github.com/joeycumines/go-pabt.newConditionNode[...] | github.com/joeycumines/go-pabt.newConditionNode[...].func1",
			Plan: func(t *testing.T, p *IPlan) {
				if p.root == nil ||
					p.root.goal == nil ||
					p.root.goal.root != p.root ||
					p.root.preconditions == nil ||
					p.root.goal.or[0] != p.root.preconditions ||
					p.root.preconditions.root != p.root {
					t.Error(p.root)
				}
			},
		},
		{
			Name: `condition key not comparable`,
			Goal: []IConditions{{&mockCondition{key: func() any { return func() {} }}}},
			Err:  errors.New(`pabt: invalid conditions`),
		},
		{
			Name: `condition key duplicated`,
			Goal: []IConditions{{
				&mockCondition{key: func() any { return true }},
				&mockCondition{key: func() any { return true }},
			}},
			Err: errors.New(`pabt: invalid conditions`),
		},
		{
			Name: `preconditions`,
			Goal: []IConditions{
				{cond2},
				{cond1, cond3},
				{cond1, cond3},
			},
			String: "[0x1 util.go:140 0x2 selector.go:21]  github.com/joeycumines/go-pabt.TestNew_initialStructure.func14.(*node[...]).bt.1 | github.com/joeycumines/go-behaviortree.Selector\n├── [0x3 util.go:140 0x4 sequence.go:21]  github.com/joeycumines/go-pabt.(*node[...]).bt.func1 | github.com/joeycumines/go-behaviortree.Sequence\n│   └── [0x5 util.go:158 0x6 util.go:158]  github.com/joeycumines/go-pabt.newConditionNode[...] | github.com/joeycumines/go-pabt.newConditionNode[...].func1\n├── [0x3 util.go:140 0x4 sequence.go:21]  github.com/joeycumines/go-pabt.(*node[...]).bt.func1 | github.com/joeycumines/go-behaviortree.Sequence\n│   ├── [0x5 util.go:158 0x6 util.go:158]  github.com/joeycumines/go-pabt.newConditionNode[...] | github.com/joeycumines/go-pabt.newConditionNode[...].func1\n│   └── [0x5 util.go:158 0x6 util.go:158]  github.com/joeycumines/go-pabt.newConditionNode[...] | github.com/joeycumines/go-pabt.newConditionNode[...].func1\n└── [0x3 util.go:140 0x4 sequence.go:21]  github.com/joeycumines/go-pabt.(*node[...]).bt.func1 | github.com/joeycumines/go-behaviortree.Sequence\n    ├── [0x5 util.go:158 0x6 util.go:158]  github.com/joeycumines/go-pabt.newConditionNode[...] | github.com/joeycumines/go-pabt.newConditionNode[...].func1\n    └── [0x5 util.go:158 0x6 util.go:158]  github.com/joeycumines/go-pabt.newConditionNode[...] | github.com/joeycumines/go-pabt.newConditionNode[...].func1",
			Plan: func(t *testing.T, p *IPlan) {
				if p.root == nil ||
					p.root.parent != nil ||
					p.root.next != nil ||
					p.root.goal == nil ||
					p.root.goal.root != p.root ||
					p.root.goal.state != state ||
					len(p.root.goal.or) != 3 ||
					p.root.preconditions != nil { // different from the single Conditions case
					t.Fatal(p.root)
				}
				if p.root.first == nil ||
					p.root.first.goal != p.root.goal ||
					p.root.first.parent != p.root ||
					p.root.first.preconditions == nil ||
					p.root.goal.or[0] != p.root.first.preconditions ||
					p.root.first.preconditions.root != p.root.first ||
					len(p.root.first.preconditions.and) != 1 ||
					p.root.first.precondition != nil {
					t.Fatal(p.root.first)
				}
				if p.root.first.first == nil ||
					p.root.first.first.goal != p.root.goal ||
					p.root.first.first.preconditions != p.root.first.preconditions ||
					p.root.first.first.parent != p.root.first ||
					p.root.first.first.precondition == nil ||
					p.root.first.first.precondition.condition != cond2 ||
					p.root.first.first.precondition.root != p.root.first.first ||
					p.root.first.first.precondition != p.root.first.preconditions.and[2] {
					t.Fatal(p.root.first.first, p.root.first.preconditions)
				}
			},
		},
	} {
		t.Run(test.Name, func(t *testing.T) {
			p, err := INew(state, test.Goal)
			if err != nil {
				if p != nil {
					t.Error(p)
				}
				if test.Err == nil || test.Err.Error() != err.Error() {
					t.Error(err)
				}
				return
			}
			n := p.root.bt()
			if n == nil {
				t.Fatal(p)
			}
			if s := replacePointers(n.String()); s != test.String {
				t.Fatalf("unexpected initial tree: %q\n%s", s, s)
			}
			if test.Plan != nil {
				test.Plan(t, p)
			}
		})
	}
}

func Test_node_generateAnd_empty(t *testing.T) {
	if v, err := (*node[Condition])(nil).generateAnd(nil); err == nil || err.Error() != `pabt: invalid conditions` || v != nil {
		t.Error(v, err)
	}
}

type (
	simpleCondition struct {
		key   string
		value any
	}
	simpleEffect struct {
		key   string
		value any
	}
	simpleAction struct {
		conditions []IConditions
		effects    Effects
		node       bt.Node
	}
	treeMetaKey struct{}
)

func (e *simpleEffect) Key() any                  { return e.key }
func (e *simpleEffect) Value() any                { return e.value }
func (c *simpleCondition) Key() any               { return c.key }
func (c *simpleCondition) Match(value any) bool   { return value == c.value }
func (a *simpleAction) Conditions() []IConditions { return a.conditions }
func (a *simpleAction) Effects() Effects          { return a.effects }
func (a *simpleAction) Node() bt.Node             { return a.node }

func patchTreeMeta() func() {
	old := bt.DefaultPrinter
	bt.DefaultPrinter = bt.TreePrinter{
		Inspector: func(node bt.Node, tick bt.Tick) (meta []any, value any) {
			meta, value = bt.DefaultPrinterInspector(node, tick)
			extra, _ := node.Value(treeMetaKey{}).([]any)
			meta = append([]any{meta[1], meta[3]}, extra...)
			return
		},
		Formatter: bt.DefaultPrinterFormatter,
	}
	return func() {
		bt.DefaultPrinter = old
	}
}
func attachTreeMeta(node bt.Node, meta ...any) bt.Node {
	return node.WithValue(treeMetaKey{}, meta)
}
