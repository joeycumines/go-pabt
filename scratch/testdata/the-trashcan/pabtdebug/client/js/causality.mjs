/*
   The two causal lanes: what the world changed, and what the plan did about it.

   A committed record carries an optional causality envelope — the commands a
   tick submitted, the receipts that settled into it, and the world events the
   host drained (DESIGN.md section 8). The envelope is the only place a reader
   can follow a world event to the plan tick and command that caused it, so the
   client shows it as two lanes rather than burying it in a dump:

     - the WORLD lane lists the selected record's world events, each labelled
       caused or exogenous. A caused event rides inside a settlement — it is
       one of the events the receipt published — and it names the command that
       produced it; an exogenous event rides the exogenous buffer and names no
       cause, because optionalStoredValue records an unnamed entity as an
       absent link rather than an empty value. The absence is the statement.
     - the PLAN lane lists the selected record's own moves: submissions and
       the settlements that answer them. A settlement whose outcome is not
       Applied is a refused command, and it carries no events — that absence
       next to its reason is what makes "a rejected command is visible with no
       world effect" a reading rather than an inference.

   The links resolve across every held record, not within one envelope,
   because the publisher drains causality inside the NEXT record's build: a
   plan-tick command's submission rides the record whose tree it produced, and
   the receipt that answered it rides the record after. A caused event is
   therefore two hops from its submission — event → settlement → submission —
   and a reader who was shown only the hop inside one envelope would be shown
   half a link. When the other half is not held (a client that joined mid-run),
   the row says the cause is not held rather than guessing a tick, because a
   plausible tick nothing in the record supports is exactly the claim this
   client exists to keep out.

   Neither lane traverses a live plan: every row here comes from a record the
   store committed and a route served.

   The wire is camelCase (the StoredCausality* structs carry explicit json
   tags) while the values inside it are PascalCase (StoredValue carries none),
   and every unsigned field arrives a decimal string (lossless.mjs quotes
   every integer literal), so ordering compares strings and never subtracts.
   Every value a row prints is application text read through the same
   displayOf/availabilityOf path the occurrence panel uses, so a redacted or
   unreadable id says so instead of rendering as a blank.
*/

import { el, displayOf, availabilityOf } from "./dom.mjs";

/**
 * causalityOf reads one record's envelope, or null when it carries none. The
 * section is additive and omitempty, so an old record — or a tick that reasoned
 * over nothing — simply has no key.
 *
 * @param {any} record
 * @returns {any|null}
 */
export function causalityOf(record) {
  return record && record.causality ? record.causality : null;
}

/**
 * isApplied reports a settlement the world accepted. The outcome is a
 * StoredValue carrying the receipt's outcome word ("Applied", "Refused",
 * "Duplicate"); anything else — a refusal, an unreadable outcome — is not an
 * application, and a refusal is exactly the case the plan lane must show with
 * no events.
 *
 * @param {any} settlement
 * @returns {boolean}
 */
export function isApplied(settlement) {
  // A withdrawn command carries no receipt and therefore no outcome word at
  // all; reading one from it would be inventing an outcome. The absence is
  // guarded before the word is compared, because an absent value is not a
  // refusal either — isAbandoned names it, and the lane shows it as neither.
  return Boolean(settlement) && Boolean(settlement.outcome) && outcomeWord(settlement.outcome) === "Applied";
}

/**
 * outcomeWord reads the receipt's outcome word out of the StoredValue the wire
 * serves. Display is the value's canonical JSON text — valuecodec derives it
 * from CanonicalEncode, so a string arrives as a quoted literal ('"Applied"'),
 * while an unreadable or redacted value's display is prose that is not that
 * text. Decoding keeps the comparison on the word the receipt recorded rather
 * than on one encoding of it, and a display that will not decode answers null:
 * a value this client cannot read is not an application, and guessing that it
 * is would be the claim the lanes exist to keep out.
 *
 * @param {any} value
 * @returns {string|null}
 */
function outcomeWord(value) {
  try {
    const word = JSON.parse(displayOf(value));
    return typeof word === "string" ? word : null;
  } catch {
    return null;
  }
}

/**
 * isAbandoned reports a settlement the loop withdrew without reducing. It
 * carries no receipt, so it is neither applied nor refused; the world decided
 * nothing, and the lane says so rather than inventing an outcome.
 *
 * @param {any} settlement
 * @returns {boolean}
 */
export function isAbandoned(settlement) {
  return Boolean(settlement) && Boolean(settlement.abandoned);
}

/**
 * causedEvents splits one envelope's events by whether a command produced
 * them. Caused events ride inside the settlements (the receipt's published
 * events); exogenous events ride the exogenous buffer. The split reads the
 * causeCommand link on each event rather than trusting which buffer carried
 * it, so an event the host mis-buffered still lands on the right side of the
 * lane — the link is the fact, the buffer is only where it was parked.
 *
 * @param {any} envelope
 * @returns {{caused: any[], exogenous: any[]}}
 */
export function causedEvents(envelope) {
  const caused = [];
  const exogenous = [];
  const push = (ev) => (ev.causeCommand ? caused : exogenous).push(ev);
  for (const st of envelope?.settled ?? []) {
    for (const ev of st.events ?? []) push(ev);
  }
  for (const ev of envelope?.exogenous ?? []) push(ev);
  return { caused, exogenous };
}

/**
 * indexCommands walks every held record's envelope and indexes the plan's own
 * moves by identity, so a link can resolve across records. Entries are keyed
 * by command id and by idempotency key — never by position — because the
 * envelope's entries link to each other by identity and a reader who followed
 * position would be following an order the store never promised.
 *
 * @param {any} model
 * @returns {{byCommand: Map<string, any>, byKey: Map<string, any>, settledByCommand: Map<string, any>, settledByKey: Map<string, any>}}
 *   each map value is {sequence, submission|settlement}.
 */
export function indexCommands(model) {
  const byCommand = new Map();
  const byKey = new Map();
  const settledByCommand = new Map();
  const settledByKey = new Map();
  for (const sequence of model.sequences()) {
    const envelope = causalityOf(model.record(sequence));
    if (!envelope) continue;
    for (const sub of envelope.submitted ?? []) {
      const id = idOf(sub.commandId);
      if (id) byCommand.set(id, { sequence, submission: sub });
      const key = idOf(sub.idempotencyKey);
      if (key) byKey.set(key, { sequence, submission: sub });
    }
    for (const st of envelope.settled ?? []) {
      const id = idOf(st.commandId);
      if (id) settledByCommand.set(id, { sequence, settlement: st });
      const key = idOf(st.idempotencyKey);
      if (key) settledByKey.set(key, { sequence, settlement: st });
    }
  }
  return { byCommand, byKey, settledByCommand, settledByKey };
}

/**
 * linkEvent answers the acceptance's central question for one world event:
 * which command produced it, at which plan tick, and in which held record
 * that command's submission sits — the hop a reader lands on when they follow
 * the link. When no held record carries the submission, the tick is reported
 * as not held rather than guessed.
 *
 * @param {any} index an indexCommands result
 * @param {any} event one world event
 * @returns {{command: string|null, sequence: string|null, submission: any|null}}
 */
export function linkEvent(index, event) {
  const command = event?.causeCommand ? displayOf(event.causeCommand) : null;
  if (!command) return { command: null, sequence: null, submission: null };
  const hit = index.byCommand.get(command) ?? null;
  return { command, sequence: hit?.sequence ?? null, submission: hit?.submission ?? null };
}

/**
 * linkSubmission answers the other half: what the world made of one command
 * the plan submitted. The settlement that answers a submission rides a later
 * record, so it is resolved through the index, not from the submission's own
 * envelope. A refusal settles with a reason and no events — the absence is
 * the finding — and a command with no settlement anywhere is still in flight.
 *
 * @param {any} index an indexCommands result
 * @param {any} submission one command from a held envelope
 * @returns {{sequence: string|null, settlement: any|null}}
 */
export function linkSubmission(index, submission) {
  const key = idOf(submission.idempotencyKey);
  const hit = key ? index.settledByKey.get(key) ?? null : null;
  return { sequence: hit?.sequence ?? null, settlement: hit?.settlement ?? null };
}

/**
 * causalityStats counts what one envelope holds, so a lane header can say how
 * much it is showing without a reader scrolling to find out.
 *
 * @param {any} envelope
 * @returns {{caused: number, exogenous: number, submitted: number, settled: number, refused: number, abandoned: number}}
 */
export function causalityStats(envelope) {
  const { caused, exogenous } = causedEvents(envelope);
  let refused = 0;
  let abandoned = 0;
  for (const st of envelope?.settled ?? []) {
    if (isAbandoned(st)) abandoned += 1;
    else if (!isApplied(st)) refused += 1;
  }
  return {
    caused: caused.length,
    exogenous: exogenous.length,
    submitted: (envelope?.submitted ?? []).length,
    settled: (envelope?.settled ?? []).length,
    refused,
    abandoned,
  };
}

/**
 * renderCausality appends the two lanes to the detail region, below the
 * occurrence panel, for the record the reader selected. The lanes are a view
 * of that one record's envelope — the same rule the tick list follows — while
 * their links resolve across every held record, because the half of a link
 * that answers a command is not in the record that asked.
 *
 * @param {HTMLElement} host the detail region's body
 * @param {any} model
 * @param {(sequence: string|null, command: string|null) => void} followCause
 *   jumps to the record holding the linked move and lights every lane row
 *   that names the same command.
 */
export function renderCausality(host, model, followCause) {
  const record = model.record(model.selectedSequence);
  const envelope = causalityOf(record);
  const index = indexCommands(model);
  host.appendChild(worldSection(envelope, index, followCause));
  host.appendChild(planSection(envelope, index, followCause));
}

/* ---------------- sections ---------------- */

function worldSection(envelope, index, followCause) {
  const { caused, exogenous } = causedEvents(envelope);
  const children = [];
  if (caused.length === 0 && exogenous.length === 0) {
    children.push(empty("This tick carries no world events."));
  }
  for (const ev of caused) children.push(eventRow(ev, "caused", index, followCause));
  for (const ev of exogenous) children.push(eventRow(ev, "exogenous", index, followCause));
  const aside = `${caused.length} caused · ${exogenous.length} exogenous`;
  return section("World events", children, aside);
}

function planSection(envelope, index, followCause) {
  const children = [];
  const submissions = envelope?.submitted ?? [];
  const settlements = envelope?.settled ?? [];
  if (submissions.length === 0 && settlements.length === 0) {
    children.push(empty("This tick submitted nothing."));
  }
  for (const sub of submissions) children.push(submissionRow(sub, index, followCause));
  // A settlement whose submission is not in this record is the buffered case —
  // a claim or a walk-home hop whose receipt landed after the last plan tick,
  // or a receipt riding the record after the command it answers. It is still
  // the plan's own history, so it is listed with the link back to its
  // submission rather than dropped.
  const submittedKeys = new Set(submissions.map((s) => idOf(s.idempotencyKey)));
  for (const st of settlements) {
    if (submittedKeys.has(displayOf(st.idempotencyKey))) continue;
    children.push(settlementRow(st, index, followCause));
  }
  const aside = `${submissions.length} submitted · ${settlements.length} settled`;
  return section("Plan commands", children, aside);
}

/* ---------------- rows ---------------- */

function eventRow(ev, kind, index, followCause) {
  const link = kind === "caused" ? linkEvent(index, ev) : { command: null, sequence: null, submission: null };
  const row = el("li", {
    class: "lane-row",
    "data-kind": kind,
    "data-command": link.command ?? "",
  }, [
    el("span", { class: "lane-kind", text: kind }),
    valueSpan("lane-what", ev.kind),
    el("span", { class: "lane-seq", text: `world ${String(ev.worldSeq)}` }),
  ]);
  for (const part of entityNote(ev)) row.appendChild(el("span", { class: "lane-id", text: part }));
  if (kind === "caused") {
    if (link.submission) {
      const button = el("button", {
        class: "lane-link",
        type: "button",
        title: `follow to ${link.command} at tick ${String(link.submission.planTickSeq)}`,
      }, [
        document.createTextNode("by "),
        valueSpan("lane-id", ev.causeCommand),
        document.createTextNode(` · tick ${String(link.submission.planTickSeq)}`),
      ]);
      button.addEventListener("click", () => followCause(link.sequence, link.command));
      row.appendChild(button);
    } else {
      row.appendChild(valueSpan("lane-id", ev.causeCommand));
      row.appendChild(el("span", { class: "lane-note", text: "cause not held" }));
    }
  } else {
    row.appendChild(el("span", { class: "lane-note", text: "the world's own change" }));
  }
  return row;
}

function submissionRow(sub, index, followCause) {
  const { settlement } = linkSubmission(index, sub);
  const state = !settlement
    ? "waiting"
    : isAbandoned(settlement)
      ? "abandoned"
      : isApplied(settlement)
        ? "applied"
        : "refused";
  const row = el("li", {
    class: "lane-row",
    "data-state": state,
    "data-command": idOf(sub.commandId),
  }, [
    valueSpan("lane-what", sub.kind),
    el("span", { class: "lane-seq", text: `tick ${String(sub.planTickSeq)}` }),
    el("span", { class: "lane-state", text: state }),
    valueSpan("lane-id", sub.idempotencyKey),
  ]);
  if (settlement) row.appendChild(settlementBody(settlement, followCause));
  return row;
}

function settlementRow(st, index, followCause) {
  const state = isAbandoned(st) ? "abandoned" : isApplied(st) ? "applied" : "refused";
  const row = el("li", {
    class: "lane-row",
    "data-state": state,
    "data-command": idOf(st.commandId),
  }, [
    valueSpan("lane-what", st.kind),
    el("span", { class: "lane-state", text: `${state} (buffered)` }),
    valueSpan("lane-id", st.idempotencyKey),
  ]);
  // The link back to the submission that asked: the command id is the same on
  // both halves, and the submission's record is where the tick lives.
  const hit = index.byKey.get(idOf(st.idempotencyKey));
  if (hit) {
    const button = el("button", {
      class: "lane-link",
      type: "button",
      title: `follow to the submission at tick ${String(hit.submission.planTickSeq)}`,
    }, [document.createTextNode(`answers tick ${String(hit.submission.planTickSeq)}`)]);
    button.addEventListener("click", () => followCause(hit.sequence, idOf(st.commandId)));
    row.appendChild(button);
  }
  row.appendChild(settlementBody(st, followCause));
  return row;
}

function settlementBody(st, followCause) {
  const body = el("span", { class: "lane-detail" });
  if (isAbandoned(st)) {
    body.appendChild(el("span", { class: "lane-note", text: "withdrawn, nothing decided" }));
    if (st.abandoned?.reason) body.appendChild(valueSpan("lane-reason", st.abandoned.reason));
    return body;
  }
  const events = st.events ?? [];
  if (!isApplied(st)) {
    // A refusal: the reason, the holder when the world named one, and the
    // explicit absence of world effects. The acceptance's "a rejected command
    // is visible with no world effect" is this row — the reason next to "no
    // world effect", not a blank that reads as unknown.
    if (st.reason) body.appendChild(valueSpan("lane-reason", st.reason));
    if (st.holder) {
      body.appendChild(el("span", { class: "lane-note" }, [
        document.createTextNode("held by "),
        valueSpan("lane-id", st.holder),
      ]));
    }
    body.appendChild(el("span", { class: "lane-note", text: "no world effect" }));
    return body;
  }
  if (events.length === 0) {
    body.appendChild(el("span", { class: "lane-note", text: "applied, no events published" }));
    return body;
  }
  const command = idOf(st.commandId);
  for (const ev of events) {
    const chip = el("button", {
      class: "lane-effect",
      type: "button",
      title: `this receipt published ${displayOf(ev.kind)}`,
    }, [valueSpan("lane-what", ev.kind)]);
    // The chip names the event this settlement published; following it lands
    // on the record whose plan lane holds this settlement, where the command
    // rows carry the same data-command and light up together.
    chip.addEventListener("click", () => followCause(null, command));
    body.appendChild(chip);
  }
  return body;
}

/* ---------------- primitives ---------------- */

function entityNote(ev) {
  const parts = [];
  for (const field of ["reefer", "slot", "node", "lane"]) {
    if (ev[field]) parts.push(`${field}=${displayOf(ev[field])}`);
  }
  return parts;
}

/**
 * idOf reads an identity field the wire may omit on a malformed or imported
 * record. A missing id is no identity: the row links to nothing and the lane
 * says so, rather than one absent key taking down the whole paint —
 * renderCausality runs on every selection, so a crash here blanks the
 * occurrence panel too, and a debugger that goes blank on bad data has
 * stopped debugging.
 *
 * @param {any} value
 * @returns {string}
 */
function idOf(value) {
  return value ? displayOf(value) : "";
}

function valueSpan(className, value) {
  if (!value) {
    return el("span", { class: className, "data-availability": "unavailable", text: "unavailable" });
  }
  return el("span", {
    class: className,
    "data-availability": availabilityOf(value),
    text: displayOf(value),
  });
}

function empty(text) {
  return el("p", { class: "detail-empty", text });
}

function section(title, children, aside) {
  const heading = el("h3", {}, [el("span", { text: title })]);
  if (aside) heading.appendChild(el("span", { class: "muted", text: aside }));
  const list = el("ul", { class: "lane-list" });
  for (const child of children) {
    if (child.tagName === "LI") list.appendChild(child);
    else list.appendChild(el("li", {}, [child]));
  }
  return el("section", { class: "field" }, [heading, list]);
}