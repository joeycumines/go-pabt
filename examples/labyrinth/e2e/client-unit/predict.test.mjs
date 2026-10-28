// predict.test.mjs - verifies that client/js/predict.mjs matches the Go server's
// netcode.Move and maze.Resolve implementations step-for-step across the golden file.

import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { move, resolve } from '../../client/js/predict.mjs';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const goldenPath = path.resolve(__dirname, '../../netcode/testdata/move_golden.json');

test('predict.mjs reproduces netcode/testdata/move_golden.json to 1e-9', () => {
    assert.ok(fs.existsSync(goldenPath), `golden file exists at ${goldenPath}`);
    const raw = fs.readFileSync(goldenPath, 'utf8');
    const golden = JSON.parse(raw);

    const maze = {
        w: golden.w,
        h: golden.h,
        grid: golden.grid,
    };

    let pos = { x: golden.start.X, y: golden.start.Y };

    for (const step of golden.steps) {
        // Assert input position matches expected input position
        assert.ok(
            Math.abs(pos.x - step.inPos.X) < 1e-9 && Math.abs(pos.y - step.inPos.Y) < 1e-9,
            `step ${step.step} inPos mismatch: got (${pos.x}, ${pos.y}), want (${step.inPos.X}, ${step.inPos.Y})`
        );

        const out = move(maze, pos, step.yaw, step.forward, step.strafe, step.sprint, step.dt);

        const dx = Math.abs(out.x - step.outPos.X);
        const dy = Math.abs(out.y - step.outPos.Y);
        assert.ok(
            dx < 1e-9 && dy < 1e-9,
            `step ${step.step} outPos mismatch: got (${out.x}, ${out.y}), want (${step.outPos.X}, ${step.outPos.Y}), diff=(${dx}, ${dy})`
        );

        pos = out;
    }
});
