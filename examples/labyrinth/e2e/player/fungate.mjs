// fungate.mjs - evaluates the 12 gameplay fun bands (D11_fun_bands)
// across J6 runs on 5 seeds:
//
//   1. time to first enemy on screen <= 25 s
//   2. enemy on screen 30-75% of play
//   3. longest dead time <= 15 s
//   4. on-screen enemies standing still <= 35%
//   5. server mobs that see the player close in >= 60%
//   6. HP lost to never-seen enemies <= 10%
//   7. silent damage 0
//   8. every melee hit telegraphed (D18) 100%
//   9. pilot wins 2-4 of 5 seeds
//  10. median J6 run 30-120 s
//  11. >= 3 archetypes engaged per run
//  12. >= 1 visible faction clash within 8u of the player per run

import { playing, sampleAt, eventTimes, fmt } from './detectors/common.mjs';
import { onScreen, feelProfile, serverPresence, detectUnseenDamage } from './detectors/feel.mjs';
import { detectSilentDamage } from './detectors/hud.mjs';
import { detectTelegraph } from './detectors/feel.mjs';

const ARCHETYPE_PREFIXES = ['B', 'S', 'C', 'K'];
const ARCHETYPE_NAMES = { B: 'Brute', S: 'Swarm', C: 'Caster', K: 'Stalker' };

export function computeRunMetrics(tl, grid) {
    const ps = playing(tl);
    const p = tl._feelProfile || feelProfile(tl, grid) || {};
    const rawEvents = eventTimes(tl).filter((e) => e && e.t !== null);
    const ev = rawEvents.length > 0 ? rawEvents : (tl.events || []).filter((e) => e && typeof e.t === 'number');

    // 1. Time to first enemy on screen
    let timeToFirstEnemyS = tl._timeToFirstEnemyS !== undefined ? tl._timeToFirstEnemyS : Infinity;
    if (timeToFirstEnemyS === Infinity && ps.length) {
        const t0 = ps[0].t;
        for (const s of ps) {
            if (s.gs !== 'playing') continue;
            const seen = (s.actors || []).some((a) => a.hp > 0 && (grid ? onScreen(grid, s, a) : (a.onScreen ?? true)));
            if (seen) {
                timeToFirstEnemyS = +((s.t - t0) / 1000).toFixed(1);
                break;
            }
        }
    }

    // 2. Enemy on screen percentage (30-75%)
    const enemyOnScreenPct = p.enemyOnScreenPct !== undefined ? p.enemyOnScreenPct : 0;

    // 3. Longest dead time (<= 15s)
    const longestDeadTimeS = p.longestDeadTimeS !== undefined ? p.longestDeadTimeS : 0;

    // 4. On-screen enemies standing still (<= 35%)
    const onScreenStillPct = p.onScreenStillPct !== undefined ? p.onScreenStillPct : 0;

    // 5. Server mobs closing in (>= 60%)
    const sPres = p.server || serverPresence(tl, grid);
    let serverMobClosingPct = 100;
    if (sPres && sPres.mobClosedInPct !== null) {
        serverMobClosingPct = sPres.mobClosedInPct;
    } else if (p.mobClosedInPct !== null && p.mobClosedInPct !== undefined) {
        serverMobClosingPct = p.mobClosedInPct;
    }

    // 6. HP lost to never-seen enemies (<= 10%)
    let hpLostToUnseenPct = 0;
    const unseenFindings = detectUnseenDamage(tl, grid);
    if (unseenFindings.length && unseenFindings[0].evidence) {
        const totalHp = unseenFindings[0].evidence.hpLostTotal || 1;
        const unseenHp = unseenFindings[0].evidence.hpLostToUnseen || 0;
        hpLostToUnseenPct = Math.round((unseenHp / totalHp) * 100);
    }

    // 7. Silent damage (0)
    const silentFindings = detectSilentDamage(tl);
    let silentDamageCount = 0;
    if (silentFindings.length && silentFindings[0].evidence) {
        silentDamageCount = silentFindings[0].evidence.silentDrops || 0;
    }

    // 8. Every melee hit telegraphed (100%)
    const telegraphFindings = detectTelegraph(tl);
    let meleeTelegraphPct = 100;
    if (telegraphFindings.length && telegraphFindings[0].evidence) {
        const total = telegraphFindings[0].evidence.totalMeleeHits || 1;
        const untelegraphed = telegraphFindings[0].evidence.untelegraphedHits || 0;
        meleeTelegraphPct = Math.round(((total - untelegraphed) / total) * 100);
    }

    // 9. Win detection
    const won = ev.some((e) => e.type === 'win') ||
        ps.some((s) => s.gs === 'won') ||
        (tl.marks || []).some((m) => m.kind === 'result' && m.result && m.result.result === 'won') ||
        (tl.wire || []).some((w) => w.kind === 'event' && w.type === 'win');

    // 10. Run length in seconds
    let runLengthS = 0;
    if (ps.length > 1) {
        runLengthS = Math.round((ps[ps.length - 1].t - ps[0].t) / 1000);
    } else if (tl.marks && tl.marks.length > 1) {
        runLengthS = Math.round((tl.marks[tl.marks.length - 1].t - tl.marks[0].t) / 1000);
    }

    // 11. Archetypes engaged (>= 3)
    const engagedArchetypes = new Set();
    for (const e of ev) {
        if (e.type === 'stunned' && e.data && e.data.minotaur) {
            const pfx = String(e.data.minotaur)[0];
            if (ARCHETYPE_PREFIXES.includes(pfx)) engagedArchetypes.add(pfx);
        }
        if (e.type === 'hit' && e.data && e.data.attacker) {
            const pfx = String(e.data.attacker)[0];
            if (ARCHETYPE_PREFIXES.includes(pfx)) engagedArchetypes.add(pfx);
        }
    }
    for (const m of (tl.marks || [])) {
        if (m.kind === 'engage' && m.id) {
            const pfx = String(m.id)[0];
            if (ARCHETYPE_PREFIXES.includes(pfx)) engagedArchetypes.add(pfx);
        }
    }
    const archetypesEngagedCount = engagedArchetypes.size;

    // 12. Visible faction clashes within 8u (>= 1)
    let clashesWithin8uCount = 0;
    for (const e of ev) {
        if (e.type === 'clash' && e.data && typeof e.data.x === 'number' && typeof e.data.y === 'number') {
            const s = sampleAt(ps, e.t);
            if (s && s.pos) {
                const dist = Math.hypot(e.data.x - s.pos[0], e.data.y - s.pos[2]);
                if (dist <= 8.0) {
                    if (!grid || onScreen(grid, s, { x: e.data.x, z: e.data.y })) {
                        clashesWithin8uCount++;
                    }
                }
            }
        }
    }

    return {
        timeToFirstEnemyS,
        enemyOnScreenPct,
        longestDeadTimeS,
        onScreenStillPct,
        serverMobClosingPct,
        hpLostToUnseenPct,
        silentDamageCount,
        meleeTelegraphPct,
        won,
        runLengthS,
        archetypesEngagedCount,
        archetypesEngagedNames: [...engagedArchetypes].map((p) => ARCHETYPE_NAMES[p]).sort(),
        clashesWithin8uCount,
    };
}

export function evaluateFunGate(runs, gridMap = new Map()) {
    const runMetrics = runs.map((r) => {
        const grid = gridMap.get(r.seed) || (r.tl ? (r.grid || gridMap.get(r.tl.seed)) : null);
        const m = computeRunMetrics(r.tl, grid);
        return { seed: r.seed, journey: r.journey, ...m };
    });

    // Compute median run length across all runs
    const lengths = runMetrics.map((r) => r.runLengthS).sort((a, b) => a - b);
    const medianRunLengthS = lengths.length
        ? (lengths.length % 2 === 1
            ? lengths[Math.floor(lengths.length / 2)]
            : Math.round((lengths[lengths.length / 2 - 1] + lengths[lengths.length / 2]) / 2))
        : 0;

    const totalWins = runMetrics.filter((r) => r.won).length;

    const bands = [
        {
            id: 1,
            name: 'Time to first enemy on screen',
            target: '<= 25 s',
            pass: runMetrics.every((r) => r.timeToFirstEnemyS <= 25),
            actual: runMetrics.map((r) => `${r.timeToFirstEnemyS}s`).join(', '),
            details: runMetrics.map((r) => ({ seed: r.seed, val: r.timeToFirstEnemyS, pass: r.timeToFirstEnemyS <= 25 })),
        },
        {
            id: 2,
            name: 'Enemy on screen of play',
            target: '30 - 75%',
            pass: runMetrics.every((r) => r.enemyOnScreenPct >= 30 && r.enemyOnScreenPct <= 75),
            actual: runMetrics.map((r) => `${r.enemyOnScreenPct}%`).join(', '),
            details: runMetrics.map((r) => ({ seed: r.seed, val: r.enemyOnScreenPct, pass: r.enemyOnScreenPct >= 30 && r.enemyOnScreenPct <= 75 })),
        },
        {
            id: 3,
            name: 'Longest dead time',
            target: '<= 15 s',
            pass: runMetrics.every((r) => r.longestDeadTimeS <= 15),
            actual: runMetrics.map((r) => `${r.longestDeadTimeS}s`).join(', '),
            details: runMetrics.map((r) => ({ seed: r.seed, val: r.longestDeadTimeS, pass: r.longestDeadTimeS <= 15 })),
        },
        {
            id: 4,
            name: 'On-screen enemies standing still',
            target: '<= 35%',
            pass: runMetrics.every((r) => r.onScreenStillPct <= 35),
            actual: runMetrics.map((r) => `${r.onScreenStillPct}%`).join(', '),
            details: runMetrics.map((r) => ({ seed: r.seed, val: r.onScreenStillPct, pass: r.onScreenStillPct <= 35 })),
        },
        {
            id: 5,
            name: 'Server mobs that see player close in',
            target: '>= 60%',
            pass: runMetrics.every((r) => r.serverMobClosingPct >= 60),
            actual: runMetrics.map((r) => `${r.serverMobClosingPct}%`).join(', '),
            details: runMetrics.map((r) => ({ seed: r.seed, val: r.serverMobClosingPct, pass: r.serverMobClosingPct >= 60 })),
        },
        {
            id: 6,
            name: 'HP lost to never-seen enemies',
            target: '<= 10%',
            pass: runMetrics.every((r) => r.hpLostToUnseenPct <= 10),
            actual: runMetrics.map((r) => `${r.hpLostToUnseenPct}%`).join(', '),
            details: runMetrics.map((r) => ({ seed: r.seed, val: r.hpLostToUnseenPct, pass: r.hpLostToUnseenPct <= 10 })),
        },
        {
            id: 7,
            name: 'Silent damage',
            target: '0 occurrences',
            pass: runMetrics.every((r) => r.silentDamageCount === 0),
            actual: runMetrics.map((r) => `${r.silentDamageCount}`).join(', '),
            details: runMetrics.map((r) => ({ seed: r.seed, val: r.silentDamageCount, pass: r.silentDamageCount === 0 })),
        },
        {
            id: 8,
            name: 'Melee hits telegraphed (D18)',
            target: '100%',
            pass: runMetrics.every((r) => r.meleeTelegraphPct === 100),
            actual: runMetrics.map((r) => `${r.meleeTelegraphPct}%`).join(', '),
            details: runMetrics.map((r) => ({ seed: r.seed, val: r.meleeTelegraphPct, pass: r.meleeTelegraphPct === 100 })),
        },
        {
            id: 9,
            name: 'Pilot wins',
            target: '2 - 4 of 5 seeds',
            pass: totalWins >= 2 && totalWins <= 4,
            actual: `${totalWins} / ${runMetrics.length} wins (${runMetrics.map((r) => r.won ? 'W' : 'L').join(', ')})`,
            details: runMetrics.map((r) => ({ seed: r.seed, won: r.won })),
        },
        {
            id: 10,
            name: 'Median J6 run length',
            target: '30 - 120 s',
            pass: medianRunLengthS >= 30 && medianRunLengthS <= 120,
            actual: `${medianRunLengthS}s (lengths: ${lengths.map((l) => `${l}s`).join(', ')})`,
            details: { median: medianRunLengthS, lengths },
        },
        {
            id: 11,
            name: 'Archetypes engaged per run',
            target: '>= 3 archetypes',
            pass: runMetrics.every((r) => r.archetypesEngagedCount >= 3),
            actual: runMetrics.map((r) => `${r.archetypesEngagedCount} (${r.archetypesEngagedNames.join('+') || 'none'})`).join(', '),
            details: runMetrics.map((r) => ({ seed: r.seed, count: r.archetypesEngagedCount, archetypes: r.archetypesEngagedNames, pass: r.archetypesEngagedCount >= 3 })),
        },
        {
            id: 12,
            name: 'Visible faction clashes within 8u',
            target: '>= 1 clash per run',
            pass: runMetrics.every((r) => r.clashesWithin8uCount >= 1),
            actual: runMetrics.map((r) => `${r.clashesWithin8uCount}`).join(', '),
            details: runMetrics.map((r) => ({ seed: r.seed, count: r.clashesWithin8uCount, pass: r.clashesWithin8uCount >= 1 })),
        },
    ];

    const allPassed = bands.every((b) => b.pass);

    const markdown = formatFunGateMarkdown({ allPassed, bands, runMetrics, totalWins, medianRunLengthS });

    return {
        pass: allPassed,
        bands,
        runMetrics,
        totalWins,
        medianRunLengthS,
        markdown,
    };
}

export function formatFunGateMarkdown({ allPassed, bands, runMetrics, totalWins, medianRunLengthS }) {
    const lines = [
        '# Fun Gate Verification Report',
        '',
        `**Verdict: ${allPassed ? 'PASS' : 'FAIL'}**`,
        '',
        `Evaluated across ${runMetrics.length} seeds: ${runMetrics.map((r) => r.seed).join(', ')} on journey J6.`,
        '',
        '| Band | Design Target | Verdict | Actual Measured Values across Seeds |',
        '| :--- | :--- | :---: | :--- |',
    ];

    for (const b of bands) {
        lines.push(`| **${b.name}** | \`${b.target}\` | **${b.pass ? 'PASS' : 'FAIL'}** | ${b.actual} |`);
    }

    lines.push('');
    lines.push('## Per-Seed Summary');
    lines.push('');
    lines.push('| Seed | Result | Time | Enemies in View | Dead Time | Still % | Closing % | Archetypes | Clashes <=8u |');
    lines.push('| :--- | :---: | :---: | :---: | :---: | :---: | :---: | :--- | :---: |');

    for (const r of runMetrics) {
        lines.push(`| ${r.seed} | ${r.won ? 'WON' : 'CAUGHT'} | ${r.runLengthS}s | ${r.enemyOnScreenPct}% | ${r.longestDeadTimeS}s | ${r.onScreenStillPct}% | ${r.serverMobClosingPct}% | ${r.archetypesEngagedCount} (${r.archetypesEngagedNames.join('+') || 'none'}) | ${r.clashesWithin8uCount} |`);
    }

    lines.push('');
    return lines.join('\n');
}
