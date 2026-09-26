/*
   Applying an exact delta, and refusing one that does not hold.

   A representation response carries the complete target tree as well as the
   delta that produced it, so the client could ignore the delta and simply
   display what it was sent. That would be a client that never checks anything,
   and the delta is the only thing in the protocol that says what changed. So it
   is applied, and the result is compared against the tree the store served:

     - the delta applied to a tree that is not the base it names, or
     - a delta that produces something other than the served target,

   is a disagreement about history, not a rendering problem. It is reported and
   the base is refetched, and the served tree stands as the authority for what to
   show. The comparison is structural rather than a digest: recomputing the
   store's canonical form in the browser would mean reimplementing its canonical
   encoder, and a second implementation of a digest is a second thing to be
   wrong. The identity that actually matters here - is this the base - is checked
   against the digest the store itself wrote, which is exact and free.
*/

import { toInt } from "./lossless.mjs";

/** ContinuityFault says a delta did not describe the history the client holds. */
export class ContinuityFault extends Error {
  /**
   * @param {string} message
   * @param {string} sequence the target the delta claimed
   */
  constructor(message, sequence) {
    super(message);
    this.name = "ContinuityFault";
    this.sequence = sequence;
  }
}

/**
 * applyDelta produces the target tree from a base and the delta between them.
 *
 * Every identity is checked before anything is applied. A delta is refused
 * rather than partially applied, because a half-applied tree is a tree that
 * looks real and is not, and the only thing that would catch it later is the
 * comparison this is about to do anyway.
 *
 * @param {any} base the tree the delta claims to start from
 * @param {any} delta
 * @returns {any} the target tree
 */
export function applyDelta(base, delta) {
  if (!delta || typeof delta !== "object") {
    throw new ContinuityFault("no delta to apply", "");
  }
  if (base.digest !== delta.baseDigest) {
    throw new ContinuityFault(
      `the held tree is ${String(base.digest).slice(0, 12)} but the delta names ` +
        `${String(delta.baseDigest).slice(0, 12)} as its base`,
      String(delta.targetSequence),
    );
  }
  if (base.eventUid !== delta.baseEventUid || base.sequence !== delta.baseSequence) {
    throw new ContinuityFault(
      "the held tree and the delta disagree about which record the base is",
      String(delta.targetSequence),
    );
  }
  if (base.planRevision !== delta.basePlanRevision) {
    throw new ContinuityFault(
      `the base is at plan revision ${base.planRevision} and the delta says ${delta.basePlanRevision}`,
      String(delta.targetSequence),
    );
  }

  // Nodes are addressed by occurrence identity, never by position, so a node that
  // merely moved is addressed by the same key before and after.
  const nodes = new Map();
  for (const node of base.nodes ?? []) {
    nodes.set(node.id, node);
  }

  // Applied in phases rather than in the order the operations happen to be
  // listed, because a remove, an insert, and a replace of the same subtree are
  // only consistent in one order and the format fixes it.
  const byKind = { remove: [], insert: [], reorder: [], replace: [] };
  for (const op of delta.operations ?? []) {
    const bucket = byKind[op.kind];
    if (!bucket) throw new ContinuityFault(`unknown operation ${String(op.kind)}`, String(delta.targetSequence));
    bucket.push(op);
  }

  for (const op of byKind.remove) {
    removeSubtree(nodes, op.remove.nodeId);
  }
  for (const op of byKind.insert) {
    const { rootId, parentId, ordinal, nodes: added } = op.insert;
    if (nodes.has(rootId)) {
      throw new ContinuityFault(`the insert names ${rootId}, which is already in the tree`, String(delta.targetSequence));
    }
    if (parentId !== null && !nodes.has(parentId)) {
      throw new ContinuityFault(`the insert names parent ${parentId}, which is not in the tree`, String(delta.targetSequence));
    }
    for (const node of added) {
      nodes.set(node.id, node);
    }
    // The inserted subtree is placed at the position it claims, and the ordinal
    // on the node is not trusted over the order: one place says where a node is.
    place(nodes, parentId, rootId, toInt(ordinal));
  }
  for (const op of byKind.reorder) {
    const { parentId, childOrder } = op.reorder;
    for (const id of childOrder) {
      if (!nodes.has(id)) {
        throw new ContinuityFault(`the reorder names ${id}, which is not in the tree`, String(delta.targetSequence));
      }
      if (parentId !== null && nodeParent(nodes, id) !== parentId) {
        throw new ContinuityFault(`the reorder places ${id} under ${parentId}, which is not its parent`, String(delta.targetSequence));
      }
    }
    setChildOrder(nodes, parentId, childOrder);
  }
  for (const op of byKind.replace) {
    const { node } = op.replace;
    if (!nodes.has(node.id)) {
      throw new ContinuityFault(`the replace names ${node.id}, which is not in the tree`, String(delta.targetSequence));
    }
    // The whole node is taken, not just the fields the delta lists. A partial
    // merge would let a field the delta did not mention survive from the base,
    // and the result would then differ from the tree the store recorded.
    nodes.set(node.id, node);
  }

  return rebuild(delta, nodes);
}

/**
 * sameTree reports whether two trees describe the same structure and content.
 *
 * The comparison is on the fields a reader can see rather than on the digest, so
 * a disagreement names what differs instead of only that something does.
 *
 * @param {any} a
 * @param {any} b
 * @returns {string[]} the ways they differ, empty when they agree
 */
export function sameTree(a, b) {
  const differences = [];
  if (a.digest !== b.digest) differences.push("digest");
  if (a.eventUid !== b.eventUid) differences.push("eventUid");
  if (a.sequence !== b.sequence) differences.push("sequence");
  if (a.planRevision !== b.planRevision) differences.push("planRevision");
  if (a.rootId !== b.rootId) differences.push("rootId");
  if ((a.nodeCount ?? 0) !== (b.nodeCount ?? 0)) differences.push("nodeCount");
  if ((a.nodes ?? []).length !== (b.nodes ?? []).length) {
    differences.push("node length");
    return differences;
  }
  const left = new Map((a.nodes ?? []).map((n) => [n.id, n]));
  for (const node of b.nodes ?? []) {
    const other = left.get(node.id);
    if (!other) {
      differences.push(`missing ${node.id}`);
      continue;
    }
    for (const field of NODE_FIELDS) {
      if (JSON.stringify(other[field] ?? null) !== JSON.stringify(node[field] ?? null)) {
        differences.push(`${node.id}.${field}`);
      }
    }
  }
  return differences;
}

const NODE_FIELDS = [
  "parentId",
  "ordinal",
  "type",
  "name",
  "status",
  "conditionId",
  "postNodeId",
  "actionId",
  "effectIds",
  "frame",
  "contentHash",
  "lineage",
];

/**
 * removeSubtree deletes a node and everything under it.
 *
 * @param {Map<string, any>} nodes
 * @param {string} rootId
 */
function removeSubtree(nodes, rootId) {
  for (const id of descendants(nodes, rootId)) {
    nodes.delete(id);
  }
}

/**
 * descendants returns a node and everything under it.
 *
 * @param {Map<string, any>} nodes
 * @param {string} rootId
 * @returns {string[]}
 */
function descendants(nodes, rootId) {
  const out = [rootId];
  for (const id of childrenOf(nodes, rootId)) {
    out.push(...descendants(nodes, id));
  }
  return out;
}

/**
 * nodeParent returns a node's parent id, or null for the root.
 *
 * @param {Map<string, any>} nodes
 * @param {string} id
 * @returns {string|null}
 */
function nodeParent(nodes, id) {
  const node = nodes.get(id);
  return node ? (node.parentId ?? null) : null;
}

/**
 * childrenOf returns a node's children in the order the nodes themselves name,
 * which is the ordinal, because that is the one place the order is written.
 *
 * @param {Map<string, any>} nodes
 * @param {string} id
 * @returns {string[]}
 */
function childrenOf(nodes, id) {
  const out = [];
  for (const node of nodes.values()) {
    if ((node.parentId ?? null) === id) out.push(node.id);
  }
  out.sort((a, b) => toInt(nodes.get(a).ordinal) - toInt(nodes.get(b).ordinal));
  return out;
}

/**
 * place puts a subtree root at a position among its siblings.
 *
 * @param {Map<string, any>} nodes
 * @param {string|null} parentId
 * @param {string} id
 * @param {number} ordinal
 */
function place(nodes, parentId, id, ordinal) {
  const siblings = childrenOf(nodes, parentId);
  const at = siblings.indexOf(id);
  if (at >= 0) siblings.splice(at, 1);
  siblings.splice(Math.max(0, Math.min(ordinal, siblings.length)), 0, id);
  setChildOrder(nodes, parentId, siblings);
}

/**
 * setChildOrder writes the sibling order back onto the nodes.
 *
 * @param {Map<string, any>} nodes
 * @param {string|null} parentId
 * @param {string[]} order
 */
function setChildOrder(nodes, parentId, order) {
  order.forEach((id, index) => {
    const node = nodes.get(id);
    if (node) node.ordinal = String(index);
  });
  if (parentId === null) return;
  // A node that is not in the order is not a child of this parent, which is the
  // disagreement a malformed reorder would otherwise hide.
  for (const id of childrenOf(nodes, parentId)) {
    if (!order.includes(id)) {
      throw new ContinuityFault(`the reorder left ${id} under ${parentId} out of the order`, "");
    }
  }
}

/**
 * rebuild lays the node map back out in the preorder the store writes, which is
 * the order the tree is addressed by.
 *
 * @param {any} delta
 * @param {Map<string, any>} nodes
 * @returns {any}
 */
function rebuild(delta, nodes) {
  const out = [];
  const walk = (parentId) => {
    for (const id of childrenOf(nodes, parentId)) {
      out.push(nodes.get(id));
      walk(id);
    }
  };
  walk(null);
  if (out.length === 0 && nodes.size > 0) {
    // Every node unreachable means the parent links form a cycle. Refusing is
    // better than returning a tree that silently lost half of itself.
    throw new ContinuityFault("the delta produced a tree with no root", String(delta.targetSequence));
  }
  if (out.length !== nodes.size) {
    throw new ContinuityFault(
      `the delta left ${nodes.size - out.length} of ${nodes.size} nodes unreachable`,
      String(delta.targetSequence),
    );
  }
  return {
    runKey: delta.runKey,
    eventUid: delta.targetEventUid,
    sequence: delta.targetSequence,
    planRevision: delta.targetPlanRevision,
    rootId: findRoot(nodes),
    nodes: out,
    nodeCount: String(out.length),
    digest: delta.targetDigest,
  };
}

/**
 * findRoot returns the one node with no parent.
 *
 * @param {Map<string, any>} nodes
 * @returns {string}
 */
function findRoot(nodes) {
  for (const node of nodes.values()) {
    if ((node.parentId ?? null) === null) return node.id;
  }
  return "";
}
