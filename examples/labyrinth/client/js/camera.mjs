// Perspective camera with pointer-lock mouse look.

export class Camera {
    constructor(canvas) {
        this.canvas = canvas;
        this.pos = [1.5, 0.5, 1.5]; // x, y (height), z
        this.yaw = 0;   // radians, 0 = +Z
        this.pitch = 0;  // radians, clamped to ~+/-89 degrees (the player's raw aim)

        // H1 movement juice. These fields are visual-only and never sent to
        // the server: the authoritative aim pitch is `this.pitch` above.
        this.baseFovDeg = 70;
        this.currentFovDeg = 70;
        this.targetFovDeg = 70;
        this.recoilPitch = 0;   // radians, added to pitch for rendering only
        this.bobPhase = 0;
        this.bobOffsetX = 0;    // weapon lateral sway (camera-right)
        this.bobOffsetY = 0;    // weapon vertical bounce

        this._locked = false;
        canvas.addEventListener('click', () => {
            if (!this._locked) canvas.requestPointerLock();
        });
        document.addEventListener('pointerlockchange', () => {
            this._locked = document.pointerLockElement === canvas;
        });
        document.addEventListener('mousemove', (e) => {
            if (!this._locked) return;
            this.yaw   += e.movementX * 0.002;
            this.pitch -= e.movementY * 0.002;
            const limit = Math.PI * 0.49;
            this.pitch = Math.max(-limit, Math.min(limit, this.pitch));
        });
    }

    get locked() { return this._locked; }

    // applyRecoil kicks the view upward for a "shoot feel" effect. The
    // player never sends the resulting pitch to the server, so this is purely
    // visual. Uses a pure displacement (no velocity kick) so the recovery
    // spring never overshoots past the initial kick value.
    applyRecoil(strength = 0.02) {
        this.recoilPitch += strength * 2.5;
    }

    // updateRecoil decays the recoil spring. Called from frame() regardless
    // of gameState so the visual effect always completes, even on the
    // death/win overlay. Separated from updateMovement (which is gated on
    // gameState==='playing') to prevent the spring from freezing mid-decay.
    updateRecoil(dt) {
        // Fast exponential decay: ~99% gone within 1 second.
        this.recoilPitch *= Math.pow(0.01, dt);
        if (Math.abs(this.recoilPitch) < 0.0001) {
            this.recoilPitch = 0;
        }
    }

    // kickFov briefly narrows the FOV for a snap-fire feel.
    kickFov(delta = 3) {
        this.currentFovDeg -= delta;
    }

    // updateMovement advances the visual juice: sprint FOV widen, recoil
    // spring recovery, weapon bob. Call once per frame before viewMatrix().
    //
    // sprinting: the player's sprint INTENT (input.sprint). FOV widens when
    // the player holds sprint, regardless of whether they are wall-blocked —
    // this matches FPS game-feel convention (FOV signals "you are in sprint
    // mode", not "you are currently displacing at sprint speed"). It also
    // avoids the review-03 diagonal-walking bug where inputMag*walkSpeed
    // exceeded the sprint threshold.
    //
    // speed: the player's ACTUAL velocity magnitude (post-collision). Bob
    // responds to real motion so the weapon doesn't swing while the player
    // is wedged against a wall (review-04 "bobbing while blocked by a wall").
    updateMovement(dt, sprinting, speed) {
        this.targetFovDeg = sprinting ? this.baseFovDeg + 10 : this.baseFovDeg;
        this.currentFovDeg += (this.targetFovDeg - this.currentFovDeg) * Math.min(1, 4 * dt);
        if (speed > 0.1) {
            const bobFreq = sprinting ? 14 : 8;
            const bobAmp = sprinting ? 0.05 : 0.03;
            this.bobPhase += dt * bobFreq;
            this.bobOffsetX = Math.sin(this.bobPhase) * bobAmp;
            this.bobOffsetY = Math.abs(Math.cos(this.bobPhase)) * bobAmp * 0.8;
        } else {
            this.bobPhase *= 1 - Math.min(1, 6 * dt);
            this.bobOffsetX *= 1 - Math.min(1, 6 * dt);
            this.bobOffsetY *= 1 - Math.min(1, 6 * dt);
        }
    }

    // aimDir is the authoritative aim direction the SERVER receives (raw
    // pitch). renderDir is what the camera renders with (raw pitch + recoil).
    // Use aimDir for anything that affects game logic or authoritative state.
    aimDir() {
        const p = this.pitch;
        return [
            Math.sin(this.yaw) * Math.cos(p),
            Math.sin(p),
            Math.cos(this.yaw) * Math.cos(p),
        ];
    }

    renderDir() {
        const p = this.pitch + this.recoilPitch;
        return [
            Math.sin(this.yaw) * Math.cos(p),
            Math.sin(p),
            Math.cos(this.yaw) * Math.cos(p),
        ];
    }

    forward() {
        return this.renderDir();
    }

    right() {
        return [-Math.cos(this.yaw), 0, Math.sin(this.yaw)];
    }

    viewMatrix() {
        const [ex,ey,ez] = this.pos;
        const [fx,fy,fz] = this.forward();
        const tx = ex+fx, ty = ey+fy, tz = ez+fz;
        return lookAt(ex,ey,ez, tx,ty,tz, 0,1,0);
    }

    projMatrix(aspect, near = 0.1, far = 100) {
        return perspective(this.currentFovDeg * Math.PI / 180, aspect, near, far);
    }
}

function lookAt(ex,ey,ez, tx,ty,tz, ux,uy,uz) {
    let zx=ex-tx, zy=ey-ty, zz=ez-tz;
    let l=Math.hypot(zx,zy,zz); zx/=l; zy/=l; zz/=l;
    let xx=uy*zz-uz*zy, xy=uz*zx-ux*zz, xz=ux*zy-uy*zx;
    l=Math.hypot(xx,xy,xz); xx/=l; xy/=l; xz/=l;
    let yx=zy*xz-zz*xy, yy=zz*xx-zx*xz, yz=zx*xy-zy*xx;
    return new Float32Array([
        xx, yx, zx, 0,
        xy, yy, zy, 0,
        xz, yz, zz, 0,
        -(xx*ex+xy*ey+xz*ez),
        -(yx*ex+yy*ey+yz*ez),
        -(zx*ex+zy*ey+zz*ez),
        1,
    ]);
}

function perspective(fov, aspect, near, far) {
    const f = 1 / Math.tan(fov / 2);
    const nf = 1 / (near - far);
    return new Float32Array([
        f/aspect, 0, 0, 0,
        0, f, 0, 0,
        0, 0, (far+near)*nf, -1,
        0, 0, 2*far*near*nf, 0,
    ]);
}
