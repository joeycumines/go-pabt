// journeys.mjs - scripted player sessions. Each journey is what a person
// would actually do, through real input only; the timeline it produces is
// what the detectors judge. Journeys record what they attempted with
// session.mark(); they never assert - judging is the detectors' job.

import { Actuator } from './actuate.mjs';
import { readPlayer, readGrid, faceYaw, walkTo, visibleActors, engage, yawTo, RAD_PER_PX } from './pilot.mjs';

// enterGame goes through the real onboarding: read the tutorial, press Enter,
// click into the game to take pointer lock. Returns whether lock was taken.
export async function enterGame(s, act, { readMs = 6000 } = {}) {
    await act.wait(readMs, 'reading the tutorial');
    const tut = await s.page.evaluate(() => { const e = document.getElementById('tutorial-overlay'); return !!e && !e.hidden; });
    if (tut) await act.tap('Enter');
    await act.wait(600, 'after tutorial');
    return lockPointer(s, act);
}

export async function lockPointer(s, act) {
    for (let i = 0; i < 3; i++) {
        await s.page.bringToFront();
        await act.clickAt(s.timeline.viewport.width / 2, s.timeline.viewport.height / 2);
        await act.wait(400);
        if (await s.page.evaluate(() => !!document.pointerLockElement)) { s.mark('locked', { attempt: i }); return true; }
    }
    s.mark('lock-failed');
    return false;
}

async function waitGrid(s) {
    for (let i = 0; i < 100; i++) {
        const g = await readGrid(s.page);
        if (g) return g;
        await new Promise((r) => setTimeout(r, 100));
    }
    throw new Error('maze grid never arrived');
}

// controlsCheck is the "first ten seconds" a new player spends learning the
// controls: look around, then tap each movement key in the most open
// direction. The D2/D3 detectors judge these segments.
async function controlsCheck(s, act, grid) {
    // Look all the way round in small steps (~98px each), pausing between
    // them: each step is a separate gesture whose on-screen shift (~75px at
    // 640px wide) fits the D3 pixel-shift window.
    s.mark('segment', { name: 'look-around' });
    for (let i = 0; i < 32; i++) {
        await act.look((Math.PI / 16) / RAD_PER_PX, 0, { steps: 4 });
        await act.wait(250, 'look step');
    }
    const p = await readPlayer(s.page);
    let best = { yaw: p.yaw, dist: 0 };
    for (let k = 0; k < 4; k++) {
        const yaw = k * Math.PI / 2;
        const r = grid.castRay(p.pos[0], p.pos[2], Math.sin(yaw), Math.cos(yaw));
        if (r.dist > best.dist) best = { yaw, dist: r.dist };
    }
    await faceYaw(act, s.page, best.yaw);
    await act.wait(300);
    s.mark('segment', { name: 'controls', openDist: best.dist });
    // 900ms between probes lets momentum (friction 0.82/frame) die out so
    // one probe's slide cannot contaminate the next one's displacement.
    for (const [key, ms] of [['w', 350], ['s', 350], ['d', 250], ['a', 250]]) {
        s.mark('probe', { key });
        await act.hold(key, ms);
        await act.wait(900);
    }
    s.mark('probe', { key: 'look-right' });
    await act.look(0.25 / RAD_PER_PX, 0, { steps: 6 });
    await act.wait(600);
    s.mark('segment', { name: 'end-controls' });
}


// clickEndOverlay does what a player does on the end screen: click it. If the
// click does not restart the game (e.g. the pointer is still locked, so the
// click lands on the canvas), it records that and falls back to Escape +
// click, as a player eventually would.
async function clickEndOverlay(s, act) {
    const ov = await s.page.evaluate(() => { const e = document.getElementById('overlay'); if (!e || e.hidden) return null; const b = e.getBoundingClientRect(); return [b.x + b.width / 2, b.y + b.height / 2]; });
    if (!ov) { s.mark('no-end-overlay'); return false; }
    const loadsBefore = s.timeline.loads.length;
    s.mark('end-click', { how: 'plain-click' });
    await act.click();
    await act.wait(2000, 'did the click restart?');
    if (s.timeline.loads.length === loadsBefore) {
        s.mark('end-click-ignored', { locked: await s.page.evaluate(() => !!document.pointerLockElement) });
        await act.tap('Escape');
        await act.wait(300);
        await act.clickAt(ov[0], ov[1]);
        await act.wait(2000);
    }
    const restarted = s.timeline.loads.length > loadsBefore;
    s.mark('end-restart', { restarted });
    return restarted;
}

export const JOURNEYS = {
    // J1: cold start - first impression, onboarding, controls.
    async J1(s) {
        const act = new Actuator(s);
        s.mark('segment', { name: 'onboarding' });
        const locked = await enterGame(s, act, { readMs: 8000 });
        const grid = await waitGrid(s);
        s.mark('segment', { name: 'first-view' });
        await act.wait(2000, 'taking in the first view');
        if (locked) await controlsCheck(s, act, grid);
        s.mark('segment', { name: 'leave-spawn' });
        const r = await walkTo(act, s.page, grid, grid.gate[0], grid.gate[1], { timeoutMs: 15000 });
        s.mark('result', r);
    },

    // J2: escape - navigate to the gate, see the win screen, play again.
    async J2(s) {
        const act = new Actuator(s);
        await enterGame(s, act, { readMs: 1500 });
        const grid = await waitGrid(s);
        s.mark('segment', { name: 'run-to-gate' });
        const r = await walkTo(act, s.page, grid, grid.gate[0], grid.gate[1], { timeoutMs: 180000, arrive: 0.2, sprint: true });
        s.mark('result', r);
        await act.wait(2500, 'reading the end screen');
        s.mark('segment', { name: 'play-again' });
        if (await clickEndOverlay(s, act)) {
            await act.wait(1000, 'second run loading');
            await enterGame(s, act, { readMs: 1500 });
            s.mark('segment', { name: 'second-run' });
            await act.hold('w', 1200);
            await act.wait(2000);
        }
    },

    // J3: fight - find enemies, aim with the mouse, click to fire.
    async J3(s) {
        const act = new Actuator(s);
        await enterGame(s, act, { readMs: 1500 });
        const grid = await waitGrid(s);
        s.mark('segment', { name: 'hunt' });
        const t0 = Date.now();
        const fired = [];
        while (Date.now() - t0 < 70000) {
            const p = await readPlayer(s.page);
            if (!p || p.gs !== 'playing') break;
            const vis = visibleActors(grid, p);
            if (vis.length) {
                s.mark('engage', { id: vis[0].id, d: vis[0].d });
                fired.push(...await engage(act, s.page, grid, vis[0].id, { shots: 4 }));
                continue;
            }
            const live = p.actors.filter((a) => a.hp > 0);
            if (!live.length) { await act.wait(500); continue; }
            const near = live.map((a) => ({ a, n: (grid.path(p.pos[0], p.pos[2], a.x, a.z) || { length: 1e9 }).length })).sort((x, y) => x.n - y.n)[0].a;
            await walkTo(act, s.page, grid, near.x, near.z, {
                timeoutMs: 6000, arrive: 2.5,
                stopWhen: (q) => visibleActors(grid, q, 8).length > 0,
            });
        }
        s.mark('result', { fired: fired.length, shots: fired });
    },

    // J4: death - stand in harm's way without fighting back, read the death
    // screen, restart.
    async J4(s) {
        const act = new Actuator(s);
        await enterGame(s, act, { readMs: 1500 });
        const grid = await waitGrid(s);
        s.mark('segment', { name: 'seek-danger' });
        const t0 = Date.now();
        while (Date.now() - t0 < 120000) {
            const p = await readPlayer(s.page);
            if (!p || p.gs !== 'playing') break;
            const live = p.actors.filter((a) => a.hp > 0);
            if (!live.length) { await act.wait(500); continue; }
            const near = live.sort((x, y) => Math.hypot(x.x - p.pos[0], x.z - p.pos[2]) - Math.hypot(y.x - p.pos[0], y.z - p.pos[2]))[0];
            await walkTo(act, s.page, grid, near.x, near.z, { timeoutMs: 4000, arrive: 0.8 });
            await act.wait(800, 'standing in harm\'s way');
        }
        await act.wait(2500, 'reading the death screen');
        s.mark('segment', { name: 'restart' });
        if (await clickEndOverlay(s, act)) {
            await act.wait(1000, 'restart loading');
            await enterGame(s, act, { readMs: 1500 });
            s.mark('segment', { name: 'after-restart' });
            await act.hold('w', 1000);
            await act.wait(2000);
        }
    },

    // J5: robustness - alt-tab while running, resize, Escape and re-lock,
    // reload mid-game.
    async J5(s) {
        const act = new Actuator(s);
        await enterGame(s, act, { readMs: 1500 });
        const grid = await waitGrid(s);
        const p = await readPlayer(s.page);
        let best = { yaw: p.yaw, dist: 0 };
        for (let k = 0; k < 4; k++) {
            const yaw = k * Math.PI / 2;
            const r = grid.castRay(p.pos[0], p.pos[2], Math.sin(yaw), Math.cos(yaw));
            if (r.dist > best.dist) best = { yaw, dist: r.dist };
        }
        await faceYaw(act, s.page, best.yaw);

        // Playwright emulates focus for every page by default, which would
        // keep this page "focused" behind the other tab; switch that off so
        // the game gets the real blur a player's alt-tab produces.
        s.mark('segment', { name: 'alt-tab-while-moving' });
        await s.cdp.send('Emulation.setFocusEmulationEnabled', { enabled: false });
        // Open the other "window" first: creating a page takes long enough
        // that the player would otherwise reach the wall before letting go.
        const other = await s.context.newPage();
        await s.page.bringToFront();
        await act.down('w');
        await act.wait(250);
        await other.bringToFront();
        s.mark('blur', { gameHasFocus: await s.page.evaluate(() => document.hasFocus()) });
        // The player lets go of W while the other tab has focus, so the keyup
        // goes to THAT page - the game never hears it.
        act.held.delete('w');
        s.mark('keyup-elsewhere', { key: 'w' });
        await other.keyboard.up('w');
        await act.wait(1500, 'other window focused');
        await other.close();
        await s.page.bringToFront();
        s.mark('refocus', { gameHasFocus: await s.page.evaluate(() => document.hasFocus()) });
        await act.wait(800, 'watching for a stuck key');
        // Re-enter and turn round with the mouse: if W is stuck, the player now
        // visibly walks back down the corridor instead of pushing on a wall.
        await s.cdp.send('Emulation.setFocusEmulationEnabled', { enabled: true });
        await lockPointer(s, act);
        s.mark('turn-round');
        await act.look(Math.PI / RAD_PER_PX, 0);
        await act.wait(1500, 'watching for a stuck key after turning');

        s.mark('segment', { name: 'resize' });
        await s.page.setViewportSize({ width: 900, height: 700 });
        s.mark('resize', { w: 900, h: 700 });
        await act.wait(1500);
        await s.page.setViewportSize({ width: 1280, height: 800 });
        s.mark('resize', { w: 1280, h: 800 });
        await act.wait(1500);

        s.mark('segment', { name: 'escape-relock' });
        await act.tap('Escape');
        await act.wait(1200, 'unlocked');
        await lockPointer(s, act);
        await act.look(0.5 / RAD_PER_PX, 0);
        await act.wait(800);

        s.mark('segment', { name: 'reload-mid-game' });
        await s.page.reload();
        s.mark('reloaded');
        await act.wait(2500);
        await enterGame(s, act, { readMs: 1000 });
        await act.hold('w', 800);
        await act.wait(1500);
    },
};

export { yawTo };
