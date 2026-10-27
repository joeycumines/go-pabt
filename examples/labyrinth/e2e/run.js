// run.js - committed automated E2E runner for the Labyrinth example.
//
// One documented command: `npm test` (from examples/labyrinth/e2e).
//
// It builds the -tags example binary, launches the server on 127.0.0.1:0
// with a fixed seed, parses the REAL port from the log line (SERVER TRAP),
// then runs the Playwright scenarios in scenarios.js. Screenshots land in
// ./artifacts (git-excluded) and are asserted genuine (>30KB, PNG magic).

import { spawn, execSync } from 'node:child_process';
import { mkdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { assertScreenshot } from './pngcheck.mjs';

// Screenshot liveness (size + PNG magic + non-blank luminance) lives in
// pngcheck.mjs so it can be unit-tested without running the full suite.

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, '..', '..', '..');
const artifactsDir = path.resolve(__dirname, 'artifacts');
const seed = 444004;

function log(msg) { process.stdout.write(`[e2e] ${msg}\n`); }

function buildBinary() {
    // E2E_BIN: skip the build and use a prebuilt server binary. This lets
    // the suite exercise a `-race` build (the reconnect/session code is
    // concurrent and deserves detector coverage under real multi-connection
    // load) or any other custom build without editing the runner.
    if (process.env.E2E_BIN) {
        log(`using prebuilt binary from E2E_BIN: ${process.env.E2E_BIN}`);
        return process.env.E2E_BIN;
    }
    log('building -tags example binary ...');
    const bin = path.join(tmpdir(), 'labyrinth-e2e-bin');
    execSync(`go build -tags example -o ${JSON.stringify(bin)} ./examples/labyrinth`, {
        cwd: repoRoot, stdio: 'pipe',
    });
    log(`binary: ${bin}`);
    return bin;
}

function launchServer(bin) {
    log(`launching server seed ${seed} ...`);
    const child = spawn(bin, ['-seed', String(seed), '-pause-without-player=false'], {
        stdio: ['ignore', 'pipe', 'pipe'],
    });
    let output = '';
    child.stdout.on('data', (d) => { output += d.toString(); });
    child.stderr.on('data', (d) => { output += d.toString(); });

    return new Promise((resolve, reject) => {
        const deadline = setTimeout(() => {
            child.kill();
            reject(new Error(`server did not print GAME line within 10s. Output:\n${output}`));
        }, 10000);

        const poll = setInterval(() => {
            const m = output.match(/GAME\s+http:\/\/127\.0\.0\.1:(\d+)\//);
            if (m) {
                clearInterval(poll);
                clearTimeout(deadline);
                const port = parseInt(m[1], 10);
                log(`server ready on port ${port} (PID ${child.pid})`);
                resolve({ child, port });
            }
        }, 100);
    });
}

async function main() {
    mkdirSync(artifactsDir, { recursive: true });
    const bin = buildBinary();
    const { child, port } = await launchServer(bin);

    const { chromium } = await import('playwright');
    const browser = await chromium.launch();
    let failures = 0;
    let passes = 0;

    try {
        const context = await browser.newContext({ viewport: { width: 1280, height: 800 } });
        const page = await context.newPage();
        const consoleErrors = [];
        page.on('console', (m) => {
            if (m.type() === 'error') {
                const t = m.text();
                if (/pointer.?lock|NotAllowedError|autoplay|user gesture|focus/i.test(t)) return;
                consoleErrors.push(t);
            }
        });

        const scenarios = (await import('./scenarios.js')).default;
        // E2E_ONLY=<substring> runs only scenarios whose name contains the
        // substring; useful for iterating on a single flaky scenario without
        // paying the full suite cost.
        const only = process.env.E2E_ONLY;
        const selected = only ? scenarios.filter(sc => sc.name.includes(only)) : scenarios;
        if (only) log(`E2E_ONLY=${only}: ${selected.length}/${scenarios.length} scenarios selected`);
        const ctx = {
            page, context, port, seed, artifactsDir, consoleErrors,
            assertScreenshot,
            url: `http://127.0.0.1:${port}/`,
        };

        for (const sc of selected) {
            const t0 = Date.now();
            try {
                await sc.run(ctx);
                const ms = Date.now() - t0;
                log(`PASS  ${sc.name} (${ms}ms)`);
                passes++;
            } catch (err) {
                const ms = Date.now() - t0;
                log(`FAIL  ${sc.name} (${ms}ms): ${err.message}`);
                failures++;
            }
        }

        if (consoleErrors.length > 0) {
            log(`FAIL  console errors accumulated (${consoleErrors.length}):`);
            for (const e of consoleErrors.slice(0, 10)) log(`      ${e}`);
            failures++;
        }
    } finally {
        await browser.close();
        child.kill();
        await new Promise(r => setTimeout(r, 300));
        if (!child.killed) child.kill('SIGKILL');
    }

    log(`\n[e2e] ${passes} passed, ${failures} failed`);
    return failures > 0 ? 1 : 0;
}

main().then(
    (code) => process.exit(code ?? 0),
    (err) => {
        log(`FATAL: ${err.stack || err.message}`);
        process.exit(1);
    },
);
