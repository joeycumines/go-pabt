// playtest.mjs - Full-Stack AI Playtesting & Multimodal Contact Sheet Runner.
//
// Usage (from repo root or examples/labyrinth/e2e):
//   node examples/labyrinth/e2e/playtest.mjs [--persona stealth_baiter] [--duration-ms 9000] [--out scratch/playtest-e2e]
//
// Builds the -tags example server binary (embedding the latest client/js),
// launches the server, opens headless Chromium via Playwright, drives the
// 60Hz in-browser PlaytestBot with ZERO teleports, captures 9 time-strided
// frames, stitches an annotated 3x3 PNG contact sheet natively in Canvas,
// fetches the server's causal PA-BT telemetry & 10 Oracle verdicts from
// /api/playtest/telemetry, and writes report.md, report.json, trajectory.svg,
// and contact-sheet.png.

import { spawn, execSync } from 'node:child_process';
import { mkdirSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { assertScreenshot } from './pngcheck.mjs';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, '..', '..', '..');

function parseArgs() {
    const args = process.argv.slice(2);
    const cfg = {
        persona: 'explorer_runner',
        durationMs: 11500,
        seed: 444004,
        outDir: path.resolve(__dirname, 'artifacts', 'playtest'),
        assertAllPass: false,
    };
    for (let i = 0; i < args.length; i++) {
        if (args[i] === '--persona' && args[i + 1]) cfg.persona = args[++i];
        else if (args[i] === '--duration-ms' && args[i + 1]) cfg.durationMs = parseInt(args[++i], 10);
        else if (args[i] === '--seed' && args[i + 1]) cfg.seed = parseInt(args[++i], 10);
        else if (args[i] === '--out' && args[i + 1]) cfg.outDir = path.resolve(process.cwd(), args[++i]);
        else if (args[i] === '--assert-all-pass') cfg.assertAllPass = true;
    }
    return cfg;
}

function buildBinary() {
    const bin = path.join(tmpdir(), 'labyrinth-playtest-bin');
    execSync(`go build -tags example -o ${JSON.stringify(bin)} ./examples/labyrinth`, {
        cwd: repoRoot,
        stdio: 'pipe',
    });
    return bin;
}

function launchServer(bin, seed) {
    const child = spawn(bin, ['-seed', String(seed), '-pause-without-player=false'], { stdio: ['ignore', 'pipe', 'pipe'] });
    let output = '';
    child.stdout.on('data', (d) => { output += d.toString(); });
    child.stderr.on('data', (d) => { output += d.toString(); });

    return new Promise((resolve, reject) => {
        const deadline = setTimeout(() => {
            child.kill();
            reject(new Error(`Server startup timed out:\n${output}`));
        }, 10000);
        const poll = setInterval(() => {
            const m = output.match(/GAME\s+http:\/\/127\.0\.0\.1:(\d+)\//);
            if (m) {
                clearInterval(poll);
                clearTimeout(deadline);
                resolve({ child, port: parseInt(m[1], 10) });
            }
        }, 50);
    });
}

async function stitchContactSheet(page, frames, trail, mazeGrid, liveActors) {
    const b64Url = await page.evaluate(async ({ items, trailPoints, grid, actors }) => {
        const cols = 3, rows = 3;
        const tileW = 480, tileH = 300;
        const canvas = document.createElement('canvas');
        canvas.width = cols * tileW;
        canvas.height = rows * tileH;
        const ctx = canvas.getContext('2d');
        ctx.fillStyle = '#0d1117';
        ctx.fillRect(0, 0, canvas.width, canvas.height);

        const beatNames = [
            'Inception / Start Cell',
            'Maze Pathfinding Traversal',
            'Perception & Encounter',
            'Combat / Defensive Stun',
            'Dynamic Tracking & Kiting',
            'Quadrant Cross & Faction Front',
            'Standoff & Caster Engagement',
            'Terminal Gate Approach',
            'Resolution / Gate Reached',
        ];

        for (let i = 0; i < items.length && i < 9; i++) {
            const item = items[i];
            const col = i % cols;
            const row = Math.floor(i / cols);
            const x0 = col * tileW;
            const y0 = row * tileH;

            const img = new Image();
            await new Promise((res, rej) => {
                img.onload = res;
                img.onerror = rej;
                img.src = 'data:image/png;base64,' + item.b64;
            });
            ctx.drawImage(img, x0, y0, tileW, tileH);

            // Draw diagnostic telemetry banner at top of each tile for VLM readability.
            ctx.fillStyle = 'rgba(10, 14, 22, 0.88)';
            ctx.fillRect(x0, y0, tileW, 46);
            ctx.strokeStyle = '#30363d';
            ctx.strokeRect(x0, y0, tileW, tileH);

            const beat = beatNames[i] || `Beat #${i + 1}`;
            ctx.fillStyle = '#58a6ff';
            ctx.font = 'bold 12px monospace';
            ctx.fillText(`#${i + 1} ${beat} | t=${item.tSec}s R=${item.round}`, x0 + 8, y0 + 16);
            ctx.fillStyle = '#e6edf3';
            ctx.font = '11px monospace';
            const nearAct = item.nearestAction ? ` | PA-BT: ${item.nearestAction}` : '';
            ctx.fillText(`Pos=(${item.px},${item.pz}) HP=${item.hp} | Near: ${item.nearestId || 'none'} (${item.nearestDist}u)${nearAct}`, x0 + 8, y0 + 34);
        }

        // Draw 2D top-down minimap inset in the bottom-right corner of tile 9.
        const mapSize = 136;
        const mapX = canvas.width - mapSize - 8;
        const mapY = canvas.height - mapSize - 8;
        ctx.fillStyle = 'rgba(13, 17, 23, 0.92)';
        ctx.fillRect(mapX, mapY, mapSize, mapSize);
        ctx.strokeStyle = '#58a6ff';
        ctx.lineWidth = 1.5;
        ctx.strokeRect(mapX, mapY, mapSize, mapSize);

        ctx.fillStyle = '#79c0ff';
        ctx.font = 'bold 10px monospace';
        ctx.fillText('2D MINIMAP (21x21)', mapX + 8, mapY + 13);

        const pad = 16;
        const innerSize = mapSize - pad * 2;
        const scale = innerSize / 21.0;
        const toScreen = (x, y) => ({
            x: mapX + pad + x * scale,
            y: mapY + pad + y * scale,
        });

        // 1. Draw maze walls
        if (grid && grid.length >= 21 * 21) {
            for (let cz = 0; cz < 21; cz++) {
                for (let cx = 0; cx < 21; cx++) {
                    const kind = grid[cz * 21 + cx];
                    if (kind === 0) { // Wall
                        const p0 = toScreen(cx, cz);
                        ctx.fillStyle = '#21262d';
                        ctx.fillRect(p0.x, p0.y, Math.max(1, scale), Math.max(1, scale));
                    }
                }
            }
        }

        // 2. Start & Gate icons
        const sPt = toScreen(1.5, 1.5);
        ctx.fillStyle = '#3fb950';
        ctx.beginPath();
        ctx.arc(sPt.x, sPt.y, 4, 0, Math.PI * 2);
        ctx.fill();
        ctx.fillText('S', sPt.x + 5, sPt.y + 3);

        const gPt = toScreen(19.5, 19.5);
        ctx.fillStyle = '#d29922';
        ctx.beginPath();
        ctx.arc(gPt.x, gPt.y, 4.5, 0, Math.PI * 2);
        ctx.fill();
        ctx.fillText('G', gPt.x - 12, gPt.y + 3);

        // 3. Runner path trail
        if (trailPoints && trailPoints.length > 1) {
            ctx.strokeStyle = '#38bdf8';
            ctx.lineWidth = 2;
            ctx.beginPath();
            const y0 = trailPoints[0].z !== undefined ? trailPoints[0].z : (trailPoints[0].y !== undefined ? trailPoints[0].y : 1.5);
            const first = toScreen(trailPoints[0].x, y0);
            ctx.moveTo(first.x, first.y);
            for (let k = 1; k < trailPoints.length; k++) {
                const yk = trailPoints[k].z !== undefined ? trailPoints[k].z : (trailPoints[k].y !== undefined ? trailPoints[k].y : 1.5);
                const pt = toScreen(trailPoints[k].x, yk);
                ctx.lineTo(pt.x, pt.y);
            }
            ctx.stroke();

            // Head marker
            const yLast = trailPoints[trailPoints.length - 1].z !== undefined ? trailPoints[trailPoints.length - 1].z : (trailPoints[trailPoints.length - 1].y !== undefined ? trailPoints[trailPoints.length - 1].y : 1.5);
            const last = toScreen(trailPoints[trailPoints.length - 1].x, yLast);
            ctx.fillStyle = '#44ff66';
            ctx.beginPath();
            ctx.arc(last.x, last.y, 4, 0, Math.PI * 2);
            ctx.fill();
        }

        // 4. Live actors
        if (actors && actors.length > 0) {
            for (const a of actors) {
                const pos = a.pos || [0, 0, 0];
                const pt = toScreen(pos[0], pos[2]);
                ctx.fillStyle = a.stunned ? '#c084fc' : (a.strategy === 2 ? '#ff9900' : '#f85149');
                ctx.beginPath();
                ctx.arc(pt.x, pt.y, 2.5, 0, Math.PI * 2);
                ctx.fill();
            }
        }

        return canvas.toDataURL('image/png');
    }, { items: frames, trailPoints: trail || [], grid: mazeGrid || null, actors: liveActors || [] });

    const base64Data = b64Url.replace(/^data:image\/png;base64,/, '');
    return Buffer.from(base64Data, 'base64');
}

async function main() {
    const cfg = parseArgs();
    mkdirSync(cfg.outDir, { recursive: true });
    const bin = buildBinary();
    const { child, port } = await launchServer(bin, cfg.seed);
    const { chromium } = await import('playwright');
    const browser = await chromium.launch();

    try {
        const context = await browser.newContext({ viewport: { width: 1280, height: 800 } });
        const page = await context.newPage();
        await page.goto(`http://127.0.0.1:${port}/`);
        await page.waitForFunction(() => window.__ariadne && window.__ariadne.wsOpen && window.__ariadne.round > 0, null, { timeout: 10000 });
        await page.evaluate(() => {
            const prompt = document.getElementById('pointer-lock-prompt');
            if (prompt) prompt.hidden = true;
            const tut = document.getElementById('tutorial-overlay');
            if (tut) tut.hidden = true;
            if (window.__ariadne && window.__ariadne.setOverlays) {
                window.__ariadne.setOverlays({ btHud: true, territory: true });
            }
        });

        // Reset server telemetry buffer and start the 60Hz closed-loop bot persona.
        await fetch(`http://127.0.0.1:${port}/api/playtest/telemetry?reset=1&persona=${encodeURIComponent(cfg.persona)}`, { method: 'POST' });
        await page.evaluate((p) => window.__ariadne.playtest.start(p), cfg.persona);

        const milestones = [
            { name: 'Inception / Start Cell', test: (s) => s.round >= 2 },
            { name: 'Maze Pathfinding Traversal', test: (s) => Math.hypot(s.px - 1.5, s.pz - 1.5) > 4.0 },
            { name: 'Perception & Encounter', test: (s) => s.nearestDist > 0 && s.nearestDist < 5.5 },
            { name: 'Combat / Defensive Engagement', test: (s) => s.nearestDist > 0 && s.nearestDist < 3.8 },
            { name: 'Dynamic Tracking & Kiting', test: (s) => Math.hypot(s.px - 1.5, s.pz - 1.5) > 14.0 },
            { name: 'Quadrant Cross & Faction Front', test: (s) => s.px > 8.0 && s.pz > 8.0 },
            { name: 'Standoff & Caster Engagement', test: (s) => s.px > 12.0 && s.pz > 12.0 },
            { name: 'Terminal Gate Approach', test: (s) => Math.hypot(s.px - 19.5, s.pz - 19.5) < 6.0 },
            { name: 'Resolution / Gate Reached', test: (s) => Math.hypot(s.px - 19.5, s.pz - 19.5) < 1.5 || s.phase === 1 },
        ];

        const frames = [];
        const startTime = Date.now();
        const maxWaitPerBeat = Math.max(800, Math.floor(cfg.durationMs / 9));

        for (let i = 0; i < milestones.length; i++) {
            const m = milestones[i];
            const beatStart = Date.now();
            let state;

            while (true) {
                state = await page.evaluate((p) => {
                    const a = window.__ariadne;
                    const pos = a.playerPos || [0, 0, 0];
                    const rep = a.playtest.report();
                    const lastTrail = rep.trail.length > 0 ? rep.trail[rep.trail.length - 1] : { nearestDist: -1 };
                    let nearestAction = '';
                    let nearestId = '';
                    if (a.btHud && a.btHud.rows && a.btHud.rows.length > 0) {
                        nearestAction = a.btHud.rows[0].active || '';
                        nearestId = a.btHud.rows[0].id || '';
                    }
                    return {
                        round: a.round,
                        phase: a.gameState === 'won' ? 1 : (a.gameState === 'caught' ? 2 : 0),
                        px: Number(pos[0].toFixed(1)),
                        pz: Number(pos[2].toFixed(1)),
                        hp: a.hp,
                        actorCount: a.actorCount,
                        nearestDist: lastTrail.nearestDist,
                        nearestAction,
                        nearestId,
                        botNote: lastTrail.note || 'navigate',
                        persona: p,
                    };
                }, cfg.persona);

                const elapsedBeat = Date.now() - beatStart;
                if (m.test(state) || elapsedBeat >= maxWaitPerBeat || state.phase !== 0) {
                    break;
                }
                await page.waitForTimeout(60);
            }

            const buf = await page.screenshot({ type: 'png' });
            const totalElapsedSec = ((Date.now() - startTime) / 1000).toFixed(1);
            frames.push({
                ...state,
                tSec: totalElapsedSec,
                b64: buf.toString('base64'),
            });
        }

        // Allow remaining duration for runner to reach Gate if not already won
        const remainingMs = cfg.durationMs - (Date.now() - startTime);
        if (remainingMs > 0) {
            await page.waitForFunction(() => {
                const a = window.__ariadne;
                return a && (a.gameState === 'won' || a.gameState === 'caught');
            }, null, { timeout: remainingMs }).catch(() => {});
        }

        const mazeGrid = await page.evaluate(() => window.__ariadne && window.__ariadne.mazeGrid ? window.__ariadne.mazeGrid : null);
        const liveActors = await page.evaluate(() => window.__ariadne && window.__ariadne.actors ? window.__ariadne.actors : []);
        const clientReport = await page.evaluate(() => window.__ariadne.playtest.stop());
        const serverRes = await fetch(`http://127.0.0.1:${port}/api/playtest/telemetry?persona=${encodeURIComponent(cfg.persona)}`);
        const serverTelemetry = await serverRes.json();

        const sheetBuf = await stitchContactSheet(page, frames, clientReport.trail, mazeGrid, liveActors);
        const sheetPath = path.join(cfg.outDir, 'contact-sheet.png');
        writeFileSync(sheetPath, sheetBuf);
        assertScreenshot(sheetPath);

        writeFileSync(path.join(cfg.outDir, 'trajectory.svg'), serverTelemetry.svg);
        writeFileSync(path.join(cfg.outDir, 'report.md'), serverTelemetry.markdown);
        writeFileSync(path.join(cfg.outDir, 'report.json'), JSON.stringify({
            client: clientReport,
            causality: serverTelemetry.causality,
            oracles: serverTelemetry.oracles,
        }, null, 2));

        if (cfg.assertAllPass && serverTelemetry.oracles) {
            const failures = serverTelemetry.oracles.filter((o) => o.verdict !== 'PASS');
            if (failures.length > 0) {
                const failSummary = failures.map((f) => `  - ${f.id} (${f.name}): ${f.verdict} -> ${f.summary}`).join('\n');
                throw new Error(`[playtest-e2e] Oracle verification failed with ${failures.length} failing oracle(s):\n${failSummary}`);
            }
            process.stdout.write(`[playtest-e2e] All ${serverTelemetry.oracles.length} Oracles passed successfully (assert-all-pass=true).\n`);
        }

        process.stdout.write(serverTelemetry.markdown + '\n');
        process.stdout.write(`[playtest-e2e] Client 60Hz Bot Summary: frames=${clientReport.frames} pulses=${clientReport.pulsesFired} maxSyncDrift=${clientReport.maxSyncDrift}u teleports=${clientReport.teleportsUsed}\n`);
        process.stdout.write(`[playtest-e2e] Artifacts written to ${cfg.outDir} (contact-sheet.png verified: ${sheetBuf.length} bytes)\n`);
    } finally {
        await browser.close();
        child.kill();
    }
}

main().catch((err) => {
    console.error(err);
    process.exit(1);
});
