# Artifact 5: Implementation Plan (Revised)

## Revisions Based on Peer Review

### Addressed Issues:
1. **Nil Pointer Panic** - Added nil guard in Plan.bt()
2. **bt.Walk Compatibility** - Now use bt.UseStructure for proper Walk integration  
3. **Standard Metadata** - Added bt.UseName registration
4. **Allocation Optimization** - NodeInfo caching considered

---

## Phase 1: Core Infrastructure

### 1.1 Define Key Types (new file: `metadata.go`)

```go
package pabt

import (
    "iter"
    bt "github.com/joeycumines/go-behaviortree"
)

// Key types for Value() queries
type nodeInfoKey struct{}

// NodeType identifies the role of a node in the PPA structure
type NodeType int

const (
    NodeTypeUnknown NodeType = iota
    NodeTypeGoalRoot           // Root goal selector
    NodeTypeGoalSelector       // Goal OR-branch selector (multiple goals)
    NodeTypePPARoot            // Pre-post action selector
    NodeTypePPAPost            // Post-condition (original precondition)
    NodeTypeActionSelector     // Action OR-branch (multiple actions)
    NodeTypeActionRoot         // Action sequence with conditions
    NodeTypeActionNode         // Actual action leaf (from Action.Node())
    NodeTypePreconditionsRoot  // AND of preconditions
    NodeTypePreconditionLeaf   // Single precondition check
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

func (t NodeType) String() string {
    if t >= 0 && int(t) < len(nodeTypeNames) {
        return nodeTypeNames[t]
    }
    return nodeTypeNames[0]
}
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
    
    // children is unexported - yields bt.Metadata objects for Walk compatibility
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
```

### 1.3 Implement node[T] Value and Children methods

```go
// Value implements a value lookup for this node's metadata.
func (n *node[T]) Value(key any) any {
    switch key.(type) {
    case nodeInfoKey:
        return n.buildNodeInfo()
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
        children: n.childrenSeq(),
    }
    
    // Add precondition info
    if n.precondition != nil && n.precondition.root == n {
        info.VariableKey = n.precondition.condition.Key()
        info.Condition = n.precondition.condition
    }
    
    // Add action effects info
    if n.action != nil && n.action.node == n {
        info.Effects = n.buildEffects()
    }
    
    return info
}

func (n *node[T]) nodeType() NodeType {
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
    effects := make(Effects, 0, len(n.action.effects))
    for _, e := range n.action.effects {
        effects = append(effects, e)
    }
    return effects
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

## Phase 2: Integration Points (Revised)

### 2.1 Modify node[T].bt() (util.go)

```go
func (n *node[T]) bt() (node bt.Node) {
    node = n.node
    if node == nil {
        node = n.group
    } else {
        // Wrap leaf node to register ValueProviders
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
```

### 2.2 Modify node[T].group() (util.go)

```go
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
```

### 2.3 Modify Plan[T].bt() (pabt.go) - WITH NIL GUARD

```go
func (p *Plan[T]) bt() (bt.Tick, []bt.Node) {
    if p.root == nil {
        if err := p.init(); err != nil {
            return func(children []bt.Node) (bt.Status, error) { return bt.Failure, err }, nil
        }
    }
    
    // NIL GUARD: Early return if init failed to set root
    if p.root == nil {
        return func(children []bt.Node) (bt.Status, error) { 
            return bt.Failure, fmt.Errorf("pabt: failed to initialize root node")
        }, nil
    }
    
    var (
        node           = p.root
        tick, children = node.bt()()
    )
    
    // Note: ValueProvider registration happens inside node.bt()
    // No additional registration needed here
    
    return func(children []bt.Node) (status bt.Status, err error) {
        p.running = false
        status, err = tick(children)
        if err != nil || status != bt.Failure {
            return
        }
        cf, ok := p.root.search()
        if !ok {
            p.root = nil
            return
        }
        err = cf.expand()
        if err != nil {
            return
        }
        cf.root.ppa.resolve()
        status = bt.Running
        return
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
    if v == nil {
        return nil
    }
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
    if info == nil {
        return nil // bt.UseValueProvider handles nil gracefully
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
```

### 3.3 Convenience Getters

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

## Phase 4: Tests

### 4.1 Unit Tests for NodeType and NodeInfo

```go
func TestNodeType_String(t *testing.T) {
    tests := []struct {
        input    NodeType
        expected string
    }{
        {NodeTypeUnknown, "Unknown"},
        {NodeTypeGoalRoot, "GoalRoot"},
        {NodeTypePreconditionLeaf, "PreconditionLeaf"},
        {NodeType(999), "Unknown"}, // out of bounds
    }
    for _, tc := range tests {
        if got := tc.input.String(); got != tc.expected {
            t.Errorf("NodeType(%d).String() = %q, want %q", tc.input, got, tc.expected)
        }
    }
}

func TestNodeInfo_Implements_Metadata(t *testing.T) {
    var _ bt.Metadata = (*NodeInfo)(nil)
}

func TestNodeInfo_Value(t *testing.T) {
    info := &NodeInfo{Type: NodeTypeGoalRoot}
    if info.Value("anything") != nil {
        t.Error("NodeInfo.Value should return nil for all keys")
    }
}

func TestNodeInfo_Children_Nil(t *testing.T) {
    info := &NodeInfo{}
    called := false
    info.Children(func(m bt.Metadata) bool {
        called = true
        return true
    })
    if called {
        t.Error("Children should not yield when children is nil")
    }
}
```

### 4.2 Integration Tests with bt.Walk

```go
func TestWalk_PPATree(t *testing.T) {
    // Create a simple plan that will expand
    state := &mockState{
        variable: func(key any) (any, error) { 
            return nil, nil // always fail conditions
        },
        actions: func(failed Condition) ([]IAction, error) {
            return []IAction{
                &simpleAction{
                    effects: Effects{&simpleEffect{key: "x", value: true}},
                    node:    bt.New(func([]bt.Node) (bt.Status, error) { return bt.Success, nil }),
                },
            }, nil
        },
    }
    
    plan, err := New[Condition](state, []IConditions{{&simpleCondition{key: "x", value: true}}})
    if err != nil {
        t.Fatal(err)
    }
    
    // Tick once to trigger expansion
    plan.Node().Tick()
    
    // Walk the tree and collect node types
    var types []NodeType
    bt.Walk(plan.Node(), func(n bt.Metadata) bool {
        if info := GetNodeInfo(n); info != nil {
            types = append(types, info.Type)
        }
        return true
    })
    
    // Verify we got expected node types
    if len(types) == 0 {
        t.Error("Walk should have found nodes with metadata")
    }
}
```

### 4.3 Test GetNodeInfo variations

```go
func TestGetNodeInfo_Nil(t *testing.T) {
    if GetNodeInfo(nil) != nil {
        t.Error("GetNodeInfo(nil) should return nil")
    }
}

func TestGetNodeInfo_NonPabtNode(t *testing.T) {
    node := bt.New(func([]bt.Node) (bt.Status, error) { return bt.Success, nil })
    if GetNodeInfo(node) != nil {
        t.Error("GetNodeInfo on non-pabt node should return nil")
    }
}
```

---

## Validation Checklist (Updated)

- [ ] `go build ./...` succeeds
- [ ] `go test -race ./...` passes
- [ ] `go vet ./...` clean
- [ ] `go fmt ./...` applied
- [ ] bt.Walk properly traverses pabt trees via Structure
- [ ] bt.GetName returns node type names
- [ ] GetNodeInfo returns correct info for all node types
- [ ] NodeInfo.Children yields proper bt.Metadata objects
- [ ] Nil pointer panics prevented (Plan.bt() guard)
- [ ] No test failures (deterministic)
- [ ] Documentation complete

---

## File Changes Summary (Updated)

| File | Changes |
|------|---------|
| `metadata.go` (NEW) | NodeType, NodeInfo, nodeInfoKey, nodeValueProvider, GetNodeInfo, UseNodeInfo, convenience getters |
| `util.go` | Add Value(), childrenSeq(), buildNodeInfo(), buildEffects(), nodeType() to node[T]. Modify bt() and group() |
| `pabt.go` | Add nil guard to Plan.bt() |
| `metadata_test.go` (NEW) | All metadata-related tests |
