#!/usr/bin/env node
// playtest-player.mjs - play the labyrinth like a person and report what a
// person would notice. See examples/labyrinth/docs/playtesting.md.
//
//   node e2e/player/playtest-player.mjs [--seed 12345[,777,...]] [--journeys J1,J2,J3,J4,J5]
//        [--out DIR] [--headless] [--keep-frames]
//   node e2e/player/playtest-player.mjs --analyze DIR/J1-seed12345   # re-judge one journey, print
//   node e2e/player/playtest-player.mjs --rejudge DIR                # re-judge + rebuild a whole run
//
// Each seed x journey gets its own fresh server process and browser context.
// Exit status: 0 = no S1/S2 findings, 1 = S1/S2 findings or an invalid run,
// 2 = the harness itself failed.

import { mkdirSync, writeFileSync, rmSync, readFileSync, existsSync, readdirSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import path from 'node:path';
import { buildBinary, startServer, launchBrowser, Session, REPO_ROOT } from './harness.mjs';
import { JOURNEYS } from './journeys.mjs';
import { runDetectors, gridOf } from './detectors/index.mjs';
import { FrameStore, finding } from './detectors/common.mjs';
import { buildEvidence, encodeVideo, pruneFrames, findingsMarkdown } from './report.mjs';
import { diary, plainVerdicts } from './narrative.mjs';

export function parseArgs(argv) {
    const cfg = { seeds: [12345], journeys: ['J1', 'J2', 'J3', 'J4', 'J5'], out: null, headless: false, keepFrames: false, analyze: null, rejudge: null };
    for (let i = 0; i < argv.length; i++) {
        const a = argv[i];
        const val = () => { if (i + 1 >= argv.length) throw new Error(`${a} needs a value`); return argv[++i]; };
        if (a === '--seed' || a === '--seeds') cfg.seeds = val().split(',').map((s) => { const n = Number(s); if (!Number.isInteger(n) || n <= 0) throw new Error(`bad seed ${s}`); return n; });
        else if (a === '--journeys') cfg.journeys = val().split(',').map((s) => s.trim().toUpperCase());
        else if (a === '--out') cfg.out = path.resolve(val());
        else if (a === '--headless') cfg.headless = true;
        else if (a === '--keep-frames') cfg.keepFrames = true;
        else if (a === '--analyze') cfg.analyze = path.resolve(val());
        else if (a === '--rejudge') cfg.rejudge = path.resolve(val());
        else throw new Error(`unknown argument ${a}`);
    }
    for (const j of cfg.journeys) if (!JOURNEYS[j]) throw new Error(`unknown journey ${j} (have ${Object.keys(JOURNEYS).join(',')})`);
    return cfg;
}

// analyze re-runs the detectors over a journey directory that was recorded
// with --keep-frames (or crashed before pruning), printing the findings.
// Used to iterate on detectors without replaying the game.
function analyze(runDir) {
    const tl = JSON.parse(readFileSync(path.join(runDir, 'timeline.json'), 'utf8'));
    if (existsSync(path.join(runDir, 'PRUNED'))) console.warn('WARNING: this run was pruned; pixel-based verdicts (D1, D3, D4 frozen screen) are unreliable. Re-run with --keep-frames.');
    const findings = runDetectors(tl, runDir);
    for (const f of findings) console.log(`[${f.severity}] ${f.id} ${f.title}\n    ${JSON.stringify(f.evidence)}`);
    if (!findings.length) console.log('no findings');
    writeDiary(tl, runDir, findings);
    console.log(`\nHow it played:\n- ${plainVerdicts(tl, gridOf(tl)).join('\n- ')}\n(diary: ${path.join(runDir, 'diary.md')})`);
}

function writeDiary(tl, runDir, findings) {
    const grid = gridOf(tl);
    const text = [
        `# Player diary: ${tl.name}, seed ${tl.seed}`, '',
        'What a player experienced, second by second. Lines starting with !! are detector findings at that moment.', '',
        '## How it played', '', ...plainVerdicts(tl, grid).map((v) => `- ${v}`), '',
        '## Timeline', '', '```', ...diary(tl, grid, findings), '```', '',
    ].join('\n');
    writeFileSync(path.join(runDir, 'diary.md'), text);
}

// judgeRun turns one recorded journey into findings + evidence. Shared by
// live runs and --rejudge so both produce identical outputs.
async function judgeRun({ tl, runDir, reportBrowser, error = null, prune = true, reuseVideo = false }) {
    let findings;
    try {
        findings = runDetectors(tl, runDir);
    } catch (e) {
        findings = [finding('HARNESS', 'S1', 'Detectors crashed on this journey', { evidence: { error: String(e && e.stack || e).split('\n').slice(0, 3).join(' | ') } })];
    }
    if (error) findings.unshift(finding('HARNESS', 'S1', 'Journey crashed before finishing', { evidence: { error } }));
    if (!tl.renderer || /swiftshader|software/i.test(tl.renderer)) {
        findings.push(finding('ENV', 'S3', `Rendering on ${tl.renderer || 'unknown'} - not what a player's GPU would show`));
    }
    let video = reuseVideo && existsSync(path.join(runDir, 'video.mp4')) ? 'video.mp4' : null;
    try {
        rmSync(path.join(runDir, 'flipbooks'), { recursive: true, force: true });
        const frames = new FrameStore(tl, runDir);
        const keep = await buildEvidence({ browser: reportBrowser, tl, findings, runDir, grid: gridOf(tl), frames });
        if (!video) video = encodeVideo(tl, runDir);
        if (prune) pruneFrames(tl, runDir, keep);
        writeDiary(tl, runDir, findings);
    } catch (e) {
        findings.unshift(finding('HARNESS', 'S1', 'Evidence could not be built for this journey', { evidence: { error: String(e && e.stack || e).split('\n').slice(0, 3).join(' | ') } }));
    }
    writeFileSync(path.join(runDir, 'findings.json'), JSON.stringify(findings, null, 1));
    return { findings, video };
}

function writeSummary(out, runs, meta) {
    writeFileSync(path.join(out, 'findings.md'), findingsMarkdown(runs, meta));
    writeFileSync(path.join(out, 'findings.json'), JSON.stringify(runs.map((r) => ({ journey: r.journey, seed: r.seed, error: r.error, findings: r.findings })), null, 1));
    return runs.flatMap((r) => r.findings).filter((f) => f.severity === 'S1' || f.severity === 'S2');
}

// rejudge re-runs the detectors and rebuilds all evidence for every journey
// directory of a previous --keep-frames run, without replaying the game.
async function rejudge(out) {
    const reportBrowser = await launchBrowser({ headless: true });
    const runs = [];
    try {
        for (const dirName of readdirSync(out).sort()) {
            const runDir = path.join(out, dirName);
            if (!existsSync(path.join(runDir, 'timeline.json'))) continue;
            if (existsSync(path.join(runDir, 'PRUNED'))) console.warn(`  ${dirName}: pruned run - pixel verdicts unreliable`);
            const tl = JSON.parse(readFileSync(path.join(runDir, 'timeline.json'), 'utf8'));
            const old = existsSync(path.join(runDir, 'findings.json')) ? JSON.parse(readFileSync(path.join(runDir, 'findings.json'), 'utf8')) : [];
            const crash = old.find((f) => f.id === 'HARNESS' && f.title.startsWith('Journey crashed'));
            const error = crash ? crash.evidence.error : null;
            const { findings, video } = await judgeRun({ tl, runDir, reportBrowser, error, prune: false, reuseVideo: true });
            const [journey, seed] = [tl.name, Number(tl.seed)];
            runs.push({ journey, seed, dirName, tl, findings, video, error, durationS: Math.round(((tl.marks.at(-1) || {}).t - (tl.marks[0] || {}).t) / 1000) || 0 });
            console.log(`  ${dirName}: ${findings.length} findings`);
        }
    } finally {
        await reportBrowser.close();
    }
    const commit = execFileSync('git', ['rev-parse', '--short', 'HEAD'], { cwd: REPO_ROOT }).toString().trim();
    const bad = writeSummary(out, runs, { commit: `${commit} (re-judged recording)`, renderer: runs.map((r) => r.tl.renderer).find(Boolean) || 'unknown', headless: false, date: new Date().toISOString() });
    console.log(`\n${bad.length} S1/S2 findings. Report: ${path.join(out, 'findings.md')}`);
    process.exitCode = bad.length ? 1 : 0;
}

async function main() {
    const cfg = parseArgs(process.argv.slice(2));
    if (cfg.analyze) { analyze(cfg.analyze); return; }
    if (cfg.rejudge) { await rejudge(cfg.rejudge); return; }
    const stamp = new Date().toISOString().replace(/[:.]/g, '-');
    const out = cfg.out || path.join(REPO_ROOT, 'examples', 'labyrinth', 'e2e', 'artifacts', 'player', stamp);
    mkdirSync(out, { recursive: true });
    const commit = execFileSync('git', ['rev-parse', '--short', 'HEAD'], { cwd: REPO_ROOT }).toString().trim();
    const dirty = execFileSync('git', ['status', '--porcelain', '--', 'examples/labyrinth/client', 'examples/labyrinth/game', 'examples/labyrinth/main.go'], { cwd: REPO_ROOT }).toString().trim();
    console.log(`player-eye playtest -> ${out}`);
    const bin = buildBinary(path.join(out, 'bin'));
    const browser = await launchBrowser({ headless: cfg.headless });
    const reportBrowser = await launchBrowser({ headless: true });
    const runs = [];
    try {
        for (const seed of cfg.seeds) {
            for (const journey of cfg.journeys) {
                const dirName = `${journey}-seed${seed}`;
                const runDir = path.join(out, dirName);
                mkdirSync(runDir, { recursive: true });
                process.stdout.write(`  ${dirName} ... `);
                const started = Date.now();
                let error = null;
                let tl = null;
                let server = null, session = null;
                try {
                    server = await startServer(bin, seed);
                    session = new Session({ browser, server, outDir: runDir, name: journey, headless: cfg.headless });
                    await session.open();
                    await JOURNEYS[journey](session);
                } catch (e) {
                    error = String(e && e.stack ? e.stack.split('\n').slice(0, 3).join(' | ') : e);
                } finally {
                    if (session) { try { tl = await session.close(); } catch (e) { error = error || `session close failed: ${e}`; tl = session.timeline; } }
                    if (server) await server.stop();
                }
                if (!tl) {
                    console.log(`could not start: ${error}`);
                    runs.push({ journey, seed, dirName, tl: { name: journey, seed, marks: [], samples: [], frames: [], events: [], wire: [], dom: [] }, findings: [finding('HARNESS', 'S1', 'Journey could not start', { evidence: { error } })], video: null, error, durationS: 0 });
                    continue;
                }
                const { findings, video } = await judgeRun({ tl, runDir, reportBrowser, error, prune: !cfg.keepFrames });
                const run = { journey, seed, dirName, tl, findings, video, error, durationS: Math.round((Date.now() - started) / 1000) };
                runs.push(run);
                const s = (sev) => findings.filter((f) => f.severity === sev).length;
                console.log(`${run.durationS}s  S1=${s('S1')} S2=${s('S2')} S3=${s('S3')}${error ? '  (journey error)' : ''}`);
            }
        }
    } finally {
        await browser.close();
        await reportBrowser.close();
        rmSync(path.join(out, 'bin'), { recursive: true, force: true });
    }
    const meta = {
        commit: commit + (dirty ? ' (+uncommitted game changes)' : ''),
        renderer: runs.map((r) => r.tl.renderer).find(Boolean) || 'unknown',
        headless: cfg.headless, date: new Date().toISOString(),
    };
    const bad = writeSummary(out, runs, meta);
    console.log(`\n${bad.length} S1/S2 findings. Report: ${path.join(out, 'findings.md')}`);
    process.exitCode = bad.length ? 1 : 0;
}

if (import.meta.url === `file://${process.argv[1]}`) {
    main().catch((e) => { console.error(e); process.exitCode = 2; });
}
