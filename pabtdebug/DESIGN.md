# pabtdebug Debug UI — Design Specification

The debugger's user interface is the browser client in `pabtdebug/client/`:
`index.html`, `app.css`, and eight ES modules, embedded and served by
`NewDurableServer` at `/debug/pabt/ui`. This document specifies it.

**Maintenance contract.** A change to the client updates this file in the same
commit: the tokens and rules it changes, and the change log. The `:root` block in
`client/app.css` carries a pointer comment back to this file. If the spec and the
implementation ever disagree, stop and reconcile before proceeding — drift
between spec and implementation is a defect.

**What is not a specification.** `docs/research/` is constraint input, not
evidence. A retired prose architecture and its screenshots are historical.
Neither establishes what the code does; the code does.

## 1. Purpose and audience

A developer reading a PA-BT run that has already happened. The client must
answer, in priority order:

1. **What happened** — which tick, in which run, recorded by which store.
2. **What the tree was** at that tick, occurrence-keyed, as the planner built it.
3. **What governed an occurrence** — the preconditions, post-conditions, actions,
   and effects around it, each with its evidence.
4. **What it set** — effect values, and whether a value the application declared
   secret is redacted.

Anything not serving those four questions is chrome, and chrome stays quiet.

The client reads **history, not a live plan**. No route it calls reads a live
tree, because a live read while a tick runs races with the writes expansion
performs. Everything on screen is a record the store committed.

## 2. Colour tokens

| Token | Hex | Usage |
|---|---|---|
| `--bg` | `#04070d` | canvas void |
| `--panel` | `#0a111d` | side panels, header, footer |
| `--card` | `#101a29` | raised surfaces, inputs, node rows |
| `--line` | `#1c2a3d` | hairline dividers |
| `--divider` | `#14202f` | subtle inner separators |
| `--text` | `#dbe7f4` | primary ink |
| `--muted` | `#8296ad` | secondary ink, labels |
| `--accent` | `#53d8fb` | instrumentation accent, focus, live |
| `--success` | `#46d68c` | Success status |
| `--failure` | `#f0566a` | Failure status |
| `--running` | `#f5a524` | Running status |
| `--pre` | `#2dd4bf` | precondition evidence |
| `--warn` | `#c792ea` | post-condition and uncertainty |

Contrast, measured with the WCAG formula and re-verified on any token change:
ink/void ≈ 15.4:1 · secondary/panel ≈ 6.9:1 · accent/panel ≈ 9.8:1 ·
running/panel ≈ 8.6:1 · failure/panel ≈ 5.9:1 — all ≥ AA.

Status is encoded by colour **and** shape. A status conveyed by colour alone is
a status a reader who cannot see that colour does not have.

## 3. Typography

- **Chrome** (labels, buttons, headers): `system-ui, -apple-system, 'Segoe UI', sans-serif`.
- **Data** (node names, conditions, effects, numbers): `ui-monospace, 'SF Mono', Menlo, Consolas, monospace`.
- **Banned:** Inter, Roboto, Arial, Space Grotesk (user directive).

The chrome stack asks for the platform's own interface font. A local debugger
runs on whatever machine the operator is sitting at, and a named face is absent on
most of them, so a stack that leads with one silently falls through to its last
entry and the chrome becomes a different face on every host. The ban list is a
directive about which faces read as generic web defaults, and `system-ui` is not
one of them.

## 4. Spacing and geometry

4px base grid. Panel padding 12/16. Gaps: 8 inline / 16 stacked.

## 5. Layout regions

Six grid areas, declared in `app.css`:

```css
grid-template-columns: minmax(0, 210px) minmax(0, 1fr) minmax(0, 370px);
grid-template-rows: auto auto minmax(150px, 1fr) auto;
grid-template-areas:
  "header     header    header"
  "breadcrumb breadcrumb breadcrumb"
  "timeline   canvas    detail"
  "scrub      scrub     scrub";
```

| Region | Element | What it holds |
|---|---|---|
| header | `#header` | store, run, head, generation, connection state, export, import |
| breadcrumb | `#breadcrumb` | the path to the selected occurrence |
| timeline | `#timeline` | the recorded ticks |
| canvas | `#canvas` | the occurrence tree |
| detail | `#detail` | the selected occurrence's evidence |
| scrub | `#scrub` | the tick scrubber |

**The `minmax(0, …)` on every track is load-bearing.** A bare `210px` track
cannot shrink below its content, so a long breadcrumb widened the grid past the
viewport and forced horizontal scrolling. Three of the four layout defects found
in the first autopsy were this one shape: breadcrumb overflow, detail compressed
at 700px, and a scrub strip with no usable height.

Below 760px the grid collapses to one column with fixed timeline and detail rows.
The header keeps its row and drops its secondary fields rather than wrapping into
the grid.

`#app` and `#connect` are not both in the document flow. The connect screen is a
fixed overlay, because a `display` on these two overrides the user agent's
`[hidden]` rule and both in flow makes the body two viewports tall — which
`overflow: hidden` does not prevent a programmatic scroll from reaching.

## 6. Evidence rendering

A selected occurrence shows its preconditions with four separately labelled
fields: **expected**, **current**, **observed**, and **uncertainty**.

An expected value the application did not declare renders as *unavailable*,
carrying the record's own reason. It is never inferred from the condition's key.
A key-only inference is a false statement, and a client that shows one number
under a heading the reader takes for "what was required" reinstates the guess at
the last possible layer, where nothing can correct it.

## 7. Numbers are strings

Every integer in the client's model is a decimal string, including the small
ones, rather than a number below 2^53 and a string above it. One type per field
makes the hazard impossible to write. A field with two possible types forces
every reader to handle both, and the one who eventually handles only one is the
bug.

## 8. Continuity is claimed only when it was checked

A representation response carries the target tree as well as the delta, so a
client could display what it was sent and check nothing. The client applies the
delta to the base it holds and compares the result with the tree the store served,
and says nothing about continuity when the base is not held.

It does not recompute the store's canonical form. That would be a second
implementation of a digest and a second thing to be wrong. The identity that
matters is checked against the digest the store itself wrote.

## 9. Motion

CSS-only, and every rule is disabled under `@media (prefers-reduced-motion:
reduce)`. Motion may carry emphasis; it may not carry information on its own.

## 10. Accessibility

Status is colour plus shape. Focus ring is 2px accent. All controls are
keyboard-reachable with a visible focus ring. The tree is one tab stop
(`role="tree"` with roving focus), not one stop per node — a 400-node tree with
400 tab stops is not keyboard-navigable, it is keyboard-hostile. The connection
state is a `role="status"` live region, and a connect failure is a `role="alert"`.

## 11. Module split

| Module | Responsibility |
|---|---|
| `js/lossless.mjs` | exact-integer reader |
| `js/api.mjs` | the transport, and the only place a URL is built |
| `js/stream.mjs` | the SSE reader |
| `js/delta.mjs` | exact-delta application |
| `js/model.mjs` | the client's state |
| `js/render.mjs` | the regions |
| `js/detail.mjs` | the evidence panel |
| `js/app.mjs` | the wiring |

The `.mjs` extension is load-bearing: node runs these with no `package.json`, so
the pure modules are testable without a bundler. `pabtdebug/clienttest/` holds the
node tests over the pure ones.

`EventSource` cannot set a request header, which is why the stream is read off
`fetch` rather than an `EventSource`. The capability has to travel in a header.

## 12. Evidence standard

A change to the client is verified by `node pabtdebug/evidence/run.mjs`, run from
`pabtdebug/evidence`, against the exact binary built from the current tree, with
Playwright pinned to 1.63.0. An unpinned browser can lay a page out differently,
which makes a layout change indistinguishable from a browser change.

The runner builds the binary, authenticates, measures the six regions at three
viewports, walks the whole tree reading the panel at every occurrence, forces a
disconnect and a gap, reloads to prove a resume, exercises the export and import
controls, scans every captured artifact for a planted secret and for the
capability, and exercises the target's failure path at three points to confirm a
failure leaves no listener and no capability file. Artifacts land in
`scratch/evidence`.

**Measured, not asserted.** At 1200×762, 1024×768, and 700×700: no horizontal
overflow, `scrollY` 0, no overlapping region rectangles, detail 640/640/260
against a floor of 180, canvas 640/442/186 against 150, scrub 44 against 24, and
one tab stop in the tree.

A check that cannot fail is not a check. Four defects in this project were found
by a test the author wrote against their own code, and three of the client's
defects were only ever findable in a browser: a stream URL built with the base
already prefixed, so every connection 404'd; a `request` that accepted an abort
signal and dropped it, on the one request that has to be cancellable; and the
two-viewports-tall body described in section 5.

## 13. Change log

- **2026-09-25 (`ee9dda2`):** The client. `index.html`, `app.css`, and
  eight modules; the six grid areas with `minmax(0)` tracks; a delta applied and
  compared only when the base is held; integers as decimal strings throughout;
  the four evidence fields labelled separately, with an undeclared expectation
  rendered as unavailable and the record's own reason. The record format moved to
  version 2 to carry the linkage a selected occurrence needs.
- **2026-09-26 (`563ad85`):** Three defects the author's own checks could not
  see. A lexicographic comparison of decimal sequences stopped following a run at
  tick 9. A continuity check compared a digest the client cannot recompute, and
  cried wolf on every move. A dropped connection left the page frozen with a
  plausible but stale history. Each is now pinned by a test that fails against the
  code it replaced.
- **2026-09-27 (`cd41f6b`):** The route namespace lost its version suffix, so
  `js/api.mjs`'s base is `/debug/pabt` and the shell, the guard, and the eight
  modules moved with it. The client's own layout, tokens, and evidence rendering
  are unchanged, which is the claim to check: a route rename that altered a pixel
  would have been a layout change wearing a rename's clothes.
- **2026-09-26, later the same day (`fd77480`):** The planner stopped formatting effect *values*
  into an action's name. A name is plain text: no codec, no redaction walk, no
  diagnostic sanitizer. A value in one is a secret in every record that mentions
  the node, every digest over those records, and every export containing them.
  The evidence run found a planted secret in an exported bundle sixteen times,
  every one inside a name field.
