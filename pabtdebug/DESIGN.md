# pabtdebug Debug UI — Design Specification

**Living document.** Maintenance contract: every change to the embedded UI in `ui.go`
must update this file in the same commit (tokens, rules, change log). The `:root` block
in `ui.go` carries a pointer comment back to this file. If the two ever disagree, stop
and reconcile before proceeding — drift between spec and implementation is a defect.

## 1. Purpose and audience

A developer debugging **live PA-BT planning trees** (go-pabt + pabtdebug). The UI must
answer, in priority order:

1. **What is executing right now**, in which plan? (active path)
2. **Why** is it executing? (conditions / post-conditions on that path)
3. **What will it change?** (effects)
4. **Where is the plan degenerate or bloated?** (chain compression + depth readout)
5. **What changed** between ticks or iterations? (timeline, diff)

Anything not serving these five questions is chrome, and chrome must stay quiet.

## 2. Aesthetic direction — "Harbor Mission Control"

A night-shift port control room: deep navy void, phosphor-cyan instrumentation,
monospace data readouts, uppercase letter-spaced geometric-sans chrome labels.
Chosen because the showcase domain is a harbor simulation, because high-contrast dark
suits long debugging sessions, and because it is unmistakably not generic dashboard
slop. Atmosphere comes from a layered background: a radial cyan glow at the top right
plus 32px instrument grid lines over the void colour — depth without noise.

## 3. Colour tokens

| Token | Hex | Usage |
|---|---|---|
| `--bg-main` | `#04070d` | canvas void |
| `--bg-panel` | `#0a111d` | side panels, header, footer |
| `--bg-card` | `#101a29` | raised surfaces, inputs, node cards |
| `--border` | `#1c2a3d` | hairline dividers |
| `--divider` | `#14202f` | subtle inner separators |
| `--text-primary` | `#dbe7f4` | primary ink |
| `--text-secondary` | `#8296ad` | secondary ink, labels |
| `--text-oncolor` | `#ffffff` | text on saturated fills |
| `--accent` | `#53d8fb` | instrumentation accent, focus, live |
| `--success` | `#46d68c` | Success status |
| `--failure` | `#f0566a` | Failure status |
| `--running` | `#f5a524` | Running status |

Type hues (node-card top border + type label, never the only signal):
Goal `#5aa2ff` · PPA `#c792ea` · ActionSelector/ActionRoot `#ff9e64` ·
ActionNode `#46d68c` · Preconditions `#2dd4bf` · Unknown `#50627a`.

Status is encoded by colour **and** shape: Running = pulsing dot, Success = filled
square, Failure = ring. Never colour alone.

Contrast (re-verify with the WCAG formula on any token change):
ink/void ≈ 15.4:1 · secondary/panel ≈ 6.9:1 · accent/panel ≈ 9.8:1 ·
running/panel ≈ 8.6:1 · failure/panel ≈ 5.9:1 — all ≥ AA.

## 4. Typography

- **Chrome** (labels, buttons, headers): `Futura, 'Avenir Next', 'Trebuchet MS', sans-serif`,
  uppercase, letter-spacing 0.08em (controls) to 0.14em (section labels).
- **Data** (node names, conditions, effects, numbers): `'SF Mono', 'JetBrains Mono', Menlo, monospace`.
- Scale: 8.5 captions/badges · 10 controls · 10.5 node names · 11 body · 13 panel titles · 15 brand.
- **Banned:** Inter, Roboto, Arial, Space Grotesk (user directive).

## 5. Spacing and geometry

4px base grid. Panel padding 12/16. Node card 210×54, radius 6, column pitch 274,
row pitch 70. Gaps: 8 inline / 16 stacked / 24 between regions.

## 6. Layout regions

Header 40px (brand · connection · search · actions). Timeline left 220px.
Canvas centre with its own toolbar (Fit / Reset / Expand all / Collapse all · stats
readout · zoom) — the readout lives there because it proves compression exactly where
the tree is visible. Inspector right 280px. Footer 60px (profile strip + status
legend). The canvas itself is transparent so the body's instrument-grid and radial
glow (§2) read through behind the tree; all other regions keep opaque panel fills.

## 7. Tree rendering rules

- Horizontal, left → right (depth = column).
- **Chain compression:** maximal single-child runs longer than 2 render as ONE card
  labelled `×N` showing the active member's name; clicking its pill unwinds one link.
- **Collapse by default:** branch nodes collapsed unless on the active path
  (root → Running leaf) or at depth < 1.
- Pills: `+N` collapsed branch · `×N` compressed chain · `−` expanded.
- Active path auto-expanded on every render.
- Viewport culling retained (`isBboxVisible`).
- The stats readout proves compression visibly (raw depth vs rendered cards).

## 8. Motion

CSS-only: staggered card reveal on structure change (`animation-delay` ∝ depth, capped
240ms), pulsing Running dot (1.4s), dash-scroll on the active edge.
`@media (prefers-reduced-motion: reduce)` disables all of it.

## 9. Accessibility

Status = colour + shape. Focus ring = 2px accent. All controls keyboard-reachable with
visible focus. ARIA tree roles land with the keyboard-navigation task.

## 10. Harness invariants (must survive every UI edit)

`verify_ui_invariants.sh` greps: `buildLayoutMap` ≥3 · `requestAnimationFrame` ≥4 ·
`isBboxVisible` ≥2 · `showSaveFilePicker|showOpenFilePicker` ≥2 · `node._level =` == 0 ·
`setInterval` == 0.

## 11. Queued steps (blueprint tasks, landed incrementally)

Horizontal chain-compressed layout · collapse pills + active path · 404-gated timeline
history · multi-plan selector · inspector upgrade · keyboard map · minimap · responsive
breakpoints. Each lands as one verifiable step with a vision check and a change-log
entry here.

## 12. Change log

- **v1 — 2026-09-16:** Tokens, typography stacks, and the layered instrument-grid
  background landed in `ui.go` (`:root` / `body`). Legacy palette (`#0f0f23`,
  `#1a1a2e`, `#00d4ff`, monospace-only) retired as the "baseline slop" reference.
  Behaviour unchanged; visual tokens only.
- **v2 — 2026-09-16:** Component layer consumes the tokens. Node cards become
  `--bg-card` surfaces with a type-hue left bar + uppercase letter-spaced type label
  (candy fills retired). Status is shape+colour everywhere: Running = pulsing dot,
  Success = filled square, Failure = ring (legend, timeline rows, node glyphs,
  status bar). `TYPE_COLORS`/`STATUS_COLORS`/profile bars/diff overlay/detail badges
  all read `:root` via `cssVar()` — zero hard-coded legacy hexes remain. Fixed the
  profile-strip Success-as-red inversion at its source. Themed browser surfaces
  (selection, scrollbars, focus-visible). Brand chrome "PA-BT // DEBUG".
- **v3 — 2026-09-16 (same commit as v2):** Horizontal chain-compressed tree layout.
  `buildViewModel()` collapses maximal single-child runs longer than 2 into ONE card
  labelled `×N` showing the active member's name; click its pill to unwind one link
  (`unwoundHeads` Set). Branch nodes collapsed by default unless on the active path
  (root → Running leaf) or at depth < 1 (`isCollapsedView` with user override Sets
  `userExpanded`/`userCollapsed`, `searchReveal`, `expandAllMode`). Active path
  auto-expanded on every render. Layout: `NODE_W=210 NODE_H=54 COL_W=274 ROW_H=70`,
  horizontal left→right (`absX = level * COL_W`). Expand-all / Collapse-all buttons
  wired. Stats readout proves compression (rendered cards vs raw depth). Harness
  invariants intact: buildLayoutMap 5, rAF 7, isBboxVisible 4, pickers 4,
  node._level= 0, setInterval 0. Still outstanding (next increments): 404-gated
  timeline (serverOldestIter), multi-plan selector dropdown, connection-resilience
  indicator (reconnect logic exists but chrome incomplete).
- **v4 — 2026-09-16:** Canvas occlusion fix + spec reconciliation. `#main` painted an
  opaque `--bg-main` sheet over the body's instrument-grid / radial glow, so the §2
  atmosphere never showed behind the tree; the canvas is now transparent (its toolbar
  keeps `--bg-panel`). §6 geometry corrected to the measured values (header 40px,
  timeline 220px, inspector 280px, footer 60px) and the stats readout's home
  documented as the canvas toolbar, where it proves compression next to the tree.
  Vision-verified; no behavioural change.
- **v5 — 2026-09-16:** Initial-view auto-fit + Reset recentering. The layout centres
  the root at y=0, so the default viewBox (pan 0,0 at zoom 1) clipped the top of the
  tree — the root card sat half off-canvas. The first render now auto-fits once
  (`fitToTree`) unless the user has already panned/zoomed (`userAdjustedView`), and
  Reset recentres the tree bounding box at zoom 1 instead of returning to the
  clipping origin. Drag/wheel interactions set `userAdjustedView` so live re-renders
  never fight the user's viewport.
- **v6 — 2026-09-16:** 404-gated timeline (sliding-window eviction floor). The server
  now advertises its oldest retained iteration on every SSE event (`oldestIteration`
  on `SSEEvent`, additive/omitempty); the UI tracks it as `serverOldestIter`, primes
  it from the initial `/timeline` load, and clamps older-history fetches to that floor
  instead of probing down to iteration 1 and silently dropping 404s. A dashed,
  non-interactive gap row (`.tl-row.tl-gap`) marks evicted ranges with a "Load to
  restore" hint, and a one-time console notice replaces silent null returns on a 404.
  Committed @ e77798b; build/vet/`go test -race` green.
- **v7 — 2026-09-16:** Multi-plan selector. A `<select id="plan-select">` in the header
  lists every registered plan (fed by `/plans`); four per-plan activity dots
  (`#plan-activity .plan-dot`, pulsing when a plan's `eventCount` advances) sit beside
  it and are also clickable. Selecting a plan calls `switchPlan()`: closes the old SSE,
  clears timeline/tree/detail/profile/eviction state, resets `initialFitDone` so the v5
  auto-fit re-runs for the new tree, reconnects SSE + refetches the timeline scoped to
  the chosen plan, and retains per-plan scroll position + selected node across switches.
  All eleven legacy fetches now route through `planPath()` (`/plans/{id}/...`) instead
  of the primary-only routes. `connect()` resolves the plan list first, then issues the
  plan-scoped history fetch — fixing a defect where the initial fetch hit `/plans/0/...`
  before `currentPlanId` was known (guaranteed 404 on harbor). Activity probing uses a
  `setTimeout` chain (no `setInterval`) to preserve the `setInterval == 0` harness
  invariant. Vision-verified switching harbor-bot-0 → harbor-bot-2; zero non-favicon
  console errors.
- **v8 — 2026-09-17:** Search match navigation keys. ArrowDown / ArrowUp now cycle
  matches in addition to Enter / Shift+Enter (the search-results acceptance requires
  arrow-key cycling); Escape clears and blurs. Count badge, prev/next buttons and
  live recompute-on-render unchanged from v7-era landing.
