# Harbor Panic — a retired load fixture for `go-pabt`

> **Retired. Migration input, not a product document and not a requirement.**
> This scenario was withdrawn on 2026-09-27 after adversarial review. It has no
> coherent incident or operational purpose, its carrier-to-container pairing is
> computed arithmetically from the actor index rather than claimed through the
> world, its storm teleports goals rather than modelling anything, and its belief
> mode is off by default. It is not a specification this repository implements,
> and no acceptance criterion refers to it.
>
> It stays in the tree as a load fixture because the benchmark and stress tests
> in this package still exercise the planner, and because
> `examples/reefer-handoff` was derived from what this one got wrong. The
> replacement's contract is `examples/reefer-handoff/DESIGN.md`.
>
> **Fixture context.** This is a load fixture for the PA-BT planner and
> simulator. It is not part of the debugger and it does not serve or attach to
> one.

Harbor Panic puts four autonomous straddle carriers, six containers, four berths,
a rogue human forklift, and a storm on a 40×18 harbor, and makes the planner
prove it can survive the lot. If `examples/tcell-pick-and-place` is the calm
tutorial, this is the sequel where the tutorial catches fire.

```
cargo ship unloads  ──►  6 containers scattered on docks
4 berths (goals) wait  ──►  each container must end colliding a goal shape
4 straddle carriers race  ──►  each is a distinct plan
1 human forklift cheats  ──►  steals containers, blocks lanes at 3.1 m/s
storm every 120 ticks  ──►  berths teleport to new docks, invalidating every plan
harbor walls  ──►  40×18 space with wall segments that create corridors
```

## Why this scenario is interesting

PA-BT planning is DNF reachability plus continual hill-climbing, so the moment a
forklift moves a container two cells, the winning plan for a carrier invalidates
and the planner must `search()` → `expand()` → `resolve()` → reorder conflicts.
The storm guarantees it happens regularly even if the forklift slept.

- **Resource contention.** Two carriers converge on one container. The first pick
  succeeds; the second carrier's `h = /0` precondition now conflicts with
  `h = container`, and the planner emits a *different* tree.
- **Spatial collision.** Placing a container adds a non-collision term per other
  sprite. Under wall corridors the only valid berth drops from twenty cells to
  four, and plan depth jumps from 12 nodes to 180+.
- **Churn, not growth.** Goals do not multiply, they *move*. The tree's
  structure stays bounded while its status fields flip every tick.

## Running it

Every file is behind `//go:build example`, so the fixture is invisible to
`go build ./...` and `go test ./...`, and needs the tag to run or to test.

```bash
go test -tags example ./examples/harbor-panic/... -count=1
go vet  -tags example ./examples/harbor-panic/...

# headless, bounded
go run -tags example ./examples/harbor-panic -headless -ticks 200

# the tcell harbour, which needs a terminal
go run -tags example ./examples/harbor-panic -tui
```

There is no `--` before the flags. Go's `flag` package treats a bare `--` as the
end of the flags, so everything after it becomes a positional argument — and the
binary now refuses one rather than running on the defaults, because a run that
silently ignores `--ticks` is a run that never stops.

### Flags

| Flag | Default | Effect |
|---|---|---|
| `--tick-ms` | 80 | milliseconds between ticks |
| `--storm-every` | 120 | ticks between goal relocations; 0 disables |
| `--human-speed` | 3.1 | forklift speed per tick |
| `--headless` | false | run without the tcell screen |
| `--tui` | false | run the tcell TUI; auto-disables with `--headless` or a missing `TERM` |
| `--ticks` | 0 | stop after N ticks; 0 runs until interrupted |
| `--burst` | 0 | tick N times as fast as possible, then stop (implies `--headless`) |
| `--scenario` | — | `calm`, `stormy`, or `maze`; overrides the storm and human defaults |
| `--rank-by-cost` | false | rank actions by an explicit cost model instead of the accidental ordering |
| `--belief` | false | hidden containers, revealed by a carrier sensing them |
| `--fleet` | false | three harbors × four actors, twelve concurrent plans |
| `--tui-audit` | false | print the TUI design-token audit table and exit |

### TUI command deck

| Key | Effect |
|---|---|
| `q` | quit |
| `p` | pause the ticker |
| `s` | step one harbor tick |
| `+` / `-` | speed up / down |
| `c` | cycle scenario |
| `r` | toggle rank-by-cost |
| `B` | toggle belief |
| `/` | open the overlay |

The TUI renders a 40×18 grid: walls `#` slate, berths `!` blue with a ghost
preview 10 ticks before a storm, containers `R/G/B/Y/M/C` bright or dim when
held, `?` when hidden under belief, carriers `0-3` yellow, forklift `H` red with
a pursuit trail. A top bar shows the harbor tick and each actor, and a bottom
strip shows rank, scenario, and belief state. Rendering is verified headless
through `tcell.NewSimulationScreen` in `tui/tui_test.go`.

## What the fixture verifies

`go test -tags example ./examples/harbor-panic/... -count=1` is the verification,
and under `-race` it is a stronger one. The tests cover:

- large tree construction, and that a printed tree carries its type labels
- verified effects and cost ranking
- belief, safety counters, fuzzing, and the cinematic and formal traces
- no-good learning
- determinism under a fixed seed
- four plans ticking concurrently while the harbor steps
- snapshot isolation, and storm relocation
- the fleet at twelve plans
- TUI rendering, glyph mapping, the command deck, and the design tokens

`scripts/` holds shell harnesses that run the binary directly — `cinematic_tour.sh`,
`verify_chaos.sh`, `verify_cross_compile.sh`, `verify_formal.sh`,
`verify_learning.sh`, and `verify_packaging.sh`. They are conveniences for driving
the fixture by hand. An earlier version of this file also listed
`verify_ui_invariants.sh`, `verify_io_tour.sh`, `verify_metrics.sh`, and
`verify_safety.sh`; all four drove a debug server this fixture does not run, and
they were removed with it. The grep-based UI harness in particular printed
success for properties it never tested, so nothing was lost that was worth
keeping.

## Appendix: research lineage

> **Design input, not verification.** The documents below are the reasoning a
> design here was checked against. They are not evidence that any code in this
> repository behaves the way this appendix says, and a claim about what the code
> does has to be checked against the code or against a test that asserts it.

Harbor's design decisions trace back to audits of behaviour-tree planning,
partial observability, and pipeline linearization, in `docs/research/`.

| Document | Topic | Where it shows up here |
|---|---|---|
| [bt-02](../docs/research/bt-02.md) | typed observations, effect verification | `hiddenVar`/`hiddenValue`, a typed state snapshot rather than a raw boolean |
| [bt-04](../docs/research/bt-04.md) | pathfinding, movable obstacles | BFS `findPath`, the dead-end cache, the wall-collision guard that reverts to the last safe cell |
| [bt-06](../docs/research/bt-06.md) | BT expansion, cost-optimal planning | `actionCost` and `rankByCost`, and the separation of a condition's declaration from its per-instance state |
| [bt-08](../docs/research/bt-08.md) | BehaVerify, Fiacre, LTLf | `checkSafetyLocked`, which enforces the wall-overlap and held-item invariants every tick |
| [ungrounded-01](../docs/research/ungrounded-01.md) | belief behaviour trees | `--belief`: containers start `Hidden=true`, `templateSense` reveals them within 3 cells, the TUI renders `?` until then |
| [ungrounded-02](../docs/research/ungrounded-02.md) | pipeline linearization, bounded per-instance state | the `--burst` loop, and the reasoning that event-driven scheduling is the principal work-elimination mechanism while linearization is a data-layout optimization on top of it |

The corpus is large and worth reading if you are changing the planner's structure.
It is input to a design discussion, not a substitute for one.
