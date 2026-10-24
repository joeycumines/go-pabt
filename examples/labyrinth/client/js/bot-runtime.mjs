// bot-runtime.mjs - Compiles and runs model-authored bot source against the
// live client.
//
// The runtime is the piece that turns a *string of bot source* into a
// running player. It compiles the source once, drives `tick(ctx, api)` once
// per rendered frame, projects the accumulated command onto the client's
// real input axes, and keeps the telemetry a playtest harness needs: frame
// counts, the command trail, bot logs and any error the bot threw.
//
// A bot that throws does not take the session down: the error is captured
// with the frame index and stack, the bot stops commanding, and the session
// keeps running so the harness can capture what happened next.

import { buildObservation, createBotAPI, newCommand, projectMove } from './bot-sdk.mjs';

// compileBot turns bot source text into a runnable bot object.
//
// Two source forms are supported:
//   module.exports = { name: 'x', tick(ctx, api) { ... } };
//   function tick(ctx, api) { ... }          (optionally with `const botName`)
//
// The source is data, not trusted code: it is compiled in the page with
// new Function and never reaches the server, the filesystem or a shell.
export function compileBot(source) {
    if (typeof source !== 'string' || source.trim() === '') {
        return { error: 'bot source is empty' };
    }
    let factory;
    try {
        factory = new Function('module', 'exports', [
            '"use strict";',
            source,
            ';return (module.exports && Object.keys(module.exports).length > 0)',
            '  ? module.exports',
            '  : (typeof tick === "function"',
            '      ? { name: (typeof botName === "string" ? botName : "bot"), tick }',
            '      : null);',
        ].join('\n'));
    } catch (err) {
        return { error: `syntax error: ${err && err.message ? err.message : err}`, stack: err && err.stack ? err.stack : '' };
    }
    const module = { exports: {} };
    let result;
    try {
        result = factory(module, module.exports);
    } catch (err) {
        return { error: `load error: ${err && err.message ? err.message : err}`, stack: err && err.stack ? err.stack : '' };
    }
    if (!result || typeof result.tick !== 'function') {
        return {
            error: 'bot source must define tick(ctx, api) - either module.exports = {name, tick} or a top-level function tick(ctx, api)',
        };
    }
    return {
        bot: {
            name: typeof result.name === 'string' && result.name ? result.name : 'bot',
            tick: result.tick,
        },
    };
}

function clamp(v, lo, hi) {
    return v < lo ? lo : (v > hi ? hi : v);
}

// BotRuntime runs one bot at a time against the live client.
export class BotRuntime {
    constructor(world) {
        // `world` is the live provider object owned by the caller. The
        // runtime refreshes the per-tick fields it owns and reads the rest.
        this.world = world;
        this.bot = null;
        this.source = '';
        this.name = '';
        this.persona = '';
        this.active = false;
        this.frames = 0;
        this.startedAt = 0;
        this.errors = [];
        this.logs = [];
        this.trail = [];
        this.personas = new Map();
        this.lastCommand = null;
        // Per-bot memory that survives across ticks (waypoint tracking).
        this.state = {};
        this.minFps = Infinity;
        this.sumSyncDrift = 0;
        this.maxSyncDrift = 0;
        this.teleportsUsed = 0;
        this.jumpsTriggered = 0;
        world.botLog = this.logs;
        world.finished = false;
        world.finishReason = '';
    }

    // registerPersona makes a built-in bot available by name so the harness
    // can start it the same way it starts a model-authored one.
    registerPersona(name, bot) {
        this.personas.set(name, bot);
    }

    // load compiles bot source and starts it. The previous bot (if any) keeps
    // its telemetry; only the commanding stops.
    load(source, opts = {}) {
        const compiled = compileBot(source);
        if (compiled.error) {
            this.errors.push({
                frame: this.frames,
                t: this.world.time || 0,
                message: compiled.error,
                stack: compiled.stack || '',
                kind: 'compile',
            });
            this.active = false;
            this.bot = null;
            return { ok: false, error: compiled.error };
        }
        this.source = source;
        this.bot = compiled.bot;
        this.name = opts.name || compiled.bot.name || 'bot';
        this.persona = opts.persona || '';
        this.active = true;
        this.frames = 0;
        this.startedAt = now();
        this.world.finished = false;
        this.world.finishReason = '';
        this.world.time = 0;
        this.world.frame = 0;
        this.state = {};
        return { ok: true, name: this.name };
    }

    // loadBot starts an already-instantiated bot object (used for the
    // built-in personas, which are ES modules rather than source text).
    loadBot(bot, opts = {}) {
        if (!bot || typeof bot.tick !== 'function') {
            const message = 'bot object must expose a tick(ctx, api) function';
            this.errors.push({ frame: this.frames, t: this.world.time || 0, message, stack: '', kind: 'compile' });
            this.active = false;
            this.bot = null;
            return { ok: false, error: message };
        }
        this.source = '';
        this.bot = { name: bot.name || opts.name || 'bot', tick: bot.tick };
        this.name = opts.name || this.bot.name;
        this.persona = opts.persona || '';
        this.active = true;
        this.frames = 0;
        this.startedAt = now();
        this.world.finished = false;
        this.world.finishReason = '';
        this.world.time = 0;
        this.world.frame = 0;
        this.state = {};
        return { ok: true, name: this.name };
    }

    // loadPersona starts one of the registered built-in personas.
    loadPersona(persona, opts = {}) {
        const bot = this.personas.get(persona);
        if (!bot) {
            const message = `unknown persona: ${persona}`;
            this.errors.push({ frame: this.frames, t: this.world.time || 0, message, stack: '', kind: 'compile' });
            return { ok: false, error: message };
        }
        return this.loadBot(bot, { ...opts, name: opts.name || bot.name, persona });
    }

    unload() {
        this.active = false;
        this.bot = null;
        return this.report();
    }

    // step runs one bot tick and applies the resulting command to the client
    // input. Returns the applied command, or null when no bot is running.
    step(dt) {
        if (!this.active || !this.bot) return null;
        const world = this.world;
        world.frame = ++this.frames;
        world.time = (now() - this.startedAt) / 1000;
        if (dt > 0.001) {
            const fps = 1 / dt;
            if (fps < this.minFps) this.minFps = fps;
        }

        const cmd = newCommand();
        const obs = buildObservation(world);
        const api = createBotAPI(world, cmd, obs, this.state);
        const ctx = { frame: world.frame, time: world.time, round: obs.round, dt };

        let result = null;
        try {
            result = this.bot.tick(ctx, api);
        } catch (err) {
            this.errors.push({
                frame: world.frame,
                t: world.time,
                message: err && err.message ? err.message : String(err),
                stack: err && err.stack ? err.stack : '',
                kind: 'tick',
            });
            // The bot stops commanding but the session keeps running: the
            // harness still gets frames, state and events after the failure.
            this.active = false;
            this.releaseInput(world);
            return null;
        }

        const applied = this.resolveCommand(cmd, result, obs);
        this.applyCommand(world, applied);
        this.lastCommand = applied;

        // Client/server divergence is a real defect in the netcode, so the
        // runtime tracks it: a bot that teleports or desyncs shows up here.
        if (world.serverPos) {
            const drift = Math.hypot(world.serverPos[0] - obs.self.x, world.serverPos[2] - obs.self.z);
            this.sumSyncDrift += drift;
            if (drift > this.maxSyncDrift) this.maxSyncDrift = drift;
        }

        if (this.frames % 10 === 0) {
            const near = obs.actors[0];
            this.trail.push({
                frame: this.frames,
                t: Number(world.time.toFixed(2)),
                x: Number(obs.self.x.toFixed(2)),
                z: Number(obs.self.z.toFixed(2)),
                hp: obs.self.hp,
                nearestDist: near ? Number(near.dist.toFixed(2)) : -1,
                nearestId: near ? near.id : '',
                nearestAction: near ? near.btAction : '',
                note: applied.note || '',
            });
        }
        return applied;
    }

    resolveCommand(cmd, result, obs) {
        let forward = 0;
        let strafe = 0;
        const direct = result && typeof result === 'object' && typeof result.forward === 'number';
        if (direct) {
            forward = result.forward;
            strafe = typeof result.strafe === 'number' ? result.strafe : 0;
        } else if (cmd.move.x !== 0 || cmd.move.z !== 0) {
            const p = projectMove(cmd.move, cmd.yaw !== null ? cmd.yaw : obs.self.yaw);
            forward = p.forward;
            strafe = p.strafe;
        }
        return {
            forward: clamp(forward, -1, 1),
            strafe: clamp(strafe, -1, 1),
            yaw: cmd.yaw !== null ? cmd.yaw : obs.self.yaw,
            pitch: cmd.pitch !== null ? cmd.pitch : obs.self.pitch,
            sprint: !!cmd.sprint,
            fire: !!cmd.fire,
            jump: !!cmd.jump,
            note: cmd.note || '',
        };
    }

    applyCommand(world, cmd) {
        world.camera.yaw = cmd.yaw;
        world.camera.pitch = cmd.pitch;
        world.input.forward = cmd.forward;
        world.input.strafe = cmd.strafe;
        world.input.sprint = cmd.sprint;
        if (cmd.jump) {
            world.input.jump = true;
            this.jumpsTriggered++;
        }
        if (cmd.fire) world.input.fire = true;
    }

    releaseInput(world) {
        world.input.forward = 0;
        world.input.strafe = 0;
        world.input.sprint = false;
    }

    report() {
        const avgDrift = this.frames > 0 ? this.sumSyncDrift / this.frames : 0;
        return {
            active: this.active,
            name: this.name,
            persona: this.persona,
            frames: this.frames,
            shotsFired: this.world.shotsFired | 0,
            pulsesFired: this.world.shotsFired | 0,
            jumpsTriggered: this.jumpsTriggered,
            teleportsUsed: this.teleportsUsed,
            maxSyncDrift: Number(this.maxSyncDrift.toFixed(3)),
            avgSyncDrift: Number(avgDrift.toFixed(3)),
            minFps: this.minFps === Infinity ? 60 : Number(this.minFps.toFixed(1)),
            finished: !!this.world.finished,
            finishReason: this.world.finishReason || '',
            errors: this.errors.slice(),
            logs: this.logs.slice(-200),
            trail: this.trail,
        };
    }
}

function now() {
    return typeof performance !== 'undefined' && performance.now ? performance.now() : Date.now();
}
