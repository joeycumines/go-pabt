// scenarios.js - the committed E2E scenario list. Each scenario asserts on
// the __ariadne debug hook, console messages, and genuine screenshots. Every
// goalState gameplay clause is covered by at least one scenario.

const ARIADNE_READY = () => window.__ariadne && window.__ariadne.wsOpen && window.__ariadne.round > 0;

async function gotoGame(ctx) {
    await ctx.page.goto(ctx.url, { waitUntil: 'domcontentloaded', timeout: 20000 });
    await ctx.page.waitForFunction(ARIADNE_READY, { timeout: 20000 });
    // Let the server fan out snapshots and tree frames.
    await ctx.page.waitForTimeout(2000);
}

async function snap(ctx, file) {
    const full = `${ctx.artifactsDir}/${file}`;
    await ctx.page.screenshot({ path: full });
    return ctx.assertScreenshot(full);
}

// Relocate the player to the safest corner (max min-distance to actors, away
// from the gate) so survival-critical scenarios are not cut short by the
// lose condition. Returns nothing; used before measurements that need the
// world to stay in the running phase.
async function relocateSafe(ctx) {
    await ctx.page.evaluate(() => {
        const a = window.__ariadne;
        const [w, h] = a.mazeSize; const gate = a.gate;
        const cands = [[1.5, 1.5], [1.5, h - 1.5], [w - 1.5, 1.5], [w - 1.5, h - 1.5]];
        let best = null, bestScore = -1;
        for (const [x, z] of cands) {
            if (gate && Math.hypot(x - gate[0], z - gate[1]) < 3) continue;
            let minD = Infinity;
            for (const act of a.actors) minD = Math.min(minD, Math.hypot(x - act.pos[0], z - act.pos[2]));
            if (minD > bestScore) { bestScore = minD; best = [x, z]; }
        }
        if (best) a.teleport(best[0], best[1]);
    });
    await ctx.page.waitForTimeout(400);
}

export default [
    // load+ws+maze+models+HUD
    {
        name: 'load+ws+maze+models+HUD',
        async run(ctx) {
            await gotoGame(ctx);
            const s = await ctx.page.evaluate(() => {
                const a = window.__ariadne;
                return {
                    wsOpen: a.wsOpen,
                    mazeSize: a.mazeSize,
                    gate: a.gate,
                    modelsLoaded: a.modelsLoaded,
                    hp: a.hp,
                    gameState: a.gameState,
                    hudHidden: document.getElementById('hud').hidden,
                    loadingVisible: getComputedStyle(document.getElementById('loading')).display !== 'none',
                };
            });
            if (!s.wsOpen) throw new Error('wsOpen false');
            if (!s.mazeSize || s.mazeSize[0] !== 21 || s.mazeSize[1] !== 21) throw new Error(`bad mazeSize ${JSON.stringify(s.mazeSize)}`);
            if (!s.gate) throw new Error('no gate');
            if (!s.modelsLoaded) throw new Error('modelsLoaded false');
            if (s.hp !== 100) throw new Error(`hp ${s.hp} != 100`);
            if (s.gameState !== 'playing') throw new Error(`gameState ${s.gameState}`);
            if (s.hudHidden) throw new Error('hud hidden');
            if (s.loadingVisible) throw new Error('loading still visible');
            await snap(ctx, 'e2e-load.png');
        },
    },

    // movement+collision: walk in up to 4 yaw orientations from spawn; at
    // least one cardinal direction from a floor cell is open, so cumulative
    // displacement must exceed a floor. Position must stay in bounds.
    {
        name: 'movement+collision',
        async run(ctx) {
            await gotoGame(ctx);
            // Measure from spawn: at least one cardinal neighbor of a floor
            // cell is open, so walking toward it must displace the player.
            const baseline = await ctx.page.evaluate(() => window.__ariadne.playerPos.slice());
            let maxDisp = 0;
            for (const yaw of [0, Math.PI / 2, Math.PI, 3 * Math.PI / 2]) {
                await ctx.page.evaluate((y) => window.__ariadne.setYaw(y), yaw);
                await ctx.page.evaluate(() => window.dispatchEvent(new KeyboardEvent('keydown', { code: 'KeyW', bubbles: true })));
                await ctx.page.waitForTimeout(900);
                await ctx.page.evaluate(() => window.dispatchEvent(new KeyboardEvent('keyup', { code: 'KeyW', bubbles: true })));
                await ctx.page.waitForTimeout(200);
                const p = await ctx.page.evaluate(() => window.__ariadne.playerPos.slice());
                const d = Math.hypot(p[0] - baseline[0], p[2] - baseline[2]);
                if (d > maxDisp) maxDisp = d;
                if (maxDisp > 0.6) break;
            }
            if (maxDisp < 0.3) throw new Error(`no meaningful movement: maxDisp ${maxDisp.toFixed(2)}`);
            const inBounds = await ctx.page.evaluate(() => {
                const a = window.__ariadne;
                const [w, h] = a.mazeSize;
                const p = a.playerPos;
                return p[0] >= 0.2 && p[0] <= w - 0.2 && p[2] >= 0.2 && p[2] <= h - 0.2;
            });
            if (!inBounds) throw new Error('player left maze bounds');
        },
    },

    // fire->stun+advantage: stand ~4.5 units from the nearest actor, face it,
    // fire up to 3 pulses; a hit must stun the actor or reduce its HP.
    {
        name: 'fire->stun+advantage',
        async run(ctx) {
            await gotoGame(ctx);
            await relocateSafe(ctx);
            const before = await ctx.page.evaluate(() => {
                const a = window.__ariadne;
                const me = a.playerPos;
                let best = null, bd = Infinity;
                for (const act of a.actors) {
                    const d = Math.hypot(act.pos[0] - me[0], act.pos[2] - me[2]);
                    if (d < bd) { bd = d; best = act; }
                }
                // Stand 4.5 units from the target along the line to it.
                const dx = best.pos[0] - me[0], dz = best.pos[2] - me[2];
                const d = Math.hypot(dx, dz) || 1;
                a.teleport(best.pos[0] - dx / d * 4.5, best.pos[2] - dz / d * 4.5);
                a.setYaw(Math.atan2(dx, dz));
                return { id: best.id, hp: best.hp };
            });
            await ctx.page.waitForTimeout(500);
            let effect = false;
            for (let shot = 0; shot < 3 && !effect; shot++) {
                await ctx.page.evaluate(() => window.__ariadne.fire());
                await ctx.page.waitForTimeout(800);
                effect = await ctx.page.evaluate(({ id, hpBefore }) => {
                    const a = window.__ariadne;
                    const act = a.actors.find(x => x.id === id);
                    if (!act) return true; // actor died or was removed = effect
                    return act.stunned === true || act.hp < hpBefore;
                }, { id: before.id, hpBefore: before.hp });
            }
            const lastShot = await ctx.page.evaluate(() => window.__ariadne.lastShotElement);
            if (lastShot < 0) throw new Error('lastShotElement not set after fire');
            if (!effect) throw new Error('fire produced no stun/hp effect after 3 shots');
        },
    },

    // gate->win overlay
    {
        name: 'gate->win overlay',
        async run(ctx) {
            await gotoGame(ctx);
            await ctx.page.evaluate(() => {
                const a = window.__ariadne;
                a.teleport(a.gate[0], a.gate[1]);
            });
            await ctx.page.waitForFunction(() => window.__ariadne.gameState === 'won', { timeout: 5000 });
            const ov = await ctx.page.evaluate(() => {
                const el = document.getElementById('overlay');
                return { hidden: el.hidden, text: el.textContent.trim() };
            });
            if (ov.hidden) throw new Error('win overlay hidden');
            if (!/ESCAPED/.test(ov.text)) throw new Error(`overlay text ${ov.text}`);
            await snap(ctx, 'e2e-win.png');
        },
    },

    // actor contact->caught/HP loss
    {
        name: 'actor contact->caught/HP loss',
        async run(ctx) {
            await gotoGame(ctx);
            await ctx.page.evaluate(() => {
                const a = window.__ariadne;
                const act = a.actors[0];
                a.teleport(act.pos[0], act.pos[2]);
            });
            await ctx.page.waitForFunction(() => {
                const a = window.__ariadne;
                return a.hp < 100 || a.gameState === 'caught';
            }, { timeout: 8000 });
            const s = await ctx.page.evaluate(() => ({ hp: window.__ariadne.hp, gs: window.__ariadne.gameState }));
            if (s.hp === 100 && s.gs !== 'caught') throw new Error(`no damage: hp ${s.hp}`);
        },
    },

    // rejoin token identity + position
    {
        name: 'rejoin token identity + position',
        async run(ctx) {
            await gotoGame(ctx);
            const spot = await ctx.page.evaluate(() => {
                const a = window.__ariadne;
                // Teleport to a safe corner far from actors and the gate so
                // the world cannot be driven terminal (HP->0 -> PhaseLost)
                // between the teleport and the reload - a terminal phase
                // legitimately triggers a world reset on rejoin, which would
                // confound this test.
                const [w, h] = a.mazeSize;
                // Exclude the maze-start cell itself so the assertion
                // "converged away from start" cannot be trivially satisfied
                // or trivially defeated by the spawn point.
                const cands = [[1.5, h - 1.5], [w - 1.5, 1.5], [w - 1.5, h - 1.5]];
                let best = null, bestScore = -1;
                const gate = a.gate;
                for (const [x, z] of cands) {
                    if (gate && Math.hypot(x - gate[0], z - gate[1]) < 3) continue;
                    let minD = Infinity;
                    for (const act of a.actors) minD = Math.min(minD, Math.hypot(x - act.pos[0], z - act.pos[2]));
                    if (minD > bestScore) { bestScore = minD; best = [x, z]; }
                }
                const [tx, tz] = best || [w - 1.5, h - 1.5];
                a.teleport(tx, tz);
                return { token: sessionStorage.getItem('ariadne-token'), tx, tz };
            });
            await ctx.page.waitForTimeout(600);
            // waitUntil: 'load' can hang after WebGL teardown on the old
            // document; domcontentloaded is enough since ARIADNE_READY gates
            // on the fresh WS connect.
            const tReload = Date.now();
            // Capture the phase and position at the instant of reload; if the
            // world went terminal (PhaseWon/PhaseLost) in the ~600ms after the
            // teleport, the server's reconnect path legitimately resets the
            // world and the position assertion below is testing a reset, not a
            // preserved rejoin. Record it so a failure is diagnosable.
            const pre = await ctx.page.evaluate(() => {
                const a = window.__ariadne;
                return { gs: a.gameState, pos: a.playerPos, round: a.round };
            });
            await ctx.page.reload({ waitUntil: 'domcontentloaded', timeout: 20000 });
            const tReloadDone = Date.now();
            let readyOK = true;
            try {
                await ctx.page.waitForFunction(ARIADNE_READY, { timeout: 15000 });
            } catch (e) {
                readyOK = false;
            }
            const tReady = Date.now();
            if (!readyOK) {
                const diag = await ctx.page.evaluate(() => {
                    const a = window.__ariadne;
                    return {
                        token: sessionStorage.getItem('ariadne-token'),
                        wsOpen: a && a.wsOpen,
                        round: a && a.round,
                        hasAriadne: !!a,
                    };
                });
                throw new Error(`reload=${tReloadDone - tReload}ms ready=${tReady - tReloadDone}ms DIAG=${JSON.stringify(diag)}`);
            }
            const after = await ctx.page.evaluate(() => {
                const a = window.__ariadne;
                return { token: sessionStorage.getItem('ariadne-token'), pos: a.playerPos, gs: a.gameState };
            });
            if (after.token !== spot.token) throw new Error(`token changed: ${spot.token} -> ${after.token}`);
            // Phase-aware rejoin assertion. The server legitimately resets the
            // world on reconnect when it is terminal (PhaseWon/PhaseLost), and
            // that reset clears the player position to the maze start. So the
            // position-preservation invariant only applies when the world was
            // still live at the moment of reload; a terminal pre-reload phase
            // (e.g. an actor killed the player during the post-teleport wait)
            // makes the reset the correct behavior, not a rejoin bug. Token
            // identity is asserted unconditionally above - that holds in both
            // cases.
            if (pre.gs !== 'playing') {
                // World was terminal when we reloaded; the server restarted it
                // and the position reset is expected. Assert the fresh game is
                // actually running (not stuck in the old terminal state).
                if (after.gs !== 'playing') throw new Error(`world still terminal after reset-triggering rejoin: ${after.gs}`);
                return;
            }
            // The camera spawns at maze start (1.5,1.5) on every page load
            // and lerps 10%/frame toward the server position. A world that
            // was wrongly reset would sit at maze start; a world correctly
            // rejoined converges to wherever the player actually was when
            // the reload happened - which is (spot.tx, spot.tz) plus any
            // drift from post-teleport movement/actor contact before the
            // reload landed. Assert it converged AWAY from maze start, i.e.
            // the rejoin preserved state rather than resetting to a spawn.
            try {
                await ctx.page.waitForFunction(() => {
                    const p = window.__ariadne.playerPos;
                    return Math.hypot(p[0] - 1.5, p[2] - 1.5) > 1.0;
                }, { timeout: 6000 });
            } catch (e) {
                const final = await ctx.page.evaluate(() => window.__ariadne.playerPos);
                throw new Error(`pos did not converge away from start: ${JSON.stringify(final)} pre=${JSON.stringify(pre)} (teleported to ${spot.tx},${spot.tz})`);
            }
            const final = await ctx.page.evaluate(() => window.__ariadne.playerPos);
            const dStart = Math.hypot(final[0] - 1.5, final[2] - 1.5);
            if (dStart < 1.0) throw new Error(`position reset to maze start (dist ${dStart.toFixed(2)}) - rejoin did not preserve state: ${JSON.stringify(final)} pre=${JSON.stringify(pre)}`);
        },
    },

    // Tab debug panel + commands
    {
        name: 'Tab debug panel + commands',
        async run(ctx) {
            await gotoGame(ctx);
            await ctx.page.evaluate(() => document.dispatchEvent(new KeyboardEvent('keydown', { code: 'Tab', bubbles: true })));
            // The actor list is populated from server tree frames, which are
            // broadcast on a ~3Hz cadence. Poll for the panel to open AND for
            // at least one actor to render, rather than asserting after a
            // fixed sleep (the first tree frame may not have arrived yet).
            const panel = await ctx.page.evaluate(() => ({
                hidden: document.getElementById('debug-panel').hidden,
            }));
            if (panel.hidden) throw new Error('debug panel hidden after Tab');
            try {
                // Tree frames are broadcast on a ~3Hz cadence; allow a couple
                // of missed cycles before treating the list as dead. The outer
                // timeout must stay well below Playwright's 30s default so a
                // failure surfaces as our diagnostic, not an opaque generic
                // timeout from the runner.
                await ctx.page.waitForFunction(
                    () => document.getElementById('debug-actor-list').children.length > 0,
                    { timeout: 20000 },
                );
            } catch (e) {
                throw new Error('no actors in debug panel (tree frames never populated it)');
            }
            await ctx.page.evaluate(() => document.dispatchEvent(new KeyboardEvent('keydown', { code: 'Tab', bubbles: true })));
        },
    },

    // G3 BT HUD live rows + KeyB
    {
        name: 'G3 BT HUD live rows + KeyB',
        async run(ctx) {
            await gotoGame(ctx);
            await relocateSafe(ctx);
            await ctx.page.evaluate(() => document.dispatchEvent(new KeyboardEvent('keydown', { code: 'KeyB', bubbles: true })));
            await ctx.page.waitForTimeout(1500);
            const snapTicks = () => ctx.page.evaluate(() => {
                const h = window.__ariadne.btHud;
                const m = {};
                for (const r of h.rows) m[r.id] = r.ticks;
                return m;
            });
            const s1 = await snapTicks();
            await ctx.page.waitForTimeout(3000);
            const s2 = await snapTicks();
            const common = Object.keys(s1).filter(id => s2[id] !== undefined);
            if (common.length === 0) throw new Error('no common actors across samples');
            let climbed = 0, unchanged = 0;
            for (const id of common) {
                if (s2[id] > s1[id]) climbed++; else unchanged++;
            }
            if (climbed === 0) throw new Error('no actor tick counts climbed over 3s');
            if (unchanged > 0) throw new Error(`${unchanged} actors' ticks frozen over 3s`);
            // Probe<->DOM match.
            const match = await ctx.page.evaluate(() => {
                const h = window.__ariadne.btHud;
                const dom = document.getElementById('bt-hud-rows');
                return { probe: h.rows.length, dom: dom.children.length, expected: Math.min(h.rows.length, 12) };
            });
            if (match.dom !== match.expected) throw new Error(`probe/DOM mismatch: ${JSON.stringify(match)}`);
            // KeyB toggles off.
            await ctx.page.evaluate(() => document.dispatchEvent(new KeyboardEvent('keydown', { code: 'KeyB', bubbles: true })));
            const off = await ctx.page.evaluate(() => document.getElementById('bt-hud').hidden);
            if (!off) throw new Error('KeyB did not hide btHud');
            await snap(ctx, 'e2e-bthud.png');
        },
    },

    // multi-session distinct tokens
    {
        name: 'multi-session distinct tokens',
        async run(ctx) {
            await gotoGame(ctx);
            const tok1 = await ctx.page.evaluate(() => sessionStorage.getItem('ariadne-token'));
            const page2 = await ctx.context.newPage();
            await page2.goto(ctx.url, { waitUntil: 'domcontentloaded' });
            await page2.waitForFunction(ARIADNE_READY, { timeout: 20000 });
            const tok2 = await page2.evaluate(() => sessionStorage.getItem('ariadne-token'));
            await page2.close();
            if (!tok1 || !tok2) throw new Error('missing token');
            if (tok1 === tok2) throw new Error('tokens not distinct');
        },
    },
];
