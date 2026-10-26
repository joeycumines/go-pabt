// view.mjs - D1 (is the 3D view legible?) and the frozen-screen half of D4.
// Both judge the composited screencast frames: what the player was shown.

import { viewMetrics } from '../image.mjs';
import { finding, playing, sampleAt, hudMasks, inSegment, marksOf, fmt, between } from './common.mjs';

// The 3D view region, as frame fractions: below the top HUD strip and above
// the weapon/status bar, where the maze itself is drawn.
export const VIEW = { x0: 0.04, y0: 0.08, x1: 0.96, y1: 0.92 };
// The first-person weapon model is drawn into the canvas bottom-right; it is
// textured and always present, so it would make any view look "legible".
export const WEAPON = { x0: 0.6, y0: 0.68, x1: 1, y1: 1 };

// A frame is illegible when the maze region is effectively ONE flat colour:
// the 5th-95th percentile luminance spread is at most 4 levels. Calibrated
// on this build (seed 12345, 640x400 screencast, HUD masked): facing the
// spawn wall or any wall at <1u scores 0; a flat wall with one visible corner
// strip 8-9; corridor/corner views 10-29 (a legible corridor with a bright
// floor scored 10, so the cut sits well below it). Edge energy is reported
// but not used: flat-shaded geometry has low edge energy even when legible
// (0.01-0.26 across all of these frames). It must persist >=1s so a pilot
// sweeping past a wall at a corner does not count.
export const ILLEGIBLE = { maxRange: 4, minMs: 1000 };

export function isIllegible(m) {
    return m.n > 0 && m.range <= ILLEGIBLE.maxRange;
}

export function detectIllegibleView(tl, frames, grid) {
    const out = [];
    const ps = playing(tl);
    if (!ps.length) return out;
    const locks = marksOf(tl, 'locked').map((m) => m.t);
    let run = null, lastEval = -Infinity;
    const close = () => {
        if (run && run.t1 - run.t0 >= ILLEGIBLE.minMs) {
            const s = run.s;
            const ray = grid && s ? grid.castRay(s.pos[0], s.pos[2], Math.sin(s.yaw), Math.cos(s.yaw)) : null;
            // "On entry" = the first seconds after the player clicks into the
            // game (first time or after re-entering), when they have not yet
            // touched the mouse to look around.
            const onEntry = inSegment(tl, 'first-view', run.t0) || locks.some((t) => run.t0 >= t - 200 && run.t0 - t < 3000);
            out.push(finding('D1', onEntry ? 'S1' : 'S2',
                onEntry ? 'The view right after entering the game is a blank, featureless screen'
                    : 'The 3D view is a featureless flat colour',
                {
                    t: run.t0, until: run.t1,
                    evidence: {
                        durationMs: Math.round(run.t1 - run.t0),
                        luminanceRange: run.m.range, edgeEnergy: fmt(run.m.edge), medianLum: run.m.p50,
                        wallAheadDist: ray ? fmt(ray.dist) : null,
                        pos: s && s.pos.map((v) => +v.toFixed(2)), yaw: s && fmt(s.yaw),
                    },
                    detail: ray && ray.dist < 1.5
                        ? `The player faces a wall ${fmt(ray.dist)}u away, but it is drawn as an untextured flat surface that cannot be told apart from empty space.`
                        : 'Nothing in the maze region has visible structure.',
                }));
        }
        run = null;
    };
    for (const f of tl.frames) {
        if (f.t - lastEval < 90) continue;
        lastEval = f.t;
        const s = sampleAt(ps, f.t);
        const dom = sampleAt(tl.dom, f.t);
        if (!s || s.gs !== 'playing' || !dom || Object.keys(dom.modals || {}).length) { close(); continue; }
        const m = viewMetrics(frames.image(f), VIEW, [WEAPON, ...hudMasks(dom)]);
        if (isIllegible(m)) {
            if (!run) run = { t0: f.t, t1: f.t, m, s };
            run.t1 = f.t;
        } else close();
    }
    close();
    return out;
}

// detectFrozenScreen: the screencast only emits a frame when the composited
// page changes. A gap of >=500ms with no new frame while the player's state
// moved means the player was looking at a frozen picture.
export function detectFrozenScreen(tl) {
    const out = [];
    const ps = playing(tl);
    const f = tl.frames;
    for (let i = 1; i < f.length; i++) {
        const gap = f[i].t - f[i - 1].t;
        if (gap < 500) continue;
        const win = between(ps, f[i - 1].t, f[i].t);
        if (win.length < 2) continue;
        const a = win[0], b = win[win.length - 1];
        const moved = Math.hypot(b.pos[0] - a.pos[0], b.pos[2] - a.pos[2]);
        const turned = Math.abs(b.yaw - a.yaw);
        if (moved > 0.15 || turned > 0.1) {
            out.push(finding('D4', 'S1', 'Screen stopped updating while the game kept moving',
                { t: f[i - 1].t, until: f[i].t, evidence: { gapMs: Math.round(gap), movedU: fmt(moved), turnedRad: fmt(turned) } }));
        }
    }
    return out;
}
