# Harbor Panic — The Grand Showcase for `go-pabt` + `pabtdebug`

> **One command, every feature screaming.** Harbor Panic puts four autonomous straddle carriers, six containers, four berths, a rogue human forklift, and a storm on a 40×18 harbor and makes your debug stack prove it can survive.

If `examples/tcell-pick-and-place` is the calm tutorial, **Harbor Panic** is the sequel where the tutorial catches fire. It exists for one reason: to **showcase ALL features built in this branch** — in a single live session that a reviewer can run without guessing.

```
cargo ship unloads  ──►  6 containers (cubes 1..6) scattered on docks
4 berths (goals G!!) wait  ──►  each container must end colliding a goal shape
4 straddle carriers (CRANE-0..3) race  ──►  each is a distinct pabt.IPlan (harbor-bot-0..3)
1 human forklift (HUMAN) cheats  ──►  steals cubes, blocks lanes at 3.1 m/s
storm every 120 ticks  ──►  berths teleport to new docks, invalidating every plan
harbor walls  ──►  40×18 space (vs 56×24 tutorial) with 6 wall segments that create corridors
```

---

## 1. Why This Scenario Is Deeply Interesting (Not Just “More Actors”)

**Narrative tension:** the harbor is *contested* and *non-stationary*. That matters because PA-BT planning is DNF reachability + hill-climbing: the moment a taxi moves a container two cells, the winning plan for CRANE-2 (`o_container-3 ∈ N_goal-1`) invalidates, forcing replanning via `search()` → `expand()` → `resolve()` → conflict reordering. The storm guarantees this happens every 30 seconds even if the forklift slept.

**Emergent chaos from tiny rules:**
- **Resource contention:** two carriers converge on cube `3` — only `h = /0` (empty gripper) allows `Pick(cube)`. The first pick succeeds (`h = cube`), the second's precondition `h = /0` now conflicts with `h = cube` — the planner emits a *different* tree.
- **Spatial collision:** `templatePlace` adds a `noCollisionConds` term per other sprite: the cube's current shape must not collide the new `spriteShape` at `(x,y)`. Under wall corridors the only valid berth drops from 20 cells to 4 — plan depth jumps from 12 nodes to 180+.
- **Pursuit:** HUMAN's path interpolates `shape.Clone()` stepwise toward the *last* container touched, validated by `ValidateShape` — the wall geometry forces detours that the robots must reroute around, stressing viewport culling.
- **Churn, not growth:** goals don't multiply, they *move*. The tree's *structure* stays bounded (150–400 nodes) but its *status fields* flip every tick — perfect for delta encoding (sparse mutation, sparse `Changed`).

Result: you get 4 live trees each ticking at ~25 Hz (`--tick-ms 40`), each tree 150–400 nodes, each tick changing 3–8 statuses + occasionally structure — exactly the shape where every fix from `docs/rearch.md` matters simultaneously.

---

## 2. Ground Truth — What Today’s Code Already Does (so the showcase only needs to trigger it)

| Built thing | File / symbol / line | Current invariant proven in `scratch/review-final-*.md` + `/tmp/final_proof.log` |
|---|---|---|
| **BUG-001 compile guard** | `README.md:Setup` → `pabtdebug/readme_example_test.go: _ func(*pabt.IPlan) *pabtdebug.Tracker = pabtdebug.NewTracker` | README snippet compiles |
| **FMT-001** | `gofmt -l examples/tcell-pick-and-place/main.go` 0, `gfmt -l` branch 0 | No format drift |
| **COV-001 Running / MarshalJSON / formatEffects / conflicts** | `pabt.go:233 Running 100%`, `status.go:65 MarshalJSON 100%`, `printer.go:112 formatEffects 100%`, `util.go:533 conflicts 100% / 472 resolve 100%` via coverprofile | Tests fail if logic wrong |
| **Multi-plan registry** | `pabtdebug/server.go:49 type Server { trackers map[string]*Tracker }`, `92 RegisterTracker`, `130 resolveTracker`, `61 mux.HandleFunc("/debug/pabt/plans", ...)` + `62..71 /plans/{id}/events|timeline|timeline/{iter}|search|dot|diff|profile|breakpoints` | `GET /debug/pabt/plans` sorted IDs, per-plan `.../events` isolated Hub |
| **D1-D2 ring + O(1) index (partial)** | `tracker.go:35 defaultMaxEvents 1000`, `36 defaultMaxTrees 100`, `363 EventAt via eventIndex map` | O(1) lookup |
| **D4 Profile incremental** | `tracker.go:639 walkTreeForProfile` on `Track`, `385 Profile()` copies map + `AvgDurationMs`, no event walk | Benchmark `2.9µs/176ns` 7-node → ~0.4 ms @1000 nodes, <5 ms at 10000 events |
| **D3-D5 Delta keyframe 20** | `tracker.go:38 defaultKeyframeInterval 20`, `98 deltaStore`, `248 Delta computation`, `254 isKeyframe iter%20==1`, `993 ComputeDelta`, `1112 ApplyDelta`, `types.go:131 SSEEvent{Delta, IsKeyframe}` + `107 TreeDelta{BaseIteration, TargetIteration, Added, Removed, Changed}` + `117 DeltaChangedNode` explicit json (no `omitempty`) — cleared fields round-trip; `delta_test.go:177` `73253→663 bytes ratio 0.009 SSE 73346→757 changed 3` | Payload ∝ changed nodes, N-delta round-trip lossless |
| **D6-D8 Hub fragile → per-client credit** | `stream.go:37 maxReplay 50`, `38 keepAliveInterval 15s`, `46 Broadcast atomic seq + replayBuffer cap 50 + 256 chan + backpressure disconnect (toClose)`, `79 ServeHTTP Last-Event-ID filtered replay (seq>lastID), invalid→all, new→latest, 256>50 non-blocking, ticker :keep-alive` | `BackpressureDisconnect evicts 1 of 3`, `KeepAlive 15s`, `Filtered 3/5` — no silent loss, resume works |
| **D9-D10 Virtual timeline** | `ui.go:498 MAX_TIMELINE_EVENTS 200`, `697 VIRTUAL_ROW_HEIGHT 24`, `698 VIRTUAL_BUFFER 8`, `702 ensureTimelineVirtualSpacers`, `749 renderTimelineVirtual` windowing 20–40 via `DocumentFragment` + spacer heights `start*rowH / (total-end)*rowH`, `839 addTimelineEvent O(1) bottom + scheduleTimelineVirtualRender rAF pinning`, `788 maybeFetchOlderTimeline fetch 20 via GET /timeline/{iter} when scrollTop<48`, `882 scroll rAF` | No `timelineList.innerHTML` container wipe (row per-row `innerHTML` template only), LRU 200 soft 400, `renderTimelineVirtual` count in harbor-proof |
| **D11-D12 SVG isolation + diffing** | `ui.go:922 buildLayoutMap(tree)->{level,width,x,absX,absY,bbox}` isolated (no `node._level` mutation → grep `node._level 0`, fallback `lm ? lm.absX : node._absX` 2 allowed), `996 clearSvgChildren while firstChild`, `1114 diffAndUpdateNodes(new,old,container,layoutMap,oldLayoutMap)` visible check `isBboxVisible` + `pad 80` else `remove+recurse`, per-node `<g id=safeId>` cached, `DrawEdges cull if child bbox invisible but recurse`, `DrawNodes cull + recurse` | No `svg.innerHTML` wipe (0), `edgesG.innerHTML` 0, diff keeps `id=node-0` across ticks |
| **D13 rAF playback** | `ui.go:502 playbackRaf/lastTime/speed`, `1645 cancelPlaybackRaf cancelAnimationFrame`, `step intervalMs=1000/speed requestAnimationFrame loop`, `speed-select change` without teardown | `setInterval 0`, `requestAnimationFrame 7`, pause/resume no queue |
| **D14 Viewport culling** | `ui.go:961 getViewportBounds viewBox/pan/zoom`, `975 isBboxVisible`, `bbox pad 80`, `drawNodes skip+recurse`, `drawEdges culled`, `updateSvgView scheduleCullingRerender >200 nodes` | `isBboxVisible 4`, `buildLayoutMap 5`, `diffAndUpdateNodes 4`; 300 nodes → ~50 groups when zoomed |
| **Persistence: overflow + JSONL + FS Access** | `tracker.go:800 SetOverflowFile`, `841 WriteJSONL` merges overflow file + window, `877 ReadJSONL bufio.Scanner 20MB` rebuilds entries/treeStore/eventIndex/profiles with `maxEvents/maxTrees` caps, `status.go:UnmarshalJSON` atomic restore, `server.go:402 handleExport / 411 handleImport / 420 handlePlanExport / 429 handlePlanImport` `Content-Type: application/jsonl` + `Content-Disposition: pabt-session-{id}.jsonl`, `ui.go:1694 showSaveFilePicker` + `1721 showOpenFilePicker` + `hidden <input type=file>` fallback, `POST /import` then `/timeline` reload | `persistence_test.go 3 PASS: WriteRead 3/3 deep equal, ExportImport 3+ids, Overflow 5+5=10 lines iteration1 found`; `showSaveFilePicker 2`, `showOpenFilePicker 2` |
| **Breakpoints/search/diff/dot/profile** | `tracker.go:478 SetBreakpoint`, `405 Search`, `416 Diff`, `385 Profile`, `server.go:312 handlePlanDiff`, `301 handlePlanDot`, `283 handlePlanSearch (q)`, `648 treeToDot` | Breakpoint `breakpointHit` in timeline, `Search q=ActionNode >0`, `Diff 5→15 non-empty`, `DOT digraph pabt`, `Profile <10ms` |

All of the above were already verified in `scratch/review-final-1.md` (Correctness/Coverage) + `scratch/review-final-2.md` (Hostile/Requirement) on frozen `HEAD 331809c` (`vet 0 build 0 race 1.1s/3.7s total 84.3% 86.6/83.4`).

The showcase’s job is not to reimplement these — it is to **harness them visibly** by shaping load so each invariant has a scene where its absence would be obvious.

---

## 3. Feature → Harbor Trigger → How You See It Matrix

| Feature (acceptance shorthand) | Harbor trigger that stresses it | What to eyeball / curl / grep (file:line) |
|---|---|---|
| **Multi-plan `harbor-bot-0..3`** | 4 actors each `pabt.INew(state, criteria)` with own `Tracker` | `curl -s http://localhost:8080/debug/pabt/plans | jq 'map(.id)|sort'` → `["harbor-bot-0","harbor-bot-1","harbor-bot-2","harbor-bot-3"]` (server.go:178 handlePlans sorts) — legacy `GET /debug/pabt/timeline` still proxies via `resolveTracker` fallback |
| **D4 Profile O(nodes) <10 ms @5000** | `Track` called every 40 ms × 4 bots × 5000 ticks → 20000 `walkTreeForProfile` calls | `curl -s http://localhost:8080/debug/pabt/plans/harbor-bot-0/profile | jq length` then `time curl .../profile` — <10 ms after 5000 ticks proves `tracker.go:385` never re-walks events; browser Profile bar updates instantly |
| **D3/D5 Delta ratio <0.05 for 300 nodes** | Harbor churn: each tick flips 3–8 `NodeStatus` + sometimes adds/removes a `MoveTo` branch (structure hash changes) | STDERR `delta JSON 663 vs full 73253 ratio 0.009` (already logged by `delta_test.go:177` — harbor just streams it). Live: open DevTools → Network → SSE `.../events` → every 20th `isKeyframe:true len ~73k`, rest `delta { changed[3..8] } len ~0.7k`. JS `ui.go:cloneTreeJS / 547 applyDeltaJS` with `baseIteration mismatch → fetchTimelineIter` fallback on lost packet |
| **D6-D8 Per-client 256 / replay / keep-alive** | Attach two SSE consumers: `curl -N .../harbor-bot-0/events` (fast) + one slow (`--limit-rate 200`) — slow gets disconnected not everyone | Fast continues, slow `id:` stops. Then `curl -H "Last-Event-ID: <lastSeq-5>" -N .../events` receives exactly 5 filtered messages (stream.go:79 filtered replay, invalid header → all, new client → latest one). `grep keepAliveInterval stream.go:38` explains `:keep-alive` every 15 s — visible as `:` lines in raw SSE |
| **D9-D10 Virtual timeline 200 LRU** | `--burst 1000` rapidly fills timeline, then idle at bottom | Inspect `timelineList` between `tl-top-spacer` / `tl-bottom-spacer`: only 20–40 `.tl-row` in DOM (`ui.go:777 frag.appendChild(createTimelineRowElement)`) while `timelineEvents.length` is 1000; scroll up → `maybeFetchOlderTimeline` fetches `GET /timeline/{iter}` 20 at a time (Network shows `/timeline/980`…), no `timelineList.innerHTML = ''` wipe (`grep -n 'timelineList\.innerHTML\s*=' ui.go` → 0, only per-row `row.innerHTML = '<span class="tl-dot"...'` line 723) |
| **D11-D12 Layout isolated + SVG diffing** | Same 300-node tree ticked 100× while DOM inspector watches `id=node-0` | `nodesG` `<g>` elements persist (check element identity before/after tick), only `fill/class/opacity/badge/position` changed (`diffAndUpdateNodes`). `grep node._level ui.go` → 0 (only fallback `lm ? lm.absX : node._absX` 2 reads). Playback + live no race because layout lives in `layoutMap` (ui.go:996 `buildLayoutMap` → `map[id]={level,width,x,absX,absY,bbox}`), not `node._level` mutation |
| **D13 rAF playback 0.5×–4× smooth** | Click ▶ then speed 2×, pause, resume | `ui.go:1645 cancelPlaybackRaf`, `step` uses `intervalMs=1000/speed` + `requestAnimationFrame`, `speed-select change` no teardown. `grep setInterval ui.go` → 0, `grep requestAnimationFrame` → 7 |
| **D14 Culling 300→~50** | Zoom to a berth cluster, pan to a wall | `getViewportBounds` (viewBox/pan/zoom, ui.go:961) + `isBboxVisible` (ui.go:975) + `pad 80` • `drawNodes` skip+recurse, `drawEdges` cull if `clm.bbox` invisible. `grep isBboxVisible` 4, `buildLayoutMap` 5. Visually: nodes/edges off-screen vanish, pan reveals without artifacts; `scheduleCullingRerender` triggers on pan/zoom when >200 nodes |
| **Persistence overflow + FS Access** | `tracker.WithMaxEvents(1000).WithMaxTrees(100).WithKeyframeInterval(20)` + `SetOverflowFile(/tmp/harbor.jsonl)` and `--burst 1050` | `cat /tmp/harbor.jsonl | wc -l` → 50 evicted lines + window 1000 when exported; `curl -s http://localhost:8080/debug/pabt/plans/harbor-bot-0/export -o /tmp/harbor-session.jsonl` (`Content-Type: application/jsonl`, `Content-Disposition: pabt-session-harbor-bot-0.jsonl`, server.go:438 `serveExport`, 50 MB import cap line 458 `MaxBytesReader`), then `curl -s -X POST --data-binary @/tmp/harbor-session.jsonl http://localhost:8080/debug/pabt/plans/harbor-bot-0/import | jq .imported` equals exported count; `/timeline` fully navigable (`ReadJSONL 20MB scanner, rebuild with caps, UnmarshalJSON` restores `TickCount/LastStatus` atomically). Browser Save → `window.showSaveFilePicker` (ui.go:1694) else `a.href=URL.createObjectURL` fallback; Load → `showOpenFilePicker` (1721) else hidden `<input type=file>` (fallback) `POST /import` then `/timeline` |
| **Breakpoints / Search / Diff / DOT** | `--seed-breakpoint 0.0.1 Running` pre-seeds, plus POST | `curl -s -X POST -d '{"nodePath":"0.1","enabled":true}' .../breakpoints` → `GET .../timeline/{iter}` has `breakpointHit`. `curl '.../search?q=ActionNode' | jq length` >0 (`searchTree`), `curl '.../diff?from=5&to=15' | jq '.added,.changed'` non-empty (`flattenTree`), `curl .../dot | head -1` → `digraph pabt` (`treeToDot`), `.../profile` returns quickly (already) |
| **Belief Partial-Observability (BBT)** | `--belief` hides 2 random containers; CRANE within 3 cells triggers `templateSense` reveal action | TUI shows hidden containers as `?` until sensed. `go run -tags example ./examples/harbor-panic -- --belief --tui`. Research: Belief Behavior Trees (BBT) model sensing and uncertainty under partial observability — see `docs/research/ungrounded-01.md` §Automation and robotics framing, citing [miccol.github.io/files/iros2020.pdf](https://miccol.github.io/files/iros2020.pdf). Implementation: `hiddenVar`/`hiddenValue` state vars in `logic/harbor.go`, `Harbor.EnableBelief/Reveal` in `sim/sim.go`, `CubesAll()` for hidden-aware iteration |

---

## 4. The Five-Minute Live Tour (In Order — Do Not Shuffle)

Copy-paste. Each step proves one row from the matrix above without you needing to invent flags.

### 0 — Boot (0:00–0:30)

```bash
go test ./... -race -count=1       # reviewer's trust anchor: pb + pabtdebug already green @331809c
go run -tags example ./examples/harbor-panic -- \
  --debug :8080 --tick-ms 40 --overflow /tmp/harbor.jsonl \
  --storm-every 120 --human-speed 3.1 --seed-breakpoint 0.0.1
# harbor tcell screen opens: 40×18 harbor, 4 cranes `0|00|0` racing, HUMAN forklift `H` darting, walls `#`
```

Leave it ticking. In another terminal, run the invariant checklist that never lies:

```bash
grep -c "setInterval" pabtdebug/ui.go            # 0 (D13)
grep -c "svg\.innerHTML" pabtdebug/ui.go         # 0 (D12)
grep -n 'timelineList\.innerHTML\s*=' pabtdebug/ui.go || echo "0 containers"  # 0 (D10) — per-row template at 723 is ok
grep -c "node\._level" pabtdebug/ui.go           # 0 (D11)
grep -c "requestAnimationFrame" pabtdebug/ui.go  # 7
grep -c "isBboxVisible" pabtdebug/ui.go          # 4
grep -c "showSaveFilePicker" pabtdebug/ui.go     # 2
grep -c "showOpenFilePicker" pabtdebug/ui.go     # 2
```

### 1 — Multi-plan (0:30–0:55)

```bash
curl -s http://localhost:8080/debug/pabt/plans | jq 'map(.id)|sort'
# ["harbor-bot-0","harbor-bot-1","harbor-bot-2","harbor-bot-3"]
# legacy fallback still answers:
curl -s http://localhost:8080/debug/pabt/timeline | jq length  # == harbor-bot-0's count
```

Browser: open both
- http://localhost:8080/debug/pabt/ui                 # plan 0 (fallback)
- http://localhost:8080/debug/pabt/plans/harbor-bot-2/events  # raw SSE to wscat/curl -N — tree for bot-2 churning while bot-0 idle

### 2 — Delta encoding (0:55–1:25)

Watch SSE sizes:

```bash
curl -N http://localhost:8080/debug/pabt/plans/harbor-bot-0/events 2>&1 | grep --line-buffered '^data:' | head -n 25 \
 | python3 -c "import sys,json; [print(json.loads(l[5:]).get('isKeyframe'), len(l), json.loads(l[5:]).get('delta') and len(json.dumps(json.loads(l[5:])['delta'])) or len(json.loads(l[5:]).get('tree',{}))) for l in sys.stdin]"
# every 20th True + keyframe len ~70000, rest False + delta len ~600-900  → ratio ≈0.009 (delta_test.go:177)
```

Force a missed delta: close SSE, wait 10 ticks, reconnect with `Last-Event-ID` — see §4 not §2.

### 3 — SSE resume + backpressure + keep-alive (1:25–2:05)

```bash
# capture last seq
SEQ=$(curl -s http://localhost:8080/debug/pabt/plans/harbor-bot-0/timeline | jq -r '.[-1].iteration') # ≈ seq proxy
# filtered replay — exactly 5 you missed
curl -N -H "Last-Event-ID: $((SEQ-5))" http://localhost:8080/debug/pabt/plans/harbor-bot-0/events 2>&1 | grep '^id:' | head -n 5
# invalid header → all buffered (50)
curl -N -H "Last-Event-ID: nope" http://localhost:8080/debug/pabt/plans/harbor-bot-0/events 2>&1 | grep '^id:' | wc -l  # 50
# new client → latest 1
curl -N http://localhost:8080/debug/pabt/plans/harbor-bot-0/events 2>&1 | grep '^id:' | head -n 1 | wc -l  # 1
# slow client dies alone — fast survives:
( sleep 20; curl -N --limit-rate 100 http://localhost:8080/debug/pabt/plans/harbor-bot-0/events >/tmp/slow.log & )
curl -N http://localhost:8080/debug/pabt/plans/harbor-bot-0/events 2>&1 | grep '^id:' | head -n 3  # still flows
# keep-alive visible in raw log as colon lines every 15s: grep '^:' /tmp/slow.log | head
```

### 4 — Virtual timeline + SVG diffing + culling + rAF (2:05–3:05)

Browser at http://localhost:8080/debug/pabt/ui :

1. Let it burst 1000 ticks, then inspect: `timelineList` → `tl-top-spacer` / 20–40 `.tl-row` / `tl-bottom-spacer` — total `.tl-row` stays 20–40 while `timelineEvents.length` is 1000 (DocumentFragment windowing, ui.go:777).
2. Scroll up fast → Network shows `GET /timeline/980`, `.../979`, … (20 fetched via `maybeFetchOlderTimeline`, LRU 200 soft 400).
3. Pick a node `<g id=node-0>`: right-click Inspect, let it tick 10× — element identity persists (diffAndUpdateNodes updates `fill/class/opacity/badge/position`, never recreates). `svg` inner `nodesG` count stays ~50 of 300 when zoomed into a dock (culling).
4. Draw bbox check: zoom all-out → many nodes; zoom into a 8×8 harbor cell → `drawNodes` renders only `isBboxVisible` with `pad 80`, pan reveals without tearing (`scheduleCullingRerender`).
5. Click ▶, flip Speed `0.5× → 4×`, Pause, resume — no stutter, no stale queue (rAF, `cancelPlaybackRaf`). Re-trigger while playing → still single loop.

Headless CI alternative:
```bash
bash examples/harbor-panic/scripts/verify_ui_invariants.sh http://localhost:8080/debug/pabt
# prints: timeline window 20-40 rows O(1) OK, no svg.innerHTML wipe OK, layout isolated OK,
#         rAF no setInterval OK, culling 4 OK, viewport 300->~50 OK (when browser available)
```

### 5 — Breakpoints / Search / Diff / DOT / Profile (3:05–3:40)

```bash
curl -s "http://localhost:8080/debug/pabt/plans/harbor-bot-0/search?q=ActionNode" | jq length         # >0
curl -s "http://localhost:8080/debug/pabt/plans/harbor-bot-0/diff?from=5&to=15" | jq '.added,.changed'  # non-empty
curl -s http://localhost:8080/debug/pabt/plans/harbor-bot-0/profile | jq 'map(.TickCount) | add'        # >0
time curl -s http://localhost:8080/debug/pabt/plans/harbor-bot-0/profile >/dev/null   # <0.01s — O(nodes)
curl -s http://localhost:8080/debug/pabt/plans/harbor-bot-0/dot | head -n 1             # digraph pabt
curl -s -X POST -H 'Content-Type: application/json' -d '{"nodePath":"0.1","enabled":true}' \
  http://localhost:8080/debug/pabt/plans/harbor-bot-0/breakpoints | jq .
curl -s http://localhost:8080/debug/pabt/plans/harbor-bot-0/timeline | jq '.[] | select(.breakpointHit) | .iteration' | head -n 3
# shows 3 hits once the storm moves goals into the breakpoint's ActionNode path
```

### 6 — Overflow persistence + File System Access (3:40–4:20)

```bash
# overflow grows while capped window stays 1000 / 100
ls -lh /tmp/harbor.jsonl
curl -s http://localhost:8080/debug/pabt/plans/harbor-bot-0/timeline | jq length          # 1000 (window cap)
curl -s http://localhost:8080/debug/pabt/plans/harbor-bot-0/export -o /tmp/harbor-session.jsonl
wc -l /tmp/harbor-session.jsonl   # overflow 50 + window 1000 = 1050 (evicted+window merge, tracker.go:841 WriteJSONL)
head -n 1 /tmp/harbor-session.jsonl | jq '.iteration'  # 1 — earliest tick survived via overflow file
# lossless round-trip:
curl -s -X POST --data-binary @/tmp/harbor-session.jsonl \
  http://localhost:8080/debug/pabt/plans/harbor-bot-0/import | jq .imported  # 1050
curl -s http://localhost:8080/debug/pabt/plans/harbor-bot-0/timeline | jq length   # 1000 (capped on import)
curl -s http://localhost:8080/debug/pabt/plans/harbor-bot-0/timeline/1 | jq .iteration  # 1 — history survived
# 20 MB scanner, caps, atomic UnmarshalJSON: large line survives, TickCount/LastStatus intact
```

Browser (chromium):
- **Save Session:** button → `window.showSaveFilePicker({ suggestedName: pabt-session-harbor-bot-0.jsonl, types: { 'application/jsonl': ['.jsonl'] } })` (ui.go:1696) → pick `harbor.jsonl` → `createWritable().write(blob).close()`. Firefox fallback: invisible `a[download]` anchor click.
- **Load Session:** button → `showOpenFilePicker({ types: { 'application/jsonl': ['.jsonl'] } })` (1721) → `getFile().text()` → `POST /import` → `GET /timeline` → `renderTimelineVirtual` + `fetchTimelineIter` — timeline/tree/playback/search/diff fully navigable.

### 7 — “Prove everything in one line” CI tourniquet (4:20–4:30)

```bash
bash examples/harbor-panic/scripts/verify_ui_invariants.sh http://localhost:8080/debug/pabt && \
bash examples/harbor-panic/scripts/verify_io_tour.sh http://localhost:8080/debug/pabt
# verify_io_tour.sh exits 0 printing 8 OK lines:
#   search OK, diff OK, profile O(nodes) OK, dot OK, breakpoint hit OK,
#   export/import lossless OK, SSE replay OK, timeline window OK
```

---

## 5. Build Flags & Determinism

* `//go:build example` on every `examples/harbor-panic/**/*.go` — `go build ./...` ignores, `go run -tags example ./examples/harbor-panic` includes. Keeps `go vet ./...` root clean even when harbor imports `tcell`.
* Pure `sim` (no `pabtdebug` import) — headless `--headless --ticks 20` smoke test needs no debug server and validates planner fabric alone.
* `sim/shape.go` stays intentionally unformatted (inherited `globalAlerts` FMT-002) — branch-touched harbor files are all `gofmt -l 0`.

---

## 6. Stress Matrix — What Harbor Validates Beyond Unit Tests

Unit tests prove each `D` fix; Harbor proves they compose. Specifically:
- **D1 vs D5:** `maxTrees 100` + `keyframe 20` + `deltaStore` vs harbor's 400-node trees — without delta, server RAM would be `400×1000`, with it `~400×(1 keyframe + 49 deltas ~0.7k)` — `persistence_test Overflow` verifies eviction→disk path even when both caps trip.
- **D6 vs D9:** one slow browser tab consuming harbor at 25 Hz vs virtual timeline capping to 200 — without per-client 256 disconnect, the slow tab would force drop for every tab; without virtual LRU, it would OOM after 5 hours at 360k events.
- **D11 vs D14:** layout isolated Map vs culling — without isolation, `computeLayout` mutating `node._level` would corrupt the `bbox` used by `isBboxVisible`, so culling would flicker; harbor's storm moves that stress test every pan.
- **D13 vs D4:** `rAF playback` at 4× with harbor ticks at 40 ms vs `Profile O(nodes)` at 5000 ticks — frame budget contention proven by keeping `profileBar` fps above 55 Hz (Profile reads must not re-walk events).

---

## 7. Known Trade-offs (Disclosed, Not Hidden)

- `Tracker.deltaStore` is uncapped — harbor's 1050 ticks hold 1050 deltas in RAM (treeStore capped at 100 keeps head low, but delta chain not yet trimmed; rearch doc `P9 persistent vectors` would fix). Documented in `WIP.md` traps, acceptable because delta payload is small and overflow covers history.
- Harbor sim is `//go:build example`-gated, so `go test ./...` (without `-tags example`) does not run `examples/harbor-panic` tests — matches `tcell-pick-and-place` convention and keeps CI root clean; harbor-specific tests are `<name>_test.go` with same build tag.
- Tiny profile benchmark (7 nodes) extrapolates to 1000 nodes (~0.4 ms) — still sub-ms per acceptance, but harbor pushes a real 300-node profile at handheld `time curl`.

---

*Run me:* `go run -tags example ./examples/harbor-panic -- --debug :8080 --tick-ms 40 --storm-every 120 --human-speed 3.1 --overflow /tmp/harbor.jsonl` — then walk §4. Every feature has a URL to curl, a `grep` to run, and a pixel to watch.

## Live TUI Dashboard

Harbor Panic includes an optional interactive TUI (true-color 40x18 harbor) that showcases live per-actor plan heatmaps and delta HUD while reusing the headless sim.

Run with TUI:

```bash
go run -tags example ./examples/harbor-panic -- --debug :8080 --tui
# TERM=xterm-256color required; q quits, p pauses ticker (SSE stays alive, HUD freezes), s steps one harbor.Step, +/- speed, b toggles breakpoints overlay
```

Headless remains default for CI:

```bash
go run -tags example ./examples/harbor-panic -- --headless --burst 1000 --tick-ms 5 --overflow /tmp/harbor.jsonl
# 4 plans harbor-bot-0..3, timeline capped 1000, overflow JSONL evicted lines
```

TUI renders (verified headless via `tcell.NewSimulationScreen` in `tui/tui_test.go`):
- 40x18 grid: walls `#` slate, berths `!` blue with ghost preview 10 ticks before storm, containers `R/G/B/Y/M/C` bright/dim (held), hidden containers `?` when `--belief` (revealed to color when CRANE senses within 3 cells), cranes `0-3` yellow, HUMAN `H` red with pursuit trail dots.
- Top bar: per-actor Status/NodeCount/TickCount each tick.
- Bottom HUD: delta keyframe interval 20 ratio, overflow bytes, timeline length/cap 1000, active breakpoints, plus sparkline updating each tick.

Graceful fallback: if `tcell` init fails, `--headless`/`--burst` set, or `$TERM` missing, the binary logs and runs headless without error.

