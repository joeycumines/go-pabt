// pilot.mjs - plays the game the way a person with the minimap would: plan a
// route over the maze, turn with the mouse while walking continuously with W,
// strafe and kite in combat, and fire with a click.
//
// What the pilot may READ: its own position/yaw, HP, phase, the maze grid
// and perceivable actors (on-screen FOV+LOS or within 6.0u minimap radar) -
// all things the player sees on screen or on the minimap. What it may WRITE:
// nothing. Every action is a real key/mouse event through Actuator. Gap cells
// are NOT avoided: they render as rifts that a player can cross, and so does
// the pilot.

import { Grid, wrapAngle, GAP, WALL } from './maze.mjs';

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
// couple of smooth passes (as a hand would), and returns the final error.
export async function faceYaw(act, page, target, { tol = 0.04, passes = 4 } = {}) {
    let p = await readPlayer(page);
    for (let i = 0; i < passes && p; i++) {
        const err = wrapAngle(target - p.yaw);
        if (Math.abs(err) <= tol) return err;
        const step = Math.max(-0.9, Math.min(0.9, err));
        await act.look(-step / RAD_PER_PX, 0, { steps: Math.max(3, Math.ceil(Math.abs(step) / 0.18)), stepMs: 8 });
        await act.wait(35);
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
    let idx = 0;
    while (idx + 1 < path.length && Math.hypot(path[idx][0] - pos[0], path[idx][1] - pos[2]) < 0.38) {
        idx++;
    }
    let best = path[idx];
    for (let i = idx + 1; i < Math.min(path.length, idx + 4); i++) {
        const [tx, tz] = path[i];
        const d = Math.hypot(tx - pos[0], tz - pos[2]);
        if (d < 1e-4) continue;
        const nx = -(tz - pos[2]) / d * 0.3, nz = (tx - pos[0]) / d * 0.3;
        const clear = [[0, 0], [nx, nz], [-nx, -nz]].every(([ox, oz]) => !grid.segment(pos[0] + ox, pos[2] + oz, tx + ox, tz + oz).wall);
        if (!clear) break;
        best = path[i];
    }
    return best;
}

// bestStrafeKey picks the lateral direction ('a' or 'd') with more wall
// clearance from the player's current position and heading.
export function bestStrafeKey(grid, pos, yaw, prefer = 'd') {
    const rx = -Math.cos(yaw), rz = Math.sin(yaw); // strafe-right vector
    const rightRoom = grid.castRay(pos[0], pos[2], rx, rz).dist;
    const leftRoom = grid.castRay(pos[0], pos[2], -rx, -rz).dist;
    if (Math.abs(rightRoom - leftRoom) < 0.25) return prefer;
    return rightRoom > leftRoom ? 'd' : 'a';
}

// walkTo pilots toward world point (gx, gz) until within `arrive`, the phase
// leaves 'playing', or `timeoutMs` elapses. Holds W continuously through
// corridor turns up to 1.25 rad (steering smoothly with the mouse), only
// releasing W for sharp hairpin reversals (> 1.25 rad) or when unsticking.
// When `defend` is true, briefly fires and sidesteps if a visible enemy
// blocks the corridor ahead within `defendRange`.
export async function walkTo(act, page, grid, gx, gz, {
    arrive = 0.4, timeoutMs = 120000, sprint = false, stopWhen = null,
    defend = false, defendRange = 4.2, visited = null,
} = {}) {
    const t0 = Date.now();
    let path = null, pathFrom = null;
    let lastProgressT = Date.now(), bestDist = Infinity;
    let lastDefenseT = 0;
    const stuck = [];
    try {
        while (Date.now() - t0 < timeoutMs) {
            const p = await readPlayer(page);
            if (!p) { await act.wait(50); continue; }
            if (p.gs !== 'playing') return { result: p.gs, stuck, ms: Date.now() - t0 };
            if (visited) visited.set(`${Math.floor(p.pos[0])},${Math.floor(p.pos[2])}`, Date.now());
            if (stopWhen && stopWhen(p)) return { result: 'stopWhen', stuck, ms: Date.now() - t0 };
            const dGoal = Math.hypot(gx - p.pos[0], gz - p.pos[2]);
            if (dGoal <= arrive) return { result: 'arrived', stuck, ms: Date.now() - t0 };

            if (defend && Date.now() - lastDefenseT > 950) {
                const blockers = visibleActors(grid, p, defendRange).filter((a) => {
                    if (a.st) return false;
                    const ang = Math.abs(wrapAngle(yawTo(p.pos[0], p.pos[2], a.x, a.z) - p.yaw));
                    return ang < 0.65 || a.d < 2.2;
                });
                if (blockers.length) {
                    const b = blockers[0];
                    const strafeKey = bestStrafeKey(grid, p.pos, p.yaw, 'd');
                    await act.down('w');
                    await act.down(strafeKey);
                    await faceYaw(act, page, yawTo(p.pos[0], p.pos[2], b.x, b.z), { tol: 0.05, passes: 3 });
                    await act.click();
                    await act.wait(160);
                    await act.up(strafeKey);
                    lastDefenseT = Date.now();
                    path = null;
                    continue;
                }
            }

            const cell = `${Math.floor(p.pos[0])},${Math.floor(p.pos[2])}`;
            if (!path || pathFrom !== cell) {
                path = grid.path(p.pos[0], p.pos[2], gx, gz);
                pathFrom = cell;
                if (!path) return { result: 'no-path', stuck, ms: Date.now() - t0 };
                if (path.length) path[path.length - 1] = [gx, gz];
                else path = [[gx, gz]];
            }
            const [tx, tz] = pickTarget(grid, p.pos, path);
            const targetYaw = yawTo(p.pos[0], p.pos[2], tx, tz);
            const err = wrapAngle(targetYaw - p.yaw);

            await act.down('w');
            if (sprint && Math.abs(err) < 0.4) await act.down('Shift');
            else await act.up('Shift');
            if (Math.abs(err) > 0.025) {
                const turnStep = Math.max(-0.65, Math.min(0.65, err * 0.8));
                await act.look(-turnStep / RAD_PER_PX, 0, { steps: 3, stepMs: 4 });
            }

            if (dGoal < bestDist - 0.2) { bestDist = dGoal; lastProgressT = Date.now(); }
            if (Date.now() - lastProgressT > 2200) {
                stuck.push({ t: Date.now(), pos: p.pos, cell, target: [tx, tz] });
                act.s.mark('pilot-stuck', { pos: p.pos, target: [tx, tz] });
                await act.releaseAll();
                await act.hold('s', 250);
                await act.hold(bestStrafeKey(grid, p.pos, p.yaw, stuck.length % 2 ? 'a' : 'd'), 250);
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
// range, line of sight not blocked by a wall.
export function visibleActors(grid, p, range = 11) {
    return (p.actors || [])
        .filter((a) => a.hp > 0)
        .map((a) => ({ ...a, d: Math.hypot(a.x - p.pos[0], a.z - p.pos[2]) }))
        .filter((a) => a.d <= range && a.d > 0.35 && !grid.segment(p.pos[0], p.pos[2], a.x, a.z).wall)
        .sort((a, b) => a.d - b.d);
}

// engage turns onto a visible actor with the mouse and clicks to fire up to
// `shots` times while actively strafing and moving laterally between shots.
// When kite is true (default), it maintains standoff distance by backing up
// when closer than 2.6u. When kite is false (e.g. J4 seeking danger to be
// overwhelmed), it stands its ground laterally without backpedaling.
export async function engage(act, page, grid, actorId, { shots = 4, kite = true } = {}) {
    const fired = [];
    let preferStrafe = 'd';
    let heldDepth = null;
    try {
        for (let i = 0; i < shots; i++) {
            const p = await readPlayer(page);
            if (!p || p.gs !== 'playing') break;
            const a = (p.actors || []).find((x) => x.id === actorId);
            if (!a || a.hp <= 0) break;
            const seg = grid.segment(p.pos[0], p.pos[2], a.x, a.z);
            if (seg.wall) break;

            // Situational awareness: if another enemy has closed to point-blank range
            // (< 2.2u) while our current target is further away (> 3.0u), break out so
            // the pilot can react to the closer threat.
            if (i > 0 && seg.dist > 3.0) {
                const threat = (p.actors || []).find((x) => x.hp > 0 && x.id !== actorId && Math.hypot(x.x - p.pos[0], x.z - p.pos[2]) < 2.2);
                if (threat) break;
            }

            const aimYaw = yawTo(p.pos[0], p.pos[2], a.x, a.z);
            const strafeKey = bestStrafeKey(grid, p.pos, aimYaw, preferStrafe);
            preferStrafe = strafeKey === 'd' ? 'a' : 'd';
            const depthKey = kite
                ? (seg.dist < 2.6 ? 's' : seg.dist > 5.8 ? 'w' : null)
                : (seg.dist > 5.5 ? 'w' : null);
            if (heldDepth && heldDepth !== depthKey) {
                await act.up(heldDepth);
                heldDepth = null;
            }
            if (depthKey && heldDepth !== depthKey) {
                await act.down(depthKey);
                heldDepth = depthKey;
            }

            await act.down(strafeKey);

            const err = await faceYaw(act, page, aimYaw, { tol: 0.035, passes: 3 });
            const q = await readPlayer(page);
            if (!q || q.gs !== 'playing') break;
            const aNow = (q.actors || []).find((x) => x.id === actorId);
            if (aNow && aNow.hp > 0 && !grid.segment(q.pos[0], q.pos[2], aNow.x, aNow.z).wall) {
                const fineErr = wrapAngle(yawTo(q.pos[0], q.pos[2], aNow.x, aNow.z) - q.yaw);
                if (Math.abs(fineErr) > 0.025) {
                    await act.look(-fineErr / RAD_PER_PX, 0, { steps: 2, stepMs: 4 });
                }
            }
            const r = await readPlayer(page);
            await act.click();
            fired.push({
                t: Date.now(), actorId, dist: seg.dist,
                gapOnLine: seg.gap, standingOnGap: seg.startGap,
                yawErr: err, pos: (r && r.pos) || (q && q.pos),
            });

            // Continue strafing/kiting during the 0.5s weapon cooldown so the
            // player is moving and dodging rather than standing frozen.
            await act.wait(320);
            await act.up(strafeKey);
            await act.wait(220);
        }
    } finally {
        await act.releaseAll();
    }
    return fired;
}

// exploreCorridors picks an unvisited (or least-recently visited) reachable
// floor cell a few steps away and walks there, stopping early as soon as any
// enemy comes into perception range.
export async function exploreCorridors(act, page, grid, visited, {
    timeoutMs = 5500, sprint = false, stopOnRadar = false,
} = {}) {
    const p = await readPlayer(page);
    if (!p || p.gs !== 'playing') return { result: p ? p.gs : 'no-player' };
    const cx = Math.floor(p.pos[0]), cz = Math.floor(p.pos[2]);
    visited.set(`${cx},${cz}`, Date.now());
    const fx = Math.sin(p.yaw), fz = Math.cos(p.yaw);

    let best = null, bestScore = -Infinity;
    for (let z = 1; z < grid.h - 1; z++) {
        for (let x = 1; x < grid.w - 1; x++) {
            const c = grid.at(x, z);
            if (c === WALL || c === GAP) continue;
            const d = Math.hypot(x + 0.5 - p.pos[0], z + 0.5 - p.pos[2]);
            if (d < 3.0 || d > 10.5) continue;
            const path = grid.path(p.pos[0], p.pos[2], x + 0.5, z + 0.5);
            if (!path || path.length < 2 || path.length > 14) continue;
            const key = `${x},${z}`;
            const lastSeen = visited.get(key) || 0;
            const ageBonus = lastSeen === 0 ? 50 : Math.min(30, (Date.now() - lastSeen) / 1000);
            const headingDot = ((x + 0.5 - p.pos[0]) * fx + (z + 0.5 - p.pos[2]) * fz) / d;
            const score = ageBonus + headingDot * 6 - Math.abs(path.length - 6) * 1.2;
            if (score > bestScore) {
                bestScore = score;
                best = [x + 0.5, z + 0.5];
            }
        }
    }
    if (!best) best = grid.gate;
    return walkTo(act, page, grid, best[0], best[1], {
        timeoutMs, arrive: 0.7, sprint, visited,
        stopWhen: (q) => {
            if (visibleActors(grid, q, 10).length > 0) return true;
            if (stopOnRadar && (q.actors || []).some((a) => a.hp > 0)) return true;
            return false;
        },
    });
}

export function gapCellsOnPath(grid, path) {
    return (path || []).filter(([x, z]) => grid.at(x, z) === GAP).length;
}
