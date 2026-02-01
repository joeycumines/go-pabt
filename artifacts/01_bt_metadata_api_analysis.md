# Artifact 1: bt.Metadata API Analysis

## Overview

The `github.com/joeycumines/go-behaviortree` package (v1.11.0) provides a comprehensive metadata system for behavior tree introspection and value attachment.

## Core Interfaces

### bt.Metadata
```go
type Metadata interface {
    Valuer
    Children(yield func(Metadata) bool)
}
```
- Represents the "conceptual" structure of a behavior tree
- Extends `Valuer` for key-value access
- `Children` yields logical children for tree traversal
- Note: `bt.Node` already implements this interface

### bt.Valuer
```go
type Valuer interface {
    Value(key any) any
}
```
- Returns value for given key, or nil
- Similar semantics to `context.Context.Value()`

### bt.ValueAttachable[T]
```go
type ValueAttachable[T any] interface {
    WithValue(key, value any) T
}
```
- Generic interface for types supporting key-value attachment
- `bt.Node` implements `ValueAttachable[Node]`

### bt.ValueProvider
```go
type ValueProvider interface {
    Value(key any) (any, bool)
}
```
- Returns `(value, true)` if key is handled, `(nil, false)` otherwise
- Used with `bt.UseValueProvider()` during node expansion

## Value Registration Mechanism

### bt.UseValueProvider(provider ValueProvider)
- Called during node expansion (inside a `func() (bt.Tick, []bt.Node)`)
- Registers a provider that responds to `Node.Value()` queries for that node
- Outermost provider that responds `true` takes precedence
- Cost is trivial when no `Node.Value()` call is active
- Uses call stack inspection for scoping

### bt.UseValueHandler(fn func(key any) (any, bool))
- Convenience wrapper that accepts a function

### bt.UseValueProviders(providers ...ValueProvider)
- Convenience wrapper for multiple providers

## Standard Keys and Helpers

### Frame (caller information)
- `bt.WithFrame[T](n ValueAttachable[T], frame *Frame) T`
- `bt.GetFrame(n Valuer) *Frame`
- `bt.UseFrame(frame *Frame) ValueProvider`

### Name (node naming)
- `bt.WithName[T](n ValueAttachable[T], name string) T`
- `bt.GetName(n Valuer) string`
- `bt.UseName(name string) ValueProvider`

### Structure (logical children)
- `bt.WithStructure[T](n ValueAttachable[T], children iter.Seq[Metadata]) T`
- `bt.GetStructure(n Valuer) iter.Seq[Metadata]`
- `bt.UseStructure(children iter.Seq[Metadata]) ValueProvider`

## bt.Walk Function
```go
func Walk(n Metadata, fn func(n Metadata) bool)
```
- Depth-first traversal of conceptual tree structure
- Prefers `n.Structure()` (logical) over physical node expansion
- Useful for tree inspection without triggering node expansion

## Implementation Patterns

### Pattern 1: Attaching via UseValueProvider (during expansion)
```go
func customNode(logic bt.Tick) bt.Node {
    return func() (bt.Tick, []bt.Node) {
        bt.UseValueProvider(bt.UseName("MyNode"))
        bt.UseValueProvider(bt.UseFrame(&myFrame))
        return logic, nil
    }
}
```

### Pattern 2: Wrapping with With* methods
```go
node := bt.New(myTick).WithName("MyNode").WithFrame(&myFrame)
```

### Pattern 3: Custom ValueProvider type
```go
type myValueProvider struct { /* fields */ }
func (p *myValueProvider) Value(key any) (any, bool) {
    switch key.(type) {
    case myKeyType:
        return p.computeValue(), true
    }
    return nil, false
}
```

## Key Observations for pabt Integration

1. **Node already implements Metadata**: Any `bt.Node` can be used with `bt.Walk`
2. **UseValueProvider is the extension point**: Called during node expansion to register handlers
3. **Efficient unexported types**: `UseName`, `UseFrame`, `UseStructure` return specialized types
4. **No mutex needed in internal map**: Values are attached during expansion, read during Value() queries
5. **Structure overrides physical children**: `WithStructure` lets conceptual tree differ from implementation:
   - Useful for pabt's internal node graph vs exposed tree structure
