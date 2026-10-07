/*
   Following the stream.

   EventSource cannot carry the capability, so the stream is read off fetch and
   parsed here. That is more code than a constructor call and it is the reason the
   capability can stay in a header at all.

   Three things the stream can say, and the client has to tell them apart:

     - a record, which advances the position;
     - behind, which ends the connection and hands back a cursor, because the
       store can always give a client the gap it missed and the queue cannot;
     - a comment, which says the connection is alive and advances nothing.

   A record is applied only when its sequence is the next one the client expects.
   Anything else is either a duplicate it already has or a gap, and a gap is
   closed by reading the store rather than by waiting, because the store is the
   authority and the queue is a convenience.
*/

import { parseJSON } from "./lossless.mjs";
import { compareSeq } from "./model.mjs";

/**
 * EventStream follows one run's committed records.
 */
export class EventStream {
  /**
   * @param {import("./api.js").Api} api
   * @param {{onRecord: function(any): void, onBehind: function(string): void,
   *          onState: function(string): void, onError: function(Error): void,
   *          onEnded: function(string): void}} handlers
   */
  constructor(api, handlers) {
    this.api = api;
    this.handlers = handlers;
    this.controller = null;
    this.token = "";
    this.position = "0";
    this.closed = false;
  }

  /**
   * start follows the run from a cursor.
   *
   * @param {string} token a cursor the store minted
   * @param {string} position the sequence the client already holds
   */
  start(token, position) {
    this.stop();
    this.token = token;
    this.position = position;
    this.closed = false;
    this.controller = new AbortController();
    this.pump(this.controller.signal).catch((cause) => {
      // An abort is this client stopping, not a failure, so it is not reported and
      // it does not ask for a reconnect.
      if (this.closed || cause.name === "AbortError") return;
      this.handlers.onError(cause);
      this.handlers.onEnded(cause.message);
    });
  }

  /** stop ends the connection and leaves nothing pending. */
  stop() {
    this.closed = true;
    if (this.controller) {
      this.controller.abort();
      this.controller = null;
    }
  }

  /**
   * @param {AbortSignal} signal
   * @returns {Promise<void>}
   */
  async pump(signal) {
    const response = await this.api.request(this.api.streamPath(this.token), { signal });
    if (!response.body) {
      throw new Error("the stream arrived with no body to read");
    }
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";
    try {
      for (;;) {
        const { value, done } = await reader.read();
        if (done) {
          // The store ended the connection, and so did a dropped one. Either way
          // the position this client holds is the truth about what it has seen, so
          // a reconnect resumes from it rather than assuming the last frame
          // arrived whole. Ending is not the same as failing: a stream the server
          // closed cleanly still has to be picked up again, or a client that was
          // closed by a proxy comes back to a run that has moved on.
          if (!this.closed) {
            this.handlers.onState("reloading");
            this.handlers.onEnded("the stream ended");
          }
          return;
        }
        buffer += decoder.decode(value, { stream: true });
        let split;
        // Frames are separated by a blank line. The terminator is two newlines,
        // and a frame's own lines are single-newline terminated, so the first
        // blank line in the buffer ends a frame.
        while ((split = buffer.indexOf("\n\n")) >= 0) {
          const frame = buffer.slice(0, split);
          buffer = buffer.slice(split + 2);
          this.handleFrame(frame);
          if (this.closed) return;
        }
      }
    } finally {
      try {
        reader.releaseLock();
      } catch {
        // A released reader is not a failure worth reporting; the connection is
        // already over either way.
      }
    }
  }

  /**
   * handleFrame interprets one SSE frame.
   *
   * @param {string} frame
   */
  handleFrame(frame) {
    if (frame.startsWith(":")) return; // A comment: a keep-alive, and nothing to apply.
    let name = "";
    let id = "";
    const data = [];
    for (const line of frame.split("\n")) {
      if (line.startsWith(":")) continue;
      const colon = line.indexOf(":");
      const field = colon < 0 ? line : line.slice(0, colon);
      // A single leading space after the colon is part of the framing.
      const value = colon < 0 ? "" : line.slice(colon + 1).replace(/^ /, "");
      switch (field) {
        case "event": name = value; break;
        case "id": id = value; break;
        case "data": data.push(value); break;
        default: break;
      }
    }
    if (data.length === 0) return;
    const payload = data.join("\n");
    switch (name) {
      case "record": {
        let record;
        try {
          record = parseJSON(payload);
        } catch (cause) {
          this.handlers.onError(new Error(`the stream sent a record that is not JSON: ${cause.message}`));
          return;
        }
        if (id && id !== record.sequence) {
          this.handlers.onError(new Error(`the stream numbered ${id} a record whose sequence is ${record.sequence}`));
          return;
        }
        if (compareSeq(record.sequence, this.position) <= 0) {
          // Already held: a duplicate, not a gap. Compared as decimals, because
          // both sides are decimal strings by construction and "<=" on two strings
          // is lexicographic - which drops every record the moment a sequence
          // crosses a digit boundary, from 9 to 10, with no error and no
          // reconnect. The connection would stay open and the page would sit at
          // tick 9 while the store moved on.
          return;
        }
        this.handlers.onRecord(record);
        this.position = record.sequence;
        return;
      }
      case "behind": {
        let body;
        try {
          body = parseJSON(payload);
        } catch {
          body = null;
        }
        // The position is reported as well as the token, so a client that cannot
        // read the token can still resume from the sequence it reached.
        this.handlers.onBehind(body && body.token ? body.token : this.token);
        this.closed = true;
        return;
      }
      case "error":
        this.handlers.onError(new Error(safeMessage(payload)));
        this.closed = true;
        return;
      default:
        return;
    }
  }
}

/**
 * safeMessage reads the message out of an error frame, falling back to the raw
 * text. The stream's own error text is the store's, and it is shown rather than
 * parsed: a client that reports "the store said X" is more useful than one that
 * reports a parse failure of X.
 *
 * @param {string} payload
 * @returns {string}
 */
function safeMessage(payload) {
  try {
    const body = parseJSON(payload);
    if (body && typeof body.message === "string") return body.message;
  } catch {
    // Fall through to the raw text below.
  }
  return payload.slice(0, 400);
}
