# Artifact 2: pabt Node Structure Analysis

## Internal Node Type: `node[T Condition]`

### Fields (from pabt.go lines 130-147)
```go
type node[T Condition] struct {
    // Contextual links (what type of node this is)
    goal          *goal[T]          // links to root goal (has State)
    ppa           *ppa[T]           // links to pre-post condition tree root
    action        *action[T]        // links to action tree
    preconditions *preconditions[T] // links to root of Conditions
    precondition  *precondition[T]  // links to leaf Condition

    // Leaf node content
    node bt.Node  // non-nil for leaf nodes

    // Group node content  
    tick bt.Tick  // set for all group nodes

    // Tree structure links
    parent, first, last, prev, next *node[T]
}
```

### Contextual Models
```go
goal[T] struct {
    root    *node[T]
    state   State[T]
    running *bool
    or      []*preconditions[T]
}

ppa[T] struct {
    root    *node[T]
    post    *node[T]
    actions []*action[T]
}

action[T] struct {
    root    *node[T]
    node    *node[T]  // the actual action node
    effects map[any]Effect
    or      []*preconditions[T]
}

preconditions[T] struct {
    root *node[T]
    and  map[any]*precondition[T]
}

precondition[T] struct {
    root      *node[T]
    condition T
    status    bt.Status
}
```

## bt.Node Creation Points in pabt

### 1. Plan.bt() - Root Node Factory (pabt.go:237-278)
```go
func (p *Plan[T]) bt() (bt.Tick, []bt.Node) {
    // Creates root node, returns tick + children
    // This IS the entry point for the entire tree
}
```
**Metadata Opportunity**: Attach plan-level metadata (goal info, plan identity)

### 2. node[T].bt() - Node to bt.Node Conversion (util.go:137-142)
```go
func (n *node[T]) bt() (node bt.Node) {
    node = n.node
    if node == nil {
        node = n.group
    }
    return
}
```
**Note**: Returns either leaf node or group factory function

### 3. node[T].group() - Group Node Factory (util.go:144-149)
```go
func (n *node[T]) group() (tick bt.Tick, children []bt.Node) {
    tick = n.tick
    for node := n.first; node != nil; node = node.next {
        children = append(children, node.bt())
    }
    return
}
```
**Metadata Opportunity**: Attach group-type info (goal/ppa/action/preconditions)

### 4. newConditionNode() - Condition Leaf Node (util.go:152-169)
```go
func newConditionNode[T Condition](
    state State[T],
    key any,
    match func(value any) bool,
    outcome *bt.Status,
) bt.Node {
    return bt.New(func([]bt.Node) (status bt.Status, err error) {
        // simple condition check logic
    })
}
```
**Metadata Opportunity**: Attach precondition identity (variable key, condition)

### 5. wrapActionNodeHandleSetRunning() - Action Node Wrapper (util.go:432-446)
```go
func wrapActionNodeHandleSetRunning(running *bool, actNode bt.Node) bt.Node {
    return func() (bt.Tick, []bt.Node) {
        tick, children := actNode()
        // wrap tick to set running flag
    }
}
```
**Metadata Opportunity**: Attach action identity, effects info

## Proposed Metadata Schema

### Node Types and Their Metadata

| Node Type | Contextual Field | Suggested Metadata |
|-----------|------------------|-------------------|
| Goal Root | `goal != nil && goal.root == n` | Goal conditions, state |
| PPA Root | `ppa != nil && ppa.root == n` | PPA structure info |
| PPA Post | `ppa != nil && ppa.post == n` | Original post-condition |
| Action Root | `action != nil && action.root == n` | Action effects, conditions |
| Action Node | `action != nil && action.node == n` | Action identity |
| Preconditions Root | `preconditions != nil && preconditions.root == n` | Conditions set |
| Precondition Leaf | `precondition != nil` | Variable key, condition |

### PPA Structure Specifics

The PPA (Pre-Post Conditions with Actions) tree has this structure:
```
Selector (PPA root)
├── post-condition subtree (the original precondition that was expanded)
└── [if len(actions) == 1:] action root
    [if len(actions) > 1:] Memorize(Selector)
    └── action1 root
    └── action2 root
    └── ...
```

Each action root:
```
Sequence
├── [conditions subtree if any]
└── action node (leaf from Action.Node())
```

## Critical Integration Points

### 1. node[T] should implement bt.Metadata
This requires:
- `Value(key any) any` method (bt.Valuer)
- `Children(yield func(Metadata) bool)` method

### 2. Value attachment during bt.Node creation
At each conversion point (bt(), group()), call:
```go
bt.UseValueProvider(nodeValueProvider{n})
```

### 3. Custom ValueProvider for structured access
Define keys/types for querying:
- Node type (goal/ppa/action/precondition)
- Condition details
- Action effects
- Structural relationships

### 4. Structure registration for optimal Walk behavior
Consider using `bt.UseStructure()` to provide logical children that avoid Node.Value lock overhead.
