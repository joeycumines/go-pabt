# Artifact 4: Detailed Implementation Plan

## Phase 1: Core Infrastructure

### 1.1 Define Key Types (new file: `metadata.go`)

```go
package pabt

// Key types for Value() queries
type (
    nodeInfoKey struct{}
)

// NodeType identifies the role of a node in the PPA structure
type NodeType int

const (
    NodeTypeUnknown NodeType = iota
    NodeTypeGoalRoot           // Root goal selector
    NodeTypePPARoot            // Pre-post action selector
    NodeTypePPAPost            // Post-condition (original precondition)
    NodeTypeActionRoot         // Action sequence with conditions
    NodeTypeActionNode         // Actual action leaf (from Action.Node())
    NodeTypePreconditionsRoot  // AND of preconditions
    NodeTypePreconditionLeaf   // Single precondition check
)

func (t NodeType) String() string { ... }
```

### 1.2 Define NodeInfo (exported metadata facade)

```go
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

    // children yields the logical children of this node.
    children func(yield func(bt.Metadata) bool)
}

// Value implements bt.Valuer. Returns nil for all keys (metadata is in fields).
func (i *NodeInfo) Value(key any) any { return nil }

// Children implements bt.Metadata.
func (i *NodeInfo) Children(yield func(bt.Metadata) bool) {
    if i.children != nil {
        i.children(yield)
    }
}
```

### 1.3 Implement node[T].Value()

```go
func (n *node[T]) Value(key any) any {
    switch key.(type) {
    case nodeInfoKey:
        return n.buildNodeInfo()
    }
    return nil
}

func (n *node[T]) buildNodeInfo() *NodeInfo {
    info := &NodeInfo{
        Type: n.nodeType(),
    }
    
    // Add precondition info
    if n.precondition != nil && n.precondition.root == n {
        info.VariableKey = n.precondition.condition.Key()
        info.Condition = n.precondition.condition
    }
    
    // Add action info
    if n.action != nil && n.action.node == n {
        info.Effects = n.action.buildEffects()
    }
    
    // Set children function
    info.children = func(yield func(bt.Metadata) bool) {
        for c := n.first; c != nil; c = c.next {
            if !yield(c.buildNodeInfo()) {
                return
            }
        }
    }
    
    return info
}

func (n *node[T]) nodeType() NodeType {
    switch {
    case n.goal != nil && n.goal.root == n:
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
        return NodeTypeUnknown
    }
}
```

### 1.4 Define nodeValueProvider

```go
// nodeValueProvider is a type alias that makes node[T] a bt.ValueProvider.
// Using a type alias avoids allocations.
type nodeValueProvider[T Condition] node[T]

func (p *nodeValueProvider[T]) Value(key any) (any, bool) {
    v := (*node[T])(p).Value(key)
    return v, v != nil
}
```

---

## Phase 2: Integration Points

### 2.1 Modify node[T].bt() (util.go)

```go
func (n *node[T]) bt() (node bt.Node) {
    node = n.node
    if node == nil {
        node = n.group
    } else {
        // Wrap leaf node to register ValueProvider
        orig := node
        node = func() (bt.Tick, []bt.Node) {
            bt.UseValueProvider((*nodeValueProvider[T])(n))
            return orig()
        }
    }
    return
}
```

### 2.2 Modify node[T].group() (util.go)

```go
func (n *node[T]) group() (tick bt.Tick, children []bt.Node) {
    bt.UseValueProvider((*nodeValueProvider[T])(n))
    tick = n.tick
    for node := n.first; node != nil; node = node.next {
        children = append(children, node.bt())
    }
    return
}
```

### 2.3 Modify newConditionNode() (util.go)

The `newConditionNode` function creates a bt.Node directly. We need to:
- Either pass in the `*node[T]` to register its ValueProvider
- Or create a wrapper

Since `newConditionNode` assigns to `node.node`, we can simply let `node[T].bt()` handle it.

**No changes needed** - the returned bt.Node is stored in `n.node` and wrapped by `bt()`.

### 2.4 Modify wrapActionNodeHandleSetRunning() (util.go)

This wraps the action node. The ValueProvider registration should happen in parent's `bt()` call.

**No changes needed** - handled by `node[T].bt()`.

### 2.5 Modify Plan[T].bt() (pabt.go)

```go
func (p *Plan[T]) bt() (bt.Tick, []bt.Node) {
    if p.root == nil {
        if err := p.init(); err != nil {
            return func(children []bt.Node) (bt.Status, error) { return bt.Failure, err }, nil
        }
    }
    var (
        node           = p.root
        tick, children = node.bt()()
    )
    // Register root node's ValueProvider
    bt.UseValueProvider((*nodeValueProvider[T])(p.root))
    
    return func(children []bt.Node) (status bt.Status, err error) {
        // ... existing logic
    }, children
}
```

---

## Phase 3: Exported API

### 3.1 GetNodeInfo Function

```go
// GetNodeInfo retrieves the NodeInfo from a bt.Valuer, or nil if not present.
// This is the primary API for accessing pabt node metadata.
func GetNodeInfo(v bt.Valuer) *NodeInfo {
    if info := v.Value(nodeInfoKey{}); info != nil {
        return info.(*NodeInfo)
    }
    return nil
}
```

### 3.2 UseNodeInfo Function (for custom nodes)

```go
// UseNodeInfo returns a bt.ValueProvider that provides the given NodeInfo.
// This can be used by custom Action.Node() implementations to attach metadata.
func UseNodeInfo(info *NodeInfo) bt.ValueProvider {
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
```

---

## Phase 4: Additional Helpers

### 4.1 Convenience Getters

```go
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
```

---

## Phase 5: Tests

### 5.1 Unit Tests for NodeInfo

```go
func TestNodeInfo_Value(t *testing.T) { ... }
func TestNodeInfo_Children(t *testing.T) { ... }
```

### 5.2 Unit Tests for GetNodeInfo

```go
func TestGetNodeInfo_GoalRoot(t *testing.T) { ... }
func TestGetNodeInfo_PPARoot(t *testing.T) { ... }
func TestGetNodeInfo_ActionNode(t *testing.T) { ... }
func TestGetNodeInfo_PreconditionLeaf(t *testing.T) { ... }
```

### 5.3 Integration Tests with bt.Walk

```go
func TestWalk_PPATree(t *testing.T) {
    // Create a plan
    // Trigger expansion
    // Walk the tree and verify metadata at each node
}
```

### 5.4 Tests for Custom Action Nodes

```go
func TestUseNodeInfo_CustomAction(t *testing.T) {
    // Action.Node() uses UseNodeInfo
    // Verify it's accessible via bt.Walk
}
```

---

## Phase 6: Documentation

### 6.1 Package Documentation
- Add section on metadata support
- Document NodeInfo struct
- Document Get* functions

### 6.2 Examples
- Example using bt.Walk with pabt trees
- Example attaching custom metadata to Action.Node()

---

## File Changes Summary

| File | Changes |
|------|---------|
| `metadata.go` (NEW) | NodeType, NodeInfo, nodeInfoKey, nodeValueProvider, GetNodeInfo, UseNodeInfo, convenience getters |
| `util.go` | Add `Value()` to node[T], modify `bt()` and `group()` to register ValueProvider |
| `pabt.go` | Modify `Plan.bt()` to register root ValueProvider |
| `metadata_test.go` (NEW) | All metadata-related tests |
| `util_test.go` | Additional tests for modified methods |

---

## Validation Checklist

- [ ] `go build ./...` succeeds
- [ ] `go test -race ./...` passes
- [ ] `go vet ./...` clean
- [ ] `staticcheck ./...` clean (if available)
- [ ] `go test -cover` shows adequate coverage
- [ ] bt.Walk properly traverses pabt trees
- [ ] GetNodeInfo returns correct info for all node types
- [ ] No allocations in hot paths (ValueProvider registration)
- [ ] Documentation is complete
