// Reefer Handoff — Yard Simulation
//
// V2: the page shell. Renders the terminal yard as SVG — seven nodes as
// rounded rectangles with labels and capacity indicators, seven lanes as
// curved paths — and keeps the title bar current by polling /sim/state
// every 200ms. The layout is this file's; the facts are the server's.
//
// V3: live entity rendering. Tractors glide between nodes with CSS
// transitions on transform. Reefers attach to their custodian tractor or
// settle into slots. Slots glow green when powered, dim when not. A legend
// identifies the colour coding.

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

// Tractor dimensions.
const TRACTOR_W = 40;
const TRACTOR_H = 24;

// Reefer dimensions.
const REEFER_W = 20;
const REEFER_H = 20;

// Tractor colours per the blueprint.
const TRACTOR_COLOR = {
  'TUG-1': '#58a6ff',
  'TUG-2': '#3fb950',
  'TUG-3': '#d29922',
};

// Reefer colour by temperature: deep freeze vs chilled.
const DEEP_FREEZE_TEMP = -15;

const SVG_NS = 'http://www.w3.org/2000/svg';

// ── DOM ─────────────────────────────────────────────────────────────────
const lanesLayer = document.getElementById('lanes-layer');
const nodesLayer = document.getElementById('nodes-layer');
const entitiesLayer = document.getElementById('entities-layer');
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

// ── Entities (built once, updated every poll) ──────────────────────────
let entitiesBuilt = false;
const tractorEls = new Map();   // id -> SVG group
const reeferEls = new Map();    // id -> SVG group
const slotGlowEls = new Map();  // slot node id -> glow rect

function buildEntities(state) {
  // Tractors.
  for (const t of state.tractors) {
    const g = svgEl('g', {
      class: 'tractor',
      'data-tractor': t.id,
    });
    g.appendChild(svgEl('rect', {
      x: -TRACTOR_W / 2,
      y: -TRACTOR_H / 2,
      width: TRACTOR_W,
      height: TRACTOR_H,
      rx: 5,
      class: 'tractor-body',
      fill: TRACTOR_COLOR[t.id] || '#8b949e',
    }));
    const label = svgEl('text', {
      x: 0,
      y: 1,
      class: 'tractor-label',
    });
    label.textContent = t.id.replace('TUG-', 'T');
    g.appendChild(label);
    entitiesLayer.appendChild(g);
    tractorEls.set(t.id, g);
  }

  // Reefers.
  for (const r of state.reefers) {
    const g = svgEl('g', {
      class: 'reefer',
      'data-reefer': r.id,
    });
    g.appendChild(svgEl('rect', {
      x: -REEFER_W / 2,
      y: -REEFER_H / 2,
      width: REEFER_W,
      height: REEFER_H,
      rx: 3,
      class: 'reefer-body',
    }));
    const badge = svgEl('text', {
      x: 0,
      y: REEFER_H / 2 + 10,
      class: 'reefer-badge',
    });
    badge.textContent = `${r.tempC}°C`;
    g.appendChild(badge);
    entitiesLayer.appendChild(g);
    reeferEls.set(r.id, g);
  }

  // Slot glow indicators (rendered behind the node rect, as a slightly
  // larger rounded rect with a glow class).
  for (const s of state.slots) {
    const pos = NODE_POS[s.node];
    if (!pos) continue;
    const glow = svgEl('rect', {
      x: pos.x - NODE_W / 2 - 3,
      y: pos.y - NODE_H / 2 - 3,
      width: NODE_W + 6,
      height: NODE_H + 6,
      rx: 10,
      class: 'slot-glow',
      'data-slot': s.id,
    });
    // Insert before the node so it appears behind.
    const nodeG = nodesLayer.querySelector(`[data-node="${s.node}"]`);
    if (nodeG) {
      nodesLayer.insertBefore(glow, nodeG);
    } else {
      nodesLayer.appendChild(glow);
    }
    slotGlowEls.set(s.node, glow);
  }

  // Legend.
  buildLegend();

  entitiesBuilt = true;
}

function buildLegend() {
  const legend = svgEl('g', { class: 'legend', transform: 'translate(10, 400)' });
  const items = [
    { color: '#58a6ff', label: 'TUG-1' },
    { color: '#3fb950', label: 'TUG-2' },
    { color: '#d29922', label: 'TUG-3' },
    { color: '#79c0ff', label: 'Deep freeze' },
    { color: '#ffa657', label: 'Chilled' },
  ];
  let x = 0;
  for (const item of items) {
    legend.appendChild(svgEl('rect', {
      x, y: -6, width: 10, height: 10, rx: 2, fill: item.color,
    }));
    const t = svgEl('text', { x: x + 14, y: 0, class: 'legend-label' });
    t.textContent = item.label;
    legend.appendChild(t);
    x += 14 + item.label.length * 6 + 16;
  }
  entitiesLayer.appendChild(legend);
}

// ── Entity positioning ─────────────────────────────────────────────────
// Tractor position: at its node, offset slightly so multiple tractors at
// STAGING don't overlap completely.
function tractorPos(t, index) {
  const pos = NODE_POS[t.at];
  if (!pos) return { x: 0, y: 0 };
  // Offset each tractor at the same node vertically.
  const offsetY = (index - 1) * 14; // spread around centre
  return { x: pos.x, y: pos.y + offsetY };
}

// Reefer position: attached to custodian tractor, or at its own node.
function reeferPos(r, tractorPositions) {
  if (r.custodian && tractorPositions.has(r.custodian)) {
    const tp = tractorPositions.get(r.custodian);
    return { x: tp.x, y: tp.y - TRACTOR_H / 2 - REEFER_H / 2 - 2 };
  }
  const pos = NODE_POS[r.at];
  if (!pos) return { x: 0, y: 0 };
  // If at a slot node, position inside the slot.
  return { x: pos.x, y: pos.y };
}

// ── Render ──────────────────────────────────────────────────────────────
let prevState = null;

function render(state) {
  if (!topologyBuilt) buildTopology(state);
  if (!entitiesBuilt) buildEntities(state);

  roundEl.textContent = `Round ${state.round}`;
  cutoffEl.textContent = `Cutoff ${state.cutoff}`;
  phaseEl.textContent = `Phase ${state.phase}`;
  phaseEl.className = `status-item phase-${String(state.phase).toLowerCase()}`;

  // Lane lock states.
  for (const lane of state.lanes) {
    const path = lanesLayer.querySelector(`[data-lane="${lane.id}"]`);
    if (path) path.classList.toggle('lane-locked', lane.locked);
  }

  // Tractor positions — animate via CSS transition on transform.
  const tractorPositions = new Map();
  const stagingIndex = new Map(); // node -> count for offset
  for (const t of state.tractors) {
    const count = stagingIndex.get(t.at) || 0;
    stagingIndex.set(t.at, count + 1);
  }
  const nodeCounters = new Map();
  for (const t of state.tractors) {
    const idx = nodeCounters.get(t.at) || 0;
    nodeCounters.set(t.at, idx + 1);
    const total = stagingIndex.get(t.at) || 1;
    const pos = tractorPos(t, idx - (total - 1) / 2);
    tractorPositions.set(t.id, pos);
    const el = tractorEls.get(t.id);
    if (el) {
      el.style.transform = `translate(${pos.x}px, ${pos.y}px)`;
    }
  }

  // Reefer positions.
  for (const r of state.reefers) {
    const pos = reeferPos(r, tractorPositions);
    const el = reeferEls.get(r.id);
    if (el) {
      el.style.transform = `translate(${pos.x}px, ${pos.y}px)`;
      // Colour by temperature.
      const body = el.querySelector('.reefer-body');
      if (body) {
        body.setAttribute('fill', r.tempC <= DEEP_FREEZE_TEMP ? '#79c0ff' : '#ffa657');
      }
    }
  }

  // Slot glow states.
  for (const s of state.slots) {
    const glow = slotGlowEls.get(s.node);
    if (glow) {
      glow.classList.toggle('slot-powered', s.plugged);
      glow.classList.toggle('slot-unpowered', !s.plugged);
    }
  }

  // Bay occupant indicator.
  const bayNode = nodesLayer.querySelector('[data-node="BAY"]');
  if (bayNode) {
    const occupant = state.tractors.find(t => t.at === 'BAY');
    bayNode.classList.toggle('bay-occupied', !!occupant);
  }

  prevState = state;
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
