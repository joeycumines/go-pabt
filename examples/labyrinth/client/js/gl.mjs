// WebGL2 rendering core: context, shaders, instanced geometry.

const VERT_SRC = `#version 300 es
precision highp float;

layout(location=0) in vec3 aPos;
layout(location=1) in vec3 aNormal;
layout(location=2) in vec4 aInstanceColor;
layout(location=3) in mat4 aModel;

uniform mat4 uView;
uniform mat4 uProj;

out vec3 vNormal;
out vec3 vWorldPos;
out vec4 vColor;

void main() {
    vec4 worldPos = aModel * vec4(aPos, 1.0);
    vWorldPos = worldPos.xyz;
    vNormal = mat3(aModel) * aNormal;
    vColor = aInstanceColor;
    gl_Position = uProj * uView * worldPos;
}
`;

const FRAG_SRC = `#version 300 es
precision highp float;

in vec3 vNormal;
in vec3 vWorldPos;
in vec4 vColor;

uniform vec3 uCameraPos;
uniform float uFogDensity;
uniform vec3 uFogColor;

out vec4 fragColor;

void main() {
    // Simple directional light
    vec3 lightDir = normalize(vec3(0.3, 1.0, 0.5));
    float diff = max(dot(normalize(vNormal), lightDir), 0.0);
    vec3 lit = vColor.rgb * (0.3 + 0.7 * diff);

    // Exponential fog
    float dist = length(vWorldPos - uCameraPos);
    float fog = 1.0 - exp(-uFogDensity * dist);
    vec3 final = mix(lit, uFogColor, fog);

    fragColor = vec4(final, vColor.a);
}
`;

export class Renderer {
    constructor(canvas) {
        this.canvas = canvas;
        this.gl = canvas.getContext('webgl2', { antialias: true, alpha: false });
        if (!this.gl) throw new Error('WebGL2 not supported');

        this.program = this._createProgram(VERT_SRC, FRAG_SRC);
        this.gl.useProgram(this.program);

        // Uniform locations
        this.uView = this.gl.getUniformLocation(this.program, 'uView');
        this.uProj = this.gl.getUniformLocation(this.program, 'uProj');
        this.uCameraPos = this.gl.getUniformLocation(this.program, 'uCameraPos');
        this.uFogDensity = this.gl.getUniformLocation(this.program, 'uFogDensity');
        this.uFogColor = this.gl.getUniformLocation(this.program, 'uFogColor');

        // Default fog
        this.gl.uniform1f(this.uFogDensity, 0.08);
        this.gl.uniform3f(this.uFogColor, 0.04, 0.04, 0.06);

        this.gl.enable(this.gl.DEPTH_TEST);
        this.gl.enable(this.gl.CULL_FACE);
        this.gl.clearColor(0.04, 0.04, 0.06, 1.0);

        this._buildCubeGeometry();
        this.instanceData = [];
    }

    _createShader(type, src) {
        const gl = this.gl;
        const shader = gl.createShader(type);
        gl.shaderSource(shader, src);
        gl.compileShader(shader);
        if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
            const log = gl.getShaderInfoLog(shader);
            gl.deleteShader(shader);
            throw new Error('Shader compile error: ' + log);
        }
        return shader;
    }

    _createProgram(vSrc, fSrc) {
        const gl = this.gl;
        const vs = this._createShader(gl.VERTEX_SHADER, vSrc);
        const fs = this._createShader(gl.FRAGMENT_SHADER, fSrc);
        const prog = gl.createProgram();
        gl.attachShader(prog, vs);
        gl.attachShader(prog, fs);
        gl.linkProgram(prog);
        if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) {
            throw new Error('Program link error: ' + gl.getProgramInfoLog(prog));
        }
        return prog;
    }

    _buildCubeGeometry() {
        const gl = this.gl;
        // Unit cube: positions and normals for 6 faces (36 vertices)
        const positions = [];
        const normals = [];
        const faces = [
            { n: [0,0,1],  verts: [[-0.5,-0.5,0.5],[0.5,-0.5,0.5],[0.5,0.5,0.5],[-0.5,0.5,0.5]] },
            { n: [0,0,-1], verts: [[0.5,-0.5,-0.5],[-0.5,-0.5,-0.5],[-0.5,0.5,-0.5],[0.5,0.5,-0.5]] },
            { n: [0,1,0],  verts: [[-0.5,0.5,0.5],[0.5,0.5,0.5],[0.5,0.5,-0.5],[-0.5,0.5,-0.5]] },
            { n: [0,-1,0], verts: [[-0.5,-0.5,-0.5],[0.5,-0.5,-0.5],[0.5,-0.5,0.5],[-0.5,-0.5,0.5]] },
            { n: [1,0,0],  verts: [[0.5,-0.5,0.5],[0.5,-0.5,-0.5],[0.5,0.5,-0.5],[0.5,0.5,0.5]] },
            { n: [-1,0,0], verts: [[-0.5,-0.5,-0.5],[-0.5,-0.5,0.5],[-0.5,0.5,0.5],[-0.5,0.5,-0.5]] },
        ];
        for (const f of faces) {
            const [a,b,c,d] = f.verts;
            // Two triangles per face
            for (const idx of [0,1,2, 0,2,3]) {
                const v = [a,b,c,d][idx];
                positions.push(...v);
                normals.push(...f.n);
            }
        }

        this.vao = gl.createVertexArray();
        gl.bindVertexArray(this.vao);

        // Position buffer (location 0)
        const posBuf = gl.createBuffer();
        gl.bindBuffer(gl.ARRAY_BUFFER, posBuf);
        gl.bufferData(gl.ARRAY_BUFFER, new Float32Array(positions), gl.STATIC_DRAW);
        gl.enableVertexAttribArray(0);
        gl.vertexAttribPointer(0, 3, gl.FLOAT, false, 0, 0);

        // Normal buffer (location 1)
        const normBuf = gl.createBuffer();
        gl.bindBuffer(gl.ARRAY_BUFFER, normBuf);
        gl.bufferData(gl.ARRAY_BUFFER, new Float32Array(normals), gl.STATIC_DRAW);
        gl.enableVertexAttribArray(1);
        gl.vertexAttribPointer(1, 3, gl.FLOAT, false, 0, 0);

        // Instance color buffer (location 2) - will be filled per frame
        this.colorBuf = gl.createBuffer();
        gl.bindBuffer(gl.ARRAY_BUFFER, this.colorBuf);
        gl.enableVertexAttribArray(2);
        gl.vertexAttribPointer(2, 4, gl.FLOAT, false, 0, 0);
        gl.vertexAttribDivisor(2, 1);

        // Instance model matrix buffer (location 3) - mat4 = 4 vec4s
        this.modelBuf = gl.createBuffer();
        gl.bindBuffer(gl.ARRAY_BUFFER, this.modelBuf);
        for (let i = 0; i < 4; i++) {
            gl.enableVertexAttribArray(3 + i);
            gl.vertexAttribPointer(3 + i, 4, gl.FLOAT, false, 64, i * 16);
            gl.vertexAttribDivisor(3 + i, 1);
        }

        gl.bindVertexArray(null);
        this.vertexCount = positions.length / 3;
    }

    resize() {
        const dpr = window.devicePixelRatio || 1;
        this.canvas.width = this.canvas.clientWidth * dpr;
        this.canvas.height = this.canvas.clientHeight * dpr;
        this.gl.viewport(0, 0, this.canvas.width, this.canvas.height);
    }

    setView(viewMat) {
        this.gl.uniformMatrix4fv(this.uView, false, viewMat);
    }

    setProj(projMat) {
        this.gl.uniformMatrix4fv(this.uProj, false, projMat);
    }

    setCamera(pos) {
        this.gl.uniform3f(this.uCameraPos, pos[0], pos[1], pos[2]);
    }

    setFog(density, color) {
        this.gl.uniform1f(this.uFogDensity, density);
        this.gl.uniform3f(this.uFogColor, color[0], color[1], color[2]);
    }

    // instances: array of { pos:[x,y,z], scale:[sx,sy,sz], color:[r,g,b,a] }
    drawInstances(instances) {
        const gl = this.gl;
        const n = instances.length;
        if (n === 0) return;

        const colors = new Float32Array(n * 4);
        const models = new Float32Array(n * 16);

        for (let i = 0; i < n; i++) {
            const inst = instances[i];
            colors[i*4]   = inst.color[0];
            colors[i*4+1] = inst.color[1];
            colors[i*4+2] = inst.color[2];
            colors[i*4+3] = inst.color[3] ?? 1.0;

            // Build TRS matrix (translation * rotation(identity) * scale)
            const [px,py,pz] = inst.pos;
            const [sx,sy,sz] = inst.scale || [1,1,1];
            const m = models.subarray(i*16, i*16+16);
            m[0]=sx; m[1]=0;  m[2]=0;  m[3]=0;
            m[4]=0;  m[5]=sy; m[6]=0;  m[7]=0;
            m[8]=0;  m[9]=0;  m[10]=sz;m[11]=0;
            m[12]=px;m[13]=py;m[14]=pz;m[15]=1;
        }

        gl.bindVertexArray(this.vao);
        gl.bindBuffer(gl.ARRAY_BUFFER, this.colorBuf);
        gl.bufferData(gl.ARRAY_BUFFER, colors, gl.DYNAMIC_DRAW);
        gl.bindBuffer(gl.ARRAY_BUFFER, this.modelBuf);
        gl.bufferData(gl.ARRAY_BUFFER, models, gl.DYNAMIC_DRAW);

        gl.drawArraysInstanced(gl.TRIANGLES, 0, this.vertexCount, n);
        gl.bindVertexArray(null);
    }

    clear() {
        this.gl.clear(this.gl.COLOR_BUFFER_BIT | this.gl.DEPTH_BUFFER_BIT);
    }
}
