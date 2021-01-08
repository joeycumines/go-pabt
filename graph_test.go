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
	"fmt"
	bt "github.com/joeycumines/go-behaviortree"
	"strings"
)

type (
	graphState struct {
		nodes []*graphNode
		actor *graphNode
		goal  []*graphNode
	}

	graphNode struct {
		name  string
		links []*graphNode
	}
)

// newGraphState initialises the graph from Example 7.3 (Fig. 7.6)
func newGraphState() (state *graphState) {
	state = new(graphState)
	var (
		s0 = &graphNode{name: `s0`}
		s1 = &graphNode{name: `s1`}
		s2 = &graphNode{name: `s2`}
		s3 = &graphNode{name: `s3`}
		s4 = &graphNode{name: `s4`}
		s5 = &graphNode{name: `s5`}
		sg = &graphNode{name: `sg`}
	)
	s0.links = []*graphNode{s1}
	s1.links = []*graphNode{s4, s3, s2, s0}
	s2.links = []*graphNode{s5, s1}
	s3.links = []*graphNode{sg, s4, s1}
	s4.links = []*graphNode{s5, s3, s1}
	s5.links = []*graphNode{sg, s4, s2}
	sg.links = []*graphNode{s5, s3}
	state.nodes = []*graphNode{s0, s1, s2, s3, s4, s5, sg}
	state.actor = s0
	state.goal = []*graphNode{sg}
	return
}

func (g *graphState) String() string {
	var (
		s strings.Builder
		p = func(nodes []*graphNode) {
			for i, node := range nodes {
				if i != 0 {
					s.WriteString(`,`)
				}
				s.WriteString(` `)
				s.WriteString(node.name)
			}
		}
	)
	for _, node := range g.nodes {
		s.WriteString(node.name)
		s.WriteString(` ->`)
		p(node.links)
		s.WriteString("\n")
	}
	s.WriteString(`goal =`)
	p(g.goal)
	s.WriteString("\n")
	s.WriteString(`actor = `)
	s.WriteString(g.actor.name)
	return s.String()
}

func (g *graphState) Goal() (conditions []Conditions) {
	for _, node := range g.goal {
		conditions = append(conditions, Conditions{&simpleCondition{key: "actor", value: node}})
	}
	return
}

func (g *graphState) Variable(key interface{}) (value interface{}, err error) {
	switch key {
	case `actor`:
		return g.actor, nil
	default:
		return nil, fmt.Errorf(`invalid key (%T): %+v`, key, key)
	}
}

func (g *graphState) Actions(failed Condition) ([]Action, error) {
	switch failed := failed.(type) {
	case *simpleCondition:
		switch failed.key {
		case `actor`:
			switch failed := failed.value.(type) {
			case *graphNode:
				var actions []Action
				for _, node := range failed.links {
					actions = append(actions, &simpleAction{
						conditions: []Conditions{{&simpleCondition{key: "actor", value: node}}},
						effects:    Effects{&simpleEffect{key: "actor", value: failed}},
						node: attachTreeMeta(bt.New(func([]bt.Node) (bt.Status, error) {
							// could be a whole subtree or whatever
							if g.actor != node {
								return bt.Failure, nil
							}
							var ok bool
							for _, node := range node.links {
								if node == failed {
									ok = true
									break
								}
							}
							if !ok {
								return bt.Failure, nil
							}
							fmt.Printf("\nactor %s -> %s\n", g.actor.name, failed.name)
							g.actor = failed
							return bt.Success, nil
						}), `pre:`+node.name, `post:`+failed.name),
					})
				}
				return actions, nil
			}
		}
	}
	return nil, fmt.Errorf(`invalid condition (%T): %+v`, failed, failed)
}

func Example_graph() {
	defer patchTreeMeta()()

	state := newGraphState()
	plan, err := New(state, state.Goal())
	if err != nil {
		panic(err)
	}
	node := plan.Node()

	fmt.Println(state)
	fmt.Println()
	fmt.Println(node)

	var (
		status     bt.Status
		iterations int
	)
	for status, err = node.Tick(); ; status, err = node.Tick() {
		iterations++
		fmt.Printf("\niteration = %d, status = %s, err = %v, actor = %s\n%s\n", iterations, status, err, state.actor.name, node)
		if status != bt.Running || err != nil {
			break
		}
	}
	if status == bt.Success && err == nil {
		old := node.String()
		status, err = node.Tick()
		if err != nil || status != bt.Success || node.String() != old {
			panic(node)
		}
	}

	fmt.Printf("\niteration = %d, status = %s, err = %v, actor = %s\nDONE\n", iterations, status, err, state.actor.name)

	// output:
	// s0 -> s1
	// s1 -> s4, s3, s2, s0
	// s2 -> s5, s1
	// s3 -> sg, s4, s1
	// s4 -> s5, s3, s1
	// s5 -> sg, s4, s2
	// sg -> s5, s3
	// goal = sg
	// actor = s0
	//
	// [pabt.go:183 pabt.go:193]  github.com/joeycumines/go-pabt.(*Plan).bt-fm | github.com/joeycumines/go-pabt.(*Plan).bt.func2
	// └── [util.go:158 util.go:158]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//
	// iteration = 1, status = running, err = <nil>, actor = s0
	// [pabt.go:183 pabt.go:193]  github.com/joeycumines/go-pabt.(*Plan).bt-fm | github.com/joeycumines/go-pabt.(*Plan).bt.func2
	// └── [util.go:144 selector.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//     ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//     └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             └── [graph_test.go:118 graph_test.go:118 pre:s3 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//
	// iteration = 2, status = running, err = <nil>, actor = s0
	// [pabt.go:183 pabt.go:193]  github.com/joeycumines/go-pabt.(*Plan).bt-fm | github.com/joeycumines/go-pabt.(*Plan).bt.func2
	// └── [util.go:144 selector.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//     ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//     └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//         │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:sg post:s5]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s4 post:s5]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │           └── [graph_test.go:118 graph_test.go:118 pre:s2 post:s5]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             └── [graph_test.go:118 graph_test.go:118 pre:s3 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//
	// iteration = 3, status = running, err = <nil>, actor = s0
	// [pabt.go:183 pabt.go:193]  github.com/joeycumines/go-pabt.(*Plan).bt-fm | github.com/joeycumines/go-pabt.(*Plan).bt.func2
	// └── [util.go:144 selector.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//     ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//     └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//         │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:sg post:s5]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s4 post:s5]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │           └── [graph_test.go:118 graph_test.go:118 pre:s2 post:s5]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//             │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//             │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   └── [graph_test.go:118 graph_test.go:118 pre:sg post:s3]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   └── [graph_test.go:118 graph_test.go:118 pre:s4 post:s3]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │           └── [graph_test.go:118 graph_test.go:118 pre:s1 post:s3]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             └── [graph_test.go:118 graph_test.go:118 pre:s3 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//
	// iteration = 4, status = running, err = <nil>, actor = s0
	// [pabt.go:183 pabt.go:193]  github.com/joeycumines/go-pabt.(*Plan).bt-fm | github.com/joeycumines/go-pabt.(*Plan).bt.func2
	// └── [util.go:144 selector.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//     ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//     └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//         │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//         │   │       │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         │   │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │           └── [graph_test.go:118 graph_test.go:118 pre:s3 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:sg post:s5]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s4 post:s5]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │           └── [graph_test.go:118 graph_test.go:118 pre:s2 post:s5]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//             │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//             │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   └── [graph_test.go:118 graph_test.go:118 pre:sg post:s3]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   └── [graph_test.go:118 graph_test.go:118 pre:s4 post:s3]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │           └── [graph_test.go:118 graph_test.go:118 pre:s1 post:s3]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             └── [graph_test.go:118 graph_test.go:118 pre:s3 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//
	// iteration = 5, status = running, err = <nil>, actor = s0
	// [pabt.go:183 pabt.go:193]  github.com/joeycumines/go-pabt.(*Plan).bt-fm | github.com/joeycumines/go-pabt.(*Plan).bt.func2
	// └── [util.go:144 selector.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//     ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//     └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//         │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//         │   │       │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         │   │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │           └── [graph_test.go:118 graph_test.go:118 pre:s3 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:sg post:s5]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//         │   │       │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         │   │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:s4]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s3 post:s4]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │           └── [graph_test.go:118 graph_test.go:118 pre:s1 post:s4]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s4 post:s5]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │           └── [graph_test.go:118 graph_test.go:118 pre:s2 post:s5]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//             │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//             │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   └── [graph_test.go:118 graph_test.go:118 pre:sg post:s3]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   └── [graph_test.go:118 graph_test.go:118 pre:s4 post:s3]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │           └── [graph_test.go:118 graph_test.go:118 pre:s1 post:s3]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             └── [graph_test.go:118 graph_test.go:118 pre:s3 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//
	// iteration = 6, status = running, err = <nil>, actor = s0
	// [pabt.go:183 pabt.go:193]  github.com/joeycumines/go-pabt.(*Plan).bt-fm | github.com/joeycumines/go-pabt.(*Plan).bt.func2
	// └── [util.go:144 selector.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//     ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//     └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//         │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//         │   │       │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         │   │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │           └── [graph_test.go:118 graph_test.go:118 pre:s3 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:sg post:s5]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//         │   │       │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         │   │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:s4]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s3 post:s4]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │           └── [graph_test.go:118 graph_test.go:118 pre:s1 post:s4]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s4 post:s5]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │           ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//         │   │           │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │           │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         │   │           │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │           │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │           │       │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:s2]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │           │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │           │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │           │           └── [graph_test.go:118 graph_test.go:118 pre:s1 post:s2]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │           └── [graph_test.go:118 graph_test.go:118 pre:s2 post:s5]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//             │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//             │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   └── [graph_test.go:118 graph_test.go:118 pre:sg post:s3]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   └── [graph_test.go:118 graph_test.go:118 pre:s4 post:s3]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │           └── [graph_test.go:118 graph_test.go:118 pre:s1 post:s3]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             └── [graph_test.go:118 graph_test.go:118 pre:s3 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//
	// iteration = 7, status = running, err = <nil>, actor = s0
	// [pabt.go:183 pabt.go:193]  github.com/joeycumines/go-pabt.(*Plan).bt-fm | github.com/joeycumines/go-pabt.(*Plan).bt.func2
	// └── [util.go:144 selector.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//     ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//     └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//         │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//         │   │       │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         │   │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │           └── [graph_test.go:118 graph_test.go:118 pre:s3 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:sg post:s5]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//         │   │       │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         │   │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:s4]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s3 post:s4]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │           └── [graph_test.go:118 graph_test.go:118 pre:s1 post:s4]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s4 post:s5]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │           ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//         │   │           │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │           │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         │   │           │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │           │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │           │       │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:s2]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │           │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │           │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │           │           └── [graph_test.go:118 graph_test.go:118 pre:s1 post:s2]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │           └── [graph_test.go:118 graph_test.go:118 pre:s2 post:s5]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//             │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//             │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//             │       │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//             │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   │           └── [graph_test.go:118 graph_test.go:118 pre:s3 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       │   └── [graph_test.go:118 graph_test.go:118 pre:sg post:s3]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   └── [graph_test.go:118 graph_test.go:118 pre:s4 post:s3]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │           └── [graph_test.go:118 graph_test.go:118 pre:s1 post:s3]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             └── [graph_test.go:118 graph_test.go:118 pre:s3 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//
	// iteration = 8, status = running, err = <nil>, actor = s0
	// [pabt.go:183 pabt.go:193]  github.com/joeycumines/go-pabt.(*Plan).bt-fm | github.com/joeycumines/go-pabt.(*Plan).bt.func2
	// └── [util.go:144 selector.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//     ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//     └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//         │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//         │   │       │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         │   │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │           └── [graph_test.go:118 graph_test.go:118 pre:s3 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:sg post:s5]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//         │   │       │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         │   │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:s4]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s3 post:s4]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │           └── [graph_test.go:118 graph_test.go:118 pre:s1 post:s4]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s4 post:s5]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │           ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//         │   │           │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │           │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         │   │           │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │           │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │           │       │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:s2]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │           │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │           │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │           │           └── [graph_test.go:118 graph_test.go:118 pre:s1 post:s2]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │           └── [graph_test.go:118 graph_test.go:118 pre:s2 post:s5]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//             │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//             │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//             │       │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//             │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   │           └── [graph_test.go:118 graph_test.go:118 pre:s3 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       │   └── [graph_test.go:118 graph_test.go:118 pre:sg post:s3]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//             │       │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//             │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:s4]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s3 post:s4]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   │           └── [graph_test.go:118 graph_test.go:118 pre:s1 post:s4]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       │   └── [graph_test.go:118 graph_test.go:118 pre:s4 post:s3]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │           └── [graph_test.go:118 graph_test.go:118 pre:s1 post:s3]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             └── [graph_test.go:118 graph_test.go:118 pre:s3 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//
	// iteration = 9, status = running, err = <nil>, actor = s0
	// [pabt.go:183 pabt.go:193]  github.com/joeycumines/go-pabt.(*Plan).bt-fm | github.com/joeycumines/go-pabt.(*Plan).bt.func2
	// └── [util.go:144 selector.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//     ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//     └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//         │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//         │   │       │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         │   │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │           └── [graph_test.go:118 graph_test.go:118 pre:s3 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:sg post:s5]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//         │   │       │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         │   │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:s4]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s3 post:s4]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │           └── [graph_test.go:118 graph_test.go:118 pre:s1 post:s4]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s4 post:s5]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │           ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//         │   │           │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │           │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         │   │           │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │           │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │           │       │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:s2]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │           │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │           │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │           │           └── [graph_test.go:118 graph_test.go:118 pre:s1 post:s2]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │           └── [graph_test.go:118 graph_test.go:118 pre:s2 post:s5]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//             │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//             │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//             │       │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//             │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   │           └── [graph_test.go:118 graph_test.go:118 pre:s3 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       │   └── [graph_test.go:118 graph_test.go:118 pre:sg post:s3]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//             │       │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//             │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:s4]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s3 post:s4]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   │           └── [graph_test.go:118 graph_test.go:118 pre:s1 post:s4]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       │   └── [graph_test.go:118 graph_test.go:118 pre:s4 post:s3]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │           ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//             │           │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │           │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//             │           │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │           │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │           │       │   └── [graph_test.go:118 graph_test.go:118 pre:s4 post:s1]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │           │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │           │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │           │       │   └── [graph_test.go:118 graph_test.go:118 pre:s3 post:s1]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │           │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │           │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │           │       │   └── [graph_test.go:118 graph_test.go:118 pre:s2 post:s1]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │           │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │           │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │           │           └── [graph_test.go:118 graph_test.go:118 pre:s0 post:s1]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │           └── [graph_test.go:118 graph_test.go:118 pre:s1 post:s3]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             └── [graph_test.go:118 graph_test.go:118 pre:s3 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//
	// actor s0 -> s1
	//
	// actor s1 -> s3
	//
	// actor s3 -> sg
	//
	// iteration = 10, status = success, err = <nil>, actor = sg
	// [pabt.go:183 pabt.go:193]  github.com/joeycumines/go-pabt.(*Plan).bt-fm | github.com/joeycumines/go-pabt.(*Plan).bt.func2
	// └── [util.go:144 selector.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//     ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//     └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//         │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//         │   │       │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         │   │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │           └── [graph_test.go:118 graph_test.go:118 pre:s3 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:sg post:s5]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//         │   │       │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         │   │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:s4]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s3 post:s4]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │       │   │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │       │   │           └── [graph_test.go:118 graph_test.go:118 pre:s1 post:s4]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s4 post:s5]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │           ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//         │   │           │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │           │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//         │   │           │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │           │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │           │       │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:s2]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │           │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//         │   │           │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//         │   │           │           └── [graph_test.go:118 graph_test.go:118 pre:s1 post:s2]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   │           └── [graph_test.go:118 graph_test.go:118 pre:s2 post:s5]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//         └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//             │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//             │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//             │       │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//             │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   │           └── [graph_test.go:118 graph_test.go:118 pre:s3 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       │   └── [graph_test.go:118 graph_test.go:118 pre:sg post:s3]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//             │       │   │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//             │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s5 post:s4]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       │   │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   │       │   └── [graph_test.go:118 graph_test.go:118 pre:s3 post:s4]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       │   │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │       │   │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │       │   │           └── [graph_test.go:118 graph_test.go:118 pre:s1 post:s4]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       │   └── [graph_test.go:118 graph_test.go:118 pre:s4 post:s3]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │           ├── [util.go:144       selector.go:21                  ]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Selector
	//             │           │   ├── [util.go:158 util.go:158   ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │           │   └── [util.go:144 memorize.go:34]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Memorize.func1
	//             │           │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │           │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │           │       │   └── [graph_test.go:118 graph_test.go:118 pre:s4 post:s1]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │           │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │           │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │           │       │   └── [graph_test.go:118 graph_test.go:118 pre:s3 post:s1]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │           │       ├── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │           │       │   ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │           │       │   └── [graph_test.go:118 graph_test.go:118 pre:s2 post:s1]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │           │       └── [util.go:144 sequence.go:21]  github.com/joeycumines/go-pabt.(*node).group-fm | github.com/joeycumines/go-behaviortree.Sequence
	//             │           │           ├── [util.go:158       util.go:158                     ]  github.com/joeycumines/go-pabt.newConditionNode | github.com/joeycumines/go-pabt.newConditionNode.func1
	//             │           │           └── [graph_test.go:118 graph_test.go:118 pre:s0 post:s1]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             │           └── [graph_test.go:118 graph_test.go:118 pre:s1 post:s3]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//             └── [graph_test.go:118 graph_test.go:118 pre:s3 post:sg]  github.com/joeycumines/go-pabt.(*graphState).Actions | github.com/joeycumines/go-pabt.(*graphState).Actions.func1
	//
	// iteration = 10, status = success, err = <nil>, actor = sg
	// DONE
}
