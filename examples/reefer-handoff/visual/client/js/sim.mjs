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
//
// V4: incident visualization. State diffs and the receipt ledger drive the
// incident effects — a locked lane turns red, pulses, and gains a lock icon,
// then returns to normal with a green flash; an applied ScanTag sweeps a
// scan-line over the reefer; a BayOccupied refusal flashes the bay orange;
// a SlotUnpowered refusal flashes the slot red with an X. An event log
// panel keeps the last five events.

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

// ── V4: domain facts the effects key off ────────────────────────────────
// Refusal reasons and outcomes as the sim domain names them. The receipt
// ledger in /sim/state carries these strings verbatim.
const REASON_BAY_OCCUPIED = 'BayOccupied';
const REASON_SLOT_UNPOWERED = 'SlotUnpowered';
const REASON_LANE_LOCKED = 'LaneLocked';
const OUTCOME_REFUSED = 'Refused';
const BAY_NODE = 'BAY';

// ── DOM ─────────────────────────────────────────────────────────────────
const lanesLayer = document.getElementById('lanes-layer');
const nodesLayer = document.getElementById('nodes-layer');
const entitiesLayer = document.getElementById('entities-layer');
const effectsLayer = document.getElementById('effects-layer');
const roundEl = document.getElementById('status-round');
const cutoffEl = document.getElementById('status-cutoff');
const phaseEl = document.getElementById('status-phase');
const connEl = document.getElementById('connection-status');
const eventLogEl = document.getElementById('event-log');

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

// ── V4: incident effects and the event log ─────────────────────────────
//
// Lane lock/reopen and slot power changes are read off the state diff;
// command-driven facts — a scan that applied, a contention refusal, a
// power rejection — are read off new receipts, which the world's ledger
// appends in command order. The first response establishes the baseline
// without firing effects.
let lastCommandSeq = null;

const lockIconEls = new Map(); // lane id -> icon group, while locked
const scanTimers = new Map();  // reefer id -> removal timeout
const bayFlashTimer = { id: null };
const slotRejectTimers = new Map(); // slot id -> removal timeout

// addEvent prepends one entry to the log and keeps only the last five.
function addEvent(round, kind, message) {
  const li = document.createElement('li');
  li.className = `event-item event-${kind}`;
  const time = document.createElement('span');
  time.className = 'event-time';
  time.textContent = new Date().toTimeString().slice(0, 8);
  const msg = document.createElement('span');
  msg.className = 'event-msg';
  msg.textContent = `R${round} · ${message}`;
  li.appendChild(time);
  li.appendChild(msg);
  eventLogEl.prepend(li);
  while (eventLogEl.children.length > 5) {
    eventLogEl.removeChild(eventLogEl.lastChild);
  }
}

// laneMidpoint finds the centre of a lane's rendered path, for the lock icon.
function laneMidpoint(laneId) {
  const path = lanesLayer.querySelector(`[data-lane="${laneId}"]`);
  if (!path) return null;
  const len = path.getTotalLength();
  if (!len) return null;
  const pt = path.getPointAtLength(len / 2);
  return { x: pt.x, y: pt.y };
}

// showLockIcon places a lock icon on a locked lane; removeLockIcon takes it
// back. Both are idempotent.
function showLockIcon(laneId) {
  if (lockIconEls.has(laneId)) return;
  const mid = laneMidpoint(laneId);
  if (!mid) return;
  const g = svgEl('g', { class: 'lock-icon', 'data-lock-lane': laneId });
  g.appendChild(svgEl('rect', { x: -6, y: -2, width: 12, height: 9, rx: 2, class: 'lock-body' }));
  g.appendChild(svgEl('path', { d: 'M -4 -2 V -5 A 4 4 0 0 1 4 -5 V -2', class: 'lock-shackle' }));
  g.setAttribute('transform', `translate(${mid.x}, ${mid.y})`);
  effectsLayer.appendChild(g);
  lockIconEls.set(laneId, g);
}

function removeLockIcon(laneId) {
  const el = lockIconEls.get(laneId);
  if (el) {
    el.remove();
    lockIconEls.delete(laneId);
  }
}

// fireLaneLock animates a lane that just locked: red pulse and a lock icon,
// and records the incident.
function fireLaneLock(round, laneId) {
  const path = lanesLayer.querySelector(`[data-lane="${laneId}"]`);
  if (path) {
    path.classList.add('lane-lock-pulse');
    setTimeout(() => path.classList.remove('lane-lock-pulse'), 2000);
  }
  showLockIcon(laneId);
  addEvent(round, 'incident', `Wind lockout: ${laneId} closed`);
}

// fireLaneReopen animates a lane that just reopened: a brief green flash,
// then back to normal.
function fireLaneReopen(round, laneId) {
  const path = lanesLayer.querySelector(`[data-lane="${laneId}"]`);
  if (path) {
    path.classList.add('lane-reopen-flash');
    setTimeout(() => path.classList.remove('lane-reopen-flash'), 900);
  }
  removeLockIcon(laneId);
  addEvent(round, 'incident', `${laneId} reopened`);
}

// fireScanLine sweeps a scan-line over a reefer whose tag was just read.
function fireScanLine(round, reeferId, pos) {
  if (!pos) return;
  if (scanTimers.has(reeferId)) {
    clearTimeout(scanTimers.get(reeferId));
    const old = effectsLayer.querySelector(`[data-scan="${reeferId}"]`);
    if (old) old.remove();
  }
  const g = svgEl('g', { class: 'scan-effect', 'data-scan': reeferId });
  g.appendChild(svgEl('line', { x1: -16, y1: 0, x2: 16, y2: 0, class: 'scan-line' }));
  g.setAttribute('transform', `translate(${pos.x}, ${pos.y})`);
  effectsLayer.appendChild(g);
  scanTimers.set(reeferId, setTimeout(() => {
    g.remove();
    scanTimers.delete(reeferId);
  }, 1200));
  addEvent(round, 'scan', `RFID scan: ${reeferId} tag read`);
}

// fireBayContention flashes the bay border orange when a move into the bay
// is refused as occupied.
function fireBayContention(round, holder) {
  const bayG = nodesLayer.querySelector(`[data-node="${BAY_NODE}"]`);
  if (bayG) {
    bayG.classList.add('bay-contention-flash');
    if (bayFlashTimer.id) clearTimeout(bayFlashTimer.id);
    bayFlashTimer.id = setTimeout(() => {
      bayG.classList.remove('bay-contention-flash');
      bayFlashTimer.id = null;
    }, 1000);
  }
  addEvent(round, 'refusal', holder ? `Bay contention: held by ${holder}` : 'Bay contention');
}

// fireSlotRejection flashes a slot red with a brief X when a placement is
// refused because the slot is unpowered.
function fireSlotRejection(round, slotId, tractorId) {
  const glow = slotGlowEls.get(slotId);
  if (glow) {
    glow.classList.add('slot-reject-flash');
    if (slotRejectTimers.has(slotId)) clearTimeout(slotRejectTimers.get(slotId));
    slotRejectTimers.set(slotId, setTimeout(() => {
      glow.classList.remove('slot-reject-flash');
      slotRejectTimers.delete(slotId);
    }, 1000));
  }
  const pos = NODE_POS[slotId];
  if (pos && !effectsLayer.querySelector(`[data-reject="${slotId}"]`)) {
    const x = svgEl('text', { x: pos.x, y: pos.y, class: 'slot-reject-x', 'data-reject': slotId });
    x.textContent = '✕';
    effectsLayer.appendChild(x);
    setTimeout(() => x.remove(), 1000);
  }
  addEvent(round, 'refusal', `Power rejection: ${slotId} refused ${tractorId}`);
}

// laneEffects diffs lane lock states against the previous response and
// reconciles lock icons, so the icons never outlive the lock they mark.
function laneEffects(state, prev) {
  if (!prev) return;
  const prevLocked = new Map();
  for (const l of prev.lanes || []) prevLocked.set(l.id, l.locked);
  for (const lane of state.lanes) {
    const was = prevLocked.get(lane.id);
    if (was === undefined || was === lane.locked) continue;
    if (lane.locked) {
      fireLaneLock(state.round, lane.id);
    } else {
      fireLaneReopen(state.round, lane.id);
    }
  }
}

// receiptEffects reacts to receipts new since the last response: the
// refusal reasons the incidents care about flash their target.
function receiptEffects(state) {
  const receipts = state.receipts || [];
  const prevSeq = lastCommandSeq;
  lastCommandSeq = state.commandSeq;
  if (prevSeq === null) return; // first response: baseline only
  for (const r of receipts) {
    if (r.commandSeq <= prevSeq) continue;
    if (r.outcome !== OUTCOME_REFUSED) continue;
    switch (r.reason) {
      case REASON_BAY_OCCUPIED:
        fireBayContention(state.round, r.holder);
        break;
      case REASON_SLOT_UNPOWERED:
        fireSlotRejection(state.round, r.holder, r.tractor);
        break;
      case REASON_LANE_LOCKED:
        addEvent(state.round, 'refusal', `${r.tractor} refused: lane ${r.holder} locked`);
        break;
    }
  }
}

// tagScanEffects detects reefers whose tag just became readable (a scan
// applied) and sweeps the scan-line over them. The receipt ledger does not
// carry the reefer ID, so the state diff is the reliable signal.
function tagScanEffects(state, prev) {
  if (!prev) return;
  const prevTags = new Map();
  for (const r of prev.reefers || []) prevTags.set(r.id, r.tagReadable);
  for (const r of state.reefers) {
    const was = prevTags.get(r.id);
    if (was === undefined || was === r.tagReadable) continue;
    if (r.tagReadable) {
      const pos = NODE_POS[r.at] || { x: 0, y: 0 };
      fireScanLine(state.round, r.id, pos);
    }
  }
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

  // V4: incident effects — lane lock/reopen and tag scans from the state
  // diff, then command outcomes from new receipts.
  laneEffects(state, prevState);
  tagScanEffects(state, prevState);
  receiptEffects(state);

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
