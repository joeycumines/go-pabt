// Keyboard input state for WASD + sprint + jump.

export class Input {
    constructor(canvas) {
        this.forward = 0;  // -1..1 (S..W)
        this.strafe = 0;   // -1..1 (A..D)
        this.sprint = false;
        this.jump = false;
        this._jumpConsumed = false;
        this.fire = false; // edge-triggered: set by click, consumed by main loop

        this._keys = {};
        window.addEventListener('keydown', (e) => {
            this._keys[e.code] = true;
            if (e.code === 'Space' && !this._jumpConsumed) {
                this.jump = true;
                this._jumpConsumed = true;
            }
        });
        window.addEventListener('keyup', (e) => {
            this._keys[e.code] = false;
            if (e.code === 'Space') {
                this._jumpConsumed = false;
            }
        });

        // Click-to-fire: set edge flag, consumed each frame by main loop.
        if (canvas) {
            canvas.addEventListener('mousedown', (e) => {
                if (e.button === 0) this.fire = true;
            });
        }

        // Losing focus or the pointer lock means the browser will never
        // deliver the matching keyup: without this reset the player keeps
        // walking (or sprinting) after Alt-Tab, Escape or a hidden tab.
        window.addEventListener('blur', () => this.clearAll());
        document.addEventListener('visibilitychange', () => {
            if (document.hidden) this.clearAll();
        });
        document.addEventListener('pointerlockchange', () => {
            if (!document.pointerLockElement) this.clearAll();
        });
    }

    // clearAll drops every held key and the movement state derived from it.
    // Safe to call repeatedly; pointer-lock changes and tab switches overlap.
    clearAll() {
        this._keys = {};
        this.forward = 0;
        this.strafe = 0;
        this.sprint = false;
        this.jump = false;
        this._jumpConsumed = false;
        this.fire = false;
    }

    update() {
        this.forward = (this._keys['KeyW'] ? 1 : 0) - (this._keys['KeyS'] ? 1 : 0);
        this.strafe  = (this._keys['KeyD'] ? 1 : 0) - (this._keys['KeyA'] ? 1 : 0);
        this.sprint  = !!this._keys['ShiftLeft'] || !!this._keys['ShiftRight'];
    }

    consumeJump() {
        const j = this.jump;
        this.jump = false;
        return j;
    }
}
