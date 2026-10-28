// predict.mjs - exact JavaScript port of the Go server's movement and
// collision logic (netcode/movement.go Move and maze/collision.go Resolve).
// Both sides must match to double precision (1e-9) so client prediction and
// server authoritativeness agree on every input step.

export const RUN_SPEED = 4.0;
export const SPRINT_SPEED = 6.5;
export const RADIUS = 0.3;

function clamp(v, min, max) {
    if (v < min) return min;
    if (v > max) return max;
    return v;
}

function clamp1(v) {
    if (v > 1) return 1;
    if (v < -1) return -1;
    return v;
}

// isSolid mirrors Go maze.Maze.Solid: out-of-range cells and CellWall (0)
// are solid; floor, platforms, and gaps do not block horizontal movement.
export function isSolid(maze, cx, cy) {
    const w = maze.w !== undefined ? maze.w : maze.W;
    const h = maze.h !== undefined ? maze.h : maze.H;
    if (cx < 0 || cy < 0 || cx >= w || cy >= h) {
        return true;
    }
    const grid = maze.grid !== undefined ? maze.grid : maze.Grid;
    const cell = grid[cy * w + cx];
    const kind = typeof cell === 'object' && cell !== null ? cell.Kind : cell;
    return kind === 0;
}

// resolve mirrors Go (*Maze).Resolve exactly.
export function resolve(maze, pos, radius = RADIUS) {
    const w = maze.w !== undefined ? maze.w : maze.W;
    const h = maze.h !== undefined ? maze.h : maze.H;

    let posX = clamp(pos.x !== undefined ? pos.x : pos.X, radius, w - radius);
    let posY = clamp(pos.y !== undefined ? pos.y : pos.Y, radius, h - radius);

    const minX = Math.floor(posX - radius);
    const maxX = Math.floor(posX + radius);
    const minY = Math.floor(posY - radius);
    const maxY = Math.floor(posY + radius);

    for (let cy = minY; cy <= maxY; cy++) {
        for (let cx = minX; cx <= maxX; cx++) {
            if (!isSolid(maze, cx, cy)) {
                continue;
            }
            const nx = clamp(posX, cx, cx + 1);
            const ny = clamp(posY, cy, cy + 1);
            const dx = posX - nx;
            const dy = posY - ny;
            const dist2 = dx * dx + dy * dy;
            if (dist2 >= radius * radius) {
                continue;
            }
            if (dist2 > 1e-12) {
                const dist = Math.sqrt(dist2);
                const push = radius - dist;
                posX += (dx / dist) * push;
                posY += (dy / dist) * push;
            } else {
                const left = posX - cx;
                const right = (cx + 1) - posX;
                const up = posY - cy;
                const down = (cy + 1) - posY;
                const mn = Math.min(Math.min(left, right), Math.min(up, down));
                if (mn === left) {
                    posX = cx - radius;
                } else if (mn === right) {
                    posX = (cx + 1) + radius;
                } else if (mn === up) {
                    posY = cy - radius;
                } else {
                    posY = (cy + 1) + radius;
                }
            }
        }
    }

    return { x: posX, y: posY };
}

// move mirrors Go netcode.Move exactly.
export function move(maze, pos, yaw, forward, strafe, sprint, dt) {
    forward = clamp1(forward);
    strafe = clamp1(strafe);

    const speed = sprint ? SPRINT_SPEED : RUN_SPEED;

    const fx = Math.sin(yaw);
    const fy = Math.cos(yaw);
    const sx = -fy;
    const sy = fx;

    let dx = fx * forward + sx * strafe;
    let dy = fy * forward + sy * strafe;

    const n = Math.hypot(dx, dy);
    if (n > 1) {
        dx /= n;
        dy /= n;
    }

    const curX = pos.x !== undefined ? pos.x : pos.X;
    const curY = pos.y !== undefined ? pos.y : pos.Y;

    const next = {
        x: curX + dx * speed * dt,
        y: curY + dy * speed * dt,
    };

    return resolve(maze, next, RADIUS);
}
