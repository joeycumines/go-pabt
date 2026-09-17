package pabtdebug

// uiHTML returns the HTML for the debug web UI.
func uiHTML() string {
	return uiHTMLStr
}

const uiHTMLStr = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>PA-BT Debug</title>
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
:root {
 /* pabtdebug/DESIGN.md v1 — Harbor Mission Control tokens. Update the spec first, then here. */
 --bg-main: #04070d;
 --bg-panel: #0a111d;
 --bg-card: #101a29;
 --text-primary: #dbe7f4;
 --text-secondary: #8296ad;
 --text-oncolor: #fff;
 --accent: #53d8fb;
 --success: #46d68c;
 --failure: #f0566a;
 --running: #f5a524;
 --border: #1c2a3d;
 --divider: #14202f;
 --type-goal: #5aa2ff;
 --type-ppa: #c792ea;
 --type-action: #ff9e64;
 --type-actionnode: #46d68c;
 --type-pre: #2dd4bf;
 --type-unknown: #50627a;
 --font-ui: 'Futura', 'Avenir Next', 'Trebuchet MS', sans-serif;
 --font-mono: 'SF Mono', 'JetBrains Mono', 'Menlo', monospace;
}
body {
 font-family: var(--font-mono);
 background:
  radial-gradient(1100px 520px at 78% -12%, rgba(83,216,251,0.07), transparent 62%),
  repeating-linear-gradient(0deg, transparent 0 31px, rgba(83,216,251,0.035) 31px 32px),
  repeating-linear-gradient(90deg, transparent 0 31px, rgba(83,216,251,0.035) 31px 32px),
  var(--bg-main);
 color: var(--text-primary);
 overflow: hidden;
 height: 100vh;
 width: 100vw;
}
#header h1, .btn, #timeline-header, #detail-header, #detail-empty, .tl-status, .dtl-label {
 font-family: var(--font-ui);
 letter-spacing: 0.08em;
 text-transform: uppercase;
}
#app {
  position: relative;
  display: grid;
  grid-template-areas:
   "header header header"
   "timeline main detail"
   "bottom bottom bottom";
  grid-template-columns: var(--col-timeline, 220px) 1fr var(--col-detail, 280px);
  grid-template-rows: 40px 1fr 60px;
  height: 100vh;
  width: 100vw;
}
#resize-tl, #resize-dt {
  position: absolute;
  top: 40px;
  bottom: 60px;
  width: 5px;
  cursor: col-resize;
  background: transparent;
  z-index: 60;
}
#resize-tl { left: var(--col-timeline, 220px); margin-left: -2px; }
#resize-dt { left: calc(100vw - var(--col-detail, 280px)); margin-left: -3px; }
#resize-tl:hover, #resize-dt:hover { background: var(--accent); }
@media (max-width: 1024px) {
  #app {
    grid-template-areas:
     "header header"
     "timeline main"
     "detail detail"
     "bottom bottom";
    grid-template-columns: var(--col-timeline, 220px) 1fr;
    grid-template-rows: 40px 1fr 200px 60px;
  }
  #detail { border-left: none; border-top: 1px solid var(--border); max-height: 200px; overflow-y: auto; }
  #resize-dt { display: none; }
  #resize-tl { bottom: 260px; }
}
@media (max-width: 768px) {
  #app {
    grid-template-areas:
     "header"
     "timeline"
     "main"
     "detail"
     "bottom";
    grid-template-columns: 1fr;
    grid-template-rows: 40px 150px 1fr 180px 60px;
  }
  #timeline { border-right: none; border-bottom: 1px solid var(--border); }
  #resize-tl, #resize-dt { display: none; }
  #minimap { display: none; }
  #header { flex-wrap: wrap; height: auto; min-height: 40px; padding: 4px 8px; gap: 6px; }
  #header-left, #search-wrap, #header-right { min-width: 0; flex-shrink: 1; }
  #search-input { width: 100%; max-width: 160px; }
  #app { width: 100%; }
  body { overflow-x: hidden; }
}
#header {
 grid-area: header;
 display: flex;
 align-items: center;
 justify-content: space-between;
 padding: 0 12px;
 background: var(--bg-panel);
 border-bottom: 1px solid var(--border);
}
#header h1 {
 color: var(--accent);
 font-size: 14px;
 letter-spacing: 1px;
 text-transform: uppercase;
}
#header-left { display: flex; align-items: center; gap: 8px; }
#conn-dot {
 width: 8px;
 height: 8px;
 border-radius: 50%;
 background: var(--failure);
 transition: background 0.3s;
}
#conn-dot.connected { background: var(--success); }
#conn-status { font-size: 10px; color: var(--text-secondary); }
#search-wrap { display: flex; align-items: center; gap: 6px; }
#search-input {
 background: var(--bg-card);
 border: 1px solid var(--border);
 border-radius: 4px;
 color: var(--text-primary);
 padding: 4px 8px;
 font-size: 11px;
 font-family: inherit;
 width: 180px;
}
#search-input:focus { outline: none; border-color: var(--accent); }
#search-count { font-size: 10px; color: var(--text-secondary); min-width: 52px; text-align: right; font-variant-numeric: tabular-nums; }
#search-count.has-matches { color: var(--accent); }
#conn-banner {
 position: absolute;
 top: 46px;
 left: 50%;
 transform: translateX(-50%);
 display: none;
 z-index: 6;
 background: rgba(10,17,29,0.92);
 border: 1px solid var(--failure);
 color: var(--failure);
 font-family: var(--font-ui);
 font-size: 10px;
 letter-spacing: 0.08em;
 text-transform: uppercase;
 padding: 6px 12px;
 border-radius: 4px;
 pointer-events: none;
}
#conn-banner.stale { border-color: var(--running); color: var(--running); }
#tree-svg.stale { opacity: 0.45; transition: opacity 0.4s; }
#header-right { display: flex; align-items: center; gap: 8px; }
.btn {
 background: var(--bg-card);
 border: 1px solid var(--border);
 border-radius: 4px;
 color: var(--text-primary);
 padding: 4px 8px;
 font-size: 10px;
 font-family: inherit;
 cursor: pointer;
 transition: background 0.2s, border-color 0.2s;
}
.btn:hover {
 background: var(--bg-main);
 border-color: var(--accent);
}
.btn-icon { color: var(--text-primary); }
.btn.active { background: #004f66; border-color: var(--accent); }
#timeline {
 grid-area: timeline;
 background: var(--bg-panel);
 border-right: 1px solid var(--border);
 display: flex;
 flex-direction: column;
 overflow: hidden;
}
#timeline-header {
 padding: 6px 10px;
 font-size: 10px;
 color: var(--accent);
 text-transform: uppercase;
 letter-spacing: 1px;
 border-bottom: 1px solid var(--border);
 display: flex;
 align-items: center;
 justify-content: space-between;
}
#timeline-list {
 flex: 1;
 overflow-y: auto;
 padding: 4px;
}
.tl-row {
 padding: 4px 6px;
 border-radius: 3px;
 cursor: pointer;
 font-size: 10px;
 display: flex;
 align-items: center;
 gap: 6px;
 margin-bottom: 2px;
 transition: background 0.2s;
}
.tl-row:hover { background: #1e1e3a; }
.tl-row.selected { background: #25254a; }
.tl-row.tl-gap {
 cursor: default;
 border: 1px dashed var(--border);
 color: var(--text-secondary);
 font-style: italic;
 justify-content: center;
 background: transparent;
}
.tl-row.tl-gap:hover { background: transparent; }
.tl-dot {
 width: 6px;
 height: 6px;
 border-radius: 50%;
 flex-shrink: 0;
}
.tl-iter { color: var(--accent); min-width: 28px; }
.tl-status { font-weight: bold; min-width: 48px; }
.tl-duration { color: var(--text-secondary); margin-left: auto; }
#main {
 grid-area: main;
 /* transparent so the body instrument-grid + radial glow (DESIGN.md §2) read through */
 background: transparent;
 position: relative;
 overflow: hidden;
 display: flex;
 flex-direction: column;
}
#main-toolbar {
 display: flex;
 align-items: center;
 gap: 6px;
 padding: 4px 8px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-panel);
}
#breadcrumb-bar {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 3px 8px;
  font-size: 10px;
  color: var(--text-secondary);
  background: var(--bg-panel);
  border-bottom: 1px solid var(--divider);
  min-height: 20px;
  overflow-x: auto;
  white-space: nowrap;
}
.bc-crumb {
  padding: 2px 6px;
  border-radius: 3px;
  cursor: pointer;
  color: var(--text-secondary);
  transition: background 0.15s, color 0.15s;
}
.bc-crumb:hover {
  background: var(--border);
  color: var(--text-primary);
}
.bc-crumb.current {
  background: var(--accent);
  color: var(--bg-main);
}
.bc-sep { color: var(--divider); font-size: 9px; }
.btn-why.active {
  background: var(--accent) !important;
  color: var(--bg-main) !important;
}
.heat-ctrl { display: inline-flex; gap: 2px; margin-left: 4px; }
.btn-heat { font-size: 9px !important; padding: 2px 5px !important; opacity: 0.6; }
.btn-heat.active { opacity: 1; background: var(--accent) !important; color: var(--bg-main) !important; }
.node-card.heat-glow { filter: drop-shadow(0 0 3px var(--heat-color, transparent)); }
.node-card.fail-glow { stroke: var(--failure); }
.node-card.recent-pulse { animation: recentPulse 1.2s ease-out; }
@keyframes recentPulse {
  0% { filter: drop-shadow(0 0 6px var(--accent)); }
  100% { filter: none; }
}
@keyframes addedFlash {
  0% { filter: drop-shadow(0 0 8px var(--success)); stroke: var(--success); stroke-width: 3; }
  100% { filter: none; }
}
@keyframes removedFade {
  0% { opacity: 0.8; stroke: var(--failure); stroke-width: 2; stroke-dasharray: 4 2; }
  100% { opacity: 0; }
}
.node-card.added-flash { animation: addedFlash 0.6s ease-out; }
.tombstone { animation: removedFade 0.8s ease-out forwards; pointer-events: none; }
.conflict-line {
  stroke: var(--running);
  stroke-width: 2;
  stroke-dasharray: 6 3;
  fill: none;
  opacity: 0.7;
  animation: conflictFade 2s ease-out forwards;
}
@keyframes conflictFade {
  0% { opacity: 0.8; }
  70% { opacity: 0.6; }
  100% { opacity: 0; }
}
#narration-bar {
  padding: 3px 8px;
  font-size: 10px;
  color: var(--text-secondary);
  background: var(--bg-panel);
  border-bottom: 1px solid var(--divider);
  min-height: 16px;
  font-family: var(--font-mono);
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.causal-path {
  fill: none;
  stroke: var(--accent);
  stroke-width: 1.2;
  stroke-dasharray: 4 3;
  opacity: 0.55;
  pointer-events: none;
}
.diff-badge {
  pointer-events: none;
  font-size: 9px;
  font-weight: 700;
  font-family: var(--font-mono);
  paint-order: stroke;
  stroke: var(--bg-card);
  stroke-width: 3;
}
.diff-badge-added { fill: var(--success); }
.diff-badge-removed { fill: var(--failure); }
.diff-badge-changed { fill: var(--running); }
.node-card.diff-added { stroke: var(--success); stroke-width: 2.5; }
.node-card.diff-removed { stroke: var(--failure); stroke-width: 2.5; stroke-dasharray: 4 2; }
.node-card.diff-changed { stroke: var(--running); stroke-width: 2.5; }
#svg-container {
 flex: 1;
 position: relative;
 overflow: hidden;
 cursor: grab;
 touch-action: none;
}
#svg-container:active { cursor: grabbing; }
#tree-svg {
 width: 100%;
 height: 100%;
 display: block;
}
#detail {
 grid-area: detail;
 background: var(--bg-panel);
 border-left: 1px solid var(--border);
 display: flex;
 flex-direction: column;
 overflow: hidden;
}
#detail-header {
 padding: 8px 10px;
 font-size: 10px;
 color: var(--accent);
 text-transform: uppercase;
 letter-spacing: 1px;
 border-bottom: 1px solid var(--border);
}
#detail-content {
 flex: 1;
 overflow-y: auto;
 padding: 8px;
 font-size: 11px;
}
.dtl-section {
 margin-bottom: 10px;
}
.dtl-label {
 color: var(--text-secondary);
 font-size: 9px;
 text-transform: uppercase;
 letter-spacing: 0.5px;
 margin-bottom: 3px;
}
.dtl-value {
 color: var(--text-primary);
 word-break: break-word;
}
.dtl-badge {
 display: inline-block;
 padding: 2px 6px;
 border-radius: var(--radius-sm);
 font-size: 9px;
 font-weight: 600;
 margin: 1px 2px;
 letter-spacing: 0.02em;
 color: #fff;
}
.dtl-badge-effects { background: rgba(70,214,140,0.16); color: var(--success); border: 1px solid var(--success); }
.dtl-badge-cond { background: rgba(45,212,191,0.16); color: var(--type-pre); border: 1px solid var(--type-pre); }
#detail-empty {
 color: var(--text-secondary);
 font-style: italic;
 text-align: center;
 padding-top: 40px;
}
#bottom {
 grid-area: bottom;
 background: var(--bg-panel);
 border-top: 1px solid var(--border);
 display: flex;
 align-items: center;
 padding: 0 10px;
 gap: 12px;
 font-size: 10px;
}
#profile-bar {
 flex: 1;
 display: flex;
 align-items: center;
 gap: 1px;
 height: 20px;
 overflow: hidden;
}
.profile-block {
 height: 100%;
 border-radius: 2px;
 cursor: pointer;
 transition: opacity 0.2s;
 position: relative;
}
.profile-block:hover { opacity: 0.8; }
#legend-bar {
 display: flex;
 align-items: center;
 gap: 8px;
 border-left: 1px solid var(--border);
 padding-left: 10px;
}
.leg-dot {
 width: 8px;
 height: 8px;
 border-radius: 2px;
 display: inline-block;
 margin-right: 2px;
}
#status-bar {
 border-left: 1px solid var(--border);
 padding-left: 10px;
 display: flex;
 align-items: center;
 gap: 6px;
}
.status-dot { width: 6px; height: 6px; border-radius: 50%; display: inline-block; }
.tooltip {
 position: absolute;
 background: var(--bg-card);
 border: 1px solid var(--border);
 border-radius: 4px;
  padding: 8px 10px;
  font-size: 10.5px;
  font-family: var(--font-mono);
  pointer-events: none;
  z-index: 1000;
  max-width: 320px;
  line-height: 1.45;
  box-shadow: 0 8px 24px rgba(0,0,0,0.55);
  display: none;
}
@keyframes nodePulse {
 0% { filter: drop-shadow(0 0 2px var(--running)); }
 50% { filter: drop-shadow(0 0 9px var(--running)); }
 100% { filter: drop-shadow(0 0 2px var(--running)); }
}
@keyframes statusPulse {
 0% { opacity: 1; }
 50% { opacity: 0.32; }
 100% { opacity: 1; }
}
.node-running .node-card {
 animation: nodePulse 1.5s ease-in-out infinite;
}
.node-card {
 rx: 6;
 ry: 6;
 cursor: pointer;
 transition: filter 0.18s ease-out;
}
.node-card:hover { filter: brightness(1.18); }
.node-typebar {
 rx: 6;
 ry: 6;
 pointer-events: none;
}
.node-text {
 pointer-events: none;
 font-family: var(--font-mono);
}
.node-type-label {
 pointer-events: none;
 font-family: var(--font-ui);
 letter-spacing: 0.12em;
 text-transform: uppercase;
}
.status-glyph-run { animation: statusPulse 1.4s ease-in-out infinite; }
.node-badge {
 cursor: default;
}
.node-meta {
 pointer-events: none;
 font-family: var(--font-mono);
 font-size: 8.5px;
 fill: var(--text-secondary);
}
.node-dot { cursor: pointer; }
.node-detail {
 pointer-events: none;
 font-family: var(--font-mono);
 font-size: 6.5px;
 fill: var(--text-secondary);
 opacity: 0.85;
}
.pill { cursor: pointer; }
.pill .cap { fill: var(--bg-card); stroke: var(--border); stroke-width: 1.25; transition: stroke 0.15s, fill 0.15s; }
.pill:hover .cap { stroke: var(--accent); fill: rgba(83,216,251,0.12); }
.pill text {
 font-family: var(--font-mono);
 font-size: 9px;
 font-weight: 600;
 fill: var(--text-primary);
 text-anchor: middle;
 dominant-baseline: central;
 pointer-events: none;
}
.pill:hover text { fill: var(--accent); }
#stats-readout {
 font-family: var(--font-mono);
 font-size: 9.5px;
 color: var(--text-secondary);
 margin-left: auto;
}
#stats-readout b { color: var(--accent); font-weight: 600; }
.edge-path {
 fill: none;
 stroke: var(--border);
 stroke-width: 1.5;
 transition: stroke 0.25s ease-out;
}
.edge-path.active {
 stroke: var(--accent);
 stroke-dasharray: 6 4;
 animation: dashScroll 1s linear infinite;
}
@keyframes dashScroll {
 to { stroke-dashoffset: -10; }
}
.node-ring {
 fill: none;
 stroke: transparent;
 stroke-width: 2;
 rx: 6;
 ry: 6;
 transition: stroke 0.18s ease-out;
 pointer-events: none;
}
.node-ring.selected { stroke: var(--accent); }
.node-ring.highlight { stroke: var(--running); }
::selection { background: rgba(83,216,251,0.28); color: var(--text-primary); }
* { scrollbar-width: thin; scrollbar-color: var(--border) transparent; }
*::-webkit-scrollbar { width: 9px; height: 9px; }
*::-webkit-scrollbar-thumb { background: var(--border); border-radius: 5px; }
*::-webkit-scrollbar-thumb:hover { background: var(--text-secondary); }
:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
.status-shape { display: inline-block; width: 7px; height: 7px; box-sizing: border-box; }
.status-sq { border-radius: 1px; }
.status-ring { border-radius: 50%; border: 1.6px solid; background: transparent; }
.status-dotrun { border-radius: 50%; animation: statusPulse 1.4s ease-in-out infinite; }
.tl-run { border-radius: 50%; animation: statusPulse 1.4s ease-in-out infinite; }
.tl-sq { border-radius: 1px; }
.tl-ring { border-radius: 50%; border: 1.5px solid; background: transparent; box-sizing: border-box; }
#plan-select { max-width: 170px; text-transform: none; letter-spacing: 0.02em; }
#plan-activity { display: flex; align-items: center; gap: 4px; }
.plan-dot { width: 8px; height: 8px; border-radius: 50%; background: var(--text-secondary); opacity: 0.4; cursor: pointer; }
.plan-dot.ticking { background: var(--running); opacity: 1; animation: statusPulse 1.4s ease-in-out infinite; }
.plan-dot.current { opacity: 1; outline: 1px solid var(--accent); outline-offset: 1px; }
#minimap {
 position: absolute;
 bottom: 10px;
 right: 10px;
 width: 140px;
 height: 100px;
 background: rgba(10,17,29,0.88);
 border: 1px solid var(--border);
 border-radius: 6px;
 overflow: hidden;
 z-index: 40;
 box-shadow: 0 8px 24px rgba(0,0,0,0.55);
}
#minimap-svg { display: block; }
#minimap-viewport {
 position: absolute;
 border: 1px solid var(--accent);
 background: rgba(83,216,251,0.10);
 pointer-events: none;
 box-sizing: border-box;
}
#shortcuts-overlay {
 position: fixed;
 inset: 0;
 background: rgba(4,7,13,0.72);
 z-index: 2000;
 align-items: center;
 justify-content: center;
}
#shortcuts-panel {
  background: var(--bg-panel);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 16px 20px;
  width: min(440px, calc(100vw - 24px));
  max-width: calc(100vw - 24px);
  box-shadow: 0 16px 48px rgba(0,0,0,0.6);
}
</style>
</head>
<body>
<a id="skip-link" href="#svg-container" style="position:absolute;left:-9999px;top:0;z-index:3000;background:var(--bg-panel);color:var(--accent);padding:6px 12px;border:1px solid var(--accent);">Skip to tree canvas</a>
<div id="app">
  <header id="header" role="banner">
    <div id="header-left">
      <h1>PA-BT // DEBUG</h1>
      <select id="plan-select" class="btn" title="Switch plan" aria-label="Switch plan" style="padding:2px 6px;font-size:10px;"><option value="">—</option></select>
      <div id="plan-activity" title="Per-plan activity"></div>
      <div id="conn-dot" aria-hidden="true"></div>
      <span id="conn-status" role="status" aria-live="polite">Connecting...</span>
    </div>
    <div id="search-wrap">
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="color:#888;vertical-align:middle;"><circle cx="11" cy="11" r="8"></circle><line x1="21" y1="21" x2="16.65" y2="16.65"></line></svg>
      <input id="search-input" type="text" placeholder="Search nodes..." aria-label="Search nodes" autocomplete="off">
      <span id="search-count" title="Matches in the current tree"></span>
      <button class="btn" id="search-prev" title="Previous match (Shift+Enter)" aria-label="Previous search match" style="padding:2px 6px;">&#8593;</button>
      <button class="btn" id="search-next" title="Next match (Enter)" aria-label="Next search match" style="padding:2px 6px;">&#8595;</button>
      <button class="btn" id="search-clear" title="Clear search (Escape)" aria-label="Clear search" style="padding:2px 6px;">&#10005;</button>
    </div>
    <div id="header-right">
      <button class="btn" id="btn-save" title="Save Session (File System Access API or download)">Save</button>
      <button class="btn" id="btn-load" title="Load Session (File System Access API or upload)">Load</button>
      <button class="btn" id="btn-export" title="Export DOT">Export DOT</button>
      <button class="btn" id="btn-diff" title="Diff">Diff</button>
      <button class="btn btn-icon" id="btn-shortcuts" title="Keyboard shortcuts (?)" aria-label="Keyboard shortcuts">?</button>
      <button class="btn btn-icon" id="btn-fullscreen" title="Fullscreen" aria-label="Toggle fullscreen">&#9974;</button>
    </div>
  </header>

  <div id="resize-tl" role="separator" aria-orientation="vertical" aria-label="Resize timeline panel"></div>
  <aside id="timeline" role="navigation" aria-label="Tick timeline">
    <div id="timeline-header">
      <span>Timeline</span>
      <div>
        <button class="btn" id="btn-next-fail" title="Jump to next failure (F)">&#8595;F</button>
        <button class="btn" id="btn-next-change" title="Jump to next structural change (C)">&#8595;C</button>
        <button class="btn" id="btn-live">Live</button>
        <button class="btn" id="btn-play">&#9654;</button>
        <select class="btn" id="speed-select" title="Playback speed" aria-label="Playback speed" style="padding:2px 4px;">
          <option value="0.5">0.5x</option>
          <option value="1" selected>1x</option>
          <option value="2">2x</option>
          <option value="5">5x</option>
        </select>
      </div>
    </div>
    <div id="timeline-list" role="log" aria-live="off" aria-label="Tick history" tabindex="0"></div>
  </aside>

  <main id="main">
    <div id="main-toolbar">
      <button class="btn" id="btn-fit">Fit</button>
      <button class="btn" id="btn-reset">Reset</button>
      <button class="btn" id="btn-expand-all" title="Expand every branch">Expand all</button>
      <button class="btn" id="btn-collapse-all" title="Collapse to the active spine">Collapse all</button>
      <button class="btn" id="btn-why" title="Toggle causal-link overlay (precondition → action)">Why?</button>
      <span class="heat-ctrl">
        <button class="btn btn-heat" data-heat="off" title="No glow">Off</button>
        <button class="btn btn-heat" data-heat="heat" title="Glow by tick frequency">Heat</button>
        <button class="btn btn-heat" data-heat="fail" title="Glow by failure rate">Fail</button>
        <button class="btn btn-heat" data-heat="recent" title="Glow on recently ticked">Recent</button>
      </span>
      <span id="stats-readout"></span>
      <span id="zoom-level" style="font-size:10px;color:var(--text-secondary);">100%</span>
    </div>
    <div id="breadcrumb-bar"></div>
    <div id="narration-bar"></div>
    <div id="scrub-strip" title="Drag to scrub the timeline"></div>
    <div id="svg-container" tabindex="-1">
      <svg id="tree-svg" xmlns="http://www.w3.org/2000/svg" role="tree" aria-label="PA-BT planning tree"></svg>
      <div id="conn-banner" role="status"></div>
      <div id="minimap" title="Overview — click to navigate">
        <svg id="minimap-svg" width="140" height="100"></svg>
        <div id="minimap-viewport"></div>
      </div>
    </div>
    <div id="tooltip" class="tooltip"></div>
  </main>

  <div id="resize-dt" role="separator" aria-orientation="vertical" aria-label="Resize detail panel"></div>
  <aside id="detail" role="complementary" aria-label="Node detail panel">
    <div id="detail-header">Detail Panel</div>
    <div id="detail-content">
      <div id="detail-empty">Select a node to view details</div>
      <div id="detail-body" style="display:none;">
        <div class="dtl-section">
          <div class="dtl-label">Name</div>
          <div class="dtl-value" id="dtl-name"></div>
        </div>
        <div class="dtl-section">
          <div class="dtl-label">Type</div>
          <div class="dtl-value" id="dtl-type"></div>
        </div>
        <div class="dtl-section" id="dtl-status-wrap">
          <div class="dtl-label">Status</div>
          <div class="dtl-value" id="dtl-status"></div>
        </div>
        <div class="dtl-section" id="dtl-frame-wrap" style="display:none;">
          <div class="dtl-label">Frame</div>
          <div class="dtl-value" id="dtl-frame"></div>
        </div>
        <div class="dtl-section" id="dtl-effects-wrap" style="display:none;">
          <div class="dtl-label">Effects</div>
          <div class="dtl-value" id="dtl-effects"></div>
        </div>
        <div class="dtl-section" id="dtl-cond-wrap" style="display:none;">
          <div class="dtl-label">Condition</div>
          <div class="dtl-value" id="dtl-cond"></div>
        </div>
        <div class="dtl-section" id="dtl-post-wrap" style="display:none;">
          <div class="dtl-label">Post-Condition</div>
          <div class="dtl-value" id="dtl-post"></div>
        </div>
        <div class="dtl-section" id="dtl-profile-wrap" style="display:none;">
          <div class="dtl-label">Profile</div>
          <div class="dtl-value" id="dtl-profile"></div>
        </div>
        <div class="dtl-section">
          <button class="btn" id="btn-breakpoint" style="width:100%;">Toggle Breakpoint</button>
        </div>
      </div>
    </div>
  </aside>

  <footer id="bottom">
    <div id="profile-bar"></div>
    <div id="legend-bar">
      <span class="leg-dot" style="background:var(--type-goal)"></span>Goal
      <span class="leg-dot" style="background:var(--type-ppa)"></span>PPA
      <span class="leg-dot" style="background:var(--type-action)"></span>Action
      <span class="leg-dot" style="background:var(--type-actionnode)"></span>ActionNode
      <span class="leg-dot" style="background:var(--type-pre)"></span>Pre
      <span class="leg-dot" style="background:var(--type-unknown)"></span>Unknown
    </div>
    <div id="status-bar">
      <span class="status-shape status-sq" style="background:var(--success)"></span>S
      <span class="status-shape status-ring" style="border-color:var(--failure)"></span>F
      <span class="status-shape status-dotrun" style="background:var(--running)"></span>R
    </div>
  </footer>
</div>

<div id="diff-modal" style="display:none;position:fixed;top:0;left:0;width:100%;height:100%;background:rgba(0,0,0,0.7);z-index:2000;align-items:center;justify-content:center;">
  <div style="background:var(--bg-panel);border:1px solid var(--border);border-radius:6px;padding:16px;min-width:240px;">
    <div style="color:var(--accent);font-size:12px;margin-bottom:8px;">Diff</div>
    <label style="font-size:10px;color:var(--text-secondary);">From: <input type="number" id="diff-from" class="btn" style="width:60px;text-align:center;"></label>
    <label style="font-size:10px;color:var(--text-secondary);margin-left:8px;">To: <input type="number" id="diff-to" class="btn" style="width:60px;text-align:center;"></label>
    <div style="margin-top:10px;display:flex;gap:6px;justify-content:flex-end;">
      <button class="btn" id="btn-diff-cancel">Cancel</button>
      <button class="btn" id="btn-diff-run">Run</button>
    </div>
  </div>
</div>

<div id="shortcuts-overlay" style="display:none;">
  <div id="shortcuts-panel">
    <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:10px;">
      <strong style="font-family:var(--font-mono);font-size:12px;color:var(--accent);letter-spacing:0.1em;">KEYBOARD SHORTCUTS</strong>
      <button class="btn" id="btn-close-shortcuts" style="padding:2px 8px;">&#10005;</button>
    </div>
    <table style="width:100%;border-collapse:collapse;font-family:var(--font-mono);font-size:10px;">
      <tr><td style="padding:3px 8px;color:var(--accent);">?</td><td style="padding:3px 8px;color:var(--text-secondary);">Toggle this overlay</td></tr>
      <tr><td style="padding:3px 8px;color:var(--accent);">drag / scroll</td><td style="padding:3px 8px;color:var(--text-secondary);">Pan the canvas</td></tr>
      <tr><td style="padding:3px 8px;color:var(--accent);">pinch / &#8984;+scroll</td><td style="padding:3px 8px;color:var(--text-secondary);">Zoom at cursor</td></tr>
      <tr><td style="padding:3px 8px;color:var(--accent);">+ / &#8722; / 0</td><td style="padding:3px 8px;color:var(--text-secondary);">Zoom in / out / reset view</td></tr>
      <tr><td style="padding:3px 8px;color:var(--accent);">Space</td><td style="padding:3px 8px;color:var(--text-secondary);">Play/pause playback</td></tr>
      <tr><td style="padding:3px 8px;color:var(--accent);">L</td><td style="padding:3px 8px;color:var(--text-secondary);">Toggle live mode</td></tr>
      <tr><td style="padding:3px 8px;color:var(--accent);">&#8592; / &#8594;</td><td style="padding:3px 8px;color:var(--text-secondary);">Step timeline one iteration</td></tr>
      <tr><td style="padding:3px 8px;color:var(--accent);">Home / End</td><td style="padding:3px 8px;color:var(--text-secondary);">First / last iteration</td></tr>
      <tr><td style="padding:3px 8px;color:var(--accent);">F</td><td style="padding:3px 8px;color:var(--text-secondary);">Jump to next failure</td></tr>
      <tr><td style="padding:3px 8px;color:var(--accent);">C</td><td style="padding:3px 8px;color:var(--text-secondary);">Jump to next structural change</td></tr>
      <tr><td style="padding:3px 8px;color:var(--accent);">E</td><td style="padding:3px 8px;color:var(--text-secondary);">Expand all nodes</td></tr>
      <tr><td style="padding:3px 8px;color:var(--accent);">W</td><td style="padding:3px 8px;color:var(--text-secondary);">Toggle Why? causal overlay</td></tr>
      <tr><td style="padding:3px 8px;color:var(--accent);">1&#8211;4</td><td style="padding:3px 8px;color:var(--text-secondary);">Heat mode: Off / Heat / Fail / Recent</td></tr>
      <tr><td style="padding:3px 8px;color:var(--accent);">Enter / Shift+Enter</td><td style="padding:3px 8px;color:var(--text-secondary);">Next / previous search match</td></tr>
      <tr><td style="padding:3px 8px;color:var(--accent);">Escape</td><td style="padding:3px 8px;color:var(--text-secondary);">Clear search / close overlay</td></tr>
    </table>
  </div>
</div>

<script>
(function() {
  const SVG_NS = 'http://www.w3.org/2000/svg';
  const NODE_W = 210;
  const NODE_H = 54;
  const COL_W = 274;
  const ROW_H = 70;
  const GAP = 20;
  const UNIT = ROW_H;

  function cssVar(name, fallback) {
    var v = getComputedStyle(document.documentElement).getPropertyValue(name);
    return (v && v.trim()) ? v.trim() : fallback;
  }
  const TYPE_COLORS = {
    GoalRoot: cssVar('--type-goal', '#5aa2ff'), GoalSelector: cssVar('--type-goal', '#5aa2ff'),
    PPARoot: cssVar('--type-ppa', '#c792ea'), PPAPost: cssVar('--type-ppa', '#c792ea'),
    ActionSelector: cssVar('--type-action', '#ff9e64'), ActionRoot: cssVar('--type-action', '#ff9e64'),
    ActionNode: cssVar('--type-actionnode', '#46d68c'),
    PreconditionsRoot: cssVar('--type-pre', '#2dd4bf'), PreconditionLeaf: cssVar('--type-pre', '#2dd4bf'),
    Unknown: cssVar('--type-unknown', '#50627a')
  };
  const STATUS_COLORS = { 1: cssVar('--running', '#f5a524'), 2: cssVar('--success', '#46d68c'), 3: cssVar('--failure', '#f0566a') };

  let eventSource = null, reconnectDelay = 1000;
  let currentTree = null, selectedNodeId = null, highlightedIds = new Set();
  const MAX_TIMELINE_EVENTS = 200;
  let timelineEvents = [], currentIteration = null;
  let pendingTree = null, rafId = null, pendingDelta = null, pendingOldTree = null, pendingOldLayout = null;
  let keyframeTree = null, lastReconstructedTree = null, lastReconstructedIteration = null;
  let liveMode = true, playback = false, playbackIdx = 0, playbackRaf = null, playbackLastTime = 0, playbackSpeed = 1;
  let pan = { x: 0, y: 0 }, zoom = 1, isDragging = false, dragStart = null, panStart = { x: 0, y: 0 };
  let renderedTier = null;
  function zoomTier() { return zoom < 0.5 ? 0 : (zoom > 1.5 ? 2 : 1); }
  let userAdjustedView = false, initialFitDone = false;
  let profileCache = null, profileCacheIter = null;
  let lastSeq = 0;
  let serverOldestIter = null, evictedNoticeShown = false;
  let searchMatches = [], searchMatchIdx = -1;
  let currentPlanId = null;
  let lastEventAt = Date.now(), disconnectedAt = null, connProbeTimer = null;
  const planSelect = document.getElementById('plan-select');
  const planActivity = document.getElementById('plan-activity');
  const planState = {};
  const planLastCounts = {};
  let planProbeTimer = null;
  let pendingRestoreScroll = null, pendingRestoreNodeId = null;

  const svg = document.getElementById('tree-svg');
  const svgContainer = document.getElementById('svg-container');
  const tooltip = document.getElementById('tooltip');
  const timelineList = document.getElementById('timeline-list');
  const connDot = document.getElementById('conn-dot');
  const connStatus = document.getElementById('conn-status');
  const searchInput = document.getElementById('search-input');
  const profileBar = document.getElementById('profile-bar');
  const btnLive = document.getElementById('btn-live');
  const btnPlay = document.getElementById('btn-play');
  const zoomLevel = document.getElementById('zoom-level');

  function escapeHtml(str) {
    if (str == null) return '';
    return String(str).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  }
  function escapeAttr(str) {
    if (str == null) return '';
    return String(str).replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/'/g, '&#39;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
  }
  function safeId(id) { return 'node-' + String(id).replace(/\./g, '-'); }
  function truncate(s, n) { s = String(s); return s.length > n ? s.slice(0, n - 1) + '\u2026' : s; }
  function makeStatusGlyph(cx, cy, status, color) {
    if (status === 1) {
      var d = document.createElementNS(SVG_NS, 'circle');
      d.setAttribute('cx', cx); d.setAttribute('cy', cy); d.setAttribute('r', '4');
      d.setAttribute('fill', color); d.setAttribute('class', 'status-glyph-run');
      return d;
    }
    if (status === 2) {
      var s = document.createElementNS(SVG_NS, 'rect');
      s.setAttribute('x', cx - 3.5); s.setAttribute('y', cy - 3.5);
      s.setAttribute('width', '7'); s.setAttribute('height', '7');
      s.setAttribute('fill', color); s.setAttribute('class', 'node-badge');
      return s;
    }
    if (status === 3) {
      var r = document.createElementNS(SVG_NS, 'circle');
      r.setAttribute('cx', cx); r.setAttribute('cy', cy); r.setAttribute('r', '3.5');
      r.setAttribute('fill', 'none'); r.setAttribute('stroke', color); r.setAttribute('stroke-width', '1.6');
      r.setAttribute('class', 'node-badge');
      return r;
    }
    return null;
  }
  function statusToString(s) { switch(s){ case 1: return 'Running'; case 2: return 'Success'; case 3: return 'Failure'; default: return 'Unknown'; } }
  function cloneTreeJS(tree) { return tree ? JSON.parse(JSON.stringify(tree)) : null; }
  function parentIdJS(id) { var idx = id.lastIndexOf('.'); return idx === -1 ? '' : id.substring(0, idx); }
  function parseIndexJS(s) { if (!s) return -1; for (var i=0;i<s.length;i++){ var c=s.charCodeAt(i); if(c<48||c>57) return -1;} return parseInt(s,10); }
  function nodesDifferJS(a,b){
    if (a.name!==b.name || a.nodeType!==b.nodeType || a.condition!==b.condition || a.postCondition!==b.postCondition || a.frame!==b.frame || a.structureHash!==b.structureHash) return true;
    var sa=a.status, sb=b.status;
    if (!sa && sb || sa && !sb) return true;
    if (sa && sb) { if (sa.LastStatus!==sb.LastStatus || sa.TickCount!==sb.TickCount) return true; }
    var ea=a.effects||[], eb=b.effects||[];
    if (ea.length!==eb.length) return true;
    for (var i=0;i<ea.length;i++){ if(ea[i].key!==eb[i].key || ea[i].value!==eb[i].value) return true; }
    return false;
  }
  function flattenTreeMapJS(tree, map){
    if(!tree) return;
    map[tree.id]=tree;
    if(tree.children){ for(var i=0;i<tree.children.length;i++) flattenTreeMapJS(tree.children[i], map); }
  }
  function applyDeltaJS(base, delta){
    if(!delta) return cloneTreeJS(base);
    if(!base){
      if(delta.added && delta.added.length){
        for(var i=0;i<delta.added.length;i++){ if(delta.added[i].id==='0') return cloneTreeJS(delta.added[i]); }
      }
      return null;
    }
    var result=cloneTreeJS(base);
    var idMap={}; flattenTreeMapJS(result, idMap);
    // Removed
    if(delta.removed){
      for(var r=0;r<delta.removed.length;r++){
        var rid=delta.removed[r];
        var parent=parentIdJS(rid);
        if(!parent) continue;
        var parentNode=idMap[parent];
        if(!parentNode||!parentNode.children) continue;
        var suffix=rid.substring(parent.length+1);
        var idx=parseIndexJS(suffix);
        if(idx<0||idx>=parentNode.children.length) {
          var found=-1;
          for(var k=0;k<parentNode.children.length;k++){ if(parentNode.children[k].id===rid){ found=k; break; } }
          if(found===-1) continue;
          idx=found;
        } else if(parentNode.children[idx].id!==rid){
          var found2=-1;
          for(var k2=0;k2<parentNode.children.length;k2++){ if(parentNode.children[k2].id===rid){ found2=k2; break; } }
          if(found2===-1) continue;
          idx=found2;
        }
        parentNode.children.splice(idx,1);
        idMap={}; flattenTreeMapJS(result, idMap);
      }
    }
    // Added
    if(delta.added){
      for(var a=0;a<delta.added.length;a++){
        var added=delta.added[a];
        var parentA=parentIdJS(added.id);
        if(!parentA){
          result=cloneTreeJS(added);
          idMap={}; flattenTreeMapJS(result, idMap);
          continue;
        }
        var pNode=idMap[parentA];
        if(!pNode) continue;
        if(!pNode.children) pNode.children=[];
        var suffixA=added.id.substring(parentA.length+1);
        var idxA=parseIndexJS(suffixA);
        if(idxA<0) continue;
        var cloneA=cloneTreeJS(added);
        if(idxA>=pNode.children.length){ pNode.children.push(cloneA); }
        else { pNode.children.splice(idxA,0,cloneA); }
        idMap={}; flattenTreeMapJS(result, idMap);
      }
    }
    // Changed
    if(delta.changed){
      for(var c=0;c<delta.changed.length;c++){
        var ch=delta.changed[c];
        var node=idMap[ch.id];
        if(!node) continue;
        node.name=ch.name||'';
        node.nodeType=ch.nodeType||node.nodeType;
        if(ch.hasOwnProperty('condition')) node.condition=ch.condition;
        if(ch.hasOwnProperty('postCondition')) node.postCondition=ch.postCondition;
        if(ch.hasOwnProperty('frame')) node.frame=ch.frame;
        if(ch.hasOwnProperty('structureHash')) node.structureHash=ch.structureHash;
        if(ch.status) node.status=ch.status; else if(ch.hasOwnProperty('status')) node.status=ch.status;
        if(ch.effects) node.effects=ch.effects; else if(ch.hasOwnProperty('effects')) node.effects=ch.effects;
      }
    }
    return result;
  }

  function planPath(suffix) {
    return window.location.pathname.replace(/\/ui$/, '/plans/' + encodeURIComponent(currentPlanId || '0') + (suffix || ''));
  }
  function switchPlan(planId) {
    if (planId === currentPlanId) return;
    if (currentPlanId != null) planState[currentPlanId] = { scrollTop: timelineList.scrollTop, selectedNodeId: selectedNodeId };
    var restore = planState[planId] || null;
    pendingRestoreScroll = restore ? restore.scrollTop : null;
    pendingRestoreNodeId = restore ? restore.selectedNodeId : null;
    currentPlanId = planId;
    if (eventSource) { eventSource.close(); eventSource = null; }
    timelineEvents = []; currentIteration = null; currentTree = null; lastRawTree = null;
    profileCache = null; profileCacheIter = null; serverOldestIter = null; evictedNoticeShown = false;
    initialFitDone = false; userAdjustedView = false;
    highlightedIds.clear(); selectedNodeId = null;
    planActivity.querySelectorAll('.plan-dot').forEach(function(d){ d.classList.toggle('current', d.getAttribute('data-plan') === planId); });
    var nodesG = document.getElementById('nodes-layer');
    var edgesG = document.getElementById('edges-layer');
    if (nodesG) clearSvgChildren(nodesG);
    if (edgesG) clearSvgChildren(edgesG);
    ['tombstone-layer', 'conflict-overlay', 'causal-overlay'].forEach(function(lid) { var l = document.getElementById(lid); if (l) clearSvgChildren(l); });
    var bc = document.getElementById('breadcrumb-bar'); if (bc) bc.innerHTML = '';
    ensureTimelineVirtualSpacers();
    var n = virtualTopSpacer.nextSibling;
    while (n && n !== virtualBottomSpacer) { var nxt = n.nextSibling; n.remove(); n = nxt; }
    virtualTopSpacer.style.height = '0px'; virtualBottomSpacer.style.height = '0px';
    document.getElementById('detail-body').style.display = 'none';
    document.getElementById('detail-empty').style.display = '';
    startSSE(planId);
    fetchInitialTimeline();
  }
  planSelect.addEventListener('change', function() { switchPlan(this.value); });
  function renderPlanActivity(plans) {
    if (!planActivity || !plans) return;
    planActivity.innerHTML = '';
    for (var i = 0; i < plans.length; i++) {
      (function(p){
        var ticking = planLastCounts[p.id] != null && p.eventCount > planLastCounts[p.id];
        var d = document.createElement('div');
        d.className = 'plan-dot' + (p.id === currentPlanId ? ' current' : '') + (ticking ? ' ticking' : '');
        d.setAttribute('data-plan', p.id);
        d.title = p.id + ' \u2014 ' + p.eventCount + ' events';
        d.addEventListener('click', function(){ planSelect.value = p.id; switchPlan(p.id); });
        planLastCounts[p.id] = p.eventCount;
        planActivity.appendChild(d);
      })(plans[i]);
    }
  }
  function schedulePlanProbe() {
    if (planProbeTimer) return;
    planProbeTimer = setTimeout(function(){
      planProbeTimer = null;
      fetch(window.location.pathname.replace(/\/ui$/, '/plans'))
        .then(function(r){ return r.ok ? r.json() : null; })
        .then(function(plans){ if (plans) renderPlanActivity(plans); schedulePlanProbe(); })
        .catch(function(){ schedulePlanProbe(); });
    }, 2000);
  }
  function connect() {
    fetch(window.location.pathname.replace(/\/ui$/, '/plans'))
      .then(function(r) { return r.ok ? r.json() : []; })
      .then(function(plans) {
        planSelect.innerHTML = '';
        if (plans && plans.length > 0) {
          for (var i = 0; i < plans.length; i++) {
            var opt = document.createElement('option');
            opt.value = plans[i].id;
            opt.textContent = plans[i].id;
            planSelect.appendChild(opt);
          }
          currentPlanId = plans[0].id;
        } else {
          var opt = document.createElement('option');
          opt.value = '0'; opt.textContent = 'plan-0';
          planSelect.appendChild(opt);
          currentPlanId = '0';
        }
        planSelect.value = currentPlanId;
        renderPlanActivity(plans);
        schedulePlanProbe();
        startSSE(currentPlanId);
        fetchInitialTimeline();
      })
      .catch(function() { currentPlanId = '0'; startSSE('0'); fetchInitialTimeline(); });
  }
  function fetchInitialTimeline() {
    fetch(planPath('/timeline'))
      .then(function(r) { return r.ok ? r.json() : []; })
      .then(function(tl) {
        if (!tl || !tl.length) return;
        if (tl.length > MAX_TIMELINE_EVENTS) tl = tl.slice(tl.length - MAX_TIMELINE_EVENTS);
        timelineEvents = tl.map(function(e) {
          return { iteration: e.iteration, status: (e.status === 'Running' ? 1 : (e.status === 'Success' ? 2 : (e.status === 'Failure' ? 3 : 0))), durationMs: e.durationMs || 0, seq: 0, nodeCount: e.nodeCount || 0 };
        });
        if (timelineEvents.length && serverOldestIter == null) serverOldestIter = timelineEvents[0].iteration;
        currentIteration = timelineEvents.length ? timelineEvents[timelineEvents.length - 1].iteration : null;
        renderTimelineVirtual();
        if (pendingRestoreScroll != null) { timelineList.scrollTop = pendingRestoreScroll; pendingRestoreScroll = null; }
        if (currentIteration != null) fetchTimelineIter(currentIteration);
      })
      .catch(function() {});
  }
  function setConnBanner(kind, text) {
    var b = document.getElementById('conn-banner');
    if (!b) return;
    if (!kind) { b.style.display = 'none'; b.textContent = ''; b.className = ''; return; }
    b.textContent = text;
    b.className = (kind === 'stale') ? 'stale' : '';
    b.style.display = 'block';
  }
  function scheduleConnProbe() {
    if (connProbeTimer) return;
    connProbeTimer = setTimeout(function() {
      connProbeTimer = null;
      var idle = Date.now() - lastEventAt;
      var b = document.getElementById('conn-banner');
      if (disconnectedAt == null && liveMode && idle > 10000) {
        setConnBanner('stale', 'Data stale \u2014 no events for ' + Math.round(idle / 1000) + 's');
        svg.classList.add('stale');
      } else if (disconnectedAt == null && b && b.className === 'stale') {
        setConnBanner(null);
        svg.classList.remove('stale');
      }
      scheduleConnProbe();
    }, 2000);
  }
  function startSSE(planId) {
    const url = window.location.pathname.replace(/\/ui$/, '/plans/' + encodeURIComponent(planId) + '/events');
    eventSource = new EventSource(url);
    eventSource.onopen = function() {
      connDot.classList.add('connected');
      connDot.style.background = '';
      connStatus.textContent = 'Connected';
      reconnectDelay = 1000;
      lastSeq = 0;
      disconnectedAt = null;
      lastEventAt = Date.now();
      setConnBanner(null);
      svg.classList.remove('stale');
      scheduleConnProbe();
    };
    let ssePending = null, sseTimer = null, lastSseAt = 0;
    const SSE_MIN_INTERVAL = 33;
    function drainSse() {
      sseTimer = null;
      const ev = ssePending;
      ssePending = null;
      if (ev) handleSseEvent(ev);
    }
    eventSource.onmessage = function(e) {
      const event = JSON.parse(e.data);
      const now = Date.now();
      if (now - lastSseAt < SSE_MIN_INTERVAL) {
        ssePending = event;
        if (!sseTimer) sseTimer = setTimeout(drainSse, SSE_MIN_INTERVAL - (now - lastSseAt));
        return;
      }
      lastSseAt = now;
      handleSseEvent(event);
    };
    function handleSseEvent(event) {
      if (event.seq && lastSeq > 0 && event.seq > lastSeq + 1) {
        connDot.classList.remove('connected');
        connDot.style.background = cssVar('--running', '#f5a524');
        connStatus.textContent = 'Events missed (seq ' + lastSeq + ' to ' + event.seq + ')';
      }
      lastSeq = event.seq || 0;
      lastEventAt = Date.now();
      if (event.oldestIteration != null) serverOldestIter = event.oldestIteration;
      addTimelineEvent(event);
      if (liveMode) {
        var treeToRender = null;
        if (event.tree) {
          keyframeTree = cloneTreeJS(event.tree);
          lastReconstructedTree = cloneTreeJS(event.tree);
          lastReconstructedIteration = event.iteration;
          treeToRender = event.tree;
        } else if (event.delta) {
          if (!lastReconstructedTree || lastReconstructedIteration == null) {
            fetchTimelineIter(event.iteration);
            return;
          }
          if (event.delta.baseIteration != null && event.delta.baseIteration !== lastReconstructedIteration) {
            // Base mismatch (missed intermediate deltas) — fetch full snapshot.
            fetchTimelineIter(event.iteration);
            return;
          }
          treeToRender = applyDeltaJS(lastReconstructedTree, event.delta);
          if (!treeToRender) {
            fetchTimelineIter(event.iteration);
            return;
          }
          lastReconstructedTree = cloneTreeJS(treeToRender);
          lastReconstructedIteration = event.iteration;
          if (event.isKeyframe && event.tree) {
            keyframeTree = cloneTreeJS(event.tree);
          }
        } else if (event.isKeyframe) {
          fetchTimelineIter(event.iteration);
          return;
        }
        if (treeToRender) {
          pendingTree = treeToRender;
          pendingDelta = event.delta || null;
          pendingOldTree = currentTree;
          pendingOldLayout = currentLayoutMap;
          updateNarration(event);
          if (pendingDelta && pendingDelta.removed && pendingDelta.removed.length) {
            flashReplanRemoved(pendingDelta);
          }
          if (!rafId) {
            rafId = requestAnimationFrame(function() {
              if (pendingTree) renderTree(pendingTree);
              if (pendingDelta) {
                flashReplanAdded(pendingDelta);
                annotateConflictReorder(pendingDelta, pendingOldTree, pendingOldLayout);
              }
              rafId = null;
              pendingTree = null;
              pendingDelta = null;
              pendingOldTree = null;
              pendingOldLayout = null;
            });
          }
        }
      }
    };
    eventSource.onerror = function() {
      connDot.classList.remove('connected');
      disconnectedAt = Date.now();
      connStatus.textContent = 'Disconnected';
      setConnBanner('disconnected', 'Server disconnected \u2014 last iteration ' + (currentIteration != null ? currentIteration : '?') + ' \u2014 reconnecting');
      svg.classList.add('stale');
      eventSource.close();
      setTimeout(function() {
        reconnectDelay = Math.min(reconnectDelay * 2, 30000);
        connStatus.textContent = 'Reconnecting in ' + (reconnectDelay/1000) + 's...';
        connect();
      }, reconnectDelay);
    };
  }

  // ---- Virtual Timeline (D9/D10) ----
  const VIRTUAL_ROW_HEIGHT = 24;
  const VIRTUAL_BUFFER = 8;
  let virtualTopSpacer = null, virtualBottomSpacer = null;
  let virtualFetchingOlder = false;
  let virtualPendingRender = false;
  function ensureTimelineVirtualSpacers() {
    if (virtualTopSpacer && virtualBottomSpacer) return;
    virtualTopSpacer = document.createElement('div');
    virtualTopSpacer.id = 'tl-top-spacer';
    virtualBottomSpacer = document.createElement('div');
    virtualBottomSpacer.id = 'tl-bottom-spacer';
    // Insert spacers if not already present; rows will be between them.
    if (timelineList.firstChild) {
      timelineList.insertBefore(virtualTopSpacer, timelineList.firstChild);
      timelineList.appendChild(virtualBottomSpacer);
    } else {
      timelineList.appendChild(virtualTopSpacer);
      timelineList.appendChild(virtualBottomSpacer);
    }
  }
  function createTimelineRowElement(ev) {
    const row = document.createElement('div');
    if (ev.evicted) {
      row.className = 'tl-row tl-gap';
      row.dataset.iter = ev.iteration;
      row.innerHTML = '<span>iterations &le; ' + escapeHtml(ev.iteration) + ' evicted &mdash; Load to restore</span>';
      return row;
    }
    row.className = 'tl-row' + (ev.iteration === currentIteration ? ' selected' : '');
    row.dataset.iter = ev.iteration;
    const st = statusToString(ev.status);
    const sc = STATUS_COLORS[ev.status] || '#888';
    // Use DOM construction for dot to avoid innerHTML fragment parsing cost; keep innerHTML for row template (not container wipe)
    var dotClass = 'tl-dot' + (ev.status === 1 ? ' tl-run' : (ev.status === 2 ? ' tl-sq' : (ev.status === 3 ? ' tl-ring' : '')));
    var dotStyle = ev.status === 3 ? 'border-color:' + escapeAttr(sc) : 'background:' + escapeAttr(sc);
    row.innerHTML = '<span class="' + dotClass + '" style="' + dotStyle + '"></span>' +
                    '<span class="tl-iter">#' + escapeHtml(ev.iteration) + '</span>' +
                    '<span class="tl-status" style="color:' + escapeAttr(sc) + '">' + escapeHtml(st) + '</span>' +
                    '<span class="tl-duration">' + (ev.durationMs ? ev.durationMs.toFixed(1) + 'ms' : '') + '</span>';
    row.addEventListener('click', function() {
      currentIteration = ev.iteration;
      updateTimelineSelection();
      fetchTimelineIter(ev.iteration);
    });
    return row;
  }
  function getTimelineViewport() {
    return {
      height: timelineList.clientHeight || 400,
      scrollTop: timelineList.scrollTop || 0
    };
  }
  function scheduleTimelineVirtualRender() {
    if (virtualPendingRender) return;
    virtualPendingRender = true;
    requestAnimationFrame(function() {
      virtualPendingRender = false;
      renderTimelineVirtual();
    });
  }
  function renderTimelineVirtual() {
    ensureTimelineVirtualSpacers();
    const total = timelineEvents.length;
    if (total === 0) {
      // No events: clear rows between spacers.
      let n = virtualTopSpacer.nextSibling;
      while (n && n !== virtualBottomSpacer) { const nxt = n.nextSibling; n.remove(); n = nxt; }
      virtualTopSpacer.style.height = '0px';
      virtualBottomSpacer.style.height = '0px';
      return;
    }
    const vp = getTimelineViewport();
    const rowH = VIRTUAL_ROW_HEIGHT;
    const visibleCount = Math.ceil(vp.height / rowH) + VIRTUAL_BUFFER * 2;
    let start = Math.floor(vp.scrollTop / rowH) - VIRTUAL_BUFFER;
    if (start < 0) start = 0;
    let end = start + visibleCount;
    if (end > total) { end = total; start = Math.max(0, end - visibleCount); }
    // Update spacers
    virtualTopSpacer.style.height = (start * rowH) + 'px';
    virtualBottomSpacer.style.height = ((total - end) * rowH) + 'px';
    // Reuse existing rows map for minimal DOM churn is complex; for now rebuild visible slice via DocumentFragment.
    // Remove old visible rows (between spacers).
    let n = virtualTopSpacer.nextSibling;
    while (n && n !== virtualBottomSpacer) { const nxt = n.nextSibling; n.remove(); n = nxt; }
    const frag = document.createDocumentFragment();
    for (let i = start; i < end; i++) {
      const ev = timelineEvents[i];
      frag.appendChild(createTimelineRowElement(ev));
    }
    timelineList.insertBefore(frag, virtualBottomSpacer);
    // Lazy: if near top and we have older history on server, fetch.
    if (vp.scrollTop < rowH * 2 && !virtualFetchingOlder && total > 0) {
      const oldest = timelineEvents[0] ? timelineEvents[0].iteration : null;
      if (oldest != null && oldest > 1) {
        maybeFetchOlderTimeline(oldest);
      }
    }
  }
  function maybeFetchOlderTimeline(oldestIter) {
    if (virtualFetchingOlder) return;
    // Fetch up to 20 older entries via individual GET /timeline/{iter}
    virtualFetchingOlder = true;
    const batch = 20;
    const floor = (serverOldestIter != null) ? serverOldestIter : 1;
    let toFetch = Math.min(batch, oldestIter - floor);
    if (toFetch <= 0) { virtualFetchingOlder = false; return; }
    let fetched = [];
    let pending = toFetch;
    for (let k = 0; k < toFetch; k++) {
      const iter = oldestIter - 1 - k;
      fetch(planPath('/timeline/' + iter))
        .then(function(r){
          if (r.status === 404) {
            if (serverOldestIter == null || iter < serverOldestIter) serverOldestIter = iter + 1;
            if (!evictedNoticeShown) { evictedNoticeShown = true; console.info('pabtdebug: iteration ' + iter + ' evicted from the sliding window; use Load to restore older history.'); }
            return null;
          }
          return r.ok ? r.json() : null;
        })
        .then(function(data){
          if (data && data.iteration) {
            fetched.push({ iteration: data.iteration, status: data.status, durationMs: data.durationMs, seq: data.seq || 0, nodeCount: data.nodeCount || 0 });
          }
        })
        .catch(function(){})
        .finally(function(){
          pending--;
          if (pending === 0) {
            if (fetched.length) {
              fetched.sort(function(a,b){ return a.iteration - b.iteration; });
              const prevTop = timelineList.scrollTop;
              const prevHeight = timelineEvents.length * VIRTUAL_ROW_HEIGHT;
              // Prepend in order
              timelineEvents = fetched.concat(timelineEvents);
              // Enforce cap 200 LRU: trim from middle if needed? Keep most recent + fetched older? Keep 200 most recent? But we just fetched older, so we may exceed cap.
              // Enforce max: keep last MAX_TIMELINE_EVENTS if exceeds, but we already have newest; so if exceeds, drop newest overflow? Actually LRU 200 means keep 200 total; if we prepend older we exceed. We should trim from the end? No, we want to keep viewport stable; simplest keep all until >250 then slice.
              // Keep cache bounded but preserve newest; if we exceeded cap after prepending older,
              // trim oldest beyond a soft limit (400) rather than discarding newest live data.
              if (timelineEvents.length > 400) {
                // Keep most recent 200 plus the newly fetched older plus viewport window: slice to keep last 400
                timelineEvents = timelineEvents.slice(timelineEvents.length - 400);
                // Adjust scroll: we removed some oldest, compensate
                const removed = 0; // already adjusted via slice; scroll offset stays approx
              } else if (timelineEvents.length > MAX_TIMELINE_EVENTS + 50 && timelineEvents.length > 300) {
                // Soft cap: keep 250 most recent when not browsing history
                timelineEvents = timelineEvents.slice(timelineEvents.length - MAX_TIMELINE_EVENTS);
              }
              // Adjust scroll to keep visual position stable
              const addedH = fetched.length * VIRTUAL_ROW_HEIGHT;
              timelineList.scrollTop = prevTop + addedH;
              renderTimelineVirtual();
            }
            virtualFetchingOlder = false;
          }
        });
    }
  }
  function addTimelineEvent(event) {
    var entry = {
      iteration: event.iteration,
      status: event.status,
      durationMs: event.durationMs,
      seq: event.seq,
      nodeCount: event.nodeCount
    };
    const wasAtBottom = (timelineList.scrollTop + timelineList.clientHeight + 40) >= timelineList.scrollHeight;
    timelineEvents.push(entry);
    let evicted = 0;
    if (timelineEvents.length > MAX_TIMELINE_EVENTS) {
      evicted = timelineEvents.length - MAX_TIMELINE_EVENTS;
      timelineEvents = timelineEvents.slice(timelineEvents.length - MAX_TIMELINE_EVENTS);
      // Virtual spacers will account for evicted count via height; scroll adjustment not needed if at bottom.
    }
    currentIteration = event.iteration;
    if (profileCacheIter !== currentIteration) { profileCache = null; }
    // Incremental path when at bottom: O(1) — virtual render will place it visible without full rebuild.
    // We schedule a virtual render (debounced via rAF) rather than direct DOM append.
    scheduleTimelineVirtualRender();
    if (liveMode && wasAtBottom) {
      // Keep pinned to bottom after rAF renders; use rAF to scroll after DOM updated.
      requestAnimationFrame(function(){ timelineList.scrollTop = timelineList.scrollHeight; });
    }
  }
  function appendTimelineRow(ev) {
    // Legacy entry point kept for compatibility; delegate to virtual increment.
    // Creating a single row directly would bypass virtual windowing, so route through addTimelineEvent path.
    // For direct calls (should be rare), just ensure virtual spacers and append via fragment if within window.
    scheduleTimelineVirtualRender();
  }
  function updateTimelineSelection() {
    // With virtualization, only visible rows exist in DOM; toggle class on those, no full scan needed beyond visible.
    const rows = timelineList.querySelectorAll('.tl-row');
    rows.forEach(function(r) { r.classList.toggle('selected', r.dataset.iter == currentIteration); });
  }
  function renderTimeline() {
    // Legacy full-rebuild entry (e.g., playback) now delegates to virtual windowed render.
    // No container innerHTML wipe; uses fragment + spacers.
    renderTimelineVirtual();
  }
  // Bind scroll -> virtual windowing
  (function bindTimelineScroll(){
    let ticking = false;
    timelineList.addEventListener('scroll', function(){
      if (!ticking) {
        ticking = true;
        requestAnimationFrame(function(){ ticking = false; renderTimelineVirtual(); });
      }
    });
    // Initial virtual render to create spacers even before first event
    ensureTimelineVirtualSpacers();
    renderTimelineVirtual();
  })();


  function fetchTimelineIter(iter) {
    fetch(planPath('/timeline/' + iter))
      .then(function(r) {
        if (r.status === 404) {
          var nodesLayer = document.getElementById('nodes-layer');
          var edgesLayer = document.getElementById('edges-layer');
          if (nodesLayer) clearSvgChildren(nodesLayer);
          if (edgesLayer) clearSvgChildren(edgesLayer);
          if (nodesLayer) {
            var msgEl = document.createElementNS(SVG_NS, 'text');
            msgEl.setAttribute('x', '50');
            msgEl.setAttribute('y', '50');
            msgEl.setAttribute('fill', '#888');
            msgEl.setAttribute('font-size', '14');
            msgEl.textContent = 'Tree snapshot no longer available for iteration ' + iter;
            nodesLayer.appendChild(msgEl);
          }
          return null;
        }
        return r.ok ? r.json() : null;
      })
      .then(function(data) { if (data && data.tree) { lastReconstructedTree = cloneTreeJS(data.tree); lastReconstructedIteration = data.iteration || iter; renderTree(data.tree); if (pendingRestoreNodeId) { var rn = findNode(currentTree, pendingRestoreNodeId); pendingRestoreNodeId = null; if (rn) selectNode(rn); } } });
  }

  // Layout is stored separately from tree data to avoid mutating shared event objects (D11).
  // Also computes bounding boxes for viewport culling (D14).
  function buildLayoutMap(tree) {
    const map = {};
    function compute(node, level) {
      if (!node) return 1;
      const entry = { level: level, width: 1, x: 0, absX: 0, absY: 0, bbox: null };
      map[node.id] = entry;
      if (!node.children || node.children.length === 0) {
        entry.width = 1;
        entry.x = 0;
        return 1;
      }
      let totalW = 0;
      for (let i = 0; i < node.children.length; i++) {
        totalW += compute(node.children[i], level + 1);
      }
      entry.width = totalW;
      let offset = -totalW / 2;
      for (let i = 0; i < node.children.length; i++) {
        const c = node.children[i];
        const ce = map[c.id];
        ce.x = offset + ce.width / 2;
        offset += ce.width;
      }
      return totalW;
    }
    function assign(node, parentY) {
      if (!node) return;
      const e = map[node.id];
      e.absX = e.level * COL_W + 40;
      e.absY = (parentY + e.x) * UNIT;
      e.bbox = { x: e.absX, y: e.absY - NODE_H/2, w: NODE_W, h: NODE_H };
      if (node.children) {
        for (let i = 0; i < node.children.length; i++) assign(node.children[i], parentY + e.x);
      }
    }
    compute(tree, 0);
    assign(tree, 0);
    return map;
  }
  function getViewportBounds() {
    // viewBox is set as  pan.x pan.y width/zoom height/zoom . Visible rect is that.
    // Fallback to container viewport if viewBox not yet set.
    const vb = svg.getAttribute('viewBox');
    if (vb) {
      const parts = vb.split(/[\s,]+/).map(Number);
      if (parts.length === 4 && parts.every(function(v){ return !isNaN(v); })) {
        return { x: parts[0], y: parts[1], w: parts[2], h: parts[3] };
      }
    }
    const w = svgContainer.clientWidth || 800;
    const h = svgContainer.clientHeight || 600;
    return { x: pan.x, y: pan.y, w: w / (zoom||1), h: h / (zoom||1) };
  }
  function isBboxVisible(bbox, vp) {
    if (!bbox || !vp) return true;
    return !(bbox.x + bbox.w < vp.x || bbox.x > vp.x + vp.w || bbox.y + bbox.h < vp.y || bbox.y > vp.y + vp.h);
  }

  // Legacy wrappers kept for test hooks (no mutation): they now delegate to map version
  function computeLayout(node, level) {
    // Deprecated mutation path removed; use buildLayoutMap. This stub ensures any external caller still works without mutating.
    if (!node) return { width: 1, x: 0 };
    const m = buildLayoutMap(node);
    const e = m[node.id];
    return { width: e.width, x: e.x };
  }
  function assignPositions(node, parentX) {
    // No-op: layout now via buildLayoutMap
  }

  let currentLayoutMap = null;
  let currentViewById = null;
  let lastRawTree = null;
  let viewVersion = 0;
  let expandAllMode = false;
  const unwoundHeads = new Set();
  const userExpanded = new Set();
  const userCollapsed = new Set();
  const searchReveal = new Set();
  function bumpView() { viewVersion++; }
  function countSubtree(n, memo) {
    if (memo.has(n.id)) return memo.get(n.id);
    let c = 1;
    if (n.children) for (let i = 0; i < n.children.length; i++) c += countSubtree(n.children[i], memo);
    memo.set(n.id, c);
    return c;
  }
  function isCollapsedView(tail, depth, active) {
    if (userCollapsed.has(tail.id)) return true;
    if (expandAllMode) return false;
    if (userExpanded.has(tail.id)) return false;
    if (searchReveal.has(tail.id)) return false;
    return !!tail.children && tail.children.length >= 2 && depth >= 1 && !active.has(tail.id);
  }
  function buildViewModel(tree) {
    const active = new Set();
    (function mark(n) {
      let a = !!(n.status && n.status.LastStatus === 1);
      if (n.children) for (let i = 0; i < n.children.length; i++) if (mark(n.children[i])) a = true;
      if (a) active.add(n.id);
      return a;
    })(tree);
    const memo = new Map();
    const byId = new Map();
    let rendered = 0, rawCount = 0, maxDepth = 0;
    (function rawStats(n, d) { rawCount++; if (d > maxDepth) maxDepth = d; if (n.children) for (let i = 0; i < n.children.length; i++) rawStats(n.children[i], d + 1); })(tree, 0);
    function vm(n, depth) {
      rendered++;
      let run = [n], cur = n;
      while (cur.children && cur.children.length === 1) { cur = cur.children[0]; run.push(cur); }
      const tailDepth = depth + run.length - 1;
      const v = { id: n.id, name: n.name, nodeType: n.nodeType, status: n.status, effects: n.effects, condition: n.condition, postCondition: n.postCondition, frame: n.frame, children: [], _view: { kind: 'node', chainCount: 0, hiddenCount: 0, members: null, tailId: n.id, rep: n, onActive: active.has(n.id) } };
      byId.set(v.id, v);
      let tail = n;
      if (run.length > 2 && zoomTier() < 2 && !unwoundHeads.has(n.id) && !searchReveal.has(n.id)) {
        let rep = run[run.length - 1];
        for (let i = 0; i < run.length; i++) if (run[i].status && run[i].status.LastStatus === 1) { rep = run[i]; break; }
        v._view = { kind: 'chain', chainCount: run.length, hiddenCount: 0, members: run.map(function(m) { return m.id; }), tailId: run[run.length - 1].id, rep: rep, onActive: active.has(n.id) };
        v.name = rep.name || n.name;
        v.nodeType = rep.nodeType || n.nodeType;
        v.status = rep.status || n.status;
        v.effects = rep.effects;
        v.condition = rep.condition;
        v.postCondition = rep.postCondition;
        tail = run[run.length - 1];
      }
      if (tail.children && tail.children.length) {
        v._view.tailId = tail.id;
        if (isCollapsedView(tail, tailDepth, active)) {
          v._view.hiddenCount = countSubtree(tail, memo) - 1;
          if (v._view.kind === 'node') v._view.kind = 'branch';
        } else {
          if (v._view.kind === 'node' && tail.children.length >= 2) v._view.kind = 'branch';
          for (let i = 0; i < tail.children.length; i++) v.children.push(vm(tail.children[i], tailDepth + 1));
        }
      }
      return v;
    }
    const root = vm(tree, 0);
    root._byId = byId;
    root.structureHash = (tree.structureHash || '') + ':' + viewVersion;
    root._stats = { rendered: rendered, rawCount: rawCount, maxDepth: maxDepth };
    return root;
  }
  function updateStatsReadout(view) {
    const el = document.getElementById('stats-readout');
    if (!el || !view._stats) return;
    el.innerHTML = '<b>' + view._stats.rendered + '</b> cards / ' + view._stats.rawCount + ' nodes / depth <b>' + view._stats.maxDepth + '</b>';
  }
  function revealAncestors(ids) {
    if (!lastRawTree || !ids.size) return false;
    const parent = new Map();
    (function walk(n) { if (n.children) for (let i = 0; i < n.children.length; i++) { parent.set(n.children[i].id, n.id); walk(n.children[i]); } })(lastRawTree);
    let added = false;
    ids.forEach(function(id) {
      let p = parent.get(id);
      while (p) { if (!searchReveal.has(p)) { searchReveal.add(p); added = true; } p = parent.get(p); }
    });
    return added;
  }
  function clearSvgChildren(el) { while (el.firstChild) el.removeChild(el.firstChild); }

  // Explorability 4: replan flash + conflict resolution annotation on SSE delta.
  // Tombstone removed nodes — must run BEFORE render (nodes still in DOM).
  function flashReplanRemoved(delta) {
    if (!delta || !delta.removed || !delta.removed.length) return;
    const nodesG = document.getElementById('nodes-layer');
    if (!nodesG) return;
    let tombstoneG = document.getElementById('tombstone-layer');
    if (!tombstoneG) {
      tombstoneG = document.createElementNS(SVG_NS, 'g');
      tombstoneG.setAttribute('id', 'tombstone-layer');
      svg.insertBefore(tombstoneG, nodesG);
    }
    let tombstoned = 0;
    for (const rid of delta.removed) {
      if (tombstoned >= 20) break;
      const el = document.getElementById(safeId(rid));
      if (el) {
        const clone = el.cloneNode(true);
        clone.querySelectorAll('.node-card').forEach(function(c) {
          c.classList.add('tombstone');
        });
        tombstoneG.appendChild(clone);
        tombstoned++;
        setTimeout(function() { if (clone.parentNode) clone.parentNode.removeChild(clone); }, 800);
      }
    }
  }
  // Flash added nodes green — must run AFTER render (nodes only exist post-render).
  function flashReplanAdded(delta) {
    if (!delta || !delta.added) return;
    let flashed = 0;
    for (const added of delta.added) {
      if (flashed >= 20) break;
      const el = document.getElementById(safeId(added.id));
      if (el) {
        const card = el.querySelector('.node-card');
        if (card) {
          card.classList.add('added-flash');
          flashed++;
          setTimeout(function() { card.classList.remove('added-flash'); }, 600);
        }
      }
    }
  }

  // Conflict reorder annotation: detect removed+added with matching structureHash.
  // oldTree/oldLayoutMap captured pre-render; currentLayoutMap is post-render.
  function annotateConflictReorder(delta, oldTree, oldLayoutMap) {
    if (!delta || !delta.added || !delta.removed || !oldLayoutMap || !currentLayoutMap) return;
    const removedHashes = {};
    for (const rid of delta.removed) {
      if (oldTree) {
        const node = findNode(oldTree, rid);
        if (node && node.structureHash) removedHashes[node.structureHash] = rid;
      }
    }
    let overlayG = document.getElementById('conflict-overlay');
    if (!overlayG) {
      overlayG = document.createElementNS(SVG_NS, 'g');
      overlayG.setAttribute('id', 'conflict-overlay');
      svg.appendChild(overlayG);
    }
    let conflicts = 0;
    for (const added of delta.added) {
      if (conflicts >= 20) break;
      if (added.structureHash && removedHashes[added.structureHash]) {
        const oldId = removedHashes[added.structureHash];
        const oldLm = oldLayoutMap[oldId];
        const newLm = currentLayoutMap[added.id];
        if (oldLm && newLm) {
          conflicts++;
          const line = document.createElementNS(SVG_NS, 'line');
          line.setAttribute('x1', oldLm.absX + NODE_W / 2);
          line.setAttribute('y1', oldLm.absY);
          line.setAttribute('x2', newLm.absX + NODE_W / 2);
          line.setAttribute('y2', newLm.absY);
          line.setAttribute('class', 'conflict-line');
          overlayG.appendChild(line);
          setTimeout(function() { if (line.parentNode) line.parentNode.removeChild(line); }, 2000);
        }
      }
    }
  }

  function updateNarration(data) {
    const bar = document.getElementById('narration-bar');
    if (!bar) return;
    const iter = data.iteration;
    if (data.delta) {
      const d = data.delta;
      const nA = d.added ? d.added.length : 0;
      const nR = d.removed ? d.removed.length : 0;
      const nC = d.changed ? d.changed.length : 0;
      let desc = 'Iter ' + iter + ': ';
      if (nA > 0 || nR > 0) {
        desc += '+' + nA + '/-' + nR + ' nodes';
        if (nA > 0 && d.added[0]) desc += ' (' + d.added.map(function(a) { return a.nodeType || '?'; }).slice(0, 3).join(', ') + ')';
        desc += ' \u2014 replanning';
      } else if (nC > 0) {
        desc += nC + ' status change' + (nC > 1 ? 's' : '');
        if (d.changed[0]) desc += ' (' + d.changed[0].nodeType + ')';
      } else {
        desc += 'no structural change';
      }
      bar.textContent = desc;
    } else {
      const st = data.status != null ? statusToString(data.status) : '?';
      bar.textContent = 'Iter ' + iter + ': keyframe \u2014 ' + st;
    }
  }

  // Explorability 3: heat/failure/recency glow modes.
  var heatMode = 'off';
  var prevTickCount = {};
  function applyHeatGlow() {
    if (!profileCache || !profileCache.length) return;
    const maxTicks = profileCache.reduce((m, p) => Math.max(m, p.TickCount || 0), 1);
    const nodesG = document.getElementById('nodes-layer');
    if (!nodesG) return;
    nodesG.querySelectorAll('.node-card').forEach(function(card) {
      card.classList.remove('heat-glow', 'fail-glow', 'recent-pulse');
      card.removeAttribute('style');
    });
    if (heatMode === 'off') return;
    for (const p of profileCache) {
      const el = document.getElementById(safeId(p.id));
      if (!el) continue;
      const card = el.querySelector('.node-card');
      if (!card) continue;
      if (heatMode === 'heat') {
        const intensity = (p.TickCount || 0) / maxTicks;
        if (intensity > 0.1) {
          card.classList.add('heat-glow');
          card.style.setProperty('--heat-color', 'hsl(' + Math.round(60 - intensity * 60) + ',100%,50%)');
          card.style.filter = 'drop-shadow(0 0 ' + Math.round(intensity * 6) + 'px hsl(' + Math.round(60 - intensity * 60) + ',100%,50%))';
        }
      } else if (heatMode === 'fail') {
        const failRate = p.TickCount > 0 ? (p.FailureCount || 0) / p.TickCount : 0;
        if (failRate > 0.05) {
          card.classList.add('fail-glow');
          card.setAttribute('stroke-width', String(1.5 + failRate * 3));
        }
      } else if (heatMode === 'recent') {
        const prev = prevTickCount[p.id] || 0;
        if ((p.TickCount || 0) > prev) {
          card.classList.add('recent-pulse');
        }
      }
    }
  }

  document.querySelectorAll('.btn-heat').forEach(function(btn) {
    btn.addEventListener('click', function() {
      heatMode = btn.getAttribute('data-heat');
      document.querySelectorAll('.btn-heat').forEach(function(b) { b.classList.toggle('active', b === btn); });
      applyHeatGlow();
    });
  });
  // Default: Off active
  document.querySelector('.btn-heat[data-heat="off"]').classList.add('active');

  // Explorability 2: scrub strip sparkline. 1px per iter, colored by root status.
  function drawScrubStrip() {
    const strip = document.getElementById('scrub-strip');
    if (!strip) return;
    strip.innerHTML = '';
    if (!timelineEvents.length) return;
    const minIter = timelineEvents[0].iteration;
    const maxIter = timelineEvents[timelineEvents.length - 1].iteration;
    const range = Math.max(1, maxIter - minIter + 1);
    const stripW = strip.clientWidth || 200;
    const pxPerIter = stripW / range;
    for (const ev of timelineEvents) {
      const bar = document.createElement('div');
      bar.className = 'scrub-bar';
      const x = (ev.iteration - minIter) * pxPerIter;
      bar.style.left = x + 'px';
      bar.style.width = Math.max(1, pxPerIter) + 'px';
      const st = ev.status === 'Running' ? 1 : (ev.status === 'Success' ? 2 : (ev.status === 'Failure' ? 3 : 0));
      bar.style.background = STATUS_COLORS[st] || '#555';
      strip.appendChild(bar);
    }
    // Playhead at current iteration
    if (currentIteration != null && currentIteration >= minIter && currentIteration <= maxIter) {
      const ph = document.createElement('div');
      ph.className = 'scrub-playhead';
      ph.style.left = ((currentIteration - minIter) * pxPerIter) + 'px';
      strip.appendChild(ph);
    }
  }

  // Scrub strip click handler: jump to iteration at click position.
  (function() {
    const strip = document.getElementById('scrub-strip');
    if (!strip) return;
    function iterFromX(clientX) {
      const rect = strip.getBoundingClientRect();
      const frac = Math.max(0, Math.min(1, (clientX - rect.left) / rect.width));
      if (!timelineEvents.length) return null;
      const minIter = timelineEvents[0].iteration;
      const maxIter = timelineEvents[timelineEvents.length - 1].iteration;
      return Math.round(minIter + frac * (maxIter - minIter));
    }
    strip.addEventListener('click', function(e) {
      const iter = iterFromX(e.clientX);
      if (iter != null) jumpToIteration(iter);
    });
    let dragging = false;
    strip.addEventListener('mousedown', function(e) { dragging = true; });
    document.addEventListener('mousemove', function(e) {
      if (!dragging) return;
      const iter = iterFromX(e.clientX);
      if (iter != null) jumpToIteration(iter);
    });
    document.addEventListener('mouseup', function() { dragging = false; });
  })();

  function jumpToIteration(iter) {
    liveMode = false;
    writeUrlState(true);
    btnLive.classList.remove('active');
    currentIteration = iter;
    fetchTimelineIter(iter);
    drawScrubStrip();
  }

  // Explorability 2: keyboard jog. Guarded by activeElement for inputs.
  document.addEventListener('keydown', function(e) {
    const tag = (document.activeElement && document.activeElement.tagName) || '';
    if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return;
    if (e.key === 'ArrowLeft') { e.preventDefault(); stepIteration(-1); }
    else if (e.key === 'ArrowRight') { e.preventDefault(); stepIteration(1); }
    else if (e.key === ' ') { e.preventDefault(); togglePlayback(); }
    else if (e.key === 'l' || e.key === 'L') { liveMode = !liveMode; btnLive.classList.toggle('active', liveMode); }
    else if (e.key === 'Home') { e.preventDefault(); if (timelineEvents.length) jumpToIteration(timelineEvents[0].iteration); }
    else if (e.key === 'End') { e.preventDefault(); if (timelineEvents.length) jumpToIteration(timelineEvents[timelineEvents.length - 1].iteration); }
    else if (e.key === 'f' || e.key === 'F') { e.preventDefault(); jumpToNextFailure(); }
    else if (e.key === 'c' && !e.ctrlKey && !e.metaKey) { e.preventDefault(); jumpToNextChange(); }
    else if (e.key === '?') { e.preventDefault(); toggleShortcutsOverlay(); }
    else if (e.key === 'Escape') {
      const ov = document.getElementById('shortcuts-overlay');
      if (ov && ov.style.display !== 'none') { ov.style.display = 'none'; e.preventDefault(); }
    }
    else if (e.key === '+' || e.key === '=') { e.preventDefault(); const r = svgContainer.getBoundingClientRect(); zoomAtPoint(r.left + r.width / 2, r.top + r.height / 2, 1.2); }
    else if (e.key === '-' || e.key === '_') { e.preventDefault(); const r = svgContainer.getBoundingClientRect(); zoomAtPoint(r.left + r.width / 2, r.top + r.height / 2, 1 / 1.2); }
    else if (e.key === '0') { e.preventDefault(); resetView(); }
    else if (e.key === 'e' || e.key === 'E') { e.preventDefault(); expandAllMode = true; bumpView(); renderTree(lastRawTree); }
    else if (e.key === 'w' || e.key === 'W') { e.preventDefault(); document.getElementById('btn-why').click(); }
    else if (e.key >= '1' && e.key <= '4') { const bs = document.querySelectorAll('.btn-heat'); const b = bs[Number(e.key) - 1]; if (b) b.click(); }
  });

  function stepIteration(delta) {
    if (!timelineEvents.length) return;
    if (currentIteration == null) { jumpToIteration(timelineEvents[0].iteration); return; }
    // Find nearest timeline event index and step
    let idx = -1;
    for (let i = 0; i < timelineEvents.length; i++) {
      if (timelineEvents[i].iteration === currentIteration) { idx = i; break; }
    }
    if (idx < 0) {
      // Find closest
      let bestDist = Infinity;
      for (let i = 0; i < timelineEvents.length; i++) {
        const d = Math.abs(timelineEvents[i].iteration - currentIteration);
        if (d < bestDist) { bestDist = d; idx = i; }
      }
    }
    const newIdx = Math.max(0, Math.min(timelineEvents.length - 1, idx + delta));
    jumpToIteration(timelineEvents[newIdx].iteration);
  }

  function togglePlayback() {
    playback = !playback;
    btnPlay.textContent = playback ? '\u23F8' : '\u25B6';
    if (playback) {
      if (!timelineEvents.length) { playback = false; btnPlay.textContent = '\u25B6'; return; }
      if (currentIteration == null) playbackIdx = 0;
      else {
        playbackIdx = -1;
        for (let i = 0; i < timelineEvents.length; i++) {
          if (timelineEvents[i].iteration === currentIteration) { playbackIdx = i; break; }
        }
        if (playbackIdx < 0) playbackIdx = 0;
      }
      playbackLastTime = performance.now() - 1000 / Math.max(0.1, playbackSpeed);
      playbackRaf = requestAnimationFrame(playbackRafStep);
    } else {
      cancelPlaybackRaf();
    }
  }
  function playbackRafStep(now) {
    if (!playback) return;
    const interval = 1000 / Math.max(0.1, playbackSpeed);
    if (now - playbackLastTime >= interval) {
      playbackLastTime = now - ((now - playbackLastTime) % interval);
      if (playbackIdx >= timelineEvents.length - 1) {
        playback = false;
        cancelPlaybackRaf();
        btnPlay.textContent = '\u25B6';
        return;
      }
      stepIteration(1);
    }
    playbackRaf = requestAnimationFrame(playbackRafStep);
  }

  function jumpToNextFailure() {
    if (!timelineEvents.length) return;
    const startIdx = currentIteration != null ? timelineEvents.findIndex(e => e.iteration >= currentIteration) + 1 : 0;
    for (let i = startIdx; i < timelineEvents.length; i++) {
      if (timelineEvents[i].status === 'Failure') { jumpToIteration(timelineEvents[i].iteration); return; }
    }
    // Wrap around
    for (let i = 0; i < startIdx && i < timelineEvents.length; i++) {
      if (timelineEvents[i].status === 'Failure') { jumpToIteration(timelineEvents[i].iteration); return; }
    }
  }

  function jumpToNextChange() {
    if (timelineEvents.length < 2) return;
    const startIdx = currentIteration != null ? timelineEvents.findIndex(e => e.iteration >= currentIteration) + 1 : 1;
    for (let i = Math.max(1, startIdx); i < timelineEvents.length; i++) {
      if (timelineEvents[i].nodeCount !== timelineEvents[i-1].nodeCount) { jumpToIteration(timelineEvents[i].iteration); return; }
    }
    for (let i = 1; i < startIdx && i < timelineEvents.length; i++) {
      if (timelineEvents[i].nodeCount !== timelineEvents[i-1].nodeCount) { jumpToIteration(timelineEvents[i].iteration); return; }
    }
  }

  // Explorability 1: breadcrumb path bar. Walks up parentID chain from selectedNodeId.
  function updateBreadcrumb(nodeId) {
    const bar = document.getElementById('breadcrumb-bar');
    if (!bar) return;
    if (!nodeId || !lastRawTree) { bar.innerHTML = ''; return; }
    const chain = [];
    let cur = nodeId;
    while (cur) {
      const n = findNode(lastRawTree, cur);
      if (!n) break;
      chain.unshift({ id: cur, name: n.name || n.nodeType || cur });
      const dot = cur.lastIndexOf('.');
      cur = dot > 0 ? cur.substring(0, dot) : null;
    }
    if (!chain.length) { bar.innerHTML = ''; return; }
    bar.innerHTML = chain.map((c, i) => {
      const isLast = i === chain.length - 1;
      return (i > 0 ? '<span class="bc-sep">\u203A</span>' : '') +
        '<span class="bc-crumb' + (isLast ? ' current' : '') + '" data-bcid="' + escapeAttr(c.id) + '">' + escapeHtml(c.name) + '</span>';
    }).join('');
    bar.querySelectorAll('.bc-crumb').forEach(el => {
      el.addEventListener('click', function() {
        const id = el.getAttribute('data-bcid');
        const n = findNode(lastRawTree, id);
        if (n) { selectNode(n); panToNodeId(id); }
      });
    });
  }

  // Explorability 1: Why? causal-link overlay.
  // Builds index of effects per node, draws dashed curves from each PreconditionLeaf
  // to ActionNodes whose effects satisfy its condition key.
  let whyMode = false;
  function drawCausalOverlay() {
    let overlay = document.getElementById('causal-overlay');
    if (!overlay) {
      overlay = document.createElementNS(SVG_NS, 'g');
      overlay.setAttribute('id', 'causal-overlay');
      svg.appendChild(overlay);
    }
    clearSvgChildren(overlay);
    if (!whyMode || !lastRawTree) return;
    // Build effect index: key -> list of {nodeId, value}
    const effectsByKey = {};
    const preconditions = [];
    (function walk(n) {
      if (!n) return;
      if (n.nodeType === 'PreconditionLeaf' && n.condition) preconditions.push(n);
      if (n.effects) {
        for (const e of n.effects) {
          if (!effectsByKey[e.key]) effectsByKey[e.key] = [];
          effectsByKey[e.key].push({ nodeId: n.id, value: e.value });
        }
      }
      if (n.children) for (const c of n.children) walk(c);
    })(lastRawTree);
    const lm = currentLayoutMap;
    for (const pre of preconditions) {
      const key = pre.condition;
      const targets = effectsByKey[key];
      if (!targets || !targets.length) continue;
      const preLm = lm ? lm[pre.id] : null;
      if (!preLm) continue;
      const px = preLm.absX + NODE_W;
      const py = preLm.absY;
      for (const t of targets) {
        const tLm = lm ? lm[t.nodeId] : null;
        if (!tLm) continue;
        const tx = tLm.absX;
        const ty = tLm.absY;
        const midX = (px + tx) / 2;
        const path = document.createElementNS(SVG_NS, 'path');
        path.setAttribute('d', 'M ' + px + ' ' + py + ' C ' + midX + ' ' + py + ', ' + midX + ' ' + ty + ', ' + tx + ' ' + ty);
        path.setAttribute('class', 'causal-path');
        overlay.appendChild(path);
      }
    }
  }
  let lastRenderMs = 0;
  // Layout worker: buildLayoutMap is pure O(n) math over the view tree, so on
  // large trees it runs off-main-thread; renderTree continues in the reply.
  const layoutWorker = (function() {
    if (typeof Worker !== 'function' || typeof URL === 'undefined' || !URL.createObjectURL) return null;
    const src = 'self.onmessage=function(ev){var d=ev.data;var n=d.n;var childStart=d.childStart;var childCount=d.childCount;var level=d.level;var width=new Int32Array(n);var xoff=new Float64Array(n);function compute(i,lvl){level[i]=lvl;var c=childCount[i];if(c===0){width[i]=1;xoff[i]=0;return 1;}var total=0;var s=childStart[i];for(var k=0;k<c;k++){total+=compute(s+k,lvl+1);}width[i]=total;var off=-total/2;for(var k2=0;k2<c;k2++){var ci=s+k2;xoff[ci]=off+width[ci]/2;off+=width[ci];}return total;}compute(0,0);var absX=new Float64Array(n);var absY=new Float64Array(n);function assign(i,parentY){absX[i]=level[i]*d.colW+40;absY[i]=(parentY+xoff[i])*d.unit;if(childCount[i]){var s=childStart[i];for(var k=0;k<childCount[i];k++){assign(s+k,parentY+xoff[i]);}}}assign(0,0);self.postMessage({seq:d.seq,absX:absX,absY:absY},[absX.buffer,absY.buffer]);};';
    try {
      const w = new Worker(URL.createObjectURL(new Blob([src], { type: 'application/javascript' })));
      return w;
    } catch (e) { return null; }
  })();
  let layoutSeq = 0;
  function serializeViewForWorker(view) {
    const ids = [], childStart = [], childCount = [];
    const index = new Map();
    (function walk(n) { index.set(n, ids.length); ids.push(n.id); if (n.children) for (let i = 0; i < n.children.length; i++) walk(n.children[i]); else void 0; })(view);
    const n = ids.length;
    const start = new Int32Array(n), count = new Int32Array(n), level = new Int32Array(n);
    (function walk2(n, lvl) { const me = index.get(n); level[me] = lvl; const cs = n.children ? n.children.length : 0; count[me] = cs; start[me] = me + 1; if (n.children) for (let i = 0; i < cs; i++) walk2(n.children[i], lvl + 1); })(view, 0);
    return { ids: ids, childStart: start, childCount: count, level: level };
  }
  function buildLayoutMapFromWorker(view, seq, cb) {
    const s = serializeViewForWorker(view);
    const n = s.ids.length;
    layoutWorker.onmessage = function(ev) {
      const d = ev.data;
      if (d.seq !== seq) return;
      const map = {};
      for (let i = 0; i < n; i++) {
        map[s.ids[i]] = { level: s.level[i], width: 0, x: 0, absX: d.absX[i], absY: d.absY[i], bbox: { x: d.absX[i], y: d.absY[i] - NODE_H / 2, w: NODE_W, h: NODE_H } };
      }
      cb(map);
    };
    layoutWorker.postMessage({ seq: seq, n: n, childStart: s.childStart, childCount: s.childCount, level: s.level, colW: COL_W, unit: UNIT }, [s.childStart.buffer, s.childCount.buffer, s.level.buffer]);
  }
  function renderTree(tree) {
    if (!tree) return;
    const t0 = performance.now();
    lastRawTree = tree;
    const view = buildViewModel(tree);
    if (layoutWorker && view._stats && view._stats.rawCount > 1500) {
      const seq = ++layoutSeq;
      const capturedTierCheck = zoomTier();
      buildLayoutMapFromWorker(view, seq, function(layoutMap) {
        if (seq !== layoutSeq) return;
        renderTreePhase2(view, layoutMap, t0, capturedTierCheck);
      });
      return;
    }
    renderTreePhase2(view, buildLayoutMap(view), t0, zoomTier());
  }
  function renderTreePhase2(view, layoutMap, t0, tier) {
    currentViewById = view._byId || null;
    const needsFullRebuild = currentTree === null ||
      !document.getElementById('nodes-layer') ||
      currentTree.structureHash !== view.structureHash ||
      renderedTier !== tier;

    if (needsFullRebuild) {
      clearSvgChildren(svg);
      const edgesG = document.createElementNS(SVG_NS, 'g');
      const nodesG = document.createElementNS(SVG_NS, 'g');
      edgesG.setAttribute('id', 'edges-layer');
      edgesG.setAttribute('aria-hidden', 'true');
      nodesG.setAttribute('id', 'nodes-layer');
      nodesG.setAttribute('role', 'group');
      drawEdges(view, edgesG, layoutMap);
      drawNodes(view, nodesG, layoutMap);
      svg.appendChild(edgesG);
      svg.appendChild(nodesG);
      currentTree = view;
      currentLayoutMap = layoutMap;
    } else {
      const nodesG = document.getElementById('nodes-layer');
      const edgesG = document.getElementById('edges-layer');
      clearSvgChildren(edgesG);
      drawEdges(view, edgesG, layoutMap);
      diffAndUpdateNodes(view, currentTree, nodesG, layoutMap, currentLayoutMap);
      currentTree = view;
      currentLayoutMap = layoutMap;
    }
    renderedTier = tier;
    updateSvgView();
    updateMinimap();
    if (!initialFitDone && !userAdjustedView && currentTree) { initialFitDone = true; fitToTree(); }
    scheduleIdleWork(function() { drawProfileBar(); });
    drawCausalOverlay();
    scheduleIdleWork(function() { drawScrubStrip(); });
    applyHeatGlow();
    updateStatsReadout(view);
    lastRenderMs = performance.now() - t0;
    if (selectedNodeId) highlightNode(selectedNodeId);
    if (highlightedIds.size) highlightSearch();
    if (searchInput.value.trim() && lastRawTree) {
      searchMatches = findNodesByName(lastRawTree, searchInput.value.trim().toLowerCase());
      highlightedIds = new Set(searchMatches);
      if (searchMatchIdx >= searchMatches.length) searchMatchIdx = Math.max(0, searchMatches.length - 1);
      updateSearchCount();
    }
  }

  function updateNodeGroup(group, newNode, layoutMap) {
    const lm = layoutMap ? layoutMap[newNode.id] : null;
    const absX = lm ? lm.absX : newNode._absX;
    const absY = lm ? lm.absY : newNode._absY;
    const x = absX;
    const y = absY - NODE_H / 2;
    const typeColor = TYPE_COLORS[newNode.nodeType] || TYPE_COLORS.Unknown;
    const status = newNode.status ? newNode.status.LastStatus : 0;
    const statusColor = STATUS_COLORS[status] || '#555';
    const isRunning = status === 1;
    const hasEffects = newNode.effects && newNode.effects.length > 0;
    const hasCondition = !!newNode.condition || !!newNode.postCondition;

    group.setAttribute('class', isRunning ? 'node-running' : '');
    group.setAttribute('aria-label', (newNode.nodeType || 'node') + ' ' + (newNode.name || newNode.id) + ', ' + statusToString(status) + (newNode.status ? ', ' + newNode.status.TickCount + ' ticks' : ''));

    if (zoomTier() === 0) {
      const dot = group.querySelector('.node-dot');
      if (dot) {
        dot.setAttribute('cx', absX + NODE_W / 2);
        dot.setAttribute('cy', absY);
        dot.setAttribute('r', isRunning ? '5' : '3.5');
        dot.setAttribute('fill', isRunning ? statusColor : typeColor);
      }
      return group;
    }

    const ring = group.querySelector('.node-ring');
    if (ring) {
      ring.setAttribute('x', x - 3);
      ring.setAttribute('y', y - 3);
    }

    const card = group.querySelector('.node-card');
    if (card) {
      card.setAttribute('x', x);
      card.setAttribute('y', y);
      card.setAttribute('stroke', typeColor);
      if (!isRunning && status === 0) card.setAttribute('opacity', '0.45');
      else card.setAttribute('opacity', '1');
    }

    const typebar = group.querySelector('.node-typebar');
    if (typebar) {
      typebar.setAttribute('x', x);
      typebar.setAttribute('y', y);
      typebar.setAttribute('fill', typeColor);
    }

    group.querySelectorAll('[data-glyph]').forEach(function(g){ g.remove(); });
    var ng = makeStatusGlyph(x + NODE_W - 14, y + 12, status, statusColor);
    if (ng) { ng.setAttribute('data-glyph', '1'); group.insertBefore(ng, group.querySelector('.node-type-label')); }

    const typeText = group.querySelector('.node-type-label');
    if (typeText) {
      typeText.setAttribute('x', x + 12);
      typeText.setAttribute('y', y + 18);
      typeText.setAttribute('fill', typeColor);
      typeText.textContent = newNode.nodeType || 'Unknown';
    }

    const nameText = group.querySelector('.node-text');
    if (nameText) {
      nameText.setAttribute('x', x + 12);
      nameText.setAttribute('y', y + 36);
      nameText.textContent = truncate(newNode.name || '', 22);
    }

    group.querySelectorAll('.node-badge:not([data-glyph])').forEach(function(b){ b.remove(); });
    group.querySelectorAll('.node-detail').forEach(function(d){ d.remove(); });

    if (hasEffects) {
      const eb = document.createElementNS(SVG_NS, 'circle');
      eb.setAttribute('cx', x + 16);
      eb.setAttribute('cy', y + NODE_H - 12);
      eb.setAttribute('r', '3');
      eb.setAttribute('fill', cssVar('--success', '#46d68c'));
      eb.setAttribute('class', 'node-badge');
      group.appendChild(eb);
    }
    if (hasCondition) {
      const cb = document.createElementNS(SVG_NS, 'rect');
      cb.setAttribute('x', x + 26);
      cb.setAttribute('y', y + NODE_H - 15);
      cb.setAttribute('width', '6');
      cb.setAttribute('height', '6');
      cb.setAttribute('fill', cssVar('--type-pre', '#2dd4bf'));
      cb.setAttribute('class', 'node-badge');
      group.appendChild(cb);
    }

    appendDetailText(group, newNode, x, y, zoomTier());
    syncPill(group, newNode, x, y);

    return group;
  }

  function diffAndUpdateNodes(newNode, oldNode, container, layoutMap, oldLayoutMap) {
    if (!newNode) return;
    const lm = layoutMap ? layoutMap[newNode.id] : null;
    const vp = layoutMap ? getViewportBounds() : null;
    const pad = 80;
    const evp = vp ? { x: vp.x - pad, y: vp.y - pad, w: vp.w + pad*2, h: vp.h + pad*2 } : null;
    const bbox = lm ? lm.bbox : null;
    const visible = !evp || !bbox || isBboxVisible(bbox, evp);
    const sid = safeId(newNode.id);
    let existingGroup = document.getElementById(sid);
    if (!visible) {
      if (existingGroup) existingGroup.remove();
      // Recurse to children which may still be visible even if parent is culled
      if (newNode.children) {
        for (let i = 0; i < newNode.children.length; i++) {
          const oldChild = (oldNode && oldNode.children && i < oldNode.children.length) ? oldNode.children[i] : null;
          diffAndUpdateNodes(newNode.children[i], oldChild, container, layoutMap, oldLayoutMap);
        }
        if (oldNode && oldNode.children) {
          for (let i = newNode.children.length; i < oldNode.children.length; i++) {
            const oldChildId = safeId(oldNode.children[i].id);
            const og = document.getElementById(oldChildId);
            if (og) og.remove();
            // Also remove its subtree if any (drawNodes may have left descendants)
            // Fallback: brute remove any element whose id starts with that prefix
            // (simple: query all node groups and remove those with data-id prefix)
            const prefix = oldNode.children[i].id + ".";
            document.querySelectorAll('[data-id]').forEach(function(el){
              const did = el.getAttribute('data-id');
              if (did && did.startsWith(prefix)) { const grp=document.getElementById(safeId(did)); if(grp) grp.remove(); }
            });
          }
        }
      } else if (oldNode && oldNode.children) {
        for (let i = 0; i < oldNode.children.length; i++) {
          const oldChildId = safeId(oldNode.children[i].id);
          const og = document.getElementById(oldChildId);
          if (og) og.remove();
        }
      }
      return;
    }
    if (existingGroup && oldNode && oldNode.id === newNode.id) {
      updateNodeGroup(existingGroup, newNode, layoutMap);
      if (newNode.children) {
        for (let i = 0; i < newNode.children.length; i++) {
          const oldChild = (oldNode.children && i < oldNode.children.length) ? oldNode.children[i] : null;
          diffAndUpdateNodes(newNode.children[i], oldChild, container, layoutMap, oldLayoutMap);
        }
        if (oldNode.children) {
          for (let i = newNode.children.length; i < oldNode.children.length; i++) {
            const oldChildId = safeId(oldNode.children[i].id);
            const og = document.getElementById(oldChildId);
            if (og) og.remove();
          }
        }
      } else if (oldNode.children) {
        for (let i = 0; i < oldNode.children.length; i++) {
          const oldChildId = safeId(oldNode.children[i].id);
          const og = document.getElementById(oldChildId);
          if (og) og.remove();
        }
      }
    } else {
      if (existingGroup) existingGroup.remove();
      drawNodes(newNode, container, layoutMap);
    }
  }

  function drawEdges(node, g, layoutMap) {
    if (!node.children) return;
    const lm = layoutMap ? layoutMap[node.id] : null;
    const px = (lm ? lm.absX : node._absX) + NODE_W;
    const py = lm ? lm.absY : node._absY;
    const vp = layoutMap ? getViewportBounds() : null;
    const padE = 80;
    const evp = vp ? { x: vp.x - padE, y: vp.y - padE, w: vp.w + padE*2, h: vp.h + padE*2 } : null;
    for (const c of node.children) {
      const clm = layoutMap ? layoutMap[c.id] : null;
      // Cull edge if child bbox is outside viewport (edge would be invisible)
      if (evp && clm && clm.bbox && !isBboxVisible(clm.bbox, evp)) {
        // Still need to recurse for deeper children that might be in view
        drawEdges(c, g, layoutMap);
        continue;
      }
      const cx = clm ? clm.absX : c._absX;
      const cy = clm ? clm.absY : c._absY;
      const midX = (px + cx) / 2;
      const d = 'M ' + px + ' ' + py +
                ' C ' + midX + ' ' + py +
                ', ' + midX + ' ' + cy +
                ', ' + cx + ' ' + cy;
      const path = document.createElementNS(SVG_NS, 'path');
      path.setAttribute('d', d);
      path.setAttribute('class', 'edge-path');
      const status = c.status ? c.status.LastStatus : 0;
      if (status === 1) path.classList.add('active');
      g.appendChild(path);
      drawEdges(c, g, layoutMap);
    }
  }

  function appendDetailText(group, node, x, y, tier) {
    const onActive = !!(node._view && node._view.onActive);
    if (tier < 2 && !onActive) return;
    const lines = [];
    const cond = node.condition || node.postCondition;
    if (cond) lines.push(truncate(cond, 30));
    if (node.effects && node.effects.length) {
      const eff = node.effects.map(function(e) { return e.key + '=' + e.value; }).join(', ');
      lines.push(truncate(eff, 30));
    }
    for (let i = 0; i < Math.min(lines.length, 2); i++) {
      const dt = document.createElementNS(SVG_NS, 'text');
      dt.setAttribute('x', x + 4);
      dt.setAttribute('y', y + NODE_H + 8 + i * 7);
      dt.setAttribute('class', 'node-detail');
      dt.setAttribute('font-size', '6.5');
      dt.textContent = lines[i];
      group.appendChild(dt);
    }
  }
  function makePill(node, vw, x, y) {
    let pillLabel = null;
    if (vw.kind === 'chain') pillLabel = '\u00d7' + vw.chainCount;
    else if (vw.hiddenCount > 0) pillLabel = '+' + vw.hiddenCount;
    else if (vw.kind === 'branch') pillLabel = '\u2212';
    if (!pillLabel) return null;
    const pw = Math.max(30, pillLabel.length * 6.2 + 16);
    const ph = 20;
    const px = x + NODE_W - pw - 6;
    const py = y + NODE_H - ph - 6;
    const pg = document.createElementNS(SVG_NS, 'g');
    pg.setAttribute('class', 'pill');
    const hit = document.createElementNS(SVG_NS, 'rect');
    hit.setAttribute('x', px - 5);
    hit.setAttribute('y', py - 5);
    hit.setAttribute('width', pw + 10);
    hit.setAttribute('height', ph + 10);
    hit.setAttribute('fill', 'transparent');
    pg.appendChild(hit);
    const pr = document.createElementNS(SVG_NS, 'rect');
    pr.setAttribute('class', 'cap');
    pr.setAttribute('x', px);
    pr.setAttribute('y', py);
    pr.setAttribute('width', pw);
    pr.setAttribute('height', ph);
    pr.setAttribute('rx', ph / 2);
    pr.setAttribute('ry', ph / 2);
    const pt = document.createElementNS(SVG_NS, 'text');
    pt.setAttribute('x', px + pw / 2);
    pt.setAttribute('y', py + ph / 2);
    pt.textContent = pillLabel;
    pg.appendChild(pr);
    pg.appendChild(pt);
    pg.setAttribute('data-label', pillLabel);
    return pg;
  }
  function syncPill(group, node, x, y) {
    const vw = node._view;
    let label = null;
    if (vw) {
      if (vw.kind === 'chain') label = '\u00d7' + vw.chainCount;
      else if (vw.hiddenCount > 0) label = '+' + vw.hiddenCount;
      else if (vw.kind === 'branch') label = '\u2212';
    }
    const existing = group.querySelector('.pill');
    if (!label) { if (existing) existing.remove(); return; }
    if (existing && existing.getAttribute('data-label') === label) {
      const pw = Math.max(30, label.length * 6.2 + 16);
      const ph = 20;
      const px = x + NODE_W - pw - 6;
      const py = y + NODE_H - ph - 6;
      const rects = existing.querySelectorAll('rect');
      if (rects.length === 2) {
        rects[0].setAttribute('x', px - 5); rects[0].setAttribute('y', py - 5);
        rects[0].setAttribute('width', pw + 10); rects[0].setAttribute('height', ph + 10);
        rects[1].setAttribute('x', px); rects[1].setAttribute('y', py);
        rects[1].setAttribute('width', pw); rects[1].setAttribute('height', ph);
      }
      const t = existing.querySelector('text');
      if (t) { t.setAttribute('x', px + pw / 2); t.setAttribute('y', py + ph / 2); }
      return;
    }
    if (existing) existing.remove();
    if (vw) { const pg = makePill(node, vw, x, y); if (pg) group.appendChild(pg); }
  }
  function drawNodes(node, g, layoutMap) {
    const lm = layoutMap ? layoutMap[node.id] : null;
    const absX = lm ? lm.absX : node._absX;
    const absY = lm ? lm.absY : node._absY;
    // Viewport culling: skip DOM creation for nodes outside view (with padding)
    const vp = layoutMap ? getViewportBounds() : null;
    const bbox = lm ? lm.bbox : null;
    const pad = 80;
    const expandedVp = vp ? { x: vp.x - pad, y: vp.y - pad, w: vp.w + pad*2, h: vp.h + pad*2 } : null;
    const visible = !expandedVp || !bbox || isBboxVisible(bbox, expandedVp);
    if (!visible) {
      // Still need to recurse to children which may be in viewport (subtree may span widely)
      if (node.children) {
        for (let i = 0; i < node.children.length; i++) drawNodes(node.children[i], g, layoutMap);
      }
      return;
    }
    const x = absX;
    const y = absY - NODE_H / 2;
    const typeColor = TYPE_COLORS[node.nodeType] || TYPE_COLORS.Unknown;
    const status = node.status ? node.status.LastStatus : 0;
    const statusColor = STATUS_COLORS[status] || '#555';
    const sid = safeId(node.id);
    const hasEffects = node.effects && node.effects.length > 0;
    const hasCondition = !!node.condition || !!node.postCondition;
    const isRunning = status === 1;
    const tier = zoomTier();
    const onActive = !!(node._view && node._view.onActive);

    if (tier === 0) {
      const group = document.createElementNS(SVG_NS, 'g');
      group.setAttribute('id', sid);
      group.setAttribute('class', isRunning ? 'node-running' : '');
      group.setAttribute('data-id', node.id);
      group.style.cursor = 'pointer';
      const dot = document.createElementNS(SVG_NS, 'circle');
      dot.setAttribute('cx', absX + NODE_W / 2);
      dot.setAttribute('cy', absY);
      dot.setAttribute('r', isRunning ? '5' : '3.5');
      dot.setAttribute('fill', isRunning ? statusColor : typeColor);
      dot.setAttribute('class', 'node-dot');
      if (!isRunning && status === 0) dot.setAttribute('opacity', '0.5');
      group.appendChild(dot);
      g.appendChild(group);
      if (node.children) {
        for (const c of node.children) drawNodes(c, g, layoutMap);
      }
      return;
    }

    const group = document.createElementNS(SVG_NS, 'g');
    group.setAttribute('id', sid);
    group.setAttribute('class', isRunning ? 'node-running' : '');
    group.setAttribute('data-id', node.id);
    group.setAttribute('role', 'treeitem');
    group.setAttribute('aria-label', (node.nodeType || 'node') + ' ' + (node.name || node.id) + ', ' + statusToString(status) + (node.status ? ', ' + node.status.TickCount + ' ticks' : ''));
    group.style.cursor = 'pointer';

    const ring = document.createElementNS(SVG_NS, 'rect');
    ring.setAttribute('x', x - 3);
    ring.setAttribute('y', y - 3);
    ring.setAttribute('width', NODE_W + 6);
    ring.setAttribute('height', NODE_H + 6);
    ring.setAttribute('class', 'node-ring');
    group.appendChild(ring);

    var card = document.createElementNS(SVG_NS, 'rect');
    card.setAttribute('x', x);
    card.setAttribute('y', y);
    card.setAttribute('width', NODE_W);
    card.setAttribute('height', NODE_H);
    card.setAttribute('fill', cssVar('--bg-card', '#101a29'));
    card.setAttribute('stroke', typeColor);
    card.setAttribute('stroke-width', '1.5');
    card.setAttribute('class', 'node-card');
    card.setAttribute('rx', '6');
    card.setAttribute('ry', '6');
    if (!isRunning && status === 0) card.setAttribute('opacity', '0.45');
    group.appendChild(card);

    var typebar = document.createElementNS(SVG_NS, 'rect');
    typebar.setAttribute('x', x);
    typebar.setAttribute('y', y);
    typebar.setAttribute('width', '3');
    typebar.setAttribute('height', NODE_H);
    typebar.setAttribute('fill', typeColor);
    typebar.setAttribute('class', 'node-typebar');
    group.appendChild(typebar);

    var glyph = makeStatusGlyph(x + NODE_W - 14, y + 12, status, statusColor);
    if (glyph) { glyph.setAttribute('data-glyph', '1'); group.appendChild(glyph); }

    var typeText = document.createElementNS(SVG_NS, 'text');
    typeText.setAttribute('x', x + 12);
    typeText.setAttribute('y', y + 18);
    typeText.setAttribute('class', 'node-type-label');
    typeText.setAttribute('fill', typeColor);
    typeText.setAttribute('font-size', '8.5');
    typeText.textContent = node.nodeType || 'Unknown';
    group.appendChild(typeText);

    var nameText = document.createElementNS(SVG_NS, 'text');
    nameText.setAttribute('x', x + 12);
    nameText.setAttribute('y', y + 36);
    nameText.setAttribute('class', 'node-text');
    nameText.setAttribute('fill', cssVar('--text-primary', '#dbe7f4'));
    nameText.setAttribute('font-size', '10.5');
    nameText.textContent = truncate(node.name || '', 22);
    group.appendChild(nameText);

    if (hasEffects) {
      var eb = document.createElementNS(SVG_NS, 'circle');
      eb.setAttribute('cx', x + 16);
      eb.setAttribute('cy', y + NODE_H - 12);
      eb.setAttribute('r', '3');
      eb.setAttribute('fill', cssVar('--success', '#46d68c'));
      eb.setAttribute('class', 'node-badge');
      group.appendChild(eb);
    }
    if (hasCondition) {
      var cb = document.createElementNS(SVG_NS, 'rect');
      cb.setAttribute('x', x + 26);
      cb.setAttribute('y', y + NODE_H - 15);
      cb.setAttribute('width', '6');
      cb.setAttribute('height', '6');
      cb.setAttribute('fill', cssVar('--type-pre', '#2dd4bf'));
      cb.setAttribute('class', 'node-badge');
      group.appendChild(cb);
    }

    const vw = node._view;
    if (vw) {
      const pg = makePill(node, vw, x, y);
      if (pg) group.appendChild(pg);
    }

    appendDetailText(group, node, x, y, zoomTier());

    g.appendChild(group);
    if (node.children) {
      for (const c of node.children) drawNodes(c, g, layoutMap);
    }
  }

  function selectNode(node) {
    selectedNodeId = node.id;
    writeUrlState(true);
    document.getElementById('detail-empty').style.display = 'none';
    document.getElementById('detail-body').style.display = 'block';

    document.getElementById('dtl-name').textContent = node.name || node.nodeType || 'Unknown';
    const typeBadge = '<span class="dtl-badge" style="background:#0a111d;color:' + escapeAttr(TYPE_COLORS[node.nodeType] || '#9fb4cc') + ';border:1px solid ' + escapeAttr(TYPE_COLORS[node.nodeType] || '#666') + ';">' + escapeHtml(node.nodeType || 'Unknown') + '</span>';
    const st = node.status ? node.status.LastStatus : 0;
    const stStr = statusToString(st);
    const stColor = STATUS_COLORS[st] || '#888';
    const statusBadge = '<span class="dtl-badge" style="background:' + escapeAttr(stColor) + '22;border:1px solid ' + escapeAttr(stColor) + ';color:' + escapeAttr(stColor) + ';">' + escapeHtml(stStr) + (node.status ? ' x' + node.status.TickCount : '') + '</span>';
    document.getElementById('dtl-type').innerHTML = typeBadge;
    document.getElementById('dtl-status').innerHTML = statusBadge;

    const frameWrap = document.getElementById('dtl-frame-wrap');
    if (node.frame) {
      frameWrap.style.display = 'block';
      document.getElementById('dtl-frame').textContent = node.frame;
    } else {
      frameWrap.style.display = 'none';
    }

    const effectsWrap = document.getElementById('dtl-effects-wrap');
    if (node.effects && node.effects.length > 0) {
      effectsWrap.style.display = 'block';
      document.getElementById('dtl-effects').innerHTML = node.effects.map(e =>
        '<span class="dtl-badge dtl-badge-effects">' + escapeHtml(e.key) + '=' + escapeHtml(e.value) + '</span>'
      ).join('');
    } else {
      effectsWrap.style.display = 'none';
    }

    const condWrap = document.getElementById('dtl-cond-wrap');
    if (node.condition) {
      condWrap.style.display = 'block';
      document.getElementById('dtl-cond').innerHTML = '<span class="dtl-badge dtl-badge-cond">' + escapeHtml(node.condition) + '</span>';
    } else {
      condWrap.style.display = 'none';
    }

    const postWrap = document.getElementById('dtl-post-wrap');
    if (node.postCondition) {
      postWrap.style.display = 'block';
      document.getElementById('dtl-post').innerHTML = '<span class="dtl-badge dtl-badge-cond">' + escapeHtml(node.postCondition) + '</span>';
    } else {
      postWrap.style.display = 'none';
    }

    fetchProfile(node.id);
    updateBreadcrumb(node.id);

    svg.querySelectorAll('.node-ring').forEach(r => r.classList.remove('selected'));
    const sid = safeId(node.id);
    const el = document.getElementById(sid);
    const ring = el ? el.querySelector('.node-ring') : null;
    if (ring) ring.classList.add('selected');
  }

  function displayCachedProfile(nodeId, data) {
    var wrap = document.getElementById('dtl-profile-wrap');
    var prof = null;
    for (var i = 0; i < data.length; i++) {
      if (data[i].id === nodeId) { prof = data[i]; break; }
    }
    if (!prof) { wrap.style.display = 'none'; return; }
    wrap.style.display = 'block';
    var barMax = Math.max(prof.tickCount || 1, prof.successCount || 0, prof.failureCount || 0, prof.runningCount || 0);
    var makeBar = function(label, val, color) {
      var w = barMax > 0 ? Math.round((val / barMax) * 120) : 0;
      return '<div style="display:flex;align-items:center;gap:4px;margin-bottom:3px;"><span style="width:50px;font-size:9px;color:var(--text-secondary);">' + label + '</span><div style="width:' + w + 'px;height:8px;background:' + color + ';border-radius:2px;"></div><span style="font-size:9px;">' + val + '</span></div>';
    };
    document.getElementById('dtl-profile').innerHTML =
      makeBar('Ticks', prof.tickCount, cssVar('--text-secondary', '#8296ad')) +
      makeBar('Success', prof.successCount || 0, cssVar('--success', '#46d68c')) +
      makeBar('Failure', prof.failureCount || 0, cssVar('--failure', '#f0566a')) +
      makeBar('Running', prof.runningCount || 0, cssVar('--running', '#f5a524'));
  }

  function fetchProfile(nodeId) {
    var wrap = document.getElementById('dtl-profile-wrap');
    if (profileCache && profileCacheIter === currentIteration) {
      displayCachedProfile(nodeId, profileCache);
      return;
    }
    fetch(planPath('/profile'))
      .then(function(r) { return r.ok ? r.json() : null; })
      .then(function(data) {
        if (!data) { wrap.style.display = 'none'; return; }
        profileCache = data;
        profileCacheIter = currentIteration;
        displayCachedProfile(nodeId, data);
      });
  }

  function updateMinimap() {
    const mmSvg = document.getElementById('minimap-svg');
    const mmVp = document.getElementById('minimap-viewport');
    if (!mmSvg || !mmVp || !currentTree || !currentLayoutMap) return;
    const mmW = 140, mmH = 100;
    let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
    for (const id in currentLayoutMap) {
      const e = currentLayoutMap[id];
      if (e.absX < minX) minX = e.absX;
      if (e.absY < minY) minY = e.absY;
      if (e.absX + NODE_W > maxX) maxX = e.absX + NODE_W;
      if (e.absY + NODE_H > maxY) maxY = e.absY + NODE_H;
    }
    if (!isFinite(minX)) return;
    const pad = 20;
    minX -= pad; minY -= pad; maxX += pad; maxY += pad;
    const treeW = maxX - minX, treeH = maxY - minY;
    const scale = Math.min(mmW / treeW, mmH / treeH);
    mmSvg.innerHTML = '';
    const offsetX = (mmW - treeW * scale) / 2;
    const offsetY = (mmH - treeH * scale) / 2;
    for (const id in currentLayoutMap) {
      const e = currentLayoutMap[id];
      const r = document.createElementNS(SVG_NS, 'rect');
      r.setAttribute('x', offsetX + (e.absX - minX) * scale);
      r.setAttribute('y', offsetY + (e.absY - minY) * scale);
      r.setAttribute('width', Math.max(1.5, NODE_W * scale));
      r.setAttribute('height', Math.max(1, NODE_H * scale));
      r.setAttribute('fill', 'rgba(83,216,251,0.25)');
      mmSvg.appendChild(r);
    }
    const vb = svg.getAttribute('viewBox');
    if (vb) {
      const parts = vb.split(/[\s,]+/).map(Number);
      if (parts.length === 4) {
        const vx = parts[0], vy = parts[1], vw = parts[2], vh = parts[3];
        const rx = offsetX + (vx - minX) * scale;
        const ry = offsetY + (vy - minY) * scale;
        const rw = vw * scale;
        const rh = vh * scale;
        mmVp.style.left = Math.max(0, rx) + 'px';
        mmVp.style.top = Math.max(0, ry) + 'px';
        mmVp.style.width = Math.min(mmW, rw) + 'px';
        mmVp.style.height = Math.min(mmH, rh) + 'px';
      }
    }
    mmSvg.onclick = function(e) {
      const rect = mmSvg.getBoundingClientRect();
      const mx = e.clientX - rect.left;
      const my = e.clientY - rect.top;
      const worldX = (mx - offsetX) / scale + minX;
      const worldY = (my - offsetY) / scale + minY;
      const cw = svgContainer.clientWidth || 800;
      const ch = svgContainer.clientHeight || 600;
      pan.x = worldX - cw / (zoom * 2);
      pan.y = worldY - ch / (zoom * 2);
      userAdjustedView = true;
      updateSvgView();
    };
  }

  // URL state: #plan=<id>&iter=<n>&node=<path> deep-links a debug moment.
  let urlSuppress = false;
  function writeUrlState(push) {
    if (urlSuppress) return;
    const parts = [];
    if (currentPlanId) parts.push('plan=' + encodeURIComponent(currentPlanId));
    if (currentIteration != null) parts.push('iter=' + currentIteration);
    if (selectedNodeId) parts.push('node=' + encodeURIComponent(selectedNodeId));
    const hash = '#' + parts.join('&');
    if (hash === window.location.hash) return;
    if (push) history.pushState(null, '', hash);
    else history.replaceState(null, '', hash);
  }
  function parseUrlState() {
    const h = window.location.hash.replace(/^#/, '');
    const out = {};
    if (!h) return out;
    h.split('&').forEach(function(kv) {
      const i = kv.indexOf('=');
      if (i < 0) return;
      out[kv.slice(0, i)] = decodeURIComponent(kv.slice(i + 1));
    });
    return out;
  }
  function applyUrlState(st) {
    if (!st) return;
    urlSuppress = true;
    if (st.plan && st.plan !== currentPlanId && planSelect.querySelector('option[value="' + CSS.escape(st.plan) + '"]')) {
      planSelect.value = st.plan;
      switchPlan(st.plan);
    }
    const iter = st.iter != null ? parseInt(st.iter, 10) : NaN;
    const node = st.node || null;
    const finish = function() {
      if (node) {
        const tree = currentTree || lastRawTree;
        const n = tree ? findNode(tree, node) : null;
        if (n) { selectNode(n); panToNodeId(node); }
        else pendingRestoreNodeId = node;
      }
      urlSuppress = false;
    };
    if (!isNaN(iter)) { jumpToIteration(iter); setTimeout(finish, 350); }
    else setTimeout(finish, 50);
  }
  window.addEventListener('popstate', function() { applyUrlState(parseUrlState()); });
  window.addEventListener('hashchange', function() { applyUrlState(parseUrlState()); });

  function toggleShortcutsOverlay() {
    const overlay = document.getElementById('shortcuts-overlay');
    if (!overlay) return;
    overlay.style.display = overlay.style.display === 'none' ? 'flex' : 'none';
  }
  (function() {
    const btn = document.getElementById('btn-shortcuts');
    if (btn) btn.addEventListener('click', toggleShortcutsOverlay);
    const closeBtn = document.getElementById('btn-close-shortcuts');
    if (closeBtn) closeBtn.addEventListener('click', toggleShortcutsOverlay);
    const overlay = document.getElementById('shortcuts-overlay');
    if (overlay) overlay.addEventListener('click', function(e) {
      if (e.target === overlay) overlay.style.display = 'none';
    });
  })();

  function subtreeBreakdown(node) {
    const counts = {};
    let running = 0;
    (function walk(n) {
      if (n.nodeType) counts[n.nodeType] = (counts[n.nodeType] || 0) + 1;
      if (n.status && n.status.LastStatus === 1) running++;
      if (n.children) for (let i = 0; i < n.children.length; i++) walk(n.children[i]);
    })(node);
    const parts = Object.keys(counts).sort(function(a, b) { return counts[b] - counts[a]; }).slice(0, 4).map(function(k) { return counts[k] + ' ' + k; });
    return { parts: parts, running: running, total: Object.keys(counts).reduce(function(s, k) { return s + counts[k]; }, 0) };
  }
  function showTooltip(e, node) {
    const st = node.status ? statusToString(node.status.LastStatus) : 'Unknown';
    const stColor = STATUS_COLORS[node.status ? node.status.LastStatus : 0] || '#888';
    const role = node.nodeType || 'Unknown';
    const vw = node._view;
    let html = '<div style="font-weight:600;color:var(--text-primary);margin-bottom:3px;">' + escapeHtml(node.name || role) + '</div>';
    html += '<div style="display:flex;gap:6px;align-items:center;margin-bottom:4px;">';
    html += '<span style="background:#0a111d;color:' + escapeAttr(TYPE_COLORS[role] || '#9fb4cc') + ';border:1px solid ' + escapeAttr(TYPE_COLORS[role] || '#50627a') + ';padding:1px 5px;border-radius:3px;font-size:9px;font-weight:600;">' + escapeHtml(role) + '</span>';
    html += '<span style="color:' + escapeAttr(stColor) + ';">' + escapeHtml(st) + '</span>';
    if (node.status) html += '<span style="color:var(--text-secondary);">\u00d7' + node.status.TickCount + ' ticks</span>';
    html += '</div>';
    if (node.condition) html += '<div style="color:var(--type-pre);font-size:9.5px;">cond: ' + escapeHtml(truncate(node.condition, 40)) + '</div>';
    if (node.postCondition && node.postCondition !== node.condition) html += '<div style="color:var(--type-ppa);font-size:9.5px;">post: ' + escapeHtml(truncate(node.postCondition, 40)) + '</div>';
    if (node.effects && node.effects.length) html += '<div style="color:var(--success);font-size:9.5px;">eff: ' + escapeHtml(node.effects.map(function(ef) { return ef.key + '=' + ef.value; }).join(', ')) + '</div>';
    if (vw && vw.kind === 'chain' && vw.chainCount > 1) html += '<div style="color:var(--text-secondary);font-size:9.5px;margin-top:3px;">\u00d7' + vw.chainCount + ' compressed chain (click pill to unwind)</div>';
    if (vw && vw.hiddenCount > 0) {
      const tail = findNode(lastRawTree || currentTree, vw.tailId) || node;
      const bd = subtreeBreakdown(tail);
      html += '<div style="color:var(--text-secondary);font-size:9.5px;margin-top:3px;">+' + vw.hiddenCount + ' hidden inside';
      if (bd.parts.length) html += ': ' + escapeHtml(bd.parts.join(', '));
      if (bd.running > 0) html += ' <span style="color:var(--running);">(' + bd.running + ' running)</span>';
      html += '</div>';
    }
    if (vw && vw.onActive) html += '<div style="color:var(--accent);font-size:9.5px;margin-top:3px;">\u25b8 on active path</div>';
    tooltip.innerHTML = html;
    tooltip.style.display = 'block';
    positionTooltip(e);
  }
  function hideTooltip() { tooltip.style.display = 'none'; }
  function positionTooltip(e) {
    const host = tooltip.offsetParent || svgContainer;
    const rect = host.getBoundingClientRect();
    let lx = e.clientX - rect.left + 14;
    let ly = e.clientY - rect.top + 14;
    const tw = tooltip.offsetWidth || 260;
    const th = tooltip.offsetHeight || 90;
    if (lx + tw > rect.width - 8) lx = e.clientX - rect.left - tw - 14;
    if (ly + th > rect.height - 8) ly = e.clientY - rect.top - th - 14;
    tooltip.style.left = Math.max(4, lx) + 'px';
    tooltip.style.top = Math.max(4, ly) + 'px';
  }

  function highlightNode(id) {
    svg.querySelectorAll('.node-ring').forEach(r => r.classList.remove('selected'));
    const sid = safeId(id);
    const el = document.getElementById(sid);
    if (el) {
      const ring = el.querySelector('.node-ring');
      if (ring) ring.classList.add('selected');
    }
  }

  function highlightSearch() {
    svg.querySelectorAll('.node-ring').forEach(r => r.classList.remove('highlight'));
    for (const id of highlightedIds) {
      const sid = safeId(id);
      const el = document.getElementById(sid);
      if (el) {
        const ring = el.querySelector('.node-ring');
        if (ring) ring.classList.add('highlight');
      }
    }
  }

  let viewCullingRaf = null;
  let cullingRenderInProgress = false;
  function scheduleCullingRerender() {
    if (viewCullingRaf || cullingRenderInProgress) return;
    viewCullingRaf = requestAnimationFrame(function(){
      viewCullingRaf = null;
      cullingRenderInProgress = true;
      if (currentTree) renderTree(currentTree);
      cullingRenderInProgress = false;
    });
  }
  function scheduleIdleWork(fn) {
    if (typeof requestIdleCallback === 'function') {
      requestIdleCallback(fn, { timeout: 200 });
    } else {
      setTimeout(fn, 16);
    }
  }
  function updateSvgView() {
    const w = svgContainer.clientWidth || 800;
    const h = svgContainer.clientHeight || 600;
    svg.setAttribute('viewBox', pan.x + ' ' + pan.y + ' ' + (w / zoom) + ' ' + (h / zoom));
    zoomLevel.textContent = Math.round(zoom * 100) + '%';
    if (currentTree && countNodesJS(currentTree) > 200) { scheduleCullingRerender(); }
    if (renderedTier !== null && zoomTier() !== renderedTier && lastRawTree) renderTree(lastRawTree);
  }
  function countNodesJS(tree){
    if(!tree) return 0;
    let c=1;
    if(tree.children) for(let i=0;i<tree.children.length;i++) c+=countNodesJS(tree.children[i]);
    return c;
  }


  function fitToTree() {
    if (!currentTree) return;
    var bbox;
    try { bbox = svg.getBBox(); } catch (e) { return; }
    if (!bbox.width || !bbox.height) return;
    const pad = 40;
    const cw = svgContainer.clientWidth || 800;
    const ch = svgContainer.clientHeight || 600;
    zoom = Math.min(Math.max(Math.min(cw / (bbox.width + pad * 2), ch / (bbox.height + pad * 2), 2), 0.1), 0.1);
    pan.x = bbox.x - pad;
    pan.y = bbox.y - pad;
    updateSvgView();
  }

  function resetView() {
    zoom = 1;
    var bbox = null;
    try { bbox = svg.getBBox(); } catch (e) { bbox = null; }
    const cw = svgContainer.clientWidth || 800;
    const ch = svgContainer.clientHeight || 600;
    if (bbox && bbox.width && bbox.height) {
      pan.x = bbox.x + bbox.width / 2 - cw / 2;
      pan.y = bbox.y + bbox.height / 2 - ch / 2;
    } else {
      pan = { x: 0, y: 0 };
    }
    updateSvgView();
  }

  function drawProfileBar() {
    profileBar.innerHTML = '';
    if (!currentTree) return;
    const flat = flattenTree(lastRawTree || currentTree);
    const maxTicks = Math.max(...flat.map(n => n.status ? n.status.TickCount : 0), 1);
    const totalW = profileBar.clientWidth || 400;
    for (const n of flat) {
      const ticks = n.status ? n.status.TickCount : 0;
      if (ticks === 0) continue;
      const div = document.createElement('div');
      div.className = 'profile-block';
      const w = Math.max(2, (ticks / maxTicks) * totalW);
      div.style.width = w + 'px';
      const st = n.status ? n.status.LastStatus : 0;
      let color = cssVar('--type-unknown', '#50627a');
      if (st === 1) color = cssVar('--running', '#f5a524');
      else if (st === 2) color = cssVar('--success', '#46d68c');
      else if (st === 3) color = cssVar('--failure', '#f0566a');
      div.style.background = color;
      div.title = (n.name || n.nodeType) + ' - ' + ticks + ' ticks';
      div.addEventListener('click', function(e) { e.stopPropagation(); selectNode(n); });
      div.addEventListener('mouseenter', function(e) { showTooltip(e, n); });
      div.addEventListener('mouseleave', hideTooltip);
      profileBar.appendChild(div);
    }
  }

  function flattenTree(node) {
    const res = [node];
    if (node.children) {
      for (const c of node.children) res.push(...flattenTree(c));
    }
    return res;
  }

  function findNode(node, id) {
    if (node.id === id) return node;
    if (node.children) {
      for (const c of node.children) {
        const f = findNode(c, id);
        if (f) return f;
      }
    }
    return null;
  }

  function findNodesByName(node, q) {
    const res = [];
    const nm = (node.name || '').toLowerCase();
    const nt = (node.nodeType || '').toLowerCase();
    if (nm.includes(q) || nt.includes(q)) res.push(node.id);
    if (node.children) {
      for (const c of node.children) res.push(...findNodesByName(c, q));
    }
    return res;
  }

  function panToNodeId(id) {
    const el = document.getElementById(safeId(id));
    if (!el) return;
    const rect = el.getBBox();
    const cw = svgContainer.clientWidth || 800;
    const ch = svgContainer.clientHeight || 600;
    pan.x = rect.x - cw / (zoom * 2) + NODE_W / 2;
    pan.y = rect.y - ch / (zoom * 2) + NODE_H / 2;
    updateSvgView();
  }
  function updateSearchCount() {
    const el = document.getElementById('search-count');
    if (!el) return;
    if (!searchInput.value.trim()) { el.textContent = ''; el.classList.remove('has-matches'); return; }
    if (!searchMatches.length) { el.textContent = '0 matches'; el.classList.remove('has-matches'); return; }
    el.textContent = (searchMatchIdx + 1) + '/' + searchMatches.length;
    el.classList.add('has-matches');
  }
  function cycleSearchMatch(delta) {
    if (!searchMatches.length) return;
    searchMatchIdx = (searchMatchIdx + delta + searchMatches.length) % searchMatches.length;
    panToNodeId(searchMatches[searchMatchIdx]);
    updateSearchCount();
  }
  function clearSearch() {
    searchInput.value = '';
    highlightedIds.clear();
    searchMatches = []; searchMatchIdx = -1;
    if (searchReveal.size) { searchReveal.clear(); bumpView(); renderTree(lastRawTree); }
    svg.querySelectorAll('.node-ring.highlight').forEach(r => r.classList.remove('highlight'));
    updateSearchCount();
  }
  let searchDebounce = null;
  searchInput.addEventListener('input', function() {
    clearTimeout(searchDebounce);
    searchDebounce = setTimeout(function() {
      const q = searchInput.value.trim().toLowerCase();
      highlightedIds.clear();
      searchMatches = []; searchMatchIdx = -1;
      if (!q || !currentTree) {
        if (searchReveal.size) { searchReveal.clear(); bumpView(); renderTree(lastRawTree); }
        svg.querySelectorAll('.node-ring.highlight').forEach(r => r.classList.remove('highlight'));
        updateSearchCount();
        return;
      }
      searchMatches = findNodesByName(lastRawTree || currentTree, q);
      highlightedIds = new Set(searchMatches);
      if (revealAncestors(highlightedIds)) { bumpView(); renderTree(lastRawTree); }
      highlightSearch();
      if (searchMatches.length) { searchMatchIdx = 0; panToNodeId(searchMatches[0]); }
      updateSearchCount();
    }, 300);
  });
  searchInput.addEventListener('keydown', function(e) {
    if (e.key === 'Enter') { e.preventDefault(); cycleSearchMatch(e.shiftKey ? -1 : 1); }
    else if (e.key === 'ArrowDown') { e.preventDefault(); cycleSearchMatch(1); }
    else if (e.key === 'ArrowUp') { e.preventDefault(); cycleSearchMatch(-1); }
    else if (e.key === 'Escape') { e.preventDefault(); clearSearch(); this.blur(); }
  });
  document.getElementById('search-next').addEventListener('click', function() { cycleSearchMatch(1); });
  document.getElementById('search-prev').addEventListener('click', function() { cycleSearchMatch(-1); });
  document.getElementById('search-clear').addEventListener('click', clearSearch);

  svgContainer.addEventListener('mousedown', function(e) {
    if (e.button === 0 || e.button === 1) {
      isDragging = true;
      dragStart = { x: e.clientX, y: e.clientY };
      panStart = { x: pan.x, y: pan.y };
      e.preventDefault();
    }
  });
  window.addEventListener('mousemove', function(e) {
    if (!isDragging) {
      if (tooltip.style.display === 'block') positionTooltip(e);
      return;
    }
    const dx = (e.clientX - dragStart.x) / zoom;
    const dy = (e.clientY - dragStart.y) / zoom;
    pan.x = panStart.x - dx;
    pan.y = panStart.y - dy;
    userAdjustedView = true;
    updateSvgView();
  });
  window.addEventListener('mouseup', function() { isDragging = false; });
  function zoomAtPoint(clientX, clientY, factor) {
    const rect = svgContainer.getBoundingClientRect();
    if (!rect.width || !rect.height || !isFinite(clientX) || !isFinite(clientY) || !isFinite(factor) || factor <= 0) return;
    const vb = svg.getAttribute('viewBox');
    if (!vb) { zoom = Math.min(Math.max(zoom * factor, 0.1), 5); userAdjustedView = true; updateSvgView(); return; }
    const parts = vb.split(/[\s,]+/).map(Number);
    if (parts.length !== 4 || parts.some(function(p) { return !isFinite(p); })) { zoom = Math.min(Math.max(zoom * factor, 0.1), 5); userAdjustedView = true; updateSvgView(); return; }
    const vx = parts[0], vy = parts[1], vw = parts[2], vh = parts[3];
    const worldX = vx + ((clientX - rect.left) / rect.width) * vw;
    const worldY = vy + ((clientY - rect.top) / rect.height) * vh;
    const newZoom = Math.min(Math.max(zoom * factor, 0.1), 5);
    const newVw = rect.width / newZoom;
    const newVh = rect.height / newZoom;
    const nx = worldX - ((clientX - rect.left) / rect.width) * newVw;
    const ny = worldY - ((clientY - rect.top) / rect.height) * newVh;
    if (!isFinite(nx) || !isFinite(ny)) return;
    pan.x = nx;
    pan.y = ny;
    zoom = newZoom;
    userAdjustedView = true;
    updateSvgView();
  }
  // Trackpad pinch arrives as ctrl/meta+wheel with small deltaY; plain scroll is pan.
  svgContainer.addEventListener('wheel', function(e) {
    e.preventDefault();
    if (e.ctrlKey || e.metaKey) {
      zoomAtPoint(e.clientX, e.clientY, Math.pow(0.99, e.deltaY));
    } else {
      pan.x += e.deltaX / zoom;
      pan.y += e.deltaY / zoom;
      userAdjustedView = true;
      updateSvgView();
    }
  }, { passive: false });
  let gestureScale = 1;
  svgContainer.addEventListener('gesturestart', function(e) { e.preventDefault(); gestureScale = 1; }, { passive: false });
  svgContainer.addEventListener('gesturechange', function(e) {
    e.preventDefault();
    if (e.scale && e.scale > 0) {
      zoomAtPoint(e.clientX, e.clientY, e.scale / gestureScale);
      gestureScale = e.scale;
    }
  }, { passive: false });
  svgContainer.addEventListener('gestureend', function() { gestureScale = 1; }, { passive: false });
  let touchState = null;
  svgContainer.addEventListener('touchstart', function(e) {
    if (e.touches.length === 1) {
      const t = e.touches[0];
      touchState = { mode: 'pan', x: t.clientX, y: t.clientY };
    } else if (e.touches.length === 2) {
      const a = e.touches[0], b = e.touches[1];
      touchState = { mode: 'pinch', dist: Math.hypot(b.clientX - a.clientX, b.clientY - a.clientY), cx: (a.clientX + b.clientX) / 2, cy: (a.clientY + b.clientY) / 2 };
    } else {
      touchState = null;
    }
  }, { passive: true });
  svgContainer.addEventListener('touchmove', function(e) {
    if (!touchState) return;
    e.preventDefault();
    if (touchState.mode === 'pan' && e.touches.length === 1) {
      const t = e.touches[0];
      pan.x -= (t.clientX - touchState.x) / zoom;
      pan.y -= (t.clientY - touchState.y) / zoom;
      touchState.x = t.clientX;
      touchState.y = t.clientY;
      userAdjustedView = true;
      updateSvgView();
    } else if (touchState.mode === 'pinch' && e.touches.length === 2) {
      const a = e.touches[0], b = e.touches[1];
      const dist = Math.hypot(b.clientX - a.clientX, b.clientY - a.clientY);
      const cx = (a.clientX + b.clientX) / 2, cy = (a.clientY + b.clientY) / 2;
      if (touchState.dist > 0 && dist > 0) zoomAtPoint(cx, cy, dist / touchState.dist);
      pan.x -= (cx - touchState.cx) / zoom;
      pan.y -= (cy - touchState.cy) / zoom;
      touchState.dist = dist;
      touchState.cx = cx;
      touchState.cy = cy;
      userAdjustedView = true;
      updateSvgView();
    }
  }, { passive: false });
  svgContainer.addEventListener('touchend', function(e) {
    if (e.touches.length === 0) touchState = null;
    else if (e.touches.length === 1) { const t = e.touches[0]; touchState = { mode: 'pan', x: t.clientX, y: t.clientY }; }
  }, { passive: true });
  (function bindPanelResize() {
    const app = document.getElementById('app');
    function bind(handleId, varName, min, max, invert, fallback) {
      const h = document.getElementById(handleId);
      if (!h) return;
      h.addEventListener('mousedown', function(e) {
        e.preventDefault();
        const startX = e.clientX;
        const cur = parseFloat(getComputedStyle(app).getPropertyValue(varName));
        const startW = isNaN(cur) ? fallback : cur;
        function move(ev) {
          let w = startW + (invert ? startX - ev.clientX : ev.clientX - startX);
          w = Math.max(min, Math.min(max, w));
          app.style.setProperty(varName, w + 'px');
        }
        function up() { window.removeEventListener('mousemove', move); window.removeEventListener('mouseup', up); }
        window.addEventListener('mousemove', move);
        window.addEventListener('mouseup', up);
      });
    }
    bind('resize-tl', '--col-timeline', 140, 420, false, 220);
    bind('resize-dt', '--col-detail', 180, 480, true, 280);
  })();
  // Interaction is delegated once on the SVG: per-node listeners died whenever a
  // group was rebuilt during live ticks, which made pill expand/collapse work
  // only intermittently and hover states blink. Handlers resolve the node through
  // currentViewById so they always act on the freshest view state.
  svg.addEventListener('click', function(e) {
    const pill = e.target.closest ? e.target.closest('.pill') : null;
    if (pill) {
      const g = pill.closest('[data-id]');
      const node = g && currentViewById ? currentViewById.get(g.getAttribute('data-id')) : null;
      if (!node || !node._view) return;
      const vw = node._view;
      if (vw.kind === 'chain') unwoundHeads.add(node.id);
      else if (vw.hiddenCount > 0) { userExpanded.add(vw.tailId); userCollapsed.delete(vw.tailId); }
      else { userCollapsed.add(vw.tailId); userExpanded.delete(vw.tailId); }
      bumpView();
      renderTree(lastRawTree);
      return;
    }
    const g = e.target.closest ? e.target.closest('[data-id]') : null;
    if (g && currentViewById) {
      const node = currentViewById.get(g.getAttribute('data-id'));
      if (node) selectNode(node);
    }
  });
  svg.addEventListener('mouseover', function(e) {
    const g = e.target.closest ? e.target.closest('[data-id]') : null;
    if (!g || (e.relatedTarget && g.contains(e.relatedTarget))) return;
    const node = currentViewById ? currentViewById.get(g.getAttribute('data-id')) : null;
    if (node) showTooltip(e, node);
  });
  svg.addEventListener('mouseout', function(e) {
    const g = e.target.closest ? e.target.closest('[data-id]') : null;
    if (!g || (e.relatedTarget && g.contains(e.relatedTarget))) return;
    hideTooltip();
  });
  svgContainer.addEventListener('dblclick', function(e) {
    const target = (e.target && e.target.closest) ? e.target.closest('[data-id]') : null;
    if (target) {
      const id = target.getAttribute('data-id');
      const tree = currentTree || lastRawTree;
      const node = tree ? findNode(tree, id) : null;
      if (node) {
        const bbox = target.getBBox();
        const cw = svgContainer.clientWidth || 800;
        const ch = svgContainer.clientHeight || 600;
        zoom = 1.5;
        pan.x = bbox.x - cw / (zoom * 2);
        pan.y = bbox.y - ch / (zoom * 2);
        updateSvgView();
      }
    } else {
      fitToTree();
    }
  });

  document.getElementById('btn-breakpoint').addEventListener('click', function() {
    if (!selectedNodeId) return;
    const btn = this;
    fetch(planPath('/breakpoints'), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ id: selectedNodeId, nodePath: selectedNodeId, nodeType: '', enabled: true })
    }).then(function(r) {
      if (r.ok) btn.textContent = 'Breakpoint Set';
      else btn.textContent = 'Error';
      setTimeout(function() { btn.textContent = 'Toggle Breakpoint'; }, 1500);
    });
  });

  document.getElementById('btn-fit').addEventListener('click', fitToTree);
  document.getElementById('btn-reset').addEventListener('click', resetView);
  document.getElementById('btn-expand-all').addEventListener('click', function() { expandAllMode = true; bumpView(); renderTree(lastRawTree); });
  document.getElementById('btn-collapse-all').addEventListener('click', function() { expandAllMode = false; userExpanded.clear(); unwoundHeads.clear(); bumpView(); renderTree(lastRawTree); });
  var btnWhy = document.getElementById('btn-why');
  btnWhy.addEventListener('click', function() {
    whyMode = !whyMode;
    btnWhy.classList.toggle('active', whyMode);
    drawCausalOverlay();
  });

  btnLive.addEventListener('click', function() {
    liveMode = !liveMode;
    btnLive.classList.toggle('active', liveMode);
    if (liveMode) btnPlay.textContent = '\u25B6';
  });
  btnLive.classList.add('active');

  function cancelPlaybackRaf() { if (playbackRaf != null) { cancelAnimationFrame(playbackRaf); playbackRaf = null; } }
  btnPlay.addEventListener('click', function() {
    if (playback) {
      playback = false;
      cancelPlaybackRaf();
      btnPlay.textContent = '\u25B6';
      return;
    }
    if (!timelineEvents.length) return;
    liveMode = false;
    btnLive.classList.remove('active');
    playback = true;
    playbackIdx = 0;
    playbackSpeed = parseFloat(document.getElementById('speed-select').value) || 1;
    playbackLastTime = performance.now();
    btnPlay.textContent = '\u25A0';
    function step(now) {
      if (!playback) return;
      const intervalMs = 1000 / playbackSpeed;
      if (now - playbackLastTime >= intervalMs) {
        if (playbackIdx >= timelineEvents.length) {
          playback = false;
          cancelPlaybackRaf();
          btnPlay.textContent = '\u25B6';
          return;
        }
        const ev = timelineEvents[playbackIdx];
        currentIteration = ev.iteration;
        // Use virtual render (no innerHTML wipe)
        renderTimeline();
        fetchTimelineIter(ev.iteration);
        playbackIdx++;
        playbackLastTime = now;
        // Allow speed changes to take effect immediately without teardown
        playbackSpeed = parseFloat(document.getElementById('speed-select').value) || playbackSpeed;
      }
      playbackRaf = requestAnimationFrame(step);
    }
    playbackRaf = requestAnimationFrame(step);
  });
  // Speed changes apply on next frame without timer recreation
  document.getElementById('speed-select').addEventListener('change', function(){
    playbackSpeed = parseFloat(this.value) || 1;
  });


  // ---- Disk Persistence: Save / Load Session (File System Access API with fallback) ----
  async function exportViaFSAPI(blob, suggestedName) {
    // Try File System Access API, fall back to download anchor
    if (window.showSaveFilePicker) {
      try {
        const handle = await window.showSaveFilePicker({
          suggestedName: suggestedName || 'pabt-session.jsonl',
          types: [{ description: 'PA-BT session (JSONL)', accept: { 'application/jsonl': ['.jsonl'], 'text/plain': ['.jsonl'] } }],
        });
        const writable = await handle.createWritable();
        await writable.write(blob);
        await writable.close();
        return true;
      } catch (e) {
        if (e && e.name === 'AbortError') return true; // user cancelled
        // fall through to fallback
      }
    }
    const a = document.createElement('a');
    a.href = URL.createObjectURL(blob);
    a.download = suggestedName || 'pabt-session.jsonl';
    document.body.appendChild(a);
    a.click();
    setTimeout(function(){
      URL.revokeObjectURL(a.href);
      a.remove();
    }, 1000);
    return false;
  }
  async function importViaFSAPI() {
    if (window.showOpenFilePicker) {
      try {
        const [handle] = await window.showOpenFilePicker({
          types: [{ description: 'PA-BT session (JSONL)', accept: { 'application/jsonl': ['.jsonl'], 'text/plain': ['.jsonl', '.json'] } }],
          excludeAcceptAllOption: false,
        });
        const file = await handle.getFile();
        return file;
      } catch (e) {
        if (e && e.name === 'AbortError') return null;
        return null;
      }
    }
    return null; // caller will use input[type=file] fallback
  }
  function uploadViaHiddenInput() {
    return new Promise(function(resolve){
      const input = document.createElement('input');
      input.type = 'file';
      input.accept = '.jsonl,.json,application/jsonl,text/plain';
      input.style.display = 'none';
      document.body.appendChild(input);
      input.addEventListener('change', function(){
        const f = input.files && input.files[0];
        input.remove();
        resolve(f || null);
      });
      input.addEventListener('cancel', function(){ input.remove(); resolve(null); });
      // Fallback if cancel not fired (some browsers): resolve null after timeout if no file
      setTimeout(function(){ if (document.body.contains(input) && !input.files.length) { /* still open picker */ } }, 5000);
      input.click();
    });
  }

  document.getElementById('btn-export').addEventListener('click', function() {
    fetch(planPath('/dot'))
      .then(r => r.ok ? r.text() : '')
      .then(text => {
        if (!text) return;
        const blob = new Blob([text], { type: 'text/plain' });
        exportViaFSAPI(blob, 'debug.dot');
      });
  });
  document.getElementById('btn-save').addEventListener('click', async function(){
    const btn = this; const prev = btn.textContent;
    try {
      btn.textContent = 'Saving...'; btn.disabled = true;
      const res = await fetch(planPath('/export'));
      if (!res.ok) throw new Error('export failed: ' + res.status);
      const blob = await res.blob();
      await exportViaFSAPI(blob, 'pabt-session-' + Date.now() + '.jsonl');
    } catch (e) {
      console.error(e);
      alert('Save failed: ' + (e && e.message || e));
    } finally {
      btn.textContent = prev; btn.disabled = false;
    }
  });
  document.getElementById('btn-load').addEventListener('click', async function(){
    const btn = this; const prev = btn.textContent;
    try {
      btn.textContent = 'Loading...'; btn.disabled = true;
      let file = await importViaFSAPI();
      if (!file) file = await uploadViaHiddenInput();
      if (!file) { btn.textContent = prev; btn.disabled = false; return; }
      const body = await file.text();
      // POST to /import
      const res = await fetch(planPath('/import'), {
        method: 'POST',
        headers: { 'Content-Type': 'text/plain' },
        body: body,
      });
      if (!res.ok) {
        const t = await res.text();
        throw new Error(t || ('import failed: '+res.status));
      }
      const data = await res.json().catch(function(){ return {}; });
      // Reload timeline from server
      const tlRes = await fetch(planPath('/timeline'));
      if (tlRes.ok) {
        const tl = await tlRes.json();
        // Replace local cache with imported timeline: clear virtual spacers first
          if (tl.length > MAX_TIMELINE_EVENTS) tl = tl.slice(tl.length - MAX_TIMELINE_EVENTS);
          timelineEvents = tl.map(function(e){ return { iteration: e.iteration, status: (e.status==='Running'?1:(e.status==='Success'?2:(e.status==='Failure'?3:0))), durationMs: e.durationMs||0, seq: 0, nodeCount: e.nodeCount||0 }; });
        // Status strings are "Success"/"Failure"/"Running"; fallback already handled; but server may return string
        // Re-normalize status strings if numeric not matched
        timelineEvents = timelineEvents.map(function(entry, idx){
          const orig = tl[idx];
          let s = 0;
          if (orig.status === 'Running' || orig.status === 1) s = 1;
          else if (orig.status === 'Success' || orig.status === 2) s = 2;
          else if (orig.status === 'Failure' || orig.status === 3) s = 3;
          else s = entry.status;
          entry.status = s;
          return entry;
        });
        currentIteration = timelineEvents.length ? timelineEvents[timelineEvents.length-1].iteration : null;
        renderTimelineVirtual();
        if (currentIteration != null) fetchTimelineIter(currentIteration);
      }
      if (data.imported) connStatus.textContent = 'Loaded ' + data.imported + ' events';
    } catch (e) {
      console.error(e);
      alert('Load failed: ' + (e && e.message || e));
    } finally {
      btn.textContent = prev; btn.disabled = false;
    }
  });


  const diffModal = document.getElementById('diff-modal');
  document.getElementById('btn-diff').addEventListener('click', function() {
    diffModal.style.display = 'flex';
  });
  document.getElementById('btn-diff-cancel').addEventListener('click', function() {
    diffModal.style.display = 'none';
  });
  document.getElementById('btn-diff-run').addEventListener('click', function() {
    const fromIter = document.getElementById('diff-from').value;
    const toIter = document.getElementById('diff-to').value;
    if (!fromIter || !toIter) return;
    diffModal.style.display = 'none';
    fetch(planPath('/diff?from=' + encodeURIComponent(fromIter) + '&to=' + encodeURIComponent(toIter)))
      .then(r => r.ok ? r.json() : null)
      .then(data => {
        if (!data || !currentTree) return;
        // Badge-not-fill diff: keep fill=--bg-card + typebar intact.
        // Mark cards with stroke class; add corner badges +A/-R/~C.
        // Clear prior diff classes first.
        svg.querySelectorAll('.node-card').forEach(function(c) {
          c.classList.remove('diff-added', 'diff-removed', 'diff-changed');
          c.removeAttribute('opacity');
        });
        svg.querySelectorAll('.diff-badge').forEach(function(b) { b.remove(); });
        function badgeAt(el, cls, label) {
          const card = el.querySelector('.node-card');
          if (!card) return;
          const x = parseFloat(card.getAttribute('x')) + NODE_W - 18;
          const y = parseFloat(card.getAttribute('y')) + 14;
          const t = document.createElementNS(SVG_NS, 'text');
          t.setAttribute('x', x);
          t.setAttribute('y', y);
          t.setAttribute('class', 'diff-badge ' + cls);
          t.textContent = label;
          el.appendChild(t);
        }
        if (data.added) {
          for (const dn of data.added) {
            const el = document.getElementById(safeId(dn.id));
            if (el) {
              el.querySelector('.node-card').classList.add('diff-added');
              badgeAt(el, 'diff-badge-added', '+' + (dn.nodeType ? dn.nodeType[0] : 'A'));
            }
          }
        }
        if (data.removed) {
          for (const dn of data.removed) {
            const el = document.getElementById(safeId(dn.id));
            if (el) {
              el.querySelector('.node-card').classList.add('diff-removed');
              badgeAt(el, 'diff-badge-removed', '-' + (dn.nodeType ? dn.nodeType[0] : 'R'));
            }
          }
        }
        if (data.changed) {
          for (const dn of data.changed) {
            const el = document.getElementById(safeId(dn.id));
            if (el) {
              el.querySelector('.node-card').classList.add('diff-changed');
              badgeAt(el, 'diff-badge-changed', '~');
            }
          }
        }
      });
  });

  document.getElementById('btn-next-fail').addEventListener('click', function() { jumpToNextFailure(); });
  document.getElementById('btn-next-change').addEventListener('click', function() { jumpToNextChange(); });

  document.getElementById('btn-fullscreen').addEventListener('click', function() {
    if (!document.fullscreenElement) {
      document.documentElement.requestFullscreen();
    } else {
      document.exitFullscreen();
    }
  });

  window.addEventListener('resize', function() {
    updateSvgView();
  });

  window.addEventListener('beforeunload', function() {
    if (eventSource) eventSource.close();
  });

  const initialState = parseUrlState();
  connect();
  if (initialState.plan || initialState.iter || initialState.node) setTimeout(function() { applyUrlState(initialState); }, 400);
})();
</script>
</body>
</html>`
