# Reefer Handoff — Design Record

The contract for the `examples/reefer-handoff` scenario. Written before any
implementation, so a cold-start engineer can build from it.

Sections 1–8 are requirements on code that does not exist yet. Section 9 is the
exception: it is a set of claims about two libraries that do exist, each one
checked against source or measured, and each labelled with how it was checked.
Nothing here asserts that any Reefer Handoff behaviour already works.

`examples/harbor-panic` is retired. Its README, storyboard, auto-tour, and
verification scripts are migration input and historical evidence, not
requirements. The reasoning in sections 3 and 5 is a direct response to specific
defects in that fixture, and those defects are named where they are corrected.

## 1. The physical system

The vessel *Kestrel Arrow* is alongside berth 4, loading. Her cutoff is round
**120**. After cutoff she sails, and any reefer still on the apron goes with her.

Three temperature-sensitive reefer containers were landed early and are sitting in
**quay staging** on the apron. A reefer holds its temperature only while it is
plugged into a powered reefer slot, so the work is to move all three from staging
into slots before cutoff.

Every route from staging to a slot passes through the **transfer bay**: one
covered bay with a gantry that is the only thing that can lift a box off a
tractor onto a slot feeder. The bay is a single node of **capacity one**, so every
delivery contends for it.

### Topology

Seven nodes. Seven lanes. Lanes are undirected; a tractor moves one lane per
`MoveTo`. A route is a breadth-first search in the order the table below declares
its lanes, which is what makes the apron-to-bay route go by the western approach
first. That ordering is load-bearing: the wind lockout closes the western
approach, and if the search preferred the eastern one the closure would change
nothing a plan was doing. A reefer destination is never a tractor destination, so
no route ends in a slot — a tractor hands a box over from the bay and does not
enter the slot itself. `sim/path.go` owns this table and nothing else may
hard-code a lane name.

| Node | Kind | Capacity |
|---|---|---|
| `STAGING` | where the three reefers start | unbounded |
| `WEST`, `EAST` | approach waypoints | 1 each |
| `BAY` | the transfer bay | **1** |
| `SLOT-1`, `SLOT-2`, `SLOT-3` | powered reefer slots | 1 each |

| Lane | Connects |
|---|---|
| `lane-west` | `STAGING` – `WEST` |
| `lane-east` | `STAGING` – `EAST` |
| `lane-bay-west` | `WEST` – `BAY` |
| `lane-bay-east` | `EAST` – `BAY` |
| `lane-slot-1` | `BAY` – `SLOT-1` |
| `lane-slot-2` | `BAY` – `SLOT-2` |
| `lane-slot-3` | `BAY` – `SLOT-3` |

Two approach lanes exist so that closing one forces a real re-route rather than
making the goal unreachable.

### Slots

A slot has a plug and a rating. `RatedMaxC` is the coldest temperature the unit can
hold, and a reefer is acceptable when `reefer.TempC >= slot.RatedMaxC` — a slot can
only ever cool cargo down to its own rating, so the older unit rated to −18 °C
cannot take a −22 °C reefer, while a modern unit rated to −30 °C takes anything on
the apron. A slot is powered or unpowered.

| Slot | Plug at round 0 | `RatedMaxC` |
|---|---|---|
| `SLOT-1` | powered | −30 °C |
| `SLOT-2` | powered | −18 °C |
| `SLOT-3` | powered | −30 °C |

### Cargo

| Reefer | `TempC` | Tag |
|---|---|---|
| `REEFER-1` | −22 °C (deep freeze) | readable |
| `REEFER-2` | −8 °C (chilled) | **unreadable** |
| `REEFER-3` | −8 °C (chilled) | readable |

Acceptance is `slot.Plug == powered && reefer.TempC >= slot.RatedMaxC`. So
`REEFER-1` fits only `SLOT-1` and `SLOT-3`, and that exclusion is a static
planning consequence the TUI and the debugger can both show. `REEFER-2` and
`REEFER-3` fit any slot while it is powered.

## 2. Actors

| Actor | Kind | Authority |
|---|---|---|
| `TUG-1`, `TUG-2`, `TUG-3` | autonomous yard tractors | one independent PA-BT plan runner each |
| wind controller | exogenous | emits `LaneLocked` and `LaneReopened` |
| slot power desk | exogenous | emits `SlotPowerChanged` |
| vessel cutoff | exogenous | emits `CutoffReached` at round 120 |

The exogenous actors never touch state. They emit events into the world, and the
reducer decides what they mean. The tractors never touch state either: they send
commands and read receipts. **Nothing mutates the world except the loop owner
running the reducer.**

Each tractor has its own plan, its own tick loop, and its own generation counter.
No two plans share a tick, a tree, or a lifecycle.

## 3. The incident

A fixed script, four events, each with a distinct reason to force replanning.

1. **Wind lockout.** The wind controller closes `lane-bay-west` at round 20 and
   reopens it at round 55. Any `MoveTo` over the closed lane is refused, which
   fails that step's post-condition, which makes the planner re-expand and build a
   chain through `lane-bay-east` instead. The reopen invalidates that replacement
   route in turn. Replanning is therefore not a one-shot repair, and the scenario
   proves it by making a replan itself go stale. There is no cached route object
   holding a version; the invalidation is entirely the refusal of a `MoveTo`.
2. **RFID uncertainty.** `REEFER-2`'s tag does not read, so the state of
   `TagConfirmed(REEFER-2)` is *unconfirmed* rather than confirmed. The variable
   is three-valued while `Condition.Match` is boolean, so an unconfirmed tag
   simply does not match, and the interesting part is what that forces: the only
   action whose effect sets the variable true is `ScanTag`, so the planner cannot
   route around the gap and has to grow the scan into the tree. Only after the
   scan does `PickUp` become admissible.
3. **Transfer-bay contention.** Two tractors issue `MoveTo(BAY)` in the same
   round. The first commit wins; the second is refused with reason `BayOccupied`
   and the receipt names the current holder. The loser re-expands and re-approaches.
4. **Power rejection.** The power desk unpowers `SLOT-2`'s plug at round 40. Any
   `Place` reduced after that is refused with reason `SlotUnpowered`, and nothing
   commits. The plan re-expands and re-targets a slot that is both powered and
   rated for the reefer.

The `SlotUnderRated` reason is enforced by the same predicate as the acceptance
test and is reachable, but the script does not stage a separate under-rated
refusal — the static rating is handled at action-generation time, before a
placement is ever reduced. Nothing in the debugger or the TUI may claim a
staged rating rejection that the script does not produce.

## 4. Shared resources

| Resource | Contended by | Outcome of contention |
|---|---|---|
| transfer bay `BAY` | all three tractors | one winner; loser gets `BayOccupied` naming the holder |
| `SLOT-1` | tractors placing a reefer | one winner; loser gets `SlotOccupied` |
| `SLOT-2` | as above, until round 40 | after round 40 every placement is `SlotUnpowered` |
| `SLOT-3` | as above | one winner; loser gets `SlotOccupied` |
| the job queue | the three `ClaimJob` commands | each claim is awarded a distinct job in manifest order; the only refusal is a drained queue |

The bay and the slots are contended by a **single atomic command**, not by a
reservation followed by a commit. There is no lease, no hold duration, and no
release ordering to get wrong. Contention is decided when the command is reduced.

The job queue is not a loser-takes-all resource. With three jobs and three
tractors, every claim is awarded a job no one else holds, and the queue's only
refusal is that it has run dry. There is no `JobTaken` reason, because nothing in
this manifest can produce one.

## 5. Plan and job relationship

**Jobs are claimed, not assigned.** The world holds a manifest of three jobs,
`REEFER-1` through `REEFER-3`. An idle runner sends `ClaimJob`; the reducer awards
it to exactly one claimant, atomically. Which tractor gets which reefer is an
outcome of the world. The retired fixture computed its pairing arithmetically
from the actor index (`pairsPerActor := 2`, `startPair := actorIdx * pairsPerActor`),
which is why it read as a demo rather than a system.

**One plan generation owns one job, and its goal is one condition.** The goal is
a single `IConditions` of length one:

```go
IConditions{ sim.Delivered(reefer) }
```

`Delivered(reefer)` is a world fact: the reefer is in a slot whose plug is powered
and whose rating accepts it. The reducer computes it. Because the slice has length
one, `generateOr` takes its `case 1` branch and the root is a `NodeTypeGoalRoot`.

**The slot is chosen by the planner, not written into the goal.** A goal naming a
specific slot would be unsatisfiable: `SlotPowered` and `SlotRating` are
properties of a slot, and no action in the command set can change either, so a
plan aimed at an unpowered slot could never succeed and would have to be rebuilt
mid-job. Instead the alternatives live where they belong, in action generation.
`State.Actions` for the failed condition `Delivered(reefer)` returns **one
candidate `Place` action per acceptable slot**, each carrying the preconditions it
needs. Those candidates are genuinely alternative, and the planner admits an
action only when one of its effects satisfies the failed condition, so a candidate
for an unpowered or under-rated slot is never generated in the first place.

Picking among candidate slots is therefore ordinary planning, not a special
case, and a mid-flight refusal is handled by the same mechanism as any other
failed post-condition: re-expansion.

**A goal slice of length zero is forbidden.** `generateOr`'s `case 0` sets the
root to a bare `bt.Sequence` with no children and no preconditions, and `init()`
then reports `NodeTypeGoalRoot` because `len(or)` is not greater than one. Such a
plan reports `bt.Success` on its first tick having achieved nothing. Every
generation must be constructed with exactly one condition.

**Variables the plan holds as post-conditions**, all keyed per job so one plan
never reads another's state:

| Variable | Meaning |
|---|---|
| `Delivered(reefer)` | in a powered, correctly rated slot — the goal |
| `Holding(tractor, reefer)` | custody |
| `At(tractor, node)` | position |
| `TagConfirmed(reefer)` | confirmed / unconfirmed — a post-condition the scan sets |

**Variables read during action generation only**, never held as post-conditions:

| Variable | Used to decide |
|---|---|
| `TempC(reefer)` | which slots can accept this reefer |
| `SlotPowered(slot)` | which slots can accept anything |
| `SlotRating(slot)` | which slots can accept this reefer |

**Commands.** Five, and the set is closed:

| Command | Refused when |
|---|---|
| `ClaimJob(tractor)` | the tractor already holds a job, or the queue has run dry |
| `ScanTag(tractor, reefer)` | the tractor is not at the reefer, or the tag already reads |
| `PickUp(tractor, reefer)` | the tractor's hands are full, the reefer is already held, the tractor is not at the reefer, or the tag is unconfirmed |
| `MoveTo(tractor, node)` | the destination is a slot, no lane joins the two nodes, the lane is closed, or the node is already taken |
| `Place(tractor, reefer, slot)` | the tractor does not hold that reefer, the tractor is not at the bay, the slot is unpowered, the slot is under-rated, or the slot is occupied |

**The job-to-cargo binding is a planner convention, not a domain invariant.**
Nothing stops a tractor from picking up and placing a reefer it never claimed,
and doing so completes the job of whichever tractor did claim it and releases that
tractor. The closed command table above is what makes that so: there is no
job-match refusal, because the world has no opinion about which tractor ought to
be moving which box. Section 5's one-generation-per-job rule is what keeps a plan
from relying on it, and P4 must hold a plan to its leased job. This is recorded
here so the omission is a decision and not an oversight.

## 6. Frame-bound ticking

Each runner pins **one observation frame per root tick**, through the
`WithStateViews` option. The runner implements `pabt.TypedStateViewProvider` and
opens a `StateViewPreTick` view over an immutable snapshot labelled
`CoherenceSnapshot`, carrying the world sequence and revision it was taken at.
Every condition read and every action list for that tick comes from that one
frame, so a tick cannot see half of one world and half of another. Effects are
read from a separate `StateViewPostAction` view opened after the action returns.

The immutability is not free and not automatic. `sim.World` holds slices, so a
world value shares its backing array with every copy of it, and the reducer's
transitions write through those slices by index. Every mutation path therefore
clones before it writes, and `World.clone` is the only thing standing between a
caller's world and a reducer that changed it underneath them. Without that, a TUI
or a debugger record holding a world per published event would serve the newest
state for every earlier record, and section 8's "a reader sees the old record or
the new one" would be false in the way that matters.

The other two options are not optional extras. `WithExecutionObserver` is the only
source of an observed value — a value the planner merely declared is not an
observation. `WithTransitionVerifier` is the only source of a causal relation.
Without all three a plan labels its own reads `CoherenceLive` and cannot support an
authoritative claim, and every value and relation in the debugger must then render
as unavailable.

**Command and receipt shape.** A runner submits a command and later matches a
receipt to it by idempotency key, which is the command ID minted from the plan's
own identity, so a re-submission of the same action returns the original receipt
rather than applying twice.

```go
type Command struct {
    ID             CommandID            // "<planID>/<generation>/<actionSeq>"
    IdempotencyKey string               // == ID; the match key for the receipt
    Kind           CommandKind
    Tractor        TractorID
    Reefer         ReeferID             // where applicable
    Slot           SlotID               // where applicable
    Node           NodeID               // MoveTo target
    PlanID         string
    Generation     uint64
    ActionID       pabt.OccurrenceID
    PlanTickSeq    uint64
    Expect         Expectation
}

type Expectation struct {
    WorldSeq    uint64 // the worldSeq the pinned frame was taken at
    SlotVersion uint64 // the target slot's own version, for Place
    LaneVersion uint64 // the topology version, for MoveTo
    TagVersion  uint64 // the tag's own version, for PickUp and ScanTag
}

type Receipt struct {
    ID             CommandID
    IdempotencyKey string
    CommandSeq     uint64
    WorldSeq       uint64 // the worldSeq at which it was reduced
    WorldRevision  string
    Outcome        Outcome // Applied | Refused | Duplicate
    Reason         Reason  // typed and empty only when Applied
    Holder         string  // current holder of a contended resource, on refusal
}
```

`Outcome` has no stale or abandoned member, because a command that is not reduced
is not a receipt at all — see reconciliation below.

**Every receipt is kept, applied or refused.** A refusal is a fact a reader has to
be able to find later with no world effect attached, and a ledger holding only
successes could not show that. The consequence is that an idempotency key is
spent by its first reduction: a genuine retry after a replan is a new action
instance and mints a new key. A world sequence counts only committed mutations, so
a refusal is visible in the receipt ledger with the sequence it did not move.

**Fencing.** A receipt is discarded, and can neither mutate state nor report
success, when its `PlanID` or `Generation` is not the runner's current generation,
when its `ActionID` is not present in the current tree, or when any `Expect` field
no longer matches the world. A fenced receipt is counted and shown as fenced; it
is not silently dropped.

**Reconciliation.** An admitted command that is never reduced would leave a runner
waiting on a receipt that does not exist, and section 9 records a measured case
where that happens. So shutdown reconciliation publishes an explicit
`Abandoned(id)` for every admitted command with no receipt, and a runner holding an
in-flight command with no receipt at reconciliation enters its `Blocked` state and
names the command. It never waits forever.

**Progress and its limits.** A pabt tick returns a `bt.Status` — `bt.Success`,
`bt.Failure`, or `bt.Running` — plus a separate error. The runner's own states
(`Claiming`, `Driving`, `WaitingForReceipt`, `Blocked`, `Complete`, `Retired`) are
**runner states, not behaviour-tree statuses**, and must not be described as such.
The case that needs naming is `bt.Failure` with no failed unexpanded precondition
left: the tree is fully expanded, the goal is unreachable from the pinned frame
and the available actions, and the runner enters `Blocked` and reports which
condition it could not achieve. A tick error retires the generation and is
surfaced, not retried in place.

A runner coalesces missed ticks and permits one in-flight tick and one in-flight
action at a time. When a job completes, the runner retires that generation and
builds a new one for its next claim; a retired generation is never reused and never
ticked again.

## 7. Success and failure

Both are functions of the round counter, so both are testable.

**Success:** at or before round 120, all three jobs are delivered — every reefer
standing in a powered, correctly rated slot. The world enters `Complete`.

The condition is evaluated against the physical state on every change, not
against a latch recording that a job was once carried out. The power desk may
take a plug out of a slot that already holds a reefer, and a latch would leave
the run claiming a delivery it no longer has.

**Failure:** round 120 passes with any job undelivered. The world enters `Cutoff`
and `World.Shortfall` names the reefers it missed, in manifest order.

A refused command is neither success nor failure. It is a receipt with a reason,
and its whole effect is to make the next tick re-expand. The retired fixture had no
failure condition at all, which is why nothing in it could be shown to recover.

**Rounds and world sequences are different domains.** A round is a host tick and
is advanced only by `BeginRound`. A world sequence counts committed mutations
only. A round may therefore contain several mutations or none, a refused command
and a round boundary both leave the sequence alone, and entering a terminal phase
is a derivation of the state that already advanced rather than a mutation of its
own — so a phase event rides at the sequence already reached. Tying a round to a
world revision instead would make the cutoff a statement about how much the world
happened to churn, which is not what a vessel's cutoff is.

**Round pacing.** In deterministic mode the host calls `BeginRound` once per
round; in realtime mode it does so on a 200 ms timer, so cutoff is 24 s in.

## 8. Debugger proof

The trace a reviewer must be able to follow, end to end:

```
world event ──▶ plan tick ──▶ command ──▶ receipt ──▶ re-expansion
 (worldSeq)    (planTickSeq)  (commandSeq)  (outcome)   (cause)
```

Three sequence domains and one revision, kept distinct and never conflated:
`worldSeq` orders world events, `planTickSeq` orders one plan's ticks, and
`commandSeq` orders commands. The fourth item, `worldRevision`, is a **revision,
not a sequence**, and it is carried as a string because `pabt.StateRevision.WorldRevision`
is a string. `worldSeq` has no field in `StateRevision` and travels alongside it
inside the frame.

Each record names its cause explicitly:

- a **world event** carries the `worldSeq` and `worldRevision` it produced;
- a **plan tick** carries its plan ID, plan generation, plan revision, and the
  `worldSeq` its frame was pinned at;
- a **command** carries `commandID`, its idempotency key, the `Expectation` it was
  decided against, and the `planTickSeq` plus `ActionID` that caused it;
- a **receipt** carries `commandID` and its idempotency key, the outcome, the
  `worldSeq` it was reduced at, and, on refusal, the reason and the current holder;
- a **tree change** carries the receipt that caused it.

A refusal publishes no world event. It changed nothing, so it has a receipt and no
event, and that absence is precisely what tells a reader the difference between a
command that was turned down and a change that happened. Entering `Complete` or
`Cutoff` is a world event, because a reader following the run has to see the run
end rather than infer it from the ledger going quiet.

Readers never traverse a live plan. pabtdebug serves committed published records,
and a tree that was not captured is reported as absent rather than reconstructed.
A refused command is visible with no world effect. Concurrency cannot produce a
half-written record: a reader sees the old record or the new one.

Key/effect matching stays labelled as *model support*. Only a
`TransitionVerifier` may claim a relation, and only when all four hold: the
revisions bracketing the action were read from coherent views, the window bound
that action alone, the verifier returned no error, and the planner set
`Authoritative`. Anything else renders as a non-authoritative verdict, with the
reason. A shared variable key is not causation.

## 9. Dependency and API contract

### go-eventloop v0.1.0

API read with `go doc`, and the behaviours below measured with a throwaway module
against `v0.1.0` on `go1.27.1` on 2026-09-27.

| Call | Contract relied on |
|---|---|
| `eventloop.New(opts ...LoopOption) (*Loop, error)` | may fail; the error is handled, never discarded |
| `eventloop.WithAutoExit(false)` | the world does not decide its own liveness |
| `loop.Run(ctx) error` | must be running before a task's work is trusted to owner goroutine affinity |
| `loop.Submit(task func()) error` | admission only; it does not run the task inline. **Panics on a nil task** — a nil task is a programming error, not a runtime condition, and is not defended against |
| `loop.IsCallbackOwner() bool` | asserted inside callbacks that mutate the world |
| `loop.ScheduleTimer(delay, fn) (TimerID, error)` | one-shot; there is no exported repeating variant. Realtime pacing only |
| `loop.CancelTimer(id) error` | cancelling a timer that already fired returns `ErrTimerNotFound`, an error, not a nil no-op |
| `loop.Shutdown(ctx) error` | graceful stop; safe from a callback, where it returns nil |
| `loop.Close() error` | immediate stop; from a callback it returns `ErrReentrantClose` and the loop's state afterwards is `Terminated` |
| `loop.Done() <-chan struct{}` | the cleanup barrier |
| `loop.State() LoopState` | `StateAwake`, `StateRunning`, `StateSleeping`, `StateTerminating`, `StateTerminated` |

**Measured, and load-bearing for section 6's reconciliation.** A task admitted by
`Submit` before `Run` does **not** run on the callback owner. Measured two ways:
submitting and calling `Shutdown` without ever entering `Run` gives
`IsCallbackOwner() == false`, and submitting and *then* starting `Run` gives
`IsCallbackOwner() == false` as well. So `Run` must be started before admission,
and the owner assertion is a real check rather than a decoration.

**Measured, and the reason reconciliation exists.** A task admitted by `Submit`
and then met by `Close` before `Run` is **silently discarded**: the task never
runs and `Close` returns `nil`. An admitted command can therefore vanish with no
receipt and no error. Section 6's shutdown reconciliation exists for exactly this
case, and no code path may treat a nil error from `Close` as evidence that
admitted work completed.

There is **no virtual clock**. `ScheduleTimer` takes a `time.Duration` and the
loop measures real time, so deterministic mode cannot be built on the loop's
timers. The host drives rounds through `Submit` instead, and both modes share the
same reducer and the same command path.

### pabt

The capture and authority surface is `option.go` at the current revision: plan
construction is `pabt.New(state, goal []Conditions[T], opts ...IOption)`, and the
three options this scenario depends on are `WithStateViews`,
`WithExecutionObserver`, and `WithTransitionVerifier`. `pabt` is this repository's
own module at revision `4f0cd9b` on branch `add-tree-metadata-support`.

### Go version

`go-eventloop` v0.1.0 declares `go 1.27.0` in its own `go.mod`, so requiring it
raises this module's `go` directive. Verified by trial: a module declaring
`go 1.26.4` was rewritten to `go 1.27.0` by `go mod tidy` once the require was
added. This repository therefore moves from `go 1.26.4` to `go 1.27.0` when the
first source file imports `go-eventloop`. The installed toolchain is `go1.27.1`;
that is a separate fact from the directive and the two are not to be conflated.

The cost is real and named: consumers of this library must build with Go 1.27 or
newer. The alternative — a nested module for this example — was rejected because
every root `Makefile` target and every `go ./...` command would silently skip it,
which would leave the example outside the command-based verification this project
depends on. The directive is one line and reversible if that cost is judged worse.

## 10. Package boundary

Everything here is behind `//go:build example`, so `go build ./...` and
`go test ./...` at the root are untouched and the example needs the tag to build,
run, or test. `sim/` exists; the rest lands with the task that needs it.

```
examples/reefer-handoff/
  DESIGN.md        this record
  main.go          manifest, world loop, runners, TUI, debugger session
  sim/
    manifest.go    reefers, slots, tractors, jobs, nodes, the cutoff round
    events.go      world events, the five commands, receipts, reasons
    observation.go the agent-facing frame and the variable keys
    path.go        the lane table, lane locking, and routing
    world.go       the world value and its canonical renderings
    reducer.go     Reduce, its properties, and the terminal phases
    transitions.go the five atomic transitions
    incident.go    the exogenous changes and the scripted incident
    world_loop.go  go-eventloop owner, admission, command lifecycle
  logic/
    plan.go        the State/Condition/Action implementation
    plan_runner.go the independent frame-bound runner
    conditions.go  condition types
    actions.go     action templates, including ScanTag
  tui/
    monitor.go     the operations monitor
```

`reducer.go` is a function from `(world, command)` to
`(world, receipt, worldEvents)` and touches no clock, no goroutine, and no I/O —
that purity is what makes the invariants in section 4 testable without a loop.
`world_loop.go` is the only file that imports `go-eventloop`. The `logic` package
never imports `go-eventloop` and never mutates the world. The debugger is written
by the owner and read by everyone else.
