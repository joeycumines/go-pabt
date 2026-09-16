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
 display: grid;
 grid-template-areas:
  "header header header"
  "timeline main detail"
  "bottom bottom bottom";
 grid-template-columns: 220px 1fr 280px;
 grid-template-rows: 40px 1fr 60px;
 height: 100vh;
 width: 100vw;
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
.btn:hover { background: var(--bg-main); border-color: var(--accent); }
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
#svg-container {
 flex: 1;
 position: relative;
 overflow: hidden;
 cursor: grab;
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
 border-radius: 3px;
 font-size: 9px;
 margin-right: 4px;
 margin-bottom: 3px;
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
  max-width: 240px;
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
.pill { cursor: pointer; }
.pill circle { fill: var(--bg-card); stroke: var(--border); stroke-width: 1; transition: stroke 0.15s; }
.pill:hover circle { stroke: var(--accent); }
.pill text {
 font-family: var(--font-mono);
 font-size: 8.5px;
 fill: var(--text-secondary);
 text-anchor: middle;
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
</style>
</head>
<body>
<div id="app">
  <header id="header">
    <div id="header-left">
      <h1>PA-BT // DEBUG</h1>
      <div id="conn-dot"></div>
      <span id="conn-status">Connecting...</span>
    </div>
    <div id="search-wrap">
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" style="color:#888;vertical-align:middle;"><circle cx="11" cy="11" r="8"></circle><line x1="21" y1="21" x2="16.65" y2="16.65"></line></svg>
      <input id="search-input" type="text" placeholder="Search nodes..." autocomplete="off">
    </div>
    <div id="header-right">
      <button class="btn" id="btn-save" title="Save Session (File System Access API or download)">Save</button>
      <button class="btn" id="btn-load" title="Load Session (File System Access API or upload)">Load</button>
      <button class="btn" id="btn-export" title="Export DOT">Export DOT</button>
      <button class="btn" id="btn-diff" title="Diff">Diff</button>
      <button class="btn" id="btn-fullscreen" title="Fullscreen">&#9974;</button>
    </div>
  </header>

  <aside id="timeline">
    <div id="timeline-header">
      <span>Timeline</span>
      <div>
        <button class="btn" id="btn-live">Live</button>
        <button class="btn" id="btn-play">&#9654;</button>
        <select class="btn" id="speed-select" style="padding:2px 4px;">
          <option value="0.5">0.5x</option>
          <option value="1" selected>1x</option>
          <option value="2">2x</option>
          <option value="5">5x</option>
        </select>
      </div>
    </div>
    <div id="timeline-list"></div>
  </aside>

  <main id="main">
    <div id="main-toolbar">
      <button class="btn" id="btn-fit">Fit</button>
      <button class="btn" id="btn-reset">Reset</button>
      <button class="btn" id="btn-expand-all" title="Expand every branch">Expand all</button>
      <button class="btn" id="btn-collapse-all" title="Collapse to the active spine">Collapse all</button>
      <span id="stats-readout"></span>
      <span id="zoom-level" style="font-size:10px;color:var(--text-secondary);">100%</span>
    </div>
    <div id="svg-container">
      <svg id="tree-svg" xmlns="http://www.w3.org/2000/svg"></svg>
    </div>
    <div id="tooltip" class="tooltip"></div>
  </main>

  <aside id="detail">
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
  let pendingTree = null, rafId = null;
  let keyframeTree = null, lastReconstructedTree = null, lastReconstructedIteration = null;
  let liveMode = true, playback = false, playbackIdx = 0, playbackRaf = null, playbackLastTime = 0, playbackSpeed = 1;
  let pan = { x: 0, y: 0 }, zoom = 1, isDragging = false, dragStart = null, panStart = { x: 0, y: 0 };
  let profileCache = null, profileCacheIter = null;
  let lastSeq = 0;

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

  function connect() {
    fetchInitialTimeline();
    fetch(window.location.pathname.replace(/\/ui$/, '/plans'))
      .then(function(r) { return r.ok ? r.json() : []; })
      .then(function(plans) {
        var planId = (plans && plans.length > 0) ? plans[0].id : '0';
        startSSE(planId);
      })
      .catch(function() { startSSE('0'); });
  }
  function fetchInitialTimeline() {
    fetch(window.location.pathname.replace(/\/ui$/, '/timeline'))
      .then(function(r) { return r.ok ? r.json() : []; })
      .then(function(tl) {
        if (!tl || !tl.length) return;
        timelineEvents = tl.map(function(e) {
          return { iteration: e.iteration, status: (e.status === 'Running' ? 1 : (e.status === 'Success' ? 2 : (e.status === 'Failure' ? 3 : 0))), durationMs: e.durationMs || 0, seq: 0, nodeCount: e.nodeCount || 0 };
        });
        currentIteration = timelineEvents.length ? timelineEvents[timelineEvents.length - 1].iteration : null;
        renderTimelineVirtual();
        if (currentIteration != null) fetchTimelineIter(currentIteration);
      })
      .catch(function() {});
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
    };
    eventSource.onmessage = function(e) {
      const event = JSON.parse(e.data);
      if (event.seq && lastSeq > 0 && event.seq > lastSeq + 1) {
        connDot.classList.remove('connected');
        connDot.style.background = cssVar('--running', '#f5a524');
        connStatus.textContent = 'Events missed (seq ' + lastSeq + ' to ' + event.seq + ')';
      }
      lastSeq = event.seq || 0;
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
          if (!rafId) {
            rafId = requestAnimationFrame(function() {
              if (pendingTree) renderTree(pendingTree);
              rafId = null;
              pendingTree = null;
            });
          }
        }
      }
    };
    eventSource.onerror = function() {
      connDot.classList.remove('connected');
      connStatus.textContent = 'Reconnecting in ' + (reconnectDelay/1000) + 's...';
      eventSource.close();
      setTimeout(function() {
        reconnectDelay = Math.min(reconnectDelay * 2, 30000);
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
    let toFetch = Math.min(batch, oldestIter - 1);
    if (toFetch <= 0) { virtualFetchingOlder = false; return; }
    let fetched = [];
    let pending = toFetch;
    for (let k = 0; k < toFetch; k++) {
      const iter = oldestIter - 1 - k;
      fetch(window.location.pathname.replace(/\/ui$/, '/timeline/' + iter))
        .then(function(r){ return r.ok ? r.json() : null; })
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
    fetch(window.location.pathname.replace(/\/ui$/, '/timeline/' + iter))
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
      .then(function(data) { if (data && data.tree) { lastReconstructedTree = cloneTreeJS(data.tree); lastReconstructedIteration = data.iteration || iter; renderTree(data.tree); } });
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
    let rendered = 0, rawCount = 0, maxDepth = 0;
    (function rawStats(n, d) { rawCount++; if (d > maxDepth) maxDepth = d; if (n.children) for (let i = 0; i < n.children.length; i++) rawStats(n.children[i], d + 1); })(tree, 0);
    function vm(n, depth) {
      rendered++;
      let run = [n], cur = n;
      while (cur.children && cur.children.length === 1) { cur = cur.children[0]; run.push(cur); }
      const tailDepth = depth + run.length - 1;
      const v = { id: n.id, name: n.name, nodeType: n.nodeType, status: n.status, effects: n.effects, condition: n.condition, postCondition: n.postCondition, frame: n.frame, children: [], _view: { kind: 'node', chainCount: 0, hiddenCount: 0, members: null, tailId: n.id, rep: n } };
      let tail = n;
      if (run.length > 2 && !unwoundHeads.has(n.id) && !searchReveal.has(n.id)) {
        let rep = run[run.length - 1];
        for (let i = 0; i < run.length; i++) if (run[i].status && run[i].status.LastStatus === 1) { rep = run[i]; break; }
        v._view = { kind: 'chain', chainCount: run.length, hiddenCount: 0, members: run.map(function(m) { return m.id; }), tailId: run[run.length - 1].id, rep: rep };
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
  function renderTree(tree) {
    if (!tree) return;
    lastRawTree = tree;
    const view = buildViewModel(tree);
    const layoutMap = buildLayoutMap(view);
    const needsFullRebuild = currentTree === null ||
      !document.getElementById('nodes-layer') ||
      currentTree.structureHash !== view.structureHash;

    if (needsFullRebuild) {
      clearSvgChildren(svg);
      const edgesG = document.createElementNS(SVG_NS, 'g');
      const nodesG = document.createElementNS(SVG_NS, 'g');
      edgesG.setAttribute('id', 'edges-layer');
      nodesG.setAttribute('id', 'nodes-layer');
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
    updateSvgView();
    drawProfileBar();
    updateStatsReadout(view);
    if (selectedNodeId) highlightNode(selectedNodeId);
    if (highlightedIds.size) highlightSearch();
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

    if (hasEffects) {
      const eb = document.createElementNS(SVG_NS, 'circle');
      eb.setAttribute('cx', x + NODE_W - 26);
      eb.setAttribute('cy', y + NODE_H - 12);
      eb.setAttribute('r', '3');
      eb.setAttribute('fill', cssVar('--success', '#46d68c'));
      eb.setAttribute('class', 'node-badge');
      group.appendChild(eb);
    }
    if (hasCondition) {
      const cb = document.createElementNS(SVG_NS, 'rect');
      cb.setAttribute('x', x + NODE_W - 38);
      cb.setAttribute('y', y + NODE_H - 15);
      cb.setAttribute('width', '6');
      cb.setAttribute('height', '6');
      cb.setAttribute('fill', cssVar('--type-pre', '#2dd4bf'));
      cb.setAttribute('class', 'node-badge');
      group.appendChild(cb);
    }

    const newGroup = group.cloneNode(true);
    group.parentNode.replaceChild(newGroup, group);

    newGroup.addEventListener('click', function(e) { e.stopPropagation(); selectNode(newNode); });
    newGroup.addEventListener('mouseenter', function(e) { showTooltip(e, newNode); });
    newGroup.addEventListener('mouseleave', hideTooltip);

    return newGroup;
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

    const group = document.createElementNS(SVG_NS, 'g');
    group.setAttribute('id', sid);
    group.setAttribute('class', isRunning ? 'node-running' : '');
    group.setAttribute('data-id', node.id);
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
      eb.setAttribute('cx', x + NODE_W - 26);
      eb.setAttribute('cy', y + NODE_H - 12);
      eb.setAttribute('r', '3');
      eb.setAttribute('fill', cssVar('--success', '#46d68c'));
      eb.setAttribute('class', 'node-badge');
      group.appendChild(eb);
    }
    if (hasCondition) {
      var cb = document.createElementNS(SVG_NS, 'rect');
      cb.setAttribute('x', x + NODE_W - 38);
      cb.setAttribute('y', y + NODE_H - 15);
      cb.setAttribute('width', '6');
      cb.setAttribute('height', '6');
      cb.setAttribute('fill', cssVar('--type-pre', '#2dd4bf'));
      cb.setAttribute('class', 'node-badge');
      group.appendChild(cb);
    }

    const vw = node._view;
    if (vw) {
      let metaTxt = '';
      if (vw.kind === 'chain') metaTxt = '\u00d7' + vw.chainCount;
      if (vw.hiddenCount > 0) metaTxt += (metaTxt ? ' ' : '') + '+' + vw.hiddenCount;
      if (metaTxt) {
        const mt = document.createElementNS(SVG_NS, 'text');
        mt.setAttribute('x', x + 12);
        mt.setAttribute('y', y + NODE_H - 8);
        mt.setAttribute('class', 'node-meta');
        mt.textContent = metaTxt;
        group.appendChild(mt);
      }
      let pillLabel = null;
      if (vw.kind === 'chain') pillLabel = '\u00d7' + vw.chainCount;
      else if (vw.hiddenCount > 0) pillLabel = '+' + vw.hiddenCount;
      else if (vw.kind === 'branch') pillLabel = '\u2212';
      if (pillLabel) {
        const pg = document.createElementNS(SVG_NS, 'g');
        pg.setAttribute('class', 'pill');
        const pc = document.createElementNS(SVG_NS, 'circle');
        pc.setAttribute('cx', x + NODE_W - 12);
        pc.setAttribute('cy', y + NODE_H - 12);
        pc.setAttribute('r', '8');
        const pt = document.createElementNS(SVG_NS, 'text');
        pt.setAttribute('x', x + NODE_W - 12);
        pt.setAttribute('y', y + NODE_H - 9);
        pt.textContent = pillLabel;
        pg.appendChild(pc);
        pg.appendChild(pt);
        pg.addEventListener('click', function(e) {
          e.stopPropagation();
          if (vw.kind === 'chain') unwoundHeads.add(node.id);
          else if (vw.hiddenCount > 0) { userExpanded.add(vw.tailId); userCollapsed.delete(vw.tailId); }
          else { userCollapsed.add(vw.tailId); userExpanded.delete(vw.tailId); }
          bumpView();
          renderTree(lastRawTree);
        });
        group.appendChild(pg);
      }
    }

    group.addEventListener('click', function(e) { e.stopPropagation(); selectNode(node); });
    group.addEventListener('mouseenter', function(e) { showTooltip(e, node); });
    group.addEventListener('mouseleave', hideTooltip);

    g.appendChild(group);
    if (node.children) {
      for (const c of node.children) drawNodes(c, g, layoutMap);
    }
  }

  function selectNode(node) {
    selectedNodeId = node.id;
    document.getElementById('detail-empty').style.display = 'none';
    document.getElementById('detail-body').style.display = 'block';

    document.getElementById('dtl-name').textContent = node.name || node.nodeType || 'Unknown';
    const typeBadge = '<span class="dtl-badge" style="background:' + escapeAttr(TYPE_COLORS[node.nodeType] || '#666') + ';color:#fff;">' + escapeHtml(node.nodeType || 'Unknown') + '</span>';
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
    fetch(window.location.pathname.replace(/\/ui$/, '/profile'))
      .then(function(r) { return r.ok ? r.json() : null; })
      .then(function(data) {
        if (!data) { wrap.style.display = 'none'; return; }
        profileCache = data;
        profileCacheIter = currentIteration;
        displayCachedProfile(nodeId, data);
      });
  }

  function showTooltip(e, node) {
    const st = node.status ? statusToString(node.status.LastStatus) : 'Unknown';
    tooltip.innerHTML = '<strong>' + escapeHtml(node.name || node.nodeType) + '</strong><br>' +
                        '<span style="color:' + escapeAttr(STATUS_COLORS[node.status ? node.status.LastStatus : 0] || '#888') + '">' + escapeHtml(st) + '</span>' +
                        (node.status ? ' x' + node.status.TickCount : '') +
                        (node._view && node._view.chainCount > 1 ? '<br>\u00d7' + node._view.chainCount + ' compressed chain' : '') +
                        (node._view && node._view.hiddenCount > 0 ? '<br>+' + node._view.hiddenCount + ' hidden nodes' : '');
    tooltip.style.display = 'block';
    positionTooltip(e);
  }
  function hideTooltip() { tooltip.style.display = 'none'; }
  function positionTooltip(e) {
    const rect = svgContainer.getBoundingClientRect();
    tooltip.style.left = (e.clientX - rect.left + 12) + 'px';
    tooltip.style.top = (e.clientY - rect.top + 12) + 'px';
  }

  function highlightNode(id) {
    svg.querySelectorAll('.node-ring').forEach(r => r.classList.remove('selected'));
    const sid = safeId(id);
    const el = document.getElementById(sid);
    if (el) {
      el.querySelector('.node-ring').classList.add('selected');
    }
  }

  function highlightSearch() {
    svg.querySelectorAll('.node-ring').forEach(r => r.classList.remove('highlight'));
    for (const id of highlightedIds) {
      const sid = safeId(id);
      const el = document.getElementById(sid);
      if (el) el.querySelector('.node-ring').classList.add('highlight');
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
  function updateSvgView() {
    const w = svgContainer.clientWidth || 800;
    const h = svgContainer.clientHeight || 600;
    svg.setAttribute('viewBox', pan.x + ' ' + pan.y + ' ' + (w / zoom) + ' ' + (h / zoom));
    zoomLevel.textContent = Math.round(zoom * 100) + '%';
    if (currentTree && countNodesJS(currentTree) > 200) { scheduleCullingRerender(); }
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
    zoom = Math.min(cw / (bbox.width + pad * 2), ch / (bbox.height + pad * 2), 2);
    pan.x = bbox.x - pad;
    pan.y = bbox.y - pad;
    updateSvgView();
  }

  function resetView() {
    zoom = 1;
    pan = { x: 0, y: 0 };
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

  let searchDebounce = null;
  searchInput.addEventListener('input', function() {
    clearTimeout(searchDebounce);
    searchDebounce = setTimeout(function() {
      const q = searchInput.value.trim().toLowerCase();
      highlightedIds.clear();
      if (!q || !currentTree) {
        if (searchReveal.size) { searchReveal.clear(); bumpView(); renderTree(lastRawTree); }
        svg.querySelectorAll('.node-ring.highlight').forEach(r => r.classList.remove('highlight'));
        return;
      }
      highlightedIds = new Set(findNodesByName(lastRawTree || currentTree, q));
      if (revealAncestors(highlightedIds)) { bumpView(); renderTree(lastRawTree); }
      highlightSearch();
      if (highlightedIds.size) {
        const firstId = Array.from(highlightedIds)[0];
        const el = document.getElementById(safeId(firstId));
        if (el) {
          const rect = el.getBBox();
          const cw = svgContainer.clientWidth || 800;
          const ch = svgContainer.clientHeight || 600;
          pan.x = rect.x - cw / (zoom * 2) + NODE_W / 2;
          pan.y = rect.y - ch / (zoom * 2) + NODE_H / 2;
          updateSvgView();
        }
      }
    }, 300);
  });

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
    updateSvgView();
  });
  window.addEventListener('mouseup', function() { isDragging = false; });
  svgContainer.addEventListener('wheel', function(e) {
    e.preventDefault();
    const zoomFactor = e.deltaY > 0 ? 0.9 : 1.1;
    zoom = Math.min(Math.max(zoom * zoomFactor, 0.1), 5);
    updateSvgView();
  }, { passive: false });
  svgContainer.addEventListener('dblclick', function(e) {
    const target = e.target.closest('[data-id]');
    if (target) {
      const id = target.getAttribute('data-id');
      const node = findNode(currentTree, id);
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
    fetch(window.location.pathname.replace(/\/ui$/, '/breakpoints'), {
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
    fetch(window.location.pathname.replace(/\/ui$/, '/dot'))
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
      const res = await fetch(window.location.pathname.replace(/\/ui$/, '/export'));
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
      const res = await fetch(window.location.pathname.replace(/\/ui$/, '/import'), {
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
      const tlRes = await fetch(window.location.pathname.replace(/\/ui$/, '/timeline'));
      if (tlRes.ok) {
        const tl = await tlRes.json();
        // Replace local cache with imported timeline: clear virtual spacers first
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
    fetch(window.location.pathname.replace(/\/ui$/, '/diff?from=' + encodeURIComponent(fromIter) + '&to=' + encodeURIComponent(toIter)))
      .then(r => r.ok ? r.json() : null)
      .then(data => {
        if (!data || !currentTree) return;
        svg.querySelectorAll('.node-card').forEach(r => r.setAttribute('opacity', '0.3'));
        if (data.added) {
          for (const dn of data.added) {
            const sid = safeId(dn.id);
            const el = document.getElementById(sid);
            if (el) {
              el.querySelector('.node-card').setAttribute('opacity', '1');
              el.querySelector('.node-card').setAttribute('fill', cssVar('--success', '#46d68c'));
            }
          }
        }
        if (data.removed) {
          for (const dn of data.removed) {
            const sid = safeId(dn.id);
            const el = document.getElementById(sid);
            if (el) {
              el.querySelector('.node-card').setAttribute('opacity', '1');
              el.querySelector('.node-card').setAttribute('fill', cssVar('--failure', '#f0566a'));
            }
          }
        }
        if (data.changed) {
          for (const dn of data.changed) {
            const sid = safeId(dn.id);
            const el = document.getElementById(sid);
            if (el) {
              el.querySelector('.node-card').setAttribute('opacity', '1');
              el.querySelector('.node-card').setAttribute('fill', cssVar('--running', '#f5a524'));
            }
          }
        }
      });
  });

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

  connect();
})();
</script>
</body>
</html>`
