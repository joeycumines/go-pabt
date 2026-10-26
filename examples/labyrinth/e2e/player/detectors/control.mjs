// control.mjs - D2 (do the keys do what a player expects?), D3 (does the
// mouse turn the view the right way?), D4 (does the camera move
// continuously, without snaps or stalls?).
//
// Direction checks never trust the game's own camera basis. Key directions
// are compared with each other (S must oppose W, A must oppose D, D must be
// sideways to W), and handedness is judged on SCREEN: turning right and
// strafing right must both slide the scene left. That is what catches a
// mirrored view or an inverted control that a basis-mirroring test passes.

import { horizontalShift } from '../image.mjs';
import { finding, playing, sampleAt, marksOf, between, eventTimes, fmt, hudMasks } from './common.mjs';
import { wrapAngle } from '../maze.mjs';
import { hfov } from './feel.mjs';

const RAD_PER_PX = 0.002; // camera.mjs mouse sensitivity (only used to size the expected screen shift)

// A pixel-shift estimate is trusted only with strong correlation and a shift
// well clear of zero: flat, low-texture frames correlate weakly.
const CONFIDENT = { minCorr: 0.7, minShiftPx: 8 };
const confident = (x) => !!x && x.score >= CONFIDENT.minCorr && Math.abs(x.shift) >= CONFIDENT.minShiftPx;

function disp(a, b) { return [b.pos[0] - a.pos[0], b.pos[2] - a.pos[2]]; }
function len(v) { return Math.hypot(v[0], v[1]); }
function cos(a, b) { const l = len(a) * len(b); return l > 0 ? (a[0] * b[0] + a[1] * b[1]) / l : 0; }

// probeResults measures each controls-check probe: displacement from key
// down to 400ms after key up, and latency to first movement.
export function probeResults(tl) {
    const ps = playing(tl);
    const M = tl.marks;
    const res = {};
    // Marks are appended in program order but several can share a
    // millisecond, so walk by index, never by timestamp comparison.
    const after = (i, pred) => { for (let j = i + 1; j < M.length; j++) if (pred(M[j])) return j; return -1; };
    for (let i = 0; i < M.length; i++) {
        const p = M[i];
        if (p.kind !== 'probe') continue;
        const ni = after(i, (m) => m.kind === 'keydown' || m.kind === 'look' || m.kind === 'probe');
        if (ni < 0 || M[ni].kind === 'probe') continue;
        const next = M[ni];
        if (next.kind === 'look') {
            let ei = ni;
            while (ei + 1 < M.length && M[ei + 1].kind === 'look') ei++;
            const endT = ei + 1 < M.length ? M[ei + 1].t : next.t + 600;
            const dx = M.slice(ni, ei + 1).reduce((n, m) => n + m.dx, 0);
            res[p.key] = { kind: 'look', t0: next.t, t1: endT, dx, a: sampleAt(ps, next.t - 20), b: sampleAt(ps, endT) };
            continue;
        }
        const ui = after(ni, (m) => m.kind === 'keyup' && m.key === next.key);
        if (ui < 0) continue;
        const up = M[ui];
        const a = sampleAt(ps, next.t);
        const b = sampleAt(ps, up.t + 400);
        const first = between(ps, next.t, up.t + 400).find((s) => Math.hypot(s.pos[0] - a.pos[0], s.pos[2] - a.pos[2]) > 0.01);
        res[p.key] = { kind: 'key', key: next.key, t0: next.t, t1: up.t + 400, a, b, v: disp(a, b), latencyMs: first ? Math.round(first.t - next.t) : null };
    }
    return res;
}

export function detectControls(tl, frames, grid = null) {
    const out = [];
    const r = probeResults(tl);
    if (!r.w) return out; // this journey did not run a controls check
    const names = { w: 'W (forward)', s: 'S (back)', a: 'A (strafe left)', d: 'D (strafe right)' };
    // A strafe key cannot move a player already touching a side wall, so a
    // "dead strafe" verdict needs room on both sides of the W axis (we do not
    // assume which side is "right" - that is what the pixel check decides).
    const sideRoom = (p) => {
        if (!grid || len(r.w.v) < 0.1) return true;
        const [wx, wz] = r.w.v.map((c) => c / len(r.w.v));
        const room = [1, -1].map((sg) => grid.castRay(p.a.pos[0], p.a.pos[2], -wz * sg, wx * sg).dist - 0.3);
        return Math.min(...room) > 0.05;
    };
    for (const k of ['w', 's', 'a', 'd']) {
        const p = r[k];
        if (!p) continue;
        if (len(p.v) < 0.03 && (k === 'w' || k === 's' || sideRoom(p))) {
            out.push(finding('D2', 'S1', `${names[k]} did not move the player`, { t: p.t0, until: p.t1, evidence: { moved: fmt(len(p.v), 3) } }));
        } else if (p.latencyMs !== null && p.latencyMs > 150) {
            out.push(finding('D2', 'S2', `${names[k]} responds late`, { t: p.t0, until: p.t1, evidence: { latencyMs: p.latencyMs } }));
        }
    }
    // Direction comparisons need enough displacement to have a direction:
    // strafing in a corridor is clamped by the side walls to ~0.2u.
    const ok = (k) => r[k] && len(r[k].v) >= 0.1;
    if (ok('w') && ok('s') && cos(r.w.v, r.s.v) > -0.7) {
        out.push(finding('D2', 'S1', 'S does not move opposite to W', { t: r.s.t0, until: r.s.t1, evidence: { cosWS: fmt(cos(r.w.v, r.s.v)) } }));
    }
    if (ok('a') && ok('d') && cos(r.a.v, r.d.v) > -0.7) {
        out.push(finding('D2', 'S1', 'A does not move opposite to D', { t: r.a.t0, until: r.d.t1, evidence: { cosAD: fmt(cos(r.a.v, r.d.v)) } }));
    }
    if (ok('w') && ok('d') && Math.abs(cos(r.w.v, r.d.v)) > 0.5) {
        out.push(finding('D2', 'S2', 'D strafe is not sideways relative to W', { t: r.d.t0, until: r.d.t1, evidence: { cosWD: fmt(cos(r.w.v, r.d.v)) } }));
    }
    // Handedness, judged on screen only: strafing right (D) and turning right
    // (mouse moved right) must both slide the scene LEFT. Comparing the two
    // pixel shifts is independent of any world/camera convention.
    const lr = r['look-right'];
    if (lr && frames) {
        const look = shiftBetween(tl, frames, lr.t0 - 30, lr.t1);
        const strafe = r.d && len(r.d.v) >= 0.03 ? shiftBetween(tl, frames, r.d.t0 - 30, r.d.t1) : null;
        const ev = { mouseDx: Math.round(lr.dx), lookScreenShiftPx: look && look.shift, lookCorr: look && fmt(look.score), strafeDScreenShiftPx: strafe && strafe.shift, strafeCorr: strafe && fmt(strafe.score), yawDelta: fmt(wrapAngle(lr.b.yaw - lr.a.yaw)) };
        if (confident(look) && Math.sign(look.shift) === Math.sign(lr.dx)) {
            out.push(finding('D3', 'S1', 'Mouse look is inverted: moving the mouse right turns the view LEFT', {
                t: lr.t0, until: lr.t1, evidence: ev,
                detail: confident(strafe) && strafe.shift < 0
                    ? 'Strafing right with D correctly slides the scene left, but moving the mouse right slides it right: turning and strafing disagree about which way is right.'
                    : 'The scene slid the same way the mouse moved, which is what turning the opposite way looks like.',
            }));
        }
        if (confident(strafe) && strafe.shift > 0) {
            out.push(finding('D2', 'S1', 'Strafe is mirrored: D moves the view to the LEFT', { t: r.d.t0, until: r.d.t1, evidence: ev }));
        }
        if (look && look.score >= CONFIDENT.minCorr && Math.abs(look.shift) <= 1 && Math.abs(wrapAngle(lr.b.yaw - lr.a.yaw)) > 0.1) {
            out.push(finding('D3', 'S2', 'The camera reports a turn but the picture did not move', { t: lr.t0, until: lr.t1, evidence: ev }));
        }
    }
    return out;
}

// shiftBetween measures the on-screen horizontal scene shift between the
// frames nearest t0 and t1, with the HUD (from both moments) masked out.
function shiftBetween(tl, frames, t0, t1) {
    const fa = frames.at(t0), fb = frames.at(t1);
    if (!fa || !fb) return null;
    // Same frame = the screencast saw no change at all: the picture did not
    // move, which is a certain zero shift (the purest dead-look case).
    if (fa.file === fb.file) return { shift: 0, score: 1 };
    const masks = [...hudMasks(sampleAt(tl.dom, fa.t)), ...hudMasks(sampleAt(tl.dom, fb.t))];
    return horizontalShift(frames.image(fa), frames.image(fb), { masks });
}

// detectLook judges the journey's real mouse looks ON SCREEN. A gesture is a
// run of look marks with nothing else in between (so pilot steering while
// walking never merges into one long "gesture"). Only gestures whose
// expected screen shift fits the estimator's window (20-110px at 640px wide)
// are judged, and only with a confident shift. A right look must slide the
// scene left; a look that leaves the picture still is dead.
export function detectLook(tl, frames) {
    if (!frames) return [];
    const ps = playing(tl);
    const M = tl.marks;
    const gestures = [];
    for (let i = 0; i < M.length; i++) {
        if (M[i].kind !== 'look') continue;
        let j = i, dx = 0;
        while (j < M.length && M[j].kind === 'look' && (j === i || M[j].t - M[j - 1].t < 100)) { dx += M[j].dx; j++; }
        if (j < M.length && M[j].kind !== 'wait') { i = j - 1; continue; } // something else happened mid-gesture
        gestures.push({ t0: M[i].t, t1: j < M.length ? M[j].t : M[j - 1].t + 300, dx });
        i = j - 1;
    }
    let judged = 0, inverted = 0, dead = 0, exInv = null, exDead = null;
    for (const g of gestures) {
        const a = sampleAt(ps, g.t0 - 20), b = sampleAt(ps, g.t1 + 60);
        if (!a || !b || !a.locked || a.gs !== 'playing' || b.gs !== 'playing') continue;
        const fa = frames.at(g.t0 - 30);
        const img = fa && frames.image(fa);
        if (!img) continue;
        const expected = (Math.abs(g.dx) * RAD_PER_PX / hfov(a)) * img.width;
        if (expected < 20 || expected > 110) continue;
        const sh = shiftBetween(tl, frames, g.t0 - 30, g.t1 + 60);
        if (!sh || sh.score < CONFIDENT.minCorr) continue;
        judged++;
        if (Math.abs(sh.shift) < 0.15 * expected) { dead++; exDead = exDead || { ...g, sh, expected }; } else if (Math.sign(sh.shift) === Math.sign(g.dx)) { inverted++; exInv = exInv || { ...g, sh, expected }; }
    }
    const out = [];
    if (judged >= 4 && inverted / judged > 0.5) {
        out.push(finding('D3', 'S1', 'Mouse look turns the view the opposite way to the mouse (most turns this run)', {
            t: exInv.t0, until: exInv.t1, evidence: { gesturesJudged: judged, inverted, exampleMouseDx: Math.round(exInv.dx), exampleScreenShiftPx: exInv.sh.shift, expectedShiftPx: Math.round(exInv.expected) },
        }));
    }
    if (judged >= 4 && dead / judged > 0.3) {
        out.push(finding('D3', 'S1', 'Mouse look often does not move the picture', { t: exDead.t0, until: exDead.t1, evidence: { gesturesJudged: judged, dead } }));
    }
    return out;
}

// detectSnaps: the camera must never jump further in one frame than the
// player can move (sprint 6.5u/s, momentum overshoot 1.6x) unless the server
// explains it (knockback from a hit, win/caught snap) or the page reloaded.
// Unexplained jumps are what a player sees as rubber-banding.
export function detectSnaps(tl) {
    const ps = playing(tl);
    const ev = eventTimes(tl).filter((e) => e.t !== null && ['hit', 'caught', 'win'].includes(e.type));
    const loads = tl.loads || [];
    const snaps = [];
    for (let i = 1; i < ps.length; i++) {
        const a = ps[i - 1], b = ps[i];
        const dt = (b.t - a.t) / 1000;
        if (dt <= 0 || dt > 0.25) continue;
        if (a.gs !== 'playing' || b.gs !== 'playing') continue;
        const step = Math.hypot(b.pos[0] - a.pos[0], b.pos[2] - a.pos[2]);
        const allowed = 6.5 * 1.6 * dt + 0.03;
        if (step <= allowed) continue;
        const explained = ev.some((e) => b.t >= e.t - 150 && b.t <= e.t + 600) || loads.some((l) => b.t >= l && b.t <= l + 3000);
        const crossed = Math.floor(a.pos[0]) !== Math.floor(b.pos[0]) || Math.floor(a.pos[2]) !== Math.floor(b.pos[2]);
        const va = a.vel ? Math.hypot(a.vel[0], a.vel[2]) : 0, vb = b.vel ? Math.hypot(b.vel[0], b.vel[2]) : 0;
        snaps.push({ t: b.t, step, explained, crossed, velDrop: va > 0.5 && vb < va * 0.4, drift: a.spos ? Math.hypot(a.spos[0] - a.pos[0], a.spos[2] - a.pos[2]) : null });
    }
    const bad = snaps.filter((s) => !s.explained);
    const out = [];
    if (bad.length) {
        const worst = bad.reduce((x, y) => (y.step > x.step ? y : x));
        const durMin = ps.length ? (ps[ps.length - 1].t - ps[0].t) / 60000 : 1;
        out.push(finding('D4', worst.step >= 0.3 ? 'S2' : 'S3', 'Camera jumps (rubber-banding) with no knockback to explain it', {
            t: worst.t,
            evidence: {
                count: bad.length, perMinute: fmt(bad.length / Math.max(durMin, 1 / 60), 1), worstStepU: fmt(worst.step),
                atCellBoundary: bad.filter((s) => s.crossed).length, withSpeedCollapse: bad.filter((s) => s.velDrop).length,
                worstDriftBefore: fmt(worst.drift), times: bad.slice(0, 8).map((s) => Math.round(s.t)),
            },
            detail: 'The client snaps its camera to the server position when they drift apart (always when they straddle a cell boundary by >0.2u) and cuts velocity to 25%; each snap is a visible jump and a stall in walking speed.',
        }));
    }
    const hits = snaps.filter((s) => s.explained);
    if (hits.length) {
        out.push(finding('D4', 'S3', 'Camera jumps on server knockback (expected, listed for review)', {
            t: hits[0].t, evidence: { count: hits.length, maxStepU: fmt(Math.max(...hits.map((s) => s.step))) },
        }));
    }
    return out;
}

export function detectStalls(tl) {
    const loads = tl.loads || [];
    const resizes = marksOf(tl, 'resize').map((m) => m.t);
    const quiet = (t) => loads.some((l) => t >= l && t <= l + 3000) || resizes.some((r) => t >= r && t <= r + 1000)
        || marksOf(tl, 'blur').some((b) => t >= b.t && t <= b.t + 4000);
    const stalls = tl.rafGaps.filter((g) => g.gap >= 250 && !quiet(g.t));
    if (!stalls.length) return [];
    const worst = stalls.reduce((x, y) => (y.gap > x.gap ? y : x));
    return [finding('D4', worst.gap >= 1000 ? 'S1' : 'S2', 'The game hitched (no frame drawn for a quarter second or more)', {
        t: worst.t - worst.gap, until: worst.t, evidence: { count: stalls.length, worstMs: Math.round(worst.gap) },
    })];
}

// detectStuckKey: after the player alt-tabs away while holding W and releases
// it in the other window, the character must stop. Judged on what the player
// sees: how far the character keeps travelling (path length, so walking
// into a wall and sliding still counts) from just after the release until 2s
// after they come back. Also reports whether the game page genuinely lost
// focus: without a real blur the scenario did not happen.
export function detectStuckKey(tl) {
    const rel = marksOf(tl, 'keyup-elsewhere')[0];
    const ref = marksOf(tl, 'refocus')[0];
    if (!rel || !ref) return [];
    const blur = marksOf(tl, 'blur')[0];
    const ps = playing(tl);
    // Watch until the journey moves on (it turns the player round after
    // refocusing, so a stuck W shows as walking back down the corridor).
    const next = marksOf(tl, 'segment').find((m) => m.t > ref.t);
    const end = next ? next.t : ref.t + 4000;
    // Without a real blur the scenario never happened (the release went to
    // another page either way), so there is nothing to judge.
    if (blur && blur.gameHasFocus) {
        return [finding('HARNESS', 'S3', 'Alt-tab step did not take focus away from the game; stuck-key result is not meaningful', { t: blur.t })];
    }
    const win = between(ps, rel.t + 300, end);
    if (win.length < 2) return [];
    // Enemy contact knockback moves the player too; leave out the 900ms
    // after every hit so only the player's own walking is counted.
    const hits = eventTimes(tl).filter((e) => e.type === 'hit' && e.t !== null).map((e) => e.t);
    const pushed = (t) => hits.some((h) => t >= h - 50 && t <= h + 900);
    let path = 0;
    for (let i = 1; i < win.length; i++) {
        if (pushed(win[i].t)) continue;
        path += Math.hypot(win[i].pos[0] - win[i - 1].pos[0], win[i].pos[2] - win[i - 1].pos[2]);
    }
    const out = [];
    if (path > 0.5) {
        out.push(finding('D2', 'S1', 'Key stuck after switching windows: the player keeps walking with no key held', {
            t: rel.t, until: end,
            evidence: { travelledAfterReleaseU: fmt(path), windowMs: Math.round(end - rel.t - 300), gameLostFocus: blur ? !blur.gameHasFocus : null },
            detail: 'W was released while another window had focus. The character kept walking in the background and after the player came back, with no key held.',
        }));
    }
    return out;
}

// detectKnockback: an enemy hit pushes the player back (server contact
// knockback is designed to be at most 1.5u). Measured on the SERVER position
// over the 800ms after each hit, only while the player holds no movement key,
// so the player's own walking cannot account for the displacement.
export function detectKnockback(tl) {
    const ps = playing(tl).filter((s) => s.spos);
    const hits = eventTimes(tl).filter((e) => e.type === 'hit' && e.t !== null);
    const moveKeys = new Set(['w', 'a', 's', 'd']);
    const heldDuring = (t0, t1) => {
        const held = new Set();
        for (const m of tl.marks) {
            if (m.t > t1) break;
            if (m.kind === 'keydown' && moveKeys.has(m.key)) { if (m.t >= t0) return true; held.add(m.key); }
            if ((m.kind === 'keyup' || m.kind === 'keyup-elsewhere') && moveKeys.has(m.key)) { if (m.t >= t0) return true; held.delete(m.key); }
        }
        return held.size > 0;
    };
    const pushes = [];
    for (const h of hits) {
        if (heldDuring(h.t - 100, h.t + 800)) continue;
        const a = sampleAt(ps, h.t - 100), b = sampleAt(ps, h.t + 800);
        if (!a || !b || a.gs !== 'playing') continue;
        pushes.push({ t: h.t, d: Math.hypot(b.spos[0] - a.spos[0], b.spos[2] - a.spos[2]), from: [a.spos[0], a.spos[2]], to: [b.spos[0], b.spos[2]], hpFrom: a.hp, hpTo: b.hp });
    }
    const big = pushes.filter((p) => p.d > 1.6);
    if (!big.length) return [];
    const worst = big.reduce((x, y) => (y.d > x.d ? y : x));
    return [finding('D4', 'S2', `An enemy hit throws the player ${fmt(worst.d, 1)}u (more than a cell) with no input`, {
        t: worst.t - 100, until: worst.t + 800,
        evidence: { hitsJudged: pushes.length, over1_6u: big.length, worstU: fmt(worst.d), from: worst.from.map((v) => +v.toFixed(2)), to: worst.to.map((v) => +v.toFixed(2)), hp: `${worst.hpFrom}->${worst.hpTo}` },
        detail: 'Consecutive contact hits chain their knockback, launching the player through the maze (and into walls) while HP drains.',
    })];
}
