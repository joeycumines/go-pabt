/*
   Wiring the client together.

   The shape of a session: read what the store is serving, read the records it has,
   show one, and follow it. The follow is the part with the ordering rules in it.

   A resume is a read, never something the page remembered. Everything needed to
   continue is either in the store or in a cursor the store minted, so a reload, a
   dropped connection, and a server restart all take the same path: ask again.

   Three rules govern what the client will claim:

     - a gap is closed by reading the store, not by waiting on the queue, because
       the store is the authority and the queue is a convenience;
     - the client follows the head until the reader chooses an earlier tick, and
       then stops following, so a live run is watchable without yanking a reader
       away from the tick they are reading;
     - a delta is applied only when its base is actually held, and the result is
       compared with the tree the store served. When the base is not held there is
       nothing to compare, and the client says nothing about continuity rather than
       implying a check it did not make.

   The capability lives in sessionStorage, nowhere else: a token in a URL is a token
   in an access log, and sessionStorage dies with the tab, so a machine left running
   does not keep handing out access to a history nobody is watching.
*/

import { Api, ApiError } from "./api.mjs";
import { EventStream } from "./stream.mjs";
import { Model, compareSeq, governingConditions, indexTree } from "./model.mjs";
import { applyDelta, sameTree, ContinuityFault } from "./delta.mjs";
import { renderDetail } from "./detail.mjs";
import {
  applyRovingTabindex,
  reachable,
  renderBreadcrumb,
  renderIdentity,
  renderScrub,
  renderTimeline,
  renderTree,
  setConnection,
} from "./render.mjs";

const CAPABILITY_KEY = "pabtdebug.capability";
/** Below this depth a subtree starts closed, so a deep plan is readable on arrival. */
const COLLAPSE_BELOW_DEPTH = 2;

const model = new Model();
/** @type {Set<string>} the subtrees currently closed */
const collapsed = new Set();
/** @type {Set<string>} the ticks whose delta has already been checked */
const verified = new Set();
let api = null;
let stream = null;
/** @type {string} the highest sequence this client has applied */
let position = "0";
/** Whether the client is following the head, which a reader can switch off. */
let following = true;

/* ---------------- connect ---------------- */

document.getElementById("connect-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const error = document.getElementById("connect-error");
  error.hidden = true;
  const capability = document.getElementById("capability").value.trim();
  if (capability === "") {
    error.textContent = "A capability is required.";
    error.hidden = false;
    return;
  }
  sessionStorage.setItem(CAPABILITY_KEY, capability);
  await boot(capability);
});

/* ---------------- boot and follow ---------------- */

/**
 * @param {string} capability
 */
async function boot(capability) {
  api = new Api(capability);
  try {
    await readStore();
    await readRecords("0");
  } catch (cause) {
    if (cause instanceof ApiError && cause.isUnauthenticated) {
      sessionStorage.removeItem(CAPABILITY_KEY);
      showConnect("The capability was refused. Paste the one the application issued.");
      return;
    }
    showConnect(cause.message);
    return;
  }
  document.getElementById("connect").hidden = true;
  document.getElementById("app").hidden = false;
  if (model.sequences().length > 0) {
    select(model.head);
  } else {
    paint();
  }
  await follow();
}

/**
 * @param {string} message
 */
function showConnect(message) {
  const error = document.getElementById("connect-error");
  error.textContent = message;
  error.hidden = false;
  document.getElementById("connect").hidden = false;
  document.getElementById("app").hidden = true;
  if (stream) {
    stream.stop();
    stream = null;
  }
}

/** Reads the store's own account of what it is serving. */
async function readStore() {
  const state = await api.state();
  model.adopt(state);
  renderIdentity(model);
}

/**
 * Pages the records from a position.
 *
 * @param {string} from
 */
async function readRecords(from) {
  let after = from;
  for (;;) {
    const response = await api.records(after);
    const page = response.page;
    for (const record of page.Records) {
      model.putRecord(record);
    }
    if (page.HasMore && page.Records.length === 0) {
      // A page that claims more and holds nothing would otherwise be read
      // forever. The store is contradicting itself, so it is reported rather than
      // waited on.
      model.note(after, "the store reported more records and sent none");
      break;
    }
    if (!page.HasMore) break;
    after = String(page.Through);
  }
  position = model.lastSequence();
  if (model.head && compareSeq(model.head, position) > 0) {
    // The head is a committed record, so a read that stopped short of it is
    // reported rather than treated as current.
    model.note(position, `read to ${position}, the head is ${model.head}`);
  }
}

/** Mints a cursor for the position held and follows the run from it. */
async function follow() {
  if (stream) stream.stop();
  const minted = await api.cursor(position);
  stream = new EventStream(api, {
    onRecord: (record) => {
      handleRecord(record).catch((cause) => setConnection("refused", cause.message.slice(0, 80)));
    },
    onBehind: () => {
      resume().catch((cause) => setConnection("refused", cause.message.slice(0, 80)));
    },
    onState: (state) => setConnection(state),
    onError: (cause) => setConnection("refused", cause.message.slice(0, 80)),
  });
  stream.start(minted.token, position);
  setConnection("live", `from ${position}`);
}

/**
 * @param {any} record a record the stream announced
 */
async function handleRecord(record) {
  if (compareSeq(record.sequence, position) <= 0) return; // Already held.
  if (compareSeq(record.sequence, position) > 1) {
    setConnection("reloading", `gap to ${record.sequence}`);
    await readRecords(position);
  }
  model.putRecord(record);
  position = record.sequence;
  if (compareSeq(record.sequence, model.head) > 0) {
    model.head = record.sequence;
  }
  if (following) {
    select(record.sequence);
  } else {
    paint();
  }
}

/**
 * Reconnects after the store ended the stream.
 *
 * The cursor the store sends with a `behind` event is deliberately not used to
 * choose the position. It names the last sequence the *connection* wrote, which
 * can be ahead of the last one this client applied; resuming from it would skip
 * records that never arrived. The client's own position is the conservative one,
 * and re-reading a record the client already holds is free because the stream
 * filters anything it has seen.
 */
async function resume() {
  setConnection("reloading", "fell behind");
  if (stream) stream.stop();
  await readStore();
  await readRecords(position);
  await follow();
}

/** Starts over against whatever the store serves now, after the history changed. */
async function reboot() {
  model.records.clear();
  model.trees.clear();
  verified.clear();
  collapsed.clear();
  model.continuity.length = 0;
  position = "0";
  following = true;
  await readStore();
  await readRecords("0");
  if (model.sequences().length > 0) {
    select(model.head);
  } else {
    paint();
  }
  await follow();
}

/* ---------------- continuity ---------------- */

/**
 * Applies and checks the delta behind the tick being looked at.
 *
 * The record already carries the tree it committed, so this is not how a tree is
 * obtained. It is how the client finds out whether the history it holds is
 * continuous with the store's: the delta names the base it means, the base is
 * compared against the digest the store itself wrote, and the result is compared
 * with the tree that came with the record. A disagreement is reported, because a
 * client that quietly repaired one would be hiding the only signal that history
 * and transport had parted company.
 *
 * It runs for the tick on screen rather than for every tick read, so loading a
 * long run costs one request per page instead of two, and the check happens
 * exactly where a reader would notice it.
 *
 * @param {string} sequence
 */
async function verifyContinuity(sequence) {
  if (verified.has(sequence)) return;
  const served = model.tree(sequence);
  if (!served) return;
  verified.add(sequence);
  let representation;
  try {
    representation = await api.representation(sequence);
  } catch (cause) {
    model.note(sequence, `the representation could not be read: ${cause.message}`);
    paint();
    return;
  }
  const delta = representation.delta;
  // No delta means the store stored a keyframe, which is not a claim about
  // continuity and so is not checked.
  if (!delta) return;
  const base = model.tree(String(delta.baseSequence));
  if (!base) {
    // The base is not held, so there is nothing to compare and nothing is
    // claimed. This is the keyframe path: the served tree is used as it stands.
    return;
  }
  try {
    const applied = applyDelta(base, delta);
    const differences = sameTree(applied, served);
    if (differences.length > 0) {
      model.note(
        sequence,
        `the delta from tick ${delta.baseSequence} did not reproduce this tree: ` +
          differences.slice(0, 4).join(", "),
      );
    }
  } catch (cause) {
    if (cause instanceof ContinuityFault) {
      model.note(sequence, cause.message);
    } else {
      throw cause;
    }
  }
  paint();
}

/* ---------------- selection and painting ---------------- */

/**
 * @param {string} sequence
 */
function select(sequence) {
  if (!model.records.has(sequence)) return;
  model.selectedSequence = sequence;
  // Choosing the head keeps the client following; choosing anything earlier is
  // the reader taking control, and it stays taken until they return to the head.
  following = compareSeq(sequence, model.head) === 0;
  const tree = model.tree(sequence);
  if (tree) {
    const index = indexTree(tree);
    if (!index.nodes.has(model.selectedNodeId)) {
      // The first occurrence that governs something is the more useful default
      // than the root, which governs nothing and explains nothing.
      const informative = [...index.nodes.values()].find(
        (node) => node.conditionId || node.actionId || node.postNodeId,
      );
      const roots = index.children.get(null) ?? [];
      model.selectedNodeId = (informative ?? (roots[0] ? index.nodes.get(roots[0]) : null))?.id ?? "";
    }
    if (collapsed.size === 0) collapseBelowDepth(index, COLLAPSE_BELOW_DEPTH);
  } else {
    model.selectedNodeId = "";
  }
  paint();
  verifyContinuity(sequence).catch((cause) => setConnection("refused", cause.message.slice(0, 80)));
}

/**
 * @param {string} nodeId
 */
function selectNode(nodeId) {
  model.selectedNodeId = nodeId;
  paint();
}

/**
 * Closes every subtree at or below a depth, so a deep plan arrives readable
 * without hiding the shape of its first levels.
 *
 * @param {any} index
 * @param {number} limit
 */
function collapseBelowDepth(index, limit) {
  for (const node of index.nodes.values()) {
    const childIds = index.children.get(node.id) ?? [];
    if (childIds.length === 0) continue;
    if (depthOf(index, node.id) >= limit) collapsed.add(node.id);
  }
}

/**
 * @param {any} index
 * @param {string} id
 * @returns {number} how many links from the root, so the root is zero
 */
function depthOf(index, id) {
  let depth = 0;
  let node = index.nodes.get(id);
  const guard = index.nodes.size + 1;
  while (node && (node.parentId ?? null) !== null && depth < guard) {
    node = index.nodes.get(node.parentId);
    depth += 1;
  }
  return depth;
}

function paint() {
  const record = model.record(model.selectedSequence);
  const tree = model.tree(model.selectedSequence);
  const index = tree ? indexTree(tree) : null;
  const node = index ? index.nodes.get(model.selectedNodeId) ?? null : null;
  renderIdentity(model);
  renderTimeline(model, (sequence) => select(sequence));
  renderBreadcrumb(model, model.selectedNodeId, (id) => selectNode(id));
  renderTree(model, model.selectedNodeId, (id) => selectNode(id), toggle, collapsed);
  renderScrub(model);
  renderDetail(
    document.getElementById("detail-body"),
    model,
    record,
    node,
    node ? governingConditions(record, node) : [],
  );
}

/**
 * @param {string} id
 */
function toggle(id) {
  if (collapsed.has(id)) {
    collapsed.delete(id);
  } else {
    collapsed.add(id);
  }
  paint();
}

/* ---------------- keyboard ---------------- */

document.getElementById("tree").addEventListener("keydown", (event) => {
  const root = document.getElementById("tree");
  const items = reachable(root);
  if (items.length === 0) return;
  const current = items.findIndex((item) => item.querySelector(".node") === event.target);
  if (current < 0) return;
  const focus = (item) => {
    const button = item.querySelector(".node");
    if (!button) return;
    applyRovingTabindex(root, item.dataset.nodeId);
    button.focus();
  };
  switch (event.key) {
    case "ArrowDown": focus(items[Math.min(items.length - 1, current + 1)]); break;
    case "ArrowUp": focus(items[Math.max(0, current - 1)]); break;
    case "Home": focus(items[0]); break;
    case "End": focus(items[items.length - 1]); break;
    case "ArrowRight":
      if (items[current].getAttribute("aria-expanded") === "false") {
        toggle(items[current].dataset.nodeId);
      } else {
        focus(items[Math.min(items.length - 1, current + 1)]);
      }
      break;
    case "ArrowLeft": {
      const item = items[current];
      if (item.getAttribute("aria-expanded") === "true") {
        toggle(item.dataset.nodeId);
      } else {
        const parent = item.parentElement?.closest('[role="treeitem"]');
        if (parent) focus(parent);
      }
      break;
    }
    case "Enter":
    case " ":
      selectNode(items[current].dataset.nodeId);
      break;
    default:
      return;
  }
  event.preventDefault();
});

document.getElementById("scrub-range").addEventListener("input", (event) => {
  const sequence = event.target.value;
  if (model.records.has(sequence)) select(sequence);
});

/* ---------------- transfer ---------------- */

document.getElementById("export").addEventListener("click", async () => {
  try {
    const { bytes, digest } = await api.exportBundle();
    const url = URL.createObjectURL(new Blob([bytes], { type: "application/vnd.pabtdebug.bundle" }));
    const link = document.createElement("a");
    link.href = url;
    link.download = `pabt-run-${model.runKey ? model.runKey.runId : "run"}.pabt`;
    link.click();
    // The object URL has to outlive the click that started the download, so it is
    // released on the next turn rather than immediately.
    setTimeout(() => URL.revokeObjectURL(url), 0);
    setConnection("live", `exported ${bytes.byteLength} bytes ${digest.slice(0, 12)}`);
  } catch (cause) {
    setConnection("refused", cause.message.slice(0, 80));
  }
});

document.getElementById("import").addEventListener("change", async (event) => {
  const file = event.target.files && event.target.files[0];
  if (!file) return;
  try {
    await api.importBundle(await file.arrayBuffer());
    await reboot();
  } catch (cause) {
    setConnection("refused", cause.message.slice(0, 80));
  } finally {
    event.target.value = "";
  }
});

/* ---------------- resume ---------------- */

const stored = sessionStorage.getItem(CAPABILITY_KEY);
if (stored) {
  document.getElementById("capability").value = stored;
  boot(stored);
} else {
  document.getElementById("connect").hidden = false;
}
