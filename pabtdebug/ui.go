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
  let liveMode = true, playback = false, playbackIdx = 0, playbackTimer = null;
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

  function addTimelineEvent(event) {
    var entry = {
      iteration: event.iteration,
      status: event.status,
      durationMs: event.durationMs,
      seq: event.seq,
      nodeCount: event.nodeCount
    };
    timelineEvents.push(entry);
    if (timelineEvents.length > MAX_TIMELINE_EVENTS) {
      timelineEvents = timelineEvents.slice(timelineEvents.length - MAX_TIMELINE_EVENTS);
      if (timelineList.firstChild) timelineList.removeChild(timelineList.firstChild);
    }
    currentIteration = event.iteration;
    appendTimelineRow(entry);
    if (profileCacheIter !== currentIteration) { profileCache = null; }
    if (liveMode) {
      const rows = timelineList.querySelectorAll('.tl-row');
      if (rows.length) rows[rows.length - 1].scrollIntoView({ block: 'end' });
    }
  }

  function appendTimelineRow(ev) {
    const row = document.createElement('div');
    row.className = 'tl-row' + (ev.iteration === currentIteration ? ' selected' : '');
    row.dataset.iter = ev.iteration;
    const st = statusToString(ev.status);
    const sc = STATUS_COLORS[ev.status] || '#888';
    row.innerHTML = '<span class="tl-dot" style="background:' + escapeAttr(sc) + '"></span>' +
                    '<span class="tl-iter">#' + escapeHtml(ev.iteration) + '</span>' +
                    '<span class="tl-status" style="color:' + escapeAttr(sc) + '">' + escapeHtml(st) + '</span>' +
                    '<span class="tl-duration">' + (ev.durationMs ? ev.durationMs.toFixed(1) + 'ms' : '') + '</span>';
    row.addEventListener('click', function() {
      currentIteration = ev.iteration;
      updateTimelineSelection();
      fetchTimelineIter(ev.iteration);
    });
    timelineList.appendChild(row);
  }

  function updateTimelineSelection() {
    const rows = timelineList.querySelectorAll('.tl-row');
    rows.forEach(function(r) { r.classList.toggle('selected', r.dataset.iter == currentIteration); });
  }

  function renderTimeline() {
    timelineList.innerHTML = '';
    for (const ev of timelineEvents) {
      const row = document.createElement('div');
      row.className = 'tl-row' + (ev.iteration === currentIteration ? ' selected' : '');
      row.dataset.iter = ev.iteration;
      const st = statusToString(ev.status);
      const sc = STATUS_COLORS[ev.status] || '#888';
      row.innerHTML = '<span class="tl-dot" style="background:' + escapeAttr(sc) + '"></span>' +
                      '<span class="tl-iter">#' + escapeHtml(ev.iteration) + '</span>' +
                      '<span class="tl-status" style="color:' + escapeAttr(sc) + '">' + escapeHtml(st) + '</span>' +
                      '<span class="tl-duration">' + (ev.durationMs ? ev.durationMs.toFixed(1) + 'ms' : '') + '</span>';
      row.addEventListener('click', function() {
        currentIteration = ev.iteration;
        updateTimelineSelection();
        fetchTimelineIter(ev.iteration);
      });
      timelineList.appendChild(row);
    }
  }

  function fetchTimelineIter(iter) {
    fetch(window.location.pathname.replace(/\/ui$/, '/timeline/' + iter))
      .then(function(r) {
        if (r.status === 404) {
          var nodesLayer = svg.getElementById('nodes-layer');
          var edgesLayer = svg.getElementById('edges-layer');
          if (nodesLayer) nodesLayer.innerHTML = '';
          if (edgesLayer) edgesLayer.innerHTML = '';
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

  function computeLayout(node, level) {
    if (!node) return { width: 1, x: 0 };
    level = level || 0;
    node._level = level;
    if (!node.children || node.children.length === 0) {
      node._width = 1;
      node._x = 0;
      return { width: 1, x: 0 };
    }
    let totalW = 0;
    for (const c of node.children) {
      const r = computeLayout(c, level + 1);
      totalW += r.width;
    }
    node._width = totalW;
    let offset = -totalW / 2;
    for (const c of node.children) {
      c._x = offset + c._width / 2;
      offset += c._width;
    }
    return { width: totalW, x: 0 };
  }

  function assignPositions(node, parentX) {
    node._absX = (parentX + node._x) * UNIT;
    node._absY = node._level * LEVEL_H + 40;
    if (node.children) {
      for (const c of node.children) assignPositions(c, parentX + node._x);
    }
  }

  function renderTree(tree) {
    if (!tree) return;

    const needsFullRebuild = currentTree === null ||
      !svg.getElementById('nodes-layer') ||
      currentTree.structureHash !== tree.structureHash;

    computeLayout(tree, 0);
    assignPositions(tree, 0);

    if (needsFullRebuild) {
      currentTree = tree;
      svg.innerHTML = '';

      const edgesG = document.createElementNS(SVG_NS, 'g');
      const nodesG = document.createElementNS(SVG_NS, 'g');
      edgesG.setAttribute('id', 'edges-layer');
      nodesG.setAttribute('id', 'nodes-layer');

      drawEdges(tree, edgesG);
      drawNodes(tree, nodesG);

      svg.appendChild(edgesG);
      svg.appendChild(nodesG);
    } else {
      const nodesG = svg.getElementById('nodes-layer');

      const edgesG = svg.getElementById('edges-layer');
      edgesG.innerHTML = '';
      drawEdges(tree, edgesG);

      diffAndUpdateNodes(tree, currentTree, nodesG);
      currentTree = tree;
    }

    updateSvgView();
    drawProfileBar();
    if (selectedNodeId) highlightNode(selectedNodeId);
    if (highlightedIds.size) highlightSearch();
  }

  function updateNodeGroup(group, newNode) {
    const x = newNode._absX - NODE_W / 2;
    const y = newNode._absY;
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

  function diffAndUpdateNodes(newNode, oldNode, container) {
    if (!newNode) return;

    const sid = safeId(newNode.id);
    let existingGroup = document.getElementById(sid);

    if (existingGroup && oldNode && oldNode.id === newNode.id) {
      updateNodeGroup(existingGroup, newNode);

      if (newNode.children) {
        for (let i = 0; i < newNode.children.length; i++) {
          const oldChild = (oldNode.children && i < oldNode.children.length) ? oldNode.children[i] : null;
          diffAndUpdateNodes(newNode.children[i], oldChild, container);
        }
        if (oldNode.children) {
          for (let i = newNode.children.length; i < oldNode.children.length; i++) {
            const oldChildId = safeId(oldNode.children[i].id);
            const oldChildGroup = document.getElementById(oldChildId);
            if (oldChildGroup) oldChildGroup.remove();
          }
        }
      } else if (oldNode.children) {
        for (let i = 0; i < oldNode.children.length; i++) {
          const oldChildId = safeId(oldNode.children[i].id);
          const oldChildGroup = document.getElementById(oldChildId);
          if (oldChildGroup) oldChildGroup.remove();
        }
      }
    } else {
      if (existingGroup) existingGroup.remove();
      drawNodes(newNode, container);
    }
  }

  function drawEdges(node, g) {
    if (!node.children) return;
    const px = node._absX;
    const py = node._absY + NODE_H;
    for (const c of node.children) {
      const cx = c._absX;
      const cy = c._absY;
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
      drawEdges(c, g);
    }
  }

  function drawNodes(node, g) {
    const x = node._absX - NODE_W / 2;
    const y = node._absY;
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
      for (const c of node.children) drawNodes(c, g);
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

  function updateSvgView() {
    const w = svgContainer.clientWidth || 800;
    const h = svgContainer.clientHeight || 600;
    svg.setAttribute('viewBox', pan.x + ' ' + pan.y + ' ' + (w / zoom) + ' ' + (h / zoom));
    zoomLevel.textContent = Math.round(zoom * 100) + '%';
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

  btnPlay.addEventListener('click', function() {
    if (playback) {
      playback = false;
      clearInterval(playbackTimer);
      btnPlay.textContent = '\u25B6';
      return;
    }
    if (!timelineEvents.length) return;
    liveMode = false;
    btnLive.classList.remove('active');
    playback = true;
    playbackIdx = 0;
    btnPlay.textContent = '\u25A0';
    const speed = parseFloat(document.getElementById('speed-select').value);
    playbackTimer = setInterval(function() {
      if (playbackIdx >= timelineEvents.length) {
        playback = false;
        clearInterval(playbackTimer);
        btnPlay.textContent = '\u25B6';
        return;
      }
      const ev = timelineEvents[playbackIdx];
      currentIteration = ev.iteration;
      renderTimeline();
      fetchTimelineIter(ev.iteration);
      playbackIdx++;
    }, 1000 / speed);
  });

  document.getElementById('btn-export').addEventListener('click', function() {
    fetch(window.location.pathname.replace(/\/ui$/, '/dot'))
      .then(r => r.ok ? r.text() : '')
      .then(text => {
        if (!text) return;
        const blob = new Blob([text], { type: 'text/plain' });
        const a = document.createElement('a');
        a.href = URL.createObjectURL(blob);
        a.download = 'debug.dot';
        a.click();
        URL.revokeObjectURL(a.href);
      });
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
