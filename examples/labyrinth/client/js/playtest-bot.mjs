// playtest-bot.mjs - 60Hz closed-loop autonomous playtesting controller for AI models.
// Runs inside the real browser game loop with zero teleports, recording client-side
// frame-time, position sync error, and trajectory history.

export class PlaytestBot {
    constructor() {
        this.active = false;
        this.persona = 'explorer_runner';
        this.baitPhase = 0;
        this.baitTicks = 0;
        this.stats = this.freshStats();
    }

    freshStats() {
        return {
            frames: 0,
            pulsesFired: 0,
            jumpsTriggered: 0,
            teleportsUsed: 0,
            maxSyncDrift: 0,
            sumSyncDrift: 0,
            minFps: Infinity,
            trail: [], // [{t, x, z, hp, nearestDist, note}]
        };
    }

    start(persona = 'explorer_runner') {
        this.persona = persona || 'explorer_runner';
        this.baitPhase = 0;
        this.baitTicks = 0;
        this.stats = this.freshStats();
        this.active = true;
        return { active: this.active, persona: this.persona };
    }

    stop() {
        this.active = false;
        return this.getReport();
    }

    getReport() {
        const avgDrift = this.stats.frames > 0 ? this.stats.sumSyncDrift / this.stats.frames : 0;
        return {
            active: this.active,
            persona: this.persona,
            frames: this.stats.frames,
            pulsesFired: this.stats.pulsesFired,
            jumpsTriggered: this.stats.jumpsTriggered,
            teleportsUsed: this.stats.teleportsUsed,
            maxSyncDrift: Number(this.stats.maxSyncDrift.toFixed(3)),
            avgSyncDrift: Number(avgDrift.toFixed(3)),
            minFps: this.stats.minFps === Infinity ? 60 : Number(this.stats.minFps.toFixed(1)),
            trail: this.stats.trail,
        };
    }

    // bfsNextWaypoint finds the next walkable cell center toward (gx, gz) using 4-dir BFS.
    bfsNextWaypoint(maze, sx, sz, gx, gz, allowGap = false) {
        if (!maze || !maze.grid) return [gx, gz, false];
        const scx = Math.floor(sx), scz = Math.floor(sz);
        const gcx = Math.floor(gx), gcz = Math.floor(gz);
        if (scx === gcx && scz === gcz) return [gx, gz, false];

        const w = maze.w, h = maze.h;
        const startIdx = scz * w + scx;
        const goalIdx = gcz * w + gcx;
        const prev = new Int32Array(w * h).fill(-1);
        const visited = new Uint8Array(w * h);
        const queue = [startIdx];
        visited[startIdx] = 1;
        const dirs = [[0, -1], [0, 1], [-1, 0], [1, 0]];

        let found = false;
        while (queue.length > 0) {
            const cur = queue.shift();
            if (cur === goalIdx) {
                found = true;
                break;
            }
            const cx = cur % w, cz = (cur / w) | 0;
            for (const [dx, dz] of dirs) {
                const nx = cx + dx, nz = cz + dz;
                if (nx < 0 || nz < 0 || nx >= w || nz >= h) continue;
                const ni = nz * w + nx;
                if (visited[ni]) continue;
                const kind = maze.grid[ni];
                // 0=wall, 1=floor, 2=platform, 3=gap
                if (kind === 0 || (!allowGap && kind === 3)) continue;
                visited[ni] = 1;
                prev[ni] = cur;
                queue.push(ni);
            }
        }
        if (!found) return [gx, gz, false];

        const path = [];
        let cur = goalIdx;
        while (cur !== startIdx && cur !== -1) {
            const cx = cur % w, cz = (cur / w) | 0;
            path.push([cx + 0.5, cz + 0.5, maze.grid[cur] === 3]);
            cur = prev[cur];
        }
        path.reverse();
        for (const wp of path) {
            if (Math.hypot(wp[0] - sx, wp[1] - sz) > 0.32) {
                return wp;
            }
        }
        return [gx, gz, false];
    }

    // applyFrame is called inside main.mjs update(dt) right after input.update() when this.active is true.
    applyFrame({ dt, input, camera, maze, serverPos, minotaurBuffers, powerUps, playerElement, hp, losClear }) {
        if (!this.active || !maze) return;
        this.stats.frames++;
        if (dt > 0.001) {
            const fps = 1 / dt;
            if (fps < this.stats.minFps) this.stats.minFps = fps;
        }
        const px = camera.pos[0], pz = camera.pos[2];
        if (serverPos) {
            const drift = Math.hypot(serverPos[0] - px, serverPos[2] - pz);
            this.stats.sumSyncDrift += drift;
            if (drift > this.stats.maxSyncDrift) this.stats.maxSyncDrift = drift;
        }

        let nearest = null;
        let nearestDist = Infinity;
        for (const [id, buf] of minotaurBuffers) {
            if (buf.hp <= 0) continue;
            const d = Math.hypot(buf.pos[0] - px, buf.pos[2] - pz);
            if (d < nearestDist) {
                nearestDist = d;
                nearest = { id, x: buf.pos[0], z: buf.pos[2], stunned: buf.stunned, element: buf.element };
            }
        }

        let targetX = maze.gateX, targetZ = maze.gateY;
        let forward = 1, strafe = 0, sprint = true, fire = false, note = 'navigate_gate';

        if (this.persona === 'combat_hunter' && nearest) {
            const hasLOS = losClear(px, pz, nearest.x, nearest.z);
            const [wpX, wpZ, isGap] = this.bfsNextWaypoint(maze, px, pz, maze.gateX, maze.gateY, false);
            const wdx = wpX - px;
            const wdz = wpZ - pz;
            const wn = Math.hypot(wdx, wdz);
            const vx = wn > 1e-6 ? wdx / wn : 0;
            const vz = wn > 1e-6 ? wdz / wn : 0;
            const moveYaw = Math.atan2(wdx, wdz);

            camera.yaw = moveYaw;
            forward = 1.0;
            strafe = 0.0;
            note = 'hunt_advance';

            if (isGap && wn < 1.3) {
                input.jump = true;
                this.stats.jumpsTriggered++;
            }
            if (hasLOS && nearestDist <= 6.5) {
                const aimYaw = Math.atan2(nearest.x - px, nearest.z - pz);
                camera.yaw = aimYaw;
                forward = vx * Math.sin(aimYaw) + vz * Math.cos(aimYaw);
                strafe = -vx * Math.cos(aimYaw) + vz * Math.sin(aimYaw);
                fire = !nearest.stunned;
                note = 'hunt_engage';
            }
        } else if (this.persona === 'stealth_baiter' && nearest) {
            const hasLOS = losClear(px, pz, nearest.x, nearest.z);
            if (this.baitPhase === 0) {
                targetX = nearest.x;
                targetZ = nearest.z;
                note = 'bait_approach';
                if (nearestDist <= 4.5 && hasLOS) {
                    this.baitPhase = 1;
                    this.baitTicks = 0;
                }
            } else if (this.baitPhase === 1) {
                targetX = maze.startX || 1.5;
                targetZ = maze.startY || 1.5;
                note = 'bait_kite';
                this.baitTicks++;
                if (this.baitTicks > 90) {
                    this.baitPhase = 2;
                    this.baitTicks = 0;
                }
            } else {
                targetX = maze.startX || 1.5;
                targetZ = maze.startY || 1.5;
                forward = (hasLOS || nearestDist < 6.2) ? 1 : 0;
                note = 'bait_observe_search';
                this.baitTicks++;
                if (this.baitTicks > 120) {
                    this.baitPhase = 0;
                    this.baitTicks = 0;
                }
            }
        } else if (this.persona === 'element_tactician') {
            if (powerUps && powerUps.length > 0 && playerElement === 0) {
                targetX = powerUps[0].x;
                targetZ = powerUps[0].y;
                note = 'seek_powerup';
            } else if (nearest) {
                targetX = nearest.x;
                targetZ = nearest.z;
                note = 'press_element_advantage';
            }
        } else if (this.persona === 'chaos_stress') {
            strafe = Math.sin(this.stats.frames * 0.15);
            fire = (this.stats.frames % 30) === 0;
            note = 'chaos_stress';
        }

        if (note !== 'engage_target' && note !== 'hunt_advance' && note !== 'hunt_engage') {
            const [wpX, wpZ, isGap] = this.bfsNextWaypoint(maze, px, pz, targetX, targetZ, false);
            const wdx = wpX - px;
            const wdz = wpZ - pz;
            const wn = Math.hypot(wdx, wdz);
            const vx = wn > 1e-6 ? wdx / wn : 0;
            const vz = wn > 1e-6 ? wdz / wn : 0;
            const moveYaw = Math.atan2(wdx, wdz);

            camera.yaw = moveYaw;
            forward = 1.0;
            strafe = 0.0;

            if (isGap && wn < 1.3) {
                input.jump = true;
                this.stats.jumpsTriggered++;
            }
            const isDisadvantaged = nearest && (playerElement + 1) % 4 === nearest.element;
            if (isDisadvantaged && nearestDist < 5.0) {
                sprint = false;
            }
            const shouldFire = (nearestDist < 2.5 && !isDisadvantaged) || (nearestDist < 1.0 && isDisadvantaged);
            if (nearest && !nearest.stunned && shouldFire && losClear(px, pz, nearest.x, nearest.z)) {
                const aimYaw = Math.atan2(nearest.x - px, nearest.z - pz);
                camera.yaw = aimYaw;
                forward = vx * Math.sin(aimYaw) + vz * Math.cos(aimYaw);
                strafe = -vx * Math.cos(aimYaw) + vz * Math.sin(aimYaw);
                fire = true;
                note = 'defensive_shot';
            }
        }

        camera.pitch = 0;
        input.forward = forward;
        input.strafe = strafe;
        input.sprint = sprint;
        if (fire) {
            input.fire = true;
            this.stats.pulsesFired++;
        }

        if (this.stats.frames % 15 === 0) {
            let activeAction = '';
            if (typeof window !== 'undefined' && window.__ariadne && window.__ariadne.btHud) {
                const h = window.__ariadne.btHud;
                if (h.rows && h.rows.length > 0) {
                    activeAction = h.rows[0].active || '';
                }
            }
            this.stats.trail.push({
                frame: this.stats.frames,
                x: Number(px.toFixed(2)),
                z: Number(pz.toFixed(2)),
                y: Number(pz.toFixed(2)),
                hp,
                nearestDist: nearestDist === Infinity ? -1 : Number(nearestDist.toFixed(2)),
                nearestId: nearest ? nearest.id : '',
                nearestAction: activeAction,
                note,
            });
        }
    }
}
