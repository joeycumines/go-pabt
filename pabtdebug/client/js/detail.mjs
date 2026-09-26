/*
   The panel that answers "why is this occurrence what it is".

   The four evidence fields are rendered as four separately labelled blocks, and
   that is the whole point of the panel. A condition carries what the application
   declared it expected, what the planner actually read, what an observer saw
   afterwards, and an explanation for whichever is missing. Put them in one row and
   a reader takes the expected value for the current one, or the other way round,
   and cannot tell that the record deliberately kept them apart.

   The rule the panel exists to enforce: an expected value that was never declared
   renders as unavailable, with the reason. It is never filled in from the
   condition's key. A key names a variable, not a requirement, and an inferred
   expectation would be a claim nothing supports - so the key is shown in its own
   labelled field, as the subject of the condition, and nowhere else.

   Every value here is application text. It reaches the page through textContent
   and never through markup, so a recorded value containing a tag is displayed as
   the characters it is rather than executed.
*/

/**
 * el builds an element. Text is set through textContent, so no value in a record
 * can become markup.
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
 * renderDetail fills the detail region for one occurrence.
 *
 * @param {HTMLElement} host
 * @param {any} model
 * @param {any} record the record the tree came from
 * @param {any} node the selected node, or null
 * @param {Array<{condition: any, via: string}>} governing
 */
export function renderDetail(host, model, record, node, governing) {
  host.replaceChildren();
  if (!record) {
    host.appendChild(el("p", { class: "detail-empty", text: "No tick selected." }));
    return;
  }
  if (!node) {
    host.appendChild(el("p", { class: "detail-empty", text: "Select an occurrence in the tree." }));
    appendRecordSummary(host, record);
    return;
  }
  host.appendChild(occurrenceHeader(node, record));
  host.appendChild(recordOutcome(record));

  if (governing.length === 0) {
    host.appendChild(field("Preconditions", [
      el("p", {
        class: "detail-empty",
        text: node.actionId || node.conditionId || node.postNodeId
          ? "This occurrence is named in the record, but the record names no condition for it."
          : "This occurrence is a structural node: it governs nothing and is governed by nothing.",
      }),
    ]));
  }
  for (const entry of governing) {
    host.appendChild(conditionBlock(entry));
  }
  host.appendChild(actionBlock(record, node));
  host.appendChild(relationsBlock(record, governing));
  host.appendChild(continuityBlock(model));
}

/**
 * occurrenceHeader names the occurrence and its own recorded facts.
 *
 * @param {any} node
 * @param {any} record
 * @returns {HTMLElement}
 */
function occurrenceHeader(node, record) {
  const status = node.status ?? {};
  const meta = el("div", { class: "occ-meta" });
  const add = (label, value) => {
    if (value === undefined || value === null || value === "") return;
    meta.appendChild(el("span", {}, [el("span", { text: `${label} ` }), el("b", { text: String(value) })]));
  };
  // An absent status is shown as absent. Printing nothing would read as a status
  // the recorder forgot, and printing a number would invent one.
  add("status", status.statusPresent ? status.lastStatus || "(present)" : "not evaluated");
  add("ticks", status.tickCount);
  add("revision", status.version);
  add("ordinal", node.ordinal);
  add("sequence", record.sequence);
  if (node.frame) {
    const where = node.frame.file
      ? `${basename(node.frame.file)}:${node.frame.line}`
      : "";
    add("frame", [where, node.frame.function ? ` ${node.frame.function}` : ""].join("").trim());
  }
  const head = el("div", { class: "occ-head" }, [
    el("div", { class: "occ-name", text: node.name || node.type }),
    el("div", { class: "occ-meta" }, [el("span", { text: node.type })]),
    meta,
  ]);
  head.appendChild(el("div", { class: "occ-meta" }, [
    el("span", {}, [el("span", { text: "id " }), el("b", { text: node.id })]),
  ]));
  if (node.lineage) {
    head.appendChild(el("div", { class: "occ-meta" }, [
      el("span", {}, [el("span", { text: "lineage " }), el("b", { text: node.lineage })]),
    ]));
  }
  return head;
}

/**
 * recordOutcome states what the tick itself concluded, separately from any one
 * node, so a failure is visible without hunting for the node that carries it.
 *
 * @param {any} record
 * @returns {HTMLElement}
 */
function recordOutcome(record) {
  const body = el("div", { class: "evidence" });
  const row = (label, value, availability) =>
    el("div", { class: "ev" }, [
      el("span", { class: "ev-label", text: label }),
      el("span", { class: "ev-value", "data-availability": availability, text: value }),
    ]);
  body.appendChild(row("tick", record.kind, "available"));
  body.appendChild(row(
    "status",
    record.statusPresent ? record.status || "(present)" : "not evaluated",
    record.statusPresent ? "available" : "unavailable",
  ));
  for (const [label, section] of [
    ["planner error", record.plannerError],
    ["panic", record.panic],
    ["capture error", record.captureError],
  ]) {
    if (!section) continue;
    body.appendChild(row(label, `${section.type}: ${section.message}`, "error"));
  }
  return field("This tick", [body]);
}

/**
 * appendRecordSummary describes a tick when no occurrence is selected, so the
 * region is never blank while a reader is deciding what to click.
 *
 * @param {HTMLElement} host
 * @param {any} record
 */
function appendRecordSummary(host, record) {
  const body = el("div", { class: "evidence" });
  const row = (label, value) =>
    el("div", { class: "ev" }, [
      el("span", { class: "ev-label", text: label }),
      el("span", { class: "ev-value", "data-availability": "available", text: value }),
    ]);
  body.appendChild(row("sequence", record.sequence));
  body.appendChild(row("kind", record.kind));
  body.appendChild(row("status", record.statusPresent ? record.status : "not evaluated"));
  body.appendChild(row("conditions", String((record.conditions ?? []).length)));
  body.appendChild(row("actions", String((record.actions ?? []).length)));
  body.appendChild(row("plan revision", record.revision ? record.revision.planRevision : ""));
  body.appendChild(row("digest", record.recordSha256));
  host.appendChild(field("Tick summary", [body]));
}

/**
 * conditionBlock renders one condition and its four evidence fields.
 *
 * @param {{condition: any, via: string}} entry
 * @returns {HTMLElement}
 */
function conditionBlock(entry) {
  const condition = entry.condition;
  const body = el("div", { class: "evidence" });

  // The variable, labelled as the subject. It is deliberately not the expected
  // value and never stands in for one.
  body.appendChild(evidenceRow("variable", condition.key, {
    missing: "the record carries no key for this condition",
  }));
  body.appendChild(evidenceRow("expected", condition.expected, {
    missing: describeMissingExpected(condition),
  }));
  body.appendChild(evidenceRow("current", condition.current, {
    missing: "the planner did not evaluate this condition in this tick",
  }));
  body.appendChild(evidenceRow("observed", condition.observed ?? null, {
    missing: "no post-action observer read this condition",
  }));
  body.appendChild(uncertaintyRow(condition.uncertainty));

  const children = [
    el("p", { class: "ev-note", text: `Governs this occurrence because ${entry.via}.` }),
    el("p", { class: "ev-note", text: `Truth this tick: ${condition.truth ? condition.truth : "not evaluated"}.` }),
    body,
  ];
  return field("Precondition", children, condition.conditionId);
}

/**
 * describeMissingExpected says why an expected value is absent, in the record's
 * own terms, and never offers a substitute.
 *
 * @param {any} condition
 * @returns {string}
 */
function describeMissingExpected(condition) {
  const uncertainty = condition.uncertainty ?? {};
  if (uncertainty.state && uncertainty.state !== "none") {
    return uncertainty.message || `unavailable: ${uncertainty.state}`;
  }
  return "unavailable — the application declares no expected value for this condition";
}

/**
 * evidenceRow renders one value field with its own label.
 *
 * @param {string} label
 * @param {any} value a StoredValue, or null when the field is absent
 * @param {{missing: string}} options
 * @returns {HTMLElement}
 */
function evidenceRow(label, value, options) {
  if (!value) {
    return el("div", { class: "ev" }, [
      el("span", { class: "ev-label", text: label }),
      el("span", {
        class: "ev-value",
        "data-availability": "unavailable",
        text: options.missing,
      }),
    ]);
  }
  const availability = availabilityOf(value);
  const row = el("div", { class: "ev" }, [
    el("span", { class: "ev-label", text: label }),
    el("span", { class: "ev-value", "data-availability": availability, text: displayOf(value) }),
  ]);
  // The mechanism that removed a value is shown, so a reader can tell a secret
  // the application declared from a name the recorder recognised.
  if (value.Redaction) {
    row.lastChild.appendChild(el("span", {
      class: "ev-note",
      text: ` — redacted, ${value.Redaction.replace(/_/g, " ")}`,
    }));
  }
  return row;
}

/**
 * uncertaintyRow renders the record's own explanation of what is missing.
 *
 * @param {any} uncertainty
 * @returns {HTMLElement}
 */
function uncertaintyRow(uncertainty) {
  const state = uncertainty && uncertainty.state ? uncertainty.state : "none";
  const text = state === "none"
    ? "every requested value was available"
    : [state, uncertainty.code, uncertainty.message].filter(Boolean).join(" — ");
  return el("div", { class: "ev" }, [
    el("span", { class: "ev-label", text: "uncertainty" }),
    el("span", {
      class: "ev-value",
      "data-availability": state === "none" ? "available" : "unavailable",
      text,
    }),
  ]);
}

/**
 * availabilityOf maps a stored value's visibility onto how the panel must show
 * it. An unknown value is unavailable, not empty: rendering nothing would let a
 * reader assume a value was there and simply not displayed.
 *
 * @param {any} value
 * @returns {"available"|"unavailable"|"redacted"|"error"}
 */
function availabilityOf(value) {
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
function displayOf(value) {
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

/**
 * actionBlock shows the effects the action at this occurrence declares.
 *
 * @param {any} record
 * @param {any} node
 * @returns {HTMLElement}
 */
function actionBlock(record, node) {
  const actions = record.actions ?? [];
  const action = actions.find((a) => a.nodeId === node.id) ?? actions.find((a) => a.actionId === node.actionId);
  if (!action) return el("div");
  const body = el("div", { class: "evidence" });
  body.appendChild(evidenceRow("action", null, { missing: action.name || action.actionId }));
  const effects = (record.effects ?? []).filter((e) => e.actionId === action.actionId);
  if (effects.length === 0) {
    body.appendChild(el("div", { class: "ev" }, [
      el("span", { class: "ev-label", text: "effects" }),
      el("span", { class: "ev-value", "data-availability": "unavailable", text: "the action declares none" }),
    ]));
  }
  for (const effect of effects) {
    body.appendChild(evidenceRow("effect key", effect.key, { missing: "no key recorded" }));
    body.appendChild(evidenceRow("effect value", effect.value, {
      missing: effect.uncertainty && effect.uncertainty.message
        ? effect.uncertainty.message
        : "the action declared this effect without a value",
    }));
  }
  const children = [body];
  if (action.postconditionId) {
    children.push(el("p", { class: "ev-note", text: `Its postcondition is ${action.postconditionId}.` }));
  }
  return field("Action at this occurrence", children, action.actionId);
}

/**
 * relationsBlock shows what a transition verifier concluded, including that it
 * was not authoritative.
 *
 * A verifier that reports a relation under a live state view is reporting, not
 * proving: two live reads can be different worlds, so a claim spanning them is
 * not evidence of causation. The authority is shown next to the claim rather than
 * left for the reader to infer from the absence of a warning.
 *
 * @param {any} record
 * @param {Array<{condition: any}>} governing
 * @returns {HTMLElement}
 */
function relationsBlock(record, governing) {
  const relations = record.relations ?? [];
  if (relations.length === 0) return el("div");
  const wanted = new Set(governing.map((entry) => entry.condition.conditionId));
  const about = wanted.size > 0
    ? relations.filter((r) => wanted.has(r.postconditionId))
    : relations;
  if (about.length === 0) return el("div");
  const body = el("div", { class: "evidence" });
  for (const relation of about) {
    const row = el("div", { class: "ev" }, [
      el("span", { class: "ev-label", text: relation.effectId ? shortId(relation.effectId) : "relation" }),
      el("span", { class: "ev-value", "data-availability": "available" }, [
        document.createTextNode(`${relation.result} by ${relation.method} `),
        el("span", {
          class: "tag",
          "data-authoritative": String(Boolean(relation.authoritative)),
          text: relation.authority || (relation.authoritative ? "authoritative" : "not authoritative"),
        }),
      ]),
    ]);
    if (relation.error) {
      row.lastChild.appendChild(el("span", {
        class: "ev-note",
        text: ` — ${relation.error.type}: ${relation.error.message}`,
      }));
    }
    body.appendChild(row);
  }
  return field("Transition verifier", [body], `coherence ${record.revision ? record.revision.coherence : "unknown"}`);
}

/**
 * continuityBlock reports any disagreement between a delta and the tree the
 * store served, because a client that silently repaired one would be hiding the
 * only signal that history and transport had parted company.
 *
 * @param {any} model
 * @returns {HTMLElement}
 */
function continuityBlock(model) {
  if (!model.continuity || model.continuity.length === 0) return el("div");
  const body = el("div", { class: "evidence" });
  for (const entry of model.continuity) {
    body.appendChild(el("div", { class: "ev" }, [
      el("span", { class: "ev-label", text: `tick ${entry.sequence}` }),
      el("span", { class: "ev-value", "data-availability": "error", text: entry.reason }),
    ]));
  }
  return field("Continuity", [body]);
}

/**
 * field wraps a titled block, which is how every section in this panel is
 * shaped, so the four evidence fields read as four things rather than as a table
 * a reader has to decode.
 *
 * @param {string} title
 * @param {Array<Node>} children
 * @param {string} [aside]
 * @returns {HTMLElement}
 */
function field(title, children, aside) {
  const heading = el("h3", {}, [el("span", { text: title })]);
  if (aside) heading.appendChild(el("span", { class: "muted", text: aside }));
  return el("section", { class: "field" }, [heading, ...children]);
}

/**
 * shortId trims an occurrence id to its distinguishing tail, because the full id
 * is a plan-scoped prefix repeated on every row and crowds out the claim.
 *
 * @param {string} id
 * @returns {string}
 */
function shortId(id) {
  const parts = String(id).split("-");
  return parts.length > 1 ? parts[parts.length - 1] : String(id);
}

/**
 * basename reduces a recorded source path to its file, so a frame line fits
 * beside the rest of the row at 700 pixels.
 *
 * @param {string} path
 * @returns {string}
 */
function basename(path) {
  const parts = String(path).split("/");
  return parts[parts.length - 1] || String(path);
}
