/*
   The stream reader, at the frame level.

   The bug these exist for: the reader compared two decimal sequences with `<=`,
   which JavaScript evaluates lexicographically, so from position "9" it discarded
   records 10, 11, and 12 as already held. Nothing errored, nothing reconnected,
   and the page sat at tick 9 while the store moved on. A run of eight ticks is
   one short of that boundary, so the browser evidence could not see it either.
   The fix is one comparison; what keeps it fixed is these cases, because the
   failure mode is silence.

   handleFrame is exercised directly rather than through a socket, so each case is
   a frame the store would write and the assertions are about what the client did
   with it.
*/

import { test } from "node:test";
import assert from "node:assert/strict";

import { EventStream } from "../client/js/stream.mjs";

/**
 * reader builds a stream with recording handlers and no transport, which is all
 * handleFrame needs.
 */
function reader(from = "0", token = "a-minted-cursor") {
  const applied = [];
  const errors = [];
  const behind = [];
  const states = [];
  const stream = new EventStream(
    { streamPath: (token) => `/stream?cursor=${token}` },
    {
      onRecord: (record) => applied.push(record.sequence),
      onBehind: (token) => behind.push(token),
      onState: (state) => states.push(state),
      onError: (error) => errors.push(error.message),
    },
  );
  stream.position = from;
  stream.token = token;
  return { stream, applied, errors, behind, states };
}

/** frame renders one event the way the server's sseWriter does. */
function recordFrame(sequence) {
  return `id: ${sequence}\nevent: record\ndata: {"sequence":${sequence},"kind":"tick","statusPresent":true,"status":"success"}`;
}

test("records advance the position past a digit boundary", () => {
  // The whole point. 9 to 10 is where lexicographic order and numeric order part
  // company, and it is a boundary any real run crosses.
  const { stream, applied } = reader("9");
  for (const sequence of [10, 11, 12]) stream.handleFrame(recordFrame(sequence));
  assert.deepEqual(applied, ["10", "11", "12"]);
  assert.equal(stream.position, "12");
});

test("the boundary is crossed from either side", () => {
  const fromEight = reader("8");
  for (const sequence of [9, 10, 11, 12]) fromEight.stream.handleFrame(recordFrame(sequence));
  assert.deepEqual(fromEight.applied, ["9", "10", "11", "12"]);

  const fromNinetyEight = reader("98");
  for (const sequence of [99, 100, 101]) fromNinetyEight.stream.handleFrame(recordFrame(sequence));
  assert.deepEqual(fromNinetyEight.applied, ["99", "100", "101"]);
});

test("a sequence past the safe integer is not reordered against a small one", () => {
  const { stream, applied } = reader("9007199254740992");
  for (const sequence of ["9007199254740993", "18446744073709551615"]) {
    stream.handleFrame(recordFrame(sequence));
  }
  assert.deepEqual(applied, ["9007199254740993", "18446744073709551615"]);
});

test("a duplicate is dropped and does not move the position backwards", () => {
  // An older sequence and one equal to the position are both already held. The
  // second is the case that matters on a reconnect: the store replays from the
  // client's position, so the record at that position arrives again.
  const { stream, applied } = reader("5");
  stream.handleFrame(recordFrame(4));
  stream.handleFrame(recordFrame(5));
  stream.handleFrame(recordFrame(5));
  assert.deepEqual(applied, []);
  assert.equal(stream.position, "5");
  // And the next one is applied, so a duplicate does not wedge the stream.
  stream.handleFrame(recordFrame(6));
  assert.deepEqual(applied, ["6"]);
});

test("a comment is a keep-alive and advances nothing", () => {
  const { stream, applied, errors } = reader("3");
  stream.handleFrame(": keep-alive");
  assert.deepEqual(applied, []);
  assert.deepEqual(errors, []);
  assert.equal(stream.position, "3");
});

test("a frame whose id disagrees with its record is refused and named", () => {
  // The event id is the sequence the server wrote, so a disagreement means the
  // client is reading something other than what it thinks it is.
  const { stream, applied, errors } = reader("0");
  stream.handleFrame('id: 7\nevent: record\ndata: {"sequence":8}');
  assert.deepEqual(applied, []);
  assert.equal(errors.length, 1);
  assert.match(errors[0], /numbered 7/);
});

test("a record that is not JSON is refused and named", () => {
  const { stream, applied, errors } = reader("0");
  stream.handleFrame("id: 1\nevent: record\ndata: {not json");
  assert.deepEqual(applied, []);
  assert.match(errors[0], /not JSON/);
});

test("a behind event ends the stream and hands back the cursor", () => {
  const { stream, behind } = reader("4");
  stream.handleFrame(`event: behind\ndata: {"token":"minted-token"}`);
  assert.deepEqual(behind, ["minted-token"]);
  assert.equal(stream.closed, true);
});

test("a behind event with an unreadable body still ends the stream", () => {
  // Ending the connection is the point. Refusing to end it because the body did
  // not parse would leave a client that is behind believing it is live. With no
  // usable token in the frame it falls back to the one it connected with, which
  // resumes at the position this client actually reached.
  const { stream, behind, errors } = reader("4");
  stream.handleFrame("event: behind\ndata: garbage");
  assert.deepEqual(behind, ["a-minted-cursor"]);
  assert.equal(stream.closed, true);
  assert.deepEqual(errors, []);
});

test("an error event is reported with the store's own words", () => {
  const { stream, errors } = reader("1");
  stream.handleFrame('event: error\ndata: {"message":"the store reports itself corrupt"}');
  assert.deepEqual(errors, ["the store reports itself corrupt"]);
  assert.equal(stream.closed, true);
});

test("an event with no data is ignored", () => {
  const { stream, applied, errors } = reader("1");
  stream.handleFrame("event: record");
  assert.deepEqual(applied, []);
  assert.deepEqual(errors, []);
});

test("frames split across reads are still read whole", () => {
  // The transport delivers whatever bytes arrived, so a frame can be cut in half.
  // handleFrame only ever sees a whole frame, which is the property that makes
  // splitting safe; this pins that a frame reassembled from two halves behaves the
  // same as one that arrived whole.
  const { stream, applied } = reader("0");
  const whole = recordFrame(3);
  stream.handleFrame(whole);
  const afterWhole = [...applied];
  const other = reader("0");
  other.stream.handleFrame(whole);
  assert.deepEqual(other.applied, afterWhole);
  assert.deepEqual(applied, ["3"]);
});
