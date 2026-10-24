// botlab.test.mjs - unit tests for the playtest harness.
//
// Self-contained: no browser and no server are needed for these tests.
// Run: node --test examples/labyrinth/e2e/botlab.test.mjs

import assert from 'node:assert';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

import { compileBot } from '../client/js/bot-runtime.mjs';
import { createBotAPI, newCommand, pathTo, losClear, advantage } from '../client/js/bot-sdk.mjs';
import { selectStoryBeats } from './sheets.mjs';
import { buildObservation, formatBundle } from './observation.mjs';
import { alignShots, parseArgs, mergeEpisodeStats } from './botlab.mjs';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

// --- helpers ---------------------------------------------------------------

// fakeWorld builds the world shape the SDK reads.
function fakeWorld(overrides = {}) {
    const grid = new Array(21 * 21).fill(0);
    for (let z = 0; z < 21; z++) {
        for (let x = 0; x < 21; x++) {
            if (x === 0 || z === 0 || x === 20 || z === 20) continue;
            grid[z * 21 + x] = 1;
        }
    }
    const world = {
        maze: { w: 21, h: 21, grid, startX: 1.5, startY: 1.5, gateX: 19.5, gateY: 19.5 },
        camera: { pos: [1.5, 0.5, 1.5], yaw: 0, pitch: 0 },
        actors: new Map(),
        powerUps: [],
        hazards: [],
        territory: [0, 1, 2, 3],
        round: 10,
        phase: 'playing',
        intensity: 0,
        hp: 100,
        maxHp: 100,
        playerElement: 0,
        frame: 1,
        time: 0,
        btRows: [],
        shotsFired: 0,
        serverPos: null,
        botLog: [],
        finished: false,
        finishReason: '',
    };
    return Object.assign(world, overrides);
}

function apiFor(world) {
    const cmd = newCommand();
    const obs = {
        frame: 1,
        time: 0,
        round: 10,
        self: {
            x: world.camera.pos[0], y: world.camera.pos[1], z: world.camera.pos[2],
            yaw: world.camera.yaw, pitch: world.camera.pitch, hp: world.hp,
            maxHp: world.maxHp, element: world.playerElement, elementName: 'Fire',
            phase: world.phase, intensity: world.intensity,
        },
        gate: { x: world.maze.gateX, z: world.maze.gateY },
        start: { x: world.maze.startX, z: world.maze.startY },
        actors: [],
        powerups: [],
        hazards: [],
        territory: world.territory,
        maze: {
            w: world.maze.w, h: world.maze.h, grid: world.maze.grid,
            start: { x: world.maze.startX, z: world.maze.startY },
            gate: { x: world.maze.gateX, z: world.maze.gateY },
            walkable: () => true,
            cell: () => 1,
        },
        shotsFired: 0,
    };
    return { api: createBotAPI(world, cmd, obs, {}), cmd };
}

// --- bot compilation -------------------------------------------------------

function testCompileBot() {
    const ok = compileBot("module.exports = { name: 'x', tick(ctx, api) { return api.command(); } };");
    assert.equal(ok.error, undefined);
    assert.equal(ok.bot.name, 'x');
    assert.equal(typeof ok.bot.tick, 'function');

    const fnForm = compileBot('function tick(ctx, api) { return api.command(); }');
    assert.equal(fnForm.error, undefined);
    assert.equal(typeof fnForm.bot.tick, 'function');

    const named = compileBot("const botName = 'named';\nfunction tick(ctx, api) { return api.command(); }");
    assert.equal(named.bot.name, 'named');

    const broken = compileBot('module.exports = {');
    assert.ok(broken.error, 'a syntax error must be reported');
    assert.match(broken.error, /syntax error/);

    const empty = compileBot('');
    assert.ok(empty.error);

    const noTick = compileBot('module.exports = { name: "x" };');
    assert.ok(noTick.error);
    assert.match(noTick.error, /tick/);
}

// --- the documented API matches the runtime API ----------------------------

function testDocumentedApiMatchesRuntime() {
    const doc = readFileSync(path.join(__dirname, '..', 'docs', 'bot-sdk.md'), 'utf8');
    const documented = new Set();
    for (const line of doc.split('\n')) {
        const m = line.match(/^\|\s*`api\.([A-Za-z0-9_]+)/);
        if (m) documented.add(m[1]);
    }
    assert.ok(documented.size > 20, `expected a populated API table, found ${documented.size} entries`);

    const world = fakeWorld();
    const { api } = apiFor(world);
    const runtime = new Set(Object.keys(api));
    // The documented set must cover every runtime member.
    for (const key of runtime) {
        assert.ok(documented.has(key), `api.${key} is not documented in docs/bot-sdk.md`);
    }
    // And every documented member must exist at runtime.
    for (const key of documented) {
        assert.ok(runtime.has(key), `api.${key} is documented but does not exist at runtime`);
    }
}

// --- SDK behaviour ---------------------------------------------------------

function testPathToFindsRoute() {
    const world = fakeWorld();
    const path = pathTo(world.maze, 1.5, 1.5, 19.5, 19.5, false);
    assert.ok(path.length > 10, 'an open maze must yield a path');
    const last = path[path.length - 1];
    assert.ok(Math.abs(last[0] - 19.5) < 0.01 && Math.abs(last[1] - 19.5) < 0.01);
}

function testPathToUnreachableGoalLeadsToClosestCell() {
    const world = fakeWorld();
    // Wall off the goal completely.
    for (let z = 0; z < 21; z++) world.maze.grid[z * 21 + 19] = 0;
    for (let z = 0; z < 21; z++) world.maze.grid[z * 21 + 20] = 0;
    const path = pathTo(world.maze, 1.5, 1.5, 19.5, 19.5, false);
    assert.ok(path.length > 0, 'an unreachable goal must still yield progress');
}

function testLosClearRespectsWalls() {
    const world = fakeWorld();
    world.maze.grid[5 * 21 + 1] = 0; // a wall between (1,1) and (1,10)
    assert.equal(losClear(world.maze, 1.5, 1.5, 10.5, 1.5), true);
    assert.equal(losClear(world.maze, 1.5, 1.5, 1.5, 10.5), false, 'a wall must block line of sight');
}

function testAdvantageCycle() {
    assert.equal(advantage(0, 1), 2, 'fire beats ice');
    assert.equal(advantage(1, 0), 0.5, 'ice is resisted by fire');
    assert.equal(advantage(2, 2), 1, 'the same element is neutral');
    assert.equal(advantage(3, 0), 2, 'void beats fire');
}

function testMoveToSteersAndStops() {
    const world = fakeWorld();
    const { api, cmd } = apiFor(world);
    api.moveTo(19.5, 19.5);
    const mag = Math.hypot(cmd.move.x, cmd.move.z);
    assert.ok(Math.abs(mag - 1) < 1e-6, `moveTo must command a unit world direction, got ${mag}`);
    api.moveTo(1.5, 1.5);
    assert.equal(cmd.move.x, 0);
    assert.equal(cmd.move.z, 0);
}

function testApiVerbsAccumulateIntoTheCommand() {
    const world = fakeWorld();
    const { api, cmd } = apiFor(world);
    api.aimAt(10, 10);
    api.fire(true);
    api.sprint(true);
    api.jump();
    api.note('hello');
    const out = api.command();
    assert.ok(Math.abs(out.yaw - Math.atan2(10 - 1.5, 10 - 1.5)) < 1e-9);
    assert.equal(out.fire, true);
    assert.equal(out.sprint, true);
    assert.equal(out.jump, true);
    assert.equal(out.note, 'hello');
    assert.equal(cmd.fire, true);
}

function testApiLogAndFinish() {
    const world = fakeWorld();
    const { api } = apiFor(world);
    api.log('count', 3);
    assert.equal(world.botLog.length, 1);
    assert.match(world.botLog[0].message, /count 3/);
    api.finish('done');
    assert.equal(world.finished, true);
    assert.equal(world.finishReason, 'done');
}

// --- sheet beat selection --------------------------------------------------

function testStoryBeatsAreDerivedFromTheSession() {
    const frames = [];
    for (let i = 0; i < 40; i++) {
        frames.push({
            i,
            t: i * 0.1,
            round: i * 3,
            hp: 100,
            x: 1 + i * 0.2,
            z: 1,
            cell: [i % 21, 1],
            nearest: null,
            events: i === 10 ? ['hit'] : [],
            gateDistance: Math.abs(19.5 - (1 + i * 0.2)),
        });
    }
    const picks = selectStoryBeats(frames, { maxBeats: 9 });
    assert.ok(picks.length >= 6, `expected a filled story sheet, got ${picks.length} beats`);
    const reasons = picks.map((p) => p.why);
    assert.ok(reasons.some((r) => r.includes('first frame')));
    assert.ok(reasons.some((r) => r.includes('hit')), 'a hit event must produce a beat');
    assert.ok(reasons.some((r) => r.includes('closest approach')));
    // Beats are ordered by time.
    for (let i = 1; i < picks.length; i++) {
        assert.ok(picks[i].frame.i >= picks[i - 1].frame.i);
    }
}

// --- alignment -------------------------------------------------------------

function testAlignShotsJoinsFramesToSamples() {
    const samples = [
        { i: 0, epochMs: 1000, x: 1, z: 1 },
        { i: 1, epochMs: 2000, x: 2, z: 2 },
        { i: 2, epochMs: 3000, x: 3, z: 3 },
    ];
    const shots = [
        { data: 'a', epochMs: 1500 },
        { data: 'b', epochMs: 2500 },
        { data: 'c', epochMs: 3500 },
    ];
    const aligned = alignShots(shots, samples);
    assert.equal(aligned.length, 3);
    assert.equal(aligned[0].sample.i, 0);
    assert.equal(aligned[1].sample.i, 1);
    assert.equal(aligned[2].sample.i, 2);
}

// --- observation bundle ----------------------------------------------------

function testObservationNamesTheStuckCell() {
    const obs = buildObservation({
        run: { name: 'r', seed: 1, durationSec: 10 },
        bot: { name: 'b', frames: 100 },
        stats: {
            samples: 100, stuckSamples: 40, stuckCells: [{ cell: '7,3', count: 30 }],
            cellsVisited: 20, distanceTravelled: 30, shotsFired: 2, shotsHit: 1, damageTaken: 10,
        },
        frames: [{ i: 1, t: 0, round: 1, hp: 50, x: 7, z: 3, cell: [7, 3], nearest: null, events: [], gateDistance: 9 }],
        events: [{ t: 1, i: 1, type: 'hit', data: null }],
        errors: [],
        logs: [],
    });
    assert.ok(obs.findings.some((f) => f.kind === 'stuck'), 'a stuck bot must be reported');
    const bundle = formatBundle(obs);
    assert.match(bundle, /what to fix/);
    assert.match(bundle, /\(7,3\)/);
}

function testObservationReportsBotErrorsWithTheFrameIndex() {
    const obs = buildObservation({
        run: { name: 'r', seed: 1, durationSec: 2 },
        bot: { name: 'b', frames: 10 },
        stats: { samples: 10, cellsVisited: 1, distanceTravelled: 1, shotsFired: 0, shotsHit: 0, damageTaken: 0, stuckSamples: 0 },
        frames: [{ i: 1, t: 0, round: 1, hp: 100, x: 1, z: 1, cell: [1, 1], nearest: null, events: [], gateDistance: 25 }],
        events: [],
        errors: [{ frame: 42, t: 1.2, message: 'boom', stack: 'Error: boom\n at tick' }],
        logs: [],
    });
    const bundle = formatBundle(obs);
    assert.match(bundle, /frame 42/);
    assert.match(bundle, /boom/);
}

function testObservationAcceptsAWinEventAsReachingTheGate() {
    const obs = buildObservation({
        run: { name: 'r', seed: 1, durationSec: 12 },
        bot: { name: 'b', frames: 300 },
        stats: { samples: 100, cellsVisited: 40, distanceTravelled: 60, shotsFired: 4, shotsHit: 4, damageTaken: 20, stuckSamples: 0 },
        frames: [{ i: 1, t: 11, round: 330, hp: 40, x: 19.5, z: 19.5, cell: [19, 19], nearest: null, events: [], gateDistance: 1.9 }],
        events: [{ t: 11, i: 1, type: 'win', data: null }],
        errors: [],
        logs: [],
    });
    assert.equal(obs.outcome.reachedGate, true, 'a win event means the gate was reached');
}

// --- statistics merging ----------------------------------------------------

function testMergeEpisodeStats() {
    const merged = mergeEpisodeStats([
        { stats: { samples: 10, events: 2, eventCounts: { hit: 2 }, cellsVisited: 5, distanceTravelled: 3.5, stuckSamples: 1, shotsHit: 1, damageTaken: 4, deaths: 0, gateDistanceMin: 5, gateDistanceMinAt: null, gateDistanceNow: 6 } },
        { stats: { samples: 20, events: 3, eventCounts: { hit: 1, death: 1 }, cellsVisited: 7, distanceTravelled: 6.25, stuckSamples: 2, shotsHit: 2, damageTaken: 6, deaths: 1, gateDistanceMin: 2, gateDistanceMinAt: null, gateDistanceNow: 3 } },
    ]);
    assert.equal(merged.samples, 30);
    assert.equal(merged.eventCounts.hit, 3);
    assert.equal(merged.eventCounts.death, 1);
    assert.equal(merged.cellsVisited, 12);
    assert.equal(merged.distanceTravelled, 9.75);
    assert.equal(merged.gateDistanceMin, 2);
}

// --- argument parsing ------------------------------------------------------

function testParseArgs() {
    const cfg = parseArgs(['--bot', 'b.js', '--seconds', '7', '--fps', '3', '--assert-frames', '100']);
    assert.equal(cfg.botFile, path.resolve(process.cwd(), 'b.js'));
    assert.equal(cfg.seconds, 7);
    assert.equal(cfg.fps, 3);
    assert.equal(cfg.assertFrames, 100);
    assert.equal(cfg.name, 'b');
    assert.equal(cfg.captureHeight, Math.round(cfg.captureWidth * 0.625));
}

// --- run -------------------------------------------------------------------

const tests = [
    testCompileBot,
    testDocumentedApiMatchesRuntime,
    testPathToFindsRoute,
    testPathToUnreachableGoalLeadsToClosestCell,
    testLosClearRespectsWalls,
    testAdvantageCycle,
    testMoveToSteersAndStops,
    testApiVerbsAccumulateIntoTheCommand,
    testApiLogAndFinish,
    testStoryBeatsAreDerivedFromTheSession,
    testAlignShotsJoinsFramesToSamples,
    testObservationNamesTheStuckCell,
    testObservationReportsBotErrorsWithTheFrameIndex,
    testObservationAcceptsAWinEventAsReachingTheGate,
    testMergeEpisodeStats,
    testParseArgs,
];

let failed = 0;
for (const t of tests) {
    try {
        t();
        process.stdout.write(`ok   ${t.name}\n`);
    } catch (err) {
        failed++;
        process.stdout.write(`FAIL ${t.name}: ${err && err.message}\n`);
    }
}
process.stdout.write(`\n${tests.length - failed}/${tests.length} passed\n`);
if (failed > 0) process.exit(1);
