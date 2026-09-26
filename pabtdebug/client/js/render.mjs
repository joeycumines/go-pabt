/*
   The regions: what the store is serving, the tick list, the path, the tree, and
   the scrub strip.

   The tree is an ARIA tree with a roving tabindex, built from each node's own
   parent link rather than from where a node sits in the array. That second point
   is a fix, not a style: UI-004's worker assumed the children of a node were the
   entries that followed it in preorder, so any node not stored contiguously with
   its subtree was drawn as a child of whatever preceded it. A move and a reorder
   are exactly the cases that break that assumption, and they are the cases a
   debugger exists to show.

   The markup is one treeitem per node, holding its own row and its own group.
   Collapsing removes the group from the document rather than hiding it, so the set
   of reachable items is exactly the set a keyboard reader can move through, with
   no second notion of visibility to keep in step.
*/

import { el } from "./detail.mjs";
import { compareSeq, indexTree, pathTo } from "./model.mjs";

/**
 * renderIdentity fills the header's account of the store.
 *
 * @param {any} model
 */
export function renderIdentity(model) {
  set("identity-store", model.storeId || "—");
  set("identity-run", model.runKey ? model.runKey.runId : "—");
  set("identity-head", model.head || "—");
  set("identity-generation", model.generation || "—");
}

/**
 * renderTimeline lists the recorded ticks, oldest first, and marks the selected
 * one. The list shows what the client has read: it is a view of held records, not
 * a claim about the whole run.
 *
 * @param {any} model
 * @param {(sequence: string) => void} onSelect
 */
export function renderTimeline(model, onSelect) {
  const list = document.getElementById("timeline-list");
  const sequences = model.sequences();
  const fragment = document.createDocumentFragment();
  for (const sequence of sequences) {
    const record = model.record(sequence);
    const button = el("button", {
      type: "button",
      "aria-current": String(sequence === model.selectedSequence),
      "data-sequence": sequence,
      title: `${record.kind} at tick ${sequence}`,
    }, [
      el("span", { class: "seq", text: sequence }),
      el("span", { class: "what", text: record.kind }),
      el("span", {
        class: "dot",
        "data-status": record.statusPresent ? record.status : record.kind,
        "aria-label": record.statusPresent ? record.status : "no status",
      }),
    ]);
    button.addEventListener("click", () => onSelect(sequence));
    fragment.appendChild(el("li", {}, [button]));
  }
  if (sequences.length === 0) {
    fragment.appendChild(el("li", {}, [
      el("p", { class: "detail-empty", text: "No records read yet." }),
    ]));
  }
  list.replaceChildren(fragment);
}

/**
 * renderBreadcrumb shows the root-first path to the selected occurrence.
 *
 * Every crumb selects that occurrence, so the path is a navigation control rather
 * than a label. The path wraps and the occurrence id breaks anywhere, which is the
 * fix for UI-001: the old one scrolled sideways, 3183 pixels of it against a 700
 * pixel column.
 *
 * @param {any} model
 * @param {string} nodeId
 * @param {(id: string) => void} onSelect
 */
export function renderBreadcrumb(model, nodeId, onSelect) {
  const list = document.getElementById("breadcrumb-list");
  const tree = model.tree(model.selectedSequence);
  if (!tree || !nodeId) {
    list.replaceChildren();
    return;
  }
  const index = indexTree(tree);
  const path = pathTo(index, nodeId);
  if (path.length === 0) {
    list.replaceChildren(el("li", {}, [
      el("span", { class: "crumb-name", text: "not in this tree" }),
    ]));
    return;
  }
  const fragment = document.createDocumentFragment();
  path.forEach((id, position) => {
    const node = index.nodes.get(id);
    if (position > 0) fragment.appendChild(el("li", {}, [el("span", { class: "sep", text: "›" })]));
    const button = el("button", { class: "crumb", type: "button", title: id }, [
      el("span", { class: "crumb-name", text: node.name || node.type }),
    ]);
    button.addEventListener("click", () => onSelect(id));
    fragment.appendChild(el("li", {}, [button, el("span", { class: "crumb-id", text: id })]));
  });
  list.replaceChildren(fragment);
}

/**
 * renderTree draws the occurrence tree as an ARIA tree.
 *
 * @param {any} model
 * @param {string} nodeId the selected occurrence
 * @param {(id: string) => void} onSelect
 * @param {(id: string) => void} onToggle expand or collapse one subtree
 * @param {Set<string>} collapsed the subtrees currently closed
 */
export function renderTree(model, nodeId, onSelect, onToggle, collapsed) {
  const root = document.getElementById("tree");
  const revision = document.getElementById("canvas-revision");
  const tree = model.tree(model.selectedSequence);
  root.replaceChildren();
  if (!tree) {
    revision.textContent = "";
    root.appendChild(el("li", { role: "none" }, [
      el("p", { class: "detail-empty", text: "No tree for this tick." }),
    ]));
    return;
  }
  revision.textContent = `revision ${tree.planRevision} · ${tree.nodeCount} nodes`;
  const index = indexTree(tree);
  const roots = index.children.get(null) ?? [];
  if (roots.length === 0) {
    root.appendChild(el("li", { role: "none" }, [
      el("p", { class: "detail-empty", text: "This tick committed a tree with no root." }),
    ]));
    return;
  }
  for (const id of roots) {
    root.appendChild(buildItem(index, id, 0, nodeId, onSelect, onToggle, collapsed));
  }
  applyRovingTabindex(root, nodeId);
}

/**
 * buildItem builds one treeitem, its row, and its group.
 *
 * @param {any} index
 * @param {string} id
 * @param {number} depth
 * @param {string} selectedId
 * @param {(id: string) => void} onSelect
 * @param {(id: string) => void} onToggle
 * @param {Set<string>} collapsed
 * @returns {HTMLElement}
 */
function buildItem(index, id, depth, selectedId, onSelect, onToggle, collapsed) {
  const node = index.nodes.get(id);
  const childIds = index.children.get(id) ?? [];
  const hasChildren = childIds.length > 0;
  const isCollapsed = hasChildren && collapsed.has(id);
  const selected = id === selectedId;
  const status = node.status ?? {};

  const row = el("button", {
    class: "node",
    type: "button",
    "data-node-id": id,
    "data-type": node.type,
    "aria-selected": String(selected),
    tabindex: "-1",
    title: `${node.type} · ${id}`,
  }, [
    el("span", { class: "twisty", text: hasChildren ? (isCollapsed ? "▸" : "▾") : "" }),
    el("span", {
      class: "dot",
      "data-status": status.statusPresent ? status.lastStatus : "none",
    }),
    el("span", { class: "name", text: node.name || node.type }),
    node.conditionId ? el("span", { class: "badge", "data-badge": "condition", text: "cond" }) : null,
    node.actionId ? el("span", { class: "badge", "data-badge": "action", text: "act" }) : null,
    el("span", { class: "type", text: node.type }),
  ].filter(Boolean));

  const item = el("li", { role: "treeitem", "data-node-id": id, "aria-selected": String(selected) }, [row]);
  if (hasChildren) {
    item.setAttribute("aria-expanded", String(!isCollapsed));
    item.setAttribute("aria-level", String(depth + 1));
    if (!isCollapsed) {
      const group = el("ul", { role: "group" });
      for (const childId of childIds) {
        group.appendChild(buildItem(index, childId, depth + 1, selectedId, onSelect, onToggle, collapsed));
      }
      item.appendChild(group);
    }
  }
  // The twisty is inside the row, so a click on it is a click on the row.
  // Shift-click opens or closes the subtree rather than selecting, which is the
  // convention a reader of a tree already has.
  row.addEventListener("click", (event) => {
    if (event.shiftKey && hasChildren) {
      onToggle(id);
      return;
    }
    onSelect(id);
  });
  return item;
}

/**
 * reachable returns the items a keyboard reader can move through, in visual
 * order. The tree is nested, so document order is the preorder a reader expects,
 * and a collapsed subtree's descendants are not in the document at all, so this
 * needs no visibility filter of its own.
 *
 * @param {HTMLElement} root
 * @returns {HTMLElement[]}
 */
export function reachable(root) {
  return [...root.querySelectorAll('[role="treeitem"]')];
}

/**
 * applyRovingTabindex gives the tree one tab stop, on the selected item or the
 * first one, so a keyboard reader tabs into the tree once and then moves within
 * it rather than tabbing through every node.
 *
 * @param {HTMLElement} root
 * @param {string} nodeId
 */
export function applyRovingTabindex(root, nodeId) {
  const items = reachable(root);
  if (items.length === 0) return;
  const target = items.find((item) => item.dataset.nodeId === nodeId) ?? items[0];
  for (const item of items) {
    item.querySelector(".node")?.setAttribute("tabindex", item === target ? "0" : "-1");
  }
}

/**
 * renderScrub points the strip at the held range and the selected tick.
 *
 * The strip is an input[type=range] over sequences, which is why its semantics,
 * its keyboard handling, and its announcement come from the platform rather than
 * from code here. Its upper bound is the highest sequence the client has read, so
 * the strip never offers a tick that has not been fetched and would render empty.
 *
 * @param {any} model
 */
export function renderScrub(model) {
  const range = document.getElementById("scrub-range");
  const readout = document.getElementById("scrub-readout");
  const held = model.sequences();
  const first = held.length > 0 ? held[0] : "1";
  const last = model.head && compareSeq(model.head, first) > 0 ? model.head : first;
  const selected = held.includes(model.selectedSequence) ? model.selectedSequence : first;
  range.min = first;
  range.max = last;
  range.value = selected;
  range.setAttribute("aria-valuetext", `tick ${selected}`);
  readout.textContent = selected;
}

/**
 * setConnection reports what the stream is doing, which is the one thing a reader
 * cannot infer from the data: a page full of records looks identical whether it is
 * live or frozen.
 *
 * @param {string} state
 * @param {string} [detail]
 */
export function setConnection(state, detail) {
  const node = document.getElementById("connection");
  node.dataset.state = state;
  node.textContent = detail ? `${state}: ${detail}` : state;
}

function set(id, value) {
  const node = document.getElementById(id);
  if (node) node.textContent = value;
}
