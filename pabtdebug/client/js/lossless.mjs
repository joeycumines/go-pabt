/*
   Reading a record without losing a digit.

   Every unsigned value in this protocol is a uint64 somewhere in its life: a
   sequence, a generation, a plan revision, a tick count, a node count. JSON has
   one number type and JavaScript has one too, and that one is a float64, so
   anything past 2^53 arrives already rounded. By the time a rounding has
   happened the damage is invisible: the client would hold a wrong sequence and
   fetch a different record than the store named, and nothing would say so.

   So integers are read as decimal strings, every one of them, not only the
   unsafe ones. Returning a number for small values and a string for large ones
   would give one field two possible types, and every reader of that field would
   have to handle both and would eventually handle only one. One type, always a
   string, makes the hazard impossible to write rather than merely unlikely.

   Nothing else changes. A float, a boolean, a null, and a string decode exactly
   as JSON.parse decodes them, so the only difference from the built-in parser
   is the spelling of integers.
*/

const DIGITS = "0123456789";

/**
 * parseJSON decodes text, keeping every integer exact.
 *
 * @param {string} text canonical JSON as the server wrote it
 * @returns {unknown} the decoded value, with every integer literal a decimal string
 */
export function parseJSON(text) {
  return JSON.parse(quoteIntegers(text));
}

/**
 * toInt reads a decimal string back as a number, for the few places that need
 * one: a slider's bounds and an arithmetic index.
 *
 * The conversion is exact for anything a browser can act on, and a caller that
 * needs a uint64 must not have come through here: this returns a float64, and
 * the protocol's whole point is that the value did not need to become one.
 *
 * @param {string|number} value
 * @returns {number}
 */
export function toInt(value) {
  if (typeof value === "number") return value;
  const parsed = Number.parseInt(value, 10);
  if (!Number.isFinite(parsed)) {
    throw new TypeError(`not a decimal integer: ${JSON.stringify(value)}`);
  }
  return parsed;
}

/**
 * quoteIntegers rewrites integer literals as JSON strings.
 *
 * The scan is a single pass because a second pass would have to parse to find
 * the numbers, and parsing is exactly the step that loses the digits. It tracks
 * whether it is inside a string so a recorded value containing something that
 * looks like a number is never touched: a condition whose current value is the
 * text "9007199254740993" must decode to that string and not to a number.
 *
 * @param {string} text
 * @returns {string}
 */
function quoteIntegers(text) {
  const out = [];
  let i = 0;
  const n = text.length;
  while (i < n) {
    const c = text[i];
    if (c === '"') {
      // Copy the string literal whole, honouring backslash escapes, so a digit
      // or a minus inside it is never mistaken for the start of a number.
      const start = i;
      i += 1;
      while (i < n) {
        if (text[i] === "\\") {
          i += 2;
          continue;
        }
        if (text[i] === '"') {
          i += 1;
          break;
        }
        i += 1;
      }
      out.push(text.slice(start, i));
      continue;
    }
    if (c === "-" || DIGITS.includes(c)) {
      const token = readNumber(text, i);
      if (token === null) {
        // Not actually a number, which cannot happen in well-formed JSON but is
        // cheaper to pass through than to prove.
        out.push(c);
        i += 1;
        continue;
      }
      out.push(isInteger(token.text) ? `"${token.text}"` : token.text);
      i = token.end;
      continue;
    }
    out.push(c);
    i += 1;
  }
  return out.join("");
}

/**
 * readNumber reads one JSON number token starting at start.
 *
 * @param {string} text
 * @param {number} start
 * @returns {{text: string, end: number}|null}
 */
function readNumber(text, start) {
  let i = start;
  if (text[i] === "-") i += 1;
  const intStart = i;
  while (i < text.length && DIGITS.includes(text[i])) i += 1;
  if (i === intStart) return null;
  // JSON forbids a leading zero on a multi-digit integer, and quoting one would
  // turn a malformed document into a valid one holding the string "01". It is
  // passed through instead, so the native parser is the one that refuses it.
  if (i - intStart > 1 && text[intStart] === "0") return null;
  if (text[i] === ".") {
    i += 1;
    const fracStart = i;
    while (i < text.length && DIGITS.includes(text[i])) i += 1;
    if (i === fracStart) return null;
  }
  if (text[i] === "e" || text[i] === "E") {
    i += 1;
    if (text[i] === "+" || text[i] === "-") i += 1;
    const expStart = i;
    while (i < text.length && DIGITS.includes(text[i])) i += 1;
    if (i === expStart) return null;
  }
  return { text: text.slice(start, i), end: i };
}

/**
 * isInteger reports whether a number token has no fraction and no exponent.
 *
 * A token with either is a different kind of fact from a count, and turning
 * 1.5 into the string "1.5" would claim it is an integer the protocol never
 * wrote. Only plain integer tokens are quoted.
 *
 * @param {string} token
 * @returns {boolean}
 */
function isInteger(token) {
  return !token.includes(".") && !token.includes("e") && !token.includes("E");
}
