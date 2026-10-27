// player.test.mjs - self-tests for the player-eye detectors.
//
//   node --test e2e/player/
//
// Every detector has a timeline that MUST trip it, and all but the combined
// engagement test also have one that must NOT. The pixel detectors are also held to golden frames captured from
// a live run of this game (fixtures/fixtures.json says what each one shows),
// so a change that makes the harness blind to the real spawn-wall or
// inverted-mouse-look bugs fails here.

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { decodePng, viewMetrics, horizontalShift } from './image.mjs';
import { Grid, WALL, FLOOR, GAP } from './maze.mjs';
import { FrameStore } from './detectors/common.mjs';
import { VIEW, WEAPON, isIllegible, detectIllegibleView, detectFrozenScreen } from './detectors/view.mjs';
import { detectControls, detectLook, detectSnaps, detectStalls, detectStuckKey, detectKnockback } from './detectors/control.mjs';
import { detectCrosshair, detectSilentDamage, detectLayering, detectHpTruth } from './detectors/hud.mjs';
import { detectFlow, detectPlayerInWall, detectShots, detectEmbeddedActors, detectHealth } from './detectors/world.mjs';
import { detectBodyCollision, detectGhosts, detectEngagement, detectUnseenDamage, detectGunImpact } from './detectors/feel.mjs';
import { detectRenderTruth } from './detectors/truth.mjs';
import { guard, runDetectors } from './detectors/index.mjs';
import { diary } from './narrative.mjs';
import { installProbe } from './inpage.mjs';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const FIX = path.join(HERE, 'fixtures');
const fixMeta = JSON.parse(readFileSync(path.join(FIX, 'fixtures.json'), 'utf8'));
const img = (name) => decodePng(readFileSync(path.join(FIX, name)));

// A 7x7 maze: border walls, open interior, a wall column at x=3 (z 1..4),
// and a GAP cell at (5,5).
function grid() {
    const w = 7, h = 7, c = [];
    for (let z = 0; z < h; z++) {
        for (let x = 0; x < w; x++) {
            let v = (x === 0 || z === 0 || x === w - 1 || z === h - 1) ? WALL : FLOOR;
            if (x === 3 && z >= 1 && z <= 4) v = WALL;
            if (x === 5 && z === 5) v = GAP;
            c.push(v);
        }
    }
    return { w, h, cells: c, gate: [5.5, 1.5] };
}

// tl builds a minimal timeline; samples default to "playing" with HP 100.
function tl(over = {}) {
    return {
        name: 'T', seed: 1, viewport: { width: 1280, height: 800 },
        samples: [], dom: [], violations: [], rafGaps: [], pageErrors: [], console: [], network: [], sockets: [],
        marks: [], frames: [], events: [], wire: [], loads: [], grid: grid(), ...over,
    };
}
function S(t, o = {}) {
    return { t, gs: 'playing', pos: [1.5, 0.5, 1.5], spos: [1.5, 0.5, 1.5], yaw: 0, hp: 100, round: 100 + t / 33, actors: [], locked: true, vel: [0, 0, 0], input: { forward: 0, strafe: 0 }, ...o };
}
function dom(t, o = {}) {
    return { t, W: 1280, H: 800, modals: {}, hud: {}, chips: {}, hpFrac: 1, topCenter: 'glcanvas', topOverlay: null, killFeed: [], ...o };
}
const ids = (fs) => fs.map((f) => `${f.id}:${f.severity}`);

// ---------- geometry ----------

test('Grid.segment sees walls, gaps on the line, and a gap under the shooter', () => {
    const g = new Grid(7, 7, grid().cells);
    assert.equal(g.segment(1.5, 2.5, 4.5, 2.5).wall, true);
    assert.equal(g.segment(1.5, 5.5, 2.5, 5.5).wall, false);
    assert.equal(g.segment(4.5, 5.5, 4.9, 5.5).gap, false, 'segment ending before the gap cell');
    assert.equal(g.segment(4.5, 5.5, 5.9, 5.5).gap, true, 'target standing on a gap: the server ray samples inside it');
    assert.equal(g.segment(4.5, 5.5, 5.5, 4.5).gap, false);
    assert.equal(g.segment(5.4, 5.4, 5.6, 2.5).startGap, true, 'shooter standing on a gap');
    assert.equal(g.segment(5.4, 5.4, 5.6, 2.5).gap, true);
    assert.ok(Math.abs(g.castRay(1.5, 1.5, 1, 0).dist - 1.5) < 1e-9, 'wall face at x=3 is 1.5u away');
    assert.deepEqual(g.path(1.5, 1.5, 5.5, 1.5).at(-1), [5.5, 1.5]);
});

// ---------- golden frames from the live game ----------

test('D1 golden: the real spawn-wall frame is illegible, a real corridor is not', () => {
    const wall = viewMetrics(img('spawn-wall.png'), VIEW, [WEAPON, ...fixMeta['spawn-wall.png'].masks]);
    const cor = viewMetrics(img('corridor.png'), VIEW, [WEAPON, ...fixMeta['corridor.png'].masks]);
    assert.ok(isIllegible(wall), `spawn wall range ${wall.range}`);
    assert.ok(!isIllegible(cor), `corridor range ${cor.range}`);
});

test('D3 golden: pixel shift reads a mouse-right turn as scene-moved-right and D strafe as scene-moved-left', () => {
    const look = horizontalShift(img('look-right-before.png'), img('look-right-after.png'));
    const strafe = horizontalShift(img('strafe-d-before.png'), img('strafe-d-after.png'));
    assert.ok(look.score > 0.5 && look.shift > 4, JSON.stringify(look));
    assert.ok(strafe.score > 0.5 && strafe.shift < -4, JSON.stringify(strafe));
    const back = horizontalShift(img('look-right-after.png'), img('look-right-before.png'));
    assert.ok(back.shift < -4, 'reversing the pair reverses the sign');
});

// Controls probe timeline wired to the golden frames.
function controlsTl({ lookFrames = ['look-right-before.png', 'look-right-after.png'] } = {}) {
    const marks = [];
    const samples = [];
    let t = 1000;
    const pos = [1.5, 0.5, 1.5];
    const key = (k, v) => {
        marks.push({ t, kind: 'probe', key: k }, { t, kind: 'keydown', key: k[0] === 'w' ? 'w' : k });
        samples.push(S(t, { pos: [...pos] }));
        pos[0] += v[0]; pos[2] += v[1];
        samples.push(S(t + 40, { pos: [...pos] }));
        t += 300;
        marks.push({ t, kind: 'keyup', key: k[0] === 'w' ? 'w' : k });
        t += 500;
        samples.push(S(t, { pos: [...pos] }));
    };
    key('w', [0, 1]); key('s', [0, -1]); key('d', [-0.2, 0]); key('a', [0.2, 0]);
    marks.push({ t, kind: 'probe', key: 'look-right' }, { t, kind: 'look', dx: 125 });
    const lookT0 = t;
    samples.push(S(t - 20, { yaw: 0, pos: [...pos] }));
    t += 300;
    samples.push(S(t, { yaw: 0.25, pos: [...pos] }));
    samples.push(S(t + 600, { yaw: 0.25, pos: [...pos] }));
    marks.push({ t: t + 600, kind: 'segment', name: 'end-controls' });
    // Frames: the strafe-D pair around the d probe, the look pair around the look.
    const dDown = marks.find((m) => m.kind === 'keydown' && m.key === 'd').t;
    const frames = [
        { t: dDown - 30, file: 'strafe-d-before.png' }, { t: dDown + 700, file: 'strafe-d-after.png' },
        { t: lookT0 - 30, file: lookFrames[0] }, { t: lookT0 + 300, file: lookFrames[1] },
    ].sort((a, b) => a.t - b.t);
    return tl({ marks, samples, frames });
}

test('D3: inverted mouse look is reported from real frames; a correct turn is not', () => {
    const bad = controlsTl();
    assert.ok(detectControls(bad, new FrameStore(bad, FIX)).some((f) => f.id === 'D3' && f.severity === 'S1'));
    const good = controlsTl({ lookFrames: ['look-right-after.png', 'look-right-before.png'] });
    assert.deepEqual(ids(detectControls(good, new FrameStore(good, FIX))).filter((x) => x.startsWith('D3')), []);
});

test('D2: a dead key and a non-opposed S are reported', () => {
    const t = controlsTl();
    // Make S move the same way as W.
    const sDown = t.marks.find((m) => m.kind === 'keydown' && m.key === 's').t;
    for (const s of t.samples) if (s.t > sDown) s.pos = [s.pos[0], s.pos[1], s.pos[2] + 2];
    assert.ok(detectControls(t, null).some((f) => f.title.startsWith('S does not move opposite')));
});

test('D2: a hit shake does not turn a correct strafe into a mirrored one', () => {
    const t = controlsTl();
    const dDown = t.marks.find((m) => m.kind === 'keydown' && m.key === 'd').t;
    // Reverse the D frame pair so the raw pixel shift looks like a mirror.
    const pair = t.frames.filter((f) => f.file.startsWith('strafe-d-'));
    if (pair.length === 2) { const a = pair[0].file; pair[0].file = pair[1].file; pair[1].file = a; }
    for (const s of t.samples) if (s.t >= dDown - 100 && s.t <= dDown + 700) s.shake = true;
    assert.deepEqual(ids(detectControls(t, new FrameStore(t, FIX))).filter((x) => x.startsWith('D2:S1')), []);
});

test('D1: a sustained flat view right after entering is S1, a brief one is ignored', () => {
    // The HUD rects recorded with the fixture frames, back in page pixels.
    const hudOf = (name) => Object.fromEntries(fixMeta[name].masks.map((m, i) => [`m${i}`, { rect: [m.x0 * 1280, m.y0 * 800, (m.x1 - m.x0) * 1280, (m.y1 - m.y0) * 800], text: '' }]));
    const mk = (ms) => {
        const frames = [], samples = [], domS = [];
        for (let t = 0; t <= ms; t += 100) { frames.push({ t, file: 'spawn-wall.png' }); samples.push(S(t)); domS.push(dom(t, { hud: hudOf('spawn-wall.png') })); }
        frames.push({ t: ms + 100, file: 'corridor.png' }); samples.push(S(ms + 100)); domS.push(dom(ms + 100, { hud: hudOf('corridor.png') }));
        return tl({ frames, samples, dom: domS, marks: [{ t: 0, kind: 'locked' }] });
    };
    const long = mk(1500);
    assert.deepEqual(ids(detectIllegibleView(long, new FrameStore(long, FIX), new Grid(7, 7, grid().cells))), ['D1:S1']);
    const short = mk(500);
    assert.deepEqual(detectIllegibleView(short, new FrameStore(short, FIX), null), []);
});

// ---------- HUD ----------

test('D5: red crosshair with the only enemy behind a wall is a lie; through open floor it is not; under a menu it is not judged', () => {
    const g = new Grid(7, 7, grid().cells);
    const behindWall = [{ id: 'S-1', x: 4.5, z: 1.5, hp: 10 }];
    const mk = (actors, modals = {}) => {
        const samples = [], domS = [];
        for (let t = 0; t <= 600; t += 50) { samples.push(S(t, { yaw: Math.PI / 2, aim: true, actors })); domS.push(dom(t, { modals })); }
        return tl({ samples, dom: domS });
    };
    assert.deepEqual(ids(detectCrosshair(mk(behindWall), g)), ['D5:S2']);
    assert.deepEqual(detectCrosshair(mk([{ id: 'S-1', x: 2.5, z: 1.5, hp: 10 }]), g), []);
    assert.deepEqual(detectCrosshair(mk(behindWall, { 'tutorial-overlay': { rect: [0, 0, 1, 1], text: '' } }), g), []);
});

test('D5: negative HP is reported', () => {
    assert.deepEqual(ids(detectHpTruth(tl({ samples: [S(0, { hp: 3 }), S(30, { hp: -7 })] }))), ['D5:S2']);
});

test('D6: HP loss without any cue is reported, with a flash it is not', () => {
    const quiet = tl({ samples: [S(0, { hp: 100 }), S(30, { hp: 80 }), S(400, { hp: 80 })] });
    assert.deepEqual(ids(detectSilentDamage(quiet)), ['D6:S2']);
    const cued = tl({ samples: [S(0, { hp: 100 }), S(30, { hp: 80, flash: true }), S(400, { hp: 80 })] });
    assert.deepEqual(detectSilentDamage(cued), []);
});

test('D6: the page default HP before the first snapshot is not silent damage', () => {
    const reload = tl({
        samples: [S(0, { hp: 92, round: -1 }), S(20, { hp: 100, round: -1 }), S(40, { hp: 92, round: 269 })],
        loads: [{ t: 10 }],
    });
    assert.deepEqual(detectSilentDamage(reload), []);
    const after = tl({ samples: [S(0, { hp: 92, round: 269 }), S(30, { hp: 72, round: 270 })] });
    assert.deepEqual(ids(detectSilentDamage(after)), ['D6:S2']);
});

test('D15: a paused game with frozen enemies is not reported as still enemies', () => {
    const actor = { id: 'B-1', x: 1.5, z: 2.5, hp: 40, type: 0, el: 0 };
    const samples = (play) => [0, 100, 200, 300, 400, 500, 600].map((t) => S(t, { play, actors: [actor] }));
    const g = new Grid(7, 7, grid().cells);
    const paused = tl({ samples: samples(false) });
    assert.deepEqual(ids(detectEngagement(paused, g)), []);
    const playing = tl({ samples: samples(true) });
    assert.ok(ids(detectEngagement(playing, g)).includes('D15:S2'));
});

test('D7: end screen covered by another element is S1; stacked menus are S2', () => {
    const samples = [S(0), S(100, { gs: 'caught' })];
    const covered = tl({ samples, dom: [dom(500, { modals: { overlay: { rect: [0, 0, 100, 100], text: 'CAUGHT' }, 'tutorial-overlay': { rect: [0, 0, 100, 100], text: '' } }, topOverlay: 'tutorial-overlay>li' })] });
    const f = ids(detectLayering(covered));
    assert.ok(f.includes('D7:S1'));
    const fine = tl({ samples, dom: [dom(500, { modals: { overlay: { rect: [0, 0, 100, 100], text: 'CAUGHT' } }, topOverlay: 'overlay' })] });
    assert.deepEqual(detectLayering(fine), []);
    const stacked = tl({ dom: [0, 200, 400].map((t) => dom(t, { modals: { loading: { rect: [0, 0, 1, 1], text: '' }, 'tutorial-overlay': { rect: [0, 0, 1, 1], text: '' } } })) });
    assert.ok(ids(detectLayering(stacked)).includes('D7:S2'));
});

// ---------- flow / world ----------

test('D9: world advancing for minutes before the player enters is reported', () => {
    const t = tl({ samples: [S(0, { round: 13 }), S(9000, { round: 287 })], marks: [{ t: 9000, kind: 'locked' }], loads: [0] });
    assert.ok(ids(detectFlow(t)).includes('D9:S2'));
    const fresh = tl({ samples: [S(0, { round: 1 }), S(1500, { round: 46 })], marks: [{ t: 1500, kind: 'locked' }], loads: [0] });
    assert.deepEqual(detectFlow(fresh), []);
});

test('D9: the default HP of a freshly reloaded page is not damage under a menu', () => {
    const t = tl({
        samples: [S(0, { hp: 92, locked: false }), S(900, { hp: 92, locked: false }), S(1000, { hp: 100, round: -1, locked: false }), S(1100, { hp: 92, locked: false })],
        dom: [dom(1050, { modals: { 'pointer-lock-prompt': { rect: [0, 0, 100, 100], text: 'Click to enter' } } })],
        loads: [0, 950],
    });
    assert.deepEqual(detectFlow(t), []);
});

test('D9: a real HP drop under a menu after the reload is still reported', () => {
    const t = tl({
        samples: [S(0, { hp: 100, locked: false }), S(900, { hp: 100, locked: false }), S(1000, { hp: 100, locked: false }), S(1100, { hp: 92, locked: false })],
        dom: [dom(1050, { modals: { 'pointer-lock-prompt': { rect: [0, 0, 100, 100], text: 'Click to enter' } } })],
        loads: [0, 950],
    });
    assert.ok(ids(detectFlow(t)).includes('D9:S1'));
});

test('D9: an ignored end-screen click is reported', () => {
    assert.ok(ids(detectFlow(tl({ marks: [{ t: 5, kind: 'end-click-ignored', locked: true }] }))).includes('D9:S2'));
});

test('D8: camera inside a wall or outside the maze is S1; hugging a wall at collision distance is fine', () => {
    const g = new Grid(7, 7, grid().cells);
    const inWall = tl({ samples: [0, 50, 100, 150].map((t) => S(t, { pos: [3.5, 0.5, 2.5] })) });
    assert.deepEqual(ids(detectPlayerInWall(inWall, g)), ['D8:S1']);
    const hugging = tl({ samples: [0, 50, 100, 150].map((t) => S(t, { pos: [1.3, 0.5, 1.5] })) });
    assert.deepEqual(detectPlayerInWall(hugging, g), []);
});

test('D8: enemies standing inside walls are reported', () => {
    const g = new Grid(7, 7, grid().cells);
    const t = tl({ samples: [0, 200, 400].map((x) => S(x, { actors: [{ id: 'K-1', x: 3.5, z: 2.5, hp: 5 }] })) });
    assert.deepEqual(ids(detectEmbeddedActors(t, g)), ['D8:S2']);
});

test('D8: shots that fail while the shooter stands on a gap are attributed to the gap', () => {
    const shots = [0, 1, 2].map((i) => ({ t: 1000 + i * 600, actorId: 'S-1', dist: 3, yawErr: 0, gapOnLine: true, pos: [5.4, 0.5, 5.4] }));
    const t = tl({ marks: [{ t: 3000, kind: 'result', shots }], samples: [S(900, { pos: [5.4, 0.5, 5.4], actors: [{ id: 'S-1', x: 5.5, z: 2.5, hp: 10 }] })] });
    const f = detectShots(t);
    assert.ok(f.some((x) => x.id === 'D8' && /Standing on certain floor tiles/.test(x.title)));
    const landed = tl({ ...t, wire: shots.map((s) => ({ kind: 'event', t: s.t + 30, type: 'stunned', data: { minotaur: 'S-1', hp: 5 } })) });
    assert.deepEqual(detectShots(landed), []);
});

// ---------- controls / continuity ----------

test('D2: movement continuing after alt-tab with no key held is a stuck key; stopping is fine', () => {
    const mk = (moving, focusLost = true) => tl({
        marks: [{ t: 0, kind: 'blur', gameHasFocus: !focusLost }, { t: 10, kind: 'keyup-elsewhere', key: 'w' }, { t: 1500, kind: 'refocus' }],
        samples: [400, 800, 1200, 1600, 2000, 2400, 3000].map((t, i) => S(t, { pos: [1.5 + (moving ? i * 0.3 : 0), 0.5, 1.5] })),
    });
    assert.deepEqual(ids(detectStuckKey(mk(true))), ['D2:S1']);
    assert.deepEqual(detectStuckKey(mk(false)), []);
    assert.deepEqual(ids(detectStuckKey(mk(false, false))), ['HARNESS:S3'], 'a blur that never happened is reported, not silently passed');
});

test('D4: an unexplained per-frame jump is rubber-banding; normal walking is not', () => {
    const walk = tl({ samples: [0, 8, 16, 24].map((t, i) => S(t, { pos: [1.5 + i * 0.03, 0.5, 1.5] })) });
    assert.deepEqual(detectSnaps(walk), []);
    const snap = tl({ samples: [0, 8, 16].map((t, i) => S(t, { pos: [1.5 + (i === 2 ? 0.6 : i * 0.03), 0.5, 1.5] })) });
    assert.ok(ids(detectSnaps(snap)).includes('D4:S2'));
});

test('D4: a hit that throws the player more than a cell with no keys held is reported', () => {
    const t = tl({
        wire: [{ kind: 'event', t: 1000, type: 'hit', data: { damage: 15 } }],
        samples: [S(900, { spos: [1.5, 0.5, 1.5] }), S(1800, { spos: [1.5, 0.5, 3.8] })],
    });
    assert.ok(ids(detectKnockback(t)).includes('D4:S2'));
});

// ---------- feel / truth ----------

test('D11: the player and an enemy swapping sides at close range is a pass-through', () => {
    const t = tl({ samples: [S(0, { actors: [{ id: 'B-1', x: 1.5, z: 1.9, hp: 40 }] }), S(50, { actors: [{ id: 'B-1', x: 1.5, z: 1.1, hp: 40 }] })] });
    assert.deepEqual(ids(detectBodyCollision(t)), ['D11:S2']);
    const blocked = tl({ samples: [S(0, { actors: [{ id: 'B-1', x: 1.5, z: 2.1, hp: 40 }] }), S(50, { actors: [{ id: 'B-1', x: 1.5, z: 2.1, hp: 40 }] })] });
    assert.deepEqual(detectBodyCollision(blocked), []);
});

test('D16: a killed enemy that keeps being drawn is a ghost', () => {
    const wire = [{ kind: 'event', t: 1000, type: 'death', data: { id: 'S-1' } }];
    const ghost = tl({ wire, samples: [1300, 1800, 2400, 2900].map((t) => S(t, { actors: [{ id: 'S-1', x: 2, z: 2, hp: 5 }] })) });
    assert.deepEqual(ids(detectGhosts(ghost)), ['D16:S2']);
    const clean = tl({ wire, samples: [1300, 1800].map((t) => S(t)) });
    assert.deepEqual(detectGhosts(clean), []);
});

test('D17: enemies drawn frozen while the server moves them is S1; drawn where the server says is fine', () => {
    const wire = [], frozen = [], tracking = [];
    for (let i = 0; i <= 40; i++) {
        const t = i * 100, x = 1.5 + i * 0.05;
        wire.push({ kind: 'snap', t: t - 20, round: i, runner: [1.5, 1.5, 100], m: [['B-1', x, 4.5, 40, 0]] });
        frozen.push(S(t, { actors: [{ id: 'B-1', x: 1.5, z: 4.5, hp: 40 }] }));
        tracking.push(S(t, { actors: [{ id: 'B-1', x, z: 4.5, hp: 40 }] }));
    }
    assert.deepEqual(ids(detectRenderTruth(tl({ wire, samples: frozen }))), ['D17:S1']);
    assert.deepEqual(detectRenderTruth(tl({ wire, samples: tracking })), []);
});

test('D17: a killed actor is not "missing from the screen" while a stale snapshot still lists it', () => {
    // Live false positive: the death event arrives a few ms before the
    // snapshot that removes the actor. The client deletes the buffer on the
    // event, so for a few sampled moments the newest snapshot still has it
    // (hp > 0) but the screen correctly does not.
    const wire = [], samples = [];
    for (let i = 0; i <= 19; i++) {
        wire.push({ kind: 'snap', t: i * 100, round: i, runner: [1.5, 1.5, 100], m: [['B-1', 1.6, 1.6, 40, 0]] });
        samples.push(S(i * 100, { actors: [{ id: 'B-1', x: 1.6, z: 1.6, hp: 40 }] }));
    }
    // The death event lands at 1.91s; no further snapshot arrives for a while.
    wire.push({ kind: 'event', t: 1910, type: 'death', data: { id: 'B-1' } });
    wire.sort((a, b) => a.t - b.t);
    for (const t of [1950, 2050, 2150, 2250, 2350]) samples.push(S(t, { actors: [] }));
    assert.deepEqual(detectRenderTruth(tl({ wire, samples })), []);
});

test('D17: a model that circles and comes back is moving, not frozen', () => {
    // Live false positive (J1 seed 777, K-22): the server moved 1.02u over the
    // window while the drawn model's net displacement was 0.045u but its
    // excursion was 4.16u. Net displacement called that "frozen"; the model
    // was in fact moving the whole time.
    const wire = [], samples = [];
    for (let i = 0; i <= 30; i++) {
        const t = i * 100;
        // Server: 3.0 -> 3.55 -> 4.02 over the 1s..3s window (net 1.02u).
        const sx = i <= 10 ? 3 : i <= 20 ? 3 + (i - 10) * 0.055 : 3.55 + (i - 20) * 0.047;
        wire.push({ kind: 'snap', t: t - 20, round: i, runner: [1.5, 1.5, 100], m: [['B-1', sx, 4.5, 40, 0]] });
        // Drawn: loops out to 4.4 and back to 3.02 (net 0.02u, excursion 1.4u).
        const dx = i <= 10 ? 3 : i <= 20 ? 3 + (i - 10) * 0.14 : 4.4 - (i - 20) * 0.138;
        samples.push(S(t, { actors: [{ id: 'B-1', x: dx, z: 4.5, hp: 40 }] }));
    }
    assert.deepEqual(detectRenderTruth(tl({ wire, samples })), []);
});

test('D13/D14/D15: unseen damage, dead time, idle enemies and enemies that ignore the player are reported', () => {
    const g = new Grid(7, 7, grid().cells);
    // Player looks down +x along row 5; an enemy sits idle in view at (4.5,5.5),
    // another hits the player from behind (not in view).
    const samples = [];
    for (let t = 0; t <= 30000; t += 100) {
        samples.push(S(t, { pos: [1.5, 0.5, 5.5], yaw: Math.PI / 2, hp: t < 25000 ? 100 : 80, actors: t < 4000 ? [{ id: 'S-1', x: 4.5, z: 5.5, hp: 10 }] : [] }));
    }
    const t = tl({ samples });
    const f = ids([...detectUnseenDamage(t, g), ...detectEngagement(t, g)]);
    assert.ok(f.includes('D13:S2'), f.join());
    assert.ok(f.includes('D14:S3'), f.join());
    assert.ok(f.includes('D15:S2'), f.join());
});


// ---------- tests for the remaining detectors ----------

test('D3 detectLook: a run of real right-looks that slide the scene right is inverted; sliding left is fine', () => {
    const mk = (pair) => {
        const marks = [], frames = [], samples = [];
        for (let i = 0; i < 5; i++) {
            const t0 = 1000 + i * 1000;
            marks.push({ t: t0, kind: 'look', dx: 125 }, { t: t0 + 40, kind: 'wait', ms: 250, why: 'look step' });
            frames.push({ t: t0 - 40, file: pair[0] }, { t: t0 + 50, file: pair[1] });
            samples.push(S(t0 - 50), S(t0 + 120));
        }
        return tl({ marks, frames, samples });
    };
    const bad = mk(['look-right-before.png', 'look-right-after.png']);
    assert.deepEqual(ids(detectLook(bad, new FrameStore(bad, FIX))), ['D3:S1']);
    const good = mk(['look-right-after.png', 'look-right-before.png']);
    assert.deepEqual(detectLook(good, new FrameStore(good, FIX)), []);
});

test('D4: a frame gap while the player moves is a frozen screen; a gap while standing still is not', () => {
    const frames = [{ t: 0, file: 'a' }, { t: 900, file: 'b' }];
    const moving = tl({ frames, samples: [S(100), S(800, { pos: [2.5, 0.5, 1.5] })] });
    assert.deepEqual(ids(detectFrozenScreen(moving)), ['D4:S1']);
    const still = tl({ frames, samples: [S(100), S(800)] });
    assert.deepEqual(detectFrozenScreen(still), []);
});

test('D4: a long rAF stall is a hitch, unless it is during a reload', () => {
    assert.deepEqual(ids(detectStalls(tl({ rafGaps: [{ t: 5000, gap: 400 }] }))), ['D4:S2']);
    assert.deepEqual(detectStalls(tl({ rafGaps: [{ t: 5000, gap: 400 }], loads: [4000] })), []);
});

test('D10: page errors, console errors and lost connections are reported; a clean page is not', () => {
    const t = tl({ pageErrors: [{ t: 1, msg: 'TypeError: x is undefined' }], console: [{ t: 2, type: 'error', text: 'boom' }], sockets: [{ t: 0, kind: 'open' }, { t: 9000, kind: 'close' }] });
    assert.deepEqual(ids(detectHealth(t)).sort(), ['D10:S1', 'D10:S1', 'D10:S2'].sort());
    assert.deepEqual(detectHealth(tl({ console: [{ t: 1, type: 'log', text: 'hi' }] })), []);
});

test('D12: hits that do not move the enemy are weak; hits that push it are not', () => {
    const wire = [1000, 3000].map((t) => ({ kind: 'event', t, type: 'stunned', data: { minotaur: 'B-1', hp: 20 } }));
    const at = (t, x, st) => S(t, { actors: [{ id: 'B-1', x, z: 3, hp: 20, st }] });
    const mk = (push) => tl({ wire, samples: [at(940, 3, false), at(1450, 3 + push, true), at(2940, 3 + push, false), at(3450, 3 + 2 * push, true)] });
    assert.deepEqual(ids(detectGunImpact(mk(0))), ['D12:S3']);
    assert.deepEqual(detectGunImpact(mk(0.6)), []);
});

test('D5: an HP bar that disagrees with server HP is reported; a matching bar is not', () => {
    const samples = [0, 300, 600, 900].map((t) => S(t, { hp: 40 }));
    assert.deepEqual(ids(detectHpTruth(tl({ samples, dom: [0, 300, 600, 900].map((t) => dom(t, { hpFrac: 1 })) }))), ['D5:S2']);
    assert.deepEqual(detectHpTruth(tl({ samples, dom: [0, 300, 600, 900].map((t) => dom(t, { hpFrac: 0.4 })) })), []);
});

test('D7: HUD text cut off by the window edge or overlapping is reported; a tidy HUD is not', () => {
    const mk = (rectA) => tl({ dom: [0, 300, 600].map((t) => dom(t, { W: 900, H: 700, hud: { minimap: { rect: rectA, text: '' }, 'kill-feed': { rect: [20, 60, 200, 90], text: '' } } })) });
    assert.ok(ids(detectLayering(mk([800, 20, 164, 164]))).includes('D7:S3'), 'minimap off the right edge');
    assert.ok(ids(detectLayering(mk([100, 80, 164, 164]))).includes('D7:S3'), 'minimap over the kill feed');
    assert.deepEqual(detectLayering(mk([700, 20, 164, 164])), []);
});

test('negative cases: positive HP, a working end click, actors in corridors, and short knockback are not findings', () => {
    assert.deepEqual(detectHpTruth(tl({ samples: [S(0, { hp: 3 }), S(30, { hp: 0 })] })), []);
    assert.deepEqual(detectFlow(tl({ marks: [{ t: 5, kind: 'end-restart', restarted: true }] })), []);
    const g = new Grid(7, 7, grid().cells);
    assert.deepEqual(detectEmbeddedActors(tl({ samples: [0, 200, 400].map((x) => S(x, { actors: [{ id: 'K-1', x: 2.5, z: 2.5, hp: 5 }] })) }), g), []);
    assert.deepEqual(detectKnockback(tl({ wire: [{ kind: 'event', t: 1000, type: 'hit', data: {} }], samples: [S(900), S(1800, { spos: [1.5, 0.5, 2.3] })] })), []);
});


test('review regressions: knockback is not a stuck key; an unchanged picture is a dead look; missing frames are skipped', () => {
    // Motion only inside the 900ms after an enemy hit is knockback, not W.
    const kb = tl({
        marks: [{ t: 0, kind: 'blur', gameHasFocus: false }, { t: 10, kind: 'keyup-elsewhere', key: 'w' }, { t: 1500, kind: 'refocus' }],
        wire: [{ kind: 'event', t: 900, type: 'hit', data: {} }],
        samples: [400, 800, 1000, 1400, 2000, 2400, 3000].map((t) => S(t, { pos: [t >= 1000 ? 3.0 : 1.5, 0.5, 1.5] })),
    });
    assert.deepEqual(detectStuckKey(kb), []);
    // The same screencast frame before and after five looks = a dead look.
    const marks = [], frames = [{ t: 0, file: 'corridor.png' }], samples = [];
    for (let i = 0; i < 5; i++) { const t0 = 1000 + i * 1000; marks.push({ t: t0, kind: 'look', dx: 125 }, { t: t0 + 40, kind: 'wait', ms: 250 }); samples.push(S(t0 - 50), S(t0 + 120)); }
    const dead = tl({ marks, frames, samples });
    assert.deepEqual(ids(detectLook(dead, new FrameStore(dead, FIX))), ['D3:S1']);
    assert.match(detectLook(dead, new FrameStore(dead, FIX))[0].title, /does not move the picture/);
    // A pruned frame is not on disk: the store ignores it instead of crashing.
    const pruned = tl({ frames: [{ t: 0, file: 'spawn-wall.png' }, { t: 100, file: 'gone.png' }] });
    assert.equal(new FrameStore(pruned, FIX).at(150).file, 'spawn-wall.png');
});

// ---------- guard + probe ----------

test('GUARD: any debug-action call invalidates the run', () => {
    assert.deepEqual(ids(guard(tl({ violations: [{ t: 1, what: '__ariadne.teleport()', stack: '' }] }))), ['GUARD:S1']);
    assert.deepEqual(guard(tl()), []);
});

test('in-page probe: calling __ariadne.teleport() is recorded as a violation, reading state is not', async () => {
    const { chromium } = await import('playwright');
    const browser = await chromium.launch({ headless: true });
    try {
        const page = await browser.newPage();
        await page.addInitScript(installProbe, { rafGapMs: 100, domEveryMs: 100 });
        // Init scripts run on navigation, as they do for the real game page.
        await page.route('http://probe.test/', (route) => route.fulfill({ contentType: 'text/html', body: `<canvas id="glcanvas"></canvas><script>
            window.__ariadne = { get hp() { return 42; }, teleport() { window.__teleported = true; }, bots: { load() {} } };
            window.__wsIsTap = new WebSocket('ws://127.0.0.1:9/') instanceof WebSocket && WebSocket.OPEN === 1;
            window.__read = window.__ariadne.hp;
            window.__ariadne.teleport(1, 2);
            window.__ariadne.bots.load('x');
        </script>` }));
        await page.goto('http://probe.test/');
        const r = await page.evaluate(() => ({ v: window.__pe.drain().violations.map((x) => x.what), read: window.__read, did: window.__teleported, ws: window.__wsIsTap, raw: 'raw' in window.__pe }));
        assert.equal(r.ws, true, 'the wire tap keeps WebSocket instanceof and statics intact');
        assert.equal(r.raw, false, 'the unguarded game object is not reachable from the page');
        assert.equal(r.read, 42);
        assert.equal(r.did, true, 'the guard records but does not break the call');
        assert.deepEqual(r.v, ['__ariadne.teleport()', '__ariadne.bots.load()']);
    } finally {
        await browser.close();
    }
});

test('runDetectors and the diary run end-to-end on a synthetic journey', () => {
    const samples = [0, 100, 200].map((t) => S(t));
    const t = tl({ samples, marks: [{ t: 0, kind: 'segment', name: 'x' }, { t: 50, kind: 'keydown', key: 'w' }] });
    const f = runDetectors(t, FIX);
    assert.ok(Array.isArray(f));
    const lines = diary(t, new Grid(7, 7, grid().cells), f);
    assert.ok(lines.some((l) => l.includes('you press W')));
});
