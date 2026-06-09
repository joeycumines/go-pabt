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
		}

		// Populate effects and post-condition for action nodes
		if next.action != nil && next.action.node == next {
			entry.Effects = next.buildEffects()
			// The post-condition is the condition that triggered the expansion
			// that created this action's PPA subtree. It's stored in the
			// precondition that was expanded.
			if next.ppa != nil && next.ppa.post != nil &&
				next.ppa.post.precondition != nil {
				entry.PostCondition = next.ppa.post.precondition.condition
			}
		}

		path = append(path, entry)
		n = next
	}
	return path
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
		case any(entry.Condition) != nil:
			s += fmt.Sprintf("(cond:%v)", any(entry.Condition).(Condition).Key())
		case len(entry.Effects) > 0:
			var effectParts []string
			for _, e := range entry.Effects {
				effectParts = append(effectParts, fmt.Sprintf("%v=%v", e.Key(), e.Value()))
			}
			s += "(" + strings.Join(effectParts, ",") + ")"
		case any(entry.PostCondition) != nil:
			s += fmt.Sprintf("(post:%v)", any(entry.PostCondition).(Condition).Key())
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " → ")
}
