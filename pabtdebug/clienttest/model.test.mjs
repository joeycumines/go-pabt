/*
   The client's view of a run.

   The property that matters most here is that a tree is addressed by occurrence
   identity and by each node's own parent link, never by where a node happens to
   sit in the array. UI-004's worker read the children of a node as the entries
   that followed it, so a node not stored contiguously with its subtree was drawn
   under whatever preceded it. The array order in the fixtures below is therefore
   deliberately not preorder: if this module were reading positions, these tests
   would fail rather than quietly agree.
*/

import { test } from "node:test";
import assert from "node:assert/strict";

import { Model, compareSeq, governingConditions, indexTree, pathTo } from "../client/js/model.mjs";

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

/** scrambled is the same tree as ordered, stored in an order that is not preorder. */
const ordered = [
  node("r", null, 0, { type: "GoalRoot" }),
  node("a", "r", 0),
  node("a1", "a", 0),
  node("a2", "a", 1),
  node("b", "r", 1),
];
const scrambled = [ordered[0], ordered[3], ordered[1], ordered[4], ordered[2]];

test("children come from the parent link, not from where a node sits", () => {
  const index = indexTree({ nodes: scrambled, rootId: "r" });
  assert.deepEqual(index.children.get("r"), ["a", "b"]);
  assert.deepEqual(index.children.get("a"), ["a1", "a2"]);
  assert.deepEqual(index.children.get(null), ["r"]);
});

test("siblings are ordered by the ordinal, not by the array", () => {
  const index = indexTree({
    nodes: [
      node("r", null, 0, { type: "GoalRoot" }),
      node("second", "r", 1),
      node("first", "r", 0),
    ],
    rootId: "r",
  });
  assert.deepEqual(index.children.get("r"), ["first", "second"]);
});

test("a rootId the store did not write is recovered from the parent links", () => {
  const index = indexTree({ nodes: scrambled, rootId: "not-in-the-tree" });
  assert.equal(index.rootId, "r");
  assert.deepEqual(index.children.get(null), ["r"]);
});

test("an empty tree indexes to nothing rather than throwing", () => {
  const index = indexTree({ nodes: [], rootId: "" });
  assert.equal(index.nodes.size, 0);
  assert.deepEqual(index.children.get(null), undefined);
  assert.equal(indexTree(null).nodes.size, 0);
});

test("the path to an occurrence is root first", () => {
  const index = indexTree({ nodes: scrambled, rootId: "r" });
  assert.deepEqual(pathTo(index, "a2"), ["r", "a", "a2"]);
  assert.deepEqual(pathTo(index, "r"), ["r"]);
});

test("the path to an occurrence that is not there is empty, not half built", () => {
  const index = indexTree({ nodes: scrambled, rootId: "r" });
  assert.deepEqual(pathTo(index, "ghost"), []);
});

test("a parent cycle cannot make the path loop forever", () => {
  const index = indexTree({
    nodes: [node("a", "b", 0), node("b", "a", 0)],
    rootId: "a",
  });
  const path = pathTo(index, "a");
  assert.ok(path.length <= index.nodes.size + 1, `path length ${path.length}`);
});

test("sequences order by decimal, not by float, past the safe integer", () => {
  // As floats these two are the same number, and 9 would sort after 10.
  assert.equal(compareSeq("9", "10"), -1);
  assert.equal(compareSeq("18446744073709551615", "18446744073709551614"), 1);
  assert.equal(compareSeq("18446744073709551615", "18446744073709551616"), -1);
  assert.equal(compareSeq("7", "7"), 0);
});

test("the model finds the highest sequence it holds", () => {
  const model = new Model();
  for (const sequence of ["3", "18446744073709551615", "10", "9"]) {
    model.putRecord({ sequence, kind: "tick", statusPresent: true, status: "success" });
  }
  assert.equal(model.lastSequence(), "18446744073709551615");
  assert.deepEqual(model.sequences(), ["3", "9", "10", "18446744073709551615"]);
});

test("the model reports a gap but not a duplicate", () => {
  const model = new Model();
  model.putRecord({ sequence: "1", kind: "tick", statusPresent: true, status: "success" });
  model.putRecord({ sequence: "2", kind: "tick", statusPresent: true, status: "success" });
  assert.equal(model.isGap("3"), false);
  assert.equal(model.isGap("2"), false);
  assert.equal(model.isGap("1"), false);
  model.putRecord({ sequence: "9", kind: "tick", statusPresent: true, status: "success" });
  assert.equal(model.isGap("12"), true);
});

test("a record with no tree leaves the sequence absent from the trees", () => {
  // A panicked tick commits a record and no tree. Reporting a tree for it would
  // be inventing one.
  const model = new Model();
  model.putRecord({ sequence: "1", kind: "panicked", statusPresent: false, tree: null });
  assert.equal(model.record("1").kind, "panicked");
  assert.equal(model.tree("1"), null);
});

test("the conditions governing an occurrence come back with why each one does", () => {
  const record = {
    conditions: [
      { conditionId: "cond-post", key: { Display: '"x"' } },
      { conditionId: "cond-own", key: { Display: '"y"' } },
      { conditionId: "cond-guard", key: { Display: '"z"' } },
      { conditionId: "cond-elsewhere", key: { Display: '"w"' } },
    ],
    actions: [
      {
        actionId: "act-1",
        nodeId: "action-node",
        postconditionId: "cond-post",
        conditionIds: ["cond-guard", "cond-post"],
      },
    ],
  };
  const governing = governingConditions(record, { id: "action-node", actionId: "act-1" });
  assert.deepEqual(governing.map((entry) => entry.condition.conditionId), [
    "cond-post",
    "cond-guard",
  ]);
  assert.match(governing[0].via, /there to make it/);
  assert.match(governing[1].via, /guarded by it/);
});

test("a condition is not listed twice when two relations name it", () => {
  // The postcondition is also the action's own guard in this plan. One condition
  // with two reasons is one row with the first reason, not two rows.
  const record = {
    conditions: [{ conditionId: "cond-1" }, { conditionId: "cond-2" }],
    actions: [{ actionId: "act-1", nodeId: "n", postconditionId: "cond-1", conditionIds: ["cond-1", "cond-2"] }],
  };
  const governing = governingConditions(record, { id: "n" });
  assert.deepEqual(governing.map((entry) => entry.condition.conditionId), ["cond-1", "cond-2"]);
});

test("a node that evaluates a condition lists it", () => {
  const record = {
    conditions: [
      { conditionId: "cond-1", nodeId: "n" },
      { conditionId: "cond-2", postNodeId: "n" },
    ],
    actions: [],
  };
  const governing = governingConditions(record, { id: "n", conditionId: "cond-1" });
  assert.deepEqual(governing.map((entry) => entry.condition.conditionId), ["cond-1", "cond-2"]);
});

test("a structural node is governed by nothing, and says so with nothing", () => {
  const record = { conditions: [{ conditionId: "cond-1", nodeId: "other" }], actions: [] };
  assert.deepEqual(governingConditions(record, { id: "n" }), []);
});

test("the model refuses a store that is not the one it was reading", () => {
  const model = new Model();
  model.adopt({
    storeId: "store-1",
    generation: 1,
    run: { planId: "p", runId: "r", sessionId: "s" },
    runState: "active",
    recovery: "ready",
    head: { HeadSequence: "4", HeadTreeSequence: "4" },
  });
  assert.equal(model.head, "4");
  assert.throws(() => model.adopt({ storeId: "store-2", head: { HeadSequence: "0" } }), /store-2/);
});

test("a continuity note is kept once per tick, newest reason winning", () => {
  const model = new Model();
  model.note("3", "first reason");
  model.note("3", "second reason");
  assert.equal(model.continuity.length, 1);
  assert.equal(model.continuity[0].reason, "second reason");
});
