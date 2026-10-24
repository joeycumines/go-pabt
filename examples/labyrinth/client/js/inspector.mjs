// inspector.mjs - The playtest instrument.
//
// The inspector is the part of the harness that makes a session *queryable*
// after the fact. While the game runs it records two things at a fixed rate:
// a full state sample (the runner, every actor, the maze cell it occupies,
// the command the bot issued, the PA-BT action of the nearest actor) and the
// server event stream. Afterwards those records can be sliced by time
// window, by event type, by actor, or by anomaly.
//
// It deliberately records raw observations rather than verdicts: an oracle
// that only reports "the run failed" hides the reason. The derived numbers
// in stats() are computed from the same raw samples, so a reader can always
// re-derive them and disagree.

import { projectToScreen } from './bot-sdk.mjs';

const MAX_SAMPLES = 8000;
const MAX_EVENTS = 4000;

export class Inspector {
    constructor(provider) {
        // provider.world     - the live world state (maze, camera, actors...)
        // provider.command() - the last command the bot issued, or null
        // provider.gl()      - the WebGL2 context, for canvas liveness probes
        // provider.canvas()  - the game canvas
        this.provider = provider;
        this.glFn = provider.gl || (() => null);
        this.canvasFn = provider.canvas || (() => null);
        this._pixelBuf = new Uint8Array(8 * 8 * 4);
        this.blankSamples = 0;
        this.samples = [];
        this.eventLog = [];
        this.active = false;
        this.startedAt = 0;
        this.lastSampleAt = -Infinity;
        this.intervalMs = 50;
        this.pendingEventTypes = [];
    }

    // start begins recording. `hz` is the sampling rate; samples are also
    // capped so a long session cannot exhaust the page's memory.
    start(opts = {}) {
        const hz = opts.hz && opts.hz > 0 ? opts.hz : 20;
        this.intervalMs = 1000 / hz;
        this.samples = [];
        this.eventLog = [];
        this.pendingEventTypes = [];
        this.blankSamples = 0;
        this.active = true;
        this.startedAt = nowMs();
        this.lastSampleAt = -Infinity;
        return { ok: true, hz };
    }

    stop() {
        this.active = false;
        return { samples: this.samples.length, events: this.eventLog.length };
    }

    // recordEvent is called for every server event. The event is stamped
    // with the sample index that was current when it arrived, so a reader
    // can join an event to the world state that produced it.
    recordEvent(evt) {
        if (!evt || !evt.type) return;
        this.eventLog.push({
            i: this.samples.length,
            t: Number(((nowMs() - this.startedAt) / 1000).toFixed(3)),
            epochMs: nowMs(),
            type: evt.type,
            data: evt.data === undefined ? null : evt.data,
        });
        if (this.eventLog.length > MAX_EVENTS) {
            this.eventLog.splice(0, this.eventLog.length - MAX_EVENTS);
        }
        this.pendingEventTypes.push(evt.type);
    }

    // _probeCanvas reads an 8x8 patch at the centre of the WebGL canvas and
    // reports whether the frame is alive. A synchronous 8x8 readPixels costs
    // a fraction of a millisecond; a toDataURL capture would stall the whole
    // render loop, so it must never be used here.
    _probeCanvas() {
        const gl = this.glFn();
        const cvs = this.canvasFn();
        if (!gl || !cvs || typeof gl.readPixels !== 'function' || !cvs.width || !cvs.height) {
            return { probed: false, lumaStddev: null, nonBlackRatio: null, blank: false };
        }
        const x = Math.max(0, (cvs.width >> 1) - 4);
        const y = Math.max(0, (cvs.height >> 1) - 4);
        gl.readPixels(x, y, 8, 8, gl.RGBA, gl.UNSIGNED_BYTE, this._pixelBuf);
        let sum = 0;
        let sumSq = 0;
        let nonBlack = 0;
        for (let i = 0; i < 64; i++) {
            const r = this._pixelBuf[i * 4];
            const g = this._pixelBuf[i * 4 + 1];
            const b = this._pixelBuf[i * 4 + 2];
            const luma = 0.299 * r + 0.587 * g + 0.114 * b;
            sum += luma;
            sumSq += luma * luma;
            if (r + g + b > 12) nonBlack++;
        }
        const mean = sum / 64;
        const lumaStddev = Math.sqrt(Math.max(0, sumSq / 64 - mean * mean));
        const nonBlackRatio = nonBlack / 64;
        return {
            probed: true,
            lumaStddev: round2(lumaStddev),
            nonBlackRatio: round2(nonBlackRatio),
            blank: nonBlackRatio === 0 && lumaStddev < 0.5,
        };
    }

    // sample records one state observation. It is called from the game loop
    // every frame and records only when the sampling interval has elapsed.
    sample(force = false) {
        if (!this.active) return null;
        const t = nowMs();
        if (!force && t - this.lastSampleAt < this.intervalMs) return null;
        this.lastSampleAt = t;
        const world = this.provider.world;
        const camera = world.camera;
        if (!camera) return null;

        const cmd = this.provider.command ? this.provider.command() : null;
        const motion = this.provider.motion ? this.provider.motion() : null;
        const canvasProbe = this._probeCanvas();
        if (canvasProbe.probed && canvasProbe.blank && !force) {
            // A dead readback is not a state sample: reject it rather than
            // poison the record with a frame that shows nothing.
            this.blankSamples++;
            return null;
        }
        const actors = [];
        let nearest = null;
        for (const [id, buf] of world.actors) {
            if (buf.hp <= 0) continue;
            const x = buf.pos[0];
            const z = buf.pos[2];
            const dist = Math.hypot(x - camera.pos[0], z - camera.pos[2]);
            const row = (world.btRows || []).find((r) => r.id === id);
            const actor = {
                id,
                x: Number(x.toFixed(2)),
                z: Number(z.toFixed(2)),
                hp: buf.hp,
                type: buf.type | 0,
                element: buf.element | 0,
                stunned: !!buf.stunned,
                strategy: buf.strategy | 0,
                dist: Number(dist.toFixed(2)),
                btAction: row ? (row.active || '') : '',
            };
            actors.push(actor);
            if (!nearest || actor.dist < nearest.dist) nearest = actor;
        }
        actors.sort((a, b) => a.dist - b.dist);

        const types = this.pendingEventTypes.slice();
        this.pendingEventTypes.length = 0;

        // Client/server divergence and frustum perception, from the same
        // sources the Bot SDK uses. The motion provider reports the freshest
        // serverPos (read at sample time, after this frame's reconciliation),
        // so prefer it over the possibly frame-stale world.serverPos.
        const srvPos = motion && motion.serverPos ? motion.serverPos : world.serverPos;
        const srvZ = Array.isArray(srvPos)
            ? (srvPos.length >= 3 ? srvPos[2] : srvPos[1])
            : 0;
        const syncDrift = Array.isArray(srvPos)
            ? round2(Math.hypot(srvPos[0] - camera.pos[0], srvZ - camera.pos[2]))
            : 0;
        let perceivableCount = 0;
        for (const a of actors) {
            const proj = projectToScreen(camera, a.x === undefined ? 0 : a.x, 0.5, a.z);
            a.inFrustum = proj.inFrustum;
            a.screenX = proj.screenX;
            a.screenY = proj.screenY;
            if (proj.inFrustum) perceivableCount++;
        }

        const sample = {
            i: this.samples.length,
            t: Number(((t - this.startedAt) / 1000).toFixed(3)),
            epochMs: t,
            frame: world.frame,
            round: world.round,
            phase: world.phase,
            hp: world.hp,
            element: world.playerElement,
            intensity: world.intensity,
            x: Number(camera.pos[0].toFixed(3)),
            z: Number(camera.pos[2].toFixed(3)),
            yaw: Number(camera.yaw.toFixed(3)),
            cell: world.maze
                ? [Math.floor(camera.pos[0]), Math.floor(camera.pos[2])]
                : [0, 0],
            gate: world.maze
                ? { x: world.maze.gateX, z: world.maze.gateY }
                : null,
            actors,
            nearest: nearest
                ? { id: nearest.id, dist: nearest.dist, action: nearest.btAction }
                : null,
            command: cmd
                ? { forward: Number(cmd.forward.toFixed(3)), strafe: Number(cmd.strafe.toFixed(3)), note: cmd.note || '' }
                : null,
            motion: motion
                ? {
                    vel: [Number(motion.vel[0].toFixed(3)), Number(motion.vel[1].toFixed(3))],
                    serverPos: motion.serverPos
                        ? [Number(motion.serverPos[0].toFixed(3)), Number(motion.serverPos[1].toFixed(3))]
                        : null,
                    drift: motion.serverPos
                        ? Number(Math.hypot(
                            motion.serverPos[0] - camera.pos[0],
                            motion.serverPos[1] - camera.pos[2],
                        ).toFixed(3))
                        : null,
                }
                : null,
            syncDrift,
            perceivableCount,
            canvasProbe,
            events: types,
            notable: types.length > 0,
        };
        this.samples.push(sample);
        if (this.samples.length > MAX_SAMPLES) {
            this.samples.splice(0, this.samples.length - MAX_SAMPLES);
        }
        return sample;
    }

    // frames returns the recorded state samples, optionally filtered.
    //   from,to    - sample index window (inclusive)
    //   tFrom,tTo  - seconds since the inspector started
    //   notableOnly - only samples that carry at least one event
    //   eventType  - only samples carrying that event type
    frames(opts = {}) {
        const { from, to, tFrom, tTo, notableOnly, eventType } = opts;
        return this.samples.filter((s) => {
            if (from !== undefined && s.i < from) return false;
            if (to !== undefined && s.i > to) return false;
            if (tFrom !== undefined && s.t < tFrom) return false;
            if (tTo !== undefined && s.t > tTo) return false;
            if (notableOnly && !s.notable) return false;
            if (eventType && !s.events.includes(eventType)) return false;
            return true;
        });
    }

    // events returns the recorded server events.
    //   types  - event type filter (array or single string)
    //   tFrom,tTo - seconds since the inspector started
    //   from,to - sample index window
    events(opts = {}) {
        const { types, tFrom, tTo, from, to } = opts;
        let wanted = null;
        if (types) wanted = new Set(Array.isArray(types) ? types : [types]);
        return this.eventLog.filter((e) => {
            if (wanted && !wanted.has(e.type)) return false;
            if (tFrom !== undefined && e.t < tFrom) return false;
            if (tTo !== undefined && e.t > tTo) return false;
            if (from !== undefined && e.i < from) return false;
            if (to !== undefined && e.i > to) return false;
            return true;
        });
    }

    // timeline merges samples and events into one ordered stream so a reader
    // can ask "what happened between 4.0s and 6.0s" in one query.
    timeline(opts = {}) {
        const samples = this.frames(opts).map((s) => ({
            kind: 'sample',
            t: s.t,
            i: s.i,
            epochMs: s.epochMs,
            round: s.round,
            hp: s.hp,
            x: s.x,
            z: s.z,
            cell: s.cell,
            nearest: s.nearest,
            events: s.events,
        }));
        const events = this.events(opts).map((e) => ({
            kind: 'event',
            t: e.t,
            i: e.i,
            epochMs: e.epochMs,
            type: e.type,
            data: e.data,
        }));
        const merged = samples.concat(events);
        merged.sort((a, b) => (a.epochMs - b.epochMs) || (a.kind < b.kind ? -1 : 1));
        return merged;
    }

    // stats derives the diagnostics a playtest reader needs. Every number is
    // computed from the raw samples, never from a separate counter.
    stats() {
        const samples = this.samples;
        const eventCounts = {};
        for (const e of this.eventLog) {
            eventCounts[e.type] = (eventCounts[e.type] || 0) + 1;
        }
        const cells = new Set();
        const stuckCells = new Map();
        let distance = 0;
        let stuck = 0;
        let gateMin = Infinity;
        let gateMinAt = null;
        let gateNow = null;
        let damageTaken = 0;
        let lastHp = null;
        let sumDrift = 0;
        let maxDrift = 0;
        let driftSamples = 0;

        for (let k = 0; k < samples.length; k++) {
            const s = samples[k];
            cells.add(`${s.cell[0]},${s.cell[1]}`);
            if (lastHp !== null && s.hp < lastHp) damageTaken += lastHp - s.hp;
            lastHp = s.hp;
            if (typeof s.syncDrift === 'number') {
                sumDrift += s.syncDrift;
                driftSamples++;
                if (s.syncDrift > maxDrift) maxDrift = s.syncDrift;
            }
            if (s.gate) {
                const d = Math.hypot(s.x - s.gate.x, s.z - s.gate.z);
                if (d < gateMin) {
                    gateMin = d;
                    gateMinAt = { t: s.t, i: s.i, x: s.x, z: s.z };
                }
                gateNow = d;
            }
            if (k === 0) continue;
            const prev = samples[k - 1];
            const moved = Math.hypot(s.x - prev.x, s.z - prev.z);
            distance += moved;
            const commanded = s.command
                ? Math.hypot(s.command.forward, s.command.strafe)
                : 0;
            if (commanded > 0.2 && moved < 0.03) {
                stuck++;
                const key = `${s.cell[0]},${s.cell[1]}`;
                stuckCells.set(key, (stuckCells.get(key) || 0) + 1);
            }
        }

        const stuckList = Array.from(stuckCells.entries())
            .map(([cell, count]) => ({ cell, count }))
            .sort((a, b) => b.count - a.count);

        return {
            samples: samples.length,
            events: this.eventLog.length,
            eventCounts,
            cellsVisited: cells.size,
            distanceTravelled: Number(distance.toFixed(2)),
            stuckSamples: stuck,
            stuckCells: stuckList.slice(0, 8),
            shotsFired: this.provider.shotsFired ? this.provider.shotsFired() : 0,
            shotsHit: eventCounts.stunned || 0,
            damageTaken,
            deaths: eventCounts.death || 0,
            gateDistanceNow: gateNow === null ? null : Number(gateNow.toFixed(2)),
            gateDistanceMin: gateMin === Infinity ? null : Number(gateMin.toFixed(2)),
            gateDistanceMinAt: gateMinAt,
            maxSyncDrift: driftSamples > 0 ? round2(maxDrift) : 0,
            avgSyncDrift: driftSamples > 0 ? round2(sumDrift / driftSamples) : 0,
            // Sustained-desync counter: one-sample knockback transients
            // (server knockback displaces serverPos by up to ~1.1u and the
            // reconciler snaps within a frame) are physically unavoidable,
            // so the desync verdict counts samples that stay out of sync
            // rather than the instantaneous maximum.
            desyncSamples: samples.filter((s) => (s.syncDrift || 0) > 0.60).length,
            blankSamples: this.blankSamples || 0,
        };
    }

    // export returns the raw record for offline analysis.
    export() {
        return {
            samples: this.samples,
            events: this.eventLog,
            stats: this.stats(),
        };
    }
}

function round2(v) {
    return Number(v.toFixed(2));
}

function nowMs() {
    return typeof performance !== 'undefined' && performance.now
        ? performance.timeOrigin + performance.now()
        : Date.now();
}
