// Perspective camera with pointer-lock mouse look.

export class Camera {
    constructor(canvas) {
        this.canvas = canvas;
        this.pos = [1.5, 0.5, 1.5]; // x, y (height), z
        this.yaw = 0;   // radians, 0 = +Z
        this.pitch = 0;  // radians, clamped to ~+/-89 degrees

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

    forward() {
        return [
            Math.sin(this.yaw) * Math.cos(this.pitch),
            Math.sin(this.pitch),
            Math.cos(this.yaw) * Math.cos(this.pitch),
        ];
    }

    right() {
        return [Math.cos(this.yaw), 0, -Math.sin(this.yaw)];
    }

    viewMatrix() {
        const [ex,ey,ez] = this.pos;
        const [fx,fy,fz] = this.forward();
        const tx = ex+fx, ty = ey+fy, tz = ez+fz;
        return lookAt(ex,ey,ez, tx,ty,tz, 0,1,0);
    }

    projMatrix(aspect, fovDeg = 70, near = 0.1, far = 100) {
        return perspective(fovDeg * Math.PI / 180, aspect, near, far);
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
