// Interpolated actor buffers, fed by server snapshots and read by the
// renderer, HUD, bot SDK and debug hooks.
//
// The rules that make the picture match the game:
//   - only a NEW snapshot seq may move targets. Snapshots arrive at 20 Hz and
//     frames run at 60+ Hz; re-reading the same snapshot every frame used to
//     reset the interpolation clock and freeze every enemy on screen.
//   - a new snapshot is the authority on which actors exist. An id it does
//     not list is gone (killed, despawned) and its buffer must not survive.
//   - a death event wins over any snapshot still in flight: dead ids are
//     remembered and never re-created, so the corpse burst is the last thing
//     the player sees rather than a ghost standing where the enemy died.
//
// This module is deliberately pure (no DOM, no globals) so node tests can
// drive it directly.

const DEFAULT_APPROACH_RATE = 12; // 1/s: ~63% of the gap closed in 83 ms

function newBuffer(m, nowSec) {
    return {
        pos: [m.x, 0.5, m.y],
        targetPos: [m.x, 0.5, m.y],
        prevPos: [m.x, 0.5, m.y],
        lastUpdate: nowSec,
        type: m.type | 0,
        element: m.element | 0,
        hp: m.hp | 0,
        stunned: !!m.stunned,
        strategy: m.strategy | 0,
        yaw: 0,
        step: 0,
        attackUntil: 0,
        hurtUntil: 0,
    };
}

export class ActorStore {
    constructor({ approachRate = DEFAULT_APPROACH_RATE } = {}) {
        this.buffers = new Map();
        this.deadIds = new Map(); // id -> time of death, in seconds
        this.lastSeq = null;
        this.approachRate = approachRate;
    }

    // applySnapshot ingests one full-state snapshot. Returns false when the
    // seq was already applied (nothing changes) and true otherwise.
    applySnapshot(seq, minotaurs, nowSec) {
        if (seq === this.lastSeq) return false;
        this.lastSeq = seq;

        const seen = new Set();
        for (const m of minotaurs || []) {
            const id = m.id;
            seen.add(id);
            if (this.deadIds.has(id)) {
                this.buffers.delete(id);
                continue;
            }
            // A zero-HP actor is dead even if it slipped into this snapshot
            // ahead of the death event; remember it and stop drawing it.
            if ((m.hp | 0) <= 0) {
                this.deadIds.set(id, nowSec);
                this.buffers.delete(id);
                continue;
            }
            let buf = this.buffers.get(id);
            if (!buf) {
                buf = newBuffer(m, nowSec);
                this.buffers.set(id, buf);
            }
            buf.targetPos[0] = m.x;
            buf.targetPos[1] = 0.5;
            buf.targetPos[2] = m.y;
            buf.lastUpdate = nowSec;
            buf.type = m.type | 0;
            buf.element = m.element | 0;
            buf.hp = m.hp | 0;
            buf.stunned = !!m.stunned;
            buf.strategy = m.strategy | 0;
        }

        for (const id of [...this.buffers.keys()]) {
            if (!seen.has(id)) this.buffers.delete(id);
        }
        // Forget a death mark once the server's roster no longer carries the
        // id, so the map cannot grow for the life of the session.
        for (const id of [...this.deadIds.keys()]) {
            if (!seen.has(id)) this.deadIds.delete(id);
        }
        return true;
    }

    // markDead is called from the death event. The buffer goes immediately;
    // the id stays quarantined until a snapshot omits it.
    markDead(id, nowSec) {
        this.deadIds.set(id, nowSec);
        this.buffers.delete(id);
    }

    // step advances every live buffer toward its target. dt is in seconds;
    // the approach is frame-rate independent. Facing and gait come from the
    // displacement the renderer actually drew, so models walk the way they
    // move rather than snapping to the server's heading.
    step(dt) {
        const a = 1 - Math.exp(-this.approachRate * Math.max(0, dt));
        for (const buf of this.buffers.values()) {
            buf.pos[0] += (buf.targetPos[0] - buf.pos[0]) * a;
            buf.pos[1] += (buf.targetPos[1] - buf.pos[1]) * a;
            buf.pos[2] += (buf.targetPos[2] - buf.pos[2]) * a;
            const px = buf.pos[0] - buf.prevPos[0];
            const pz = buf.pos[2] - buf.prevPos[2];
            buf.step = Math.hypot(px, pz);
            if (buf.step > 0.002) buf.yaw = Math.atan2(px, pz);
            buf.prevPos[0] = buf.pos[0];
            buf.prevPos[1] = buf.pos[1];
            buf.prevPos[2] = buf.pos[2];
        }
        return this.buffers;
    }

    entries() { return this.buffers.entries(); }
    values() { return this.buffers.values(); }
    get(id) { return this.buffers.get(id); }
    get size() { return this.buffers.size; }
}
