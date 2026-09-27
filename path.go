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
	"strings"

	bt "github.com/joeycumines/go-behaviortree"
)

// PathEntry represents a single node in the plan execution path.
type PathEntry[T Condition] struct {
	// NodeType identifies the role of this node in the PPA structure.
	NodeType NodeType

	// Condition is set for precondition nodes.
	Condition T

	// Effects is set for action nodes.
	Effects Effects

	// PostCondition is set for action nodes that have a post-condition
	// matching the effect type. It holds the condition that the action
	// was expanded to satisfy.
	PostCondition T

	// conditionSummary and effectsSummary are the condition's declaration and
	// the action's effects, already rendered while the planner read them. The
	// path renderer prefers them, because re-deriving either from Condition,
	// Effect, or PostCondition calls back into the application. They are empty
	// for a plan built without a capture.
	conditionSummary string
	effectsSummary   string
}

// isUnset reports whether an entry's optional condition reference was never set.
//
// It cannot be a comparison against nil, because a nil held in an interface is
// only nil as an interface when T is itself the Condition interface. Over the
// concrete condition pointer a plan is normally built with, an unset T is a
// typed nil, which is not a nil interface: every entry without a condition - the
// root included, since it is appended before the walk begins - read as having
// one, and rendering it called a method on a nil receiver and panicked.
// Comparing against the zero value of T is the one test that means the same thing
// for both forms, and it keeps honouring an entry a caller built by hand, which
// is a contract two tests in this file depend on.
//
// A T that cannot be compared has no such test. That is not only a slice, map, or
// function type: a struct holding one of those fields is just as uncomparable, and
// a struct-valued condition with a slice field is far more ordinary than a
// slice-valued one. Comparing it panics, and this function used to be a
// comparison. So the comparison is made through sameValue, which reports "not the
// same" for a type it cannot compare rather than taking the process down with it.
//
// The fallback is "set", which is what a comparison against nil already said for
// every one of those types. A condition that has no comparable form is an
// application's own shape to make sense of, and this function's job is to stop
// reporting a typed nil as a value - not to decide what a struct's zero field
// means. Asking the type through reflect would answer it, and would cost a
// reflect.Value on a path walked once per rendered string.
func isUnset[T any](v T) bool {
	var zero T
	return sameValue(any(v), any(zero))
}

// sameValue reports whether two values are equal, and reports false for a pair it
// cannot compare.
//
// Comparing two any values whose dynamic type is uncomparable is a run-time panic
// in net/http's own idiom and in this one's: a struct with a slice field, a slice,
// a map, a function. The recover is what makes the question answerable rather than
// fatal, and it is here rather than at the call site because the caller cannot
// tell which T it was handed.
func sameValue(a, b any) (same bool) {
	defer func() { _ = recover() }()
	return a == b
}

// Path represents the execution path from root to the current active node.
type Path[T Condition] []PathEntry[T]

// IPathEntry is an alias for a [PathEntry] without a more-specific [Condition] type.
type IPathEntry = PathEntry[Condition]

// IPath is an alias for a [Path] without a more-specific [Condition] type.
type IPath = Path[Condition]

// GetIPath returns the execution path for an [IPlan].
// It is a convenience wrapper around [GetPath] for the non-generic [IPlan] type.
func GetIPath(plan *IPlan) IPath {
	return GetPath(plan)
}

// GetPath walks the plan's node tree from root and follows the path through
// the tree based on which branches are "active" (i.e., precondition succeeded,
// or action is running). It returns the path from root to the current active node.
func GetPath[T Condition](plan *Plan[T]) Path[T] {
	if plan == nil || plan.root == nil {
		return nil
	}
	var path Path[T]
	n := plan.root
	path = append(path, PathEntry[T]{
		NodeType: n.nodeType(),
	})

	for {
		next := followActive(n, plan)
		if next == nil {
			break
		}
		entry := PathEntry[T]{
			NodeType: next.nodeType(),
		}

		// Populate condition for precondition nodes
		if next.precondition != nil && next.precondition.root == next {
			entry.Condition = next.precondition.condition
			if next.precondition.captured {
				entry.conditionSummary = next.precondition.declaration()
			}
		}

		// Populate effects and post-condition for action nodes
		if next.action != nil && next.action.node == next {
			entry.Effects = next.buildEffects()
			// The post-condition is the condition that triggered the expansion
			// that created this action's PPA subtree. It's stored in the
			// precondition that was expanded.
			if next.action.cache != nil {
				entry.effectsSummary = next.action.cache.name
			}
			if next.ppa != nil && next.ppa.post != nil &&
				next.ppa.post.precondition != nil {
				entry.PostCondition = next.ppa.post.precondition.condition
				if next.ppa.post.precondition.captured {
					entry.conditionSummary = next.ppa.post.precondition.declaration()
				}
			}
		}

		path = append(path, entry)
		n = next
	}
	return path
}

// render returns the captured declaration of a condition, or falls back to
// reading its key from the application when the plan was not built with a
// capture.
func (e PathEntry[T]) render(cond T) string {
	if e.conditionSummary != "" {
		return e.conditionSummary
	}
	return fmt.Sprintf("%v", any(cond).(Condition).Key())
}

// followActive returns the next active child of n, or nil if no active child.
func followActive[T Condition](n *node[T], plan *Plan[T]) *node[T] {
	nt := n.nodeType()

	switch nt {
	case NodeTypeGoalRoot, NodeTypeGoalSelector:
		// Goal nodes: follow the child whose subtree contains the active path
		return findActiveChild(n, plan)

	case NodeTypePPARoot:
		// PPA selector: first child is post-condition, rest are action branches
		// If post-condition succeeded, the path goes through it
		// Otherwise, follow the action branches
		if n.first != nil && n.first.precondition != nil &&
			n.first.precondition.status == bt.Success {
			return n.first
		}
		// Follow action branches
		return findActiveChild(n, plan)

	case NodeTypePPAPost:
		// Post-condition leaf: no children to follow
		return nil

	case NodeTypeActionSelector:
		// Memorized selector over actions: follow the active action
		return findActiveChild(n, plan)

	case NodeTypeActionRoot:
		// Sequence: follow children in order, find the active one
		return findActiveChild(n, plan)

	case NodeTypeActionNode:
		// Action leaf: no children
		return nil

	case NodeTypePreconditionsRoot:
		// AND sequence of preconditions: follow children
		return findActiveChild(n, plan)

	case NodeTypePreconditionLeaf:
		// Precondition leaf: no children
		return nil

	default:
		return findActiveChild(n, plan)
	}
}

// findActiveChild walks the children of n and returns the first child
// that is part of the active execution path.
func findActiveChild[T Condition](n *node[T], plan *Plan[T]) *node[T] {
	for child := n.first; child != nil; child = child.next {
		if isActive(child, plan) {
			return child
		}
	}
	return nil
}

// isActive returns true if the given node is part of the active execution path.
func isActive[T Condition](n *node[T], plan *Plan[T]) bool {
	nt := n.nodeType()

	switch nt {
	case NodeTypePreconditionLeaf:
		// A precondition is active if it succeeded (part of the path)
		if n.precondition != nil {
			return n.precondition.status == bt.Success
		}
		return false

	case NodeTypeActionNode:
		// An action node is active if the plan is running and this
		// action is the one that's running, or if it has succeeded
		if n.status != nil {
			return n.status.LastStatus() == bt.Running || n.status.LastStatus() == bt.Success
		}
		// All planner-created action nodes have status; conservative fallback
		return false

	case NodeTypePPAPost:
		// Post-condition is active if it succeeded
		if n.precondition != nil {
			return n.precondition.status == bt.Success
		}
		return false

	case NodeTypeGoalRoot, NodeTypeGoalSelector,
		NodeTypePPARoot, NodeTypeActionSelector,
		NodeTypeActionRoot, NodeTypePreconditionsRoot:
		// Group nodes are active if any of their children are active
		for child := n.first; child != nil; child = child.next {
			if isActive(child, plan) {
				return true
			}
		}
		return false

	default:
		return false
	}
}

// String formats the path like:
// GoalRoot → PPARoot → ActionSelector → ActionRoot → ActionNode(post:actor=s5)
func (p Path[T]) String() string {
	if len(p) == 0 {
		return ""
	}
	var parts []string
	for _, entry := range p {
		s := entry.NodeType.String()
		switch {
		case !isUnset(entry.Condition):
			s += "(cond:" + entry.render(entry.Condition) + ")"
		case len(entry.Effects) > 0:
			if entry.effectsSummary != "" {
				s += "(" + entry.effectsSummary + ")"
				break
			}
			var effectParts []string
			for _, e := range entry.Effects {
				effectParts = append(effectParts, fmt.Sprintf("%v=%v", e.Key(), e.Value()))
			}
			s += "(" + strings.Join(effectParts, ",") + ")"
		case !isUnset(entry.PostCondition):
			s += "(post:" + entry.render(entry.PostCondition) + ")"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " → ")
}
