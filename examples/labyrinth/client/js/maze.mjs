// Client-side maze generation (mirrors the Go server's algorithm).
// Deterministic from seed: same seed = same maze.

export function generateMaze(seed, w, h) {
    if (w < 9) w = 9;
    if (h < 9) h = 9;
    if (w % 2 === 0) w++;
    if (h % 2 === 0) h++;

    const rng = mulberry32(seed);
    const grid = new Uint8Array(w * h); // 0 = wall, 1 = floor, 2 = platform, 3 = gap
    grid.fill(0);

    // Recursive backtracker
    const stack = [[1, 1]];
    grid[1 * w + 1] = 1;
    const dirs = [[0,-2],[0,2],[-2,0],[2,0]];

    while (stack.length > 0) {
        const [cx, cy] = stack[stack.length - 1];
        const open = [];
        for (const [dx, dy] of dirs) {
            const nx = cx + dx, ny = cy + dy;
            if (nx > 0 && ny > 0 && nx < w-1 && ny < h-1 && grid[ny * w + nx] === 0) {
                open.push([dx, dy]);
            }
        }
        if (open.length === 0) {
            stack.pop();
            continue;
        }
        const [dx, dy] = open[Math.floor(rng() * open.length)];
        const nx = cx + dx, ny = cy + dy;
        grid[(cy + dy/2) * w + (cx + dx/2)] = 1;
        grid[ny * w + nx] = 1;
        stack.push([nx, ny]);
    }

    // Carve rooms
    const rooms = 2 + Math.floor(rng() * 3);
    for (let i = 0; i < rooms; i++) {
        const rw = 3 + Math.floor(rng() * 3);
        const rh = 3 + Math.floor(rng() * 3);
        const rx = 1 + Math.floor(rng() * (w - rw - 1));
        const ry = 1 + Math.floor(rng() * (h - rh - 1));
        for (let y = ry; y < ry + rh; y++) {
            for (let x = rx; x < rx + rw; x++) {
                if (grid[y * w + x] === 0) grid[y * w + x] = 1;
            }
        }
    }

    // Start and gate
    const start = findFloorNear(grid, w, h, 1, 1, rng);
    const gate = findFloorNear(grid, w, h, w - 2, h - 2, rng);

    return {
        W: w, H: h,
        grid: grid,
        start: { x: start[0] + 0.5, y: start[1] + 0.5 },
        gate: { x: gate[0] + 0.5, y: gate[1] + 0.5 },
    };
}

function findFloorNear(grid, w, h, tx, ty, rng) {
    for (let r = 0; r < w + h; r++) {
        for (let dy = -r; dy <= r; dy++) {
            for (let dx = -r; dx <= r; dx++) {
                const x = tx + dx, y = ty + dy;
                if (x > 0 && y > 0 && x < w-1 && y < h-1 && grid[y * w + x] === 1) {
                    return [x, y];
                }
            }
        }
    }
    return [1, 1];
}

// Mulberry32 PRNG - deterministic, fast, sufficient for maze generation.
function mulberry32(seed) {
    let a = seed | 0;
    return function() {
        a = (a + 0x6D2B79F5) | 0;
        let t = Math.imul(a ^ (a >>> 15), 1 | a);
        t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
        return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
    };
}
