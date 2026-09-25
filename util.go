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
	"iter"
	"sort"
	"strings"
	"time"

	bt "github.com/joeycumines/go-behaviortree"
)

func captureFrame(n bt.Node) *FrameInfo {
	if n == nil {
		return nil
	}
	if f := bt.GetFrame(n); f != nil {
		return &FrameInfo{
			File:     f.File,
			Line:     f.Line,
			Function: f.Function,
		}
	}
	return nil
}

func (n *node[T]) append(next *node[T], children ...*node[T]) {
	if n.node != nil {
		panic(fmt.Errorf(`pabt: invalid append`))
	}
	for _, child := range children {
		child.delete()
		var prev *node[T]
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
func (n *node[T]) delete() {
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
func (n *node[T]) generateOr(goal []Conditions[T]) (or []*preconditions[T], err error) {
	switch len(goal) {
	case 0:
		n.tick = bt.Sequence
	case 1:
		n.preconditions = &preconditions[T]{root: n}
		n.typ = NodeTypePreconditionsRoot
		or = []*preconditions[T]{n.preconditions}
	default:
		n.tick = bt.Selector
		for range goal {
			node := &node[T]{
				goal:          n.goal,
				ppa:           n.ppa,
				action:        n.action,
				preconditions: &preconditions[T]{},
				typ:           NodeTypePreconditionsRoot,
				id:            n.goal.mint(),
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
func (n *node[T]) generateAnd(conditions Conditions[T]) (and map[any]*precondition[T], err error) {
	err = fmt.Errorf(`pabt: invalid conditions`)
	if len(conditions) == 0 {
		return
	}
	n.tick = bt.Sequence
	and = make(map[any]*precondition[T], len(conditions))
	c := n.goal.capture()
	for _, condition := range conditions {
		key := condition.Key()
		if c != nil {
			c.recordKey(SubjectCondition, "", "", key)
		}
		if !func() bool {
			defer func() { _ = recover() }()
			if _, ok := and[key]; ok {
				return false
			}
			return true
		}() {
			return
		}
		pre := &precondition[T]{condition: condition, key: key, id: n.goal.mint(), captured: c != nil}
		if c != nil {
			// An absent declaration stays unknown. It is never inferred from
			// the key, because the key names a variable, not a requirement.
			pre.expected, pre.expectedKnown = ExpectedValue(condition)
		}
		node := &node[T]{
			goal:          n.goal,
			ppa:           n.ppa,
			action:        n.action,
			preconditions: n.preconditions,
			precondition:  pre,
			typ:           NodeTypePreconditionLeaf,
			status:        &NodeStatus{},
			id:            n.goal.mint(),
		}
		node.precondition.root = node
		node.node = newConditionNode(n.goal, key, pre.id, node.id, condition.Match, pre, node.status)
		node.frame = captureFrame(node.node)
		n.append(nil, node)
		and[key] = node.precondition
	}
	err = nil
	return
}
func (n *node[T]) bt() (node bt.Node) {
	node = n.node
	if node == nil {
		node = n.group
	} else {
		orig := node
		node = func() (bt.Tick, []bt.Node) {
			bt.UseValueProviders(
				(*nodeValueProvider[T])(n),
				bt.UseName(n.nodeType().String()),
				bt.UseStructure(n.childrenSeq()),
			)
			return orig()
		}
	}
	return
}

func (n *node[T]) group() (tick bt.Tick, children []bt.Node) {
	bt.UseValueProviders(
		(*nodeValueProvider[T])(n),
		bt.UseName(n.nodeType().String()),
		bt.UseStructure(n.childrenSeq()),
	)
	tick = n.tick
	for node := n.first; node != nil; node = node.next {
		children = append(children, node.bt())
	}
	return
}

// Value implements a value lookup for this node's metadata.
func (n *node[T]) Value(key any) any {
	switch key.(type) {
	case nodeInfoKey:
		return n.buildNodeInfo()
	case statusKey:
		return n.status
	}
	return nil
}

// childrenSeq returns an iter.Seq[bt.Metadata] for this node's children.
// This is used by bt.UseStructure for bt.Walk compatibility.
func (n *node[T]) childrenSeq() iter.Seq[bt.Metadata] {
	return func(yield func(bt.Metadata) bool) {
		for c := n.first; c != nil; c = c.next {
			if !yield(c.buildNodeInfo()) {
				return
			}
		}
	}
}

func (n *node[T]) buildNodeInfo() *NodeInfo {
	info := &NodeInfo{
		Type:     n.nodeType(),
		Status:   n.status,
		children: n.childrenSeq(),
		Name:     n.cachedName(),
	}

	// Add precondition info
	if n.precondition != nil && n.precondition.root == n {
		// The key was read once when the condition was created; reading it
		// again here would call back into the application on every inspection.
		info.VariableKey = n.precondition.key
		info.Condition = n.precondition.condition
		if n.precondition.captured {
			// A condition that declares no expectation renders as such. It is
			// never replaced by a value derived from its key.
			info.conditionSummary = "<undeclared>"
			if n.precondition.expectedKnown {
				info.conditionSummary = fmt.Sprintf("%v", n.precondition.expected)
			}
		}
	}

	// Add action effects info
	if n.action != nil && n.action.node == n {
		info.Effects = n.buildEffects()
		if n.action.cache != nil {
			info.effectsSummary = n.action.cache.name
		}
	}

	// Capture frame info for leaf nodes
	if n.frame != nil {
		info.Frame = n.frame
	}

	return info
}

// nodeName is the node's display name, read from the current tree.
func (n *node[T]) nodeName() string {
	switch n.nodeType() {
	case NodeTypeActionNode:
		return n.actionName()
	case NodeTypePreconditionLeaf:
		if n.precondition != nil {
			return fmt.Sprintf("%v", n.precondition.key)
		}
	}
	return n.nodeType().String()
}

// cachedName prefers facts captured when the planner called into the
// application, so inspecting a captured plan makes no application call.
func (n *node[T]) cachedName() string {
	if n.action != nil && n.action.node == n && n.action.cache != nil {
		return n.action.cache.name
	}
	return n.nodeName()
}

func (n *node[T]) actionName() string {
	if n.action == nil {
		return "Action"
	}
	return n.action.actionName()
}

// effectName renders an action's effect keys and their declared values in a
// stable order, so the same effects always produce the same name.
func (a *action[T]) effectName(values map[any]any) string {
	if len(a.effects) == 0 {
		return "Action"
	}
	keys := make([]any, 0, len(a.effects))
	for key := range a.effects {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return fmt.Sprintf("%v", keys[i]) < fmt.Sprintf("%v", keys[j])
	})
	var b strings.Builder
	for i, key := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(fmt.Sprintf("%v→%v", key, values[key]))
	}
	return b.String()
}

// actionName returns the cached name when a capture recorded the effect values,
// and otherwise reads them from the application as before.
func (a *action[T]) actionName() string {
	if a.cache != nil {
		return a.cache.name
	}
	values := make(map[any]any, len(a.effects))
	for key, eff := range a.effects {
		values[key] = eff.Value()
	}
	return a.effectName(values)
}

func (n *node[T]) nodeType() NodeType {
	if n.typ != NodeTypeUnknown {
		return n.typ
	}
	switch {
	case n.goal != nil && n.goal.root == n:
		if len(n.goal.or) > 1 {
			return NodeTypeGoalSelector
		}
		return NodeTypeGoalRoot
	case n.ppa != nil && n.ppa.root == n:
		return NodeTypePPARoot
	case n.ppa != nil && n.ppa.post == n:
		return NodeTypePPAPost
	case n.action != nil && n.action.root == n:
		return NodeTypeActionRoot
	case n.action != nil && n.action.node == n:
		return NodeTypeActionNode
	case n.preconditions != nil && n.preconditions.root == n:
		return NodeTypePreconditionsRoot
	case n.precondition != nil && n.precondition.root == n:
		return NodeTypePreconditionLeaf
	default:
		// Check if this is an intermediate selector node
		if n.tick != nil && n.action != nil {
			return NodeTypeActionSelector
		}
		return NodeTypeUnknown
	}
}

func (n *node[T]) buildEffects() Effects {
	if n.action == nil {
		return nil
	}
	keys := make([]any, 0, len(n.action.effects))
	for key := range n.action.effects {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return fmt.Sprintf("%v", keys[i]) < fmt.Sprintf("%v", keys[j])
	})
	effects := make(Effects, 0, len(keys))
	for _, key := range keys {
		effects = append(effects, n.action.effects[key])
	}
	return effects
}

// newConditionNode builds the leaf that evaluates one condition.
//
// The variable read and the match are both routed through the tick's capture
// when one is active, so every condition value a tick reports came from the same
// world and each match is attributed to the reason it was made.
func newConditionNode[T Condition](
	g *goal[T],
	key any,
	conditionID, nodeID OccurrenceID,
	match func(value any) bool,
	pre *precondition[T],
	nodeStatus *NodeStatus,
) bt.Node {
	return bt.New(func([]bt.Node) (status bt.Status, err error) {
		var (
			value   any
			matched bool
		)
		c := g.capture()
		if c != nil {
			value, err = c.variable(key, nodeID, conditionID)
		} else {
			value, err = g.state.Variable(key)
		}
		if err == nil {
			if c != nil {
				matched = c.match(conditionID, nodeID, UseRuntimeEvaluation, key, value, match)
			} else {
				matched = match(value)
			}
		}
		if matched {
			status = bt.Success
		} else {
			status = bt.Failure
		}
		pre.status = status
		pre.ticked = true
		if nodeStatus != nil {
			nodeStatus.record(status)
		}
		return
	})
}

// copy updates all fields of the receiver from src except the tree links then returns the receiver
func (n *node[T]) copy(src *node[T]) *node[T] {
	n.goal = src.goal
	n.ppa = src.ppa
	n.action = src.action
	n.preconditions = src.preconditions
	n.precondition = src.precondition
	n.node = src.node
	n.tick = src.tick
	n.typ = src.typ
	n.status = src.status
	n.frame = src.frame
	return n
}
func (n *node[T]) search() (*precondition[T], bool) {
	queue := []*node[T]{n}
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
func (p *precondition[T]) expand() (err error) {
	var acts []Action[T]
	c := p.root.goal.capture()
	if c != nil {
		acts, err = c.actions(p.condition, p.id)
	} else {
		acts, err = p.root.goal.state.Actions(p.condition)
	}
	if err != nil {
		return
	}

	// original root is copied and used as the post-condition, then has it's links preserved and is updated
	// with a new ppa (linking to the new copy), note the original root has all fields overwritten except it's links
	post := new(node[T]).copy(p.root)
	post.id = p.root.goal.mint()
	post.lineage = lineageOf(p.root.id)
	p.postNodeID = post.id
	p.root.copy(&node[T]{
		goal: p.root.goal,
		ppa: &ppa[T]{
			root: p.root,
			post: post,
		},
		tick: bt.Selector,
		typ:  NodeTypePPARoot,
	})
	p.root.status = &NodeStatus{}
	// first child of the selector is the post-condition
	p.root.append(nil, p.root.ppa.post)
	p.root.ppa.post.ppa = p.root.ppa
	p.root.ppa.post.typ = NodeTypePPAPost
	p.root.ppa.post.status = &NodeStatus{}

	// need to build all actions as their own trees first
	for _, act := range acts {
		_, err = p.root.generateAction(p.condition, p.id, act)
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
		node := &node[T]{
			goal: p.root.goal,
			ppa:  p.root.ppa,
			tick: bt.Memorize(bt.Selector),
			typ:  NodeTypeActionSelector,
			id:   p.root.goal.mint(),
		}
		for _, act := range p.root.ppa.actions {
			node.append(nil, act.root)
		}
		p.root.append(nil, node)
	}
	p.root.goal.bump()
	return
}
func (n *node[T]) generateAction(post Condition, postID OccurrenceID, act Action[T]) (ok bool, err error) {
	r := new(action[T])
	r.id = n.goal.mint()
	r.postconditionID = postID
	c := n.goal.capture()
	if c != nil {
		r.cache = &actionCache{values: map[any]any{}}
	}

	// map the effects
	{
		started := time.Now()
		effects := act.Effects()
		if c != nil {
			c.recordActionCalls(CallbackActionEffects, "", r.id, len(effects), time.Since(started))
		}
		if len(effects) == 0 {
			return
		}
		pk := post.Key()
		if c != nil {
			c.recordKey(SubjectCondition, postID, "", pk)
		}
		r.effects = make(map[any]Effect, len(effects))
		for _, effect := range effects {
			key := effect.Key()
			if c != nil {
				c.recordKey(SubjectEffect, "", "", key)
			}
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
			var (
				id        OccurrenceID
				value     any
				valueRead bool
			)
			if c != nil {
				// The declared value is read once, here, so that naming the
				// action later needs no further application call.
				started := time.Now()
				value = effect.Value()
				valueRead = true
				id = n.goal.mint()
				c.recordEffectValue("", id, key, value, "", time.Since(started))
				r.cache.effects = append(r.cache.effects, EffectOccurrence{
					ID:         id,
					ActionID:   r.id,
					Key:        key,
					Value:      value,
					ValueKnown: valueRead,
				})
				r.cache.values[key] = value
			}
			if !ok && key == pk {
				if c != nil {
					ok = c.match(postID, "", UseEffectSupport, key, value, post.Match)
				} else {
					ok = post.Match(effect.Value())
				}
			}
		}
	}
	if !ok {
		return false, nil
	}
	if c != nil {
		r.cache.name = r.effectName(r.cache.values)
	}

	// create action node
	if actNode := act.Node(); actNode == nil {
		return false, fmt.Errorf(`pabt: invalid action`)
	} else {
		actionStatus := &NodeStatus{}
		nodeID := n.goal.mint()
		actNode = wrapActionNodeHandleSetRunning(n.goal, r, actNode, actionStatus, nodeID)
		r.node = &node[T]{
			goal:   n.goal,
			ppa:    n.ppa,
			action: r,
			node:   actNode,
			typ:    NodeTypeActionNode,
			status: actionStatus,
			frame:  captureFrame(actNode),
			id:     nodeID,
		}
	}

	// build the conditions root as the action root (for the moment)
	r.root = &node[T]{
		goal:   n.goal,
		ppa:    n.ppa,
		action: r,
		typ:    NodeTypeActionRoot,
		id:     n.goal.mint(),
	}
	{
		started := time.Now()
		conds := act.Conditions()
		if c != nil {
			c.recordActionCalls(CallbackActionConditions, r.node.id, r.id, len(conds), time.Since(started))
		}
		r.or, err = r.root.generateOr(conds)
	}
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
		r.root = &node[T]{
			goal:   n.goal,
			ppa:    n.ppa,
			action: r,
			tick:   bt.Sequence,
			typ:    NodeTypeActionRoot,
			id:     n.goal.mint(),
		}
		r.root.append(nil, condRoot)
	}

	// add the action node to the action root
	r.root.append(nil, r.node)

	// update the receiver (wiring up of the tree happens independently)
	n.ppa.actions = append(n.ppa.actions, r)
	return
}
func (p *ppa[T]) resolve() (conflicts int) {
	for c := p.conflict(); c != nil; c = p.conflict() {
		c.root.parent.append(c.root, p.root)
		conflicts++
	}
	if conflicts > 0 {
		p.root.goal.bump()
	}
	return
}
func (p *ppa[T]) conflict() *ppa[T] {
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
func (p *ppa[T]) conflicts(o *ppa[T]) bool {
	// prepare conditions for comparison
	type Pair struct {
		K  any
		V  Condition
		ID OccurrenceID
	}
	var pairs []Pair
	for _, act := range p.actions {
		for _, or := range act.or {
			for key, and := range or.and {
				pairs = append(pairs, Pair{key, and.condition, and.id})
			}
		}
	}

	// fast path
	if len(pairs) == 0 {
		return false
	}

	// Map iteration order is unspecified, so the pairs are ordered before any
	// are compared. The answer is unchanged by the order, but the recorded
	// evidence for a run would otherwise differ between identical runs.
	sort.Slice(pairs, func(i, j int) bool {
		return fmt.Sprintf("%v", pairs[i].K) < fmt.Sprintf("%v", pairs[j].K)
	})

	// ensure that p's conditions are compatible with any corresponding effects from o (also checks all sub-ppa)
	c := p.root.goal.capture()
	queue := []*ppa[T]{o}
	for len(queue) != 0 {
		o = queue[0]
		queue[0] = nil
		queue = queue[1:]
		for _, act := range o.actions {
			for _, pair := range pairs {
				eff, ok := act.effects[pair.K]
				if !ok {
					continue
				}
				started := time.Now()
				value := eff.Value()
				if c != nil {
					c.recordEffectValue("", "", pair.K, value, UseConflictTest, time.Since(started))
				}
				var matched bool
				if c != nil {
					matched = c.match(pair.ID, "", UseConflictTest, pair.K, value, pair.V.Match)
				} else {
					matched = pair.V.Match(value)
				}
				if !matched {
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

// wrapActionNodeHandleSetRunning wraps an action's node so the plan notices a
// running action and so the action's completion is observed.
//
// The revision read before the action runs and the revision read after it are
// the two ends of a transition claim, which is why they are taken here rather
// than both being read from one post-action world.
func wrapActionNodeHandleSetRunning[T Condition](
	g *goal[T],
	act *action[T],
	actNode bt.Node,
	nodeStatus *NodeStatus,
	nodeID OccurrenceID,
) bt.Node {
	return func() (bt.Tick, []bt.Node) {
		tick, children := actNode()
		if tick == nil {
			return nil, children
		}
		return func(children []bt.Node) (status bt.Status, err error) {
			c := g.capture()
			var (
				before          StateRevision
				beforeCoherence StateCoherence
			)
			if c != nil {
				before, beforeCoherence = c.view.Revision(), c.view.Coherence()
			}
			started := time.Now()
			status, err = tick(children)
			if err == nil && status == bt.Running && g.running != nil {
				*g.running = true
			}
			if nodeStatus != nil {
				nodeStatus.record(status)
			}
			if c != nil {
				c.afterAction(g, act, nodeID, ActionExecution{
					ActionID:      act.id,
					NodeID:        nodeID,
					Status:        status,
					StatusPresent: ValidateStatus(status) == nil,
					Err:           recordedError(err),
					StartedAt:     started,
				}, before, beforeCoherence, started)
			}
			return
		}, children
	}
}
