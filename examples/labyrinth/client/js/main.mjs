// Main game loop: ties renderer, camera, input, HUD, net together.

import { Renderer } from './gl.mjs';
import { Camera } from './camera.mjs';
import { Input } from './input.mjs';
import { HUD } from './hud.mjs';
import { NetClient } from './net.mjs';
import { AudioEngine } from './audio.mjs';
import { ModelBank, actorModelMatrix } from './models.mjs';
import { DebugPanel } from './debug.mjs';
import { BTHud } from './bt-hud.mjs';
import { ParticleEngine } from './particles.mjs';

const canvas = document.getElementById('glcanvas');
const loading = document.getElementById('loading');
const hudEl = document.getElementById('hud');
const prompt = document.getElementById('pointer-lock-prompt');
const overlay = document.getElementById('overlay');

let renderer, camera, input, hud, net, audio, debugPanel, btHud, particles;
let maze = null; // authoritative maze from server
let lastTime = 0;
let gameState = 'playing'; // 'playing' | 'caught' | 'won'

// Player physics state
let playerVelY = 0;
let grounded = true;
// I2: velocity-based XZ movement for momentum + friction + gun blowback.
let playerVelX = 0;
let playerVelZ = 0;
const GRAVITY = -15;
const JUMP_VEL = 6;
const PLAYER_HEIGHT = 0.5;
const PLAYER_RADIUS = 0.3;
const MAX_HP = 100;
// I2 tuning: friction decay per frame (0.85 = heavy sliding, 0.0 = instant stop).
const FRICTION = 0.82;
// I2: gun blowback force applied on fire (velocity impulse opposite aim).
const BLOWBACK_FORCE = 2.8;

// Minotaur interpolation state
let minotaurBuffers = new Map(); // id -> { pos, targetPos, lastUpdate, type, stunned }

// Projectile state (client-side visual only)
let projectiles = []; // { pos, dir, born, color }

// Pulse fire state
let lastFireTime = 0;
let lastShotElement = -1; // element of the most recent pulse (S6b probe)
const FIRE_COOLDOWN = 0.5; // seconds
let muzzleFlashTimer = 0;
let hitFlashTimer = 0;
let damageFlashTimer = 0; // red flash when player takes damage

// Position reconciliation
let serverPos = null; // last server position for reconciliation
let lastHP = 100; // last known player HP from server

// S2 elemental system: 0=fire 1=ice 2=lightning 3=void, cycle
// fire > ice > lightning > void > fire. Mirrors game/element.go.
const ELEMENT_NAMES = ['Fire', 'Ice', 'Lightning', 'Void'];
const ELEMENT_COLORS = [
    [1.0, 0.35, 0.10], // fire
    [0.35, 0.75, 1.0], // ice
    [1.0, 0.95, 0.25], // lightning
    [0.55, 0.20, 0.85] // void
];
const TERRITORY_TINTS = [
    [0.40, 0.13, 0.06], // fire (NE)
    [0.08, 0.20, 0.35], // ice (NW)
    [0.30, 0.28, 0.05], // lightning (SE)
    [0.17, 0.07, 0.27]  // void (SW)
];
// Per-faction emissive trim (rim glow). Distinct hues so each faction reads
// as a different culture, not just a body-tint swap.
const FACTION_EMISSIVE = [
    [1.0, 0.25, 0.05],  // fire — hot ember rim
    [0.20, 0.85, 1.0],  // ice — cold cyan rim
    [1.0, 0.90, 0.15],  // lightning — electric yellow rim
    [0.75, 0.30, 1.0]   // void — violet arcane rim
];
const FACTION_EMISSIVE_STRENGTH = [0.6, 0.55, 0.7, 0.65]; // per-faction intensity
let playerElement = 0; // active element; power-ups change it
let hitMarkerTimer = 0; // frames the hit marker text stays visible

// S5: territory overlay toggle. When off the floor renders neutral; when on,
// floor tiles glow with the controlling faction's element color.
let territoryOverlayOn = false;

// S5: in-world PA-BT projection threads, updated from tree-frame world views.
let actorThreads = new Map(); // actorID -> { focus: [x,y], element, advantage }

// S3 gang war: live territory control from the server, indexed by quadrant
// (0=NE, 1=NW, 2=SE, 3=SW) with the controlling element as value. Defaults
// to innate control until the first snapshot arrives.
let territoryControl = [0, 1, 2, 3];
let hazards = []; // live crossfire rifts from the snapshot
let clashSparks = []; // transient twin-color flashes at crossfire points
let clashCount = 0;
const ACTOR_MAX_HP = [40, 15, 25, 20]; // mirrors game.actorHP per archetype
const StrategyRaid = 1;
const StrategyRetreat = 2;

// S6a: real skeletal creatures (CC BY 4.0, see assets/PROVENANCE.md).
// Archetype -> base model. Stalkers alternate bat/wasp by ID hash for
// silhouette variety. Models load async; until then actors render as
// fallback cubes.
let modelBank = null;
let modelsReady = false;
const TYPE_MODELS = ['ps1_spider', 'ps1_rat', 'ps1_eaterplant', 'ps1_bat'];
// Per-faction model overrides so the same archetype in different factions
// renders a different silhouette. Index = element (0=fire,1=ice,2=lightning,
// 3=void). null falls back to TYPE_MODELS[type].
const FACTION_MODEL_OVERRIDES = [
    ['ps1_spider', 'ps1_rat',     'ps1_eaterplant', 'ps1_bat'],   // fire
    ['ps1_spider', 'ps1_rat',     'ps1_eaterplant', 'ps1_wasp'],  // ice
    ['ps1_wasp',   'ps1_spider',  'ps1_rat',        'ps1_bat'],   // lightning
    ['ps1_eaterplant', 'ps1_wasp','ps1_spider',     'ps1_rat'],   // void
];
const corpses = []; // { id, pos, yaw, type, element, start }
const MODEL_SCALE_TARGET = 1.5; // world units tall at full scale
let lastRoarAt = 0;
let nextRoarIn = 6;

function actorDist(x, z) {
    return Math.hypot(x - camera.pos[0], z - camera.pos[2]);
}

function hashId(id) {
    let h = 0;
    for (const ch of String(id)) h = (h * 31 + ch.charCodeAt(0)) | 0;
    return h;
}

function modelFor(id, type, element) {
    // Prefer the faction override so two factions never share a silhouette
    // for the same archetype; fall back to the archetype default.
    const ov = FACTION_MODEL_OVERRIDES[element|0];
    if (ov && ov[type|0]) return ov[type|0];
    if ((type|0) === 3) return (hashId(id) & 1) ? 'ps1_wasp' : 'ps1_bat';
    return TYPE_MODELS[type|0] || TYPE_MODELS[0];
}

function hashPhase(id) {
    let h = 0;
    for (const ch of String(id)) h = (h * 31 + ch.charCodeAt(0)) | 0;
    return (h & 7) * 0.37;
}

function stratColorFor(buf) {
    return buf.stunned ? [0.3, 0.3, 0.8, 1.0]
        : (buf.strategy|0) === StrategyRaid ? [1.0, 0.15, 0.05, 1.0]
        : (buf.strategy|0) === StrategyRetreat ? [0.15, 0.9, 1.0, 1.0]
        : [0.35, 0.35, 0.45, 1.0];
}

function init() {
    renderer = new Renderer(canvas);
    camera = new Camera(canvas);
    input = new Input(canvas);
    hud = new HUD();
    net = new NetClient();
    audio = new AudioEngine();
    debugPanel = new DebugPanel();
    debugPanel.onCommand = (cmd, actorID) => net.sendDebugCmd(cmd, actorID);
    debugPanel.getInjectPos = () => maze ? [maze.gateX, maze.gateY] : [camera.pos[0], camera.pos[2]];
    // G3: the in-game BT-tick HUD shares the same tree-frame fan-out as the
    // debug panel so normal play shows live planner activity without Tab.
    btHud = new BTHud();
    // I1: particle engine for projectile trails, muzzle bursts, impact sparks,
    // and death bursts. Renders as GL_POINTS with additive blending.
    particles = new ParticleEngine(renderer.gl);
    net.onTree = (actorID, frame) => {
        debugPanel.handleTreeFrame(actorID, frame);
        btHud.handleTreeFrame(actorID, frame);
        // S5: update in-world projection threads from tree frame world view.
        if (frame.worldView && frame.worldView.hasLastKnown) {
            actorThreads.set(actorID, {
                focus: [frame.worldView.lastKnownX, frame.worldView.lastKnownY],
                element: frame.worldView.selfElement,
                advantage: frame.worldView.atAdvantage ? 1 : frame.worldView.atDisadvantage ? -1 : 0,
            });
        } else {
            actorThreads.delete(actorID);
        }
    };

    // S5: territory overlay toggle via KeyT.
    document.addEventListener('keydown', (e) => {
        if (e.code === 'KeyT') territoryOverlayOn = !territoryOverlayOn;
    });

    // Skeletal creature models; fallback primitives until they arrive.
    modelBank = new ModelBank(renderer.gl);
    Promise.all([
        modelBank.load('ps1_spider', 'assets/ps1_spider.glb'),
        modelBank.load('ps1_rat', 'assets/ps1_rat.glb'),
        modelBank.load('ps1_eaterplant', 'assets/ps1_eaterplant.glb'),
        modelBank.load('ps1_bat', 'assets/ps1_bat.glb'),
        modelBank.load('ps1_wasp', 'assets/ps1_wasp.glb'),
    ]).then(() => { modelsReady = true; }).catch((err) => {
        modelsReady = false;
        modelBank = null;
        console.warn('models unavailable, falling back to primitives:', err);
    });

    // H4: Show tutorial on first load.
    const tutorialSeen = sessionStorage.getItem('ariadne-tutorial');
    if (!tutorialSeen) {
        const tutorial = document.getElementById('tutorial-overlay');
        if (tutorial) {
            tutorial.hidden = false;
            const dismissTutorial = () => {
                tutorial.hidden = true;
                sessionStorage.setItem('ariadne-tutorial', '1');
                document.removeEventListener('keydown', dismissOnEnter);
            };
            const dismissOnEnter = (e) => {
                if (e.code === 'Enter') dismissTutorial();
            };
            tutorial.addEventListener('click', dismissTutorial);
            document.addEventListener('keydown', dismissOnEnter);
        }
    }

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

    // HUD updates that must continue on the caught/win overlay: HP, round,
    // and intensity are driven by the latest snapshot regardless of gameState.
    // They sit outside update() so the death screen shows final HP, not a
    // frozen value from the last playing frame.
    const snap = net.getSnapshot();
    if (snap && snap.data) {
        const sp = snap.data.runner;
        lastHP = sp.hp;
        hud.setHP(sp.hp, MAX_HP);
        hud.setRound(snap.data.round);
        hud.setIntensity(snap.data.intensity);
        if (sp.element !== undefined) {
            playerElement = sp.element | 0;
            hud.setElement(playerElement);
        }

        // H4: Update objective chip based on gate proximity.
        const objectiveChip = document.getElementById('chip-objective');
        if (objectiveChip && maze) {
            const gateDistChip = Math.hypot(sp.x - maze.gateX, sp.y - maze.gateY);
            if (gateDistChip < 5) {
                objectiveChip.textContent = 'ENTER THE GATE';
                objectiveChip.style.animation = 'pulse-text 0.8s ease-in-out infinite';
            } else {
                objectiveChip.textContent = 'ESCAPE THE LABYRINTH';
                objectiveChip.style.animation = '';
            }
        }
    }

    hud.updateFps(dt);

    // Hit-flash overlay (server-confirmed stun feedback).
    const hf = document.getElementById('hit-flash');
    if (hf) hf.hidden = hitFlashTimer <= 0 && damageFlashTimer <= 0;

    requestAnimationFrame(frame);
}

function update(dt) {
    input.update();
    const maxSpeed = input.sprint ? 6.5 : 4.0;
    camera.updateMovement(dt, Math.hypot(input.forward, input.strafe) * maxSpeed);

    // I2: velocity-based movement with momentum and friction.
    // Movement basis: yaw-relative WASD.
    const fwd = [Math.sin(camera.yaw), 0, Math.cos(camera.yaw)];
    const right = [-Math.cos(camera.yaw), 0, Math.sin(camera.yaw)];

    // Desired velocity from input.
    const wantX = (fwd[0] * input.forward + right[0] * input.strafe) * maxSpeed;
    const wantZ = (fwd[2] * input.forward + right[2] * input.strafe) * maxSpeed;

    // Acceleration: when input is present, lerp toward target velocity quickly;
    // otherwise friction decays existing velocity (momentum / sliding).
    const hasInput = Math.hypot(input.forward, input.strafe) > 0.01;
    if (hasInput) {
        // Tight response: ~95% of target within 0.1s.
        const a = 1 - Math.pow(0.001, dt);
        playerVelX += (wantX - playerVelX) * a;
        playerVelZ += (wantZ - playerVelZ) * a;
    } else {
        const decay = Math.pow(FRICTION, dt * 60);
        playerVelX *= decay;
        playerVelZ *= decay;
        // Snap to zero below threshold to avoid perpetual drift.
        if (Math.abs(playerVelX) < 0.01) playerVelX = 0;
        if (Math.abs(playerVelZ) < 0.01) playerVelZ = 0;
    }

    // Clamp total speed so impulses (blowback) can briefly exceed maxSpeed
    // but sustained motion is capped.
    const spd = Math.hypot(playerVelX, playerVelZ);
    const cap = maxSpeed * 1.6; // allow blowback overshoot up to 1.6x
    if (spd > cap) {
        playerVelX = playerVelX / spd * cap;
        playerVelZ = playerVelZ / spd * cap;
    }

    const prevX = camera.pos[0], prevZ = camera.pos[2];
    let newX = prevX + playerVelX * dt;
    let newZ = prevZ + playerVelZ * dt;
    [newX, newZ] = resolveCollision(maze, newX, newZ, PLAYER_RADIUS);
    // If collision clamped motion, zero velocity along the blocked axis so
    // momentum doesn't re-push the player into the wall next frame.
    const actualDx = newX - prevX;
    const actualDz = newZ - prevZ;
    if (Math.abs(actualDx) < Math.abs(playerVelX * dt) * 0.5) playerVelX *= 0.2;
    if (Math.abs(actualDz) < Math.abs(playerVelZ * dt) * 0.5) playerVelZ *= 0.2;
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

        // Gang war state: dynamic territory control and crossfire rifts.
        if (snap.data.territoryControl) territoryControl = snap.data.territoryControl;
        hazards = snap.data.hazards || [];

        // Terminal phase fallback: the authoritative phase rides every
        // snapshot, so a dropped event message can never wedge the win/lose
        // overlays (proto phase: 0 running, 1 won, 2 lost).
        if (gameState === 'playing' && snap.data.phase === 1) {
            handleEvent({type: 'win'});
        } else if (gameState === 'playing' && snap.data.phase === 2) {
            handleEvent({type: 'caught'});
        }
    }

    // Pulse fire: click to fire (with cooldown).
    const now = performance.now() / 1000;
    if (input.fire && now - lastFireTime > FIRE_COOLDOWN) {
        lastFireTime = now;
        lastShotElement = playerElement;
        audio.playShot(playerElement);
        // Send authoritative aim direction (raw pitch, no recoil) to server
        const aimYaw = camera.yaw;
        const aimPitch = camera.pitch;
        net.sendInput(input.forward, input.strafe, aimYaw, aimPitch, input.sprint, false, true);
        // Visual tracer uses render direction (with recoil) for immediate feedback
        const rd = camera.renderDir();
        const ec = ELEMENT_COLORS[playerElement];
        projectiles.push({
            pos: [camera.pos[0] + rd[0] * 0.5, camera.pos[1] + rd[1] * 0.5 - 0.05, camera.pos[2] + rd[2] * 0.5],
            dir: [rd[0], rd[1], rd[2]],
            born: now,
            color: [Math.min(1, ec[0] + 0.35), Math.min(1, ec[1] + 0.35), Math.min(1, ec[2] + 0.35), 1.0],
        });
        muzzleFlashTimer = 3;
        // I1: muzzle burst at weapon position.
        if (particles) {
            const muzzPos = [camera.pos[0] + rd[0] * 0.6, camera.pos[1] + rd[1] * 0.6 - 0.2, camera.pos[2] + rd[2] * 0.6];
            particles.muzzleBurst(muzzPos, rd, ec);
        }
        // I2: gun blowback — impulse player velocity opposite to aim (XZ only;
        // vertical aim component ignored so shots don't launch the player).
        playerVelX -= rd[0] * BLOWBACK_FORCE;
        playerVelZ -= rd[2] * BLOWBACK_FORCE;
        // Apply recoil AFTER firing so the shot itself is accurate
        camera.applyRecoil(0.02);
        camera.kickFov(3);
    }
    input.fire = false;

    // H2: Crosshair aim feedback — raycast from camera forward, turn crosshair
    // red if it intersects an actor within 12u.
    const crosshairEl = document.getElementById('crosshair');
    if (crosshairEl) {
        const aimDir = camera.aimDir();
        let aimLocked = false;
        for (const [id, buf] of minotaurBuffers) {
            if (buf.stunned || buf.hp <= 0) continue;
            const dx = buf.pos[0] - camera.pos[0];
            const dz = buf.pos[2] - camera.pos[2];
            const dist = Math.hypot(dx, dz);
            if (dist > 12 || dist < 0.5) continue;
            const toActorX = dx / dist;
            const toActorZ = dz / dist;
            const dot = aimDir[0] * toActorX + aimDir[2] * toActorZ;
            if (dot > 0.95) {
                aimLocked = true;
                break;
            }
        }
        crosshairEl.classList.toggle('aim-locked', aimLocked);
    }

    // Send regular movement input with authoritative aim (no recoil).
    net.sendInput(input.forward, input.strafe, camera.yaw, camera.pitch, input.sprint, false, false);

    // Interpolate minotaur positions from server snapshots.
    if (snap && snap.data) {
        for (const m of snap.data.minotaurs) {
            let buf = minotaurBuffers.get(m.id);
            if (!buf) {
                buf = { pos: [m.x, 0.5, m.y], targetPos: [m.x, 0.5, m.y], stunned: m.stunned, type: m.type|0, element: m.element|0, hp: m.hp|0, strategy: m.strategy|0 };
                minotaurBuffers.set(m.id, buf);
            }
            buf.targetPos = [m.x, 0.5, m.y];
            buf.stunned = m.stunned;
            buf.type = m.type|0;
            buf.element = m.element|0;
            buf.hp = m.hp|0;
            buf.strategy = m.strategy|0;
            buf.lastUpdate = now;
        }
        // G3: keep the BT HUD rows synced with the live roster so a dead
        // actor drops immediately instead of lingering on its last frame.
        if (btHud) btHud.pruneToRoster(snap.data.minotaurs.map(m => m.id));
    }

    // Lerp minotaur positions toward targets.
    for (const [id, buf] of minotaurBuffers) {
        const t = Math.min(1, (now - buf.lastUpdate) * 10);
        buf.pos[0] += (buf.targetPos[0] - buf.pos[0]) * t;
        buf.pos[1] += (buf.targetPos[1] - buf.pos[1]) * t;
        buf.pos[2] += (buf.targetPos[2] - buf.pos[2]) * t;
        // Facing and gait: derive yaw from actual displacement so creatures
        // look where they walk.
        const px = buf.prevPos ? buf.pos[0] - buf.prevPos[0] : 0;
        const pz = buf.prevPos ? buf.pos[2] - buf.prevPos[2] : 0;
        buf.step = Math.hypot(px, pz);
        if (buf.step > 0.002) buf.yaw = Math.atan2(px, pz);
        buf.prevPos = [buf.pos[0], buf.pos[1], buf.pos[2]];
    }

    // Prune buffers for actors the server removed (killed in the gang war).
    // The death event prunes immediately; this backstop covers dropped
    // events and keeps stale IDs from resurrecting ghosts.
    for (const [id, buf] of minotaurBuffers) {
        if (now - buf.lastUpdate > 2) minotaurBuffers.delete(id);
    }

    // Advance projectiles; cull after 0.5s. Clash sparks fade over 0.4s.
    projectiles = projectiles.filter(p => now - p.born < 0.5);
    clashSparks = clashSparks.filter(s => now - s.born < 0.4);
    for (const p of projectiles) {
        p.pos[0] += p.dir[0] * 40 * dt;
        p.pos[1] += p.dir[1] * 40 * dt;
        p.pos[2] += p.dir[2] * 40 * dt;
        // I1: trail particle behind each projectile every frame.
        if (particles) particles.trail(p.pos, p.color);
    }
    // I1: advance particle simulation.
    if (particles) particles.update(dt);
    if (muzzleFlashTimer > 0) muzzleFlashTimer--;
    if (hitFlashTimer > 0) hitFlashTimer--;
    if (damageFlashTimer > 0) damageFlashTimer--;
    if (hitMarkerTimer > 0) {
        hitMarkerTimer--;
        if (hitMarkerTimer === 0) {
            const marker = document.getElementById('hit-marker');
            if (marker) marker.hidden = true;
        }
    }

    // Audio: proximity heartbeat.
    let nearestDist = Infinity;
    for (const buf of minotaurBuffers.values()) {
        const dx = buf.pos[0] - camera.pos[0];
        const dz = buf.pos[2] - camera.pos[2];
        const d = Math.sqrt(dx*dx + dz*dz);
        if (d < nearestDist) nearestDist = d;
    }
    audio.setHeartbeatProximity(nearestDist);
    if (now - lastRoarAt > nextRoarIn) {
        lastRoarAt = now;
        nextRoarIn = 5 + Math.random() * 7;
        if (nearestDist < 14) audio.playRoar(nearestDist);
    }

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
                out.push({ id, pos: [...buf.pos], type: buf.type, stunned: buf.stunned, element: buf.element, hp: buf.hp, strategy: buf.strategy, attackUntil: buf.attackUntil || 0, hurtUntil: buf.hurtUntil || 0 });
            }
            return out;
        },
        get hazards() { return hazards.map(h => ({ x: h.x, y: h.y, element: h.element, ttl: h.ttl })); },
        get territoryControl() { return [...territoryControl]; },
        get clashCount() { return clashCount; },
        get actorCount() { return minotaurBuffers.size; },
        get actorPositions() { return Array.from(minotaurBuffers.values()).map(b => [b.pos[0], b.pos[2]]); },
        get playerPosition() { return camera.pos.slice(); },
        get round() { const s = net.getSnapshot(); return s ? s.data.round : -1; },
        get intensity() { const s = net.getSnapshot(); return s ? s.data.intensity : -1; },
        get wsOpen() { return net.ws && net.ws.readyState === WebSocket.OPEN; },
        get mazeSize() { return maze ? [maze.w, maze.h] : null; },
        get gate() { return maze ? [maze.gateX, maze.gateY] : null; },
        get projectileCount() { return projectiles.length; },
        get particleCount() { return particles ? particles.count : 0; },
        get playerVel() { return [playerVelX, playerVelY, playerVelZ]; },
        get playerElement() { return playerElement; },
        get powerUps() {
            const s = net.getSnapshot();
            return s && s.data.powerUps ? s.data.powerUps.filter(p => p.active).map(p => ({x: p.x, y: p.y, element: p.element})) : [];
        },
        get modelsLoaded() { return modelsReady; },
        get corpseCount() { return corpses.length; },
        get audioState() { return audio && audio.ctx ? audio.ctx.state : null; },
        get lastShotElement() { return lastShotElement; },
        get territoryOverlayOn() { return territoryOverlayOn; },
        get actorThreadCount() { return actorThreads.size; },
        get actorThreads() {
            const out = [];
            for (const [id, t] of actorThreads) out.push({ id, focus: [...t.focus], element: t.element, advantage: t.advantage });
            return out;
        },
        fire() { input.fire = true; },
        setYaw(y) { camera.yaw = y; },
        teleport(x, z) { camera.pos[0] = x; camera.pos[2] = z; net.sendDebugTeleport(x, z); },
        // G3: BT-tick HUD state for verification (__ariadne.btHud).
        get btHud() { return btHud ? btHud.probe() : null; },
        toggleBTHud() { if (btHud) btHud.toggle(); },
        // H1 probes for weapon + movement juice verification.
        get cameraFov() { return camera.currentFovDeg; },
        get cameraRecoil() { return camera.recoilPitch; },
        get cameraBob() { return [camera.bobOffsetX, camera.bobOffsetY]; },
        get weaponVisible() { return true; }, // weapon always renders when modelsReady
    };
}

// H2: Project world position to screen pixel coordinates.
function worldToScreen(wx, wy, wz) {
    const view = camera.viewMatrix();
    const proj = camera.projMatrix(canvas.width / canvas.height);
    const vx = view[0]*wx + view[4]*wy + view[8]*wz + view[12];
    const vy = view[1]*wx + view[5]*wy + view[9]*wz + view[13];
    const vz = view[2]*wx + view[6]*wy + view[10]*wz + view[14];
    const vw = view[3]*wx + view[7]*wy + view[11]*wz + view[15];
    const cx = proj[0]*vx + proj[4]*vy + proj[8]*vz + proj[12]*vw;
    const cy = proj[1]*vx + proj[5]*vy + proj[9]*vz + proj[13]*vw;
    const cw = proj[3]*vx + proj[7]*vy + proj[11]*vz + proj[15]*vw;
    if (cw <= 0.01) return null;
    return [(cx/cw * 0.5 + 0.5) * canvas.clientWidth,
            (1 - (cy/cw * 0.5 + 0.5)) * canvas.clientHeight];
}

// H2: Floating damage number at an actor's screen-projected position.
function spawnDamageNumber(worldX, worldZ, damage, advantage) {
    const sp = worldToScreen(worldX, 0.8, worldZ);
    const el = document.createElement('div');
    el.className = 'damage-number';
    if (advantage >= 2) {
        el.classList.add('advantage');
        el.textContent = damage + ' x2';
    } else if (advantage > 0 && advantage < 1) {
        el.classList.add('disadvantage');
        el.textContent = damage + ' resisted';
    } else {
        el.classList.add('neutral');
        el.textContent = String(damage);
    }
    if (sp) {
        el.style.left = sp[0] + 'px';
        el.style.top = sp[1] + 'px';
    } else {
        // Fallback: actor is off-screen or behind camera (e.g. after
        // knockback pushed it out of the frustum). Show at screen center
        // so the damage feedback is never silently dropped.
        el.style.left = '50%';
        el.style.top = '40%';
    }
    document.body.appendChild(el);
    setTimeout(() => el.remove(), 1200);
}

// H2: Kill feed entry.
const FACTION_NAMES = ['Ember Legion', 'Frost Covenant', 'Storm Pact', 'Void Cult'];
const TYPE_NAMES = ['Brute', 'Swarm', 'Caster', 'Stalker'];
function addKillEntry(element, type) {
    const feed = document.getElementById('kill-feed');
    if (!feed) return;
    const entry = document.createElement('div');
    entry.className = 'kill-entry';
    entry.style.borderColor = FACTION_COLORS[element] || '#ffffff';
    entry.textContent = (FACTION_NAMES[element] || 'Unknown') + ' ' + (TYPE_NAMES[type] || 'hunter') + ' eliminated';
    feed.appendChild(entry);
    while (feed.children.length > 5) feed.removeChild(feed.firstChild);
    setTimeout(() => entry.remove(), 5000);
}

// H2: Screen shake state.
let screenShakeTimer = 0;
let screenShakeIntensity = 0;

// H2: Trigger a screen shake of given intensity for duration seconds.
// Applied per-frame in render() by offsetting camera.pos.
function applyScreenShake(intensity, duration) {
    screenShakeIntensity = intensity;
    screenShakeTimer = duration;
}
// H2: Expose for E2E verification. ES module scope is not accessible from
// page.evaluate(), so the scenario's typeof check needs these on window.
window.applyScreenShake = applyScreenShake;
Object.defineProperty(window, 'screenShakeTimer', {
    get: () => screenShakeTimer,
    configurable: true,
});

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
        case 'stunned': {
            audio.playStun();
            const sb = evt.data && evt.data.minotaur ? minotaurBuffers.get(evt.data.minotaur) : null;
            // Show the elemental outcome of the pulse.
            const adv = evt.data && evt.data.advantage;
            if (sb) {
                sb.hurtUntil = performance.now() / 1000 + 0.6;
                audio.playHitReaction(actorDist(sb.pos[0], sb.pos[2]));
                // H2: floating damage number at the hit actor's position.
                const dmgVal = (evt.data && typeof evt.data.damage === 'number') ? evt.data.damage : 0;
                if (dmgVal > 0) spawnDamageNumber(sb.pos[0], sb.pos[2], dmgVal, adv);
                // I1: impact sparks at the hit actor's position.
                if (particles) {
                    const elemIdx = (evt.data && evt.data.element !== undefined) ? (evt.data.element|0) : playerElement;
                    const sparkColor = ELEMENT_COLORS[elemIdx] || ELEMENT_COLORS[playerElement];
                    particles.impactSparks([sb.pos[0], sb.pos[1] + 0.4, sb.pos[2]], sparkColor);
                }
            }
            hitFlashTimer = 8;
            const marker = document.getElementById('hit-marker');
            if (marker) {
                if (adv >= 2) {
                    marker.textContent = 'ADVANTAGE ×2';
                    marker.className = 'advantage';
                } else if (adv > 0 && adv < 1) {
                    marker.textContent = 'resisted';
                    marker.className = 'disadvantage';
                } else {
                    marker.textContent = 'hit';
                    marker.className = 'neutral';
                }
                marker.hidden = false;
                hitMarkerTimer = 30;
            }
            break;
        }
        case 'powerup':
            audio.playStun();
            hitFlashTimer = 6;
            break;
        case 'clash':
            clashCount++;
            if (evt.data) {
                audio.playClash(actorDist(evt.data.x, evt.data.y));
                for (const buf of minotaurBuffers.values()) {
                    const dx = buf.pos[0] - evt.data.x, dz = buf.pos[2] - evt.data.y;
                    if (dx*dx + dz*dz < 2.2 * 2.2) buf.attackUntil = performance.now() / 1000 + 0.7;
                }
                clashSparks.push({
                    pos: [evt.data.x, 0.6, evt.data.y],
                    born: performance.now() / 1000,
                    colorA: ELEMENT_COLORS[evt.data.a|0] || ELEMENT_COLORS[0],
                    colorB: ELEMENT_COLORS[evt.data.b|0] || ELEMENT_COLORS[0],
                });
            }
            hud.setWar(clashCount);
            break;
        case 'death': {
            const buf = evt.data && evt.data.id ? minotaurBuffers.get(evt.data.id) : null;
            if (buf) {
                corpses.push({ id: evt.data.id, pos: [...buf.pos], yaw: buf.yaw || 0, type: buf.type, element: buf.element, start: performance.now() / 1000 });
                audio.playDeath(actorDist(buf.pos[0], buf.pos[2]));
                // I1: death burst at corpse position.
                if (particles) {
                    const ec = ELEMENT_COLORS[buf.element|0] || ELEMENT_COLORS[0];
                    particles.deathBurst([buf.pos[0], buf.pos[1] + 0.4, buf.pos[2]], ec);
                }
                minotaurBuffers.delete(evt.data.id);
            }
            break;
        }
        case 'hit':
            damageFlashTimer = 10;
            break;
        case 'spawn':
            // H5: Wave announcement banner.
            if (evt.data && evt.data.intensity !== undefined) {
                const banner = document.getElementById('wave-banner');
                if (banner) {
                    banner.textContent = 'WAVE ' + evt.data.count + ' — INTENSITY ' + evt.data.intensity;
                    banner.hidden = false;
                    // Re-trigger animation by removing and re-adding the element.
                    banner.style.animation = 'none';
                    banner.offsetHeight; // force reflow
                    banner.style.animation = '';
                    setTimeout(() => { banner.hidden = true; }, 3000);
                }
            }
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
    if (modelsReady) {
        modelBank.setViewProj(camera.viewMatrix(), camera.projMatrix(aspect), camera.pos, 0.08 + intensity * 0.02, [0.04, 0.04, 0.06]);
    }

    const instances = [];
    const wallColor = [0.25, 0.25, 0.38, 1.0];
    const floorColor = [0.08, 0.08, 0.12, 1.0];
    const gateColor = [0.0, 1.0, 0.5, 1.0];
    const stunnedColor = [0.3, 0.3, 0.8, 1.0];

    // Maze geometry from server-provided grid. Floor tiles are tinted by
    // the controlling faction's territory quadrant.
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
                // S5: territory overlay toggle. When off, floor is neutral;
                // when on, floor glows with the controlling faction color.
                let floorCol;
                if (territoryOverlayOn) {
                    const t = TERRITORY_TINTS[territoryControl[territoryAt(x, z, maze)]|0] || TERRITORY_TINTS[0];
                    floorCol = [
                        floorColor[0] + t[0] * 2.5,
                        floorColor[1] + t[1] * 2.5,
                        floorColor[2] + t[2] * 2.5,
                        1.0,
                    ];
                } else {
                    floorCol = [floorColor[0], floorColor[1], floorColor[2], 1.0];
                }
                instances.push({
                    pos: [x + 0.5, -0.05, z + 0.5],
                    scale: [1, 0.1, 1],
                    color: floorCol,
                });
            }
        }
    }

    // Gate marker.
    const gateDist = Math.hypot(camera.pos[0] - maze.gateX, camera.pos[2] - maze.gateY);
    const gatePulse = gateDist < 5 ? 0.7 + Math.sin(performance.now() / 150) * 0.3 : 1.0;
    instances.push({
        pos: [maze.gateX, 0.5, maze.gateY],
        scale: [0.8, 1.0, 0.8],
        color: [gateColor[0] * gatePulse, gateColor[1] * gatePulse, gateColor[2] * gatePulse, gateColor[3]],
    });

    // H4: 3D gate compass — tall beacon visible from anywhere.
    const compassHeight = 3.0;
    instances.push({
        pos: [maze.gateX, compassHeight / 2, maze.gateY],
        scale: [0.15, compassHeight, 0.15],
        color: [0, 1, 0.5, 0.6],
    });
    // Compass arrow pointing toward gate from player.
    const gdx = maze.gateX - camera.pos[0];
    const gdz = maze.gateY - camera.pos[2];
    const gDist = Math.hypot(gdx, gdz);
    if (gDist > 2) {
        const arrowDist = 1.5;
        const arrowX = camera.pos[0] + (gdx / gDist) * arrowDist;
        const arrowZ = camera.pos[2] + (gdz / gDist) * arrowDist;
        instances.push({
            pos: [arrowX, 1.2, arrowZ],
            scale: [0.2, 0.2, 0.2],
            color: [0, 1, 0.5, 0.8],
        });
    }

    // Elemental power-up orbs (only active ones).
    if (snap && snap.data.powerUps) {
        const bob = Math.sin(performance.now() / 300) * 0.08;
        for (const pu of snap.data.powerUps) {
            if (!pu.active) continue;
            const ec = ELEMENT_COLORS[pu.element|0] || ELEMENT_COLORS[0];
            instances.push({
                pos: [pu.x, 0.55 + bob, pu.y],
                scale: [0.3, 0.3, 0.3],
                color: [ec[0], ec[1], ec[2], 1.0],
            });
        }
    }

    // Crossfire rifts: pulsing elemental hazards left by faction clashes.
    // These are the gang war's dynamic obstacles.
    const nowMs = performance.now();
    for (const h of hazards) {
        const ec = ELEMENT_COLORS[h.element|0] || ELEMENT_COLORS[0];
        const pulse = 0.75 + Math.sin(nowMs / 120) * 0.25;
        const fade = Math.min(1, (h.ttl|0) / 120 + 0.25);
        instances.push({
            pos: [h.x, 0.12, h.y],
            scale: [0.9 * pulse, 0.12, 0.9 * pulse],
            color: [ec[0] * fade, ec[1] * fade, ec[2] * fade, 1.0],
        });
        instances.push({
            pos: [h.x, 0.55, h.y],
            scale: [0.12, 0.9 * pulse, 0.12],
            color: [Math.min(1, ec[0] + 0.3), Math.min(1, ec[1] + 0.3), Math.min(1, ec[2] + 0.3), 1.0],
        });
    }

    // Clash sparks: brief twin-color flash where two factions crossed fire.
    const nowSec = nowMs / 1000;
    for (const s of clashSparks) {
        const grow = 0.2 + (nowSec - s.born) * 2.2;
        instances.push({
            pos: [s.pos[0], s.pos[1] + 0.1, s.pos[2]],
            scale: [grow, 0.08, grow],
            color: [s.colorA[0], s.colorA[1], s.colorA[2], 1.0],
        });
        instances.push({
            pos: [s.pos[0], s.pos[1] + 0.25, s.pos[2]],
            scale: [grow * 0.6, 0.06, grow * 0.6],
            color: [s.colorB[0], s.colorB[1], s.colorB[2], 1.0],
        });
    }

    // S5: In-world PA-BT projection: glowing floor threads from each actor
    // to its current focus (lastKnownPos). Threads pulse and use the actor's
    // element color; advantage actors glow brighter, disadvantage dimmer.
    for (const [id, thread] of actorThreads) {
        const buf = minotaurBuffers.get(id);
        if (!buf) continue;
        const ec = ELEMENT_COLORS[thread.element|0] || ELEMENT_COLORS[0];
        const brightness = thread.advantage > 0 ? 1.0 : thread.advantage < 0 ? 0.4 : 0.7;
        const pulse = 0.6 + Math.sin(nowMs / 200 + buf.pos[0] * 3) * 0.4;
        const ax = buf.pos[0], az = buf.pos[2];
        const fx = thread.focus[0], fz = thread.focus[1];
        const dx = fx - ax, dz = fz - az;
        const dist = Math.hypot(dx, dz);
        if (dist < 0.5) continue;
        const steps = Math.min(12, Math.max(3, Math.floor(dist * 2)));
        for (let i = 0; i < steps; i++) {
            const t = i / steps;
            const px = ax + dx * t;
            const pz = az + dz * t;
            const fade = brightness * pulse * (0.3 + 0.7 * t);
            instances.push({
                pos: [px, 0.02, pz],
                scale: [0.15, 0.04, 0.15],
                color: [ec[0] * fade, ec[1] * fade, ec[2] * fade, 1.0],
            });
        }
        // Focus marker: small bright beacon at the target position.
        instances.push({
            pos: [fx, 0.05, fz],
            scale: [0.25, 0.1, 0.25],
            color: [Math.min(1, ec[0] + 0.3) * brightness, Math.min(1, ec[1] + 0.3) * brightness, Math.min(1, ec[2] + 0.3) * brightness, 1.0],
        });
    }

    // Actors (interpolated): real skeletal creatures once models are loaded,
    // fallback primitives otherwise. Body tint is the faction element so each
    // of the four factions reads at a glance.
    const actNow = performance.now() / 1000;
    for (const [id, buf] of minotaurBuffers) {
        const ec = ELEMENT_COLORS[buf.element|0] || ELEMENT_COLORS[0];
        // Wounded actors dim toward black so battle damage reads at a glance.
        const hpFrac = Math.max(0, Math.min(1, (buf.hp === undefined ? 1 : buf.hp) / (ACTOR_MAX_HP[buf.type|0] || 30)));
        const dim = 0.45 + 0.55 * hpFrac;
        const tint = buf.stunned ? stunnedColor : [ec[0] * dim, ec[1] * dim, ec[2] * dim, 1.0];
        const model = modelsReady ? modelBank.models.get(modelFor(id, buf.type, buf.element)) : null;
        if (model) {
            let anim = 'idle';
            if (buf.stunned || (buf.hurtUntil || 0) > actNow) anim = 'hurt';
            else if ((buf.attackUntil || 0) > actNow) anim = 'attack';
            else if ((buf.step || 0) > 0.002) anim = 'move';
            const animName = ModelBank.pickAnim(model, anim) || ModelBank.pickAnim(model, 'idle');
            const scale = MODEL_SCALE_TARGET / model.height;
            const mm = new Float32Array(16);
            actorModelMatrix(buf.pos, buf.yaw || 0, scale, -buf.pos[1] - model.minY * scale, mm);
            const fae = FACTION_EMISSIVE[buf.element|0] || FACTION_EMISSIVE[0];
            const fas = FACTION_EMISSIVE_STRENGTH[buf.element|0] || 0.5;
            modelBank.draw(model, animName, actNow + hashPhase(id), mm, tint, true, fae, fas);
        } else {
            let bodyScale, headY, eyeY;
            switch (buf.type|0) {
                case 1: bodyScale = [0.3, 0.4, 0.3]; headY = 0.5; eyeY = 0.55; break;
                case 2: bodyScale = [0.35, 1.0, 0.35]; headY = 1.05; eyeY = 1.1; break;
                case 3: bodyScale = [0.6, 0.5, 0.6]; headY = 0.6; eyeY = 0.65; break;
                default: bodyScale = [0.5, 0.8, 0.5]; headY = 0.8; eyeY = 0.85;
            }
            instances.push({ pos: [buf.pos[0], buf.pos[1] + bodyScale[1] / 2, buf.pos[2]], scale: bodyScale, color: tint });
            instances.push({ pos: [buf.pos[0], buf.pos[1] + headY, buf.pos[2]], scale: [0.3, 0.3, 0.3], color: buf.stunned ? stunnedColor : [Math.min(1, ec[0] + 0.2), Math.min(1, ec[1] + 0.2), Math.min(1, ec[2] + 0.2), 1.0] });
            instances.push({ pos: [buf.pos[0] - 0.1, buf.pos[1] + eyeY, buf.pos[2] - 0.15], scale: [0.08, 0.08, 0.08], color: [1, 1, 1, 1] });
            instances.push({ pos: [buf.pos[0] + 0.1, buf.pos[1] + eyeY, buf.pos[2] - 0.15], scale: [0.08, 0.08, 0.08], color: [1, 1, 1, 1] });
        }
        // H2: Enemy HP bar billboard above wounded actors.
        if (buf.hp !== undefined && buf.hp > 0 && buf.hp < (ACTOR_MAX_HP[buf.type|0] || 30)) {
            const barW = 0.8;
            const barH = 0.08;
            const barY = buf.pos[1] + MODEL_SCALE_TARGET + 0.6;
            instances.push({
                pos: [buf.pos[0], barY, buf.pos[2]],
                scale: [barW, barH, 0.02],
                color: [0.1, 0.1, 0.1, 0.85],
            });
            const fillW = barW * hpFrac;
            const fillColor = hpFrac > 0.5 ? [0, 1, 0.3, 1] : hpFrac > 0.25 ? [1, 0.8, 0, 1] : [1, 0.2, 0.2, 1];
            instances.push({
                pos: [buf.pos[0] - (barW - fillW) / 2, barY, buf.pos[2] + 0.01],
                scale: [fillW, barH, 0.03],
                color: fillColor,
            });
        }

        // Strategy beacon: red=raid, cyan=retreat, dim grey=patrol.
        instances.push({
            pos: [buf.pos[0], buf.pos[1] + MODEL_SCALE_TARGET + 0.35, buf.pos[2]],
            scale: [0.12, 0.12, 0.12],
            color: stratColorFor(buf),
        });
    }

    // Corpses: play the death clip once, then leave the world.
    for (let i = corpses.length - 1; i >= 0; i--) {
        const c = corpses[i];
        const age = actNow - c.start;
        const model = modelsReady ? modelBank.models.get(modelFor(c.id, c.type, c.element)) : null;
        if (age > 2.5 || !model) { corpses.splice(i, 1); continue; }
        const ec = ELEMENT_COLORS[c.element|0] || ELEMENT_COLORS[0];
        const scale = MODEL_SCALE_TARGET / model.height;
        const mm = new Float32Array(16);
        actorModelMatrix(c.pos, c.yaw, scale, -c.pos[1] - model.minY * scale, mm);
        modelBank.draw(model, ModelBank.pickAnim(model, 'death'), age, mm, [ec[0] * 0.35, ec[1] * 0.35, ec[2] * 0.35, 1.0], false);
    }

    // Projectile tracers.
    for (const p of projectiles) {
        instances.push({
            pos: [p.pos[0], p.pos[1], p.pos[2]],
            scale: [0.12, 0.12, 0.6],
            color: p.color,
        });
    }

    // Muzzle flash in the active element color (uses render direction with recoil).
    if (muzzleFlashTimer > 0) {
        const ec = ELEMENT_COLORS[playerElement];
        const rd = camera.renderDir();
        instances.push({
            pos: [camera.pos[0] + rd[0] * 0.6, camera.pos[1] + rd[1] * 0.6 - 0.2, camera.pos[2] + rd[2] * 0.6],
            scale: [0.25, 0.25, 0.25],
            color: [Math.min(1, ec[0] + 0.4), Math.min(1, ec[1] + 0.4), Math.min(1, ec[2] + 0.4), 1.0],
        });
    }

    renderer.drawInstances(instances);

    // I1: render particles AFTER world geometry (additive blending layers
    // correctly on top; depth writes disabled inside render()).
    if (particles) particles.render(camera.viewMatrix(), camera.projMatrix(canvas.width / canvas.height));

    // H1: First-person weapon model. Clear depth so it renders on top of
    // the world, then draw a procedural elemental gauntlet in camera space.
    // The weapon bobs with movement and rotates with the camera.
    renderer.clearDepth();
    const weaponInstances = buildWeaponInstances(camera, playerElement);
    renderer.drawInstances(weaponInstances);

    // Update minimap with actors and power-ups.
    if (maze) {
        hud.drawMinimap(maze, camera.pos, camera.yaw, minotaurBuffers, snap && snap.data ? snap.data.powerUps : null, territoryControl, hazards);
    }

    // H5: Off-screen threat arrows.
    const threatArrowsEl = document.getElementById('threat-arrows');
    if (threatArrowsEl) {
        threatArrowsEl.innerHTML = '';
        const fwd = camera.aimDir();
        for (const [id, buf] of minotaurBuffers) {
            if (buf.stunned || buf.hp <= 0) continue;
            const dx = buf.pos[0] - camera.pos[0];
            const dz = buf.pos[2] - camera.pos[2];
            const dist = Math.hypot(dx, dz);
            if (dist > 8 || dist < 1) continue;
            const toActorX = dx / dist;
            const toActorZ = dz / dist;
            const dot = fwd[0] * toActorX + fwd[2] * toActorZ;
            if (dot < 0.3) {
                const ec = ELEMENT_COLORS[buf.element|0] || ELEMENT_COLORS[0];
                const angle = Math.atan2(toActorX, toActorZ);
                const arrow = document.createElement('div');
                arrow.className = 'threat-arrow';
                arrow.style.borderBottomColor = 'rgb(' + Math.round(ec[0]*255) + ',' + Math.round(ec[1]*255) + ',' + Math.round(ec[2]*255) + ')';
                arrow.style.transform = 'rotate(' + angle + 'rad)';
                threatArrowsEl.appendChild(arrow);
            }
        }
    }
}

// H1: Build the first-person weapon model instances in camera space.
// The weapon is a procedural elemental gauntlet (4 cubes: forearm, palm,
// two finger-guards) that bobs with movement and rotates with the camera.
function buildWeaponInstances(cam, element) {
    const ec = ELEMENT_COLORS[element] || ELEMENT_COLORS[0];
    const tint = [ec[0] * 0.8, ec[1] * 0.8, ec[2] * 0.8, 1.0];
    const accent = [Math.min(1, ec[0] + 0.3), Math.min(1, ec[1] + 0.3), Math.min(1, ec[2] + 0.3), 1.0];

    // Camera basis vectors in world space.
    const fwd = cam.renderDir();
    const right = cam.right();
    const up = [0, 1, 0];

    // Weapon anchor: lower-right of viewport, offset from camera.
    // right * 0.3 = right side, fwd * 0.5 = in front, down * 0.3 = below eye.
    const bobX = cam.bobOffsetX;
    const bobY = cam.bobOffsetY;
    const anchor = [
        cam.pos[0] + right[0] * (0.3 + bobX) + fwd[0] * 0.5 + up[0] * (-0.3 + bobY),
        cam.pos[1] + right[1] * (0.3 + bobX) + fwd[1] * 0.5 + up[1] * (-0.3 + bobY),
        cam.pos[2] + right[2] * (0.3 + bobX) + fwd[2] * 0.5 + up[2] * (-0.3 + bobY),
    ];

    // Weapon local axes: forward = camera forward, right = camera right, up = world up.
    // Build a rotation matrix from these axes.
    const m = new Float32Array(16);
    m[0] = right[0]; m[1] = right[1]; m[2] = right[2]; m[3] = 0;
    m[4] = up[0];    m[5] = up[1];    m[6] = up[2];    m[7] = 0;
    m[8] = fwd[0];   m[9] = fwd[1];   m[10] = fwd[2];  m[11] = 0;
    m[12] = anchor[0]; m[13] = anchor[1]; m[14] = anchor[2]; m[15] = 1;

    // Weapon parts: forearm (long), palm (block), two finger-guards (small).
    // Each part is { offset: [x,y,z] in local space, scale: [sx,sy,sz], color }.
    const parts = [
        { offset: [0, 0, -0.15], scale: [0.08, 0.08, 0.3], color: tint },      // forearm
        { offset: [0, 0, 0.05], scale: [0.12, 0.10, 0.12], color: accent },    // palm
        { offset: [-0.06, 0.04, 0.12], scale: [0.04, 0.04, 0.08], color: tint }, // left finger-guard
        { offset: [0.06, 0.04, 0.12], scale: [0.04, 0.04, 0.08], color: tint }, // right finger-guard
    ];

    const instances = [];
    for (const part of parts) {
        // Transform local offset to world space using the rotation matrix.
        const wx = m[0] * part.offset[0] + m[4] * part.offset[1] + m[8] * part.offset[2] + m[12];
        const wy = m[1] * part.offset[0] + m[5] * part.offset[1] + m[9] * part.offset[2] + m[13];
        const wz = m[2] * part.offset[0] + m[6] * part.offset[1] + m[10] * part.offset[2] + m[14];

        // Build a per-instance model matrix that includes the weapon rotation.
        const mm = new Float32Array(16);
        mm[0] = m[0] * part.scale[0]; mm[1] = m[1] * part.scale[0]; mm[2] = m[2] * part.scale[0]; mm[3] = 0;
        mm[4] = m[4] * part.scale[1]; mm[5] = m[5] * part.scale[1]; mm[6] = m[6] * part.scale[1]; mm[7] = 0;
        mm[8] = m[8] * part.scale[2]; mm[9] = m[9] * part.scale[2]; mm[10] = m[10] * part.scale[2]; mm[11] = 0;
        mm[12] = wx; mm[13] = wy; mm[14] = wz; mm[15] = 1;

        instances.push({
            pos: [wx, wy, wz],
            scale: part.scale,
            color: part.color,
            modelMatrix: mm,
        });
    }
    return instances;
}

// Territory quadrant for floor tinting; mirrors game.TerritoryOf.
function territoryAt(x, y, mazeData) {
    const east = x >= mazeData.w / 2;
    const north = y < mazeData.h / 2;
    if (north && east) return 0;
    if (north) return 1;
    if (east) return 2;
    return 3;
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
