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
 --bg-main: #0f0f23;
 --bg-panel: #1a1a2e;
 --bg-card: #16213e;
 --text-primary: #e0e0e0;
 --text-secondary: #888;
 --text-oncolor: #fff;
 --accent: #00d4ff;
 --success: #4caf50;
 --failure: #f44336;
 --running: #ff9800;
 --border: #333;
 --divider: #222;
}
body {
 font-family: 'Menlo', 'Monaco', 'Courier New', monospace;
 background: var(--bg-main);
 color: var(--text-primary);
 overflow: hidden;
 height: 100vh;
 width: 100vw;
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
 background: #f44336;
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
 background: var(--bg-main);
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
.dtl-badge-effects { background: rgba(76,175,80,0.2); color: #4caf50; border: 1px solid #4caf50; }
.dtl-badge-cond { background: rgba(0,188,212,0.2); color: #00bcd4; border: 1px solid #00bcd4; }
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
 padding: 6px 8px;
 font-size: 10px;
 pointer-events: none;
 z-index: 1000;
 max-width: 200px;
 box-shadow: 0 4px 12px rgba(0,0,0,0.5);
 display: none;
}
@keyframes nodePulse {
 0% { filter: drop-shadow(0 0 2px currentColor); }
 50% { filter: drop-shadow(0 0 8px currentColor); }
 100% { filter: drop-shadow(0 0 2px currentColor); }
}
@keyframes statusFlashGreen {
 0% { fill: #4caf50; }
 50% { fill: #81c784; }
 100% { fill: #4caf50; }
}
@keyframes statusFlashRed {
 0% { transform: translateX(0); }
 20% { transform: translateX(-2px); }
 40% { transform: translateX(2px); }
 60% { transform: translateX(-1px); }
 80% { transform: translateX(1px); }
 100% { transform: translateX(0); }
}
.node-running .node-rect {
 animation: nodePulse 1.5s ease-in-out infinite;
}
.node-rect {
 rx: 8;
 ry: 8;
 cursor: pointer;
 transition: filter 0.2s;
}
.node-rect:hover { filter: brightness(1.2); }
.node-text {
 pointer-events: none;
 font-family: 'Menlo', 'Monaco', 'Courier New', monospace;
}
.node-stripe {
 rx: 8;
 ry: 8;
}
.node-badge {
 cursor: default;
}
.edge-path {
 fill: none;
 stroke: #555;
 stroke-width: 1.5;
 transition: stroke 0.3s;
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
 stroke-width: 3;
 rx: 8;
 ry: 8;
 transition: stroke 0.2s;
 pointer-events: none;
}
.node-ring.selected { stroke: #fff; }
.node-ring.highlight { stroke: #ffeb3b; }
</style>
</head>
<body>
<div id="app">
  <header id="header">
    <div id="header-left">
      <h1>PA-BT Debug</h1>
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
      <span id="zoom-level" style="font-size:10px;color:var(--text-secondary);margin-left:auto;">100%</span>
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
      <span class="leg-dot" style="background:#2196f3"></span>Goal
      <span class="leg-dot" style="background:#9c27b0"></span>PPA
      <span class="leg-dot" style="background:#ff9800"></span>Action
      <span class="leg-dot" style="background:#4caf50"></span>ActionNode
      <span class="leg-dot" style="background:#00bcd4"></span>Pre
      <span class="leg-dot" style="background:#666"></span>Unknown
    </div>
    <div id="status-bar">
      <span class="status-dot" style="background:var(--success)"></span>S
      <span class="status-dot" style="background:var(--failure)"></span>F
      <span class="status-dot" style="background:var(--running)"></span>R
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
  const NODE_W = 150;
  const NODE_H = 48;
  const LEVEL_H = 90;
  const GAP = 20;
  const UNIT = NODE_W + GAP;

  const TYPE_COLORS = {
    GoalRoot: '#2196f3', GoalSelector: '#2196f3',
    PPARoot: '#9c27b0', PPAPost: '#9c27b0',
    ActionSelector: '#ff9800', ActionRoot: '#ff9800',
    ActionNode: '#4caf50',
    PreconditionsRoot: '#00bcd4', PreconditionLeaf: '#00bcd4',
    Unknown: '#666'
  };
  const STATUS_COLORS = { 1: '#4caf50', 2: '#f44336', 3: '#ff9800' };

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
  function statusToString(s) { switch(s){ case 1: return 'Success'; case 2: return 'Failure'; case 3: return 'Running'; default: return 'Unknown'; } }
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
    const url = window.location.pathname.replace(/\/ui$/, '/plans/0/events');
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
        connDot.style.background = '#ff9800';
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
    row.innerHTML = '<span class="tl-dot" style="background:' + escapeAttr(sc) + '"></span>' +
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
          var nodesLayer = svg.getElementById('nodes-layer');
          var edgesLayer = svg.getElementById('edges-layer');
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
      .then(function(data) { if (data && data.tree) renderTree(data.tree); });
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
    function assign(node, parentX) {
      if (!node) return;
      const e = map[node.id];
      e.absX = (parentX + e.x) * UNIT;
      e.absY = e.level * LEVEL_H + 40;
      e.bbox = { x: e.absX - NODE_W/2, y: e.absY, w: NODE_W, h: NODE_H };
      if (node.children) {
        for (let i = 0; i < node.children.length; i++) assign(node.children[i], parentX + e.x);
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
  function clearSvgChildren(el) { while (el.firstChild) el.removeChild(el.firstChild); }
  function renderTree(tree) {
    if (!tree) return;
    const layoutMap = buildLayoutMap(tree);
    const needsFullRebuild = currentTree === null ||
      !svg.getElementById('nodes-layer') ||
      currentTree.structureHash !== tree.structureHash;

    if (needsFullRebuild) {
      // No innerHTML wipe: remove via DOM API and rebuild.
      clearSvgChildren(svg);
      const edgesG = document.createElementNS(SVG_NS, 'g');
      const nodesG = document.createElementNS(SVG_NS, 'g');
      edgesG.setAttribute('id', 'edges-layer');
      nodesG.setAttribute('id', 'nodes-layer');
      drawEdges(tree, edgesG, layoutMap);
      drawNodes(tree, nodesG, layoutMap);
      svg.appendChild(edgesG);
      svg.appendChild(nodesG);
      currentTree = tree;
      currentLayoutMap = layoutMap;
    } else {
      const nodesG = svg.getElementById('nodes-layer');
      const edgesG = svg.getElementById('edges-layer');
      clearSvgChildren(edgesG);
      drawEdges(tree, edgesG, layoutMap);
      diffAndUpdateNodes(tree, currentTree, nodesG, layoutMap, currentLayoutMap);
      currentTree = tree;
      currentLayoutMap = layoutMap;
    }
    updateSvgView();
    drawProfileBar();
    if (selectedNodeId) highlightNode(selectedNodeId);
    if (highlightedIds.size) highlightSearch();
  }

  function updateNodeGroup(group, newNode, layoutMap) {
    const lm = layoutMap ? layoutMap[newNode.id] : null;
    const absX = lm ? lm.absX : newNode._absX;
    const absY = lm ? lm.absY : newNode._absY;
    const x = absX - NODE_W / 2;
    const y = absY;
    const typeColor = TYPE_COLORS[newNode.nodeType] || TYPE_COLORS.Unknown;
    const status = newNode.status ? newNode.status.LastStatus : 0;
    const statusColor = STATUS_COLORS[status] || '#555';
    const isRunning = status === 3;
    const hasEffects = newNode.effects && newNode.effects.length > 0;
    const hasCondition = !!newNode.condition || !!newNode.postCondition;

    group.setAttribute('class', isRunning ? 'node-running' : '');

    const ring = group.querySelector('.node-ring');
    if (ring) {
      ring.setAttribute('x', x - 3);
      ring.setAttribute('y', y - 3);
    }

    const rect = group.querySelector('.node-rect');
    if (rect) {
      rect.setAttribute('x', x);
      rect.setAttribute('y', y);
      rect.setAttribute('fill', typeColor);
      if (!isRunning && status === 0) rect.setAttribute('opacity', '0.5');
      else rect.setAttribute('opacity', '1');
    }

    const stripe = group.querySelector('.node-stripe');
    if (stripe) {
      stripe.setAttribute('x', x);
      stripe.setAttribute('y', y);
      stripe.setAttribute('fill', statusColor);
    }

    const typeText = group.querySelectorAll('.node-text')[0];
    if (typeText) {
      typeText.setAttribute('x', x + 10);
      typeText.setAttribute('y', y + 18);
      typeText.textContent = newNode.nodeType || 'Unknown';
    }

    const nameText = group.querySelectorAll('.node-text')[1];
    if (nameText) {
      nameText.setAttribute('x', x + 10);
      nameText.setAttribute('y', y + 34);
      nameText.textContent = newNode.name || '';
    }

    const existingBadges = group.querySelectorAll('.node-badge');
    for (let i = 0; i < existingBadges.length; i++) {
      existingBadges[i].remove();
    }

    if (hasEffects) {
      const badge = document.createElementNS(SVG_NS, 'circle');
      badge.setAttribute('cx', x + NODE_W - 8);
      badge.setAttribute('cy', y + 10);
      badge.setAttribute('r', '5');
      badge.setAttribute('fill', '#4caf50');
      badge.setAttribute('class', 'node-badge');
      group.appendChild(badge);
    }
    if (hasCondition) {
      const badge = document.createElementNS(SVG_NS, 'circle');
      badge.setAttribute('cx', x + NODE_W - 8);
      badge.setAttribute('cy', y + 22);
      badge.setAttribute('r', '5');
      badge.setAttribute('fill', '#00bcd4');
      badge.setAttribute('class', 'node-badge');
      group.appendChild(badge);
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
    const px = lm ? lm.absX : node._absX;
    const py = (lm ? lm.absY : node._absY) + NODE_H;
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
      const d = 'M ' + px + ' ' + py +
                ' C ' + px + ' ' + (py + LEVEL_H/2) +
                ', ' + cx + ' ' + (cy - LEVEL_H/2) +
                ', ' + cx + ' ' + cy;
      const path = document.createElementNS(SVG_NS, 'path');
      path.setAttribute('d', d);
      path.setAttribute('class', 'edge-path');
      const status = c.status ? c.status.LastStatus : 0;
      if (status === 3) path.classList.add('active');
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
    const x = absX - NODE_W / 2;
    const y = absY;
    const typeColor = TYPE_COLORS[node.nodeType] || TYPE_COLORS.Unknown;
    const status = node.status ? node.status.LastStatus : 0;
    const statusColor = STATUS_COLORS[status] || '#555';
    const sid = safeId(node.id);
    const hasEffects = node.effects && node.effects.length > 0;
    const hasCondition = !!node.condition || !!node.postCondition;
    const isRunning = status === 3;

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

    const rect = document.createElementNS(SVG_NS, 'rect');
    rect.setAttribute('x', x);
    rect.setAttribute('y', y);
    rect.setAttribute('width', NODE_W);
    rect.setAttribute('height', NODE_H);
    rect.setAttribute('fill', typeColor);
    rect.setAttribute('class', 'node-rect');
    rect.setAttribute('rx', '8');
    rect.setAttribute('ry', '8');
    if (!isRunning && status === 0) rect.setAttribute('opacity', '0.5');
    group.appendChild(rect);

    const stripe = document.createElementNS(SVG_NS, 'rect');
    stripe.setAttribute('x', x);
    stripe.setAttribute('y', y);
    stripe.setAttribute('width', '4');
    stripe.setAttribute('height', NODE_H);
    stripe.setAttribute('fill', statusColor);
    stripe.setAttribute('class', 'node-stripe');
    stripe.setAttribute('rx', '8');
    stripe.setAttribute('ry', '8');
    group.appendChild(stripe);

    const typeText = document.createElementNS(SVG_NS, 'text');
    typeText.setAttribute('x', x + 10);
    typeText.setAttribute('y', y + 18);
    typeText.setAttribute('class', 'node-text');
    typeText.setAttribute('fill', '#fff');
    typeText.setAttribute('font-size', '11');
    typeText.setAttribute('font-weight', 'bold');
    typeText.textContent = node.nodeType || 'Unknown';
    group.appendChild(typeText);

    const nameText = document.createElementNS(SVG_NS, 'text');
    nameText.setAttribute('x', x + 10);
    nameText.setAttribute('y', y + 34);
    nameText.setAttribute('class', 'node-text');
    nameText.setAttribute('fill', '#ccc');
    nameText.setAttribute('font-size', '10');
    nameText.textContent = node.name || '';
    group.appendChild(nameText);

    if (hasEffects) {
      const badge = document.createElementNS(SVG_NS, 'circle');
      badge.setAttribute('cx', x + NODE_W - 8);
      badge.setAttribute('cy', y + 10);
      badge.setAttribute('r', '5');
      badge.setAttribute('fill', '#4caf50');
      badge.setAttribute('class', 'node-badge');
      group.appendChild(badge);
    }
    if (hasCondition) {
      const badge = document.createElementNS(SVG_NS, 'circle');
      badge.setAttribute('cx', x + NODE_W - 8);
      badge.setAttribute('cy', y + 22);
      badge.setAttribute('r', '5');
      badge.setAttribute('fill', '#00bcd4');
      badge.setAttribute('class', 'node-badge');
      group.appendChild(badge);
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
    const ring = document.getElementById(sid).querySelector('.node-ring');
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
      makeBar('Ticks', prof.tickCount, '#888') +
      makeBar('Success', prof.successCount || 0, '#4caf50') +
      makeBar('Failure', prof.failureCount || 0, '#f44336') +
      makeBar('Running', prof.runningCount || 0, '#ff9800');
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
                        (node.status ? ' x' + node.status.TickCount : '');
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
  function scheduleCullingRerender() {
    if (viewCullingRaf) return;
    viewCullingRaf = requestAnimationFrame(function(){ viewCullingRaf=null; if(currentTree) renderTree(currentTree); });
  }
  function updateSvgView() {
    const w = svgContainer.clientWidth || 800;
    const h = svgContainer.clientHeight || 600;
    svg.setAttribute('viewBox', pan.x + ' ' + pan.y + ' ' + (w / zoom) + ' ' + (h / zoom));
    zoomLevel.textContent = Math.round(zoom * 100) + '%';
    // Viewport changed: re-evaluate culling if we have many nodes (>200) to avoid missing nodes when panning into previously culled area
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
    const bbox = svg.getBBox();
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
    const flat = flattenTree(currentTree);
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
      let color = '#666';
      if (st === 1) color = '#4caf50';
      else if (st === 2) color = '#f44336';
      else if (st === 3) color = '#ff9800';
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
        svg.querySelectorAll('.node-ring.highlight').forEach(r => r.classList.remove('highlight'));
        return;
      }
      highlightedIds = new Set(findNodesByName(currentTree, q));
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
      body: JSON.stringify({ path: selectedNodeId })
    }).then(r => {
      if (r.ok) btn.textContent = 'Breakpoint Set';
      else btn.textContent = 'Error';
      setTimeout(() => btn.textContent = 'Toggle Breakpoint', 1500);
    });
  });

  document.getElementById('btn-fit').addEventListener('click', fitToTree);
  document.getElementById('btn-reset').addEventListener('click', resetView);

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
        timelineEvents = tl.map(function(e){ return { iteration: e.iteration, status: (e.status==='Success'?1:(e.status==='Failure'?2:(e.status==='Running'?3:0))), durationMs: e.durationMs||0, seq: 0, nodeCount: e.nodeCount||0 }; });
        // Status strings are "Success"/"Failure"/"Running"; fallback already handled; but server may return string
        // Re-normalize status strings if numeric not matched
        timelineEvents = timelineEvents.map(function(entry, idx){
          const orig = tl[idx];
          let s = 0;
          if (orig.status === 'Success' || orig.status === 1) s = 1;
          else if (orig.status === 'Failure' || orig.status === 2) s = 2;
          else if (orig.status === 'Running' || orig.status === 3) s = 3;
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
        svg.querySelectorAll('.node-rect').forEach(r => r.setAttribute('opacity', '0.3'));
        if (data.added) {
          for (const dn of data.added) {
            const sid = safeId(dn.id);
            const el = document.getElementById(sid);
            if (el) {
              el.querySelector('.node-rect').setAttribute('opacity', '1');
              el.querySelector('.node-rect').setAttribute('fill', '#4caf50');
            }
          }
        }
        if (data.removed) {
          for (const dn of data.removed) {
            const sid = safeId(dn.id);
            const el = document.getElementById(sid);
            if (el) {
              el.querySelector('.node-rect').setAttribute('opacity', '1');
              el.querySelector('.node-rect').setAttribute('fill', '#f44336');
            }
          }
        }
        if (data.changed) {
          for (const dn of data.changed) {
            const sid = safeId(dn.id);
            const el = document.getElementById(sid);
            if (el) {
              el.querySelector('.node-rect').setAttribute('opacity', '1');
              el.querySelector('.node-rect').setAttribute('fill', '#ff9800');
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
