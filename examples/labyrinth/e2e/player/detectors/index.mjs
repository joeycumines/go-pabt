// index.mjs - runs every detector over one journey's timeline.

import { Grid } from '../maze.mjs';
import { FrameStore, finding } from './common.mjs';
import { detectIllegibleView, detectFrozenScreen } from './view.mjs';
import { detectControls, detectLook, detectSnaps, detectStalls, detectStuckKey, detectKnockback } from './control.mjs';
import { detectCrosshair, detectHpTruth, detectSilentDamage, detectLayering } from './hud.mjs';
import { detectEmbeddedActors, detectPlayerInWall, detectShots, detectFlow, detectHealth } from './world.mjs';
import { detectRenderTruth } from './truth.mjs';
import { detectBodyCollision, detectGunImpact, detectUnseenDamage, detectEngagement, detectGhosts } from './feel.mjs';

export function gridOf(tl) {
    if (!tl.grid) return null;
    const g = new Grid(tl.grid.w, tl.grid.h, tl.grid.cells);
    g.gate = tl.grid.gate;
    return g;
}

// guard turns cheat-guard violations into a finding that invalidates the run:
// a journey that called a debug action did not play the game as a player.
export function guard(tl) {
    if (!tl.violations.length) return [];
    return [finding('GUARD', 'S1', 'Run is INVALID: a debug action bypassed real player input', {
        t: tl.violations[0].t, evidence: { calls: [...new Set(tl.violations.map((v) => v.what))], count: tl.violations.length, stack: tl.violations[0].stack },
    })];
}

export function runDetectors(tl, runDir) {
    const grid = gridOf(tl);
    const frames = new FrameStore(tl, runDir);
    const all = [
        ...guard(tl),
        ...detectIllegibleView(tl, frames, grid),
        ...detectFrozenScreen(tl),
        ...detectControls(tl, frames, grid),
        ...detectLook(tl, frames),
        ...detectSnaps(tl),
        ...detectKnockback(tl),
        ...detectStalls(tl),
        ...detectStuckKey(tl),
        ...detectCrosshair(tl, grid),
        ...detectHpTruth(tl),
        ...detectSilentDamage(tl),
        ...detectLayering(tl),
        ...detectEmbeddedActors(tl, grid),
        ...detectPlayerInWall(tl, grid),
        ...detectShots(tl),
        ...detectFlow(tl),
        ...detectHealth(tl),
        ...detectBodyCollision(tl),
        ...detectGunImpact(tl),
        ...detectUnseenDamage(tl, grid),
        ...detectEngagement(tl, grid),
        ...detectGhosts(tl),
        ...detectRenderTruth(tl),
    ];
    const rank = { S1: 0, S2: 1, S3: 2 };
    return all.sort((a, b) => rank[a.severity] - rank[b.severity] || (a.t || 0) - (b.t || 0));
}
