// feel.mjs - is the game any GOOD to play? Everything here is measured from
// the player's side of the screen: what was in view, what came at them, what
// their actions did. A "visible" enemy is one inside the camera's horizontal
// field of view, within draw range, with no wall between - what a person
// would actually have seen.
//
//   D11 body collision  - the player and mobs pass through each other
//   D12 gun impact      - shots barely move or stop the enemy they hit
//   D13 unseen damage   - HP lost with no enemy on screen
//   D14 engagement      - dead time, how rarely anything is on screen
//   D15 presence        - mobs that see the player but do not come for them,
//                         mobs that idle on screen or only fight each other
//   D16 pop-in          - mobs appearing/vanishing/teleporting in plain view
//
// Thresholds marked DESIGN BAND are judgement calls about what reads as
// broken or boring to a player; they are reported with the raw numbers so a
// reviewer can disagree with the band, not with the measurement.

import { finding, playing, sampleAt, eventTimes, fmt } from './common.mjs';

const VIEW_RANGE = 15;

export function hfov(s) {
    const v = ((s.fov || 70) * Math.PI) / 180;
    return 2 * Math.atan(Math.tan(v / 2) * (s.aspect || 1.6));
}

// onScreen: is actor a visible to the player in sample s?
export function onScreen(grid, s, a) {
    const dx = a.x - s.pos[0], dz = a.z - s.pos[2];
    const d = Math.hypot(dx, dz);
    if (d > VIEW_RANGE || d < 0.05) return false;
    const fx = Math.sin(s.yaw), fz = Math.cos(s.yaw);
    const ang = Math.acos(Math.max(-1, Math.min(1, (dx * fx + dz * fz) / d)));
    if (ang > hfov(s) / 2 + 0.05) return false;
    return !grid.segment(s.pos[0], s.pos[2], a.x, a.z).wall;
}

// Subsample the per-frame stream to ~10Hz: plenty for movement-scale
// questions and keeps the O(samples x actors) passes cheap.
function tenHz(tl) {
    const out = [];
    let last = -Infinity;
    for (const s of playing(tl)) {
        if (s.gs !== 'playing' || s.t - last < 100) continue;
        last = s.t;
        out.push(s);
    }
    return out;
}

export function detectBodyCollision(tl) {
    const ps = playing(tl).filter((s) => s.gs === 'playing');
    const prev = new Map();
    const passes = [];
    let closeMs = 0;
    for (let i = 0; i < ps.length; i++) {
        const s = ps[i];
        const dt = i ? Math.min(100, s.t - ps[i - 1].t) : 0;
        let minD = Infinity;
        for (const a of s.actors || []) {
            if (a.hp <= 0) continue;
            const rel = [a.x - s.pos[0], a.z - s.pos[2]];
            const d = Math.hypot(rel[0], rel[1]);
            minD = Math.min(minD, d);
            const p = prev.get(a.id);
            if (p && s.t - p.t < 400 && d < 1.0 && p.d < 1.0) {
                const dot = (rel[0] * p.rel[0] + rel[1] * p.rel[1]) / Math.max(1e-6, d * p.d);
                if (dot < -0.3 && Math.min(d, p.d) < 0.7) passes.push({ t: s.t, id: a.id, d: Math.min(d, p.d) });
            }
            prev.set(a.id, { t: s.t, rel, d });
        }
        if (minD < 0.6) closeMs += dt;
    }
    if (!passes.length) return [];
    return [finding('D11', 'S2', 'The player and enemies pass straight through each other', {
        t: passes[0].t,
        evidence: { passThroughs: passes.length, actors: [...new Set(passes.map((p) => p.id))].length, closestCentreDistU: fmt(Math.min(...passes.map((p) => p.d))), msWithinBodyRange: Math.round(closeMs) },
        detail: 'An enemy went from in front of the player to behind them (or vice versa) while their centres were closer than 0.7u - two bodies swapped sides through each other. The enemy models are drawn far larger than the collision radius, so the camera ends up inside them.',
    })];
}

export function detectGunImpact(tl) {
    const ps = playing(tl);
    const hits = eventTimes(tl).filter((e) => e.type === 'stunned' && e.t !== null && e.data && e.data.minotaur && e.data.hp > 0);
    const rows = [];
    for (const h of hits) {
        const before = sampleAt(ps, h.t - 60), after = sampleAt(ps, h.t + 450), late = sampleAt(ps, h.t + 1200);
        const a0 = before && (before.actors || []).find((a) => a.id === h.data.minotaur);
        const a1 = after && (after.actors || []).find((a) => a.id === h.data.minotaur);
        const a2 = late && (late.actors || []).find((a) => a.id === h.data.minotaur);
        if (!a0 || !a1) continue;
        rows.push({ t: h.t, push: Math.hypot(a1.x - a0.x, a1.z - a0.z), stunnedShown: !!a1.st, driftWhileStunned: a2 && a2.st ? Math.hypot(a2.x - a1.x, a2.z - a1.z) : null });
    }
    if (rows.length < 2) return [];
    const avg = rows.reduce((n, r) => n + r.push, 0) / rows.length;
    const shown = rows.filter((r) => r.stunnedShown).length / rows.length;
    const out = [];
    // DESIGN BAND: a pulse that moves its target less than ~0.25u reads as
    // having no physical impact at this camera distance.
    if (avg < 0.25) {
        out.push(finding('D12', 'S3', 'Shots barely move the enemy they hit (weak impact)', {
            t: rows[0].t, evidence: { hitsMeasured: rows.length, avgPushU: fmt(avg), maxPushU: fmt(Math.max(...rows.map((r) => r.push))) },
        }));
    }
    if (shown < 0.5) {
        out.push(finding('D12', 'S2', 'Enemies hit by a shot are not shown as stunned', { t: rows[0].t, evidence: { hitsMeasured: rows.length, shownStunned: `${Math.round(shown * 100)}%` } }));
    }
    return out.map((f) => ({ ...f, evidence: { ...f.evidence, perHit: rows.slice(0, 6).map((r) => `push ${fmt(r.push)}u stunned ${r.stunnedShown}`) } }));
}

export function detectUnseenDamage(tl, grid) {
    if (!grid) return [];
    const ps = playing(tl);
    const drops = [];
    for (let i = 1; i < ps.length; i++) {
        const a = ps[i - 1], b = ps[i];
        if (typeof a.hp !== 'number' || b.hp >= a.hp || b.t - a.t > 1000 || a.gs !== 'playing') continue;
        // Was an enemy on screen at any time in the 1.5s before the hit?
        const win = ps.filter((s) => s.t >= b.t - 1500 && s.t <= b.t && s.pos);
        const seen = win.some((s) => (s.actors || []).some((x) => x.hp > 0 && onScreen(grid, s, x)));
        drops.push({ t: b.t, lost: a.hp - b.hp, seen });
    }
    const unseen = drops.filter((d) => !d.seen);
    const lost = unseen.reduce((n, d) => n + d.lost, 0);
    if (!unseen.length || lost < 10) return [];
    return [finding('D13', 'S2', 'The player is hurt by enemies that were never on screen', {
        t: unseen[0].t,
        evidence: { hitsFromUnseen: unseen.length, of: drops.length, hpLostToUnseen: lost, hpLostTotal: drops.reduce((n, d) => n + d.lost, 0) },
        detail: 'No enemy was inside the camera view (field of view + line of sight) during the 1.5s before these HP losses: the player cannot see what is hurting them.',
    })];
}


// ghostWindows: after the server reports an enemy's death, the client must
// stop drawing it. Returns per-death how long the dead enemy stayed in the
// client's world (still drawn, with its last HP) after the death event.
export function ghostWindows(tl) {
    const ps = playing(tl);
    const deaths = eventTimes(tl).filter((e) => e.type === 'death' && e.t !== null && e.data && e.data.id);
    return deaths.map((d) => {
        const after = ps.filter((s) => s.t > d.t + 250 && s.t < d.t + 4000 && (s.actors || []).some((a) => a.id === d.data.id));
        return { id: d.data.id, t: d.t, ghostMs: after.length ? Math.round(after[after.length - 1].t - after[0].t) : 0 };
    });
}

export function detectGhosts(tl) {
    const g = ghostWindows(tl);
    const ghosts = g.filter((x) => x.ghostMs >= 500);
    if (!ghosts.length) return [];
    return [finding('D16', 'S2', 'Killed enemies keep standing, frozen and alive-looking, after they die', {
        t: ghosts[0].t, until: ghosts[0].t + ghosts[0].ghostMs + 250,
        evidence: { deaths: g.length, leftGhosts: ghosts.length, avgGhostMs: Math.round(ghosts.reduce((n, x) => n + x.ghostMs, 0) / ghosts.length) },
        detail: 'The death burst and kill-feed entry play, but the enemy model stays standing in place (with its last HP) until the client prunes it about 2s later.',
    })];
}

// profile computes the engagement numbers every journey report carries.
export function feelProfile(tl, grid) {
    const ps = tenHz(tl);
    if (!ps.length || !grid) return null;
    const ev = eventTimes(tl).filter((e) => e.t !== null);
    // Dead enemies lingering as ghosts are judged by detectGhosts; leave them
    // out of the presence numbers so "mobs stand still" means LIVE mobs.
    const ghostOf = new Map(ghostWindows(tl).map((x) => [x.id, x]));
    const alive = (a, t) => { const g = ghostOf.get(a.id); return a.hp > 0 && !(g && t > g.t && t < g.t + 4000); };
    let visibleT = 0, total = 0, stillOnScreen = 0, movingOnScreen = 0;
    let dead = 0, longestDead = 0, deadStart = ps[0].t, longestDeadAt = ps[0].t;
    let canSee = 0, closing = 0;
    const lastPos = new Map();
    const popIns = [], vanishes = [], teleports = [];
    const prevVisible = new Map();
    for (let i = 0; i < ps.length; i++) {
        const s = ps[i];
        const dt = i ? Math.min(250, s.t - ps[i - 1].t) / 1000 : 0;
        total += dt;
        const vis = (s.actors || []).filter((a) => alive(a, s.t) && onScreen(grid, s, a));
        if (vis.length) visibleT += dt;
        // Dead time: no enemy on screen, no HP change, no shot of ours, no kill.
        const busy = vis.length > 0 || (i && s.hp !== ps[i - 1].hp)
            || ev.some((e) => (e.type === 'stunned' || e.type === 'hit') && e.t > (i ? ps[i - 1].t : 0) && e.t <= s.t);
        if (busy) { dead = 0; deadStart = s.t; } else {
            dead += dt;
            if (dead > longestDead) { longestDead = dead; longestDeadAt = deadStart; }
        }
        const ids = new Set();
        for (const a of s.actors || []) {
            if (!alive(a, s.t)) continue;
            ids.add(a.id);
            const lp = lastPos.get(a.id);
            if (lp && s.t - lp.t <= 250) {
                const step = Math.hypot(a.x - lp.x, a.z - lp.z);
                const speed = step / Math.max(0.05, (s.t - lp.t) / 1000);
                if (step > 1.5) teleports.push({ t: s.t, id: a.id, step, visible: onScreen(grid, s, a) || (lp.vis) });
                if (onScreen(grid, s, a)) { if (speed < 0.15) stillOnScreen += dt; else movingOnScreen += dt; }
                // Presence: can this mob see the player (within 8u, LOS)? Then
                // is it closing in over the next ~2s?
                const d = Math.hypot(a.x - s.pos[0], a.z - s.pos[2]);
                if (d < 8 && d > 1.2 && !a.st && !grid.segment(a.x, a.z, s.pos[0], s.pos[2]).wall && i % 5 === 0) {
                    // The ENEMY's own movement toward the player, so the
                    // player walking up to a statue does not count.
                    const fut = ps.slice(i + 1).find((x) => x.t >= s.t + 2000);
                    const af = fut && (fut.actors || []).find((x) => x.id === a.id);
                    if (af) { canSee++; if (((af.x - a.x) * (s.pos[0] - a.x) + (af.z - a.z) * (s.pos[2] - a.z)) / d >= 0.5) closing++; }
                }
            }
            lastPos.set(a.id, { t: s.t, x: a.x, z: a.z, vis: onScreen(grid, s, a) });
        }
        const nowVisible = new Map(vis.map((a) => [a.id, a]));
        if (i) {
            for (const [id, a] of nowVisible) {
                if (!prevVisible.has(id) && !(ps[i - 1].actors || []).some((x) => x.id === id)) popIns.push({ t: s.t, id, d: Math.hypot(a.x - s.pos[0], a.z - s.pos[2]) });
            }
            for (const [id] of prevVisible) {
                if (!ids.has(id)) {
                    const died = ev.some((e) => e.type === 'death' && e.data && e.data.id === id && Math.abs(e.t - s.t) < 1500);
                    if (!died) vanishes.push({ t: s.t, id });
                }
            }
        }
        prevVisible.clear();
        for (const [id, a] of nowVisible) prevVisible.set(id, a);
    }
    const clashesWhileVisible = ev.filter((e) => e.type === 'clash' && sampleAt(ps, e.t) && (sampleAt(ps, e.t).actors || []).some((a) => onScreen(grid, sampleAt(ps, e.t), a))).length;
    const hitsOnPlayer = ev.filter((e) => e.type === 'hit').length;
    return {
        server: serverPresence(tl, grid),
        playSeconds: +total.toFixed(1),
        enemyOnScreenPct: Math.round((visibleT / Math.max(total, 1e-6)) * 100),
        longestDeadTimeS: +longestDead.toFixed(1), longestDeadAt,
        onScreenStillPct: Math.round((stillOnScreen / Math.max(stillOnScreen + movingOnScreen, 1e-6)) * 100),
        mobSawPlayerSamples: canSee, mobClosedInPct: canSee ? Math.round((closing / canSee) * 100) : null,
        onScreenClashes: clashesWhileVisible, hitsOnPlayer,
        popIns, vanishes, teleports,
    };
}

// serverPresence repeats the presence questions on the SERVER's positions
// (wire tap), separating "the AI does not come for the player" from "the
// client draws enemies frozen" (D17). Returns null without a wire tap.
export function serverPresence(tl, grid) {
    const snaps = (tl.wire || []).filter((w) => w.kind === 'snap' && w.runner);
    if (snaps.length < 20) return null;
    let canSee = 0, closing = 0, still = 0, moving = 0;
    for (let i = 0, last = -Infinity; i < snaps.length; i++) {
        const s = snaps[i];
        if (s.t - last < 500) continue;
        last = s.t;
        const fut = snaps.find((x) => x.t >= s.t + 2000);
        const prev = snaps[Math.max(0, i - 2)];
        for (const m of s.m) {
            if (m[3] <= 0) continue;
            const pm = prev.m.find((x) => x[0] === m[0]);
            if (pm && s.t > prev.t) { if (Math.hypot(m[1] - pm[1], m[2] - pm[2]) / ((s.t - prev.t) / 1000) < 0.15) still++; else moving++; }
            const d = Math.hypot(m[1] - s.runner[0], m[2] - s.runner[1]);
            if (!fut || m[4] || d >= 8 || d <= 1.2 || grid.segment(m[1], m[2], s.runner[0], s.runner[1]).wall) continue;
            const fm = fut.m.find((x) => x[0] === m[0]);
            if (!fm) continue;
            canSee++;
            if (((fm[1] - m[1]) * (s.runner[0] - m[1]) + (fm[2] - m[2]) * (s.runner[1] - m[2])) / d >= 0.5) closing++;
        }
    }
    return {
        mobsStillPct: still + moving ? Math.round((still / (still + moving)) * 100) : null,
        mobSawPlayerSamples: canSee, mobClosedInPct: canSee ? Math.round((closing / canSee) * 100) : null,
    };
}

export function detectEngagement(tl, grid) {
    const p = feelProfile(tl, grid);
    if (!p) return [];
    const out = [];
    // DESIGN BAND: 20s with nothing on screen and nothing happening is a lull
    // most players read as "nothing is going on".
    if (p.longestDeadTimeS >= 20) {
        out.push(finding('D14', 'S3', `${p.longestDeadTimeS}s with nothing happening (no enemy in view, no damage, no shots landing)`, {
            t: p.longestDeadAt, until: p.longestDeadAt + p.longestDeadTimeS * 1000, evidence: { enemyOnScreenPct: p.enemyOnScreenPct, playSeconds: p.playSeconds },
        }));
    }
    // DESIGN BAND: mobs that spend most of their on-screen time standing
    // still, or that see the player and do not come for them, have no threat.
    if (p.onScreenStillPct >= 50) {
        out.push(finding('D15', 'S2', `Enemies in view stand still ${p.onScreenStillPct}% of the time`, { evidence: { onScreenStillPct: p.onScreenStillPct, onScreenClashes: p.onScreenClashes } }));
    }
    if (p.mobSawPlayerSamples >= 5 && p.mobClosedInPct !== null && p.mobClosedInPct < 40) {
        out.push(finding('D15', 'S2', `Enemies that can see the player usually do not come for them (only ${p.mobClosedInPct}% close in)`, {
            evidence: { mobSawPlayerSamples: p.mobSawPlayerSamples, mobClosedInPct: p.mobClosedInPct, onScreenClashes: p.onScreenClashes, hitsOnPlayer: p.hitsOnPlayer },
            detail: 'Sampled whenever a non-stunned enemy within 8u had line of sight to the player: did the enemy itself move at least 0.5u toward the player over the next 2s?',
        }));
    }
    const shown = [...p.popIns.filter((x) => x.d < 10), ...p.vanishes];
    if (shown.length) {
        out.push(finding('D16', 'S2', 'Enemies pop into or out of existence in plain view', {
            t: shown[0].t, evidence: { popIns: p.popIns.filter((x) => x.d < 10).length, vanishedWithoutDying: p.vanishes.length, example: shown[0] },
        }));
    }
    const tp = p.teleports.filter((x) => x.visible);
    if (tp.length) {
        out.push(finding('D16', 'S2', 'Enemies teleport in plain view', { t: tp[0].t, evidence: { count: tp.length, maxJumpU: fmt(Math.max(...tp.map((x) => x.step))) } }));
    }
    return out;
}
