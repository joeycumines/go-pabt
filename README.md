# go-pabt

An implementation of the planning algorithm described by chapter 7 of
https://www.researchgate.net/publication/319463746_Behavior_Trees_in_Robotics_and_AI_An_Introduction, in golang, built
using https://github.com/joeycumines/go-behaviortree

## Overview

The PA-BT planning algorithm uses a so-called "reachability graph", combined
with "continual hill-climbing", to incrementally find and execute a suitable
plan, to achieve a given goal, in disjunctive normal form (DNF). It is well
suited for composing disparate sets of "actions" which have interconnected
"effects" or "conditions", such as narrower, domain-specific algorithms.
In the "pick and place" example, PA-BT is used to unify path finding and object
manipulation.

This algorithm is relatively simple, does not support "optimal" planning
natively, though it may be guided towards a more optimal plan, through the use
of conditions. Conceptually, this is "constraint programming" as opposed to
"constrained optimization".

## Use cases

- Compose simpler "action templates" into more complex plans
- Perform tasks in changing environments
- Interoperability with any other behavior tree or compatible implementation

## Introspection API

go-pabt provides rich introspection facilities for inspecting the structure and
state of planning trees during execution. All introspection types live in the
main `pabt` package.

### Node Metadata

Each node in a plan tree carries metadata that identifies its role in the
planning algorithm. Use `GetNodeInfo` to retrieve the full metadata, or use the
convenience accessor functions to read individual fields.

```go
import (
    "fmt"
    bt "github.com/joeycumines/go-behaviortree"
    "github.com/joeycumines/go-pabt"
)

// Retrieve the full NodeInfo from any bt.Valuer
info := pabt.GetNodeInfo(node)
if info != nil {
    fmt.Println("Node type:", info.Type)
    fmt.Println("Effects:", info.Effects)
}

// Convenience accessors
nodeType := pabt.GetNodeType(node)
effects, ok := pabt.GetEffects(node)
cond, ok := pabt.GetCondition(node)
key, ok := pabt.GetPreconditionKey(node)
```

### NodeType

`NodeType` is an integer type with named constants that identify the role of a
node within the PPA (Precondition-Postcondition-Action) structure:

| Constant             | Description                                    |
|----------------------|------------------------------------------------|
| `NodeTypeUnknown`    | Not a pabt node                                |
| `NodeTypeGoalRoot`   | Root goal node                                 |
| `NodeTypeGoalSelector` | Goal OR-branch selector (multiple goals)     |
| `NodeTypePPARoot`    | Pre-post action selector                       |
| `NodeTypePPAPost`    | Post-condition (original precondition)         |
| `NodeTypeActionSelector` | Action OR-branch (multiple actions)         |
| `NodeTypeActionRoot` | Action sequence with conditions                |
| `NodeTypeActionNode` | Actual action leaf (from `Action.Node()`)      |
| `NodeTypePreconditionsRoot` | AND of preconditions                     |
| `NodeTypePreconditionLeaf` | Single precondition check                |

Each `NodeType` implements `String()` for human-readable output.

### Attaching Metadata to Custom Actions

The internal `UseNodeInfo` helper can be used by custom `Action` implementations
that construct their own node structure with value providers to attach `NodeInfo`
for introspection purposes. See the `NodeInfo` struct documentation for details.

## Tree Printing

The `Printer` variable is a pabt-aware `bt.Printer` that enriches the default
behavior tree tree output with node type labels, post-condition info, and
effects.

Assign it to `bt.DefaultPrinter` to get pabt-aware output everywhere:

```go
import (
    bt "github.com/joeycumines/go-behaviortree"
    "github.com/joeycumines/go-pabt"
)

bt.DefaultPrinter = pabt.Printer

// Now bt.Print() will show enriched output:
bt.Print(plan.Node())
```

Output format:

```
[frame1 frame2]  ActionNode | mypackage.myAction
[frame1 post:actor=s5]  PreconditionLeaf | mypackage.checkActor
[frame1 effects:actor=s5,loc=table]  ActionNode | mypackage.pickup
```

The `effects:` line is a live display of the action's effect rows and does show
their values. Two different strings are involved, and they are not the same one.

The name the planner derives for an action — `NodeInfo.Name`, and the name the
durable record stores as `StoredAction.Name` — names the *keys* the action
touches: `actor, loc`. It never carries the values, in a captured plan or an
uncaptured one. The reason is that a name is plain text: it passes through no
codec, no redaction walk, and no diagnostic sanitizer, so a value formatted into
one would be written into every record that mentions the node, into every digest
over those records, and into every export containing them, with nothing recorded
that would let a later reader tell it had been there. The values live in the
effect rows, where a value the application declared secret is redacted.

So the display above is safe to read and is not safe to store, and the two are
distinguished by which string you are looking at. See `effectName` in `util.go`
for the name and `formatEffects` in `printer.go` for the display.

## Node Status Tracking

`NodeStatus` tracks tick counts and the last status result for a node. Use
`GetNodeStatus` to retrieve the current status at any point during execution.

```go
status, ok := pabt.GetNodeStatus(node)
if ok {
    fmt.Printf("Status: %v (ticks: %d)\n", status.LastStatus, status.TickCount)
}
```

## Plan Path Extraction

`Path` represents the execution path from the plan root to the currently active
node. Each step in the path records the node type, any associated condition or
effects, and the post-condition that triggered the action expansion.

```go
path := pabt.GetPath(plan)
fmt.Println(path.String())
// Output: GoalRoot → PPARoot → ActionSelector → ActionRoot → ActionNode(actor=s5)

// Walk the path manually
for i, entry := range path {
    fmt.Printf("Step %d: %s\n", i, entry.NodeType)
    if len(entry.Effects) > 0 {
        for _, e := range entry.Effects {
            // Key is an any, not a string, so it needs %v rather than %s.
            fmt.Printf("  Effect: %v = %v\n", e.Key(), e.Value())
        }
    }
}
```

The parenthesised part of `String()` is the same live display the printer emits
above: an action reads as `key=value` pairs from its effect rows, and a
precondition or post-condition reads as the condition's own rendering. `String()`
is a display, not storage — nothing it produces reaches a record, a digest, or an
export.

`GetPath` walks the live plan, so it is an opt-in read. The durable server never
calls it: every route it serves answers from a committed record, so a browser
reading history cannot race a tick that is still expanding the tree.

## Labyrinth

ARIADNE is a networked first-person maze game where PA-BT planners visibly
drive every hunter. The browser page renders each hunter's live plan tree
while the game is being played — the BT explorer is part of the game, not a
separate tool.

All labyrinth code is behind `//go:build example`, so it is invisible to
`go build ./...` and `go test ./...`.

```
go run -tags example ./examples/labyrinth
```

Flags: `-addr` (default `127.0.0.1:0`) sets the listen address, `-seed`
(default random) sets the maze seed.
