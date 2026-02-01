# Artifact 3: Design Variant Analysis

## Design Goals

1. **bt.Metadata Implementation**: Allow `bt.Walk` to traverse pabt trees
2. **bt.ValueAttachable Implementation**: Allow users to attach custom metadata
3. **Structured Metadata Exposure**: Expose PPA structure via queryable APIs
4. **Memory Efficiency**: Minimal allocations, no mutex overhead
5. **Clean API**: Well-conceived helpers matching bt.Get*/Use* patterns

## Variant A: Keep `node[T]` Unexported, Use ValueProviders Only

### Approach
- Don't modify `node[T]` at all
- At each bt.Node creation point, call `bt.UseValueProvider()` with structured metadata
- Define exported Get* functions and unexported value provider types

### Implementation
```go
// Unexported key types
type (
    nodeTypeKey     struct{}
    preconditionKey struct{}
    actionKey       struct{}
    ppaKey          struct{}
)

// NodeType enum
type NodeType int
const (
    NodeTypeGoal NodeType = iota + 1
    NodeTypePPA
    NodeTypeAction
    NodeTypePrecondition
)

// Exported getters
func GetNodeType(v bt.Valuer) NodeType { ... }
func GetPrecondition(v bt.Valuer) (key any, condition Condition, ok bool) { ... }
func GetActionEffects(v bt.Valuer) (map[any]Effect, bool) { ... }

// At creation points:
func (n *node[T]) group() (tick bt.Tick, children []bt.Node) {
    bt.UseValueProvider(n.valueProvider())
    ...
}
```

### Pros
- Minimal changes to existing code
- Clean separation of internal representation and exposed API
- No need to export any internal types

### Cons
- Must ensure every bt.Node creation point calls UseValueProvider
- node[T] doesn't implement bt.Metadata directly (but bt.Node does)
- Potential for missed registration points

---

## Variant B: `node[T]` Implements bt.Metadata and bt.ValueAttachable

### Approach
- Add internal `values map[any]any` field to node[T]
- Implement `Value(key any) any` and `Children(yield func(bt.Metadata) bool)`
- Implement `WithValue(key, value any) *node[T]`
- At bt.Node creation, use bt.UseValueProvider with node as provider

### Implementation
```go
type node[T Condition] struct {
    // ... existing fields ...
    values map[any]any  // nil by default, initialized on first WithValue
}

func (n *node[T]) Value(key any) any {
    if n.values != nil {
        if v, ok := n.values[key]; ok {
            return v
        }
    }
    // Check contextual metadata
    return n.contextualValue(key)
}

func (n *node[T]) Children(yield func(bt.Metadata) bool) {
    for c := n.first; c != nil; c = c.next {
        if !yield(c) {
            return
        }
    }
}

func (n *node[T]) WithValue(key, value any) *node[T] {
    if n.values == nil {
        n.values = make(map[any]any)
    }
    n.values[key] = value
    return n
}
```

### Pros
- node[T] is a first-class Metadata implementer
- Users could theoretically attach values to node[T] directly
- Natural fit with bt.Walk

### Cons
- node[T] is unexported - can't be used with bt.With* helpers externally
- Mixing internal tree mutation with metadata attachment is awkward
- WithValue mutates in place - doesn't match bt.Node's copy semantics

---

## Variant C: Separate Exported Metadata Type (RECOMMENDED)

### Approach
- Define new exported type `type NodeInfo struct { ... }` that implements bt.Metadata
- At bt.Node creation, attach NodeInfo via UseValueProvider
- NodeInfo provides structured access to PPA metadata
- External users can query via GetNodeInfo(bt.Valuer)

### Implementation
```go
// Exported metadata type
type NodeInfo struct {
    Type          NodeType
    VariableKey   any        // for preconditions
    Condition     Condition  // for preconditions
    Effects       Effects    // for actions
    Children      func(yield func(bt.Metadata) bool)  // logical children
    // ... other relevant fields
}

// NodeInfo implements bt.Metadata
func (i *NodeInfo) Value(key any) any { ... }
func (i *NodeInfo) Children(yield func(bt.Metadata) bool) { i.Children(yield) }

// Registration at creation points
func (n *node[T]) valueProvider() bt.ValueProvider {
    return &nodeInfoProvider{n}
}

// Unexported provider
type nodeInfoProvider[T Condition] struct { n *node[T] }
func (p *nodeInfoProvider[T]) Value(key any) (any, bool) {
    if _, ok := key.(nodeInfoKey); ok {
        return p.n.buildNodeInfo(), true
    }
    return nil, false
}

// Exported getter
func GetNodeInfo(v bt.Valuer) *NodeInfo {
    if info := v.Value(nodeInfoKey{}); info != nil {
        return info.(*NodeInfo)
    }
    return nil
}
```

### Pros
- Clean exported API
- NodeInfo is a proper bt.Metadata implementation
- Clear separation between internal tree and exposed metadata
- Users get structured, typed access

### Cons
- Additional type to maintain
- buildNodeInfo() called on each Value query (could cache)

---

## Variant D: Hybrid - node[T] + Lightweight ValueProvider Delegation

### Approach
- node[T] implements a minimal Value() that delegates to registered ValueProviders
- At each bt.Node creation point, register the node's provider
- Exported helpers retrieve strongly-typed info

### Implementation
```go
// node gains a Value method for internal consistency
func (n *node[T]) Value(key any) any {
    switch key.(type) {
    case nodeTypeKey:
        return n.nodeType()
    case preconditionKey:
        if n.precondition != nil {
            return preconditionInfo{...}
        }
    case actionInfoKey:
        if n.action != nil && n.action.node == n {
            return actionInfo{...}
        }
    }
    return nil
}

// ValueProvider that wraps node
type nodeValueProvider[T Condition] node[T]
func (p *nodeValueProvider[T]) Value(key any) (any, bool) {
    n := (*node[T])(p)
    v := n.Value(key)
    return v, v != nil
}

// At bt.Node creation
func (n *node[T]) bt() bt.Node {
    if n.node != nil {
        return func() (bt.Tick, []bt.Node) {
            bt.UseValueProvider((*nodeValueProvider[T])(n))
            return n.node()
        }
    }
    return n.group
}

func (n *node[T]) group() (tick bt.Tick, children []bt.Node) {
    bt.UseValueProvider((*nodeValueProvider[T])(n))
    tick = n.tick
    for node := n.first; node != nil; node = node.next {
        children = append(children, node.bt())
    }
    return
}
```

### Pros
- node[T] has a natural Value() method
- Type alias for ValueProvider is zero-allocation
- Clean delegation pattern
- Matches bt package's own internal patterns

### Cons
- Type conversion looks odd
- node[T] still unexported

---

## Recommendation: Variant D with NodeInfo Facade

Combine the efficiency of Variant D with the API clarity of Variant C:

1. **`node[T]` gets a `Value(key any) any` method** for internal use
2. **`nodeValueProvider[T]` type alias** makes it a ValueProvider  
3. **Register at all bt.Node creation points**
4. **Define `NodeInfo` struct** as the exported value type for the primary key
5. **Define `GetNodeInfo(bt.Valuer) *NodeInfo`** as the primary API
6. **Define individual `GetNodeType`, `GetPrecondition`, etc.** as convenience

This gives:
- Zero-allocation ValueProvider (type alias on node)
- Clean exported API (NodeInfo struct + Get* functions)
- Proper bt.Metadata semantics (NodeInfo implements it)
- Matches bt.Get*/Use* patterns from the bt package

## Next Steps

1. Define the complete NodeInfo schema
2. Define all key types
3. Implement node[T].Value()
4. Implement nodeValueProvider
5. Modify all bt.Node creation points
6. Add comprehensive tests
7. Update documentation
