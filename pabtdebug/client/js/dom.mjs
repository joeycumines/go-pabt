/*
   The two primitives every region builds with: the element builder and the
   stored-value readers.

   They live here, and not in detail.mjs, because the causality lanes need them
   too. The lanes are a record-level view wired from app.mjs beside
   renderDetail, and tying them to the occurrence panel for el would be a
   dependency with no reason behind it; lifting the primitives to a leaf keeps
   every module pointing downward. This file imports nothing, which is what
   keeps the graph acyclic: detail.mjs and causality.mjs both sit above it, and
   render.mjs keeps getting el through detail.mjs's re-export, so no existing
   import moves.

   The rule the value readers exist to enforce is the one detail.mjs states in
   full: an absent value renders as unavailable with a reason, never as an
   empty string a reader would take for a value that was there and hidden. A
   causality link is application text like any other, so it reaches the page
   through the same path — textContent, never markup.
*/

/**
 * el builds an element. Text is set through textContent, so no value in a
 * record can become markup.
 *
 * @param {string} tag
 * @param {Record<string, any>} [props] attributes; `text` sets textContent
 * @param {Array<Node>} [children]
 * @returns {HTMLElement}
 */
export function el(tag, props = {}, children = []) {
  const node = document.createElement(tag);
  for (const [name, value] of Object.entries(props)) {
    if (value === undefined || value === null) continue;
    if (name === "text") {
      node.textContent = String(value);
    } else if (name === "class") {
      node.className = String(value);
    } else {
      node.setAttribute(name, String(value));
    }
  }
  for (const child of children) node.appendChild(child);
  return node;
}

/**
 * availabilityOf maps a stored value's visibility onto how a panel must show
 * it. An unknown value is unavailable, not empty: rendering nothing would let a
 * reader assume a value was there and simply not displayed.
 *
 * @param {any} value
 * @returns {"available"|"unavailable"|"redacted"|"error"}
 */
export function availabilityOf(value) {
  switch (value.Visibility) {
    case "exact":
    case "semantic":
      return "available";
    case "redacted":
      return "redacted";
    case "error":
      return "error";
    default:
      return "unavailable";
  }
}

/**
 * displayOf returns the text a value is shown as.
 *
 * @param {any} value
 * @returns {string}
 */
export function displayOf(value) {
  if (value.Visibility === "error") {
    return value.Error ? `unreadable: ${value.Error}` : "unreadable";
  }
  if (value.Visibility === "unknown") {
    return value.Error && value.Error !== "undeclared"
      ? `unavailable — ${value.Error}`
      : "unavailable";
  }
  if (value.Display) return value.Display;
  return value.ReflectType ? `(a ${value.ReflectType} with no display form)` : "(no display form)";
}
