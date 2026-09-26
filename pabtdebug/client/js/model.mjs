/*
   What the client holds, and how a new record joins it.

   The store is the authority on what it committed, so the model never invents a
   tree, a sequence, or a value. It holds what the routes served, applies the
   delta when a delta is what arrived, and reports a disagreement rather than
   papering over one.

   Sequences are decimal strings throughout, because lossless.js made them so and
   because a uint64 must never have been a float. Comparison is therefore a
   decimal comparison, not a subtraction, and that is the one place where
   toInt must not be used.
*/

import { toInt } from "./lossless.mjs";

/**
 * model is the client's view of one run.
 */
export class Model {
  constructor() {
    /** @type {Map<string, any>} records by sequence */
    this.records = new Map();
    /** @type {Map<string, any>} trees by sequence */
    this.trees = new Map();
    this.storeId = "";
    this.generation = "";
    this.runKey = null;
    this.runState = "";
    this.recovery = "";
    this.head = "0";
    this.headTreeSequence = "0";
    this.selectedSequence = "";
    this.selectedNodeId = "";
    this.cursorToken = "";
    this.continuity = [];
  }

  /**
   * adopt reads the store's own account of what it is serving.
   *
   * @param {any} state
   */
  adopt(state) {
    if (this.storeId && state.storeId !== this.storeId) {
      throw new Error(`the store is ${state.storeId}, this client was reading ${this.storeId}`);
    }
    this.storeId = state.storeId;
    this.generation = String(state.generation);
    this.runKey = state.run;
    this.runState = state.runState;
    this.recovery = state.recovery;
    this.head = String(state.head.HeadSequence);
    this.headTreeSequence = String(state.head.HeadTreeSequence);
  }

  /**
   * putRecord holds one whole record.
   *
   * @param {any} record
   */
  putRecord(record) {
    this.records.set(record.sequence, record);
    if (record.tree) {
      this.trees.set(record.sequence, record.tree);
    }
  }

  /**
   * isGap reports whether a sequence stands beyond the next one this client
   * holds, which is the condition for reading the store rather than waiting.
   *
   * It is a decimal comparison, not arithmetic on a number: the sequence after
   * 18446744073709551615 is not a float, and a check written as `sequence >
   * last + 1` would both overflow and quietly stop detecting gaps.
   *
   * @param {string} sequence
   * @returns {boolean}
   */
  isGap(sequence) {
    if (this.records.size === 0) return false;
    const last = this.lastSequence();
    if (compareSeq(sequence, last) <= 0) return false; // A duplicate or older: not a gap.
    return String(sequence) !== incrementDecimal(last);
  }

  /** @returns {string} the highest sequence held */
  lastSequence() {
    let last = "0";
    for (const sequence of this.records.keys()) {
      if (compareSeq(sequence, last) > 0) last = sequence;
    }
    return last;
  }

  /** @returns {string[]} every sequence held, in order */
  sequences() {
    return [...this.records.keys()].sort(compareSeq);
  }

  /**
   * tree returns the tree at a sequence, or null when it is not held.
   *
   * @param {string} sequence
   * @returns {any|null}
   */
  tree(sequence) {
    return this.trees.get(sequence) ?? null;
  }

  /**
   * record returns the record at a sequence, or null when it is not held.
   *
   * @param {string} sequence
   * @returns {any|null}
   */
  record(sequence) {
    return this.records.get(sequence) ?? null;
  }

  /**
   * note records a continuity disagreement for the operator to see.
   *
   * @param {string} sequence
   * @param {string} reason
   */
  note(sequence, reason) {
    this.continuity = this.continuity.filter((entry) => entry.sequence !== sequence);
    this.continuity.push({ sequence, reason });
  }
}

/**
 * compareSeq orders two decimal sequences without going through a number.
 *
 * Length first, then bytes: a decimal string's length orders its magnitude, so
 * "9" precedes "10" and "18446744073709551615" precedes nothing that is longer.
 * Zero padding is not a shape the store writes, so it is not handled.
 *
 * @param {string} a
 * @param {string} b
 * @returns {number}
 */
export function compareSeq(a, b) {
  const left = String(a);
  const right = String(b);
  if (left.length !== right.length) return left.length < right.length ? -1 : 1;
  if (left === right) return 0;
  return left < right ? -1 : 1;
}

/**
 * incrementDecimal returns the decimal string one greater.
 *
 * @param {string} value
 * @returns {string}
 */
function incrementDecimal(value) {
  const digits = String(value).split("");
  for (let i = digits.length - 1; i >= 0; i -= 1) {
    if (digits[i] === "9") {
      digits[i] = "0";
      continue;
    }
    digits[i] = String(Number(digits[i]) + 1);
    return digits.join("");
  }
  return `1${digits.join("")}`;
}

/**
 * indexTree addresses a tree by occurrence identity.
 *
 * Nodes are keyed by their own id and children are read from each node's parent
 * rather than from where a node happens to sit in the array. That is what makes
 * a layout survive a move and a reorder, and it is the fix for UI-004, whose
 * worker assumed the children of a node were the next entries in preorder and so
 * drew a different tree from the one the store held.
 *
 * @param {any} tree
 * @returns {{nodes: Map<string, any>, children: Map<string, string[]>, rootId: string}}
 */
export function indexTree(tree) {
  const nodes = new Map();
  const children = new Map();
  if (!tree) return { nodes, children, rootId: "" };
  for (const node of tree.nodes ?? []) {
    nodes.set(node.id, node);
  }
  for (const node of nodes.values()) {
    const parent = node.parentId ?? null;
    if (!children.has(parent)) children.set(parent, []);
    children.get(parent).push(node.id);
  }
  // The order is the ordinal, which is the one place the store writes it.
  for (const [parent, ids] of children) {
    ids.sort((a, b) => toInt(nodes.get(a).ordinal) - toInt(nodes.get(b).ordinal));
    children.set(parent, ids);
  }
  let rootId = tree.rootId ?? "";
  if (!nodes.has(rootId)) {
    for (const [parent, ids] of children) {
      if (parent === null && ids.length > 0) rootId = ids[0];
    }
  }
  return { nodes, children, rootId };
}

/**
 * pathTo returns the root-first path to a node, which is what the breadcrumb
 * shows. An occurrence not in the tree yields an empty path rather than a
 * half-built one, because a path that stops in the middle reads as a claim about
 * the tree's shape.
 *
 * @param {{nodes: Map<string, any>, children: Map<string, string[]>}} index
 * @param {string} id
 * @returns {string[]}
 */
export function pathTo(index, id) {
  const out = [];
  let current = id;
  const guard = index.nodes.size + 1;
  for (let step = 0; step < guard; step += 1) {
    const node = index.nodes.get(current);
    if (!node) return [];
    out.unshift(current);
    const parent = node.parentId ?? null;
    if (parent === null) break;
    current = parent;
  }
  return out;
}

/**
 * governingConditions returns the conditions that govern one node, in a stated
 * order: the condition the node evaluates, the condition it re-evaluates, the
 * postcondition of the action it belongs to, then that action's guarded
 * alternatives.
 *
 * The order is the one a reader asks for - what stopped this occurrence, then what
 * it was there to achieve, then what else could have run instead - and it is
 * computed from the record's own links rather than from a shared variable key,
 * because a shared key is support and not governance.
 *
 * The links are read from both ends deliberately. A node says which condition it
 * is, and a condition says which nodes evaluate it; a record missing either would
 * otherwise silently govern nothing.
 *
 * @param {any} record
 * @param {any} node
 * @returns {any[]} the conditions, each still carrying its evidence fields
 */
export function governingConditions(record, node) {
  if (!record || !node) return [];
  const conditions = record.conditions ?? [];
  const byId = new Map(conditions.map((c) => [c.conditionId, c]));
  const out = [];
  const push = (id, via) => {
    const condition = byId.get(id);
    if (!condition) return;
    if (out.some((entry) => entry.condition.conditionId === id)) return;
    out.push({ condition, via });
  };
  if (node.conditionId) push(node.conditionId, "this occurrence evaluates it");
  for (const condition of conditions) {
    if (condition.nodeId === node.id) push(condition.conditionId, "this occurrence evaluates it");
  }
  for (const condition of conditions) {
    if (condition.postNodeId === node.id) push(condition.conditionId, "this occurrence re-evaluates it");
  }
  const action = (record.actions ?? []).find((a) => a.nodeId === node.id || a.actionId === node.actionId);
  if (action) {
    if (action.postconditionId) {
      push(action.postconditionId, "the action at this occurrence is there to make it");
    }
    for (const id of action.conditionIds ?? []) {
      push(id, "an alternative action at this occurrence is guarded by it");
    }
  }
  return out;
}
