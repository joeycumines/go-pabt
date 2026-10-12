// BTHud: compact in-game HUD showing live PA-BT tick activity per actor.
// Visible during normal pointer-locked play (unlike the Tab debug panel),
// toggled with KeyB, DOM updates throttled to 5Hz.

const MAX_ROWS = 12;
const UPDATE_INTERVAL_MS = 200; // 5Hz
const ELEMENT_COLORS = ['#ff5920', '#59bfff', '#fff240', '#8c33d9'];
const ELEMENT_ABBR = ['F', 'I', 'L', 'V'];
const STATUS_ABBR = { Running: 'RUN', Success: 'OK', Failure: 'FAIL', 'zero/never-ticked': '-' };
// bt.Status wire values (brain/encode.go): 0=never-ticked 1=Running 2=Success 3=Failure.
const STATUS_BY_INT = { 0: 'zero/never-ticked', 1: 'Running', 2: 'Success', 3: 'Failure' };

export class BTHud {
    constructor() {
        this.el = document.getElementById('bt-hud');
        this.rowsEl = document.getElementById('bt-hud-rows');
        this.visible = false;
        this.rows = new Map(); // actorID -> row state
        this.frames = new Map(); // actorID -> last frame
        this._lastUpdate = 0;
        this._lastFrameAt = new Map(); // actorID -> arrival time (ms)
        this._renderedRows = []; // rows actually painted in the last render()

        // KeyB toggles the HUD. Works during pointer lock.
        document.addEventListener('keydown', (e) => {
            if (e.code === 'KeyB') this.toggle();
        });
    }

    toggle() {
        this.visible = !this.visible;
        this.el.hidden = !this.visible;
        if (this.visible) this.render(true);
    }

    // handleTreeFrame shares the net.onTree fan-out with DebugPanel and the
    // S5 threads. Data path: one tree frame per actor every 10 server ticks.
    handleTreeFrame(actorID, frame) {
        const prev = this.rows.get(actorID);
        const ticks = this._extractTickCount(frame);
        const now = Date.now();
        const row = {
            id: actorID,
            element: frame.worldView ? frame.worldView.selfElement : (prev ? prev.element : 0),
            ticks,
            delta: prev ? Math.max(0, ticks - prev.ticks) : 0,
            active: this._activeNode(frame),
            status: this._lastStatus(frame),
            advantage: frame.worldView
                ? (frame.worldView.atAdvantage ? 'ADV' : frame.worldView.atDisadvantage ? 'DIS' : 'NEU')
                : 'NEU',
            sinceLast: prev ? Math.min(9999, prev.sinceLast + (now - (this._lastFrameAt.get(actorID) || now))) : 0,
            // H3: Planning process visibility.
            failedCondition: frame.failedCondition || null,
            candidates: frame.candidates || [],
        };
        this.rows.set(actorID, row);
        this.frames.set(actorID, frame);
        this._lastFrameAt.set(actorID, now);
        this._pruneStale();
        if (this.visible) this.render();
    }

    // _extractTickCount sums tick counts across the frame's nodes so the HUD
    // reports planner activity (how many node executions the server ticked).
    _extractTickCount(frame) {
        if (!frame) return 0;
        if (typeof frame.tickCount === 'number') return frame.tickCount;
        if (Array.isArray(frame.nodes)) {
            let sum = 0;
            for (const n of frame.nodes) {
                if (n && typeof n.tickCount === 'number') sum += n.tickCount;
            }
            return sum;
        }
        return 0;
    }

    _activeNode(frame) {
        if (!frame) return '';
        // Map action IDs to names so the active cell reads like a behavior.
        const nameById = new Map();
        if (Array.isArray(frame.actions)) {
            for (const a of frame.actions) if (a && a.id) nameById.set(a.id, a.name || a.id);
        }
        // Prefer the currently-executing action (last execution row).
        if (Array.isArray(frame.executions) && frame.executions.length > 0) {
            const exec = frame.executions[frame.executions.length - 1];
            if (exec && exec.actionId) return nameById.get(exec.actionId) || exec.actionId;
        }
        // Fall back to the leaf of the planner path.
        if (typeof frame.path === 'string' && frame.path) {
            const parts = frame.path.split(/→|\//).map(s => s.trim()).filter(Boolean);
            if (parts.length) return parts[parts.length - 1];
        }
        return '';
    }

    _lastStatus(frame) {
        if (!frame) return 'zero/never-ticked';
        // Per-action execution status first (the meaningful "last" outcome).
        if (Array.isArray(frame.executions) && frame.executions.length > 0) {
            const exec = frame.executions[frame.executions.length - 1];
            if (exec && exec.statusPresent) {
                return STATUS_BY_INT[exec.status] || ('status:' + exec.status);
            }
        }
        // Otherwise the most-recently-ticked node's last status.
        if (Array.isArray(frame.nodes) && frame.nodes.length > 0) {
            let best = frame.nodes[0];
            for (const n of frame.nodes) {
                if (n && n.tickCount > best.tickCount) best = n;
            }
            if (best.statusPresent) {
                return STATUS_BY_INT[best.lastStatus] || ('status:' + best.lastStatus);
            }
        }
        return 'zero/never-ticked';
    }

    // pruneStale drops actors that stopped broadcasting frames (10s without
    // an update means the actor left the world or the server stopped).
    _pruneStale() {
        const now = Date.now();
        for (const [id, t] of this._lastFrameAt) {
            if (now - t > 10000) {
                this._lastFrameAt.delete(id);
                this.rows.delete(id);
                this.frames.delete(id);
            }
        }
    }

    // pruneToRoster drops rows whose actor is no longer in the live roster.
    // Called from the snapshot handler each update: a dead actor's last
    // frame is still re-broadcast until the server drops it, so without
    // this the HUD would show frozen rows for up to 10 seconds after death.
    pruneToRoster(liveIds) {
        if (!Array.isArray(liveIds)) return;
        const live = new Set(liveIds);
        for (const id of this.rows.keys()) {
            if (!live.has(id)) {
                this.rows.delete(id);
                this.frames.delete(id);
                this._lastFrameAt.delete(id);
            }
        }
        if (this.visible) this.render(true);
    }

    // render paints the rows. Throttled to 5Hz so DOM churn never eats the
    // frame budget while the HUD sits on screen during play.
    render(force = false) {
        const now = Date.now();
        if (!force && now - this._lastUpdate < UPDATE_INTERVAL_MS) return;
        this._lastUpdate = now;

        const rows = Array.from(this.rows.values()).slice(0, MAX_ROWS);
        this._renderedRows = rows;
        const parts = [];
        for (const r of rows) {
            const color = ELEMENT_COLORS[r.element] || '#ffffff';
            const abbr = ELEMENT_ABBR[r.element] || '?';
            const badge = r.advantage === 'ADV' ? 'adv'
                : r.advantage === 'DIS' ? 'dis' : 'neu';
            const badgeClass = r.advantage === 'ADV' ? 'bt-badge-adv'
                : r.advantage === 'DIS' ? 'bt-badge-dis' : 'bt-badge-neu';
            let inner = `<div class="bt-row">` +
                `<span class="bt-elem" style="border-color:${color};color:${color}">${abbr}</span>` +
                `<span class="bt-id">${r.id}</span>` +
                `<span class="bt-ticks">t${r.ticks}</span>` +
                `<span class="bt-delta">+${r.delta}</span>` +
                `<span class="bt-active">${r.active || '-'}</span>` +
                `<span class="bt-status">${STATUS_ABBR[r.status] || r.status}</span>` +
                `<span class="bt-badge ${badgeClass}">${badge}</span>` +
                `</div>`;
            // H3: Show planning process data if available.
            if (r.failedCondition && r.candidates.length > 0) {
                const candStr = r.candidates.slice(0, 3).join(', ');
                const more = r.candidates.length > 3 ? ` +${r.candidates.length - 3}` : '';
                inner += `<div class="bt-plan-row" style="font-size:0.55rem;color:#a0a0c0;padding-left:20px;margin-top:-2px;margin-bottom:4px;">↳ ${r.failedCondition}: ${candStr}${more}</div>`;
            }
            // Wrap in a single container div so DOM children count equals
            // rows.length (the plan-row sub-div would otherwise inflate
            // the count and break the probe<->DOM assertion).
            parts.push(`<div class="bt-actor">${inner}</div>`);
        }
        this.rowsEl.innerHTML = parts.join('');
    }

    // probe exposes the HUD state for verification (__ariadne.btHud).
    // Returns only the rows that were actually painted in the last render()
    // call, so the probe count always matches the DOM children count even
    // when render() is throttled and new rows arrived since the last paint.
    probe() {
        const rendered = this._renderedRows || [];
        return {
            visible: this.visible,
            rows: rendered.map(r => ({
                id: r.id, element: r.element, ticks: r.ticks, delta: r.delta,
                active: r.active, status: r.status, advantage: r.advantage,
            })),
        };
    }
}
