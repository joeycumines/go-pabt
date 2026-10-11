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

const canvas = document.getElementById('glcanvas');
const loading = document.getElementById('loading');
const hudEl = document.getElementById('hud');
const prompt = document.getElementById('pointer-lock-prompt');
const overlay = document.getElementById('overlay');

let renderer, camera, input, hud, net, audio, debugPanel, btHud;
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
    const right = [-Math.cos(camera.yaw), 0, Math.sin(camera.yaw)];

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

        // Update HUD HP and the active element.
        lastHP = sp.hp;
        hud.setHP(sp.hp, MAX_HP);
        hud.setRound(snap.data.round);
        hud.setIntensity(snap.data.intensity);
        if (sp.element !== undefined) {
            playerElement = sp.element | 0;
            hud.setElement(playerElement);
        }

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
        net.sendInput(input.forward, input.strafe, camera.yaw, camera.pitch, input.sprint, false, true);
        const ec = ELEMENT_COLORS[playerElement];
        projectiles.push({
            pos: [camera.pos[0] + Math.sin(camera.yaw) * 0.5, camera.pos[1] - 0.05, camera.pos[2] + Math.cos(camera.yaw) * 0.5],
            dir: [Math.sin(camera.yaw), 0, Math.cos(camera.yaw)],
            born: now,
            color: [Math.min(1, ec[0] + 0.35), Math.min(1, ec[1] + 0.35), Math.min(1, ec[2] + 0.35), 1.0],
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
    }
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
        case 'stunned': {
            audio.playStun();
            const sb = evt.data && evt.data.minotaur ? minotaurBuffers.get(evt.data.minotaur) : null;
            if (sb) {
                sb.hurtUntil = performance.now() / 1000 + 0.6;
                audio.playHitReaction(actorDist(sb.pos[0], sb.pos[2]));
            }
            hitFlashTimer = 8;
            // Show the elemental outcome of the pulse.
            const adv = evt.data && evt.data.advantage;
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
                minotaurBuffers.delete(evt.data.id);
            }
            break;
        }
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
    instances.push({
        pos: [maze.gateX, 0.5, maze.gateY],
        scale: [0.8, 1.0, 0.8],
        color: gateColor,
    });

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

    // Muzzle flash in the active element color.
    if (muzzleFlashTimer > 0) {
        const ec = ELEMENT_COLORS[playerElement];
        instances.push({
            pos: [camera.pos[0] + Math.sin(camera.yaw) * 0.6, camera.pos[1] - 0.2, camera.pos[2] + Math.cos(camera.yaw) * 0.6],
            scale: [0.25, 0.25, 0.25],
            color: [Math.min(1, ec[0] + 0.4), Math.min(1, ec[1] + 0.4), Math.min(1, ec[2] + 0.4), 1.0],
        });
    }

    renderer.drawInstances(instances);

    // Update minimap with actors and power-ups.
    if (maze) {
        hud.drawMinimap(maze, camera.pos, camera.yaw, minotaurBuffers, snap && snap.data ? snap.data.powerUps : null, territoryControl, hazards);
    }
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
