// hud.mjs - D5 (does the HUD tell the truth?), D6 (does every hit the player
// takes have visible feedback?), D7 (is the right thing on top?).

import { finding, playing, sampleAt, between, eventTimes, fmt } from './common.mjs';

const AIM_RANGE = 12; // client crosshair range (main.mjs crosshair raycast)

function aimCandidates(grid, s) {
    const fx = Math.sin(s.yaw), fz = Math.cos(s.yaw);
    return (s.actors || []).filter((a) => a.hp > 0 && !a.st).map((a) => {
        const dx = a.x - s.pos[0], dz = a.z - s.pos[2];
        const d = Math.hypot(dx, dz);
        return { a, d, dot: d > 0 ? (dx * fx + dz * fz) / d : 0 };
    }).filter((c) => c.d <= AIM_RANGE && c.d >= 0.5 && c.dot > 0.93)
        .map((c) => ({ ...c, wall: grid.segment(s.pos[0], s.pos[2], c.a.x, c.a.z).wall }));
}

// D5a: the red "target locked" crosshair must only light up when the player
// could actually hit something - an actor in the aim cone with no wall
// between. Locking through walls lies to the player about what a click does.
export function detectCrosshair(tl, grid) {
    if (!grid) return [];
    const ps = playing(tl).filter((s) => s.gs === 'playing');
    const runs = [];
    let run = null;
    for (const s of ps) {
        // Only judge what the player can see: no menu covering the crosshair.
        const d = sampleAt(tl.dom, s.t);
        if (d && Object.keys(d.modals || {}).length) { if (run) { runs.push(run); run = null; } continue; }
        const lying = s.aim && aimCandidates(grid, s).every((c) => c.wall);
        if (lying) {
            if (!run) run = { t0: s.t, t1: s.t, s, cands: aimCandidates(grid, s) };
            run.t1 = s.t;
        } else if (run) { runs.push(run); run = null; }
    }
    if (run) runs.push(run);
    const long = runs.filter((r) => r.t1 - r.t0 >= 300);
    if (!long.length) return [];
    const total = long.reduce((n, r) => n + r.t1 - r.t0, 0);
    const first = long[0];
    return [finding('D5', 'S2', 'Crosshair shows "target locked" (red) when no enemy can be hit', {
        t: first.t0, until: first.t1,
        evidence: {
            episodes: long.length, totalMs: Math.round(total),
            example: first.cands.length
                ? first.cands.map((c) => `actor ${c.a.id} ${fmt(c.d, 1)}u away behind a wall`).join('; ')
                : 'no actor in the aim cone at all',
        },
        detail: 'The crosshair test checks direction and distance only; it ignores walls.',
    })];
}

// D5b: the HP bar must match server HP, and HP must never go below zero.
export function detectHpTruth(tl) {
    const ps = playing(tl);
    const out = [];
    const neg = ps.filter((s) => typeof s.hp === 'number' && s.hp < 0);
    if (neg.length) {
        out.push(finding('D5', 'S2', 'Player HP goes below zero', { t: neg[0].t, evidence: { minHp: Math.min(...neg.map((s) => s.hp)) } }));
    }
    let run = null;
    const bad = [];
    for (const d of tl.dom) {
        if (d.hpFrac === null || d.hpFrac === undefined) { run = null; continue; }
        const s = sampleAt(ps, d.t);
        if (!s || typeof s.hp !== 'number') continue;
        const want = Math.max(0, Math.min(1, s.hp / 100));
        if (Math.abs(d.hpFrac - want) > 0.08) {
            if (!run) run = { t0: d.t, t1: d.t, shown: d.hpFrac, want };
            run.t1 = d.t;
            if (run.t1 - run.t0 >= 600 && !bad.includes(run)) bad.push(run);
        } else run = null;
    }
    if (bad.length) {
        out.push(finding('D5', 'S2', 'HP bar does not match the player\'s actual HP', {
            t: bad[0].t0, until: bad[0].t1, evidence: { episodes: bad.length, shown: fmt(bad[0].shown), actual: fmt(bad[0].want) },
        }));
    }
    return out;
}

// D6: every HP loss must come with a visible cue within 300ms: the red hit
// flash, a screen shake, or the hit marker. A player who loses HP with no cue
// cannot tell what is hurting them.
export function detectSilentDamage(tl) {
    const ps = playing(tl);
    const ev = eventTimes(tl).filter((e) => e.t !== null);
    const drops = [];
    for (let i = 1; i < ps.length; i++) {
        const a = ps[i - 1], b = ps[i];
        if (typeof a.hp !== 'number' || typeof b.hp !== 'number' || b.hp >= a.hp) continue;
        if (b.t - a.t > 1000) continue; // across a reload
        // The page reports a default HP before its first snapshot; that is
        // not a damage event (and the carried HP is not feedback-worthy).
        if (typeof a.round === 'number' && a.round < 0) continue;
        const loads = (tl.loads || []).map((l) => (typeof l === 'number' ? l : l && l.t));
        if (loads.some((lt) => lt > a.t && lt <= b.t)) continue;
        const win = between(ps, b.t - 100, b.t + 300);
        const cued = win.some((s) => s.flash || s.shake || s.marker);
        const near = ev.filter((e) => Math.abs(e.t - b.t) <= 400).map((e) => e.type);
        drops.push({ t: b.t, from: a.hp, to: b.hp, cued, near: [...new Set(near)] });
    }
    const silent = drops.filter((d) => !d.cued);
    if (!silent.length) return [];
    const lost = silent.reduce((n, d) => n + d.from - d.to, 0);
    return [finding('D6', lost >= 10 ? 'S2' : 'S3', 'Player loses HP with no on-screen feedback', {
        t: silent[0].t,
        evidence: {
            silentDrops: silent.length, of: drops.length, hpLostSilently: lost,
            examples: silent.slice(0, 5).map((d) => `${d.from}->${d.to} (server events nearby: ${d.near.join(',') || 'none'})`),
        },
        detail: 'No hit flash, screen shake or hit marker appeared within 300ms of the HP loss.',
    })];
}

const MODAL_NAMES = { 'tutorial-overlay': 'tutorial', 'pointer-lock-prompt': 'click-to-enter prompt', overlay: 'end screen', loading: 'loading screen', 'debug-panel': 'debug panel' };

// D7: when the game ends, the end screen must be what is on top; two modals
// must never stack; HUD elements must stay on screen and not overlap.
export function detectLayering(tl) {
    const out = [];
    const ps = playing(tl);
    const ends = ps.filter((s, i) => i > 0 && ps[i - 1].gs === 'playing' && (s.gs === 'caught' || s.gs === 'won'));
    for (const e of ends) {
        const probes = between(tl.dom, e.t + 300, e.t + 1500);
        if (!probes.length) continue;
        const shown = probes.filter((d) => d.modals.overlay);
        const onTop = shown.filter((d) => d.topOverlay && d.topOverlay.startsWith('overlay'));
        if (!shown.length) {
            out.push(finding('D7', 'S1', `Game ended (${e.gs}) but no end screen is shown`, { t: e.t, until: e.t + 1500 }));
        } else if (!onTop.length) {
            out.push(finding('D7', 'S1', 'End screen is covered by another element', { t: e.t, until: e.t + 1500, evidence: { topElement: shown[0].topOverlay } }));
        }
    }
    let stack = null;
    const stacks = [];
    for (const d of tl.dom) {
        const m = Object.keys(d.modals || {});
        const key = m.length > 1 ? m.sort().join('+') : null;
        if (key && stack && stack.key === key) { stack.t1 = d.t; continue; }
        if (stack && stack.t1 - stack.t0 >= 300) stacks.push(stack);
        stack = key ? { key, t0: d.t, t1: d.t } : null;
    }
    if (stack && stack.t1 - stack.t0 >= 300) stacks.push(stack);
    for (const st of uniqueBy(stacks, (x) => x.key)) {
        out.push(finding('D7', 'S2', `Two screens shown at once: ${st.key.split('+').map((k) => MODAL_NAMES[k] || k).join(' over ')}`, { t: st.t0, until: st.t1 }));
    }
    out.push(...hudGeometry(tl));
    return out;
}

function uniqueBy(arr, key) {
    const seen = new Set();
    return arr.filter((x) => { const k = key(x); if (seen.has(k)) return false; seen.add(k); return true; });
}

function overlapArea(a, b) {
    const w = Math.min(a[0] + a[2], b[0] + b[2]) - Math.max(a[0], b[0]);
    const h = Math.min(a[1] + a[3], b[1] + b[3]) - Math.max(a[1], b[1]);
    return w > 0 && h > 0 ? w * h : 0;
}

const TRANSIENT = new Set(['crosshair', 'hit-marker', 'wave-banner', 'status-chips']);

function hudGeometry(tl) {
    const issues = new Map();
    for (const d of tl.dom) {
        const items = [...Object.entries(d.hud || {}), ...Object.entries(d.chips || {})].filter(([k]) => !TRANSIENT.has(k));
        for (const [k, v] of items) {
            const [x, y, w, h] = v.rect;
            if (x < -1 || y < -1 || x + w > d.W + 1 || y + h > d.H + 1) note(issues, `offscreen:${k}`, d.t, { el: k, rect: v.rect.map(Math.round), viewport: [d.W, d.H] });
        }
        for (let i = 0; i < items.length; i++) {
            for (let j = i + 1; j < items.length; j++) {
                const ov = overlapArea(items[i][1].rect, items[j][1].rect);
                if (ov > 16) note(issues, `overlap:${items[i][0]}+${items[j][0]}`, d.t, { a: items[i][0], b: items[j][0], areaPx: Math.round(ov), viewport: [d.W, d.H] });
            }
        }
    }
    const out = [];
    for (const [k, v] of issues) {
        if (v.t1 - v.t0 < 500) continue;
        const [kind] = k.split(':');
        out.push(finding('D7', 'S3', kind === 'offscreen' ? `HUD element "${v.ev.el}" is cut off by the window edge` : `HUD elements "${v.ev.a}" and "${v.ev.b}" overlap`, { t: v.t0, until: v.t1, evidence: v.ev }));
    }
    return out;
}

function note(map, key, t, ev) {
    const cur = map.get(key);
    if (cur && t - cur.t1 < 400) { cur.t1 = t; return; }
    if (cur && cur.t1 - cur.t0 >= 500) return; // keep the first long episode
    map.set(key, { t0: t, t1: t, ev });
}
