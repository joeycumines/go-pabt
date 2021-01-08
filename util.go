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
)

func (n *node) append(next *node, children ...*node) {
	if n.node != nil {
		panic(fmt.Errorf(`pabt: invalid append`))
	}
	for _, child := range children {
		child.delete()
		var prev *node
		if next != nil {
			prev = next.prev
		} else {
			prev = n.last
		}
		child.parent = n
		if next != nil {
			child.next = next
			next.prev = child
		} else {
			n.last = child
		}
		if prev != nil {
			child.prev = prev
			prev.next = child
		} else {
			n.first = child
		}
	}
}
func (n *node) delete() {
	var (
		prev   = n.prev
		next   = n.next
		parent = n.parent
	)
	if prev != nil {
		prev.next = next
	}
	if next != nil {
		next.prev = prev
	}
	if parent != nil {
		if parent.first == n {
			parent.first = next
		}
		if parent.last == n {
			parent.last = prev
		}
	}
	n.prev = nil
	n.next = nil
	n.parent = nil
}
func (n *node) generateOr(goal []Conditions) (or []*preconditions, err error) {
	switch len(goal) {
	case 0:
		n.tick = bt.Sequence
	case 1:
		n.preconditions = &preconditions{root: n}
		or = []*preconditions{n.preconditions}
	default:
		n.tick = bt.Selector
		for range goal {
			node := &node{
				goal:          n.goal,
				ppa:           n.ppa,
				action:        n.action,
				preconditions: &preconditions{},
			}
			node.preconditions.root = node
			n.append(nil, node)
			or = append(or, node.preconditions)
		}
	}
	for i, preconditions := range or {
		preconditions.and, err = preconditions.root.generateAnd(goal[i])
		if err != nil {
			return
		}
	}
	return
}
func (n *node) generateAnd(conditions Conditions) (and map[interface{}]*precondition, err error) {
	err = fmt.Errorf(`pabt: invalid conditions`)
	if len(conditions) == 0 {
		return
	}
	n.tick = bt.Sequence
	and = make(map[interface{}]*precondition, len(conditions))
	for _, condition := range conditions {
		key := condition.Key()
		if !func() bool {
			defer func() { _ = recover() }()
			if _, ok := and[key]; ok {
				return false
			}
			return true
		}() {
			return
		}
		node := &node{
			goal:          n.goal,
			ppa:           n.ppa,
			action:        n.action,
			preconditions: n.preconditions,
			precondition:  &precondition{condition: condition},
		}
		node.precondition.root = node
		node.node = newConditionNode(n.goal.state, key, condition.Match, &node.precondition.status)
		n.append(nil, node)
		and[key] = node.precondition
	}
	err = nil
	return
}
func (n *node) bt() (node bt.Node) {
	node = n.node
	if node == nil {
		node = n.group
	}
	return
}
func (n *node) group() (tick bt.Tick, children []bt.Node) {
	tick = n.tick
	for node := n.first; node != nil; node = node.next {
		children = append(children, node.bt())
	}
	return
}

func newConditionNode(
	state State,
	key interface{},
	match func(value interface{}) bool,
	outcome *bt.Status,
) bt.Node {
	return bt.New(func([]bt.Node) (status bt.Status, err error) {
		var value interface{}
		value, err = state.Variable(key)
		if err == nil && match(value) {
			status = bt.Success
		} else {
			status = bt.Failure
		}
		*outcome = status
		return
	})
}

// copy updates all fields of the receiver from src except the tree links then returns the receiver
func (n *node) copy(src *node) *node {
	// TODO test
	n.goal = src.goal
	n.ppa = src.ppa
	n.action = src.action
	n.preconditions = src.preconditions
	n.precondition = src.precondition
	n.node = src.node
	n.tick = src.tick
	return n
}
func (n *node) search() (*precondition, bool) {
	queue := []*node{n}
	for len(queue) != 0 {
		item := queue[0]
		queue[0] = nil
		queue = queue[1:]
		if cf := item.precondition; cf != nil &&
			cf.status == bt.Failure &&
			cf.root.precondition == cf {
			// failed unexpanded condition
			return cf, true
		}
		for item = item.first; item != nil; item = item.next {
			queue = append(queue, item)
		}
	}
	return nil, false
}
func (p *precondition) expand() (err error) {
	var acts []Action
	acts, err = p.root.goal.state.Actions(p.condition)
	if err != nil {
		return
	}

	// original root is copied and used as the post-condition, then has it's links preserved and is updated
	// with a new ppa (linking to the new copy), note the original root has all fields overwritten except it's links
	p.root.copy(&node{
		goal: p.root.goal,
		ppa: &ppa{
			root: p.root,
			post: new(node).copy(p.root),
		},
		tick: bt.Selector,
	})
	// first child of the selector is the post-condition
	p.root.append(nil, p.root.ppa.post)

	// need to build all actions as their own trees first
	for _, act := range acts {
		_, err = p.root.generateAction(p.condition, act)
		if err != nil {
			return
		}
	}

	// switch how actions are wired up based on how many there are
	switch len(p.root.ppa.actions) {
	case 0:
	case 1:
		p.root.append(nil, p.root.ppa.actions[0].root)
	default:
		node := &node{
			goal: p.root.goal,
			ppa:  p.root.ppa,
			tick: bt.Memorize(bt.Selector),
		}
		for _, act := range p.root.ppa.actions {
			node.append(nil, act.root)
		}
		p.root.append(nil, node)
	}
	return
}
func (n *node) generateAction(post Condition, act Action) (ok bool, err error) {
	err = fmt.Errorf(`pabt: invalid action`)

	r := new(action)

	// map the effects
	{
		effects := act.Effects()
		if len(effects) == 0 {
			return
		}
		pk := post.Key()
		r.effects = make(map[interface{}]Effect, len(effects))
		for _, effect := range effects {
			key := effect.Key()
			if !func() bool {
				defer func() { _ = recover() }()
				if _, ok := r.effects[key]; ok {
					return false
				}
				return true
			}() {
				return
			}
			r.effects[key] = effect
			if !ok && key == pk && post.Match(effect.Value()) {
				ok = true
			}
		}
	}
	if !ok {
		err = nil
		return
	}

	// create action node
	r.node = &node{
		goal:   n.goal,
		ppa:    n.ppa,
		action: r,
		node:   act.Node(),
	}
	if r.node.node == nil {
		return
	}

	// build the conditions root as the action root (for the moment)
	r.root = &node{
		goal:   n.goal,
		ppa:    n.ppa,
		action: r,
	}
	r.or, err = r.root.generateOr(act.Conditions())
	if err != nil {
		return
	}

	// finish preparing the action root
	switch len(r.or) {
	case 0, 1:
		// r.root.tick is bt.Sequence, action can be appended as-is
	default:
		// more than one Conditions, need another layer
		condRoot := r.root
		r.root = &node{
			goal:   n.goal,
			ppa:    n.ppa,
			action: r,
			tick:   bt.Sequence,
		}
		r.root.append(nil, condRoot)
	}

	// add the action node to the action root
	r.root.append(nil, r.node)

	// update the receiver (wiring up of the tree happens independently)
	n.ppa.actions = append(n.ppa.actions, r)
	return
}
func (p *ppa) resolve() (conflicts int) {
	for c := p.conflict(); c != nil; c = p.conflict() {
		c.root.parent.append(c.root, p.root)
		conflicts++
	}
	return
}
func (p *ppa) conflict() *ppa {
	// finding a conflict involves checking the new subtree's (receiver) conditions against the
	// effects of any actions that may be executed prior to it, in the tree structure
	//
	// the provided examples did not make it clear how to handle EITHER the multi-action or
	// multi-condition cases (disjoint sets of either i.e. multiple feasible actions and
	// multiple feasible sets of conditions).
	//
	// Relevant excerpt:
	//
	// Similar to any STRIPS-style planner, adding a new action in the plan can cause a
	// conflict (i.e. the execution of this new action reverses the effects of a previous action).
	// In PA-BT, this possibility is checked in Algorithm 5 Line 11 by analyzing
	// the conditions of the new action added with the effects of the actions that the subtree executes
	// before executing the new action. If this effects/conditions pair is in conflict, the goal will
	// not be reached.
	// ..
	// Again, following the approach used in STRIPS-style planners, we resolve this
	// conflict by finding the correct action order. Exploiting the structure of BTs we can
	// do so by moving the tree composed by the new action and its condition leftward (a
	// BT executes its children from left to right, thus moving a subtree leftward implies
	// executing the new action earlier). If it is the leftmost one, this means that it must
	// be executed before its parent (i.e. it must be placed at the same depth of the parent
	// but to its left). This operation is done in Algorithm 5 Line 12. PA-BT incrementally
	// increases the priority of this subtree in this way, until it finds a feasible tree. In [10]
	// it is proved that, under certain assumptions, a feasible tree always exists.

	// check everything left -> up, of n
	// n must always be a child in a sequence (ppa and unexpanded precondition roots always are)
	n := p.root
	for {
		switch {
		case n.prev != nil:
			// can move left
			n = n.prev
			if n.ppa == nil || n.ppa.root != n {
				// only checking actions in an expanded ppa
				continue
			}
		case n.parent != nil && n.parent.ppa != nil:
			// can move up
			n = n.parent.ppa.root
			// avoid checking the same tree
			continue
		default:
			// no conflict
			return nil
		}
		// n should always be an as-yet unchecked ppa root
		if p.conflicts(n.ppa) {
			return n.ppa
		}
	}
}
func (p *ppa) conflicts(o *ppa) bool {
	// prepare conditions for comparison
	type Pair struct {
		K interface{}
		V Condition
	}
	var pairs []Pair
	for _, act := range p.actions {
		for _, or := range act.or {
			for key, and := range or.and {
				pairs = append(pairs, Pair{key, and.condition})
			}
		}
	}

	// fast path
	if len(pairs) == 0 {
		return false
	}

	// ensure that p's conditions are compatible with any corresponding effects from o (also checks all sub-ppa)
	queue := []*ppa{o}
	for len(queue) != 0 {
		o = queue[0]
		queue[0] = nil
		queue = queue[1:]
		for _, act := range o.actions {
			for _, pair := range pairs {
				if eff, ok := act.effects[pair.K]; ok && !pair.V.Match(eff.Value()) {
					return true
				}
			}
			for _, or := range act.or {
				for _, and := range or.and {
					if and.root == and.root.ppa.root {
						queue = append(queue, and.root.ppa)
					}
				}
			}
		}
	}

	return false
}
