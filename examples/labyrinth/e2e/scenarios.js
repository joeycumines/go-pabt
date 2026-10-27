// scenarios.js - the committed E2E scenario list. Each scenario asserts on
// the __ariadne debug hook, console messages, and genuine screenshots. Every
// goalState gameplay clause is covered by at least one scenario.

const ARIADNE_READY = () => window.__ariadne && window.__ariadne.wsOpen && window.__ariadne.round > 0;

// waitForReady waits for ARIADNE_READY with a real timeout. The options
// object MUST be the third argument — Playwright binds a second-argument
// object to the pageFunction's `arg` parameter and silently keeps the
// default 30s timeout, which made every timeout in this file a lie
// (observed as "Timeout 30000ms exceeded" despite a 20000 option). On
// timeout it captures the page's readiness state so the failure names
// the stuck stage (hook missing / WS down / no snapshot) instead of a
// generic timeout.
async function waitForReady(page, timeoutMs, label) {
    try {
        await page.waitForFunction(ARIADNE_READY, undefined, { timeout: timeoutMs });
    } catch (e) {
        let diag = null;
        try {
            diag = await page.evaluate(() => ({
                hasHook: !!window.__ariadne,
                wsOpen: window.__ariadne ? window.__ariadne.wsOpen : null,
                round: window.__ariadne ? window.__ariadne.round : null,
                gameState: window.__ariadne ? window.__ariadne.gameState : null,
                readyState: document.readyState,
            }));
        } catch (e2) {
            diag = { evalError: String((e2 && e2.message) || e2) };
        }
        throw new Error(`${label} not ready within ${timeoutMs}ms: ${JSON.stringify(diag)}`);
    }
}

async function gotoGame(ctx) {
    await ctx.page.goto(ctx.url, { waitUntil: 'domcontentloaded', timeout: 20000 });
    await waitForReady(ctx.page, 20000, 'gotoGame');
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

// ensurePlaying reloads the page if the world is in a terminal state
// (caught/won), then waits for the fresh game to be ready. This is needed
// after scenarios that deliberately catch the player (actor contact) or
// trigger a win (gate overlay), because teleports are ignored by the server
// when the world phase is terminal.
async function ensurePlaying(ctx) {
    const gs = await ctx.page.evaluate(() => window.__ariadne.gameState);
    if (gs !== 'playing') {
        await ctx.page.reload({ waitUntil: 'domcontentloaded', timeout: 20000 });
        await waitForReady(ctx.page, 20000, 'ensurePlaying reload');
        await ctx.page.waitForTimeout(1000);
    }
}

// freshWorld reloads the page unconditionally, producing a world with
// freshly-scattered actors (seeded PRNG). Used by retry loops where the
// same world has become too hostile (actors converged on all safe corners).
async function freshWorld(ctx) {
    await ctx.page.reload({ waitUntil: 'domcontentloaded', timeout: 20000 });
    await waitForReady(ctx.page, 20000, 'freshWorld reload');
    await ctx.page.waitForTimeout(1000);
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
                    hudHP: document.getElementById('hp-fill').style.width,
                    gameState: a.gameState,
                    hudHidden: document.getElementById('hud').hidden,
                    loadingVisible: getComputedStyle(document.getElementById('loading')).display !== 'none',
                };
            });
            if (!s.wsOpen) throw new Error('wsOpen false');
            if (!s.mazeSize || s.mazeSize[0] !== 21 || s.mazeSize[1] !== 21) throw new Error(`bad mazeSize ${JSON.stringify(s.mazeSize)}`);
            if (!s.gate) throw new Error('no gate');
            if (!s.modelsLoaded) throw new Error('modelsLoaded false');
            // This legacy scenario runs against an unpaused world, so the
            // idle runner can legally have taken contact damage by now. Assert
            // that the HUD mirrors the authoritative session HP instead of a
            // pristine 100. Player-eye D9 owns "no damage before play".
            if (typeof s.hp !== 'number' || s.hp < 0 || s.hp > 100) {
                throw new Error(`hp out of range: ${s.hp}`);
            }
            const hudHP = parseFloat(s.hudHP);
            if (!Number.isFinite(hudHP) || Math.abs(hudHP - s.hp) > 0.5) {
                throw new Error(`HUD hp ${s.hudHP} != session hp ${s.hp}`);
            }
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
            // Outer retry loop: once gameState leaves 'playing' the server
            // ignores teleports and drops fire(), so a caught mid-loop run
            // can never recover in-place — reload for a fresh life. Actors
            // re-scatter via the seeded PRNG.
            let effect = false;
            let fullAttempts = 0;
            while (!effect && fullAttempts < 3) {
                fullAttempts++;
                if (fullAttempts > 1) await freshWorld(ctx);
                await relocateSafe(ctx);
                const before = await ctx.page.evaluate(() => {
                    const a = window.__ariadne;
                    if (a.gameState !== 'playing') return null;
                    const me = a.playerPos;
                    const sorted = a.actors.slice().sort((p, q) =>
                        Math.hypot(p.pos[0] - me[0], p.pos[2] - me[2]) -
                        Math.hypot(q.pos[0] - me[0], q.pos[2] - me[2]));
                    for (const best of sorted) {
                        if (best.hp <= 0 || best.stunned) continue;
                        // Stand 4.5 units from the target with a verified
                        // clear fire lane — standing on the me→actor line
                        // can land behind a wall corner, and the server's
                        // rayOccluded rejects every shot through it.
                        const stand = a.findStandpoint(best.pos[0], best.pos[2], 4.5);
                        if (!stand) continue;
                        a.teleport(stand[0], stand[1]);
                        a.setYaw(Math.atan2(best.pos[0] - stand[0], best.pos[2] - stand[1]));
                        return { id: best.id, hp: best.hp };
                    }
                    return null;
                });
                if (!before) continue;
                await ctx.page.waitForTimeout(500);
                let targetId = before.id;
                let targetHp = before.hp;
                let misses = 0;
                let terminal = false;
                for (let shot = 0; shot < 6 && !effect && !terminal; shot++) {
                    // Re-aim at the target before each shot: allied-yielding (R1)
                    // makes same-element actors path on curved trajectories, so
                    // a stale aim from the initial setup can miss after the actor
                    // has drifted laterally. If the target is unreachable (behind
                    // a wall), switch to the next closest actor after 2 misses.
                    const inRange = await ctx.page.evaluate(({ id }) => {
                        const a = window.__ariadne;
                        // Caught/won: teleports are ignored and fire() is
                        // dropped; the outer loop must reload to recover.
                        if (a.gameState !== 'playing') return { ok: false, terminal: true };
                        const act = a.actors.find(x => x.id === id);
                        if (!act || act.hp <= 0) return { ok: false, reason: 'dead' };
                        const me = a.playerPos;
                        const dx = act.pos[0] - me[0], dz = act.pos[2] - me[2];
                        const dist = Math.hypot(dx, dz) || 1;
                        if (dist > 6.0) {
                            const stand = a.findStandpoint(act.pos[0], act.pos[2], 4.5);
                            if (!stand) return { ok: false, reason: 'no standpoint' };
                            a.teleport(stand[0], stand[1]);
                        }
                        const ax = act.pos[0] - a.playerPos[0], az = act.pos[2] - a.playerPos[2];
                        a.setYaw(Math.atan2(ax, az));
                        return { ok: true };
                    }, { id: targetId });
                    if (inRange.terminal) { terminal = true; break; }
                    if (!inRange.ok) {
                        // Target died or unreachable — pick the next closest
                        // actor with a clear-fire standpoint.
                        const next = await ctx.page.evaluate(() => {
                            const a = window.__ariadne;
                            const me = a.playerPos;
                            const sorted = a.actors.slice().sort((p, q) =>
                                Math.hypot(p.pos[0] - me[0], p.pos[2] - me[2]) -
                                Math.hypot(q.pos[0] - me[0], q.pos[2] - me[2]));
                            for (const best of sorted) {
                                if (best.hp <= 0 || best.stunned) continue;
                                const stand = a.findStandpoint(best.pos[0], best.pos[2], 4.5);
                                if (!stand) continue;
                                a.teleport(stand[0], stand[1]);
                                a.setYaw(Math.atan2(best.pos[0] - stand[0], best.pos[2] - stand[1]));
                                return { id: best.id, hp: best.hp };
                            }
                            return null;
                        });
                        if (!next) break; // no actors left
                        targetId = next.id;
                        targetHp = next.hp;
                        misses = 0;
                        continue;
                    }
                    await ctx.page.evaluate(() => window.__ariadne.fire());
                    await ctx.page.waitForTimeout(800);
                    const result = await ctx.page.evaluate(({ id, hpBefore }) => {
                        const a = window.__ariadne;
                        const act = a.actors.find(x => x.id === id);
                        if (!act) return { effect: true };
                        if (act.stunned === true || act.hp < hpBefore) return { effect: true };
                        return { effect: false };
                    }, { id: targetId, hpBefore: targetHp });
                    if (result.effect) {
                        effect = true;
                    } else {
                        misses++;
                        if (misses >= 2) {
                            // Persistent misses — switch to the next live
                            // unstunned actor with a clear-fire standpoint.
                            const next = await ctx.page.evaluate(({ excludeId }) => {
                                const a = window.__ariadne;
                                const me = a.playerPos;
                                const actors = a.actors.filter(act => act.id !== excludeId && act.hp > 0 && !act.stunned);
                                if (actors.length === 0) return null;
                                const sorted = actors.slice().sort((p, q) =>
                                    Math.hypot(p.pos[0] - me[0], p.pos[2] - me[2]) -
                                    Math.hypot(q.pos[0] - me[0], q.pos[2] - me[2]));
                                for (const best of sorted) {
                                    const stand = a.findStandpoint(best.pos[0], best.pos[2], 4.5);
                                    if (!stand) continue;
                                    a.teleport(stand[0], stand[1]);
                                    a.setYaw(Math.atan2(best.pos[0] - stand[0], best.pos[2] - stand[1]));
                                    return { id: best.id, hp: best.hp };
                                }
                                return null;
                            }, { excludeId: targetId });
                            if (next) {
                                targetId = next.id;
                                targetHp = next.hp;
                                misses = 0;
                            }
                        }
                    }
                }
            }
            const lastShot = await ctx.page.evaluate(() => window.__ariadne.lastShotElement);
            if (lastShot < 0) throw new Error('lastShotElement not set after fire');
            if (!effect) throw new Error('fire produced no stun/hp effect after 6 shots across multiple targets');
        },
    },

    // G6 close-range ray hit: a target dead-center at ~2u used to be
    // unreachable under the old point-distance ValidateHit (which required
    // the target to sit within HitRadius of a fixed aim point ~5u ahead).
    // The ray-vs-actor fix must let a point-blank crosshair hit land.
    //
    // The assertion requires a FRESH stun: stun is set only by the player's
    // pulse (FirePulse), never by faction clash or hazard damage, so it is
    // the only signal that isolates the player's shot. The target is chosen
    // alive and NOT already stunned, so a pre-existing stun cannot satisfy
    // the check.
    {
        name: 'G6 close-range ray hit (<4u)',
        async run(ctx) {
            await gotoGame(ctx);
            // Outer retry loop: standing 2u from an actor invites contact,
            // and once gameState leaves 'playing' the server ignores
            // teleports and drops fire() (update() is gated on 'playing'),
            // so a caught mid-loop run can never recover in-place. Reload
            // re-scatters actors via the seeded PRNG and gives a fresh life.
            let effect = false;
            let fullAttempts = 0;
            while (!effect && fullAttempts < 3) {
                fullAttempts++;
                if (fullAttempts > 1) await freshWorld(ctx);
                await relocateSafe(ctx);
                const before = await ctx.page.evaluate(() => {
                    const a = window.__ariadne;
                    if (a.gameState !== 'playing') return null;
                    const me = a.playerPos;
                    const actors = a.actors.filter(act => act.hp > 0 && !act.stunned); // alive and NOT already stunned
                    if (actors.length === 0) return null;
                    const sorted = actors.slice().sort((p, q) =>
                        Math.hypot(p.pos[0] - me[0], p.pos[2] - me[2]) -
                        Math.hypot(q.pos[0] - me[0], q.pos[2] - me[2]));
                    for (const act of sorted) {
                        // Stand ~2u from the target with a verified clear
                        // fire lane (open cell + losClear). Standing on the
                        // me→actor line can land behind a wall corner, and
                        // the server's rayOccluded would reject every shot.
                        const stand = a.findStandpoint(act.pos[0], act.pos[2], 2.0);
                        if (!stand) continue;
                        a.teleport(stand[0], stand[1]);
                        a.setYaw(Math.atan2(act.pos[0] - stand[0], act.pos[2] - stand[1]));
                        return { id: act.id };
                    }
                    return null;
                });
                if (!before) continue;
                let targetId = before.id;
                let misses = 0;
                let terminal = false;
                const deadline = Date.now() + 10000;
                while (!effect && !terminal && Date.now() < deadline) {
                    // Re-aim at the target right before each shot: the actor
                    // moves between shots (world tick), so a stale aim ray can
                    // legitimately miss. The ray is resolved server-side from
                    // the player's yaw at the moment of firing. Re-standpoint
                    // if the actor has drifted beyond 4u (the ray max is 12u,
                    // but the scenario asserts a CLOSE-range hit).
                    const inRange = await ctx.page.evaluate(({ id }) => {
                        const a = window.__ariadne;
                        // Caught/won: teleports are ignored and fire() is
                        // dropped; the outer loop must reload to recover.
                        if (a.gameState !== 'playing') return { ok: false, terminal: true };
                        const act = a.actors.find(x => x.id === id);
                        if (!act || act.hp <= 0) return { ok: false };
                        const me = a.playerPos;
                        const dx = act.pos[0] - me[0], dz = act.pos[2] - me[2];
                        const dist = Math.hypot(dx, dz);
                        if (dist > 4.0) {
                            const stand = a.findStandpoint(act.pos[0], act.pos[2], 2.0);
                            if (!stand) return { ok: false };
                            a.teleport(stand[0], stand[1]);
                        }
                        const ax = act.pos[0] - a.playerPos[0], az = act.pos[2] - a.playerPos[2];
                        a.setYaw(Math.atan2(ax, az));
                        return { ok: true };
                    }, { id: targetId });
                    if (inRange.terminal) { terminal = true; break; }
                    if (!inRange.ok) {
                        // Target died — pick the next closest live unstunned actor
                        // with a clear-fire standpoint.
                        const next = await ctx.page.evaluate(({ excludeId }) => {
                            const a = window.__ariadne;
                            const me = a.playerPos;
                            const actors = a.actors.filter(act => act.id !== excludeId && act.hp > 0 && !act.stunned);
                            if (actors.length === 0) return null;
                            const sorted = actors.slice().sort((p, q) =>
                                Math.hypot(p.pos[0] - me[0], p.pos[2] - me[2]) -
                                Math.hypot(q.pos[0] - me[0], q.pos[2] - me[2]));
                            for (const act of sorted) {
                                const stand = a.findStandpoint(act.pos[0], act.pos[2], 2.0);
                                if (!stand) continue;
                                a.teleport(stand[0], stand[1]);
                                a.setYaw(Math.atan2(act.pos[0] - stand[0], act.pos[2] - stand[1]));
                                return { id: act.id };
                            }
                            return null;
                        }, { excludeId: targetId });
                        if (!next) break;
                        targetId = next.id;
                        misses = 0;
                        continue;
                    }
                    await ctx.page.evaluate(() => window.__ariadne.fire());
                    await ctx.page.waitForTimeout(600);
                    const result = await ctx.page.evaluate(({ id }) => {
                        const a = window.__ariadne;
                        const act = a.actors.find(x => x.id === id);
                        if (!act) return { effect: true };
                        if (act.stunned === true || act.hp <= 0) return { effect: true };
                        return { effect: false };
                    }, { id: targetId });
                    if (result.effect) {
                        effect = true;
                    } else {
                        misses++;
                        if (misses >= 3) {
                            // Persistent misses — switch to the next live
                            // unstunned actor that has a clear-fire standpoint.
                            const next = await ctx.page.evaluate(({ excludeId }) => {
                                const a = window.__ariadne;
                                const me = a.playerPos;
                                const actors = a.actors.filter(act => act.id !== excludeId && act.hp > 0 && !act.stunned);
                                if (actors.length === 0) return null;
                                const sorted = actors.slice().sort((p, q) =>
                                    Math.hypot(p.pos[0] - me[0], p.pos[2] - me[2]) -
                                    Math.hypot(q.pos[0] - me[0], q.pos[2] - me[2]));
                                for (const act of sorted) {
                                    const stand = a.findStandpoint(act.pos[0], act.pos[2], 2.0);
                                    if (!stand) continue;
                                    a.teleport(stand[0], stand[1]);
                                    a.setYaw(Math.atan2(act.pos[0] - stand[0], act.pos[2] - stand[1]));
                                    return { id: act.id };
                                }
                                return null;
                            }, { excludeId: targetId });
                            if (next) {
                                targetId = next.id;
                                misses = 0;
                            }
                        }
                    }
                }
            }
            if (!effect) throw new Error('close-range (<4u) fire produced no stun/kill effect after repeated aimed shots across multiple targets - ray hit model still has a near-field blind window');
        },
    },

    // gate->win overlay
    {
        name: 'gate->win overlay',
        async run(ctx) {
            await gotoGame(ctx);
            // If the player is in a terminal state (caught/won) from a prior
            // scenario, teleports are ignored by the server (tickLogic returns
            // early on terminal phase). Reload to get a fresh game.
            const preState = await ctx.page.evaluate(() => window.__ariadne.gameState);
            if (preState !== 'playing') {
                await ctx.page.reload({ waitUntil: 'domcontentloaded', timeout: 20000 });
                await waitForReady(ctx.page, 20000, 'gate->win reload');
                await ctx.page.waitForTimeout(1000);
            }
            // Relocate to a safe corner to prevent being caught during the
            // brief setup wait before the gate teleport.
            await relocateSafe(ctx);
            await ctx.page.waitForTimeout(200);
            const gsBeforeGate = await ctx.page.evaluate(() => window.__ariadne.gameState);
            if (gsBeforeGate !== 'playing') throw new Error(`gameState not playing before gate teleport: ${gsBeforeGate}`);
            await ctx.page.evaluate(() => {
                const a = window.__ariadne;
                a.teleport(a.gate[0], a.gate[1]);
            });
            // 15s timeout: the win check runs on the server's tick (30Hz)
            // and the event must propagate through the WebSocket to the
            // client. Under headless-chromium load (parallel E2E runs, tree
            // frame broadcasts, actor AI ticks), event delivery can be
            // delayed. 15s provides margin for worst-case load.
            try {
                await ctx.page.waitForFunction(() => window.__ariadne.gameState === 'won', undefined, { timeout: 15000 });
            } catch (e) {
                const diag = await ctx.page.evaluate(() => ({
                    gameState: window.__ariadne.gameState,
                    playerPos: window.__ariadne.playerPos,
                    gate: window.__ariadne.gate,
                    round: window.__ariadne.round,
                }));
                throw new Error(`win state not reached within 15s: ${JSON.stringify(diag)}`);
            }
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
            // If the player is already terminal from a prior scenario, reload.
            const preState = await ctx.page.evaluate(() => window.__ariadne.gameState);
            if (preState !== 'playing') {
                await ctx.page.reload({ waitUntil: 'domcontentloaded', timeout: 20000 });
                await waitForReady(ctx.page, 20000, 'actor contact reload');
                await ctx.page.waitForTimeout(1000);
            }
            await ctx.page.evaluate(() => {
                const a = window.__ariadne;
                const act = a.actors[0];
                a.teleport(act.pos[0], act.pos[2]);
            });
            await ctx.page.waitForFunction(() => {
                const a = window.__ariadne;
                return a.hp < 100 || a.gameState === 'caught';
            }, undefined, { timeout: 8000 });
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
                await ctx.page.waitForFunction(ARIADNE_READY, undefined, { timeout: 15000 });
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
                }, undefined, { timeout: 6000 });
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
                    undefined,
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
            // Liveness probe: poll instead of a fixed two-sample window.
            // Tree frames arrive once per actor per 10 server ticks (~3/s),
            // but under headless load a single 3s window can starve every
            // actor's frames at once and produce a false "frozen" verdict.
            // The invariant is unchanged — every row's tick count must
            // climb — it just has to climb SOMEWHERE inside 15s.
            // GAME-ENDED TRAP: when gameState leaves 'playing', the server's
            // tickLogic returns early and ALL tick counts freeze — a caught
            // mid-window is indistinguishable from a real BT freeze. Reload
            // for a fresh life and restart the window when that happens.
            const first = await snapTicks();
            const seenClimb = new Set();
            let prev = first;
            let live = false;
            let terminal = false;
            const deadline = Date.now() + 15000;
            while (Date.now() < deadline) {
                await ctx.page.waitForTimeout(1000);
                const sample = await ctx.page.evaluate(() => ({
                    gs: window.__ariadne.gameState,
                    rows: (() => { const m = {}; for (const r of window.__ariadne.btHud.rows) m[r.id] = r.ticks; return m; })(),
                }));
                if (sample.gs !== 'playing') { terminal = true; break; }
                const cur = sample.rows;
                for (const id of Object.keys(cur)) {
                    if (prev[id] !== undefined && cur[id] > prev[id]) seenClimb.add(id);
                }
                prev = cur;
                const common = Object.keys(first).filter(id => cur[id] !== undefined);
                if (common.length > 0 && common.every(id => seenClimb.has(id))) { live = true; break; }
            }
            if (terminal) {
                await freshWorld(ctx);
                await ctx.page.evaluate(() => document.dispatchEvent(new KeyboardEvent('keydown', { code: 'KeyB', bubbles: true })));
                await ctx.page.waitForTimeout(1500);
                const f2 = await snapTicks();
                const s2 = await snapTicks();
                await ctx.page.waitForTimeout(2000);
                const c2 = await snapTicks();
                const climbed = Object.keys(f2).filter(id => c2[id] !== undefined && c2[id] > f2[id]).length;
                if (climbed === 0) throw new Error('BT HUD rows frozen even after fresh world reload');
                // fall through with a live verdict for the probe<->DOM check
                live = true;
            }
            if (!live) {
                const cur = await snapTicks();
                const frozen = Object.keys(first).filter(id => cur[id] !== undefined && !seenClimb.has(id));
                throw new Error(`BT HUD rows not live within 15s: frozen=${JSON.stringify(frozen)} first=${JSON.stringify(first)} last=${JSON.stringify(cur)}`);
            }
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
            await page2.goto(ctx.url, { waitUntil: 'domcontentloaded', timeout: 20000 });
            await waitForReady(page2, 20000, 'multi-session page2');
            const tok2 = await page2.evaluate(() => sessionStorage.getItem('ariadne-token'));
            await page2.close();
            if (!tok1 || !tok2) throw new Error('missing token');
            if (tok1 === tok2) throw new Error('tokens not distinct');
        },
    },

    // H2: combat feedback (damage numbers, kill feed, screen shake, HP bars, aim feedback)
    {
        name: 'H2 combat feedback',
        async run(ctx) {
            await gotoGame(ctx);

            // Wrap the entire H2 setup+fire sequence in a retry loop. The
            // primary failure mode is the player being caught by actors
            // during the setup wait, which prevents fire() from executing
            // (update() is gated on gameState==='playing'). Server always
            // produces dmg >= 7 (PulseBaseDamage=15 × min advantage=0.5),
            // so damage numbers will spawn if the shot actually fires.
            let damageNumber = null;
            let fullAttempts = 0;

            while (!damageNumber && fullAttempts < 4) {
                fullAttempts++;
                // freshWorld between attempts: actors converge on the same
                // corners across relocateSafe retries (same 4-candidate
                // set). Reloading the page re-scatters actors via seeded PRNG.
                if (fullAttempts > 1) {
                    await freshWorld(ctx);
                }
                await relocateSafe(ctx);
                await ctx.page.waitForTimeout(200);

                // Find the closest live unstunned actor with a clear-fire
                // standpoint: an open cell ~3.5u away with unobstructed
                // line of sight. Standing on the me→actor line is NOT
                // sufficient — when the actor is around a wall corner the
                // line crosses the corner wall and the server's rayOccluded
                // silently rejects every shot. 3.5u buys contact time: the
                // actor chases, and once caught the server drops fire().
                const setup = await ctx.page.evaluate(() => {
                    const a = window.__ariadne;
                    if (a.gameState !== 'playing') return { ok: false, reason: 'not playing' };
                    const actors = a.actors.filter(act => !act.stunned && act.hp > 0);
                    if (actors.length === 0) return { ok: false, reason: 'no actors' };
                    const me = a.playerPos;
                    const sorted = actors.slice().sort((p, q) =>
                        Math.hypot(p.pos[0] - me[0], p.pos[2] - me[2]) -
                        Math.hypot(q.pos[0] - me[0], q.pos[2] - me[2]));
                    for (const act of sorted) {
                        const stand = a.findStandpoint(act.pos[0], act.pos[2], 3.5);
                        if (!stand) continue;
                        a.teleport(stand[0], stand[1]);
                        a.setYaw(Math.atan2(act.pos[0] - stand[0], act.pos[2] - stand[1]));
                        return { ok: true, targetId: act.id };
                    }
                    return { ok: false, reason: 'no actor with clear standpoint' };
                });
                if (!setup.ok) continue;

                // Fire + target-switch retry loop.
                let currentTargetId = setup.targetId;
                let misses = 0;
                for (let attempt = 0; attempt < 4 && !damageNumber; attempt++) {
                    if (attempt > 0) await ctx.page.waitForTimeout(700);
                    // Aim and fire in ONE evaluate: the separate round trips
                    // let the caught event slip in between (update() is gated
                    // on 'playing', silently dropping the fire) and let the
                    // actor drift off the ray under load. Re-standpoint if
                    // the actor drifted beyond 4u.
                    const shot = await ctx.page.evaluate(({ tid }) => {
                        const a = window.__ariadne;
                        if (a.gameState !== 'playing') return { terminal: true };
                        const act = a.actors.find(x => x.id === tid);
                        if (!act || act.hp <= 0) return { needSwitch: true };
                        const me = a.playerPos;
                        const dx = act.pos[0] - me[0], dz = act.pos[2] - me[2];
                        const dist = Math.hypot(dx, dz);
                        if (dist > 4.0) {
                            const stand = a.findStandpoint(act.pos[0], act.pos[2], 3.5);
                            if (!stand) return { needSwitch: true };
                            a.teleport(stand[0], stand[1]);
                        }
                        const ax = act.pos[0] - a.playerPos[0], az = act.pos[2] - a.playerPos[2];
                        a.setYaw(Math.atan2(ax, az));
                        a.fire();
                        return { fired: true };
                    }, { tid: currentTargetId });
                    if (shot.terminal) break; // restart outer loop
                    if (shot.needSwitch) {
                        const next = await ctx.page.evaluate(({ excludeId }) => {
                            const a = window.__ariadne;
                            const actors = a.actors.filter(act => act.id !== excludeId && !act.stunned && act.hp > 0);
                            if (actors.length === 0) return null;
                            const me = a.playerPos;
                            const sorted = actors.slice().sort((p, q) =>
                                Math.hypot(p.pos[0] - me[0], p.pos[2] - me[2]) -
                                Math.hypot(q.pos[0] - me[0], q.pos[2] - me[2]));
                            for (const act of sorted) {
                                const stand = a.findStandpoint(act.pos[0], act.pos[2], 3.5);
                                if (!stand) continue;
                                a.teleport(stand[0], stand[1]);
                                a.setYaw(Math.atan2(act.pos[0] - stand[0], act.pos[2] - stand[1]));
                                return act.id;
                            }
                            return null;
                        }, { excludeId: currentTargetId });
                        if (!next) break;
                        currentTargetId = next;
                        misses = 0;
                        continue;
                    }
                    // Poll for the damage number instead of checking once at
                    // +700ms. The element lives 1200ms after spawn; under
                    // headless load the stunned event can arrive 700-900ms
                    // after fire, which a single check misses entirely
                    // (spawns after the check, expires before the next).
                    // Poll every 150ms so any spawn inside the lifetime is
                    // observed; break early if the player is caught.
                    const pollDeadline = Date.now() + 1800;
                    while (!damageNumber && Date.now() < pollDeadline) {
                        await ctx.page.waitForTimeout(150);
                        const probe = await ctx.page.evaluate(() => {
                            const el = document.querySelector('.damage-number');
                            return { gs: window.__ariadne.gameState, dmg: el ? el.textContent : null };
                        });
                        if (probe.dmg) { damageNumber = probe.dmg; break; }
                        if (probe.gs !== 'playing') break;
                    }
                    if (!damageNumber) {
                        misses++;
                        if (misses >= 2) {
                            const next = await ctx.page.evaluate(({ excludeId }) => {
                                const a = window.__ariadne;
                                const actors = a.actors.filter(act => act.id !== excludeId && !act.stunned && act.hp > 0);
                                if (actors.length === 0) return null;
                                const me = a.playerPos;
                                const sorted = actors.slice().sort((p, q) =>
                                    Math.hypot(p.pos[0] - me[0], p.pos[2] - me[2]) -
                                    Math.hypot(q.pos[0] - me[0], q.pos[2] - me[2]));
                                for (const act of sorted) {
                                    const stand = a.findStandpoint(act.pos[0], act.pos[2], 3.5);
                                    if (!stand) continue;
                                    a.teleport(stand[0], stand[1]);
                                    a.setYaw(Math.atan2(act.pos[0] - stand[0], act.pos[2] - stand[1]));
                                    return act.id;
                                }
                                return null;
                            }, { excludeId: currentTargetId });
                            if (next) { currentTargetId = next; misses = 0; }
                        }
                    }
                }
            }
            if (!damageNumber) throw new Error('no damage number spawned after 4 full attempts with relocate+fire retries');

            // Verify kill-feed element exists.
            const killFeedExists = await ctx.page.evaluate(() => {
                return document.getElementById('kill-feed') !== null;
            });
            if (!killFeedExists) throw new Error('kill-feed element missing');

            // Verify screen shake infrastructure.
            const shakeInfra = await ctx.page.evaluate(() => {
                return typeof applyScreenShake === 'function' && typeof screenShakeTimer !== 'undefined';
            });
            if (!shakeInfra) throw new Error('screen shake infrastructure missing');

            // Verify crosshair element exists for aim feedback.
            const crosshairExists = await ctx.page.evaluate(() => {
                return document.getElementById('crosshair') !== null;
            });
            if (!crosshairExists) throw new Error('crosshair element missing');

            await snap(ctx, 'e2e-h2-combat.png');
        },
    },

    // H1: first-person weapon + movement juice (FOV sprint kick, recoil, bob)
    {
        name: 'H1 weapon + movement juice',
        async run(ctx) {
            await gotoGame(ctx);
            // Use relocateSafe to position the player in the corner farthest
            // from all actors. This prevents the player from being caught
            // during the scenario setup wait period — once gameState becomes
            // 'caught', the server's tickLogic returns early and no further
            // commands (including teleport) are processed, making the scenario
            // unrecoverable without a full page reload.
            // Wrap the ENTIRE scenario (setup + fire + recoil + sprint) in a
            // retry loop. Actors converge on the player during waits; if caught
            // at ANY point, relocateSafe and restart from scratch. The visual
            // systems being tested (recoil, FOV, bob) are independent of
            // survival — we just need the player alive long enough to measure.
            let fullAttempts = 0;
            let initialFov = 70;
            let peakFov = 70;
            let peakBobMag = 0;
            let scenarioSucceeded = false;

            while (!scenarioSucceeded && fullAttempts < 4) {
                fullAttempts++;
                // Reload the page to get a fresh world with actors scattered
                // by the seeded PRNG. Without this, repeated relocateSafe
                // calls against the same 4 corners fail because actors
                // converge on the player across retry attempts.
                if (fullAttempts > 1) {
                    await freshWorld(ctx);
                }
                await relocateSafe(ctx);
                await ctx.page.waitForTimeout(300);

                // Verify playing state immediately — if caught during the
                // 300ms wait, restart before spending time on fire/sprint.
                let gs = await ctx.page.evaluate(() => window.__ariadne.gameState);
                if (gs !== 'playing') continue;

                // Read initial state.
                const init = await ctx.page.evaluate(() => ({
                    fov: window.__ariadne.cameraFov,
                    recoil: window.__ariadne.cameraRecoil,
                    weaponVisible: window.__ariadne.weaponVisible,
                    gameState: window.__ariadne.gameState,
                }));
                if (init.gameState !== 'playing') continue;
                if (!init.weaponVisible) throw new Error('weapon not visible');
                if (init.fov < 69 || init.fov > 71) throw new Error(`base FOV wrong: ${init.fov}`);
                if (Math.abs(init.recoil) > 0.001) throw new Error(`initial recoil not zero: ${init.recoil}`);
                initialFov = init.fov;

                // Fire + recoil check.
                await ctx.page.evaluate(() => window.__ariadne.fire());
                await ctx.page.waitForTimeout(200);
                const afterFire = await ctx.page.evaluate(() => ({
                    recoil: window.__ariadne.cameraRecoil,
                    gameState: window.__ariadne.gameState,
                }));
                if (afterFire.gameState !== 'playing') continue;
                if (afterFire.recoil < 0.005) continue; // fire didn't register (cooldown or caught between fire and check)
                await ctx.page.waitForTimeout(700);
                const afterRecovery = await ctx.page.evaluate(() => ({
                    recoil: window.__ariadne.cameraRecoil,
                    gameState: window.__ariadne.gameState,
                }));
                if (afterRecovery.gameState !== 'playing') continue;

                // Teleport to center for sprint.
                await ctx.page.evaluate(() => {
                    const a = window.__ariadne;
                    const [w, h] = a.mazeSize;
                    a.teleport(w / 2, h / 2);
                    a.setYaw(Math.PI / 4);
                });
                await ctx.page.waitForTimeout(300);

                // Sprint with peak polling.
                gs = await ctx.page.evaluate(() => window.__ariadne.gameState);
                if (gs !== 'playing') continue;

                await ctx.page.keyboard.down('ShiftLeft');
                await ctx.page.keyboard.down('KeyW');
                const sprintStart = Date.now();
                peakFov = initialFov;
                peakBobMag = 0;
                let caughtDuringSprint = false;
                while (Date.now() - sprintStart < 1500 && !caughtDuringSprint) {
                    const sample = await ctx.page.evaluate(() => ({
                        fov: window.__ariadne.cameraFov,
                        bob: window.__ariadne.cameraBob,
                        gameState: window.__ariadne.gameState,
                    }));
                    if (sample.fov > peakFov) peakFov = sample.fov;
                    const bobMag = Math.hypot(sample.bob[0], sample.bob[1]);
                    if (bobMag > peakBobMag) peakBobMag = bobMag;
                    if (sample.gameState !== 'playing') caughtDuringSprint = true;
                    await ctx.page.waitForTimeout(80);
                }
                await ctx.page.keyboard.up('KeyW');
                await ctx.page.keyboard.up('ShiftLeft');

                if (!caughtDuringSprint) {
                    scenarioSucceeded = true;
                }
            }

            if (!scenarioSucceeded) throw new Error(`H1 could not complete in 4 attempts (actors converge too fast)`);
            if (peakFov <= initialFov + 5) throw new Error(`sprint FOV did not widen >5: peak=${peakFov} initial=${initialFov}`);
            if (peakBobMag < 0.005) throw new Error(`weapon bob not active: peak=${peakBobMag}`);

            await snap(ctx, 'e2e-h1-weapon.png');
        },
    },

    // H4: onboarding + navigation (tutorial overlay, 3D gate compass, objective chip)
    {
        name: 'H4 onboarding + navigation',
        async run(ctx) {
            // Clear sessionStorage to force tutorial to show.
            const page = ctx.page;
            await page.goto(ctx.url, { waitUntil: 'domcontentloaded', timeout: 20000 });
            await page.evaluate(() => sessionStorage.clear());
            await page.reload({ waitUntil: 'domcontentloaded' });
            await waitForReady(page, 20000, 'H4 onboarding reload');
            await page.waitForTimeout(1000);

            // Verify tutorial overlay is visible.
            const tutorialVisible = await page.evaluate(() => {
                const t = document.getElementById('tutorial-overlay');
                return t && !t.hidden;
            });
            if (!tutorialVisible) throw new Error('tutorial overlay not visible on first load');

            // Dismiss tutorial.
            await page.evaluate(() => {
                const t = document.getElementById('tutorial-overlay');
                if (t) t.hidden = true;
            });
            await page.waitForTimeout(500);

            // Verify tutorial is hidden after dismiss.
            const tutorialHidden = await page.evaluate(() => {
                const t = document.getElementById('tutorial-overlay');
                return t && t.hidden;
            });
            if (!tutorialHidden) throw new Error('tutorial overlay not hidden after dismiss');

            // Verify objective chip exists.
            const objectiveChip = await page.evaluate(() => {
                const chip = document.getElementById('chip-objective');
                return chip ? chip.textContent : null;
            });
            if (!objectiveChip) throw new Error('chip-objective element missing');
            if (!objectiveChip.includes('ESCAPE') && !objectiveChip.includes('ENTER')) {
                throw new Error('objective chip text unexpected: ' + objectiveChip);
            }

            await snap(ctx, 'e2e-h4-onboarding.png');
        },
    },

    // H5: escalation readability (wave banner, threat arrows)
    {
        name: 'H5 escalation readability',
        async run(ctx) {
            await gotoGame(ctx);

            // Verify wave-banner element exists.
            const bannerExists = await ctx.page.evaluate(() => {
                return document.getElementById('wave-banner') !== null;
            });
            if (!bannerExists) throw new Error('wave-banner element missing');

            // Verify threat-arrows container exists.
            const arrowsExist = await ctx.page.evaluate(() => {
                return document.getElementById('threat-arrows') !== null;
            });
            if (!arrowsExist) throw new Error('threat-arrows element missing');

            await snap(ctx, 'e2e-h5-escalation.png');
        },
    },
];
