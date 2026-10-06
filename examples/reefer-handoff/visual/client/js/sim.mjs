// Reefer Handoff — Yard Simulation
//
// V2: the page shell. Renders the terminal yard as SVG — seven nodes as
// rounded rectangles with labels and capacity indicators, seven lanes as
// curved paths — and keeps the title bar current by polling /sim/state
// every 200ms. The layout is this file's; the facts are the server's.

// ── Layout ──────────────────────────────────────────────────────────────
// Node centres in SVG user units. STAGING left, WEST upper-middle, EAST
// lower-middle, BAY centre-right, SLOT-1/2/3 in a right column: a
// left-to-right flow matching the operational narrative.
const NODE_POS = {
  'STAGING': { x: 80, y: 220 },
  'WEST': { x: 300, y: 120 },
  'EAST': { x: 300, y: 320 },
  'BAY': { x: 520, y: 220 },
  'SLOT-1': { x: 720, y: 80 },
  'SLOT-2': { x: 720, y: 220 },
  'SLOT-3': { x: 720, y: 360 },
};

const NODE_W = 90;
const NODE_H = 44;

const SVG_NS = 'http://www.w3.org/2000/svg';

// ── DOM ─────────────────────────────────────────────────────────────────
const lanesLayer = document.getElementById('lanes-layer');
const nodesLayer = document.getElementById('nodes-layer');
const roundEl = document.getElementById('status-round');
const cutoffEl = document.getElementById('status-cutoff');
const phaseEl = document.getElementById('status-phase');
const connEl = document.getElementById('connection-status');

function svgEl(tag, attrs) {
  const el = document.createElementNS(SVG_NS, tag);
  for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, v);
  return el;
}

// lanePath draws a gentle curve between two node centres.
function lanePath(a, b) {
  const pa = NODE_POS[a];
  const pb = NODE_POS[b];
  if (!pa || !pb) return '';
  const mx = (pa.x + pb.x) / 2;
  const my = (pa.y + pb.y) / 2;
  const dx = pb.x - pa.x;
  const dy = pb.y - pa.y;
  const len = Math.hypot(dx, dy) || 1;
  const off = Math.min(24, len * 0.12);
  const cx = mx - (dy / len) * off;
  const cy = my + (dx / len) * off;
  return `M ${pa.x} ${pa.y} Q ${cx} ${cy} ${pb.x} ${pb.y}`;
}

// ── Topology (built once, from the first state response) ───────────────
let topologyBuilt = false;

function buildTopology(state) {
  // Lanes first so nodes draw over the path endpoints.
  for (const lane of state.lanes) {
    lanesLayer.appendChild(svgEl('path', {
      d: lanePath(lane.a, lane.b),
      class: 'lane-path',
      'data-lane': lane.id,
    }));
  }
  for (const node of state.nodes) {
    const pos = NODE_POS[node.id];
    if (!pos) continue;
    const g = svgEl('g', { 'data-node': node.id });
    g.appendChild(svgEl('rect', {
      x: pos.x - NODE_W / 2,
      y: pos.y - NODE_H / 2,
      width: NODE_W,
      height: NODE_H,
      class: 'node-rect',
    }));
    const label = svgEl('text', { x: pos.x, y: pos.y - 5, class: 'node-label' });
    label.textContent = node.id;
    g.appendChild(label);
    const cap = svgEl('text', { x: pos.x, y: pos.y + 11, class: 'node-capacity' });
    cap.textContent = node.capacity === 0 ? 'capacity ∞' : `capacity ${node.capacity}`;
    g.appendChild(cap);
    nodesLayer.appendChild(g);
  }
  topologyBuilt = true;
}

// ── Render ──────────────────────────────────────────────────────────────
function render(state) {
  if (!topologyBuilt) buildTopology(state);

  roundEl.textContent = `Round ${state.round}`;
  cutoffEl.textContent = `Cutoff ${state.cutoff}`;
  phaseEl.textContent = `Phase ${state.phase}`;
  phaseEl.className = `status-item phase-${String(state.phase).toLowerCase()}`;

  for (const lane of state.lanes) {
    const path = lanesLayer.querySelector(`[data-lane="${lane.id}"]`);
    if (path) path.classList.toggle('lane-locked', lane.locked);
  }
}

// ── Polling ─────────────────────────────────────────────────────────────
async function pollState() {
  try {
    const resp = await fetch('/sim/state', { cache: 'no-store' });
    if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
    render(await resp.json());
    connEl.textContent = 'Connected';
    connEl.className = 'footer-item connected';
  } catch {
    connEl.textContent = 'Disconnected';
    connEl.className = 'footer-item disconnected';
  }
}

pollState();
setInterval(pollState, 200);
