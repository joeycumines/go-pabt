/*
   The exact delta, applied and refused.

   The claim under test is that a client holding a base can apply the delta the
   store served and land on the tree the store served, and that every way of being
   wrong is refused rather than absorbed. An absorbed error here is the worst kind
   of client bug: the page shows a tree, it is internally consistent, and it is not
   the history the store holds.

   The fixtures are built in the shape the v2 routes actually serve, including the
   fields the reader has to be right about: a base named four ways, a parent link
   that is not a position, and an ordinal that follows from the order rather than
   from the node.
*/

import { test } from "node:test";
import assert from "node:assert/strict";

import { applyDelta, sameTree, ContinuityFault } from "../client/js/delta.mjs";

/**
 * node builds a stored node. ordinal is a decimal string because that is what the
 * reader produces, and a fixture that used a number would not exercise the
 * conversion the client actually performs.
 */
function node(id, parentId, ordinal, extra = {}) {
  return {
    actionId: null,
    conditionId: null,
    contentHash: `hash-${id}`,
    effectIds: [],
    frame: null,
    id,
    lineage: "",
    name: id,
    ordinal: String(ordinal),
    parentId,
    postNodeId: null,
    status: { lastStatus: "success", statusPresent: true, tickCount: "1", version: "1" },
    type: "ActionNode",
    ...extra,
  };
}

function tree(sequence, planRevision, nodes, digest) {
  return {
    digest,
    eventUid: `event-${sequence}`,
    nodeCount: String(nodes.length),
    nodes,
    planRevision: String(planRevision),
    rootId: "r",
    runKey: { planId: "p", runId: "r", sessionId: "s" },
    sequence: String(sequence),
  };
}

function op(index, kind, payload) {
  return { index: String(index), insert: null, kind, remove: null, reorder: null, replace: null, ...payload };
}

/**
 * baseTree is a plan with a removable grandchild, a renameable child, and three
 * siblings to reorder, so one delta can exercise all four operations.
 */
function baseTree() {
  return tree(1, 2, [
    node("r", null, 0, { name: "Root", type: "GoalRoot" }),
    node("a", "r", 0, { actionId: "act-1", effectIds: ["eff-1"] }),
    node("a1", "a", 0, { conditionId: "cond-1", type: "PreconditionLeaf" }),
    node("b", "r", 1, { type: "ActionRoot" }),
    node("c", "r", 2),
  ], "digest-base");
}

/**
 * targetTree is what baseTree becomes: a1 removed, b1 and c1 inserted, the root's
 * children reordered to c, a, b, and a renamed.
 */
function targetTree() {
  return tree(2, 2, [
    node("r", null, 0, { name: "Root", type: "GoalRoot" }),
    node("c", "r", 0),
    node("a", "r", 1, { name: "a-renamed", actionId: "act-1", effectIds: ["eff-1"] }),
    node("b", "r", 2, { type: "ActionRoot" }),
    node("b1", "b", 0),
    node("c1", "c", 0),
  ], "digest-target");
}

/** fullDelta expresses targetTree as a change from baseTree. */
function fullDelta() {
  return {
    baseDigest: "digest-base",
    baseEventUid: "event-1",
    basePlanRevision: "2",
    baseSequence: "1",
    format: "pabtdebug.exact-delta",
    operations: [
      op(0, "remove", { remove: { nodeId: "a1" } }),
      op(1, "insert", {
        insert: {
          rootId: "b1",
          parentId: "b",
          ordinal: "0",
          nodes: [node("b1", "b", 0)],
        },
      }),
      op(2, "insert", {
        insert: {
          rootId: "c1",
          parentId: "c",
          ordinal: "0",
          nodes: [node("c1", "c", 0)],
        },
      }),
      op(3, "reorder", { reorder: { parentId: "r", childOrder: ["c", "a", "b"] } }),
      op(4, "replace", {
        replace: {
          fields: ["name", "ordinal"],
          node: node("a", "r", 1, { name: "a-renamed", actionId: "act-1", effectIds: ["eff-1"] }),
        },
      }),
    ],
    runKey: { planId: "p", runId: "r", sessionId: "s" },
    sha256: "delta-digest",
    targetDigest: "digest-target",
    targetEventUid: "event-2",
    targetPlanRevision: "2",
    targetSequence: "2",
    version: "1",
  };
}

test("a delta of all four kinds reproduces the tree the store served", () => {
  const applied = applyDelta(baseTree(), fullDelta());
  assert.deepEqual(sameTree(applied, targetTree()), []);
});

test("the nodes come back in the preorder the tree is addressed by", () => {
  const applied = applyDelta(baseTree(), fullDelta());
  assert.deepEqual(applied.nodes.map((n) => n.id), ["r", "c", "c1", "a", "b", "b1"]);
});

test("a move is placed by the reorder and changes nothing else", () => {
  // Reordering the root's children must not alter any node's content, only where
  // it sits. If a move were reported as a replace, a reader would be told the
  // node changed when only its position did.
  const delta = fullDelta();
  const applied = applyDelta(baseTree(), delta);
  const before = baseTree().nodes.find((n) => n.id === "c");
  const after = applied.nodes.find((n) => n.id === "c");
  assert.equal(after.contentHash, before.contentHash);
  assert.equal(after.ordinal, "0");
});

test("a delta naming a base this client does not hold is refused", () => {
  const other = baseTree();
  other.digest = "digest-of-something-else";
  assert.throws(() => applyDelta(other, fullDelta()), ContinuityFault);
});

test("a base that agrees on digest but not on record is refused", () => {
  // The digest is the store's, so a tree that carries it while naming another
  // record is a disagreement, not a base.
  const tampered = baseTree();
  tampered.sequence = "7";
  assert.throws(() => applyDelta(tampered, fullDelta()), ContinuityFault);
});

test("a base at another plan revision is refused", () => {
  const tampered = baseTree();
  tampered.planRevision = "3";
  assert.throws(() => applyDelta(tampered, fullDelta()), ContinuityFault);
});

test("a replace naming a node that is not in the tree is refused", () => {
  const delta = fullDelta();
  delta.operations = [{
    index: "0",
    insert: null,
    kind: "replace",
    remove: null,
    reorder: null,
    replace: { fields: ["name"], node: node("ghost", "r", 9) },
  }];
  assert.throws(() => applyDelta(baseTree(), delta), ContinuityFault);
});

test("an insert naming a parent that is not in the tree is refused", () => {
  const delta = fullDelta();
  delta.operations = [{
    index: "0",
    insert: {
      rootId: "x",
      parentId: "ghost",
      ordinal: "0",
      nodes: [node("x", "ghost", 0)],
    },
    kind: "insert",
    remove: null,
    reorder: null,
    replace: null,
  }];
  assert.throws(() => applyDelta(baseTree(), delta), ContinuityFault);
});

test("a reorder that would leave a child out of the order is refused", () => {
  const delta = fullDelta();
  delta.operations = [{
    index: "0",
    insert: null,
    kind: "reorder",
    remove: null,
    reorder: { parentId: "r", childOrder: ["c", "a"] },
    replace: null,
  }];
  assert.throws(() => applyDelta(baseTree(), delta), ContinuityFault);
});

test("an unknown operation kind is refused rather than skipped", () => {
  const delta = fullDelta();
  delta.operations = [op(0, "transmute", {})];
  assert.throws(() => applyDelta(baseTree(), delta), ContinuityFault);
});

test("an insert of a node already in the tree is refused", () => {
  const delta = fullDelta();
  delta.operations = [{
    index: "0",
    insert: { rootId: "a", parentId: "r", ordinal: "0", nodes: [node("a", "r", 0)] },
    kind: "insert",
    remove: null,
    reorder: null,
    replace: null,
  }];
  assert.throws(() => applyDelta(baseTree(), delta), ContinuityFault);
});

test("an empty delta leaves the base alone", () => {
  const delta = fullDelta();
  delta.operations = [];
  delta.targetDigest = "digest-base";
  const applied = applyDelta(baseTree(), delta);
  assert.deepEqual(applied.nodes.map((n) => n.id), ["r", "a", "a1", "b", "c"]);
});

test("sameTree names what differs rather than only that something does", () => {
  const left = baseTree();
  const right = baseTree();
  right.nodes[1].name = "renamed";
  right.nodes[1].contentHash = "hash-changed";
  const differences = sameTree(left, right);
  assert.ok(differences.includes("a.name"), differences.join(","));
  assert.ok(differences.includes("a.contentHash"), differences.join(","));
  assert.ok(!differences.includes("digest"), differences.join(","));
});

test("sameTree notices a node count that disagrees with the nodes", () => {
  const left = baseTree();
  const right = baseTree();
  right.nodes.pop();
  assert.ok(sameTree(left, right).includes("node length"));
});

test("a delta applied twice is not idempotent, and says so", () => {
  // Applying a delta to a tree that is already the target must not silently
  // succeed: the second application would look for a node that is gone and be
  // refused, which is the correct answer.
  const applied = applyDelta(baseTree(), fullDelta());
  assert.throws(() => applyDelta(applied, fullDelta()), ContinuityFault);
});
