/*
   Talking to the debug server's route set.

   Every request carries the capability in a header and nowhere else. That is the
   server's boundary, not a habit: a token in a query string is a token in an
   access log, in a referrer, and in a shell history, and the server's route test
   refuses a token offered any other way. The same reason rules out EventSource,
   which cannot set a header, which is why stream.mjs reads the body off fetch.
*/

import { parseJSON } from "./lossless.mjs";

/** ApiError is a refusal the client can act on, kept apart from a transport failure. */
export class ApiError extends Error {
  /**
   * @param {string} message
   * @param {number} status
   */
  constructor(message, status) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }

  /** @returns {boolean} whether this refusal is the capability being wrong. */
  get isUnauthenticated() {
    return this.status === 401;
  }

  /**
   * @returns {boolean} whether the store moved to another history, which a
   *   client cannot follow and has to reload against.
   */
  get isStaleHistory() {
    return this.status === 409;
  }
}

const BASE = "/debug/pabt";

/** Api is one authenticated connection to one store's active run. */
export class Api {
  /**
   * @param {string} capability
   */
  constructor(capability) {
    this.capability = capability;
  }

  /**
   * request issues one call and hands back the response for a caller that wants
   * the body itself, which the export route needs because its body is a bundle
   * rather than JSON.
   *
   * The path is relative to the route set's base, which is composed here and
   * nowhere else. A caller that built a whole path itself would produce a
   * doubled one, which the server answers as a 404 from path matching rather than
   * as anything that looks like a mistake in the client.
   *
   * @param {string} path
   * @param {{method?: string, body?: BodyInit|null, signal?: AbortSignal}} [init]
   * @returns {Promise<Response>}
   */
  async request(path, init = {}) {
    const headers = { "X-PABT-Capability": this.capability };
    if (init.body !== undefined && init.body !== null) {
      headers["Content-Type"] = "application/vnd.pabtdebug.bundle";
    }
    let response;
    try {
      response = await fetch(BASE + path, {
        method: init.method ?? "GET",
        headers,
        body: init.body ?? null,
        cache: "no-store",
        // Forwarded rather than accepted and dropped: the stream is the one
        // request that has to be cancellable, and a signal that never reaches
        // fetch leaves the connection open after the page has moved on.
        signal: init.signal ?? null,
      });
    } catch (cause) {
      if (cause.name === "AbortError") throw cause;
      throw new ApiError(`the server did not answer: ${cause.message}`, 0);
    }
    if (!response.ok) {
      const text = (await response.text()).trim();
      throw new ApiError(text || response.statusText, response.status);
    }
    return response;
  }

  /**
   * getJSON reads one JSON route.
   *
   * @param {string} path
   * @returns {Promise<any>}
   */
  async getJSON(path) {
    const response = await this.request(path);
    return parseJSON(await response.text());
  }

  /** @returns {Promise<any>} what the store is serving. */
  state() {
    return this.getJSON("/state");
  }

  /**
   * @param {string} after decimal string position, "" for the start
   * @returns {Promise<any>} a bounded page of whole records and the cursor it leaves
   */
  records(after) {
    const query = after === "" || after === undefined ? "" : `?after=${encodeURIComponent(after)}`;
    return this.getJSON(`/records${query}`);
  }

  /**
   * @param {string} sequence
   * @returns {Promise<any>} the tree with the delta that produced it and a named base
   */
  representation(sequence) {
    return this.getJSON(`/representations/${encodeURIComponent(sequence)}`);
  }

  /**
   * @param {string} sequence
   * @returns {Promise<any>} one committed tree
   */
  tree(sequence) {
    return this.getJSON(`/trees/${encodeURIComponent(sequence)}`);
  }

  /**
   * @param {string} after
   * @returns {Promise<any>} a cursor minted by the store rather than built here
   */
  cursor(after) {
    return this.getJSON(`/cursor?after=${encodeURIComponent(after)}`);
  }

  /**
   * streamPath builds the stream's path for a cursor, relative to the base that
   * request composes.
   *
   * The cursor is a token in a query string, which the capability rule would
   * otherwise forbid. It is not a secret - the cursor's own doc comment says a
   * forged one only chooses a position the client could have asked for by name -
   * and it has to travel in the address because it selects the stream's starting
   * point, which is a property of the connection rather than of the message.
   *
   * @param {string} token
   * @returns {string}
   */
  streamPath(token) {
    return `/stream?cursor=${encodeURIComponent(token)}`;
  }

  /**
   * exportBundle fetches the run's history as a bundle, returning the bytes and
   * the digest the server sealed them with.
   *
   * @returns {Promise<{bytes: ArrayBuffer, digest: string}>}
   */
  async exportBundle() {
    const response = await this.request("/export");
    const bytes = await response.arrayBuffer();
    return { bytes, digest: response.headers.get("X-PABT-Bundle-SHA256") ?? "" };
  }

  /**
   * @param {ArrayBuffer} bytes
   * @returns {Promise<any>} what an import stored and published
   */
  async importBundle(bytes) {
    const response = await this.request("/import", { method: "POST", body: bytes });
    return parseJSON(await response.text());
  }
}
