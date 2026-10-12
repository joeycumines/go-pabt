// Particle engine: trail, muzzle burst, impact sparks, death burst.
// Renders as GL_POINTS with a soft-circle fragment shader for efficiency.
// Cap: MAX_PARTICLES (oldest culled when exceeded).

const MAX_PARTICLES = 600;

const PART_VERT = `#version 300 es
precision highp float;
layout(location=0) in vec3 aPos;
layout(location=1) in vec4 aColor;
layout(location=2) in float aSize;

uniform mat4 uView;
uniform mat4 uProj;
uniform float uScale; // viewport scale for point size

out vec4 vColor;

void main() {
    vec4 vp = uView * vec4(aPos, 1.0);
    gl_Position = uProj * vp;
    // Point size scales with distance so near particles look bigger.
    float dist = max(-vp.z, 0.1);
    gl_PointSize = clamp(aSize * uScale / dist, 1.0, 64.0);
    vColor = aColor;
}
`;

const PART_FRAG = `#version 300 es
precision highp float;
in vec4 vColor;
out vec4 fragColor;

void main() {
    // Soft circular particle: distance from center of point sprite.
    vec2 c = gl_PointCoord - vec2(0.5);
    float r = dot(c, c) * 4.0; // 0 at center, 1 at edge
    if (r > 1.0) discard;
    float a = (1.0 - r) * vColor.a;
    // Additive-friendly: output pre-multiplied alpha.
    fragColor = vec4(vColor.rgb * a, a);
}
`;

export class ParticleEngine {
    constructor(gl) {
        this.gl = gl;
        this.program = this._createProgram(PART_VERT, PART_FRAG);
        this.uView = gl.getUniformLocation(this.program, 'uView');
        this.uProj = gl.getUniformLocation(this.program, 'uProj');
        this.uScale = gl.getUniformLocation(this.program, 'uScale');

        // Allocate buffers for MAX_PARTICLES.
        this.positions = new Float32Array(MAX_PARTICLES * 3);
        this.colors = new Float32Array(MAX_PARTICLES * 4);
        this.sizes = new Float32Array(MAX_PARTICLES);

        this.vao = gl.createVertexArray();
        gl.bindVertexArray(this.vao);

        this.posBuf = gl.createBuffer();
        gl.bindBuffer(gl.ARRAY_BUFFER, this.posBuf);
        gl.bufferData(gl.ARRAY_BUFFER, this.positions.byteLength, gl.DYNAMIC_DRAW);
        gl.enableVertexAttribArray(0);
        gl.vertexAttribPointer(0, 3, gl.FLOAT, false, 0, 0);

        this.colBuf = gl.createBuffer();
        gl.bindBuffer(gl.ARRAY_BUFFER, this.colBuf);
        gl.bufferData(gl.ARRAY_BUFFER, this.colors.byteLength, gl.DYNAMIC_DRAW);
        gl.enableVertexAttribArray(1);
        gl.vertexAttribPointer(1, 4, gl.FLOAT, false, 0, 0);

        this.sizeBuf = gl.createBuffer();
        gl.bindBuffer(gl.ARRAY_BUFFER, this.sizeBuf);
        gl.bufferData(gl.ARRAY_BUFFER, this.sizes.byteLength, gl.DYNAMIC_DRAW);
        gl.enableVertexAttribArray(2);
        gl.vertexAttribPointer(2, 1, gl.FLOAT, false, 0, 0);

        gl.bindVertexArray(null);

        // Particle storage (JS side).
        this.particles = [];
        // Live count exposed for probes.
        this.count = 0;
    }

    _createShader(type, src) {
        const gl = this.gl;
        const s = gl.createShader(type);
        gl.shaderSource(s, src);
        gl.compileShader(s);
        if (!gl.getShaderParameter(s, gl.COMPILE_STATUS)) {
            const log = gl.getShaderInfoLog(s);
            gl.deleteShader(s);
            throw new Error('particle shader: ' + log);
        }
        return s;
    }

    _createProgram(v, f) {
        const gl = this.gl;
        const vs = this._createShader(gl.VERTEX_SHADER, v);
        const fs = this._createShader(gl.FRAGMENT_SHADER, f);
        const p = gl.createProgram();
        gl.attachShader(p, vs);
        gl.attachShader(p, fs);
        gl.linkProgram(p);
        if (!gl.getProgramParameter(p, gl.LINK_STATUS)) {
            throw new Error('particle link: ' + gl.getProgramInfoLog(p));
        }
        return p;
    }

    // Spawn a single particle. Oldest particles are culled when cap is hit.
    spawn(pos, vel, life, color, size, gravity = false) {
        if (this.particles.length >= MAX_PARTICLES) {
            // Drop the oldest.
            this.particles.shift();
        }
        this.particles.push({
            px: pos[0], py: pos[1], pz: pos[2],
            vx: vel[0], vy: vel[1], vz: vel[2],
            born: performance.now() / 1000,
            life,
            r: color[0], g: color[1], b: color[2],
            size,
            gravity,
        });
    }

    // --- Emitters ---

    // Trail behind a projectile: small, short-lived, slight random spread.
    trail(pos, color) {
        const spread = 0.08;
        const rx = (Math.random() - 0.5) * spread;
        const ry = (Math.random() - 0.5) * spread;
        const rz = (Math.random() - 0.5) * spread;
        this.spawn(
            [pos[0] + rx, pos[1] + ry, pos[2] + rz],
            [rx * 0.5, ry * 0.5, rz * 0.5],
            0.3,
            [color[0], color[1], color[2]],
            0.10 + Math.random() * 0.06,
            false
        );
    }

    // Muzzle burst: cone of bright particles at weapon muzzle on fire.
    muzzleBurst(pos, dir, color) {
        const n = 14 + Math.floor(Math.random() * 6);
        for (let i = 0; i < n; i++) {
            const spread = 0.4;
            const vx = dir[0] * 6 + (Math.random() - 0.5) * spread * 6;
            const vy = dir[1] * 6 + (Math.random() - 0.5) * spread * 6;
            const vz = dir[2] * 6 + (Math.random() - 0.5) * spread * 6;
            this.spawn(
                [pos[0], pos[1], pos[2]],
                [vx, vy, vz],
                0.18 + Math.random() * 0.08,
                [Math.min(1, color[0] + 0.3), Math.min(1, color[1] + 0.3), Math.min(1, color[2] + 0.3)],
                0.18 + Math.random() * 0.1,
                false
            );
        }
    }

    // Impact sparks: radiate from hit point, gravity-affected.
    impactSparks(pos, color) {
        const n = 20 + Math.floor(Math.random() * 10);
        for (let i = 0; i < n; i++) {
            const theta = Math.random() * Math.PI * 2;
            const phi = Math.random() * Math.PI - Math.PI / 2;
            const s = 3 + Math.random() * 4;
            const vx = Math.cos(theta) * Math.cos(phi) * s;
            const vy = Math.abs(Math.sin(phi)) * s + 1;
            const vz = Math.sin(theta) * Math.cos(phi) * s;
            this.spawn(
                [pos[0], pos[1], pos[2]],
                [vx, vy, vz],
                0.4 + Math.random() * 0.3,
                [color[0], color[1], color[2]],
                0.12 + Math.random() * 0.08,
                true
            );
        }
    }

    // Death burst: larger cloud fading to transparent.
    deathBurst(pos, color) {
        const n = 35 + Math.floor(Math.random() * 15);
        for (let i = 0; i < n; i++) {
            const theta = Math.random() * Math.PI * 2;
            const phi = Math.random() * Math.PI - Math.PI / 2;
            const s = 1.5 + Math.random() * 3;
            const vx = Math.cos(theta) * Math.cos(phi) * s;
            const vy = Math.abs(Math.sin(phi)) * s + 0.5;
            const vz = Math.sin(theta) * Math.cos(phi) * s;
            this.spawn(
                [pos[0], pos[1], pos[2]],
                [vx, vy, vz],
                0.6 + Math.random() * 0.4,
                [color[0], color[1], color[2]],
                0.2 + Math.random() * 0.15,
                true
            );
        }
    }

    // Advance particles; cull dead ones.
    update(dt) {
        const now = performance.now() / 1000;
        const GRAV = -9.8;
        for (let i = this.particles.length - 1; i >= 0; i--) {
            const p = this.particles[i];
            const age = now - p.born;
            if (age >= p.life) {
                this.particles.splice(i, 1);
                continue;
            }
            if (p.gravity) p.vy += GRAV * dt;
            p.px += p.vx * dt;
            p.py += p.vy * dt;
            p.pz += p.vz * dt;
            // Light drag.
            p.vx *= 0.98;
            p.vy *= 0.98;
            p.vz *= 0.98;
        }
        this.count = this.particles.length;
    }

    // Render all live particles. Call AFTER world geometry so additive
    // blending layers correctly; disable depth writes for translucency.
    render(viewMat, projMat) {
        const gl = this.gl;
        const n = this.particles.length;
        if (n === 0) return;

        const now = performance.now() / 1000;
        for (let i = 0; i < n; i++) {
            const p = this.particles[i];
            const age = now - p.born;
            const t = age / p.life; // 0 -> 1
            const fade = 1.0 - t;
            this.positions[i * 3] = p.px;
            this.positions[i * 3 + 1] = p.py;
            this.positions[i * 3 + 2] = p.pz;
            this.colors[i * 4] = p.r;
            this.colors[i * 4 + 1] = p.g;
            this.colors[i * 4 + 2] = p.b;
            this.colors[i * 4 + 3] = fade;
            this.sizes[i] = p.size * (0.4 + 0.6 * fade);
        }

        gl.useProgram(this.program);
        gl.uniformMatrix4fv(this.uView, false, viewMat);
        gl.uniformMatrix4fv(this.uProj, false, projMat);
        // uScale: rough viewport-height proxy so point sizes feel consistent.
        gl.uniform1f(this.uScale, gl.drawingBufferHeight * 0.35);

        // Additive blending, no depth write.
        gl.enable(gl.BLEND);
        gl.blendFunc(gl.SRC_ALPHA, gl.ONE);
        gl.depthMask(false);

        gl.bindVertexArray(this.vao);
        gl.bindBuffer(gl.ARRAY_BUFFER, this.posBuf);
        gl.bufferSubData(gl.ARRAY_BUFFER, 0, this.positions.subarray(0, n * 3));
        gl.bindBuffer(gl.ARRAY_BUFFER, this.colBuf);
        gl.bufferSubData(gl.ARRAY_BUFFER, 0, this.colors.subarray(0, n * 4));
        gl.bindBuffer(gl.ARRAY_BUFFER, this.sizeBuf);
        gl.bufferSubData(gl.ARRAY_BUFFER, 0, this.sizes.subarray(0, n));

        gl.drawArrays(gl.POINTS, 0, n);
        gl.bindVertexArray(null);

        // Restore state.
        gl.depthMask(true);
        gl.disable(gl.BLEND);
    }
}
