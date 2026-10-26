// truth.mjs - D17: does the game DRAW the world the server is simulating?
//
// Two channels that the game itself never compares:
//   - wire:  every snapshot the server sent, captured from the page's own
//            WebSocket before any client code touched it (inpage.mjs tap);
//   - drawn: the positions the client renders enemies at (its actor buffers,
//            sampled every presented frame).
// If they disagree, the player is fighting a picture of the game rather than
// the game: enemies that look frozen while they actually move, models you
// walk through because the real enemy is elsewhere, hits from "nowhere".

import { finding, playing, fmt } from './common.mjs';

function snaps(tl) {
    return (tl.wire || []).filter((w) => w.kind === 'snap');
}

// latestSnapBefore: the newest snapshot the client had received at time t.
function latestSnapBefore(list, t) {
    let lo = 0, hi = list.length - 1, best = -1;
    while (lo <= hi) {
        const mid = (lo + hi) >> 1;
        if (list[mid].t <= t) { best = mid; lo = mid + 1; } else hi = mid - 1;
    }
    return best >= 0 ? list[best] : null;
}

export function renderTruth(tl) {
    const sn = snaps(tl);
    if (sn.length < 10) return null;
    const ps = playing(tl).filter((s) => s.gs === 'playing');
    const errs = [];
    const perActor = new Map();
    let notDrawnNear = 0, judged = 0;
    let last = -Infinity;
    for (const s of ps) {
        if (s.t - last < 100) continue;
        last = s.t;
        const snap = latestSnapBefore(sn, s.t);
        if (!snap || s.t - snap.t > 500) continue;
        judged++;
        const server = new Map(snap.m.filter((m) => m[3] > 0).map((m) => [m[0], m]));
        const drawn = new Map((s.actors || []).map((a) => [a.id, a]));
        for (const [id, m] of server) {
            const a = drawn.get(id);
            if (!a) {
                if (Math.hypot(m[1] - s.pos[0], m[2] - s.pos[2]) < 6) notDrawnNear++;
                continue;
            }
            const e = Math.hypot(a.x - m[1], a.z - m[2]);
            errs.push(e);
            if (!perActor.has(id)) perActor.set(id, []);
            perActor.get(id).push({ t: s.t, dx: a.x, dz: a.z, sx: m[1], sz: m[2], e });
        }
    }
    if (!errs.length) return null;
    errs.sort((a, b) => a - b);
    const q = (p) => errs[Math.min(errs.length - 1, Math.floor(p * errs.length))];
    // Frozen render: over any 2s window the server moved the enemy >1u but
    // the drawn model moved <0.05u.
    const frozen = [];
    for (const [id, rows] of perActor) {
        for (let i = 0, j = 0; i < rows.length; i++) {
            while (j < rows.length && rows[j].t - rows[i].t < 2000) j++;
            if (j >= rows.length) break;
            const a = rows[i], b = rows[j];
            const serverMoved = Math.hypot(b.sx - a.sx, b.sz - a.sz);
            const drawnMoved = Math.hypot(b.dx - a.dx, b.dz - a.dz);
            if (serverMoved > 1 && drawnMoved < 0.05) { frozen.push({ id, t: a.t, serverMoved, drawnMoved }); break; }
        }
    }
    return {
        samplesJudged: judged, pairs: errs.length,
        medianErrU: q(0.5), p95ErrU: q(0.95), maxErrU: errs[errs.length - 1],
        pctOver1u: Math.round((errs.filter((e) => e > 1).length / errs.length) * 100),
        frozen, actorsSeen: perActor.size, notDrawnNear,
        worst: [...perActor.values()].flat().reduce((x, y) => (y.e > x.e ? y : x)),
    };
}

export function detectRenderTruth(tl) {
    const r = renderTruth(tl);
    if (!r) return [];
    const out = [];
    if (r.frozen.length) {
        out.push(finding('D17', 'S1', 'Enemies are drawn frozen in place while the server moves them', {
            t: r.frozen[0].t, until: r.frozen[0].t + 2000,
            evidence: {
                frozenActors: r.frozen.length, ofActorsSeen: r.actorsSeen,
                example: `${r.frozen[0].id}: server moved ${fmt(r.frozen[0].serverMoved)}u in 2s, drawn model moved ${fmt(r.frozen[0].drawnMoved, 3)}u`,
                medianDrawnVsServerU: fmt(r.medianErrU), p95U: fmt(r.p95ErrU), pctPairsOver1u: r.pctOver1u,
            },
            detail: 'What the player sees is not where the enemies are. They look idle, hits come from enemies that are really elsewhere, the player walks through models, and an enemy "teleports" when its model is finally re-created at its real position.',
        }));
    } else if (r.p95ErrU > 1) {
        out.push(finding('D17', 'S2', 'Enemies are often drawn far from where they really are', {
            t: r.worst.t, evidence: { medianU: fmt(r.medianErrU), p95U: fmt(r.p95ErrU), maxU: fmt(r.maxErrU), pctPairsOver1u: r.pctOver1u },
        }));
    }
    if (r.notDrawnNear > 3) {
        out.push(finding('D17', 'S2', 'Live enemies near the player are missing from the screen', { evidence: { samples: r.notDrawnNear } }));
    }
    return out;
}
