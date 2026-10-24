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

const MAX_SAMPLES = 8000;
const MAX_EVENTS = 4000;

export class Inspector {
    constructor(provider) {
        // provider.world     - the live world state (maze, camera, actors...)
        // provider.command() - the last command the bot issued, or null
        this.provider = provider;
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

    // sample records one state observation. It is called from the game loop
    // every frame and records only when the sampling interval has elapsed.
    sample() {
        if (!this.active) return null;
        const t = nowMs();
        if (t - this.lastSampleAt < this.intervalMs) return null;
        this.lastSampleAt = t;
        const world = this.provider.world;
        const camera = world.camera;
        if (!camera) return null;

        const cmd = this.provider.command ? this.provider.command() : null;
        const motion = this.provider.motion ? this.provider.motion() : null;
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

        for (let k = 0; k < samples.length; k++) {
            const s = samples[k];
            cells.add(`${s.cell[0]},${s.cell[1]}`);
            if (lastHp !== null && s.hp < lastHp) damageTaken += lastHp - s.hp;
            lastHp = s.hp;
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

function nowMs() {
    return typeof performance !== 'undefined' && performance.now
        ? performance.timeOrigin + performance.now()
        : Date.now();
}
