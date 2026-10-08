// Main game loop: ties renderer, camera, input, HUD, net together.

import { Renderer } from './gl.mjs';
import { Camera } from './camera.mjs';
import { Input } from './input.mjs';
import { HUD } from './hud.mjs';
import { NetClient } from './net.mjs';
import { AudioEngine } from './audio.mjs';

const canvas = document.getElementById('glcanvas');
const loading = document.getElementById('loading');
const hudEl = document.getElementById('hud');
const prompt = document.getElementById('pointer-lock-prompt');
const overlay = document.getElementById('overlay');

let renderer, camera, input, hud, net, audio;
let maze = null; // authoritative maze from server
let lastTime = 0;
let gameState = 'playing'; // 'playing' | 'caught' | 'won'

// Player physics state
let playerVelY = 0;
let grounded = true;
const GRAVITY = -15;
const JUMP_VEL = 6;
const PLAYER_HEIGHT = 0.5;
const PLAYER_RADIUS = 0.3;
const MAX_HP = 100;

// Minotaur interpolation state
let minotaurBuffers = new Map(); // id -> { pos, targetPos, lastUpdate, type, stunned }

// Projectile state (client-side visual only)
let projectiles = []; // { pos, dir, born, color }

// Pulse fire state
let lastFireTime = 0;
const FIRE_COOLDOWN = 0.5; // seconds
let muzzleFlashTimer = 0;
let hitFlashTimer = 0;
let damageFlashTimer = 0; // red flash when player takes damage

// Position reconciliation
let serverPos = null; // last server position for reconciliation
let lastHP = 100; // last known player HP from server

function init() {
    renderer = new Renderer(canvas);
    camera = new Camera(canvas);
    input = new Input(canvas);
    hud = new HUD();
    net = new NetClient();
    audio = new AudioEngine();

    // Connect to server.
    net.connect();
    net.onEvent = handleEvent;
    net.onMaze = (mazeData) => {
        maze = mazeData;
        // Place camera at maze start.
        camera.pos = [maze.startX, PLAYER_HEIGHT, maze.startY];
        // Hide loading, show HUD and prompt.
        loading.hidden = true;
        hudEl.hidden = false;
        prompt.hidden = false;
        overlay.hidden = true;
        // Start the render loop.
        requestAnimationFrame(frame);
    };

    canvas.addEventListener('click', () => {
        prompt.hidden = true;
        audio.resume();
    });
    document.addEventListener('pointerlockchange', () => {
        prompt.hidden = document.pointerLockElement === canvas;
    });

    renderer.resize();
    window.addEventListener('resize', () => renderer.resize());
}

function frame(time) {
    const dt = Math.min((time - lastTime) / 1000, 0.05);
    lastTime = time;

    if (gameState === 'playing' && maze) {
        update(dt);
    }
    if (maze) {
        render();
    }
    hud.updateFps(dt);

    // Hit-flash overlay (server-confirmed stun feedback).
    const hf = document.getElementById('hit-flash');
    if (hf) hf.hidden = hitFlashTimer <= 0 && damageFlashTimer <= 0;

    requestAnimationFrame(frame);
}

function update(dt) {
    input.update();

    // Movement: yaw-relative WASD.
    const speed = input.sprint ? 6.5 : 4.0;
    const fwd = [Math.sin(camera.yaw), 0, Math.cos(camera.yaw)];
    const right = [Math.cos(camera.yaw), 0, -Math.sin(camera.yaw)];

    let dx = (fwd[0] * input.forward + right[0] * input.strafe) * speed * dt;
    let dz = (fwd[2] * input.forward + right[2] * input.strafe) * speed * dt;

    const len = Math.hypot(dx, dz);
    if (len > speed * dt) {
        dx = dx / len * speed * dt;
        dz = dz / len * speed * dt;
    }

    let newX = camera.pos[0] + dx;
    let newZ = camera.pos[2] + dz;
    [newX, newZ] = resolveCollision(maze, newX, newZ, PLAYER_RADIUS);
    camera.pos[0] = newX;
    camera.pos[2] = newZ;

    // Jump and gravity.
    if (input.consumeJump() && grounded) {
        playerVelY = JUMP_VEL;
        grounded = false;
    }
    playerVelY += GRAVITY * dt;
    camera.pos[1] += playerVelY * dt;

    if (camera.pos[1] <= PLAYER_HEIGHT) {
        camera.pos[1] = PLAYER_HEIGHT;
        playerVelY = 0;
        grounded = true;
    }

    // Position reconciliation: lerp toward server position.
    const snap = net.getSnapshot();
    if (snap && snap.data) {
        const sp = snap.data.runner;
        serverPos = [sp.x, PLAYER_HEIGHT, sp.y];
        // Gentle correction: lerp 10% per frame toward server pos.
        const lerpRate = 0.1;
        camera.pos[0] += (serverPos[0] - camera.pos[0]) * lerpRate;
        camera.pos[2] += (serverPos[2] - camera.pos[2]) * lerpRate;

        // Update HUD HP.
        lastHP = sp.hp;
        hud.setHP(sp.hp, MAX_HP);
        hud.setRound(snap.data.round);
        hud.setIntensity(snap.data.intensity);
    }

    // Pulse fire: click to fire (with cooldown).
    const now = performance.now() / 1000;
    if (input.fire && now - lastFireTime > FIRE_COOLDOWN) {
        lastFireTime = now;
        audio.playShot();
        net.sendInput(input.forward, input.strafe, camera.yaw, camera.pitch, input.sprint, false, true);
        projectiles.push({
            pos: [camera.pos[0] + Math.sin(camera.yaw) * 0.5, camera.pos[1] - 0.05, camera.pos[2] + Math.cos(camera.yaw) * 0.5],
            dir: [Math.sin(camera.yaw), 0, Math.cos(camera.yaw)],
            born: now,
            color: [0.2, 1.0, 0.9, 1.0],
        });
        muzzleFlashTimer = 3;
    }
    input.fire = false;

    // Send regular movement input.
    net.sendInput(input.forward, input.strafe, camera.yaw, camera.pitch, input.sprint, false, false);

    // Interpolate minotaur positions from server snapshots.
    if (snap && snap.data) {
        for (const m of snap.data.minotaurs) {
            let buf = minotaurBuffers.get(m.id);
            if (!buf) {
                buf = { pos: [m.x, 0.5, m.y], targetPos: [m.x, 0.5, m.y], stunned: m.stunned, type: m.type|0 };
                minotaurBuffers.set(m.id, buf);
            }
            buf.targetPos = [m.x, 0.5, m.y];
            buf.stunned = m.stunned;
            buf.type = m.type|0;
            buf.lastUpdate = now;
        }
    }

    // Lerp minotaur positions toward targets.
    for (const [id, buf] of minotaurBuffers) {
        const t = Math.min(1, (now - buf.lastUpdate) * 10);
        buf.pos[0] += (buf.targetPos[0] - buf.pos[0]) * t;
        buf.pos[1] += (buf.targetPos[1] - buf.pos[1]) * t;
        buf.pos[2] += (buf.targetPos[2] - buf.pos[2]) * t;
    }

    // Advance projectiles; cull after 0.5s.
    projectiles = projectiles.filter(p => now - p.born < 0.5);
    for (const p of projectiles) {
        p.pos[0] += p.dir[0] * 40 * dt;
        p.pos[1] += p.dir[1] * 40 * dt;
        p.pos[2] += p.dir[2] * 40 * dt;
    }
    if (muzzleFlashTimer > 0) muzzleFlashTimer--;
    if (hitFlashTimer > 0) hitFlashTimer--;
    if (damageFlashTimer > 0) damageFlashTimer--;

    // Audio: proximity heartbeat.
    let nearestDist = Infinity;
    for (const buf of minotaurBuffers.values()) {
        const dx = buf.pos[0] - camera.pos[0];
        const dz = buf.pos[2] - camera.pos[2];
        const d = Math.sqrt(dx*dx + dz*dz);
        if (d < nearestDist) nearestDist = d;
    }
    audio.setHeartbeatProximity(nearestDist);

    // Read-only debug hook for verification and the S4 debugging GUI.
    // Exposes live game state without pausing the game.
    window.__ariadne = {
        get gameState() { return gameState; },
        get playerPos() { return [...camera.pos]; },
        get playerYaw() { return camera.yaw; },
        get hp() { return lastHP; },
        get actors() {
            const out = [];
            for (const [id, buf] of minotaurBuffers) {
                out.push({ id, pos: [...buf.pos], type: buf.type, stunned: buf.stunned });
            }
            return out;
        },
        get actorCount() { return minotaurBuffers.size; },
        get round() { const s = net.getSnapshot(); return s ? s.data.round : -1; },
        get intensity() { const s = net.getSnapshot(); return s ? s.data.intensity : -1; },
        get wsOpen() { return net.ws && net.ws.readyState === WebSocket.OPEN; },
        get mazeSize() { return maze ? [maze.w, maze.h] : null; },
        get gate() { return maze ? [maze.gateX, maze.gateY] : null; },
        get projectileCount() { return projectiles.length; },
        fire() { input.fire = true; },
        setYaw(y) { camera.yaw = y; },
        teleport(x, z) { camera.pos[0] = x; camera.pos[2] = z; },
    };
}

function handleEvent(evt) {
    switch (evt.type) {
        case 'caught':
            gameState = 'caught';
            overlay.hidden = false;
            overlay.textContent = 'CAUGHT\nClick to restart';
            overlay.onclick = () => location.reload();
            break;
        case 'win':
            gameState = 'won';
            overlay.hidden = false;
            overlay.textContent = 'ESCAPED\nClick to play again';
            overlay.onclick = () => location.reload();
            break;
        case 'stunned':
            audio.playStun();
            hitFlashTimer = 8;
            break;
        case 'hit':
            damageFlashTimer = 10;
            break;
        case 'spawn':
            break;
    }
}

function render() {
    renderer.clear();

    const aspect = canvas.width / canvas.height;
    renderer.setProj(camera.projMatrix(aspect));
    renderer.setView(camera.viewMatrix());
    renderer.setCamera(camera.pos);

    const snap = net.getSnapshot();
    const intensity = snap ? snap.data.intensity : 0;
    renderer.setFog(0.08 + intensity * 0.02, [0.04, 0.04, 0.06]);

    const instances = [];
    const wallColor = [0.25, 0.25, 0.38, 1.0];
    const floorColor = [0.08, 0.08, 0.12, 1.0];
    const gateColor = [0.0, 1.0, 0.5, 1.0];
    const stunnedColor = [0.3, 0.3, 0.8, 1.0];

    // Maze geometry from server-provided grid.
    for (let z = 0; z < maze.h; z++) {
        for (let x = 0; x < maze.w; x++) {
            const cell = maze.grid[z * maze.w + x];
            if (cell === 0) {
                instances.push({
                    pos: [x + 0.5, 0.5, z + 0.5],
                    scale: [1, 1, 1],
                    color: wallColor,
                });
            } else {
                instances.push({
                    pos: [x + 0.5, -0.05, z + 0.5],
                    scale: [1, 0.1, 1],
                    color: floorColor,
                });
            }
        }
    }

    // Gate marker.
    instances.push({
        pos: [maze.gateX, 0.5, maze.gateY],
        scale: [0.8, 1.0, 0.8],
        color: gateColor,
    });

    // Actors (interpolated), visually distinct by archetype.
    for (const [id, buf] of minotaurBuffers) {
        let body, head, eye, bodyScale, headY, eyeY;
        switch (buf.type|0) {
            case 1: // swarm imp
                body = buf.stunned ? stunnedColor : [1.0, 0.5, 0.1, 1.0];
                head = body; eye = [1.0, 1.0, 0.0, 1.0];
                bodyScale = [0.3, 0.4, 0.3]; headY = 0.5; eyeY = 0.55;
                break;
            case 2: // caster pillar
                body = buf.stunned ? stunnedColor : [0.7, 0.2, 1.0, 1.0];
                head = [0.9, 0.5, 1.0, 1.0]; eye = [1.0, 0.4, 1.0, 1.0];
                bodyScale = [0.35, 1.0, 0.35]; headY = 1.05; eyeY = 1.1;
                break;
            case 3: // stalker hunter
                body = buf.stunned ? stunnedColor : [0.1, 0.8, 0.7, 1.0];
                head = body; eye = [0.0, 1.0, 1.0, 1.0];
                bodyScale = [0.6, 0.5, 0.6]; headY = 0.6; eyeY = 0.65;
                break;
            default: // brute titan
                body = buf.stunned ? stunnedColor : [0.8, 0.1, 0.1, 1.0];
                head = body; eye = [1.0, 0.0, 0.0, 1.0];
                bodyScale = [0.5, 0.8, 0.5]; headY = 0.8; eyeY = 0.85;
        }
        instances.push({
            pos: [buf.pos[0], buf.pos[1] + bodyScale[1] / 2, buf.pos[2]],
            scale: bodyScale,
            color: body,
        });
        instances.push({
            pos: [buf.pos[0], buf.pos[1] + headY, buf.pos[2]],
            scale: [0.3, 0.3, 0.3],
            color: head,
        });
        instances.push({
            pos: [buf.pos[0] - 0.1, buf.pos[1] + eyeY, buf.pos[2] - 0.15],
            scale: [0.08, 0.08, 0.08],
            color: eye,
        });
        instances.push({
            pos: [buf.pos[0] + 0.1, buf.pos[1] + eyeY, buf.pos[2] - 0.15],
            scale: [0.08, 0.08, 0.08],
            color: eye,
        });
    }

    // Projectile tracers.
    for (const p of projectiles) {
        instances.push({
            pos: [p.pos[0], p.pos[1], p.pos[2]],
            scale: [0.12, 0.12, 0.6],
            color: p.color,
        });
    }

    // Muzzle flash.
    if (muzzleFlashTimer > 0) {
        instances.push({
            pos: [camera.pos[0] + Math.sin(camera.yaw) * 0.6, camera.pos[1] - 0.2, camera.pos[2] + Math.cos(camera.yaw) * 0.6],
            scale: [0.25, 0.25, 0.25],
            color: [1.0, 0.9, 0.4, 1.0],
        });
    }

    renderer.drawInstances(instances);

    // Update minimap with actors.
    if (maze) {
        hud.drawMinimap(maze, camera.pos, camera.yaw, minotaurBuffers);
    }
}

function resolveCollision(mazeData, px, pz, radius) {
    const minX = Math.floor(px - radius);
    const maxX = Math.floor(px + radius);
    const minZ = Math.floor(pz - radius);
    const maxZ = Math.floor(pz + radius);

    for (let cz = minZ; cz <= maxZ; cz++) {
        for (let cx = minX; cx <= maxX; cx++) {
            if (cx < 0 || cz < 0 || cx >= mazeData.w || cz >= mazeData.h) continue;
            if (mazeData.grid[cz * mazeData.w + cx] !== 0) continue;

            const nx = Math.max(cx, Math.min(px, cx + 1));
            const nz = Math.max(cz, Math.min(pz, cz + 1));
            const ddx = px - nx;
            const ddz = pz - nz;
            const dist2 = ddx * ddx + ddz * ddz;

            if (dist2 < radius * radius) {
                if (dist2 > 1e-12) {
                    const dist = Math.sqrt(dist2);
                    px += ddx / dist * (radius - dist);
                    pz += ddz / dist * (radius - dist);
                } else {
                    const left = px - cx, right = cx + 1 - px;
                    const up = pz - cz, down = cz + 1 - pz;
                    const mn = Math.min(left, right, up, down);
                    if (mn === left) px = cx - radius;
                    else if (mn === right) px = cx + 1 + radius;
                    else if (mn === up) pz = cz - radius;
                    else pz = cz + 1 + radius;
                }
            }
        }
    }
    return [px, pz];
}

init();
