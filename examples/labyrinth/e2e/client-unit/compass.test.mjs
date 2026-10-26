// The threat-arrow bearing sign, pinned independently of WebGL/DOM.
// Screen-right is camera.right() and CSS rotate() is clockwise, so an
// enemy on the player's right must give +PI/2 (a left enemy -PI/2).
// Run: node --test examples/labyrinth/e2e/client-unit/*.test.mjs

import test from 'node:test';
import assert from 'node:assert/strict';
import { relativeBearing } from '../../client/js/compass.mjs';

const norm = (a) => Math.atan2(Math.sin(a), Math.cos(a));
const near = (a, b) => assert.ok(Math.abs(norm(a - b)) < 1e-9, `${a} != ${b}`);

test('an enemy dead ahead is at zero for any facing', () => {
    for (const yaw of [0, 0.7, -2.1, 3.0]) near(relativeBearing(yaw, Math.sin(yaw), Math.cos(yaw)), 0);
});

test('an enemy to the player right points right (+PI/2)', () => {
    // camera.right() is screen-right at every yaw.
    for (const yaw of [0, 0.7, -2.1]) {
        near(relativeBearing(yaw, -Math.cos(yaw), Math.sin(yaw)), Math.PI / 2);
    }
});

test('an enemy to the player left points left (-PI/2)', () => {
    for (const yaw of [0, 0.7, -2.1]) {
        near(relativeBearing(yaw, Math.cos(yaw), -Math.sin(yaw)), -Math.PI / 2);
    }
});

test('an enemy dead behind is at PI', () => {
    for (const yaw of [0, 0.4, -2.1]) {
        near(relativeBearing(yaw, -Math.sin(yaw), -Math.cos(yaw)), Math.PI);
    }
});
