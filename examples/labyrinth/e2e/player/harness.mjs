// harness.mjs - one player-eye session: a fresh game server, a fresh browser
// context, the read-only in-page probe, a CDP screencast of the composited
// page, and unfiltered capture of every console message, page error, socket
// close and failed request.
//
// Nothing here moves the player or edits the page. Input comes only from
// actuate.mjs (real Playwright keyboard/mouse); this module records.

import { spawn, execFileSync } from 'node:child_process';
import { mkdirSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';
import { installProbe } from './inpage.mjs';

const HERE = path.dirname(fileURLToPath(import.meta.url));
export const LABYRINTH_DIR = path.resolve(HERE, '..', '..');
export const REPO_ROOT = path.resolve(LABYRINTH_DIR, '..', '..');

export const VIEWPORT = { width: 1280, height: 800 };
// Screencast frames are half-size: enough for every pixel detector, small
// enough to keep a 90s journey's frames in the low hundreds of MB.
const CAST = { maxWidth: 640, maxHeight: 400, everyNthFrame: 2 };

// buildBinary compiles the game (client/ is go:embed'ed, so a fresh build is
// the only way to be sure the page under test is the source on disk).
export function buildBinary(outDir) {
    mkdirSync(outDir, { recursive: true });
    const bin = path.join(outDir, 'labyrinth');
    execFileSync('go', ['build', '-tags', 'example', '-o', bin, './examples/labyrinth'], { cwd: REPO_ROOT, stdio: 'inherit' });
    return bin;
}

// startServer launches one game server on a random loopback port and resolves
// once it prints its URL. The returned stop() kills exactly this PID.
export function startServer(bin, seed) {
    const proc = spawn(bin, ['-addr', '127.0.0.1:0', '-seed', String(seed)], { stdio: ['ignore', 'pipe', 'pipe'] });
    const log = [];
    return new Promise((resolve, reject) => {
        let buf = '';
        const timer = setTimeout(() => { proc.kill('SIGKILL'); reject(new Error(`server did not start: ${buf}`)); }, 15000);
        const onData = (d) => {
            const s = String(d);
            log.push(s);
            buf += s;
            const m = buf.match(/GAME\s+(http:\/\/\S+?)\/?\s+seed\s+(\d+)/);
            if (m) {
                clearTimeout(timer);
                resolve({
                    url: m[1].replace(/\/$/, '') + '/', seed: m[2], pid: proc.pid, log,
                    stop: () => new Promise((res) => {
                        if (proc.exitCode !== null) return res();
                        proc.once('exit', () => res());
                        proc.kill('SIGTERM');
                        setTimeout(() => { if (proc.exitCode === null) proc.kill('SIGKILL'); }, 3000);
                    }),
                });
            }
        };
        proc.stdout.on('data', onData);
        proc.stderr.on('data', onData);
        proc.once('exit', (code) => { clearTimeout(timer); reject(new Error(`server exited ${code}: ${buf}`)); });
    });
}

export async function launchBrowser({ headless = false } = {}) {
    return chromium.launch({
        headless,
        args: ['--use-angle=metal', '--enable-gpu', '--ignore-gpu-blocklist', '--autoplay-policy=no-user-gesture-required'],
    });
}

// Session records one journey. Timeline arrays grow while the page runs;
// detectors read them after close().
export class Session {
    constructor({ browser, server, outDir, name, headless }) {
        this.browser = browser;
        this.server = server;
        this.outDir = outDir;
        this.name = name;
        this.headless = headless;
        this.framesDir = path.join(outDir, 'frames');
        mkdirSync(this.framesDir, { recursive: true });
        this.timeline = {
            name, seed: server.seed, url: server.url, viewport: VIEWPORT,
            samples: [], dom: [], violations: [], rafGaps: [], pageErrors: [], wire: [],
            console: [], network: [], sockets: [], marks: [], frames: [], events: [],
            grid: null, renderer: null, loads: [],
        };
        this._eventTick = -1;
    }

    async open() {
        this.context = await this.browser.newContext({ viewport: VIEWPORT, deviceScaleFactor: 1 });
        await this.context.addInitScript(installProbe, { rafGapMs: 100, domEveryMs: 100 });
        if (this.headless) await this.context.addInitScript(pointerLockShim);
        this.page = await this.context.newPage();
        const tl = this.timeline;
        this.page.on('console', (m) => {
            const text = m.text();
            if (text.startsWith('[pe-violation] ')) {
                try { tl.violations.push(JSON.parse(text.slice(15))); } catch { tl.violations.push({ t: Date.now(), what: text }); }
                return;
            }
            tl.console.push({ t: Date.now(), type: m.type(), text, loc: m.location() });
        });
        this.page.on('pageerror', (e) => tl.pageErrors.push({ t: Date.now(), msg: String(e.stack || e) }));
        this.page.on('requestfailed', (r) => tl.network.push({ t: Date.now(), url: r.url(), failure: r.failure() && r.failure().errorText }));
        this.page.on('response', (r) => { if (r.status() >= 400) tl.network.push({ t: Date.now(), url: r.url(), status: r.status() }); });
        this.page.on('websocket', (ws) => {
            tl.sockets.push({ t: Date.now(), url: ws.url(), kind: 'open' });
            ws.on('close', () => tl.sockets.push({ t: Date.now(), url: ws.url(), kind: 'close' }));
            ws.on('socketerror', (err) => tl.sockets.push({ t: Date.now(), url: ws.url(), kind: 'error', err: String(err) }));
        });
        this.page.on('load', () => tl.loads.push(Date.now()));

        this.cdp = await this.context.newCDPSession(this.page);
        let n = 0;
        this.cdp.on('Page.screencastFrame', async (f) => {
            const file = path.join(this.framesDir, `${String(n++).padStart(6, '0')}.png`);
            writeFileSync(file, Buffer.from(f.data, 'base64'));
            tl.frames.push({ t: f.metadata.timestamp * 1000, file: path.relative(this.outDir, file) });
            try { await this.cdp.send('Page.screencastFrameAck', { sessionId: f.sessionId }); } catch { /* page closing */ }
        });

        await this.page.goto(this.server.url);
        await this.page.bringToFront();
        await this.cdp.send('Page.startScreencast', { format: 'png', ...CAST });
        this._drainTimer = setInterval(() => { this.drain().catch(() => {}); }, 250);
        this._eventTimer = setInterval(() => { this.pullEvents().catch(() => {}); }, 1000);
    }

    mark(kind, detail = {}) {
        this.timeline.marks.push({ t: Date.now(), kind, ...detail });
    }

    async drain() {
        if (!this.page || this.page.isClosed()) return;
        let d;
        try {
            d = await this.page.evaluate(() => (window.__pe ? window.__pe.drain() : null));
        } catch { return; } // mid-navigation; the next drain picks the new page up
        if (!d) return;
        const tl = this.timeline;
        tl.samples.push(...d.samples);
        tl.dom.push(...d.dom);
        // d.violations duplicates what the console listener already recorded.
        tl.rafGaps.push(...d.rafGaps);
        tl.wire.push(...d.wire);
        tl.pageErrors.push(...d.errors.map((e) => ({ t: e.t, msg: `${e.kind}: ${e.msg} ${e.src || ''}` })));
        if (d.grid && !tl.grid) tl.grid = d.grid;
        if (!tl.renderer) {
            try {
                tl.renderer = await this.page.evaluate(() => {
                    const c = document.getElementById('glcanvas');
                    const gl = c && (c.getContext('webgl2') || c.getContext('webgl'));
                    if (!gl) return 'no-webgl';
                    const e = gl.getExtension('WEBGL_debug_renderer_info');
                    return e ? gl.getParameter(e.UNMASKED_RENDERER_WEBGL) : gl.getParameter(gl.RENDERER);
                });
            } catch { /* retry next drain */ }
        }
    }

    // pullEvents copies the authoritative server event log incrementally (it
    // is a bounded rolling buffer, so it cannot be read once at the end).
    async pullEvents() {
        const q = this._eventTick >= 0 ? `?from=${this._eventTick + 1}` : '';
        const res = await fetch(`${this.server.url}api/playtest/events${q}`);
        if (!res.ok) return;
        const body = await res.json();
        const t = Date.now();
        for (const e of body.events || []) {
            this.timeline.events.push({ ...e, pulledAt: t });
            if (e.tick > this._eventTick) this._eventTick = e.tick;
        }
    }

    async close() {
        clearInterval(this._drainTimer);
        clearInterval(this._eventTimer);
        if (this.cdp) { try { await this.cdp.send('Page.stopScreencast'); } catch { /* closed */ } }
        await this.drain();
        try { await this.pullEvents(); } catch { /* server gone */ }
        if (this.context) await this.context.close().catch(() => {});
        this.timeline.frames.sort((a, b) => a.t - b.t);
        writeFileSync(path.join(this.outDir, 'timeline.json'), JSON.stringify(this.timeline));
        return this.timeline;
    }
}

// pointerLockShim is used ONLY in --headless mode, where Chromium cannot grant
// pointer lock. It emulates the browser permission (pointerLockElement,
// pointerlockchange, Escape to release) so the game's unmodified camera.mjs
// handlers run; look input then arrives as real CDP mouse moves whose
// movementX/Y Chromium computes. Headed runs use the real lock.
function pointerLockShim() {
    let locked = null;
    Object.defineProperty(Document.prototype, 'pointerLockElement', { configurable: true, get() { return locked; } });
    Element.prototype.requestPointerLock = function () {
        locked = this;
        queueMicrotask(() => document.dispatchEvent(new Event('pointerlockchange')));
        return Promise.resolve();
    };
    Document.prototype.exitPointerLock = function () {
        locked = null;
        queueMicrotask(() => document.dispatchEvent(new Event('pointerlockchange')));
    };
    window.addEventListener('keydown', (e) => { if (e.code === 'Escape' && locked) document.exitPointerLock(); }, true);
}
