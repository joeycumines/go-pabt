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
            fmt.Printf("  Effect: %s = %v\n", e.Key(), e.Value())
        }
    }
}
```

## Debug Server

The `pabtdebug` package provides a full HTTP debug server for live introspection
of PA-BT planning trees. It records tick events, exposes the plan tree over
HTTP, and streams real-time updates via Server-Sent Events (SSE).

### Setup

Create a `Tracker` and wrap your plan. Pass the tracker to `NewServer`:

```go
import "github.com/joeycumines/go-pabt/pabtdebug"

tracker := pabtdebug.NewTracker(plan)

// After each tick, record the event:
status, err := plan.Node().Tick()
tracker.Track(status, err)

// Start the debug server:
server := pabtdebug.NewServer(tracker, ":8080")
go server.Start()
defer server.Close()
```

### HTTP Endpoints

| Endpoint                                         | Description                           |
|--------------------------------------------------|---------------------------------------|
| `GET /debug/pabt/plans`                          | JSON array of plan info               |
| `GET /debug/pabt/plans/{id}`                     | Full tree structure in JSON           |
| `GET /debug/pabt/plans/{id}/events`              | SSE stream of tick events             |
| `GET /debug/pabt/ui`                             | Embedded web UI for tree visualization|

### Data Types

**TreeNode** — A node in the planning tree with its metadata:

```json
{
  "id": "0.1.2",
  "name": "pickup",
  "nodeType": "ActionNode",
  "status": { "TickCount": 5, "LastStatus": "Success" },
  "effects": [
    { "key": "actor", "value": "s5" },
    { "key": "loc", "value": "table" }
  ]
}
```

**TickEvent** — A snapshot captured after each tick:

```json
{
  "iteration": 42,
  "status": "Running",
  "tree": { ... },
  "timestamp": "2026-01-15T10:30:00Z"
}
```

**Hub** — Manages SSE client connections and broadcasts tick events to all
subscribers. Use `Tracker.Hub()` to access the hub directly for custom
integrations.

## Examples

### tcell-pick-and-place

![tcell-pick-and-place demo 1](https://imgur.com/W0NfhSY.gif "A demonstration of the example")
