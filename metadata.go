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
	"iter"

	bt "github.com/joeycumines/go-behaviortree"
)

// nodeInfoKey is the unexported key type for retrieving NodeInfo via Value.
type nodeInfoKey struct{}

// NodeType identifies the role of a node in the PPA structure.
type NodeType int

const (
	NodeTypeUnknown           NodeType = iota
	NodeTypeGoalRoot                   // Root goal node
	NodeTypeGoalSelector               // Goal OR-branch selector (multiple goals)
	NodeTypePPARoot                    // Pre-post action selector
	NodeTypePPAPost                    // Post-condition (original precondition)
	NodeTypeActionSelector             // Action OR-branch (multiple actions)
	NodeTypeActionRoot                 // Action sequence with conditions
	NodeTypeActionNode                 // Actual action leaf (from Action.Node())
	NodeTypePreconditionsRoot          // AND of preconditions
	NodeTypePreconditionLeaf           // Single precondition check
)

var nodeTypeNames = [...]string{
	"Unknown",
	"GoalRoot",
	"GoalSelector",
	"PPARoot",
	"PPAPost",
	"ActionSelector",
	"ActionRoot",
	"ActionNode",
	"PreconditionsRoot",
	"PreconditionLeaf",
}

// String returns a human-readable name for the NodeType.
func (t NodeType) String() string {
	if t >= 0 && int(t) < len(nodeTypeNames) {
		return nodeTypeNames[t]
	}
	return nodeTypeNames[0]
}

// NodeInfo provides structured access to pabt node metadata.
// It implements bt.Metadata for use with bt.Walk.
type NodeInfo struct {
	// Type identifies the role of this node in the PPA structure.
	Type NodeType

	// VariableKey is set for precondition nodes, identifying the variable.
	VariableKey any

	// Condition is set for precondition nodes.
	Condition Condition

	// Effects is set for action nodes, containing the action's effects.
	Effects Effects

	// children yields bt.Metadata objects for Walk compatibility.
	children iter.Seq[bt.Metadata]
}

// Value implements bt.Valuer. Returns nil for all keys.
// NodeInfo fields should be accessed directly.
func (i *NodeInfo) Value(key any) any { return nil }

// Children implements bt.Metadata.
// Yields the logical children as bt.Metadata for bt.Walk compatibility.
func (i *NodeInfo) Children(yield func(bt.Metadata) bool) {
	if i.children != nil {
		for child := range i.children {
			if !yield(child) {
				return
			}
		}
	}
}

// GetNodeInfo retrieves the NodeInfo from a bt.Valuer, or nil if not present.
// This is the primary API for accessing pabt node metadata.
func GetNodeInfo(v bt.Valuer) *NodeInfo {
	if v == nil {
		return nil
	}
	if info := v.Value(nodeInfoKey{}); info != nil {
		return info.(*NodeInfo)
	}
	return nil
}

// UseNodeInfo returns a bt.ValueProvider that provides the given NodeInfo.
// This can be used by custom Action.Node() implementations to attach metadata.
func UseNodeInfo(info *NodeInfo) bt.ValueProvider {
	if info == nil {
		return nil
	}
	return nodeInfoProvider{info}
}

type nodeInfoProvider struct {
	info *NodeInfo
}

func (p nodeInfoProvider) Value(key any) (any, bool) {
	if _, ok := key.(nodeInfoKey); ok {
		return p.info, true
	}
	return nil, false
}

// GetNodeType retrieves the NodeType from a bt.Valuer, or NodeTypeUnknown.
func GetNodeType(v bt.Valuer) NodeType {
	if info := GetNodeInfo(v); info != nil {
		return info.Type
	}
	return NodeTypeUnknown
}

// GetPreconditionKey retrieves the precondition's variable key, if present.
func GetPreconditionKey(v bt.Valuer) (key any, ok bool) {
	if info := GetNodeInfo(v); info != nil && info.VariableKey != nil {
		return info.VariableKey, true
	}
	return nil, false
}

// GetCondition retrieves the Condition from a precondition node, if present.
func GetCondition(v bt.Valuer) (Condition, bool) {
	if info := GetNodeInfo(v); info != nil && info.Condition != nil {
		return info.Condition, true
	}
	return nil, false
}

// GetEffects retrieves the Effects from an action node, if present.
func GetEffects(v bt.Valuer) (Effects, bool) {
	if info := GetNodeInfo(v); info != nil && len(info.Effects) > 0 {
		return info.Effects, true
	}
	return nil, false
}

// nodeValueProvider is a type alias that makes *node[T] a bt.ValueProvider.
// Using a type conversion avoids extra allocations.
type nodeValueProvider[T Condition] node[T]

func (p *nodeValueProvider[T]) Value(key any) (any, bool) {
	v := (*node[T])(p).Value(key)
	return v, v != nil
}
