/*
   The integer reader, against the cases that would actually lose a digit.

   The property under test is not "parses JSON". It is that no integer the store
   wrote is ever rounded, and that a digit which is part of a recorded string is
   never mistaken for one. A reader that got either wrong would look perfectly
   correct on a fixture of small numbers, which is why every case here is either
   past 2^53 or inside a string.
*/

import { test } from "node:test";
import assert from "node:assert/strict";

import { parseJSON, toInt } from "../client/js/lossless.mjs";

test("a sequence past the safe integer survives exactly", () => {
  const parsed = parseJSON('{"sequence":18446744073709551615}');
  assert.equal(parsed.sequence, "18446744073709551615");
});

test("two values that a float64 would merge stay distinct", () => {
  // These two are adjacent uint64 values. Through a float64 they are the same
  // number, so a client holding both would fetch one record for two sequences.
  const parsed = parseJSON('{"a":9007199254740993,"b":9007199254740992}');
  assert.equal(parsed.a, "9007199254740993");
  assert.equal(parsed.b, "9007199254740992");
  assert.notEqual(parsed.a, parsed.b);
});

test("every integer arrives as a decimal string, not only the unsafe ones", () => {
  // One type per field is the point. A field that is a number below 2^53 and a
  // string above it would force every reader to handle both.
  const parsed = parseJSON('{"small":7,"zero":0,"big":9007199254740992}');
  for (const [key, value] of Object.entries(parsed)) {
    assert.equal(typeof value, "string", `${key} is ${typeof value}`);
  }
  assert.deepEqual(parsed, { small: "7", zero: "0", big: "9007199254740992" });
});

test("a digit inside a recorded value is never rewritten", () => {
  // A condition's current value can be the text of a large number. Reading it as
  // a number would change what the planner said it read.
  const parsed = parseJSON('{"Display":"\\"18446744073709551615\\"","note":"tick 12 of 9007199254740993"}');
  assert.equal(parsed.Display, '"18446744073709551615"');
  assert.equal(parsed.note, "tick 12 of 9007199254740993");
});

test("escapes inside strings do not end the string early", () => {
  const parsed = parseJSON('{"a":"quote \\" then 99999999999999999999 then \\\\ end","b":"2"}');
  assert.equal(parsed.a, 'quote " then 99999999999999999999 then \\ end');
  assert.equal(parsed.b, "2");
});

test("a float and an exponent are left as numbers", () => {
  // 1.5 is not a count. Quoting it would claim the protocol wrote an integer.
  const parsed = parseJSON('{"f":1.5,"e":1e3,"neg":-2.5}');
  assert.equal(parsed.f, 1.5);
  assert.equal(parsed.e, 1000);
  assert.equal(parsed.neg, -2.5);
});

test("a negative integer is a string and keeps its sign", () => {
  assert.deepEqual(parseJSON('{"n":-9007199254740993}'), { n: "-9007199254740993" });
});

test("nested structures and arrays are walked", () => {
  const parsed = parseJSON('{"nodes":[{"ordinal":18446744073709551615},{"ordinal":1}],"deep":{"a":{"b":2}}}');
  assert.equal(parsed.nodes[0].ordinal, "18446744073709551615");
  assert.equal(parsed.nodes[1].ordinal, "1");
  assert.equal(parsed.deep.a.b, "2");
});

test("the exact boundary is not lost either way", () => {
  const parsed = parseJSON('{"max":9007199254740991,"over":9007199254740992}');
  assert.equal(parsed.max, "9007199254740991");
  assert.equal(parsed.over, "9007199254740992");
});

test("toInt reads a decimal string back for a slider bound", () => {
  assert.equal(toInt("18446744073709551615"), 18446744073709551615);
  assert.equal(toInt("7"), 7);
  assert.equal(toInt(7), 7);
  assert.throws(() => toInt("seven"), TypeError);
});

test("malformed input is refused rather than half read", () => {
  assert.throws(() => parseJSON("{"), SyntaxError);
  assert.throws(() => parseJSON('{"a":}'), SyntaxError);
});
