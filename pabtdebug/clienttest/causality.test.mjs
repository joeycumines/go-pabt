/*
   The causal lanes' substance: the split, the cross-record links, and the
   refusal shape.

   The lanes render DOM, and the clienttest suite has no DOM — the same boundary
   model.test.mjs draws. What is tested here is everything the rendering claims:
   which events are caused, which record holds the command behind one, what the
   world made of a submission, and that a refusal carries a reason and no
   events. The buttons are thin; the resolution they display is the surface
   with the acceptance criteria on it.

   The fixtures are wire-shaped: camelCase envelope fields (the StoredCausality*
   json tags), PascalCase StoredValue fields (no tags), and every unsigned
   value a decimal string (lossless.mjs quotes every integer literal). A
   fixture that drifted to Go casing would make every assertion here vacuous,
   which is the failure mode the first draft of causality.mjs actually had.
*/

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  causalityOf,
  causalityStats,
  causedEvents,
  indexCommands,
  isAbandoned,
  isApplied,
  linkEvent,
  linkSubmission,
} from "../client/js/causality.mjs";
import { Model } from "../client/js/model.mjs";

/** value is the exact-visibility StoredValue the routes serve for application text. */
function value(display) {
  return { Visibility: "exact", Display: display };
}

function submission(commandId, key, kind, tick) {
  return {
    commandId: value(commandId),
    idempotencyKey: value(key),
    kind: value(kind),
    tractor: value("TUG-1"),
    planTickSeq: String(tick),
    actionId: "1",
    generation: "1",
    expect: { worldSeq: "9", slotVersion: "0", laneVersion: "0", tagVersion: "0" },
  };
}

function settlement(commandId, key, kind, outcome, events = []) {
  return {
    commandId: value(commandId),
    idempotencyKey: value(key),
    kind: value(kind),
    outcome: value(outcome),
    worldSeq: "10",
    worldRevision: value("rev-10"),
    commandSeq: "4",
    events,
  };
}

function worldEvent(kind, causeCommand) {
  const ev = { kind: value(kind), worldSeq: "10", worldRevision: value("rev-10") };
  // The wire omits an unnamed entity entirely: optionalStoredValue records an
  // absent link as no key at all, never an empty value.
  if (causeCommand) ev.causeCommand = value(causeCommand);
  return ev;
}

/**
 * heldRecords builds the two-record shape DD-37 forces: the submission rides
 * the tick's own record, the settlement that answered it rides the next one,
 * and the caused event rides inside that settlement.
 */
function heldRecords() {
  const first = {
    sequence: "1",
    kind: "tick",
    causality: {
      generation: "1",
      planTickSeq: "3",
      frameWorldSeq: "9",
      submitted: [submission("cmd-move", "plan/g1/t3/move", "MoveTo", 3)],
      settled: [],
      exogenous: [],
    },
  };
  const second = {
    sequence: "2",
    kind: "tick",
    causality: {
      generation: "1",
      planTickSeq: "4",
      frameWorldSeq: "10",
      submitted: [],
      settled: [settlement("cmd-move", "plan/g1/t3/move", "MoveTo", "Applied", [
        worldEvent("Moved", "cmd-move"),
      ])],
      exogenous: [worldEvent("LaneClosed", null)],
    },
  };
  const model = new Model();
  model.putRecord(first);
  model.putRecord(second);
  return { model, first, second };
}

test("a record with no envelope reads as none", () => {
  assert.equal(causalityOf({ sequence: "1", kind: "tick" }), null);
  assert.equal(causalityOf(null), null);
  const envelope = { submitted: [] };
  assert.equal(causalityOf({ causality: envelope }), envelope);
});

test("the outcome word decides applied", () => {
  assert.equal(isApplied(settlement("c", "k", "MoveTo", "Applied")), true);
  assert.equal(isApplied(settlement("c", "k", "MoveTo", "Refused")), false);
  assert.equal(isApplied(settlement("c", "k", "MoveTo", "Duplicate")), false);
  assert.equal(isApplied(null), false);
});

test("an abandoned settlement is neither applied nor refused", () => {
  // The loop withdrew it without reducing, so it carries no receipt and no
  // outcome word — the lane must say nothing was decided, not guess a refusal.
  const st = { commandId: value("c"), idempotencyKey: value("k"), abandoned: { commandId: value("c"), reason: value("shutdown") } };
  assert.equal(isAbandoned(st), true);
  assert.equal(isApplied(st), false);
});

test("caused events come from inside the settlements, exogenous from the buffer", () => {
  const { second } = heldRecords();
  const { caused, exogenous } = causedEvents(second.causality);
  assert.deepEqual(caused.map((ev) => ev.kind.Display), ["Moved"]);
  assert.deepEqual(exogenous.map((ev) => ev.kind.Display), ["LaneClosed"]);
});

test("the split reads the cause link, not the buffer that held it", () => {
  // An event the host mis-buffered — a caused event parked in the exogenous
  // list — still lands on the caused side, because the link is the fact and
  // the buffer is only where it was parked.
  const envelope = {
    settled: [],
    exogenous: [worldEvent("Moved", "cmd-x"), worldEvent("LaneClosed", null)],
  };
  const { caused, exogenous } = causedEvents(envelope);
  assert.deepEqual(caused.map((ev) => ev.kind.Display), ["Moved"]);
  assert.deepEqual(exogenous.map((ev) => ev.kind.Display), ["LaneClosed"]);
});

test("an envelope with no events splits to nothing", () => {
  const { caused, exogenous } = causedEvents({ settled: [], exogenous: [] });
  assert.equal(caused.length, 0);
  assert.equal(exogenous.length, 0);
  assert.deepEqual(causedEvents(null), { caused: [], exogenous: [] });
});

test("the index walks every held record and keys by identity", () => {
  const { model } = heldRecords();
  const index = indexCommands(model);
  assert.equal(index.byCommand.get("cmd-move").sequence, "1");
  assert.equal(index.settledByCommand.get("cmd-move").sequence, "2");
  assert.equal(index.byKey.get("plan/g1/t3/move").sequence, "1");
  assert.equal(index.settledByKey.get("plan/g1/t3/move").sequence, "2");
});

test("a world event follows to the command and tick that caused it", () => {
  // The acceptance's central clause, as a reading: the event rides record 2
  // inside the settlement, and its cause resolves to the submission in record
  // 1 at plan tick 3 — across the record boundary DD-37 forces, not within
  // one envelope.
  const { model, second } = heldRecords();
  const index = indexCommands(model);
  const { caused } = causedEvents(second.causality);
  const link = linkEvent(index, caused[0]);
  assert.equal(link.command, "cmd-move");
  assert.equal(link.sequence, "1");
  assert.equal(String(link.submission.planTickSeq), "3");
  assert.equal(link.submission.kind.Display, "MoveTo");
});

test("a cause the client does not hold is reported, not guessed", () => {
  // A reader who joined mid-run holds the settlement but not the submission.
  // The tick is null — the row says the cause is not held rather than
  // displaying a plausible tick nothing in the record supports.
  const model = new Model();
  const { second } = heldRecords();
  model.putRecord(second);
  const index = indexCommands(model);
  const { caused } = causedEvents(second.causality);
  const link = linkEvent(index, caused[0]);
  assert.equal(link.command, "cmd-move"); // the event still names its cause
  assert.equal(link.sequence, null);
  assert.equal(link.submission, null);
});

test("an exogenous event names no cause at all", () => {
  const { model, second } = heldRecords();
  const index = indexCommands(model);
  const { exogenous } = causedEvents(second.causality);
  const link = linkEvent(index, exogenous[0]);
  assert.equal(link.command, null);
  assert.equal(link.sequence, null);
  assert.equal(link.submission, null);
});

test("a submission resolves to the settlement that answered it", () => {
  const { model, first } = heldRecords();
  const index = indexCommands(model);
  const { settlement, sequence } = linkSubmission(index, first.causality.submitted[0]);
  assert.equal(sequence, "2");
  assert.equal(settlement.outcome.Display, "Applied");
  assert.equal(settlement.events.length, 1);
});

test("a command with no settlement anywhere is still in flight", () => {
  // Waiting, not refused: the plan asked and the world has not answered.
  const model = new Model();
  const { first } = heldRecords();
  model.putRecord(first);
  const index = indexCommands(model);
  const { settlement, sequence } = linkSubmission(index, first.causality.submitted[0]);
  assert.equal(settlement, null);
  assert.equal(sequence, null);
});

test("a refusal carries a reason and no world effect", () => {
  // The acceptance's "a rejected command is visible with no world effect":
  // the reason is present, the events list is empty, and the two together
  // are the reading — not a blank a reader has to interpret.
  const refused = settlement("cmd-place", "plan/g1/t5/place", "Place", "Refused");
  refused.reason = value("SlotOccupied");
  refused.holder = value("TUG-2");
  const model = new Model();
  model.putRecord({
    sequence: "1",
    kind: "tick",
    causality: {
      submitted: [submission("cmd-place", "plan/g1/t5/place", "Place", 5)],
      settled: [refused],
      exogenous: [],
    },
  });
  const index = indexCommands(model);
  // The local is `answered`, not `settlement`: the helper the fixture is built
  // with shares that name, and a const of the same name anywhere in this block
  // puts the helper in the temporal dead zone for the whole block — the call
  // above would throw before the destructuring ever ran.
  const { settlement: answered } = linkSubmission(index, model.record("1").causality.submitted[0]);
  assert.equal(isApplied(answered), false);
  assert.equal(isAbandoned(answered), false);
  assert.equal(answered.reason.Display, "SlotOccupied");
  assert.equal(answered.holder.Display, "TUG-2");
  assert.deepEqual(answered.events, []);
  const { caused } = causedEvents(model.record("1").causality);
  assert.equal(caused.length, 0);
});

test("stats count what one envelope holds", () => {
  const { second } = heldRecords();
  const stats = causalityStats(second.causality);
  assert.deepEqual(stats, {
    caused: 1,
    exogenous: 1,
    submitted: 0,
    settled: 1,
    refused: 0,
    abandoned: 0,
  });
  const withRefusal = {
    submitted: [submission("a", "ka", "MoveTo", 1), submission("b", "kb", "Place", 2)],
    settled: [
      settlement("a", "ka", "MoveTo", "Applied"),
      settlement("b", "kb", "Place", "Refused"),
      { commandId: value("c"), idempotencyKey: value("kc"), abandoned: { commandId: value("c"), reason: value("shutdown") } },
    ],
    exogenous: [],
  };
  assert.deepEqual(causalityStats(withRefusal), {
    caused: 0,
    exogenous: 0,
    submitted: 2,
    settled: 3,
    refused: 1,
    abandoned: 1,
  });
});

test("an entry with no identity is skipped, not crashed on", () => {
  // A record imported from an older server, or a hand-built envelope missing a
  // key, must not take the whole lane down — the index walks every held record.
  const model = new Model();
  model.putRecord({
    sequence: "1",
    kind: "tick",
    causality: { submitted: [{ kind: value("MoveTo") }], settled: [{}], exogenous: [] },
  });
  const index = indexCommands(model);
  assert.equal(index.byCommand.size, 0);
  assert.equal(index.settledByKey.size, 0);
});
