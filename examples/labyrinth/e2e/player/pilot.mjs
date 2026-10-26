// pilot.mjs - plays the game the way a person with the minimap would: plan a
// route over the maze, turn with the mouse, walk with W, fire with a click.
//
// What the pilot may READ: its own position/yaw, HP, phase, the maze grid
// and visible-by-line-of-sight actors - all things the player sees on screen
// or on the minimap. What it may WRITE: nothing. Every action is a real
// key/mouse event through Actuator. Gap cells are NOT avoided: they render as
// floor, so a player walks onto them, and so does the pilot.

import { Grid, wrapAngle, GAP } from './maze.mjs';

export const RAD_PER_PX = 0.002; // camera.mjs mouse sensitivity, as a player calibrates it

export async function readPlayer(page) {
    return page.evaluate(() => {
        const r = window.__pe && window.__pe.read();
        if (!r) return null;
        delete r.grid;
        return r;
    });
}

export async function readGrid(page) {
    const g = await page.evaluate(() => { const r = window.__pe && window.__pe.read(); return r && r.grid; });
    if (!g) return null;
    const grid = new Grid(g.w, g.h, g.cells);
    grid.gate = g.gate;
    return grid;
}

// faceYaw turns the camera to an absolute yaw with real mouse movement, in a
// couple of corrective passes (as a hand would), and returns the final error.
export async function faceYaw(act, page, target, { tol = 0.04, passes = 4 } = {}) {
    let p = await readPlayer(page);
    for (let i = 0; i < passes && p; i++) {
        const err = wrapAngle(target - p.yaw);
        if (Math.abs(err) <= tol) return err;
        await act.look(-err / RAD_PER_PX, 0);
        await act.wait(40);
        p = await readPlayer(page);
    }
    return p ? wrapAngle(target - p.yaw) : NaN;
}

export function yawTo(fromX, fromZ, toX, toZ) {
    return Math.atan2(toX - fromX, toZ - fromZ);
}

// pickTarget chooses the farthest of the next few waypoints that a straight
// walk can reach without brushing a wall (checked along both shoulders).
function pickTarget(grid, pos, path) {
    let best = path[0];
    for (let i = 1; i < Math.min(path.length, 4); i++) {
        const [tx, tz] = path[i];
        const d = Math.hypot(tx - pos[0], tz - pos[2]);
        const nx = -(tz - pos[2]) / d * 0.3, nz = (tx - pos[0]) / d * 0.3;
        const clear = [[0, 0], [nx, nz], [-nx, -nz]].every(([ox, oz]) => !grid.segment(pos[0] + ox, pos[2] + oz, tx + ox, tz + oz).wall);
        if (!clear) break;
        best = path[i];
    }
    return best;
}

// walkTo pilots toward world point (gx, gz) until within `arrive`, the phase
// leaves 'playing', or `timeoutMs` elapses. Returns a summary including any
// stuck episodes (no progress for 2s while walking).
export async function walkTo(act, page, grid, gx, gz, { arrive = 0.4, timeoutMs = 120000, sprint = false, stopWhen = null } = {}) {
    const t0 = Date.now();
    let path = null, pathFrom = null;
    let lastProgressT = Date.now(), bestDist = Infinity;
    const stuck = [];
    try {
        while (Date.now() - t0 < timeoutMs) {
            const p = await readPlayer(page);
            if (!p) { await act.wait(50); continue; }
            if (p.gs !== 'playing') return { result: p.gs, stuck, ms: Date.now() - t0 };
            if (stopWhen && stopWhen(p)) return { result: 'stopWhen', stuck, ms: Date.now() - t0 };
            const dGoal = Math.hypot(gx - p.pos[0], gz - p.pos[2]);
            if (dGoal <= arrive) return { result: 'arrived', stuck, ms: Date.now() - t0 };
            const cell = `${Math.floor(p.pos[0])},${Math.floor(p.pos[2])}`;
            if (!path || pathFrom !== cell) {
                path = grid.path(p.pos[0], p.pos[2], gx, gz);
                pathFrom = cell;
                if (!path) return { result: 'no-path', stuck, ms: Date.now() - t0 };
                if (path.length) path[path.length - 1] = [gx, gz];
                else path = [[gx, gz]];
            }
            const [tx, tz] = pickTarget(grid, p.pos, path);
            const err = wrapAngle(yawTo(p.pos[0], p.pos[2], tx, tz) - p.yaw);
            if (Math.abs(err) > 0.03) await act.look(-Math.max(-0.6, Math.min(0.6, err * 0.8)) / RAD_PER_PX, 0, { stepMs: 4 });
            if (Math.abs(err) < 0.45) {
                await act.down('w');
                if (sprint) await act.down('Shift');
            } else {
                await act.up('w');
                await act.up('Shift');
            }
            if (dGoal < bestDist - 0.2) { bestDist = dGoal; lastProgressT = Date.now(); }
            if (Date.now() - lastProgressT > 2000) {
                stuck.push({ t: Date.now(), pos: p.pos, cell, target: [tx, tz] });
                act.s.mark('pilot-stuck', { pos: p.pos, target: [tx, tz] });
                await act.releaseAll();
                await act.hold('s', 250);
                await act.hold(stuck.length % 2 ? 'a' : 'd', 250);
                lastProgressT = Date.now();
                bestDist = Infinity;
                path = null;
            }
            await act.wait(30);
        }
        return { result: 'timeout', stuck, ms: Date.now() - t0 };
    } finally {
        await act.releaseAll();
    }
}

// visibleActors returns live actors the player could actually see: within
// range, line of sight not blocked by a wall. Gap cells do not block SIGHT
// (they look like floor) - that distinction is exactly what lets the D8
// detector catch shots the server silently rejects.
export function visibleActors(grid, p, range = 11) {
    return p.actors
        .filter((a) => a.hp > 0)
        .map((a) => ({ ...a, d: Math.hypot(a.x - p.pos[0], a.z - p.pos[2]) }))
        .filter((a) => a.d <= range && a.d > 0.4 && !grid.segment(p.pos[0], p.pos[2], a.x, a.z).wall)
        .sort((a, b) => a.d - b.d);
}

// engage turns onto a visible actor with the mouse and clicks to fire, up to
// `shots` times, honouring the weapon's 0.5s cooldown like a player would.
export async function engage(act, page, grid, actorId, { shots = 4 } = {}) {
    const fired = [];
    for (let i = 0; i < shots; i++) {
        const p = await readPlayer(page);
        if (!p || p.gs !== 'playing') break;
        const a = p.actors.find((x) => x.id === actorId);
        if (!a || a.hp <= 0) break;
        const seg = grid.segment(p.pos[0], p.pos[2], a.x, a.z);
        if (seg.wall) break;
        const err = await faceYaw(act, page, yawTo(p.pos[0], p.pos[2], a.x, a.z), { tol: 0.03 });
        const q = await readPlayer(page);
        await act.click();
        fired.push({ t: Date.now(), actorId, dist: seg.dist, gapOnLine: seg.gap, standingOnGap: seg.startGap, yawErr: err, pos: q && q.pos });
        await act.wait(560);
    }
    return fired;
}

export function gapCellsOnPath(grid, path) {
    return (path || []).filter(([x, z]) => grid.at(x, z) === GAP).length;
}
