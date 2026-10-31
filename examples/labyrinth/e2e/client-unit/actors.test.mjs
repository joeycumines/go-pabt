// Unit tests for the client-side ActorStore (client/js/actors.mjs).
// These pin the three rules that keep the drawn world honest:
//   - a repeated snapshot seq cannot reset interpolation;
//   - a death event cannot be undone by an in-flight snapshot;
//   - the roster of a new snapshot is authoritative.
// Run: node --test examples/labyrinth/e2e/client-unit/*.test.mjs

import test from 'node:test';
import assert from 'node:assert/strict';
import { ActorStore } from '../../client/js/actors.mjs';

function snap(id, x, z, extra = {}) {
    return { id, x, y: z, hp: 10, type: 0, element: 0, stunned: false, strategy: 0, ...extra };
}

test('a repeated seq is ignored even when its payload differs', () => {
    const store = new ActorStore();
    store.applySnapshot(7, [snap('B-1', 10, 0)], 0);
    store.step(0.1);
    const targetBefore = store.get('B-1').targetPos[0];
    const posBefore = store.get('B-1').pos[0];

    assert.equal(store.applySnapshot(7, [snap('B-1', -10, 0)], 0.2), false);
    assert.equal(store.get('B-1').targetPos[0], targetBefore);
    assert.equal(store.get('B-1').pos[0], posBefore);
});

test('positions converge to the newest snapshot target', () => {
    const store = new ActorStore();
    store.applySnapshot(1, [snap('S-1', 3, -4)], 0);
    for (let i = 0; i < 60; i++) store.step(1 / 60);
    const buf = store.get('S-1');
    assert.ok(Math.hypot(buf.pos[0] - 3, buf.pos[2] + 4) < 0.01, `gap ${Math.hypot(buf.pos[0] - 3, buf.pos[2] + 4)}`);
});

test('a dead id is not resurrected by a stale or repeated snapshot', () => {
    const store = new ActorStore();
    store.applySnapshot(1, [snap('B-2', 1, 1)], 0);
    store.markDead('B-2', 0.5);
    assert.equal(store.get('B-2'), undefined);

    // Same seq (a straggler from before the death) and a newer snapshot that
    // was already in flight both still list the actor.
    store.applySnapshot(1, [snap('B-2', 1, 1)], 0.6);
    assert.equal(store.get('B-2'), undefined);
    store.applySnapshot(2, [snap('B-2', 1, 1)], 0.7);
    assert.equal(store.get('B-2'), undefined);
});

test('an id missing from a new snapshot is removed', () => {
    const store = new ActorStore();
    store.applySnapshot(1, [snap('A', 1, 1), snap('B', 2, 2)], 0);
    assert.deepEqual([...store.buffers.keys()], ['A', 'B']);

    store.applySnapshot(2, [snap('A', 1, 1)], 0.1);
    assert.deepEqual([...store.buffers.keys()], ['A']);
});

test('dead ids are forgotten once a snapshot omits them', () => {
    const store = new ActorStore();
    store.applySnapshot(1, [snap('B-3', 1, 1)], 0);
    store.markDead('B-3', 0.5);
    assert.equal(store.deadIds.has('B-3'), true);

    store.applySnapshot(2, [], 0.6);
    assert.equal(store.deadIds.has('B-3'), false);
});

test('a zero-HP actor in a snapshot is treated as dead', () => {
    const store = new ActorStore();
    store.applySnapshot(1, [snap('C-1', 1, 1, { hp: 0 })], 0);
    assert.equal(store.get('C-1'), undefined);
    assert.equal(store.deadIds.has('C-1'), true);
});

test('facing and gait follow the drawn displacement', () => {
    const store = new ActorStore();
    store.applySnapshot(1, [snap('K-1', 0, 0)], 0);
    store.applySnapshot(2, [snap('K-1', 0, 2)], 0.05);
    store.step(0.5);
    const buf = store.get('K-1');
    assert.ok(buf.step > 0);
    assert.ok(Math.abs(buf.yaw) < 1e-6, `yaw ${buf.yaw}`);
});

test('step with maze resolves interpolated position out of corner walls', () => {
    // 5x5 maze with a wall at (2, 1) so interpolating straight from (1.5, 1.5)
    // to (2.5, 2.5) would cut through the wall corner at (2, 1) without resolve.
    const grid = new Array(25).fill(1);
    grid[1 * 5 + 2] = 0; // wall at cx=2, cz=1
    const maze = { w: 5, h: 5, grid };
    const store = new ActorStore();
    store.applySnapshot(1, [snap('B-1', 1.85, 1.85)], 0);
    store.applySnapshot(2, [snap('B-1', 2.15, 2.15)], 0.05);
    for (let i = 0; i < 10; i++) {
        store.step(0.016, maze);
        const p = store.get('B-1').pos;
        const cx = Math.floor(p[0]), cz = Math.floor(p[2]);
        assert.notEqual(grid[cz * 5 + cx], 0, `actor entered wall cell (${cx},${cz}) at (${p[0]},${p[2]})`);
    }
});

test('atk telegraph and justArrived flag are tracked across snapshots', () => {
    const store = new ActorStore();
    store.applySnapshot(1, [snap('B-1', 1, 1)], 0);
    assert.equal(store.get('B-1').justArrived, false, 'initial snapshot roster should not flag justArrived');
    assert.equal(store.get('B-1').atk, 0);

    store.applySnapshot(2, [snap('B-1', 1, 1, { atk: 0.5, atkType: 1 }), snap('C-2', 5, 5, { atk: 0.8, atkType: 2 })], 0.05);
    assert.equal(store.get('B-1').atk, 0.5);
    assert.equal(store.get('B-1').atkType, 1);
    assert.equal(store.get('C-2').justArrived, true, 'actor appearing in subsequent snapshot flags justArrived');
    assert.equal(store.get('C-2').atk, 0.8);
    assert.equal(store.get('C-2').atkType, 2);
});
