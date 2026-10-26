// common.mjs - timeline lookups shared by the detectors.
//
// A timeline (see harness.mjs) is a set of time-stamped streams in epoch ms:
// samples (one per presented frame, from the in-page probe), dom probes
// (~10Hz), marks (every real input and journey step), frames (screencast PNGs
// of the composited page), server events (tick-stamped), and error streams.

import { readFileSync, existsSync } from 'node:fs';
import path from 'node:path';
import { decodePng } from '../image.mjs';

// Severity: S1 breaks or blocks the experience; S2 is clearly wrong and
// visible to a player; S3 is a polish issue or a suspect that needs a human
// look. Runs fail on S1/S2.
export function finding(id, severity, title, { t = null, until = null, evidence = {}, detail = '' } = {}) {
    return { id, severity, title, t, until, evidence, detail };
}

export function playing(tl) {
    return tl.samples.filter((s) => !s.early && s.gs !== undefined && s.pos);
}

// sampleAt returns the last sample at or before t (or the first after it).
export function sampleAt(samples, t) {
    let lo = 0, hi = samples.length - 1, best = -1;
    while (lo <= hi) {
        const mid = (lo + hi) >> 1;
        if (samples[mid].t <= t) { best = mid; lo = mid + 1; } else hi = mid - 1;
    }
    return best >= 0 ? samples[best] : samples[0];
}

export function between(arr, t0, t1) {
    return arr.filter((x) => x.t >= t0 && x.t <= t1);
}

export function marksOf(tl, kind) {
    return tl.marks.filter((m) => m.kind === kind);
}

// segments returns [{name, t0, t1}] from the journey's 'segment' marks.
export function segments(tl) {
    const segs = marksOf(tl, 'segment');
    return segs.map((m, i) => ({ name: m.name, t0: m.t, t1: i + 1 < segs.length ? segs[i + 1].t : Infinity }));
}

export function inSegment(tl, name, t) {
    return segments(tl).some((s) => s.name === name && t >= s.t0 && t < s.t1);
}

// eventTimes maps server event ticks onto wall-clock time using the samples'
// (round, t) pairs: an event at tick k happened when the client first saw
// round >= k in the page load that was live when the harness pulled it.
export function eventTimes(tl) {
    // Prefer the exact moment the page received each event over the socket
    // (inpage wire tap). Older timelines without a tap fall back to mapping
    // the server log's ticks through the client's observed rounds.
    const wireEvents = (tl.wire || []).filter((w) => w.kind === 'event');
    if (wireEvents.length) return wireEvents.map((w) => ({ type: w.type, data: w.data, t: w.t }));
    const rs = tl.samples.filter((s) => typeof s.round === 'number' && s.round >= 0);
    return tl.events.map((e) => {
        let t = null;
        for (const s of rs) {
            if (s.t > e.pulledAt + 1500) break;
            if (s.round >= e.tick) { t = s.t; break; }
        }
        return { ...e, t };
    });
}

export function xz(p) { return [p[0], p[2]]; }

// Frames: decoded lazily and cached by file.
export class FrameStore {
    constructor(tl, runDir) {
        this.tl = tl;
        this.dir = runDir;
        this.cache = new Map();
        // Only frames that still exist on disk (pruned runs keep a subset).
        this.frames = (tl.frames || []).filter((f) => existsSync(path.join(runDir, f.file)));
    }

    // at returns what was on screen at time t: the last frame emitted at or
    // before t. The CDP screencast only emits a frame when the composited
    // page changes, so that frame is still what the player was looking at.
    // (Only true for unpruned runs - pruneFrames deletes the in-between
    // frames, and --analyze warns about it.)
    at(t) {
        const f = this.frames;
        let lo = 0, hi = f.length - 1, best = -1;
        while (lo <= hi) {
            const mid = (lo + hi) >> 1;
            if (f[mid].t <= t) { best = mid; lo = mid + 1; } else hi = mid - 1;
        }
        return best >= 0 ? f[best] : null;
    }

    image(frame) {
        if (!frame) return null;
        if (!this.cache.has(frame.file)) {
            if (this.cache.size > 64) this.cache.delete(this.cache.keys().next().value);
            this.cache.set(frame.file, decodePng(readFileSync(path.join(this.dir, frame.file))));
        }
        return this.cache.get(frame.file);
    }
}

// hudMasks converts the visible HUD rects of a DOM probe into frame
// fractions so pixel detectors can ignore them.
export function hudMasks(dom, pad = 6) {
    if (!dom) return [];
    const out = [];
    const add = (r) => out.push({
        x0: (r[0] - pad) / dom.W, y0: (r[1] - pad) / dom.H,
        x1: (r[0] + r[2] + pad) / dom.W, y1: (r[1] + r[3] + pad) / dom.H,
    });
    for (const v of Object.values(dom.hud || {})) add(v.rect);
    for (const v of Object.values(dom.chips || {})) add(v.rect);
    return out;
}

export function fmt(n, d = 2) { return typeof n === 'number' && Number.isFinite(n) ? n.toFixed(d) : String(n); }
