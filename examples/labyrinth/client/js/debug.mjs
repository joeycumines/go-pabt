// DebugPanel: integrated PA-BT debugging GUI. Tab toggles visibility.
// Shows live PA-BT trees for all actors with interactive controls.
// Keyboard-operable without releasing pointer lock; game never pauses.

export class DebugPanel {
    constructor() {
        this.el = document.getElementById('debug-panel');
        this.actorListEl = document.getElementById('debug-actor-list');
        this.treeEl = document.getElementById('debug-tree');
        this.controlsEl = document.getElementById('debug-controls');

        this.visible = false;
        this.frames = new Map(); // actorID -> frame
        this.frameTimes = new Map(); // actorID -> last frame arrival (ms)
        this.selectedActor = null;
        this.onCommand = null; // callback(cmd, arg)
        this.getInjectPos = null; // callback() -> [x, y] for inject command

        // Tab toggles the panel. This works even during pointer lock.
        document.addEventListener('keydown', (e) => {
            if (e.code === 'Tab') {
                e.preventDefault();
                this.toggle();
            }
            if (!this.visible) return;
            switch (e.code) {
                case 'KeyR': // force replan
                    this.sendCommand('replan');
                    break;
                case 'KeyP': // pause
                    this.sendCommand('pause');
                    break;
                case 'KeyU': // resume
                    this.sendCommand('resume');
                    break;
                case 'KeyI': // inject state override
                    this.sendInject();
                    break;
                case 'ArrowUp':
                    e.preventDefault();
                    this.selectPrev();
                    break;
                case 'ArrowDown':
                    e.preventDefault();
                    this.selectNext();
                    break;
            }
        });
    }

    toggle() {
        this.visible = !this.visible;
        this.el.hidden = !this.visible;
        if (this.visible) this.render();
    }

    // handleTreeFrame is called by net.mjs when a tree frame arrives.
    handleTreeFrame(actorID, frame) {
        this.frames.set(actorID, frame);
        this.frameTimes.set(actorID, Date.now());
        this.pruneStale();
        if (this.visible) this.render();
    }

    // pruneStale drops actors that stopped broadcasting (removed from the
    // server's world). Live actors - including paused ones - keep sending
    // frames every broadcast cycle, so 10s without an update means gone.
    pruneStale() {
        const now = Date.now();
        for (const [id, t] of this.frameTimes) {
            if (now - t > 10000) {
                this.frameTimes.delete(id);
                this.frames.delete(id);
                if (this.selectedActor === id) this.selectedActor = null;
            }
        }
    }

    selectPrev() {
        const ids = Array.from(this.frames.keys());
        if (ids.length === 0) return;
        const idx = ids.indexOf(this.selectedActor);
        const newIdx = idx <= 0 ? ids.length - 1 : idx - 1;
        this.selectedActor = ids[newIdx];
        this.render();
    }

    selectNext() {
        const ids = Array.from(this.frames.keys());
        if (ids.length === 0) return;
        const idx = ids.indexOf(this.selectedActor);
        const newIdx = idx >= ids.length - 1 ? 0 : idx + 1;
        this.selectedActor = ids[newIdx];
        this.render();
    }

    sendCommand(cmd) {
        if (!this.onCommand || !this.selectedActor) return;
        this.onCommand(cmd, this.selectedActor);
    }

    sendInject() {
        if (!this.onCommand || !this.selectedActor || !this.getInjectPos) return;
        const [x, y] = this.getInjectPos();
        this.onCommand('inject', this.selectedActor + ' ' + x.toFixed(1) + ',' + y.toFixed(1));
    }

    render() {
        if (!this.visible) return;
        this.renderActorList();
        this.renderTree();
        this.renderControls();
    }

    renderActorList() {
        if (!this.actorListEl) return;
        this.actorListEl.innerHTML = '';
        for (const [id, frame] of this.frames) {
            const div = document.createElement('div');
            div.className = 'debug-actor' + (id === this.selectedActor ? ' selected' : '');
            div.textContent = id;
            div.onclick = () => { this.selectedActor = id; this.render(); };
            this.actorListEl.appendChild(div);
        }
    }

    renderTree() {
        if (!this.treeEl) return;
        if (!this.selectedActor || !this.frames.has(this.selectedActor)) {
            this.treeEl.innerHTML = '<div class="debug-empty">Select an actor</div>';
            return;
        }
        const frame = this.frames.get(this.selectedActor);
        let html = `<div class="debug-header">${this.selectedActor} (rev ${frame.planRevision})</div>`;

        // World View: elemental state, detection, advantage
        if (frame.worldView) {
            const wv = frame.worldView;
            const ELEM = ['Fire', 'Ice', 'Lightning', 'Void'];
            const advClass = wv.atDisadvantage ? 'debug-disadv' : wv.atAdvantage ? 'debug-advant' : 'debug-neutral';
            html += `<div class="debug-section">World View</div>`;
            html += `<div class="debug-wv">Self: <span class="${advClass}">${ELEM[wv.selfElement] || wv.selfElement}</span>`;
            html += ` | Player: ${ELEM[wv.playerElement] || wv.playerElement}</div>`;
            html += `<div class="debug-wv ${advClass}">${wv.atDisadvantage ? 'AT DISADVANTAGE' : wv.atAdvantage ? 'AT ADVANTAGE' : 'NEUTRAL'}</div>`;
            html += `<div class="debug-wv">Detected: ${wv.playerDetected} | Stunned: ${wv.stunned} | Band: ${wv.distanceBand}</div>`;
            if (wv.hasLastKnown) {
                html += `<div class="debug-wv">Focus: (${wv.lastKnownX.toFixed(1)}, ${wv.lastKnownY.toFixed(1)})</div>`;
            }
        }

        // Nodes
        html += '<div class="debug-section">Nodes</div>';
        for (const n of frame.nodes || []) {
            const status = n.lastStatus === 1 ? 'RUN' : n.lastStatus === 2 ? 'OK' : n.lastStatus === 3 ? 'FAIL' : '--';
            html += `<div class="debug-node"><span class="debug-type">${n.type}</span> <span class="debug-status">${status}</span> ticks:${n.tickCount}</div>`;
        }

        // Conditions
        if (frame.conditions && frame.conditions.length) {
            html += '<div class="debug-section">Conditions</div>';
            for (const c of frame.conditions) {
                const truth = c.truthKnown ? (c.truth ? 'TRUE' : 'FALSE') : '?';
                html += `<div class="debug-cond">${c.key}: ${truth}</div>`;
            }
        }

        // Actions
        if (frame.actions && frame.actions.length) {
            html += '<div class="debug-section">Actions</div>';
            for (const a of frame.actions) {
                html += `<div class="debug-action">${a.name || a.id} (post:${a.postconditionId})</div>`;
            }
        }

        // Executions
        if (frame.executions && frame.executions.length) {
            html += '<div class="debug-section">Executions</div>';
            for (const ex of frame.executions) {
                const st = ex.statusPresent ? (ex.status === 2 ? 'OK' : ex.status === 3 ? 'FAIL' : 'RUN') : '--';
                html += `<div class="debug-exec">${ex.actionId}: ${st}`;
                if (ex.relations && ex.relations.length) {
                    for (const r of ex.relations) {
                        html += ` <span class="debug-rel">[${r.result}${r.authoritative ? '*' : ''}]</span>`;
                    }
                }
                html += '</div>';
            }
        }

        // Path
        if (frame.path) {
            html += `<div class="debug-path">Path: ${frame.path}</div>`;
        }

        this.treeEl.innerHTML = html;
    }

    renderControls() {
        if (!this.controlsEl) return;
        this.controlsEl.innerHTML = '';
        if (!this.selectedActor) return;

        const mkBtn = (label, cmd) => {
            const btn = document.createElement('button');
            btn.textContent = label;
            btn.onclick = () => this.sendCommand(cmd);
            return btn;
        };
        this.controlsEl.appendChild(mkBtn('Replan [R]', 'replan'));
        this.controlsEl.appendChild(mkBtn('Pause [P]', 'pause'));
        this.controlsEl.appendChild(mkBtn('Resume [U]', 'resume'));
        const injectBtn = document.createElement('button');
        injectBtn.textContent = 'Inject [I]';
        injectBtn.onclick = () => this.sendInject();
        this.controlsEl.appendChild(injectBtn);
    }
}
