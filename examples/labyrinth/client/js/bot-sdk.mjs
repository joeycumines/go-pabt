// bot-sdk.mjs - The Bot SDK: an orient/decide/act surface for model-authored bots.
//
// A bot is a source file that exports `tick(ctx, api)`. The `api` object is
// the bot's entire world: it reads orientation from `api.self`, `api.actors`,
// `api.maze` and friends, and it acts through verbs such as `api.aimAt`,
// `api.moveTo`, `api.fire`, `api.sprint` and `api.jump`. Every action is
// accumulated into a command that the runtime applies to the real client
// input, so a bot drives the game exactly the way a player does - no
// teleports, no direct state writes, no privileged access.

export const ELEMENT_NAMES = ['Fire', 'Ice', 'Lightning', 'Void'];
export const ARCHETYPE_NAMES = ['brute', 'swarm', 'caster', 'stalker'];
export const STRATEGY_NAMES = ['patrol', 'raid', 'retreat'];

export const CELL_WALL = 0;
export const CELL_FLOOR = 1;
export const CELL_PLATFORM = 2;
export const CELL_GAP = 3;

// advantage returns the damage multiplier of `attacker` against `defender` in
// the fire > ice > lightning > void > fire cycle: 2 when the attacker beats
// the defender, 0.5 when the defender beats the attacker, 1 otherwise.
export function advantage(attacker, defender) {
    if (attacker === defender) return 1;
    return ((attacker + 1) % 4) === defender ? 2 : 0.5;
}

// walkable reports whether a maze cell can be stood on. Floor and platform
// are walkable; wall and gap are not. A gap needs a jump, so pathing treats
// it as blocked unless the caller opts in.
export function walkable(maze, cx, cz) {
    if (!maze || !maze.grid) return false;
    if (cx < 0 || cz < 0 || cx >= maze.w || cz >= maze.h) return false;
    const kind = maze.grid[cz * maze.w + cx];
    return kind === CELL_FLOOR || kind === CELL_PLATFORM;
}

// pathTo returns the walkable cell centres leading from (sx,sz) towards
// (gx,gz). When the goal is unreachable the path leads to the closest
// reachable cell instead, so a bot still makes progress towards a goal that
// sits behind a wall instead of grinding into that wall.
export function pathTo(maze, sx, sz, gx, gz, allowGap = false) {
    if (!maze || !maze.grid) return [];
    const w = maze.w;
    const h = maze.h;
    const scx = Math.floor(sx);
    const scz = Math.floor(sz);
    const gcx = Math.floor(gx);
    const gcz = Math.floor(gz);
    if (scx === gcx && scz === gcz) return [];

    const startIdx = scz * w + scx;
    const goalIdx = gcz * w + gcx;
    const prev = new Int32Array(w * h).fill(-1);
    const visited = new Uint8Array(w * h);
    const queue = [startIdx];
    visited[startIdx] = 1;
    const dirs = [[0, -1], [0, 1], [-1, 0], [1, 0]];

    let best = startIdx;
    let bestDist = Math.abs(scx - gcx) + Math.abs(scz - gcz);
    let found = false;
    let head = 0;
    while (head < queue.length) {
        const cur = queue[head++];
        if (cur === goalIdx) {
            found = true;
            best = cur;
            break;
        }
        const cx = cur % w;
        const cz = (cur / w) | 0;
        const d = Math.abs(cx - gcx) + Math.abs(cz - gcz);
        if (d < bestDist) {
            bestDist = d;
            best = cur;
        }
        for (const [dx, dz] of dirs) {
            const nx = cx + dx;
            const nz = cz + dz;
            if (nx < 0 || nz < 0 || nx >= w || nz >= h) continue;
            const ni = nz * w + nx;
            if (visited[ni]) continue;
            const kind = maze.grid[ni];
            if (kind === CELL_WALL) continue;
            if (kind === CELL_GAP && !allowGap) continue;
            visited[ni] = 1;
            prev[ni] = cur;
            queue.push(ni);
        }
    }
    if (!found && best === startIdx) return [];

    const out = [];
    let cur = best;
    while (cur !== startIdx && cur !== -1) {
        const cx = cur % w;
        const cz = (cur / w) | 0;
        out.push([cx + 0.5, cz + 0.5]);
        cur = prev[cur];
    }
    out.reverse();
    return out;
}

// projectToScreen projects a world point into the camera's 3D frustum.
// With a real camera (viewMatrix/projMatrix available) it runs the full
// view-projection transform; without one (plain test doubles) it falls back
// to a 2D yaw cone so callers see a consistent shape either way.
export function projectToScreen(cam, wx, wy, wz) {
    if (cam && typeof cam.viewMatrix === 'function' && typeof cam.projMatrix === 'function') {
        const V = cam.viewMatrix();
        // The camera's render aspect when it exposes one (main.mjs keeps it
        // synced to the canvas), so the projection matches what was rendered.
        const P = cam.projMatrix(cam.aspect || 16 / 9);
        const vx = V[0] * wx + V[4] * wy + V[8] * wz + V[12];
        const vy = V[1] * wx + V[5] * wy + V[9] * wz + V[13];
        const vz = V[2] * wx + V[6] * wy + V[10] * wz + V[14];
        const vw = V[3] * wx + V[7] * wy + V[11] * wz + V[15];
        const cx = P[0] * vx + P[4] * vy + P[8] * vz + P[12] * vw;
        const cy = P[1] * vx + P[5] * vy + P[9] * vz + P[13] * vw;
        // For the client's perspective() (column-major), the clip-space w row
        // is the FOURTH row (indices 3, 7, 11, 15): w_clip = -z_view. The
        // third row is the depth row and must not be used for the divide.
        const cw = P[3] * vx + P[7] * vy + P[11] * vz + P[15] * vw;
        if (cw <= 0.05) return { inFrustum: false, screenX: null, screenY: null };
        const ndcX = cx / cw;
        const ndcY = cy / cw;
        const inFrustum = Math.abs(ndcX) <= 1.0 && Math.abs(ndcY) <= 1.0;
        return {
            inFrustum,
            screenX: inFrustum ? round3(ndcX * 0.5 + 0.5) : null,
            screenY: inFrustum ? round3(1 - (ndcY * 0.5 + 0.5)) : null,
        };
    }
    const dx = wx - (cam ? cam.pos[0] : 0);
    const dz = wz - (cam ? cam.pos[2] : 0);
    const dist = Math.hypot(dx, dz);
    if (dist < 1e-6) return { inFrustum: true, screenX: 0.5, screenY: 0.5 };
    const yaw = cam ? (cam.yaw || 0) : 0;
    const fwdX = Math.sin(yaw);
    const fwdZ = Math.cos(yaw);
    const rightX = -Math.cos(yaw);
    const rightZ = Math.sin(yaw);
    const dot = (dx * fwdX + dz * fwdZ) / dist;
    const inFrustum = dot >= 0.5;
    const side = (dx * rightX + dz * rightZ) / dist;
    return {
        inFrustum,
        screenX: inFrustum ? round3(0.5 + side * 0.5) : null,
        screenY: inFrustum ? 0.5 : null,
    };
}

function round3(v) {
    return Number(v.toFixed(3));
}

// losClear reports whether an unobstructed line of sight exists between two
// XZ points. Only floor and platform cells are transparent, which matches
// the server's rayOccluded rule and the client's own visibility test.
export function losClear(maze, x1, z1, x2, z2) {
    if (!maze || !maze.grid) return false;
    const dist = Math.hypot(x2 - x1, z2 - z1);
    const steps = Math.max(1, Math.ceil(dist / 0.25));
    for (let i = 1; i < steps; i++) {
        const t = i / steps;
        const cx = Math.floor(x1 + (x2 - x1) * t);
        const cz = Math.floor(z1 + (z2 - z1) * t);
        if (cx < 0 || cz < 0 || cx >= maze.w || cz >= maze.h) return false;
        const kind = maze.grid[cz * maze.w + cx];
        if (kind !== CELL_FLOOR && kind !== CELL_PLATFORM) return false;
    }
    return true;
}

// buildObservation snapshots the live world into the plain object a bot
// reads. It is rebuilt once per tick so every accessor inside one tick sees
// a consistent world rather than a half-updated one.
export function buildObservation(world) {
    const camera = world.camera;
    const px = camera.pos[0];
    const pz = camera.pos[2];
    // serverPos may be [x, height, z] (main.mjs) or [x, z] (test doubles);
    // the z component is the last element either way.
    const srvX = Array.isArray(world.serverPos) ? world.serverPos[0] : px;
    const srvZ = Array.isArray(world.serverPos)
        ? world.serverPos[world.serverPos.length - 1]
        : pz;
    const syncDrift = Array.isArray(world.serverPos)
        ? round3(Math.hypot(srvX - px, srvZ - pz))
        : 0;
    const self = {
        x: px,
        y: camera.pos[1],
        z: pz,
        serverX: round3(srvX),
        serverZ: round3(srvZ),
        syncDrift,
        yaw: camera.yaw,
        pitch: camera.pitch,
        hp: world.hp,
        maxHp: world.maxHp,
        element: world.playerElement,
        elementName: ELEMENT_NAMES[world.playerElement] || 'Fire',
        phase: world.phase,
        intensity: world.intensity,
    };

    const actors = [];
    for (const [id, buf] of world.actors) {
        if (buf.hp <= 0) continue;
        const x = buf.pos[0];
        const z = buf.pos[2];
        const proj = projectToScreen(camera, x, 0.5, z);
        const los = losClear(world.maze, px, pz, x, z);
        actors.push({
            id,
            x,
            y: buf.pos[1],
            z,
            hp: buf.hp,
            type: buf.type | 0,
            typeName: ARCHETYPE_NAMES[buf.type | 0] || 'brute',
            element: buf.element | 0,
            elementName: ELEMENT_NAMES[buf.element | 0] || 'Fire',
            stunned: !!buf.stunned,
            strategy: buf.strategy | 0,
            strategyName: STRATEGY_NAMES[buf.strategy | 0] || 'patrol',
            dist: Math.hypot(x - px, z - pz),
            los,
            inFrustum: proj.inFrustum,
            screenX: proj.screenX,
            screenY: proj.screenY,
            perceivable: Boolean(los && proj.inFrustum),
        });
    }
    actors.sort((a, b) => a.dist - b.dist);

    // The PA-BT HUD knows which action each actor's brain is currently
    // running. Surfacing it lets a bot reason about intent, not just
    // position.
    const btById = new Map();
    for (const row of world.btRows || []) {
        btById.set(row.id, row);
    }
    for (const a of actors) {
        const row = btById.get(a.id);
        a.btAction = row ? (row.active || '') : '';
        a.btStatus = row ? (row.status || '') : '';
        a.btAdvantage = row ? (row.advantage || 0) : 0;
    }

    const maze = world.maze;
    return {
        frame: world.frame,
        time: world.time,
        round: world.round,
        self,
        gate: maze ? { x: maze.gateX, z: maze.gateY } : { x: 0, z: 0 },
        start: maze ? { x: maze.startX, z: maze.startY } : { x: 0, z: 0 },
        actors,
        powerups: (world.powerUps || []).map((p) => ({
            x: p.x, z: p.y, element: p.element | 0,
            elementName: ELEMENT_NAMES[p.element | 0] || 'Fire',
        })),
        hazards: (world.hazards || []).map((h) => ({
            x: h.x, z: h.y, element: h.element | 0, ttl: h.ttl,
        })),
        territory: (world.territory || []).slice(),
        maze: maze ? {
            w: maze.w,
            h: maze.h,
            grid: maze.grid,
            start: { x: maze.startX, z: maze.startY },
            gate: { x: maze.gateX, z: maze.gateY },
            walkable: (x, z) => walkable(maze, Math.floor(x), Math.floor(z)),
            cell: (x, z) => {
                if (!maze || x < 0 || z < 0 || x >= maze.w || z >= maze.h) return CELL_WALL;
                return maze.grid[z * maze.w + x];
            },
        } : null,
        shotsFired: world.shotsFired | 0,
    };
}

// newCommand returns a fresh command accumulator. The accumulator holds the
// bot's intent in world space; the runtime projects it onto the client's
// yaw-relative input axes once the final facing is known.
export function newCommand() {
    return {
        move: { x: 0, z: 0 },
        yaw: null,
        pitch: null,
        sprint: false,
        fire: false,
        jump: false,
        note: '',
    };
}

// projectMove converts a world-space move vector into the client's
// yaw-relative forward/strafe pair.
export function projectMove(move, yaw) {
    const s = Math.sin(yaw);
    const c = Math.cos(yaw);
    return {
        forward: move.x * s + move.z * c,
        strafe: -move.x * c + move.z * s,
    };
}

function clamp(v, lo, hi) {
    return v < lo ? lo : (v > hi ? hi : v);
}

// STUCK_MS is how long a bot may press against the same waypoint without
// making progress before the SDK skips that waypoint. Without the escape a
// body that cannot round a corner stays wedged there forever.
const STUCK_MS = 400;

// createBotAPI builds the per-tick api object handed to bot code. `cmd` is
// the command accumulator the bot mutates through the action verbs, and
// `state` persists across ticks so a bot can remember where it was.
export function createBotAPI(world, cmd, obs, state = {}) {
    const api = {
        // ---- orientation -------------------------------------------------
        get frame() { return obs.frame; },
        get time() { return obs.time; },
        get round() { return obs.round; },
        get self() { return obs.self; },
        get gate() { return obs.gate; },
        get start() { return obs.start; },
        get actors() { return obs.actors; },
        get powerups() { return obs.powerups; },
        get hazards() { return obs.hazards; },
        get territory() { return obs.territory; },
        get maze() { return obs.maze; },
        get shotsFired() { return obs.shotsFired; },
        get finished() { return !!world.finished; },

        // ---- actions -----------------------------------------------------
        aimAt(x, z) {
            // The server raycasts pulses from its own authoritative position,
            // so when the client has drifted we aim from where the server
            // believes we stand, not from where the camera does.
            const ox = (obs.self.syncDrift > 0.08 && typeof obs.self.serverX === 'number')
                ? obs.self.serverX
                : obs.self.x;
            const oz = (obs.self.syncDrift > 0.08 && typeof obs.self.serverZ === 'number')
                ? obs.self.serverZ
                : obs.self.z;
            cmd.yaw = Math.atan2(x - ox, z - oz);
            return cmd.yaw;
        },
        look(yaw, pitch) {
            cmd.yaw = yaw;
            if (pitch !== undefined) cmd.pitch = pitch;
        },
        moveTo(tx, tz, opts = {}) {
            const arrive = opts.arrive !== undefined ? opts.arrive : 0.35;
            const sx = obs.self.x;
            const sz = obs.self.z;
            const direct = Math.hypot(tx - sx, tz - sz);
            if (direct <= arrive) {
                cmd.move.x = 0;
                cmd.move.z = 0;
                state.waypointKey = null;
                state.bestDist = Infinity;
                state.lastProgressAt = 0;
                return { reached: true, distance: direct, waypoint: null };
            }
            const path = pathTo(world.maze, sx, sz, tx, tz, opts.allowGap === true);
            if (path.length === 0) {
                // The goal is unreachable over walkable cells. Steer straight
                // at it so the bot still makes visual progress, and let the
                // pathfinding helpers report the blockage.
                cmd.move.x = (tx - sx) / direct;
                cmd.move.z = (tz - sz) / direct;
                return { reached: false, distance: direct, waypoint: null, unreachable: true };
            }

            let idx = 0;
            while (idx < path.length - 1 && Math.hypot(path[idx][0] - sx, path[idx][1] - sz) <= arrive) {
                idx++;
            }
            state.waypointIndex = idx;
            const target = path[idx];
            const dx = target[0] - sx;
            const dz = target[1] - sz;
            const len = Math.hypot(dx, dz);
            if (len < 1e-6) {
                cmd.move.x = 0;
                cmd.move.z = 0;
                return { reached: false, distance: direct, waypoint: target };
            }
            cmd.move.x = dx / len;
            cmd.move.z = dz / len;
            if (opts.sprint !== undefined) cmd.sprint = !!opts.sprint;
            return { reached: false, distance: direct, waypoint: target };
        },
        moveWorld(dx, dz, scale = 1) {
            const len = Math.hypot(dx, dz);
            if (len < 1e-6) {
                cmd.move.x = 0;
                cmd.move.z = 0;
                return;
            }
            cmd.move.x = (dx / len) * clamp(scale, -1, 1);
            cmd.move.z = (dz / len) * clamp(scale, -1, 1);
        },
        moveAwayFrom(tx, tz, opts = {}) {
            const keep = opts.keep !== undefined ? opts.keep : 3.0;
            const sx = obs.self.x;
            const sz = obs.self.z;
            const dx = sx - tx;
            const dz = sz - tz;
            const d = Math.hypot(dx, dz);
            if (d < 1e-6) {
                cmd.move.x = 0;
                cmd.move.z = 0;
                return d;
            }
            if (d > keep) {
                // At or beyond the standoff distance: hold the ground and
                // keep facing the threat.
                cmd.move.x = 0;
                cmd.move.z = 0;
                return d;
            }
            cmd.move.x = dx / d;
            cmd.move.z = dz / d;
            return d;
        },
        orbit(tx, tz, opts = {}) {
            const radius = opts.radius !== undefined ? opts.radius : 3.5;
            const sign = opts.clockwise === false ? -1 : 1;
            const sx = obs.self.x;
            const sz = obs.self.z;
            const dx = tx - sx;
            const dz = tz - sz;
            const d = Math.hypot(dx, dz) || 1e-6;
            const ux = dx / d;
            const uz = dz / d;
            const px = -uz * sign;
            const pz = ux * sign;
            const radial = clamp((d - radius) / 1.5, -1, 1);
            let mx = ux * radial + px * 0.8;
            let mz = uz * radial + pz * 0.8;
            const len = Math.hypot(mx, mz) || 1;
            cmd.move.x = mx / len;
            cmd.move.z = mz / len;
            return d;
        },
        stop() {
            cmd.move.x = 0;
            cmd.move.z = 0;
        },
        fire(on = true) {
            cmd.fire = !!on;
        },
        sprint(on = true) {
            cmd.sprint = !!on;
        },
        jump() {
            cmd.jump = true;
        },
        note(text) {
            cmd.note = String(text);
        },
        log(...args) {
            world.botLog.push({
                frame: obs.frame,
                t: obs.time,
                message: args.map((a) => (typeof a === 'string' ? a : JSON.stringify(a))).join(' '),
            });
        },
        finish(reason) {
            world.finished = true;
            world.finishReason = String(reason || '');
        },

        // ---- queries -----------------------------------------------------
        command() {
            return {
                move: { x: cmd.move.x, z: cmd.move.z },
                yaw: cmd.yaw,
                pitch: cmd.pitch,
                sprint: cmd.sprint,
                fire: cmd.fire,
                jump: cmd.jump,
                note: cmd.note,
            };
        },
        path(from, to, allowGap = false) {
            return pathTo(world.maze, from.x, from.z, to.x, to.z, allowGap);
        },
        los(x1, z1, x2, z2) {
            return losClear(world.maze, x1, z1, x2, z2);
        },
        nearest(filter = {}) {
            const f = typeof filter === 'function' ? { test: filter } : filter;
            let best = null;
            for (const a of obs.actors) {
                if (f.type !== undefined && a.type !== f.type) continue;
                if (f.element !== undefined && a.element !== f.element) continue;
                if (f.excludeStunned && a.stunned) continue;
                if (f.visibleOnly && !a.los) continue;
                if (f.maxDist !== undefined && a.dist > f.maxDist) continue;
                if (f.minDist !== undefined && a.dist < f.minDist) continue;
                if (f.test && !f.test(a)) continue;
                if (!best || a.dist < best.dist) best = a;
            }
            return best;
        },
        perceivable(opts = {}) {
            return this.nearest({
                ...opts,
                visibleOnly: true,
                test: (a) => a.perceivable && (!opts.test || opts.test(a)),
            });
        },
        ofType(type) {
            return obs.actors.filter((a) => a.type === type);
        },
        beats(element) {
            return advantage(obs.self.element, element) === 2;
        },
        losesTo(element) {
            return advantage(obs.self.element, element) === 0.5;
        },
        advantageAgainst(element) {
            return advantage(obs.self.element, element);
        },
    };
    return api;
}

function nowMs() {
    return typeof performance !== 'undefined' && performance.now
        ? performance.timeOrigin + performance.now()
        : Date.now();
}
