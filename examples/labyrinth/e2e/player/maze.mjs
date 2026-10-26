// maze.mjs - grid geometry the harness computes INDEPENDENTLY of the renderer.
//
// The grid is the one the server sent the client (row-major, index z*w+x):
// 0 wall, 1 floor, 2 platform, 3 gap. World X maps to the column, world Z to
// the row. Detectors use this as ground truth to ask "what should the player
// be able to see / reach / hit" and compare it with what the pixels and the
// HUD actually showed.

export const WALL = 0, FLOOR = 1, PLATFORM = 2, GAP = 3;

export class Grid {
    constructor(w, h, cells) {
        if (!Array.isArray(cells) && !(cells instanceof Uint8Array)) throw new Error('grid cells missing');
        if (cells.length !== w * h) throw new Error(`grid size ${cells.length} != ${w}x${h}`);
        this.w = w; this.h = h; this.cells = cells;
    }

    cell(cx, cz) {
        if (cx < 0 || cz < 0 || cx >= this.w || cz >= this.h) return WALL;
        return this.cells[cz * this.w + cx];
    }

    at(x, z) { return this.cell(Math.floor(x), Math.floor(z)); }

    // Physical walkability for the player: the client and server collision
    // both treat only walls as solid.
    passable(cx, cz) { return this.cell(cx, cz) !== WALL; }

    // castRay walks the grid from (x,z) along (dx,dz) (unit vector) with a DDA
    // and returns the distance to the first wall face plus every cell the ray
    // crossed before it ({cx, cz, type, dist}). maxDist bounds the walk.
    castRay(x, z, dx, dz, maxDist = 40) {
        let cx = Math.floor(x), cz = Math.floor(z);
        const stepX = dx > 0 ? 1 : -1, stepZ = dz > 0 ? 1 : -1;
        const tDeltaX = dx !== 0 ? Math.abs(1 / dx) : Infinity;
        const tDeltaZ = dz !== 0 ? Math.abs(1 / dz) : Infinity;
        let tMaxX = dx !== 0 ? (dx > 0 ? cx + 1 - x : x - cx) * tDeltaX : Infinity;
        let tMaxZ = dz !== 0 ? (dz > 0 ? cz + 1 - z : z - cz) * tDeltaZ : Infinity;
        const crossed = [];
        let t = 0;
        while (t <= maxDist) {
            if (tMaxX < tMaxZ) { t = tMaxX; tMaxX += tDeltaX; cx += stepX; } else { t = tMaxZ; tMaxZ += tDeltaZ; cz += stepZ; }
            const type = this.cell(cx, cz);
            if (type === WALL) return { dist: t, hit: { cx, cz }, crossed };
            crossed.push({ cx, cz, type, dist: t });
        }
        return { dist: Infinity, hit: null, crossed };
    }

    // segment classifies the straight segment between two points: whether a
    // wall blocks it, and whether a gap (which RENDERS as floor but the server
    // treats as opaque to shots) lies on it - including the cell the segment
    // starts in, since a shooter standing on a gap fires through it.
    segment(x1, z1, x2, z2) {
        const dist = Math.hypot(x2 - x1, z2 - z1);
        const startGap = this.at(x1, z1) === GAP;
        if (dist < 1e-6) return { dist, wall: false, gap: startGap, startGap };
        const r = this.castRay(x1, z1, (x2 - x1) / dist, (z2 - z1) / dist, dist);
        const wall = r.dist < dist;
        const gap = startGap || r.crossed.some((c) => c.type === GAP && c.dist < dist);
        return { dist, wall, gap, startGap };
    }

    // path is A* over 4-connected passable cells, returning cell centres from
    // the start cell's successor to the goal cell, or null if unreachable.
    path(sx, sz, gx, gz) {
        const s = [Math.floor(sx), Math.floor(sz)], g = [Math.floor(gx), Math.floor(gz)];
        const key = (cx, cz) => cz * this.w + cx;
        const h = (cx, cz) => Math.abs(cx - g[0]) + Math.abs(cz - g[1]);
        const open = [[h(s[0], s[1]), 0, s[0], s[1]]];
        const came = new Map(), cost = new Map([[key(s[0], s[1]), 0]]);
        while (open.length) {
            let bi = 0;
            for (let i = 1; i < open.length; i++) if (open[i][0] < open[bi][0]) bi = i;
            const [, gc, cx, cz] = open.splice(bi, 1)[0];
            if (cx === g[0] && cz === g[1]) {
                const out = [];
                let k = key(cx, cz);
                while (k !== key(s[0], s[1])) {
                    out.push([(k % this.w) + 0.5, Math.floor(k / this.w) + 0.5]);
                    k = came.get(k);
                }
                return out.reverse();
            }
            for (const [ox, oz] of [[1, 0], [-1, 0], [0, 1], [0, -1]]) {
                const nx = cx + ox, nz = cz + oz;
                if (!this.passable(nx, nz)) continue;
                const nk = key(nx, nz), nc = gc + 1;
                if (nc < (cost.get(nk) ?? Infinity)) {
                    cost.set(nk, nc); came.set(nk, key(cx, cz));
                    open.push([nc + h(nx, nz), nc, nx, nz]);
                }
            }
        }
        return null;
    }
}

// The client's camera basis, as read from client/js/main.mjs update(): the
// player looks along (sin yaw, cos yaw). The detectors deliberately do NOT
// rely on this for input-direction checks (those compare measured motion
// against measured motion); the pilot uses it only to steer.
export function forwardOf(yaw) { return [Math.sin(yaw), Math.cos(yaw)]; }

export function wrapAngle(a) {
    while (a > Math.PI) a -= 2 * Math.PI;
    while (a < -Math.PI) a += 2 * Math.PI;
    return a;
}
