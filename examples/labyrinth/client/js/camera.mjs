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
        this.recoilVel = 0;     // spring velocity for recoil recovery
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

    // applyRecoil kicks the view upward and arms the spring recovery. The
    // player never sends the resulting pitch to the server, so this is purely
    // a visual "shoot feel" effect.
    applyRecoil(strength = 0.02) {
        this.recoilPitch += strength;
        this.recoilVel += strength * 40;
    }

    // kickFov briefly narrows the FOV for a snap-fire feel.
    kickFov(delta = 3) {
        this.currentFovDeg -= delta;
    }

    // updateMovement advances the visual juice: sprint FOV widen, recoil
    // spring recovery, weapon bob. Call once per frame before viewMatrix().
    updateMovement(dt, speed) {
        const sprinting = speed > 5;
        this.targetFovDeg = sprinting ? this.baseFovDeg + 10 : this.baseFovDeg;
        this.currentFovDeg += (this.targetFovDeg - this.currentFovDeg) * Math.min(1, 4 * dt);
        const k = 60, d = 12;
        this.recoilVel += (-k * this.recoilPitch - d * this.recoilVel) * dt;
        this.recoilPitch += this.recoilVel * dt;
        if (Math.abs(this.recoilPitch) < 0.0001 && Math.abs(this.recoilVel) < 0.0001) {
            this.recoilPitch = 0;
            this.recoilVel = 0;
        }
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
