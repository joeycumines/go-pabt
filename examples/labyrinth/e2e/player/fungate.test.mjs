// fungate.test.mjs - unit tests for fungate.mjs design band evaluation
import test from 'node:test';
import assert from 'node:assert/strict';
import { computeRunMetrics, evaluateFunGate, formatFunGateMarkdown } from './fungate.mjs';
import { parseArgs } from './playtest-player.mjs';

function makeSyntheticTimeline({
    t0 = 1000,
    durationS = 180,
    firstEnemyDelayS = 10,
    enemyOnScreenPct = 50,
    longestDeadTimeS = 8,
    onScreenStillPct = 15,
    mobClosedInPct = 80,
    hpLostToUnseen = 0,
    hpLostTotal = 100,
    silentDamage = 0,
    meleeHits = 5,
    untelegraphedMelee = 0,
    won = true,
    archetypes = ['B', 'S', 'C'],
    clashes = 2,
} = {}) {
    const tEnd = t0 + durationS * 1000;
    const ps = [
        { t: t0, gs: 'playing', pos: [1.5, 0.4, 1.5], actors: [] },
        { t: t0 + firstEnemyDelayS * 1000, gs: 'playing', pos: [3.5, 0.4, 3.5], actors: [{ id: 'B-1', hp: 40, x: 5.5, z: 5.5 }] },
        { t: tEnd, gs: won ? 'won' : 'playing', pos: [19.5, 0.4, 19.5], actors: [] },
    ];

    const ev = [];
    if (won) {
        ev.push({ t: tEnd, type: 'win', data: {} });
    }
    for (const a of archetypes) {
        ev.push({ t: t0 + 20000, type: 'stunned', data: { minotaur: `${a}-1` } });
    }
    for (let i = 0; i < clashes; i++) {
        ev.push({ t: t0 + 30000 + i * 5000, type: 'clash', data: { x: 3.5, y: 3.5 } });
    }

    const marks = [
        { t: t0, kind: 'start' },
        { t: tEnd, kind: 'result', result: { result: won ? 'won' : 'caught' } },
    ];

    const tl = {
        seed: 12345,
        journey: 'J6',
        samples: ps,
        events: ev,
        marks,
        wire: ev.map((e) => ({ kind: 'event', type: e.type, data: e.data, t: e.t })),
        // Pre-computed feel metrics in timeline to avoid needing full spatial raycasting in synthetic tests:
        _feelProfile: {
            enemyOnScreenPct,
            longestDeadTimeS,
            onScreenStillPct,
            mobClosedInPct,
            server: { mobClosedInPct },
        },
    };

    return tl;
}

test('computeRunMetrics extracts metrics accurately from synthetic timeline', () => {
    const tl = makeSyntheticTimeline({
        durationS: 200,
        won: true,
        archetypes: ['B', 'S', 'C', 'K'],
    });

    const m = computeRunMetrics(tl, null);
    assert.equal(m.won, true);
    assert.equal(m.runLengthS, 200);
    assert.equal(m.archetypesEngagedCount, 4);
    assert.deepEqual(m.archetypesEngagedNames, ['Brute', 'Caster', 'Stalker', 'Swarm']);
});

test('evaluateFunGate passes when all 12 bands are satisfied', () => {
    // 5 seeds with 3 wins (target 2-4 wins out of 5), lengths [45, 55, 65, 75, 85] (median 65, target 30-120)
    const seeds = [12345, 777, 4242, 31337, 2024];
    const wins = [true, true, true, false, false];
    const lengths = [45, 55, 65, 75, 85];

    const runs = seeds.map((seed, idx) => ({
        seed,
        journey: 'J6',
        tl: makeSyntheticTimeline({
            durationS: lengths[idx],
            won: wins[idx],
            firstEnemyDelayS: 12,
            enemyOnScreenPct: 45,
            longestDeadTimeS: 10,
            onScreenStillPct: 20,
            mobClosedInPct: 75,
            archetypes: ['B', 'S', 'C'],
            clashes: 2,
        }),
    }));

    const result = evaluateFunGate(runs, new Map());
    assert.equal(result.pass, true);
    assert.equal(result.totalWins, 3);
    assert.equal(result.medianRunLengthS, 65);
    assert.equal(result.bands.length, 12);
    assert.ok(result.bands.every((b) => b.pass));
    assert.ok(result.markdown.includes('Verdict: PASS'));
});

test('evaluateFunGate fails when a band is violated (e.g. 0 wins)', () => {
    const seeds = [12345, 777, 4242, 31337, 2024];
    const runs = seeds.map((seed) => ({
        seed,
        journey: 'J6',
        tl: makeSyntheticTimeline({
            won: false,
        }),
    }));

    const result = evaluateFunGate(runs, new Map());
    assert.equal(result.pass, false);
    const winBand = result.bands.find((b) => b.id === 9);
    assert.ok(winBand);
    assert.equal(winBand.pass, false);
    assert.ok(result.markdown.includes('Verdict: FAIL'));
});

test('parseArgs configures fun gate defaults when --gate fun is specified', () => {
    const cfg = parseArgs(['--gate', 'fun']);
    assert.equal(cfg.gate, 'fun');
    assert.deepEqual(cfg.seeds, [12345, 777, 4242, 31337, 2024]);
    assert.deepEqual(cfg.journeys, ['J6']);
});

test('parseArgs allows explicit seeds and journeys override with --gate fun', () => {
    const cfg = parseArgs(['--gate', 'fun', '--seeds', '777,4242', '--journeys', 'J2']);
    assert.equal(cfg.gate, 'fun');
    assert.deepEqual(cfg.seeds, [777, 4242]);
    assert.deepEqual(cfg.journeys, ['J2']);
});

