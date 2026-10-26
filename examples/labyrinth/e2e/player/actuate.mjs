// actuate.mjs - the ONLY way a player-eye journey touches the game: real
// Playwright keyboard and mouse input, dispatched by Chromium exactly as a
// person's would be. Every action is marked on the session timeline so the
// detectors can line input up with what the screen and the server did.
//
// Mouse look relies on pointer lock: under lock Chromium reports
// movementX/Y = the delta between successive mouse positions, which the
// game's camera.mjs turns into yaw/pitch. Playwright tracks the virtual
// cursor, so look(dx) is a move from the current virtual position by dx.

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

export class Actuator {
    constructor(session) {
        this.s = session;
        this.page = session.page;
        this.mx = session.timeline.viewport.width / 2;
        this.my = session.timeline.viewport.height / 2;
        this.held = new Set();
    }

    async down(key) {
        if (this.held.has(key)) return;
        this.held.add(key);
        this.s.mark('keydown', { key });
        await this.page.keyboard.down(key);
    }

    async up(key) {
        if (!this.held.has(key)) return;
        this.held.delete(key);
        this.s.mark('keyup', { key });
        await this.page.keyboard.up(key);
    }

    async releaseAll() {
        for (const k of [...this.held]) await this.up(k);
    }

    async hold(keys, ms) {
        const ks = Array.isArray(keys) ? keys : [keys];
        this.s.mark('hold', { keys: ks, ms });
        for (const k of ks) await this.down(k);
        await sleep(ms);
        for (const k of ks) await this.up(k);
    }

    async tap(key) {
        this.s.mark('tap', { key });
        await this.page.keyboard.press(key);
    }

    // look turns the view by (dx, dy) mouse pixels, split into small steps the
    // way a hand moves a mouse. Positive dx = mouse moved right.
    async look(dx, dy = 0, { steps = Math.max(1, Math.ceil(Math.hypot(dx, dy) / 25)), stepMs = 8 } = {}) {
        this.s.mark('look', { dx, dy });
        for (let i = 1; i <= steps; i++) {
            const nx = this.mx + dx / steps, ny = this.my + dy / steps;
            await this.page.mouse.move(nx, ny);
            this.mx = nx; this.my = ny;
            if (stepMs) await sleep(stepMs);
        }
    }

    // click presses the left button where the cursor already is (moving it
    // first would turn the camera while locked).
    async click() {
        this.s.mark('click');
        await this.page.mouse.down();
        await sleep(40);
        await this.page.mouse.up();
    }

    // clickAt moves the (unlocked) cursor to a page position and clicks - used
    // for menus and overlays, never while the pointer is locked.
    async clickAt(x, y) {
        this.s.mark('clickAt', { x, y });
        await this.page.mouse.move(x, y);
        this.mx = x; this.my = y;
        await this.page.mouse.down();
        await sleep(40);
        await this.page.mouse.up();
    }

    async wait(ms, why = '') {
        if (why) this.s.mark('wait', { ms, why });
        await sleep(ms);
    }
}
