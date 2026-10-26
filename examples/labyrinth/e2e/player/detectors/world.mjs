// world.mjs - D8 (is the world physically sane, and do shots at visible
// enemies land?), D9 (is the flow fair: start, end, restart?), D10 (did the
// page throw, log errors, or lose its connection?).

import { finding, playing, sampleAt, between, marksOf, eventTimes, fmt, segments } from './common.mjs';
import { WALL, Grid } from '../maze.mjs';

// D8a: actors must never stand inside walls or outside the maze.
export function detectEmbeddedActors(tl, grid) {
    if (!grid) return [];
    const ps = playing(tl);
    const since = new Map();
    const bad = new Map();
    for (const s of ps) {
        const seen = new Set();
        for (const a of s.actors || []) {
            if (a.hp <= 0) continue;
            const inWall = grid.at(a.x, a.z) === WALL;
            const out = a.x < 0 || a.z < 0 || a.x >= grid.w || a.z >= grid.h;
            if (!inWall && !out) continue;
            seen.add(a.id);
            if (!since.has(a.id)) since.set(a.id, s.t);
            if (s.t - since.get(a.id) >= 300 && !bad.has(a.id)) bad.set(a.id, { t: since.get(a.id), x: a.x, z: a.z, out });
        }
        for (const id of since.keys()) if (!seen.has(id)) since.delete(id);
    }
    if (!bad.size) return [];
    const first = [...bad.values()][0];
    return [finding('D8', 'S2', 'Enemies stand inside walls', {
        t: first.t, evidence: { actors: [...bad.keys()], example: `(${fmt(first.x)}, ${fmt(first.z)})${first.out ? ' outside the maze' : ''}` },
    })];
}

// D8c: the player's camera must stay inside the maze and out of the walls.
// Collision keeps the player 0.3u from wall faces and the camera's near
// plane is 0.1u, so a camera inside a wall cell, outside the maze, or within
// 0.1u of a wall face means the player sees through or from inside geometry.
export function detectPlayerInWall(tl, grid) {
    if (!grid) return [];
    const ps = playing(tl).filter((s) => s.gs === 'playing');
    const ev = eventTimes(tl).filter((e) => e.type === 'hit' && e.t !== null);
    const runs = [];
    let run = null;
    const faceDist = (x, z) => {
        let best = Infinity;
        const cx = Math.floor(x), cz = Math.floor(z);
        for (let dz = -1; dz <= 1; dz++) {
            for (let dx = -1; dx <= 1; dx++) {
                if (grid.cell(cx + dx, cz + dz) !== WALL) continue;
                const nx = Math.max(cx + dx, Math.min(x, cx + dx + 1));
                const nz = Math.max(cz + dz, Math.min(z, cz + dz + 1));
                best = Math.min(best, Math.hypot(x - nx, z - nz));
            }
        }
        return best;
    };
    for (const s of ps) {
        const [x, z] = [s.pos[0], s.pos[2]];
        const outside = x < 0 || z < 0 || x >= grid.w || z >= grid.h;
        const inWall = !outside && grid.at(x, z) === WALL;
        const clip = !outside && !inWall && faceDist(x, z) < 0.1;
        const kind = outside ? 'outside the maze' : inWall ? 'inside a wall' : clip ? 'clipping a wall face' : null;
        if (kind) {
            if (!run || run.kind !== kind) { if (run) runs.push(run); run = { kind, t0: s.t, t1: s.t, pos: [x, z], spos: s.spos && [s.spos[0], s.spos[2]] }; }
            run.t1 = s.t;
        } else if (run) { runs.push(run); run = null; }
    }
    if (run) runs.push(run);
    const long = runs.filter((r) => r.t1 - r.t0 >= 100);
    if (!long.length) return [];
    const worst = long.find((r) => r.kind !== 'clipping a wall face') || long[0];
    const afterHit = long.filter((r) => ev.some((e) => r.t0 >= e.t - 200 && r.t0 <= e.t + 1500)).length;
    return [finding('D8', worst.kind === 'clipping a wall face' ? 'S2' : 'S1', `Player camera ends up ${worst.kind}`, {
        t: worst.t0, until: worst.t1,
        evidence: {
            episodes: long.length, kinds: [...new Set(long.map((r) => r.kind))], totalMs: Math.round(long.reduce((n, r) => n + r.t1 - r.t0, 0)),
            examplePos: worst.pos.map((v) => +v.toFixed(2)), exampleServerPos: worst.spos && worst.spos.map((v) => +v.toFixed(2)),
            episodesStartingWithin1_5sOfEnemyHit: afterHit,
        },
        detail: afterHit ? 'Most episodes start right after an enemy hit: contact knockback shoves the player into or through walls.' : '',
    })];
}

function gridForShots(tl) {
    if (!tl.grid) return null;
    return new Grid(tl.grid.w, tl.grid.h, tl.grid.cells);
}

// D8b: a shot the player aimed at a visible enemy (no wall between, inside
// weapon range, crosshair on it) should land. The server confirms a hit with
// a 'stunned' event. Each miss is attributed using the server's own position
// of the target (from the wire tap, when recorded):
//   - the shooter stands on a GAP cell (drawn like floor, opaque to shots);
//   - a GAP cell lies on the line to the target;
//   - the enemy is not where it is drawn (D17), so the player shot at a
//     picture of an enemy.
export function detectShots(tl) {
    const res = marksOf(tl, 'result').find((m) => Array.isArray(m.shots));
    if (!res || !res.shots.length) return [];
    const ev = eventTimes(tl).filter((e) => e.type === 'stunned' && e.t !== null);
    const grid = gridForShots(tl);
    const snaps = (tl.wire || []).filter((w) => w.kind === 'snap');
    const serverPosOf = (id, t) => {
        let snap = null;
        for (const w of snaps) { if (w.t > t) break; snap = w; }
        const m = snap && snap.m.find((x) => x[0] === id);
        return m ? [m[1], m[2]] : null;
    };
    const ps = playing(tl);
    const shots = res.shots.filter((s) => s.dist <= 11 && Math.abs(s.yawErr) <= 0.05).map((s) => {
        if (!grid || !s.pos) return s;
        const smp = sampleAt(ps, s.t);
        const a = smp && (smp.actors || []).find((x) => x.id === s.actorId);
        if (!a) return s;
        const real = serverPosOf(s.actorId, s.t);
        const offBy = real ? Math.hypot(real[0] - a.x, real[1] - a.z) : null;
        // Judge the line the server actually traces: to where the enemy IS.
        const tgt = real || [a.x, a.z];
        const seg = grid.segment(s.pos[0], s.pos[2], tgt[0], tgt[1]);
        return { ...s, gapOnLine: seg.gap, standingOnGap: seg.startGap, drawnOffBy: offBy };
    });
    // The server hits the NEAREST actor on the ray, which may not be the one
    // the pilot picked (a pack can stand in line), so any confirmed hit in
    // the window counts as the shot landing.
    const judged = shots.map((s) => ({ ...s, landed: ev.some((e) => e.t >= s.t - 50 && e.t <= s.t + 700) }));
    const misses = judged.filter((s) => !s.landed);
    const cause = (s) => (s.standingOnGap ? 'standingOnGap' : s.drawnOffBy !== null && s.drawnOffBy > 0.8 ? 'enemyNotWhereDrawn' : s.gapOnLine ? 'gapOnLine' : 'unexplained');
    const by = {};
    for (const m of misses) (by[cause(m)] = by[cause(m)] || []).push(m);
    const out = [];
    const gapMiss = [...(by.standingOnGap || []), ...(by.gapOnLine || [])];
    if (gapMiss.length) {
        const onGap = by.standingOnGap || [];
        const ex = onGap[0] || gapMiss[0];
        out.push(finding('D8', 'S2', onGap.length
            ? 'Standing on certain floor tiles makes every shot fail silently (gap cells look like floor but block shots)'
            : 'Shots at a visible enemy are silently blocked by floor that looks open (gap cells)', {
            t: ex.t,
            evidence: { blocked: gapMiss.length, whileStandingOnGap: onGap.length, acrossGapOnLine: (by.gapOnLine || []).length, ofShotsJudged: judged.length, exampleShooterPos: ex.pos && ex.pos.map((v) => +v.toFixed(2)) },
            detail: 'Gap cells render exactly like floor, but the server treats them as opaque to shots, including the one the player is standing on.',
        }));
    }
    const ghostMiss = by.enemyNotWhereDrawn || [];
    if (ghostMiss.length >= 2) {
        out.push(finding('D8', 'S2', 'Shots at enemies miss because the enemy is not really where it is drawn', {
            t: ghostMiss[0].t,
            evidence: { missed: ghostMiss.length, ofShotsJudged: judged.length, medianOffByU: fmt(ghostMiss.map((m) => m.drawnOffBy).sort((x, y) => x - y)[Math.floor(ghostMiss.length / 2)]) },
            detail: 'The player aimed at the enemy model on screen; the server has that enemy somewhere else (see D17), so the shot finds nothing.',
        }));
    }
    const unexplained = by.unexplained || [];
    const clear = judged.filter((s) => cause(s) === 'unexplained' || s.landed);
    if (clear.length >= 4 && unexplained.length / clear.length > 0.5) {
        out.push(finding('D8', 'S2', 'Most shots at a clearly visible, centred enemy do not register', {
            t: unexplained[0].t, evidence: { missed: unexplained.length, of: clear.length, examples: unexplained.slice(0, 4).map((s) => `${fmt(s.dist, 1)}u, aim error ${fmt(s.yawErr, 3)}rad`) },
        }));
    }
    return out;
}

// D9: the flow around play - is the player dropped into a fair game, do the
// end screens work, does restarting give a clean game?
export function detectFlow(tl) {
    const out = [];
    const ps = playing(tl);
    const ev = eventTimes(tl).filter((e) => e.t !== null);
    const firstLoad = (tl.loads || [])[0] || (ps[0] && ps[0].t) || 0;
    const secondLoad = (tl.loads || [])[1] || Infinity;
    const locked = marksOf(tl, 'locked').find((m) => m.t < secondLoad);
    const firstRound = ps.find((s) => s.t < secondLoad && typeof s.round === 'number' && s.round >= 0);
    if (locked && firstRound) {
        const atEntry = sampleAt(ps, locked.t);
        const ticks = atEntry.round - firstRound.round;
        const during = ev.filter((e) => e.t >= firstLoad && e.t <= locked.t);
        const kinds = {};
        for (const e of during) kinds[e.type] = (kinds[e.type] || 0) + 1;
        if (ticks >= 90 || firstRound.round >= 90) {
            out.push(finding('D9', 'S2', 'The game is already running before the player has started', {
                t: firstRound.t, until: locked.t,
                evidence: {
                    roundWhenPageOpened: firstRound.round, roundWhenPlayerEntered: atEntry.round,
                    intensityWhenEntered: atEntry.intensity, hpWhenEntered: atEntry.hp,
                    eventsWhileInMenus: kinds,
                },
                detail: 'The world simulation (round counter, faction war, escalation) advances while the tutorial and click-to-enter prompt are up and before the page was even opened.',
            }));
        }
    }
    // Damage taken while a menu/modal covers the game.
    const hurt = [];
    for (let i = 1; i < ps.length; i++) {
        if (typeof ps[i].hp !== 'number' || ps[i].hp >= ps[i - 1].hp || ps[i].t - ps[i - 1].t > 1000) continue;
        const d = sampleAt(tl.dom, ps[i].t);
        const modals = Object.keys((d && d.modals) || {}).filter((k) => k !== 'overlay');
        if (modals.length) hurt.push({ t: ps[i].t, modals, from: ps[i - 1].hp, to: ps[i].hp });
    }
    if (hurt.length) {
        out.push(finding('D9', 'S1', 'Player takes damage while a menu covers the game', {
            t: hurt[0].t, evidence: { times: hurt.length, menus: [...new Set(hurt.flatMap((h) => h.modals))], example: `${hurt[0].from}->${hurt[0].to}` },
        }));
    }
    for (const m of marksOf(tl, 'end-click-ignored')) {
        out.push(finding('D9', 'S2', 'Clicking the end screen does nothing; the player must discover Escape first', {
            t: m.t, evidence: { pointerStillLocked: m.locked },
            detail: 'The end screen says "Click to restart" but the pointer is still locked to the game, so the click goes to the 3D canvas.',
        }));
    }
    for (const m of marksOf(tl, 'no-end-overlay')) {
        const res = marksOf(tl, 'result').filter((r) => r.t <= m.t).pop();
        if (res && (res.result === 'caught' || res.result === 'won')) {
            out.push(finding('D9', 'S1', 'No end screen after the game ended', { t: m.t, evidence: { result: res.result } }));
        }
    }
    // Restart must give a clean game.
    for (const seg of segments(tl).filter((x) => x.name === 'second-run' || x.name === 'after-restart')) {
        const s = ps.find((x) => x.t >= seg.t0 && typeof x.hp === 'number');
        const d = sampleAt(tl.dom, seg.t0 + 200);
        if (!s) continue;
        const problems = [];
        if (s.hp < 100) problems.push(`starts with HP ${s.hp}`);
        if (s.gs !== 'playing') problems.push(`starts in state "${s.gs}"`);
        if (d && d.modals && d.modals.overlay) problems.push('old end screen still shown');
        if (problems.length) out.push(finding('D9', 'S1', 'Restarting does not give a clean game', { t: seg.t0, evidence: { problems } }));
    }
    for (const m of marksOf(tl, 'lock-failed')) {
        out.push(finding('D9', 'S3', 'Could not enter the game (pointer lock refused) after 3 clicks', { t: m.t, detail: 'May be environmental (window focus); check the frames.' }));
    }
    return out;
}

// D10: page health. Nothing is filtered: every error, warning, failed
// request and unexpected socket close is listed.
export function detectHealth(tl) {
    const out = [];
    if (tl.pageErrors.length) {
        out.push(finding('D10', 'S1', 'Uncaught errors in the page', {
            t: tl.pageErrors[0].t, evidence: { count: tl.pageErrors.length, messages: [...new Set(tl.pageErrors.map((e) => e.msg.split('\n')[0]))].slice(0, 6) },
        }));
    }
    const errs = tl.console.filter((c) => c.type === 'error');
    const warns = tl.console.filter((c) => c.type === 'warning');
    if (errs.length) out.push(finding('D10', 'S2', 'Console errors', { t: errs[0].t, evidence: { count: errs.length, messages: [...new Set(errs.map((e) => e.text))].slice(0, 6) } }));
    if (warns.length) out.push(finding('D10', 'S3', 'Console warnings', { t: warns[0].t, evidence: { count: warns.length, messages: [...new Set(warns.map((e) => e.text))].slice(0, 6) } }));
    const loads = tl.loads || [];
    const reloadAborted = (n) => n.failure === 'net::ERR_ABORTED' && loads.some((l) => Math.abs(n.t - l) < 3000);
    const net = tl.network.filter((n) => !reloadAborted(n));
    if (net.length) out.push(finding('D10', 'S2', 'Failed network requests', { t: net[0].t, evidence: { count: net.length, examples: net.slice(0, 5).map((n) => `${n.status || n.failure} ${n.url}`) } }));
    const closes = tl.sockets.filter((s) => s.kind !== 'open' && !loads.some((l) => s.t <= l + 50 && s.t >= l - 3000));
    const nav = marksOf(tl, 'reloaded').concat(marksOf(tl, 'end-click'), marksOf(tl, 'end-restart'));
    const unexpected = closes.filter((c) => !nav.some((m) => Math.abs(m.t - c.t) < 3000));
    if (unexpected.length) out.push(finding('D10', 'S1', 'Lost connection to the game server', { t: unexpected[0].t, evidence: { count: unexpected.length } }));
    return out;
}

export { between };
