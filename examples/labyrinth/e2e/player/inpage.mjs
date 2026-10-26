// inpage.mjs - the read-only probe installed into the game page before any
// game script runs (page.addInitScript). It does three things:
//
//  1. Records one state sample per presented animation frame (wrapping
//     requestAnimationFrame) and a DOM probe every ~100ms: what the HUD shows,
//     which element is actually on top at key points, whether damage cues are
//     visible. Timestamps are epoch ms so they join with Node-side input marks
//     and screencast frames.
//  2. Guards window.__ariadne: the game reassigns it every update(); a setter
//     wraps each assignment so any call to a debug ACTION (teleport, setYaw,
//     fire, bots.*, playtest.*, inspect.*, setOverlays, toggleBTHud) is
//     recorded as a violation. Player-eye runs must fail on any violation:
//     those calls bypass the input path a real player uses.
//  3. Records rAF stalls and uncaught errors.
//
// The function is serialised by Playwright, so it must be self-contained.

export function installProbe(opts) {
    const FORBIDDEN = new Set(['fire', 'setYaw', 'teleport', 'setOverlays', 'toggleBTHud']);
    const FORBIDDEN_NS = new Set(['inspect', 'bots', 'playtest']);
    const now = () => performance.timeOrigin + performance.now();
    // The unguarded game object stays in this closure: nothing outside the
    // probe can reach its debug actions except through the guard.
    let raw = null;
    const pe = {
        samples: [], dom: [], violations: [], rafGaps: [], errors: [],
    };
    Object.defineProperty(window, '__pe', { value: pe, enumerable: false });

    // Violations are also logged synchronously so the harness's console
    // listener sees them even if the page navigates before the next drain.
    const violation = (what) => {
        const v = { t: now(), what, stack: String(new Error().stack).split('\n').slice(2, 6).join(' | ') };
        pe.violations.push(v);
        console.debug(`[pe-violation] ${JSON.stringify(v)}`);
    };
    const guardNs = (ns, obj) => new Proxy(obj, {
        get(target, prop, recv) {
            const v = Reflect.get(target, prop, recv);
            if (typeof v === 'function') {
                return function (...args) { violation(`__ariadne.${ns}.${String(prop)}()`); return v.apply(target, args); };
            }
            return v;
        },
    });
    let guarded = null;
    Object.defineProperty(window, '__ariadne', {
        configurable: false,
        get() { return guarded; },
        set(v) {
            raw = v;
            guarded = v && new Proxy(v, {
                get(target, prop) {
                    const val = Reflect.get(target, prop, target);
                    if (FORBIDDEN.has(prop) && typeof val === 'function') {
                        return function (...args) { violation(`__ariadne.${String(prop)}()`); return val.apply(target, args); };
                    }
                    if (FORBIDDEN_NS.has(prop) && val && typeof val === 'object') return guardNs(String(prop), val);
                    return val;
                },
            });
        },
    });

    // Wire tap: record what the SERVER sent (snapshots + events), parsed
    // independently of the client's own net/render code. This is the
    // authoritative channel the render-vs-truth detector compares against.
    // A subclass keeps WebSocket's statics (OPEN, ...) and instanceof intact.
    pe.wire = [];
    const OrigWS = window.WebSocket;
    window.WebSocket = class TapWebSocket extends OrigWS {
        constructor(...args) {
            super(...args);
            this.addEventListener('message', (e) => {
                const t = now();
                try {
                    const env = JSON.parse(e.data);
                    if (env.t === 'snap' && env.p) {
                        pe.wire.push({
                            t, kind: 'snap', round: env.p.round,
                            runner: env.p.runner ? [env.p.runner.x, env.p.runner.y, env.p.runner.hp] : null,
                            m: (env.p.minotaurs || []).map((m) => [m.id, m.x, m.y, m.hp, m.stunned ? 1 : 0]),
                        });
                    } else if (env.t === 'event' && env.p) {
                        pe.wire.push({ t, kind: 'event', type: env.p.type, data: env.p.data });
                    }
                } catch { /* non-JSON frames are not ours to judge */ }
            });
        }
    };

    window.addEventListener('error', (e) => pe.errors.push({ t: now(), kind: 'error', msg: String(e.message), src: `${e.filename}:${e.lineno}` }));
    window.addEventListener('unhandledrejection', (e) => pe.errors.push({ t: now(), kind: 'unhandledrejection', msg: String(e.reason && (e.reason.stack || e.reason)) }));

    const visible = (el) => {
        if (!el || el.hidden) return false;
        const cs = getComputedStyle(el);
        if (cs.display === 'none' || cs.visibility === 'hidden' || Number(cs.opacity) === 0) return false;
        const r = el.getBoundingClientRect();
        return r.width > 0 && r.height > 0;
    };
    const rect = (el) => { const r = el.getBoundingClientRect(); return [r.x, r.y, r.width, r.height]; };
    const MODALS = ['tutorial-overlay', 'pointer-lock-prompt', 'overlay', 'loading', 'debug-panel'];
    const HUD = ['minimap', 'status-chips', 'hp-bar', 'intensity-bar', 'kill-feed', 'crosshair', 'credits', 'wave-banner', 'hit-marker', 'bt-hud'];
    const topAt = (x, y) => {
        const el = document.elementFromPoint(x, y);
        if (!el) return null;
        return el.id || (el.closest('[id]') ? el.closest('[id]').id + '>' + el.tagName.toLowerCase() : el.tagName.toLowerCase());
    };

    let lastDom = 0;
    const domProbe = (t) => {
        const W = innerWidth, H = innerHeight;
        const modals = {};
        for (const id of MODALS) {
            const el = document.getElementById(id);
            if (el && visible(el)) modals[id] = { rect: rect(el), text: (el.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 120), opacity: Number(getComputedStyle(el).opacity) };
        }
        const hud = {};
        for (const id of HUD) {
            const el = document.getElementById(id);
            if (el && visible(el)) hud[id] = { rect: rect(el), text: (el.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 160) };
        }
        const chips = {};
        for (const el of document.querySelectorAll('#status-chips > *')) {
            if (el.id && visible(el)) chips[el.id] = { rect: rect(el), text: (el.textContent || '').trim() };
        }
        const hpBar = document.getElementById('hp-bar'), hpFill = document.getElementById('hp-fill');
        const hpFrac = hpBar && hpFill && visible(hpBar)
            ? hpFill.getBoundingClientRect().width / Math.max(1, hpBar.clientWidth) : null;
        const ov = modals.overlay;
        pe.dom.push({
            t, W, H, modals, hud, chips, hpFrac,
            topCenter: topAt(W / 2, H / 2),
            topOverlay: ov ? topAt(ov.rect[0] + ov.rect[2] / 2, ov.rect[1] + ov.rect[3] / 2) : null,
            killFeed: Array.from(document.querySelectorAll('#kill-feed > *')).map((e) => (e.textContent || '').trim()).slice(0, 8),
        });
    };

    // read returns plain data only (no methods): what the pilot is allowed
    // to know - its own position/heading/HP, the maze (= the minimap) and the
    // enemies it could locate on screen or minimap.
    pe.read = () => {
        const a = raw;
        if (!a) return null;
        return {
            gs: a.gameState, pos: a.playerPos, yaw: a.playerYaw, hp: a.hp, round: a.round,
            locked: !!document.pointerLockElement,
            actors: a.actors.map((x) => ({ id: x.id, x: x.pos[0], y: x.pos[1], z: x.pos[2], hp: x.hp, st: x.stunned, el: x.element })),
            grid: a.mazeGrid ? { w: a.mazeSize[0], h: a.mazeSize[1], cells: a.mazeGrid, gate: a.gate } : null,
        };
    };

    const sample = (t) => {
        const a = raw;
        const s = { t, locked: !!document.pointerLockElement, focus: document.hasFocus(), vis: document.visibilityState };
        if (a) {
            try {
                s.gs = a.gameState; s.pos = a.playerPos; s.spos = a.serverPlayerPos; s.yaw = a.playerYaw;
                s.hp = a.hp; s.round = a.round; s.intensity = a.intensity; s.ws = a.wsOpen;
                s.vel = a.playerVel; s.input = a.inputState; s.elem = a.playerElement; s.fov = a.cameraFov;
                s.aspect = innerWidth / Math.max(1, innerHeight);
                s.actors = a.actors.map((x) => ({ id: x.id, x: x.pos[0], y: x.pos[1], z: x.pos[2], hp: x.hp, type: x.type, el: x.element, st: x.stunned }));
                if (!pe.grid && a.mazeGrid) { pe.grid = { w: a.mazeSize[0], h: a.mazeSize[1], cells: a.mazeGrid, gate: a.gate }; }
            } catch (e) { s.err = String(e); }
        }
        const ch = document.getElementById('crosshair');
        s.aim = !!(ch && ch.classList.contains('aim-locked'));
        s.flash = visible(document.getElementById('hit-flash'));
        s.marker = visible(document.getElementById('hit-marker'));
        s.shake = typeof window.screenShakeTimer === 'number' ? window.screenShakeTimer > 0 : null;
        pe.samples.push(s);
    };

    let lastFrameTs = -1, lastWall = 0;
    const origRaf = window.requestAnimationFrame.bind(window);
    window.requestAnimationFrame = (cb) => origRaf((ts) => {
        cb(ts);
        if (ts === lastFrameTs) return;
        lastFrameTs = ts;
        const t = now();
        if (lastWall && t - lastWall > opts.rafGapMs && document.visibilityState === 'visible') {
            pe.rafGaps.push({ t, gap: t - lastWall });
        }
        lastWall = t;
        sample(t);
        if (t - lastDom >= opts.domEveryMs) { lastDom = t; domProbe(t); }
    });
    // The DOM can show things before the render loop starts (tutorial,
    // loading screen); probe on a timer until the first frame arrives.
    const early = setInterval(() => {
        if (lastWall) { clearInterval(early); return; }
        const t = now();
        domProbe(t);
        pe.samples.push({ t, early: true, focus: document.hasFocus(), vis: document.visibilityState, locked: !!document.pointerLockElement });
    }, opts.domEveryMs);

    pe.drain = () => {
        const out = {
            samples: pe.samples.splice(0), dom: pe.dom.splice(0),
            violations: pe.violations.splice(0), rafGaps: pe.rafGaps.splice(0), errors: pe.errors.splice(0),
            wire: pe.wire.splice(0),
            grid: pe.grid || null,
        };
        return out;
    };
}
