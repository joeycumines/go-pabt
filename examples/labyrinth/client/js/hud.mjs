// HUD: minimap (canvas 2D), intensity bar, status chips, health bar.

export class HUD {
    constructor() {
        this.minimap = document.getElementById('minimap');
        this.minimapCtx = this.minimap.getContext('2d');
        this.intensityFill = document.getElementById('intensity-fill');
        this.hpFill = document.getElementById('hp-fill');
        this.hpText = document.getElementById('hp-text');
        this.chipRound = document.getElementById('chip-round');
        this.chipFps = document.getElementById('chip-fps');
        this.chipElement = document.getElementById('chip-element');
        this.chipWar = document.getElementById('chip-war');
        this._fpsBuffer = [];
    }

    updateFps(dt) {
        this._fpsBuffer.push(dt);
        if (this._fpsBuffer.length > 30) this._fpsBuffer.shift();
        const avg = this._fpsBuffer.reduce((a,b) => a+b, 0) / this._fpsBuffer.length;
        this.chipFps.textContent = 'FPS: ' + Math.round(1 / avg);
    }

    setRound(r) {
        this.chipRound.textContent = 'Round: ' + r;
    }

    setIntensity(level) {
        this.intensityFill.style.width = (level / 5 * 100) + '%';
    }

    setHP(hp, maxHP) {
        const pct = Math.max(0, Math.min(100, (hp / maxHP) * 100));
        this.hpFill.style.width = pct + '%';
        if (this.hpText) this.hpText.textContent = Math.max(0, Math.round(hp)) + ' / ' + maxHP;
        // Color shift: green > yellow > red
        if (pct > 60) {
            this.hpFill.style.background = 'linear-gradient(90deg, #00ff80, #00cc60)';
        } else if (pct > 30) {
            this.hpFill.style.background = 'linear-gradient(90deg, #ffcc00, #ff9900)';
        } else {
            this.hpFill.style.background = 'linear-gradient(90deg, #ff4040, #cc0000)';
        }
    }

    setElement(elem) {
        if (!this.chipElement) return;
        const names = ['Fire', 'Ice', 'Lightning', 'Void'];
        const colors = ['#ff5920', '#59bfff', '#fff240', '#8c33d9'];
        this.chipElement.textContent = 'Element: ' + (names[elem] || 'Unknown');
        this.chipElement.style.borderColor = colors[elem] || '#ffffff';
        this.chipElement.style.color = colors[elem] || '#ffffff';
    }

    setWar(clashes) {
        if (!this.chipWar) return;
        this.chipWar.textContent = 'War: ' + clashes + ' clashes';
    }

    drawMinimap(mazeData, playerPos, playerYaw, minotaurBuffers, powerUps, territoryControl, hazards) {
        const ctx = this.minimapCtx;
        const w = this.minimap.width;
        const h = this.minimap.height;
        ctx.fillStyle = '#0a0a0f';
        ctx.fillRect(0, 0, w, h);

        const cellW = w / mazeData.w;
        const cellH = h / mazeData.h;

        // Draw walls
        ctx.fillStyle = '#404060';
        for (let y = 0; y < mazeData.h; y++) {
            for (let x = 0; x < mazeData.w; x++) {
                if (mazeData.grid[y * mazeData.w + x] === 0) {
                    ctx.fillRect(x * cellW, y * cellH, cellW, cellH);
                }
            }
        }

        // Territory tint on floor cells (matches the 3D floor tinting).
        const terrTints = [
            'rgba(255, 90, 30, 0.10)',  // fire NE
            'rgba(60, 140, 255, 0.10)', // ice NW
            'rgba(255, 240, 60, 0.08)', // lightning SE
            'rgba(140, 50, 220, 0.10)'  // void SW
        ];
        for (let y = 0; y < mazeData.h; y++) {
            for (let x = 0; x < mazeData.w; x++) {
                if (mazeData.grid[y * mazeData.w + x] === 0) continue;
                const east = x >= mazeData.w / 2;
                const north = y < mazeData.h / 2;
                const ti = north ? (east ? 0 : 1) : (east ? 2 : 3);
                const ctrl = territoryControl || [0, 1, 2, 3];
                ctx.fillStyle = terrTints[ctrl[ti]|0] || terrTints[0];
                ctx.fillRect(x * cellW, y * cellH, cellW, cellH);
            }
        }

        // Draw gate
        ctx.fillStyle = '#00ff80';
        ctx.fillRect(
            mazeData.gateX * cellW - 2, mazeData.gateY * cellH - 2,
            cellW + 4, cellH + 4
        );

        // Draw active power-ups
        if (powerUps) {
            const puColors = ['#ff5920', '#59bfff', '#fff240', '#8c33d9'];
            for (const pu of powerUps) {
                if (!pu.active) continue;
                ctx.fillStyle = puColors[pu.element|0] || '#ffffff';
                ctx.fillRect(pu.x * cellW - 2, pu.y * cellH - 2, 4, 4);
            }
        }

        // Draw crossfire rifts (gang-war dynamic obstacles).
        if (hazards) {
            const hzColors = ['#ff5920', '#59bfff', '#fff240', '#8c33d9'];
            for (const h of hazards) {
                ctx.strokeStyle = hzColors[h.element|0] || '#ffffff';
                ctx.lineWidth = 1;
                ctx.beginPath();
                ctx.arc(h.x * cellW, h.y * cellH, 4, 0, Math.PI * 2);
                ctx.stroke();
            }
        }

        // Draw actors, colored by faction element with a strategy ring:
        // red ring = raid, cyan ring = retreat.
        if (minotaurBuffers) {
            const elemColors = ['#ff5920', '#59bfff', '#fff240', '#8c33d9'];
            for (const [id, buf] of minotaurBuffers) {
                const mx = buf.pos[0] * cellW;
                const my = buf.pos[2] * cellH;
                ctx.fillStyle = buf.stunned ? '#4040ff' : (elemColors[buf.element|0] || '#ff4040');
                ctx.beginPath();
                ctx.arc(mx, my, 2, 0, Math.PI * 2);
                ctx.fill();
                const strat = buf.strategy|0;
                if (strat === 1 || strat === 2) {
                    ctx.strokeStyle = strat === 1 ? '#ff2010' : '#20e0ff';
                    ctx.lineWidth = 1;
                    ctx.beginPath();
                    ctx.arc(mx, my, 4, 0, Math.PI * 2);
                    ctx.stroke();
                }
            }
        }

        // Draw player
        const px = playerPos[0] * cellW;
        const py = playerPos[2] * cellH;
        ctx.fillStyle = '#ff00ff';
        ctx.beginPath();
        ctx.arc(px, py, 3, 0, Math.PI * 2);
        ctx.fill();

        // Direction indicator
        ctx.strokeStyle = '#ff00ff';
        ctx.lineWidth = 2;
        ctx.beginPath();
        ctx.moveTo(px, py);
        ctx.lineTo(px + Math.sin(playerYaw) * 8, py + Math.cos(playerYaw) * 8);
        ctx.stroke();
    }
}
