// GLB (glTF-Binary) parser + CPU-driven skeletal renderer for vanilla WebGL2.
// Parses nodes/skins/animations, samples keyframes per frame, computes bone
// matrices on the CPU (joints <= 32 => one mat4 uniform array), and draws each
// actor with one drawElements call. No frameworks, no build step.

const MAX_JOINTS = 32;

const VERT_SRC = `#version 300 es
precision highp float;

layout(location=0) in vec3 aPos;
layout(location=1) in vec3 aNormal;
layout(location=2) in vec4 aJoints;
layout(location=3) in vec4 aWeights;

uniform mat4 uView;
uniform mat4 uProj;
uniform mat4 uModel;
uniform mat4 uBones[${MAX_JOINTS}];

out vec3 vNormal;
out vec3 vWorldPos;

void main() {
    mat4 skin = uBones[int(aJoints.x)] * aWeights.x
              + uBones[int(aJoints.y)] * aWeights.y
              + uBones[int(aJoints.z)] * aWeights.z
              + uBones[int(aJoints.w)] * aWeights.w;
    vec4 wp = uModel * skin * vec4(aPos, 1.0);
    vWorldPos = wp.xyz;
    vNormal = mat3(uModel) * (mat3(skin) * aNormal);
    gl_Position = uProj * uView * wp;
}
`;

const FRAG_SRC = `#version 300 es
precision highp float;

in vec3 vNormal;
in vec3 vWorldPos;

uniform vec3 uCameraPos;
uniform float uFogDensity;
uniform vec3 uFogColor;
uniform vec4 uColor;
uniform vec3 uEmissive;      // faction trim color (rim glow)
uniform float uEmissiveStrength; // 0..1 intensity of the rim glow

out vec4 fragColor;

void main() {
    vec3 lightDir = normalize(vec3(0.3, 1.0, 0.5));
    float diff = max(dot(normalize(vNormal), lightDir), 0.0);
    vec3 lit = uColor.rgb * (0.30 + 0.70 * diff) + uColor.rgb * 0.12;
    // Fresnel rim: edges facing away from the camera glow with the faction
    // trim color, giving each faction a distinct emissive accent beyond
    // the body tint.
    vec3 viewDir = normalize(uCameraPos - vWorldPos);
    float rim = 1.0 - max(dot(normalize(vNormal), viewDir), 0.0);
    rim = pow(rim, 3.0);
    lit += uEmissive * rim * uEmissiveStrength;
    float dist = length(vWorldPos - uCameraPos);
    float fog = 1.0 - exp(-uFogDensity * dist);
    vec3 final = mix(lit, uFogColor, fog);
    fragColor = vec4(final, uColor.a);
}
`;

// ---- minimal mat4/quat helpers (column-major, glTF convention) ----

function mat4Identity() {
    return new Float32Array([1,0,0,0, 0,1,0,0, 0,0,1,0, 0,0,0,1]);
}

function mat4Mul(a, b, out) {
    for (let c = 0; c < 4; c++) {
        for (let r = 0; r < 4; r++) {
            out[c*4+r] = a[r]*b[c*4] + a[4+r]*b[c*4+1] + a[8+r]*b[c*4+2] + a[12+r]*b[c*4+3];
        }
    }
    return out;
}

function quatToMat4(q, out) {
    const [x, y, z, w] = q;
    const x2 = x+x, y2 = y+y, z2 = z+z;
    const xx = x*x2, xy = x*y2, xz = x*z2;
    const yy = y*y2, yz = y*z2, zz = z*z2;
    const wx = w*x2, wy = w*y2, wz = w*z2;
    out[0] = 1-(yy+zz); out[1] = xy+wz;     out[2] = xz-wy;     out[3] = 0;
    out[4] = xy-wz;     out[5] = 1-(xx+zz); out[6] = yz+wx;     out[7] = 0;
    out[8] = xz+wy;     out[9] = yz-wx;     out[10] = 1-(xx+yy); out[11] = 0;
    out[12] = 0; out[13] = 0; out[14] = 0; out[15] = 1;
    return out;
}

function composeTRS(t, r, s, out) {
    quatToMat4(r, out);
    out[0] *= s[0]; out[1] *= s[0]; out[2] *= s[0];
    out[4] *= s[1]; out[5] *= s[1]; out[6] *= s[1];
    out[8] *= s[2]; out[9] *= s[2]; out[10] *= s[2];
    out[12] = t[0]; out[13] = t[1]; out[14] = t[2];
    return out;
}

// Model matrix for an actor standing at pos (feet on floor), facing yaw
// radians (0 = +Z, matching the game's yaw convention), uniformly scaled.
export function actorModelMatrix(pos, yaw, scale, yOff, out) {
    const c = Math.cos(yaw), s = Math.sin(yaw);
    out[0] = c*scale; out[1] = 0; out[2] = -s*scale; out[3] = 0;
    out[4] = 0;       out[5] = scale; out[6] = 0;      out[7] = 0;
    out[8] = s*scale; out[9] = 0; out[10] = c*scale;  out[11] = 0;
    out[12] = pos[0]; out[13] = pos[1] + yOff; out[14] = pos[2]; out[15] = 1;
    return out;
}

function slerp(a, b, t, out) {
    let dot = a[0]*b[0] + a[1]*b[1] + a[2]*b[2] + a[3]*b[3];
    let bb = b;
    if (dot < 0) { dot = -dot; bb = [-b[0], -b[1], -b[2], -b[3]]; }
    if (dot > 0.9995) {
        out[0] = a[0] + (bb[0]-a[0])*t; out[1] = a[1] + (bb[1]-a[1])*t;
        out[2] = a[2] + (bb[2]-a[2])*t; out[3] = a[3] + (bb[3]-a[3])*t;
    } else {
        const th = Math.acos(dot), st = Math.sin(th);
        const wa = Math.sin((1-t)*th)/st, wb = Math.sin(t*th)/st;
        out[0] = wa*a[0] + wb*bb[0]; out[1] = wa*a[1] + wb*bb[1];
        out[2] = wa*a[2] + wb*bb[2]; out[3] = wa*a[3] + wb*bb[3];
    }
    return out;
}

// ---- GLB parsing ----

function readAccessor(json, bin, idx) {
    const acc = json.accessors[idx];
    const view = json.bufferViews[acc.bufferView];
    const base = (view.byteOffset || 0) + (acc.byteOffset || 0);
    const comps = { SCALAR: 1, VEC2: 2, VEC3: 3, VEC4: 4, MAT4: 16 }[acc.type];
    const stride = view.byteStride || 0;
    const elemBytes = { 5120: 1, 5121: 1, 5122: 2, 5123: 2, 5125: 4, 5126: 4 }[acc.componentType];
    const out = new Float32Array(acc.count * comps);
    const dv = new DataView(bin.buffer, bin.byteOffset, bin.byteLength);
    for (let i = 0; i < acc.count; i++) {
        let off = base + (stride ? i * stride : i * comps * elemBytes);
        for (let c = 0; c < comps; c++) {
            let v;
            switch (acc.componentType) {
                case 5120: v = dv.getInt8(off); break;
                case 5121: v = dv.getUint8(off); break;
                case 5122: v = dv.getInt16(off, true); break;
                case 5123: v = dv.getUint16(off, true); break;
                case 5125: v = dv.getUint32(off, true); break;
                default:   v = dv.getFloat32(off, true); break;
            }
            out[i*comps + c] = v;
            off += elemBytes;
        }
    }
    return { data: out, comps, count: acc.count, componentType: acc.componentType };
}

function parseGLB(buf) {
    const dv = new DataView(buf);
    const magic = dv.getUint32(0, true);
    if (magic !== 0x46546c67) throw new Error('not a GLB');
    const length = dv.getUint32(8, true);
    let off = 12, json = null, bin = null;
    while (off < length) {
        const clen = dv.getUint32(off, true);
        const ctype = dv.getUint32(off + 4, true);
        const body = new Uint8Array(buf, off + 8, clen);
        if (ctype === 0x4e4f534a) json = JSON.parse(new TextDecoder().decode(body));
        else if (ctype === 0x004e4942) bin = body;
        off += 8 + clen;
    }
    if (!json) throw new Error('GLB has no JSON chunk');

    // Nodes and parent links.
    const nodes = (json.nodes || []).map(n => ({
        name: n.name || '',
        children: n.children || [],
        t: n.translation || [0, 0, 0],
        r: n.rotation || [0, 0, 0, 1],
        s: n.scale || [1, 1, 1],
        matrix: n.matrix ? new Float32Array(n.matrix) : null,
        mesh: n.mesh ?? -1,
    }));
    const parent = new Array(nodes.length).fill(-1);
    nodes.forEach((n, i) => n.children.forEach(c => { parent[c] = i; }));

    // Merge every skinned primitive into one vertex/index set.
    const pos = [], nor = [], joi = [], wei = [], idxArr = [];
    let minY = Infinity, maxY = -Infinity;
    for (const mesh of json.meshes || []) {
        for (const pr of mesh.primitives || []) {
            const at = pr.attributes;
            if (at.JOINTS_0 === undefined || at.WEIGHTS_0 === undefined) continue;
            const p = readAccessor(json, bin, at.POSITION);
            const n = readAccessor(json, bin, at.NORMAL);
            const j = readAccessor(json, bin, at.JOINTS_0);
            const wv = readAccessor(json, bin, at.WEIGHTS_0);
            const base = pos.length / 3;
            for (let i = 0; i < p.count * 3; i += 3) {
                pos.push(p.data[i], p.data[i+1], p.data[i+2]);
                if (p.data[i+1] < minY) minY = p.data[i+1];
                if (p.data[i+1] > maxY) maxY = p.data[i+1];
            }
            for (let i = 0; i < n.count * 3; i++) nor.push(n.data[i]);
            for (let i = 0; i < j.count * 4; i++) joi.push(j.data[i]);
            for (let i = 0; i < wv.count * 4; i++) wei.push(wv.data[i]);
            if (pr.indices !== undefined) {
                const ix = readAccessor(json, bin, pr.indices);
                for (let i = 0; i < ix.count; i++) idxArr.push(ix.data[i] + base);
            } else {
                for (let i = 0; i < p.count; i++) idxArr.push(i + base);
            }
        }
    }
    if (pos.length === 0) throw new Error('GLB has no skinned primitives');

    // Skin (take the first).
    const skinJson = (json.skins || [])[0];
    let joints = [], ibm = null;
    if (skinJson) {
        joints = skinJson.joints || [];
        if (skinJson.inverseBindMatrices !== undefined) {
            ibm = readAccessor(json, bin, skinJson.inverseBindMatrices).data;
        }
    }
    if (!ibm) {
        ibm = new Float32Array(joints.length * 16);
        for (let i = 0; i < joints.length; i++) ibm.set(mat4Identity(), i * 16);
    }

    // Animations.
    const animations = {};
    for (const a of json.animations || []) {
        const channels = [];
        for (const ch of a.channels || []) {
            const s = a.samplers[ch.sampler];
            if (!s || !ch.target) continue;
            const input = readAccessor(json, bin, s.input).data;
            const output = readAccessor(json, bin, s.output).data;
            const comps = ch.target.path === 'rotation' ? 4 : 3;
            channels.push({
                node: ch.target.node,
                path: ch.target.path,
                times: input,
                values: output,
                comps,
                interpolation: s.interpolation || 'LINEAR',
            });
        }
        if (channels.length) animations[a.name || ('anim' + Object.keys(animations).length)] = { channels, duration: Math.max(...channels.map(c => c.times[c.times.length - 1])) };
    }

    return {
        nodes, parent, joints, ibm, animations,
        positions: new Float32Array(pos),
        normals: new Float32Array(nor),
        jointIdx: new Float32Array(joi),
        weights: new Float32Array(wei),
        indices: (pos.length / 3) > 65535 ? new Uint32Array(idxArr) : new Uint16Array(idxArr),
        indexType: (pos.length / 3) > 65535 ? 'u32' : 'u16',
        minY: minY === Infinity ? 0 : minY,
        height: (maxY - minY) || 1,
    };
}

// ---- renderer ----

export class ModelBank {
    constructor(gl) {
        this.gl = gl;
        this.models = new Map(); // name -> gpu model
        this.program = this._createProgram();
        this.uView = gl.getUniformLocation(this.program, 'uView');
        this.uProj = gl.getUniformLocation(this.program, 'uProj');
        this.uModel = gl.getUniformLocation(this.program, 'uModel');
        this.uBones = gl.getUniformLocation(this.program, 'uBones[0]');
        this.uCameraPos = gl.getUniformLocation(this.program, 'uCameraPos');
        this.uFogDensity = gl.getUniformLocation(this.program, 'uFogDensity');
        this.uFogColor = gl.getUniformLocation(this.program, 'uFogColor');
        this.uColor = gl.getUniformLocation(this.program, 'uColor');
        this.uEmissive = gl.getUniformLocation(this.program, 'uEmissive');
        this.uEmissiveStrength = gl.getUniformLocation(this.program, 'uEmissiveStrength');
        this.boneBuf = new Float32Array(MAX_JOINTS * 16);
        this.scratch = new Float32Array(16);
        this.scratch2 = new Float32Array(16);
    }

    _createShader(type, src) {
        const gl = this.gl;
        const sh = gl.createShader(type);
        gl.shaderSource(sh, src);
        gl.compileShader(sh);
        if (!gl.getShaderParameter(sh, gl.COMPILE_STATUS)) {
            const log = gl.getShaderInfoLog(sh);
            gl.deleteShader(sh);
            throw new Error('model shader: ' + log);
        }
        return sh;
    }

    _createProgram() {
        const gl = this.gl;
        const p = gl.createProgram();
        gl.attachShader(p, this._createShader(gl.VERTEX_SHADER, VERT_SRC));
        gl.attachShader(p, this._createShader(gl.FRAGMENT_SHADER, FRAG_SRC));
        gl.linkProgram(p);
        if (!gl.getProgramParameter(p, gl.LINK_STATUS)) {
            throw new Error('model program: ' + gl.getProgramInfoLog(p));
        }
        return p;
    }

    async load(name, url) {
        const res = await fetch(url);
        if (!res.ok) throw new Error('fetch ' + url + ': ' + res.status);
        const buf = await res.arrayBuffer();
        const m = parseGLB(buf);
        const gl = this.gl;
        const vao = gl.createVertexArray();
        gl.bindVertexArray(vao);
        const mk = (loc, data, comps) => {
            const b = gl.createBuffer();
            gl.bindBuffer(gl.ARRAY_BUFFER, b);
            gl.bufferData(gl.ARRAY_BUFFER, data, gl.STATIC_DRAW);
            gl.enableVertexAttribArray(loc);
            gl.vertexAttribPointer(loc, comps, gl.FLOAT, false, 0, 0);
        };
        mk(0, m.positions, 3);
        mk(1, m.normals, 3);
        mk(2, m.jointIdx, 4);
        mk(3, m.weights, 4);
        const ib = gl.createBuffer();
        gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, ib);
        gl.bufferData(gl.ELEMENT_ARRAY_BUFFER, m.indices, gl.STATIC_DRAW);
        gl.bindVertexArray(null);
        const gpu = Object.assign({}, m, {
            vao,
            indexCount: m.indices.length,
            glIndexType: m.indexType === 'u32' ? gl.UNSIGNED_INT : gl.UNSIGNED_SHORT,
        });
        this.models.set(name, gpu);
        return gpu;
    }

    setViewProj(view, proj, cameraPos, fogDensity, fogColor) {
        const gl = this.gl;
        gl.useProgram(this.program);
        gl.uniformMatrix4fv(this.uView, false, view);
        gl.uniformMatrix4fv(this.uProj, false, proj);
        gl.uniform3f(this.uCameraPos, cameraPos[0], cameraPos[1], cameraPos[2]);
        gl.uniform1f(this.uFogDensity, fogDensity);
        gl.uniform3f(this.uFogColor, fogColor[0], fogColor[1], fogColor[2]);
    }

    // Pick an animation by candidate substrings (the PS1 pack spells attack
    // "Atack"; the spider's attack clip is named DamageSpider).
    static pickAnim(model, want) {
        const names = Object.keys(model.animations);
        const tests = {
            idle: n => /idle/i.test(n),
            move: n => /walk|run|fly/i.test(n),
            attack: n => /atack|attack/i.test(n) || /^damage/i.test(n),
            hurt: n => /getdamage/i.test(n),
            death: n => /death/i.test(n),
        };
        const t = tests[want];
        if (!t) return null;
        return names.find(t) || null;
    }

    _sampleChannel(ch, t, outT, outR, outS) {
        const times = ch.times;
        let hi = times.length - 1;
        let lo = 0;
        while (lo < hi - 1) {
            const mid = (lo + hi) >> 1;
            if (times[mid] <= t) lo = mid; else hi = mid;
        }
        const span = times[hi] - times[lo];
        const f = span > 1e-9 ? (t - times[lo]) / span : 0;
        const c = ch.comps;
        const a = lo * c, b = hi * c;
        if (ch.path === 'rotation') {
            slerp(
                [ch.values[a], ch.values[a+1], ch.values[a+2], ch.values[a+3]],
                [ch.values[b], ch.values[b+1], ch.values[b+2], ch.values[b+3]],
                ch.interpolation === 'STEP' ? 0 : f, outR,
            );
        } else if (ch.path === 'translation') {
            for (let i = 0; i < 3; i++) outT[i] = ch.values[a+i] + (ch.values[b+i] - ch.values[a+i]) * (ch.interpolation === 'STEP' ? 0 : f);
        } else if (ch.path === 'scale') {
            for (let i = 0; i < 3; i++) outS[i] = ch.values[a+i] + (ch.values[b+i] - ch.values[a+i]) * (ch.interpolation === 'STEP' ? 0 : f);
        }
    }

    _computeBones(model, anim, t) {
        const nodes = model.nodes;
        const overrides = new Map();
        if (anim) {
            const tmpT = [0, 0, 0], tmpR = [0, 0, 0, 1], tmpS = [1, 1, 1];
            for (const ch of anim.channels) {
                let o = overrides.get(ch.node);
                if (!o) {
                    const n = nodes[ch.node];
                    o = { t: [...n.t], r: [...n.r], s: [...n.s] };
                    overrides.set(ch.node, o);
                }
                tmpT[0] = o.t[0]; tmpT[1] = o.t[1]; tmpT[2] = o.t[2];
                tmpR[0] = o.r[0]; tmpR[1] = o.r[1]; tmpR[2] = o.r[2]; tmpR[3] = o.r[3];
                tmpS[0] = o.s[0]; tmpS[1] = o.s[1]; tmpS[2] = o.s[2];
                this._sampleChannel(ch, t, tmpT, tmpR, tmpS);
                o.t[0] = tmpT[0]; o.t[1] = tmpT[1]; o.t[2] = tmpT[2];
                o.r[0] = tmpR[0]; o.r[1] = tmpR[1]; o.r[2] = tmpR[2]; o.r[3] = tmpR[3];
                o.s[0] = tmpS[0]; o.s[1] = tmpS[1]; o.s[2] = tmpS[2];
            }
        }
        // Global matrices, memoized up the parent chain.
        const globals = new Array(nodes.length).fill(null);
        const local = this.scratch2;
        const globalOf = (i) => {
            if (globals[i]) return globals[i];
            const n = nodes[i];
            const o = overrides.get(i);
            const lm = new Float32Array(16);
            if (n.matrix && !o) lm.set(n.matrix);
            else composeTRS(o ? o.t : n.t, o ? o.r : n.r, o ? o.s : n.s, lm);
            const p = model.parent[i];
            if (p >= 0) {
                globals[i] = mat4Mul(globalOf(p), lm, new Float32Array(16));
            } else {
                globals[i] = lm;
            }
            return globals[i];
        };
        const out = this.boneBuf;
        out.fill(0);
        const nJ = Math.min(model.joints.length, MAX_JOINTS);
        for (let j = 0; j < nJ; j++) {
            const g = globalOf(model.joints[j]);
            const ibm = model.ibm.subarray(j * 16, j * 16 + 16);
            mat4Mul(g, ibm, this.scratch);
            out.set(this.scratch, j * 16);
        }
        for (let j = nJ; j < MAX_JOINTS; j++) out.set(mat4Identity(), j * 16);
        return nJ;
    }

    // Draw one actor instance. animName null => bind pose.
    draw(model, animName, timeSec, modelMat, color, loop = true, emissive = [0, 0, 0], emissiveStrength = 0) {
        const gl = this.gl;
        const anim = animName ? model.animations[animName] : null;
        let t = timeSec;
        if (anim) {
            t = loop ? timeSec % anim.duration : Math.min(timeSec, anim.duration);
        }
        gl.useProgram(this.program);
        this._computeBones(model, anim, t);
        gl.uniformMatrix4fv(this.uBones, false, this.boneBuf);
        gl.uniformMatrix4fv(this.uModel, false, modelMat);
        gl.uniform4f(this.uColor, color[0], color[1], color[2], color[3] ?? 1);
        gl.uniform3f(this.uEmissive, emissive[0], emissive[1], emissive[2]);
        gl.uniform1f(this.uEmissiveStrength, emissiveStrength);
        gl.bindVertexArray(model.vao);
        gl.drawElements(gl.TRIANGLES, model.indexCount, model.glIndexType, 0);
        gl.bindVertexArray(null);
    }
}
