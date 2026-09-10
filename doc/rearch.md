# go-pabt / pabtdebug — Long-Running Session Rearchitecture

> Produced by the hyperplan-debugger-rearch team   
> Date: 2026-06-09   
> Scope: Memory leak root-cause analysis & architectural proposals for infinite debug sessions

---

## 1. Current Architecture Snapshot

The `pabtdebug` package provides a live HTTP debug server + embedded web UI for PA-BT planning trees.

### Key Components

| Component | File(s) | Responsibility |
| --------- | ------- | -------------- |
| **Tracker** | `tracker.go` | Records every tick as a `TickEvent`, stores a sliding window of tree snapshots, serves queries (`Events`, `Timeline`, `Profile`, `Diff`, `Search`) |
| **Hub** | `stream.go` | SSE (Server-Sent Events) broadcaster. Manages client connections, throttles broadcasts |
| **Server** | `server.go` | HTTP mux. Routes REST endpoints (`/plans`, `/timeline`, `/profile`, `/diff`, `/breakpoints`, `/dot`, `/search`, `/ui`) |
| **UI** | `ui.go` | Single embedded HTML+CSS+JS page. Renders trees as SVG, timeline sidebar, profile bar, playback controls |
| **Core** | `pabt.go`, `status.go`, `metadata.go` | Planning algorithm, node types, atomic status tracking |

### Tick Data Flow

```
User code:  plan.Tick()
            ↓
Tracker.Track(status, err)  ← builds full TreeNode snapshot (recursive walk)
            ↓
  ├─→ append to events slice (capped at maxEvents, default 1000)
  ├─→ Hub.Broadcast(event)   ← SSE push to all connected browsers
  └─→ checkBreakpoints(event)
            ↓
Browser UI: receives SSE → addTimelineEvent() → renderTimeline() → renderTree()
```

---

## 2. Deficiency Catalogue

### 2.1 Server-Side: Unbounded & Inefficient State

#### D1 — `Tracker.events` allocation amplification (`tracker.go:90-95`, `tracker.go:100-106`)
- **What**: Every `Track()` appends a `TickEvent` containing a full `*TreeNode` snapshot.
- **Consequence**: A single tick event is `O(nodes)` in memory. Even with `maxEvents=1000`, a 1,000-node tree costs ~1M nodes retained (plus JSON-serialised copies on every HTTP request).
- **Evidence**: `Events()` and `Timeline()` both `make([]TickEvent, len(t.events))` — every read allocates a full deep copy.

#### D2 — `EventAt` is O(n) (`tracker.go:138-147`)
- **What**: Linear scan over the events slice to find by iteration number.
- **Consequence**: Poll-heavy clients (timeline slider, diff viewer) degrade from O(1) to O(n).

#### D3 — Tree snapshot on every tick is unavoidable today (`tracker.go:71-75`)
- **What**: `BuildTree()` recursively walks the live `bt.Node` tree via `bt.Walk`, constructs metadata-rich `TreeNode` objects, computes `StructureHash`.
- **Consequence**: CPU cost per tick is proportional to total nodes in the plan, not just changed nodes.

#### D4 — `Profile()` recomputes from scratch every request (`tracker.go:149-180`)
- **What**: Walks **all** events × **all** nodes to aggregate tick counts.
- **Consequence**: O(events × nodes) per HTTP GET. For a 10-minute session at 10 Hz = 6,000 events × 500 nodes = 3M node visits per request.

#### D5 — No server-side event compression or delta encoding
- **What**: Every `TickEvent.Tree` is a full independent snapshot.
- **Consequence**: If only 3 nodes changed status between ticks, we still serialise and store the entire tree.

---

### 2.2 Server-Side: SSE Hub Fragility

#### D6 — `Hub.Broadcast` silently drops under backpressure (`stream.go:47-52`)
```go
for ch := range h.clients {
    select {
    case ch <- msg:
    default:        // ← event lost forever
    }
}
```
- **What**: All clients share a fixed 64-element buffer. A single slow client blocks no one, but it *misses* events.
- **Consequence**: Debug UI shows inconsistent state — timeline jumps, missing nodes.

#### D7 — No per-client pacing or keep-alive (`stream.go:56-102`)
- **What**: `ServeHTTP` blocks forever on `for { select { case msg := <-ch: ... } }`.
- **Consequence**: Stale TCP connections may never clean up; goroutine leak risk. No SSE `id` field for replay/resume.

#### D8 — Global throttle, not per-client (`stream.go:42-45`)
- **What**: `minInterval` is a single `time.Time` on the Hub.
- **Consequence**: A new client subscribing mid-throttle may wait up to `minInterval` before seeing *any* update.

---

### 2.3 Client-Side: Browser OOM & Freeze Risks

#### D9 — `timelineEvents` grows without bound (`ui.go:547-555`)
```js
function addTimelineEvent(event) {
    timelineEvents.push(event);   // ← no cap
    // ...
}
```
- **What**: The JS array `timelineEvents` is never truncated.
- **Consequence**: After a few hours at 10 Hz, the array holds 360,000 objects. Browser tab memory climbs until crash or forced GC stall.

#### D10 — `renderTimeline` is O(n²) (`ui.go:557-575`)
```js
function renderTimeline() {
    timelineList.innerHTML = '';   // ← destroys all DOM nodes
    for (const ev of timelineEvents) {   // ← rebuilds from scratch
        // create row, append
    }
}
```
- **What**: Every incoming event rebuilds the entire timeline DOM.
- **Consequence**: At 1,000 events, each new event costs ~1,000 DOM ops. At 10,000 events the tab freezes.

#### D11 — `renderTree` mutates shared event data (`ui.go:583-634`)
```js
function computeLayout(node, level) {
    node._level = level;    // ← mutates the original event.tree object
    node._width = 1;
    // ...
}
```
- **What**: Layout metadata is written back onto the `TickEvent.Tree` reference.
- **Consequence**: Playback + live mode race: live overwrites layout while playback reads it, causing visual corruption.

#### D12 — Full SVG DOM rebuild every tick (`ui.go:614-635`)
```js
function renderTree(tree) {
    svg.innerHTML = '';     // ← destroys every SVG element
    // ... rebuild all edges + nodes from scratch
}
```
- **What**: No DOM diffing. Every tick tears down and recreates the entire visual tree.
- **Consequence**: For a 500-node tree, each tick creates ~1,000 DOM elements. Layout thrashing forces constant reflow.

#### D13 — Playback uses `setInterval`, not `requestAnimationFrame` (`ui.go:1058-1071`)
```js
playbackTimer = setInterval(function() {
    // renderTree + renderTimeline
}, 1000 / speed);
```
- **What**: Fixed wall-clock rate ignores browser frame budget.
- **Consequence**: If one frame takes > interval, callbacks queue up. Tab becomes unresponsive; no way to cancel queued renders.

#### D14 — No viewport culling for large trees
- **What**: Every node in the tree is rendered to SVG regardless of zoom/pan.
- **Consequence**: A 10,000-node plan is theoretically viewable but practically crashes the renderer.

---

## 3. Severity Matrix

| ID | Component | Impact (Memory) | Impact (CPU) | Impact (Correctness) | Frequency |
|----|-----------|-----------------|--------------|----------------------|-----------|
| D1 | Tracker   | 🔴 Critical     | 🟡 Medium    | —                    | Every tick |
| D2 | Tracker   | —               | 🟡 Medium    | —                    | Every read |
| D3 | Tracker   | 🟡 Medium       | 🔴 Critical  | —                    | Every tick |
| D4 | Tracker   | —               | 🔴 Critical  | —                    | Every HTTP GET |
| D5 | Tracker   | 🔴 Critical     | 🟡 Medium    | —                    | Every tick |
| D6 | Hub       | —               | —            | 🟠 High              | Backpressure |
| D7 | Hub       | 🟡 Medium       | 🟡 Medium    | 🟡 Medium            | Disconnects |
| D8 | Hub       | —               | —            | 🟡 Medium            | Subscribes |
| D9 | UI        | 🔴 Critical     | —            | —                    | Every event |
| D10| UI        | 🟡 Medium       | 🔴 Critical  | —                    | Every event |
| D11| UI        | —               | 🟡 Medium    | 🟠 High              | Playback+live |
| D12| UI        | 🟡 Medium       | 🔴 Critical  | —                    | Every tick |
| D13| UI        | —               | 🔴 Critical  | —                    | Playback |
| D14| UI        | 🟡 Medium       | 🔴 Critical  | —                    | Large trees |

---

## 4. Conventional Solutions (Industry Best-Practice)

### 4.1 Server: Ring Buffer + Index
- Replace `[]TickEvent` with a fixed-size ring buffer.
- Maintain a `map[int]int` (iteration → ring index) for O(1) `EventAt`.
- Keep `maxEvents` configurable; expose `Tracker.SetMaxEvents(n)`.

### 4.2 Server: Incremental / Delta Events
- Compute a `TreeDelta` per tick: only nodes whose status or structure changed.
- Store full snapshot every N ticks + delta chain in between.
- This is the classic "keyframe + delta" model used in video codecs.

### 4.3 Server: Materialised Profile View
- Maintain `map[string]*NodeProfile` incrementally inside `Track()`.
- `Profile()` becomes O(nodes) instead of O(events × nodes).
- Trade-off: slightly higher tick latency, vastly cheaper HTTP reads.

### 4.4 Server: SSE Per-Client Queue + Credit Flow
- Give each client its own bounded channel (e.g. 256 events).
- If buffer fills, disconnect the slow client rather than silently dropping.
- Add SSE `id:` field so clients can resume with `Last-Event-ID`.
- Send periodic SSE comments (`:keep-alive`) to detect dead TCP.

### 4.5 Client: Virtualised Timeline
- Render only visible timeline rows (windowing / virtual list).
- Use a capped cache (e.g. LRU of 500 events); older events fetched on-demand via `/timeline/{iter}`.

### 4.6 Client: DOM Diffing for SVG
- Use a lightweight VDOM diff (or D3.js `join` pattern) to update only changed DOM attributes.
- Cache node `<g>` elements by ID; on tick, update `fill`, `class`, `opacity` — never `innerHTML = ''`.

### 4.7 Client: `requestAnimationFrame` Playback
- Replace `setInterval` with an `rAF` loop that computes which event to show based on elapsed time.
- Guarantees one render per frame, never queues stale renders.

---

## 5. Unconventional / Counter-Intuitive Solutions

> These deliberately trade familiar patterns for unexpected gains. They may be discarded, but each addresses a root cause that conventional solutions only mitigate.

### 5.1 Reverse-Timeline: Store the Present, Reconstruct the Past
- **Problem**: D1 (unbounded history) and D5 (snapshot bloat).
- **Idea**: Keep **only** the latest full tree. Record a reverse-apply log of mutations ("node X changed status from Running to Success", "node Y was pruned"). To view tick N, start from current tree and replay mutations backwards.
- **Trade-off**: +CPU for historical views, −memory for current state. Memory becomes O(nodes + log-size) instead of O(nodes × events).
- **Why it works**: In PA-BT, most of the tree *doesn't* change between ticks — the mutation log is sparse.

### 5.2 Functional Structural Sharing (Persistent Vectors)
- **Problem**: D1, D3, D5.
- **Idea**: Represent the tree as a persistent immutable structure (like Clojure vectors or Elm's `Array.Hamt`). Each tick produces a new tree root that shares 99 % of the previous tree.
- **Trade-off**: Needs a custom Go tree implementation (or `github.com/mediocregopher/persistent`).
- **Pay-off**: `TickEvent.Tree` becomes a single pointer swap; GC sees shared subgraphs and doesn't churn. Diff becomes O(changed nodes) via structural equality.

### 5.3 WebWorker Layout Engine
- **Problem**: D12 (SVG rebuild blocks main thread).
- **Idea**: Offload `computeLayout` + `assignPositions` to a WebWorker. Post only `TreeNode` + viewport bounds; worker returns a flat array of `{id, x, y, width, height}`. Main thread morphs attributes only.
- **Trade-off**: Serialization cost for tree data, but for large trees the parallelism dominates.
- **Bonus**: Worker can also handle `findNode`, `searchTree`, `flattenTree` — all currently O(n) recursive walks.

### 5.4 Fuzzy Adaptive Throttle
- **Problem**: D8 (global throttle punishes new clients), D6 (silent drops).
- **Idea**: No fixed `minInterval`. Instead, each tick computes a "mutation score" ( structural changes × node status changes ). High score = broadcast immediately. Low score = accumulate up to a dynamic max latency. Also track client's observed FPS via heartbeat and lower resolution if client reports <30 fps.
- **Trade-off**: Less deterministic update cadence.
- **Pay-off**: Never wastes bandwidth on invisible changes; always prioritises dramatic state shifts.

### 5.5 Client-Side Binary Tree Patch (WebAssembly)
- **Problem**: D12 (DOM diffing) and D5 (serialization cost).
- **Idea**: Server sends raw binary diff (protobuf / FlatBuffers). A tiny WASM module applies the diff to an in-memory scene graph and emits a minimal list of `{elementId, property, value}` changes for the main thread.
- **Trade-off**: Introduces a WASM build step.
- **Pay-off**: Near-zero parsing overhead, no JSON string intermediates, deterministic memory.

### 5.6 Render-Only-What-You-See (Viewport Frustum Culling)
- **Problem**: D14.
- **Idea**: Use SVG `viewBox` to compute visible bounding box. Pre-compute node bounding boxes from layout. Skip `drawNodes`/`drawEdges` for nodes outside viewport. Re-render on pan/zoom only.
- **Twist**: Instead of hiding with `display:none`, literally *do not create* the DOM elements. For a 10k-node tree with viewport showing 50 nodes, this is a 200× reduction in DOM size.

### 5.7 "Time-Travel Debugger" Mode: Lazy Re-Tick
- **Problem**: D9, D10 (unbounded timeline memory).
- **Idea**: The timeline UI never stores events. Scrolling the timeline fetches `/timeline/{iter}` on demand. Playback is actually a rapid-fire fetch+render loop with a local 5-event ring buffer. The server is the single source of truth for history.
- **Trade-off**: Requires the server to store all events (but that's the server's job, not the browser's).
- **Pay-off**: Browser memory is constant regardless of session length.

---

## 6. Tradeoff Analysis

| Approach    | Complexity | Memory | CPU (server) | CPU (client) | Network | Risk |
| ----------- | ---------- | ------ | ------------ | ------------ | ------- | ---- |
| Ring buffer | Low        | ✅     | ✅           | —            | —       | Low |
| Delta events| Medium     | ✅     | ✅           | ✅           | ✅      | Medium (sync bugs) |
| Materialised profile | Low | — | ✅ | — | — | Low |
| Per-client SSE queue | Medium | — | — | — | — | Low |
| Virtual timeline | Medium | ✅ | — | ✅ | 🟡 | Medium |
| SVG DOM diff | Medium | 🟡 | — | ✅ | — | Medium |
| rAF playback | Low | — | — | ✅ | — | Low |
| **Reverse-timeline** | **High** | **✅✅** | **🟡** | **✅** | **—** | **High (novel)** |
| **Persistent vectors** | **High** | **✅✅** | **✅** | **✅** | **—** | **High (impl cost)** |
| **WebWorker layout** | **Medium** | **—** | **—** | **✅✅** | **🟡** | **Medium** |
| **Fuzzy throttle** | **Medium** | **—** | **✅** | **✅** | **✅✅** | **Medium** |
| **WASM binary diff** | **High** | **✅** | **✅** | **✅✅** | **✅✅** | **High (build)** |
| **Viewport culling** | **Low** | **✅** | **—** | **✅✅** | **—** | **Low** |
| **Lazy re-tick** | **Medium** | **✅✅** | **—** | **✅** | **🟡** | **Medium** |

**Legend**: ✅ = significant improvement, 🟡 = mixed/neutral, — = not applicable.

---

## 7. Architectural Vision: "Infinite Session"

> A session should be able to run for days without the debug UI slowing down, crashing, or lying about state.

### Invariants
1. **Browser memory is bounded** regardless of ticks elapsed. <= 50 MB for the UI.
2. **Server memory is predictable** — O(nodes) + O(window-size), never O(nodes × ticks).
3. **No silent data loss** — every tick can be reconstructed or, if dropped, the client knows it missed something.
4. **60 fps interaction** — pan, zoom, search, playback never drop below 60 fps on a modern laptop.
5. **Survive disconnects** — client drops, reconnects, and resumes exactly where it left off.

### Straw-Man Target Architecture

```
┌──────────────────────────────────────────────────────────────┐
│  CLIENT (Browser Tab)                                        │
│  ┌──────────────┐  ┌──────────────┐  ┌─────────────────────┐  │
│  │  Timeline    │  │  SVG Viewport│  │  Playback Controller│  │
│  │  (virtual)   │  │  (culled)    │  │  (rAF loop)         │  │
│  └──────────────┘  └──────────────┘  └─────────────────────┘  │
│         ↑                  ↑                    ↑            │
│         └──────────────────┴────────────────────┘            │
│                         │                                    │
│                    ┌────────┐                                 │
│                    │ Worker │ ← sends layout jobs / receives  │
│                    │ Thread │   flat render commands           │
│                    └────────┘                                 │
└──────────────────────────────────────────────────────────────┘
                              │
                    SSE + REST (Resume-ID, Delta-Events)
                              │
┌──────────────────────────────────────────────────────────────┐
│  SERVER (Go Process)                                         │
│  ┌──────────────┐  ┌──────────────┐  ┌─────────────────────┐  │
│  │   Tracker    │  │  Delta Log   │  │   Materialised      │  │
│  │  (persistent │  │  (keyframe + │  │   Profiles          │  │
│  │   ring buf)  │  │   deltas)    │  │   (incremental)     │  │
│  └──────────────┘  └──────────────┘  └─────────────────────┘  │
│         ↑                                                     │
│    Hub (per-client credit flow, reconnect resume)             │
└──────────────────────────────────────────────────────────────┘
```

### Incremental Path (no big-bang rewrite)

| Phase | Change | Effort | Impact |
| ----- | ------ | ------ | ------ |
| P1 | Add `map[int]int` index to Tracker; cap timelineEvents in UI | 1 day | Fixes D1, D2, D9 |
| P2 | Per-client SSE queue + `Last-Event-ID` resume | 2 days | Fixes D6, D7, D8 |
| P3 | Virtual timeline + capped client cache | 2 days | Fixes D9, D10 |
| P4 | SVG attribute diffing (no `innerHTML = ''`) | 3 days | Fixes D11, D12 |
| P5 | `requestAnimationFrame` playback | 1 day | Fixes D13 |
| P6 | Materialised NodeProfile + incremental update | 2 days | Fixes D4 |
| P7 | Delta encoding for events | 3 days | Fixes D3, D5 |
| P8 | Viewport culling | 2 days | Fixes D14 |
| **P9** | **Reverse-timeline or persistent vectors** | **1-2 weeks** | **Fundamentally solves D1, D3, D5** |
| **P10** | **WebWorker + optional WASM diff** | **1 week** | **Client scalability** |

---

## 8. Open Questions for Team / Lead

1. Is `maxEvents=1000` empirical, or just a guess? What is the longest session anyone has actually run?
2. Do we have telemetry / benchmarks for tick rate, tree size, or browser memory in the wild?
3. Is the debug UI allowed to drop old ticks (beyond a configurable window), or must *every* tick be reachable forever?
4. Are there plans to support **multiple simultaneous plans** in one tracker? The `plans` endpoint currently hard-codes `"id": "0"` (`server.go:81`).
5. Does the server run in-process with the planner (shared memory), or is remote debugging a future requirement? This determines whether we can use shared-memory ring buffers or must stick to HTTP/SSE.
6. What is the acceptable latency for `Profile()` and `Diff()`? Sub-100 ms? Sub-1 s?
7. Browser support floor — do we target modern Chrome only, or need Safari/Edge/Firefox parity?

---

## 9. File-Level References

| Deficiency | File | Line(s) | Snippet |
| ---------- | ---- | ------- | ------- |
| D1 | `tracker.go` | 90-95 | `t.events = append(t.events, event)` |
| D1 | `tracker.go` | 100-106 | `result := make([]TickEvent, len(t.events))` |
| D2 | `tracker.go` | 138-147 | Linear `for i := range t.events` scan |
| D3 | `tracker.go` | 71-75 | `tree := t.BuildTree()` inside `Track()` |
| D4 | `tracker.go` | 149-180 | `Profile()` re-walks all events |
| D6 | `stream.go` | 47-52 | `default:` branch drops event |
| D7 | `stream.go` | 56-102 | No heartbeat / stale conn detection |
| D8 | `stream.go` | 42-45 | Single `lastBroadcast` field |
| D9 | `ui.go` | 547-555 | `timelineEvents.push(event)` no cap |
| D10| `ui.go` | 557-575 | `timelineList.innerHTML = ''` rebuild |
| D11| `ui.go` | 583-634 | Mutates `node._level`, `node._x`, etc. |
| D12| `ui.go` | 614-635 | `svg.innerHTML = ''` |
| D13| `ui.go` | 1058-1071 | `setInterval` playback |
| D14| `ui.go` | 614-635 | No visibility check before creating SVG elements |

---

*End of document.*
