// image.mjs - dependency-free PNG decoding and the pixel measurements the
// player-eye detectors use. Every measurement here works on what the player
// was actually shown (CDP screencast frames of the composited page), never on
// a canvas readback or a renderer variable.

import { inflateSync } from 'node:zlib';

// decodePng returns {width, height, lum} where lum is a Uint8Array of
// per-pixel luminance (Rec.601). Only 8-bit non-interlaced RGB/RGBA is
// supported - that is what Chromium emits for screenshots and screencasts.
export function decodePng(buf) {
    if (buf[0] !== 0x89 || buf[1] !== 0x50 || buf[2] !== 0x4e || buf[3] !== 0x47) {
        throw new Error('not a PNG');
    }
    const width = buf.readUInt32BE(16);
    const height = buf.readUInt32BE(20);
    const bitDepth = buf[24], colorType = buf[25], interlace = buf[28];
    if (bitDepth !== 8 || interlace !== 0 || (colorType !== 2 && colorType !== 6)) {
        throw new Error(`unsupported PNG (depth ${bitDepth}, colour ${colorType}, interlace ${interlace})`);
    }
    const bpp = colorType === 6 ? 4 : 3;
    const idat = [];
    for (let off = 8; off < buf.length;) {
        const len = buf.readUInt32BE(off);
        const type = buf.toString('ascii', off + 4, off + 8);
        if (type === 'IDAT') idat.push(buf.subarray(off + 8, off + 8 + len));
        if (type === 'IEND') break;
        off += 12 + len;
    }
    const raw = inflateSync(Buffer.concat(idat));
    const stride = width * bpp;
    let prev = new Uint8Array(stride);
    let cur = new Uint8Array(stride);
    const lum = new Uint8Array(width * height);
    for (let y = 0; y < height; y++) {
        const rs = y * (stride + 1);
        const filter = raw[rs];
        for (let x = 0; x < stride; x++) {
            const a = x >= bpp ? cur[x - bpp] : 0;
            const b = prev[x];
            const c = x >= bpp ? prev[x - bpp] : 0;
            let v = raw[rs + 1 + x];
            switch (filter) {
                case 0: break;
                case 1: v += a; break;
                case 2: v += b; break;
                case 3: v += (a + b) >> 1; break;
                case 4: {
                    const p = a + b - c;
                    const pa = Math.abs(p - a), pb = Math.abs(p - b), pc = Math.abs(p - c);
                    v += pa <= pb && pa <= pc ? a : pb <= pc ? b : c;
                    break;
                }
                default: throw new Error(`bad PNG filter ${filter} on row ${y}`);
            }
            cur[x] = v & 0xff;
        }
        const row = y * width;
        for (let px = 0; px < width; px++) {
            const o = px * bpp;
            lum[row + px] = (299 * cur[o] + 587 * cur[o + 1] + 114 * cur[o + 2]) / 1000;
        }
        const t = prev; prev = cur; cur = t;
    }
    return { width, height, lum };
}

// Region is expressed in fractions of the frame so the same mask works for
// any screencast size: {x0, y0, x1, y1} in [0,1].
function regionPixels(img, region) {
    return {
        x0: Math.max(0, Math.floor(region.x0 * img.width)),
        y0: Math.max(0, Math.floor(region.y0 * img.height)),
        x1: Math.min(img.width, Math.ceil(region.x1 * img.width)),
        y1: Math.min(img.height, Math.ceil(region.y1 * img.height)),
    };
}

function masked(masks, fx, fy) {
    for (const m of masks) {
        if (fx >= m.x0 && fx < m.x1 && fy >= m.y0 && fy < m.y1) return true;
    }
    return false;
}

// viewMetrics measures how much visual information the 3D view carries inside
// `region`, ignoring pixels under any of `masks` (HUD element rects, as
// fractions). Returns luminance p5/p50/p95, the p95-p5 range, and the mean
// absolute horizontal+vertical gradient ("edge energy").
export function viewMetrics(img, region, masks = []) {
    const r = regionPixels(img, region);
    const hist = new Uint32Array(256);
    let n = 0, grad = 0, gn = 0;
    const w = img.width;
    for (let y = r.y0; y < r.y1; y++) {
        const fy = y / img.height;
        for (let x = r.x0; x < r.x1; x++) {
            if (masks.length && masked(masks, x / w, fy)) continue;
            const v = img.lum[y * w + x];
            hist[v]++; n++;
            if (x + 1 < r.x1 && y + 1 < r.y1) {
                grad += Math.abs(img.lum[y * w + x + 1] - v) + Math.abs(img.lum[(y + 1) * w + x] - v);
                gn++;
            }
        }
    }
    if (n === 0) return { n: 0, p5: 0, p50: 0, p95: 0, range: 0, edge: 0 };
    const pct = (q) => {
        const target = q * n;
        let acc = 0;
        for (let i = 0; i < 256; i++) { acc += hist[i]; if (acc >= target) return i; }
        return 255;
    };
    const p5 = pct(0.05), p50 = pct(0.5), p95 = pct(0.95);
    return { n, p5, p50, p95, range: p95 - p5, edge: gn ? grad / gn : 0 };
}

// columnProfile averages luminance per column over the given vertical band,
// skipping pixels under any mask (HUD text such as the wave banner is static
// on screen and would otherwise pin the estimated scene shift at zero).
// Columns that are fully masked take the band's mean so they carry no signal.
export function columnProfile(img, y0f, y1f, masks = []) {
    const y0 = Math.floor(y0f * img.height), y1 = Math.ceil(y1f * img.height);
    const out = new Float64Array(img.width);
    const empty = [];
    let total = 0, totalN = 0;
    for (let x = 0; x < img.width; x++) {
        let s = 0, n = 0;
        const fx = x / img.width;
        for (let y = y0; y < y1; y++) {
            if (masks.length && masked(masks, fx, y / img.height)) continue;
            s += img.lum[y * img.width + x]; n++;
        }
        if (n) { out[x] = s / n; total += s; totalN += n; } else empty.push(x);
    }
    const mean = totalN ? total / totalN : 0;
    for (const x of empty) out[x] = mean;
    return out;
}

// horizontalShift estimates how far (in pixels, signed) the scene content of
// `b` moved relative to `a`: positive means content moved RIGHT on screen.
// It maximises normalised cross-correlation of the two column profiles over
// shifts in [-maxShift, maxShift]. Returns {shift, score}; score is the best
// correlation (-1..1) so callers can ignore textureless frames.
export function horizontalShift(a, b, { y0f = 0.2, y1f = 0.65, maxShift = 120, masks = [] } = {}) {
    const pa = columnProfile(a, y0f, y1f, masks);
    const pb = columnProfile(b, y0f, y1f, masks);
    const w = Math.min(pa.length, pb.length);
    let best = { shift: 0, score: -Infinity };
    for (let s = -maxShift; s <= maxShift; s++) {
        // b[x] ~ a[x - s] when content moved right by s.
        let sa = 0, sb = 0, n = 0;
        const lo = Math.max(0, s), hi = Math.min(w, w + s);
        if (hi - lo < w / 3) continue;
        for (let x = lo; x < hi; x++) { sa += pa[x - s]; sb += pb[x]; n++; }
        const ma = sa / n, mb = sb / n;
        let num = 0, da = 0, db = 0;
        for (let x = lo; x < hi; x++) {
            const u = pa[x - s] - ma, v = pb[x] - mb;
            num += u * v; da += u * u; db += v * v;
        }
        const score = da > 0 && db > 0 ? num / Math.sqrt(da * db) : -1;
        if (score > best.score) best = { shift: s, score };
    }
    return best;
}

// meanAbsDiff is the mean absolute luminance difference between two frames of
// the same size; 0 means pixel-identical.
export function meanAbsDiff(a, b) {
    if (a.width !== b.width || a.height !== b.height) return Infinity;
    let s = 0;
    for (let i = 0; i < a.lum.length; i++) s += Math.abs(a.lum[i] - b.lum[i]);
    return s / a.lum.length;
}
