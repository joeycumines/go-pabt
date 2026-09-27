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

## Debug Server

The `pabtdebug` package serves a plan's recorded history over HTTP and to a
browser. It records every tick into a store on disk, so the history survives a
restart, is reconstructable, and can be exported and imported as a verified
bundle. No route on it reads a live plan.

### Setup

```go
ctx := context.Background()

store, err := pabtdebug.OpenSQLiteStore(ctx, pabtdebug.SQLiteStoreConfig{
    Path: "run.db",
})
if err != nil {
    return err
}
defer store.Close()

identity, err := store.Bootstrap(ctx, pabtdebug.BootstrapRequest{
    PlanID: "my-plan",
    RunID:  "my-run",
    Title:  "My run",
})
if err != nil {
    return err
}

// A captured planner is what the session drives. NewCaptured is opt-in; a plan
// built with New behaves exactly as it always did. The goal is a slice of
// condition groups, not a flat list: each element is the set of conditions that
// must all hold for one goal.
planner, err := pabt.NewCaptured[MyCondition](state, goal)
if err != nil {
    return err
}

server, err := pabtdebug.NewDurableServer(store, pabtdebug.DurableServerConfig{
    Addr: "127.0.0.1:0", // empty port asks the kernel for one
})
if err != nil {
    return err
}
defer server.Close()

session, err := pabtdebug.OpenSession(ctx, store, planner, pabtdebug.SessionConfig{
    Identity: identity,
    Announce: server.Announce,
})
if err != nil {
    return err
}

// The capability is minted by the server and is written to no file and no log.
// Printing it is the application's job; that is how an operator obtains it.
fmt.Println("debugger at", server.Addr())
fmt.Println("capability", server.Capability())

go server.Start()

// Each tick goes through the session, which orders intent, execution, bundle,
// and commit. Every path out of Tick either commits exactly one terminal record
// or returns an error with the sequence released, so a tick is never half
// recorded and a sequence is never used without a record.
result := session.Tick(ctx)
if result.Err != nil {
    return result.Err
}
```

The listener is created by `NewDurableServer` rather than by `Start`, so the
address is known before anything is served. It binds loopback only, and a
hostname is refused rather than resolved. This is a trusted-local debugger: the
loopback listener is the first boundary and the capability is the second.

### Durable endpoints

Every route under `/debug/pabt/` requires the `X-PABT-Capability` header. The
token is read from the header so it never reaches a URL, an access log, a
referrer, or a shell history. The one exception is the client shell at
`/debug/pabt/ui`, which carries no record and no token; see
[Browser client](#browser-client).

| Endpoint | Description |
|---|---|
| `GET /debug/pabt/state` | Store identity, generation, recovery, the active run and its head. A client bootstraps from this. |
| `GET /debug/pabt/records?after=N` | A bounded page of whole records, plus a cursor for the position it leaves the client at. |
| `GET /debug/pabt/records/{n}` | One whole record. |
| `GET /debug/pabt/trees/{n}` | The tree that record committed. |
| `GET /debug/pabt/representations/{n}` | That tree plus the delta that produced it and a named base. |
| `GET /debug/pabt/stream?cursor=` | SSE. A `behind` event ends the connection with a cursor to resume from. |
| `GET /debug/pabt/cursor?after=N` | Mints a cursor, so a client never has to construct one. |
| `GET /debug/pabt/export` | The run's history as a bundle, media type `application/vnd.pabtdebug.bundle`. |
| `POST /debug/pabt/import` | Stages a bundle and activates it as an independently rewritten session. |

The export's digest is in the `X-PABT-Bundle-SHA256` response header. A bundle
is unkeyed, so every digest in it is recomputable for free: what it proves is
damage, truncation, reordering, and frame exchange, never authorship.

The routes are registered in `durableserver.go`; export and import are in
`durabletransfer.go` and the stream in `durablestream.go`.

### Browser client

`GET /debug/pabt/ui` serves the client — `index.html`, `app.css`, and eight ES
modules, listed in `clientAssets` in `pabtdebug/uiclient.go`. A request cannot
name a file that is not in that map.

The shell is served **outside** the guarded subtree, and that is deliberate: it
carries no record and no token, and keeping it out is what leaves "every route
requires the capability" true as a property. The consequence is that the shell is
reachable by anything that can reach the port, and the token is what keeps the
history private. The page asks for the capability and keeps it in
`sessionStorage`, which dies with the tab.

The client reads history, not a live plan. Selecting an occurrence shows its
preconditions with the four evidence fields — expected, current, observed, and
uncertainty — each labelled separately. An expected value the application did not
declare renders as unavailable, with the record's own reason; it is never inferred
from the condition's key.

## Examples

### tcell-pick-and-place

![tcell-pick-and-place demo 1](https://imgur.com/W0NfhSY.gif "A demonstration of the example")

```
go run -tags example ./examples/tcell-pick-and-place
```

### harbor-panic

A load fixture rather than a product demo: four carriers, six containers, four
goals, a human forklift that steals them, and a storm that relocates the goals
every 120 ticks, which invalidates every plan the planner had settled on.

Both examples are behind `//go:build example`, so they are invisible to
`go build ./...` and to `go test ./...`, and they need the tag to run or to
test:

```
go test -tags example ./examples/harbor-panic/... -count=1
go vet -tags example ./examples/harbor-panic/...
```

Harbor loads the planner and the simulator; it does not serve or attach to the
debug server. Read its README as fixture context.

## Research input

`docs/research/` holds audits of behaviour-tree planning, partial observability,
and pipeline linearization. They are **constraint input**: the reasoning a design
was checked against, not evidence that anything in this repository behaves a
particular way. Nothing in `docs/research/` is a specification this code
implements, and a claim about what the code does has to be checked against the
code.

`docs/rearch.md` is the design input for the earlier in-memory debug server, which
the durable server replaced. It is history, not a description of this code.
