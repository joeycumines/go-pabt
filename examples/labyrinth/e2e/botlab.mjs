// botlab.mjs - Model-driven playtesting harness for examples/labyrinth.
//
// The harness runs a *bot* against the real game in a real browser and
// returns a record an agent can act on:
//
//   frames/frame-0001.png ...   every captured frame, individually indexed
//   frames/index.json           the index: which frame, when, what state
//   contact-sheet-01.png ...    chronological contact sheets, 36 tiles a page
//   story-sheet.png             the narrative beats, chosen from the session
//   events.jsonl                the server event stream
//   states.jsonl                the state samples
//   observation.json            the machine-readable observation bundle
//   report.md                   the same bundle in markdown
//
// The bot is *source code* supplied by the caller (--bot) or one of the
// built-in personas (--persona). It runs through the same Bot SDK, the same
// client input path and the same server as a human player: no teleports, no
// privileged access, no cheating on the rules.
//
// A session that ends (the runner is caught, or reaches the gate) is
// restarted so the capture keeps its full length: a playtest wants to see
// repeated attempts, not one attempt padded with death screens.
//
// Usage:
//   node examples/labyrinth/e2e/botlab.mjs --bot examples/labyrinth/e2e/bots/gate-runner.js \
//        --seconds 20 --out scratch/botlab
//   node examples/labyrinth/e2e/botlab.mjs --persona combat_hunter --seconds 10

import { spawn, execSync } from 'node:child_process';
import { mkdirSync, writeFileSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { assertScreenshot, assertNotBlank } from './pngcheck.mjs';
import { contactSheetHtml, storySheetHtml, selectStoryBeats } from './sheets.mjs';
import { buildObservation, formatBundle, formatReport } from './observation.mjs';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, '..', '..', '..');
const TILES_PER_PAGE = 36;

export function parseArgs(argv = process.argv.slice(2)) {
    const cfg = {
        botFile: '',
        persona: '',
        name: '',
        seconds: 20,
        seed: 444004,
        fps: 6,
        sampleHz: 20,
        captureWidth: 960,
        viewportWidth: 1280,
        viewportHeight: 800,
        outDir: path.resolve(__dirname, '..', '..', '..', 'scratch', 'botlab'),
        assertFrames: 0,
        maxEpisodes: 8,
        gpu: true,
    };
    for (let i = 0; i < argv.length; i++) {
        const a = argv[i];
        if (a === '--bot' && argv[i + 1]) cfg.botFile = path.resolve(process.cwd(), argv[++i]);
        else if (a === '--persona' && argv[i + 1]) cfg.persona = argv[++i];
        else if (a === '--name' && argv[i + 1]) cfg.name = argv[++i];
        else if (a === '--seconds' && argv[i + 1]) cfg.seconds = parseFloat(argv[++i]);
        else if (a === '--seed' && argv[i + 1]) cfg.seed = parseInt(argv[++i], 10);
        else if (a === '--fps' && argv[i + 1]) cfg.fps = parseFloat(argv[++i]);
        else if (a === '--sample-hz' && argv[i + 1]) cfg.sampleHz = parseFloat(argv[++i]);
        else if (a === '--capture-width' && argv[i + 1]) cfg.captureWidth = parseInt(argv[++i], 10);
        else if (a === '--out' && argv[i + 1]) cfg.outDir = path.resolve(process.cwd(), argv[++i]);
        else if (a === '--assert-frames' && argv[i + 1]) cfg.assertFrames = parseInt(argv[++i], 10);
        else if (a === '--max-episodes' && argv[i + 1]) cfg.maxEpisodes = parseInt(argv[++i], 10);
        else if (a === '--no-gpu') cfg.gpu = false;
    }
    if (!cfg.name) {
        cfg.name = cfg.botFile
            ? path.basename(cfg.botFile).replace(/\.js$/, '')
            : (cfg.persona || 'bot');
    }
    cfg.captureHeight = Math.round(cfg.captureWidth * (cfg.viewportHeight / cfg.viewportWidth));
    return cfg;
}

function buildBinary() {
    const bin = path.join(tmpdir(), 'labyrinth-botlab-bin');
    execSync(`go build -tags example -o ${JSON.stringify(bin)} ./examples/labyrinth`, {
        cwd: repoRoot,
        stdio: 'pipe',
    });
    return bin;
}

function launchServer(bin, seed) {
    const child = spawn(bin, ['-seed', String(seed)], { stdio: ['ignore', 'pipe', 'pipe'] });
    let output = '';
    child.stdout.on('data', (d) => { output += d.toString(); });
    child.stderr.on('data', (d) => { output += d.toString(); });
    return new Promise((resolve, reject) => {
        const deadline = setTimeout(() => {
            child.kill();
            reject(new Error(`Server startup timed out:\n${output}`));
        }, 15000);
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

// alignShots joins the captured frames to the state samples by wall clock,
// so every screenshot carries the world state that produced it.
export function alignShots(shots, samples) {
    const out = [];
    let si = 0;
    for (const shot of shots) {
        while (si + 1 < samples.length && samples[si + 1].epochMs <= shot.epochMs) si++;
        const sample = samples.length > 0
            ? samples[Math.min(si, samples.length - 1)]
            : null;
        out.push({ shot, sample });
    }
    return out;
}

// preparePage brings a freshly loaded page to a state where the bot can run:
// the tutorial and pointer-lock prompt are dismissed and the PA-BT HUD and
// territory overlay are visible.
async function preparePage(page, port) {
    await page.waitForFunction(
        () => window.__ariadne && window.__ariadne.wsOpen && window.__ariadne.round > 0,
        null,
        { timeout: 20000 },
    );
    await page.evaluate(() => {
        const prompt = document.getElementById('pointer-lock-prompt');
        if (prompt) prompt.hidden = true;
        const tut = document.getElementById('tutorial-overlay');
        if (tut) tut.hidden = true;
        if (window.__ariadne.setOverlays) {
            window.__ariadne.setOverlays({ btHud: true, territory: true });
        }
    });
}

async function startBot(page, cfg, botSource) {
    if (botSource) {
        const loaded = await page.evaluate(
            ({ src, name }) => window.__ariadne.bots.load(src, { name }),
            { src: botSource, name: cfg.name },
        );
        if (!loaded.ok) throw new Error(`bot source failed to load: ${loaded.error}`);
    } else {
        const loaded = await page.evaluate(
            (persona) => window.__ariadne.bots.loadPersona(persona),
            cfg.persona || 'explorer_runner',
        );
        if (!loaded.ok) throw new Error(`persona failed to load: ${loaded.error}`);
    }
}

// measureRenderFps counts animation frames so the screencast can be asked
// for exactly the capture rate we want instead of every composited frame.
async function measureRenderFps(page) {
    await page.evaluate(() => {
        window.__rafCount = 0;
        if (!window.__rafPatched) {
            window.__rafPatched = true;
            const orig = window.requestAnimationFrame.bind(window);
            window.requestAnimationFrame = (cb) => orig((t) => { window.__rafCount++; cb(t); });
        }
    });
    await page.waitForTimeout(1000);
    return page.evaluate(() => window.__rafCount);
}

// renderSheet writes the sheet HTML into the page, sizes the viewport to the
// content so the screenshot has no dead space, and returns the PNG bytes.
async function renderSheet(page, html) {
    await page.setContent(html, { waitUntil: 'load' });
    const size = await page.evaluate(() => {
        // Measure the content, not the document: the document is always at
        // least as tall as the viewport, which would leave dead space.
        const grid = document.querySelector('.grid');
        const rect = grid ? grid.getBoundingClientRect() : document.body.getBoundingClientRect();
        return {
            w: Math.ceil(Math.max(rect.width, document.body.scrollWidth)),
            h: Math.ceil(rect.bottom + 8),
        };
    });
    await page.setViewportSize({
        width: Math.max(320, Math.min(size.w, 4096)),
        height: Math.max(240, Math.min(size.h, 4096)),
    });
    await page.waitForTimeout(60);
    return page.screenshot({ fullPage: true });
}

export async function run(cfg) {
    mkdirSync(cfg.outDir, { recursive: true });
    const bin = buildBinary();
    const { child, port } = await launchServer(bin, cfg.seed);
    const { chromium } = await import('playwright');
    const browser = await chromium.launch({
        args: cfg.gpu ? ['--enable-gpu', '--ignore-gpu-blocklist'] : [],
    });
    const result = { cfg, port, frames: [], sheets: [], errors: [], episodes: [] };

    try {
        const context = await browser.newContext({
            viewport: { width: cfg.viewportWidth, height: cfg.viewportHeight },
            deviceScaleFactor: 1,
        });
        const page = await context.newPage();
        const client = await context.newCDPSession(page);
        await page.goto(`http://127.0.0.1:${port}/`);
        await preparePage(page, port);

        let botSource = '';
        if (cfg.botFile) {
            botSource = readFileSync(cfg.botFile, 'utf8');
            writeFileSync(path.join(cfg.outDir, 'bot-source.js'), botSource);
        }

        await fetch(`http://127.0.0.1:${port}/api/playtest/telemetry?reset=1`, { method: 'POST' });

        const renderFps = await measureRenderFps(page);
        const everyNth = Math.max(1, Math.round(renderFps / cfg.fps));
        const startedAt = new Date().toISOString();
        const startMs = Date.now();
        let budgetMs = cfg.seconds * 1000;
        let episodeIndex = 0;
        let shots = [];

        const onFrame = (f) => {
            shots.push({ data: f.data, epochMs: f.metadata.timestamp * 1000 });
            // The screencast stalls unless every frame is acknowledged.
            client.send('Page.screencastFrameAck', { sessionId: f.sessionId }).catch(() => {});
        };
        client.on('Page.screencastFrame', onFrame);

        while (budgetMs > 0 && episodeIndex < cfg.maxEpisodes) {
            episodeIndex++;
            shots = [];
            await page.evaluate((hz) => window.__ariadne.inspect.start({ hz }), cfg.sampleHz);
            await startBot(page, cfg, botSource);
            await client.send('Page.startScreencast', {
                format: 'png',
                maxWidth: cfg.captureWidth,
                maxHeight: cfg.captureHeight,
                everyNthFrame: everyNth,
            });

            const epStart = Date.now();
            let terminal = null;
            while (true) {
                const elapsed = Date.now() - epStart;
                if (elapsed >= budgetMs) break;
                const state = await page.evaluate(() => ({
                    gs: window.__ariadne.gameState,
                    round: window.__ariadne.round,
                    hp: window.__ariadne.hp,
                }));
                if (state.gs && state.gs !== 'playing') {
                    terminal = state;
                    break;
                }
                await page.waitForTimeout(120);
            }
            if (terminal) {
                // Let the terminal overlay render so the last frame shows it.
                await page.waitForTimeout(400);
            }
            await client.send('Page.stopScreencast');
            const durationMs = Date.now() - epStart;

            const botStatus = await page.evaluate(() => window.__ariadne.bots.status());
            const record = await page.evaluate(() => ({
                samples: window.__ariadne.inspect.frames(),
                events: window.__ariadne.inspect.events(),
                stats: window.__ariadne.inspect.stats(),
            }));
            result.episodes.push({
                index: episodeIndex,
                durationSec: durationMs / 1000,
                terminal: terminal ? terminal.gs : null,
                frames: shots.length,
                samples: record.samples.length,
                botFrames: botStatus.frames,
                shotsFired: botStatus.shotsFired,
                errors: botStatus.errors.length,
                samples_: record.samples,
                events_: record.events,
                stats: record.stats,
                botStatus,
                shots,
            });
            budgetMs -= durationMs;
            if (!terminal) break;
            // The session ended: reload for a fresh attempt instead of
            // capturing minutes of the end screen.
            await page.reload();
            await preparePage(page, port);
        }

        const durationSec = (Date.now() - startMs) / 1000;

        // Merge the episode records into one capture.
        const framesDir = path.join(cfg.outDir, 'frames');
        mkdirSync(framesDir, { recursive: true });
        const frameRecords = [];
        const allSamples = [];
        const allEvents = [];
        let blankFrames = 0;
        let minFrameBytes = Infinity;

        for (const ep of result.episodes) {
            const aligned = alignShots(ep.shots, ep.samples_);
            aligned.forEach(({ shot, sample }, k) => {
                const buf = Buffer.from(shot.data, 'base64');
                const file = `frame-${String(frameRecords.length + 1).padStart(4, '0')}.png`;
                writeFileSync(path.join(framesDir, file), buf);
                minFrameBytes = Math.min(minFrameBytes, buf.length);
                try {
                    assertNotBlank(buf, file);
                } catch (err) {
                    blankFrames++;
                    result.errors.push(`blank frame ${file}: ${err.message}`);
                }
                const s = sample || {};
                const gate = s.gate || null;
                const index = frameRecords.length + 1;
                frameRecords.push({
                    i: index,
                    episode: ep.index,
                    t: s.t !== undefined ? s.t : 0,
                    epochMs: shot.epochMs,
                    round: s.round !== undefined ? s.round : -1,
                    phase: s.phase || 'unknown',
                    hp: s.hp !== undefined ? s.hp : 0,
                    x: s.x !== undefined ? s.x : 0,
                    z: s.z !== undefined ? s.z : 0,
                    cell: s.cell || [0, 0],
                    nearest: s.nearest || null,
                    events: s.events || [],
                    gateDistance: gate && s.x !== undefined
                        ? Math.hypot(s.x - gate.x, s.z - gate.z)
                        : null,
                    bytes: buf.length,
                    file: `frames/${file}`,
                    dataUrl: `data:image/png;base64,${shot.data}`,
                });
            });
            allSamples.push(...ep.samples_);
            allEvents.push(...ep.events_);
        }

        writeFileSync(
            path.join(framesDir, 'index.json'),
            JSON.stringify(frameRecords.map((f) => ({ ...f, dataUrl: undefined })), null, 2),
        );
        writeFileSync(
            path.join(cfg.outDir, 'events.jsonl'),
            allEvents.map((e) => JSON.stringify(e)).join('\n') + '\n',
        );
        writeFileSync(
            path.join(cfg.outDir, 'states.jsonl'),
            allSamples.map((s) => JSON.stringify(s)).join('\n') + '\n',
        );

        const mazeGrid = await page.evaluate(() => window.__ariadne.mazeGrid || null);
        const lastEpisode = result.episodes[result.episodes.length - 1];

        // Contact sheets: every captured frame, chronologically, 36 a page.
        const sheetPage = await context.newPage();
        await sheetPage.setViewportSize({ width: 2200, height: 1600 });
        const pages = [];
        for (let i = 0; i < frameRecords.length; i += TILES_PER_PAGE) {
            const chunk = frameRecords.slice(i, i + TILES_PER_PAGE);
            const pageNo = pages.length + 1;
            const html = contactSheetHtml(chunk, {
                title: `BotLab contact sheet - ${cfg.name}`,
                meta: `run=${cfg.name} seed=${cfg.seed} duration=${durationSec.toFixed(1)}s ` +
                    `frames=${frameRecords.length} episodes=${result.episodes.length} page ${pageNo}`,
                cols: 6,
            });
            const buf = await renderSheet(sheetPage, html);
            const name = `contact-sheet-${String(pageNo).padStart(2, '0')}.png`;
            writeFileSync(path.join(cfg.outDir, name), buf);
            assertScreenshot(path.join(cfg.outDir, name));
            pages.push({ name, bytes: buf.length });
        }

        // Story sheet: the beats are chosen from the last episode, so the
        // narrative is about one attempt rather than a splice of several.
        const storyFrames = frameRecords.filter((f) => f.episode === lastEpisode.index);
        const trail = lastEpisode.samples_.map((s) => ({ x: s.x, z: s.z }));
        const beats = selectStoryBeats(storyFrames, { maxBeats: 9 });
        const lastSample = lastEpisode.samples_.length > 0
            ? lastEpisode.samples_[lastEpisode.samples_.length - 1]
            : null;
        const startPoint = trail.length > 0 ? trail[0] : { x: 1.5, z: 1.5 };
        const storyHtml = storySheetHtml(beats.map((b) => b.frame), {
            title: `BotLab story sheet - ${cfg.name} (episode ${lastEpisode.index})`,
            meta: beats.map((b) => `#${b.frame.i} ${b.why}`).join('   |   '),
            cols: 3,
            maze: mazeGrid ? {
                w: 21,
                h: 21,
                grid: mazeGrid,
                startX: startPoint.x,
                startY: startPoint.z,
                gateX: lastSample && lastSample.gate ? lastSample.gate.x : 19.5,
                gateY: lastSample && lastSample.gate ? lastSample.gate.z : 19.5,
            } : null,
            trail,
            actors: lastSample ? lastSample.actors.map((a) => ({
                x: a.x, z: a.z, stunned: a.stunned, strategy: a.strategy,
            })) : [],
        });
        const storyBuf = await renderSheet(sheetPage, storyHtml);
        writeFileSync(path.join(cfg.outDir, 'story-sheet.png'), storyBuf);
        assertScreenshot(path.join(cfg.outDir, 'story-sheet.png'));
        await sheetPage.close();

        const merged = mergeEpisodeStats(result.episodes);
        const botStatus = {
            name: lastEpisode.botStatus.name,
            frames: result.episodes.reduce((a, e) => a + e.botFrames, 0),
            shotsFired: result.episodes.reduce((a, e) => a + e.shotsFired, 0),
            teleportsUsed: result.episodes.reduce((a, e) => a + e.botStatus.teleportsUsed, 0),
            maxSyncDrift: Math.max(...result.episodes.map((e) => e.botStatus.maxSyncDrift)),
            errors: result.episodes.flatMap((e) => e.botStatus.errors),
            logs: result.episodes.flatMap((e) => e.botStatus.logs),
        };

        const obs = buildObservation({
            run: {
                name: cfg.name,
                botSource: cfg.botFile ? path.relative(repoRoot, cfg.botFile) : '',
                persona: cfg.persona,
                seed: cfg.seed,
                durationSec,
                renderFps,
                captureFps: renderFps / everyNth,
                captureWidth: cfg.captureWidth,
                startedAt,
            },
            bot: {
                name: botStatus.name,
                frames: botStatus.frames,
                shotsFired: botStatus.shotsFired,
                teleportsUsed: botStatus.teleportsUsed,
                maxSyncDrift: botStatus.maxSyncDrift,
            },
            stats: { ...merged, shotsFired: botStatus.shotsFired },
            frames: frameRecords,
            events: allEvents,
            errors: botStatus.errors,
            logs: botStatus.logs,
        });
        obs.blankFrames = blankFrames;
        obs.minFrameBytes = minFrameBytes === Infinity ? 0 : minFrameBytes;
        obs.sheets = pages;
        obs.episodes = result.episodes.map((e) => ({
            index: e.index,
            durationSec: Number(e.durationSec.toFixed(2)),
            terminal: e.terminal,
            frames: e.frames,
            samples: e.samples,
            botFrames: e.botFrames,
            shotsFired: e.shotsFired,
            errors: e.errors,
        }));

        writeFileSync(path.join(cfg.outDir, 'observation.json'), JSON.stringify(obs, null, 2));
        writeFileSync(path.join(cfg.outDir, 'report.md'), formatReport(obs));

        result.observation = obs;
        result.frames = frameRecords;
        result.sheets = pages;
        result.botStatus = botStatus;
        return result;
    } finally {
        await browser.close();
        child.kill();
    }
}

// mergeEpisodeStats combines the per-episode statistics into one record.
export function mergeEpisodeStats(episodes) {
    const merged = {
        samples: 0,
        events: 0,
        eventCounts: {},
        cellsVisited: 0,
        distanceTravelled: 0,
        stuckSamples: 0,
        stuckCells: [],
        shotsFired: 0,
        shotsHit: 0,
        damageTaken: 0,
        deaths: 0,
        gateDistanceNow: null,
        gateDistanceMin: null,
        gateDistanceMinAt: null,
    };
    const stuckByCell = new Map();
    for (const ep of episodes) {
        const s = ep.stats;
        merged.samples += s.samples || 0;
        merged.events += s.events || 0;
        merged.cellsVisited += s.cellsVisited || 0;
        merged.distanceTravelled += s.distanceTravelled || 0;
        merged.stuckSamples += s.stuckSamples || 0;
        merged.shotsFired += s.shotsFired || 0;
        merged.shotsHit += s.shotsHit || 0;
        merged.damageTaken += s.damageTaken || 0;
        merged.deaths += s.deaths || 0;
        for (const sc of (s.stuckCells || [])) {
            if (!sc || ! Array.isArray(sc.cell)) continue;
            const key = `${sc.cell[0]},${sc.cell[1]}`;
            stuckByCell.set(key, (stuckByCell.get(key) || 0) + (sc.samples || 0));
        }
        for (const [k, v] of Object.entries(s.eventCounts || {})) {
            merged.eventCounts[k] = (merged.eventCounts[k] || 0) + v;
        }
        if (s.gateDistanceMin !== null && s.gateDistanceMin !== undefined) {
            if (merged.gateDistanceMin === null || s.gateDistanceMin < merged.gateDistanceMin) {
                merged.gateDistanceMin = s.gateDistanceMin;
                merged.gateDistanceMinAt = s.gateDistanceMinAt;
            }
        }
        merged.gateDistanceNow = s.gateDistanceNow;
    }
    merged.stuckCells = Array.from(stuckByCell.entries())
        .map(([k, samples]) => ({ cell: k.split(',').map(Number), samples }))
        .sort((a, b) => b.samples - a.samples)
        .slice(0, 5);
    merged.distanceTravelled = Number(merged.distanceTravelled.toFixed(2));
    return merged;
}

async function main() {
    const cfg = parseArgs();
    const result = await run(cfg);
    const obs = result.observation;
    process.stdout.write(formatBundle(obs) + '\n');
    process.stdout.write(
        `[botlab] ${result.frames.length} frames captured across ${result.episodes.length} episode(s) ` +
        `(min ${obs.minFrameBytes}B, ${obs.blankFrames} blank) | ${result.sheets.length} contact sheets | story sheet\n` +
        `[botlab] artifacts: ${cfg.outDir}\n`,
    );
    if (obs.blankFrames > 0) {
        process.stderr.write(`[botlab] ${obs.blankFrames} blank frames captured\n`);
        process.exit(1);
    }
    if (cfg.assertFrames > 0 && result.frames.length < cfg.assertFrames) {
        process.stderr.write(
            `[botlab] expected at least ${cfg.assertFrames} frames, captured ${result.frames.length}\n`,
        );
        process.exit(1);
    }
}

const invokedDirectly = process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url);
if (invokedDirectly) {
    main().catch((err) => {
        console.error(err);
        process.exit(1);
    });
}
