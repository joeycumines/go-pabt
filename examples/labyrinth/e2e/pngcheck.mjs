// pngcheck.mjs - dependency-free screenshot-liveness gate for the E2E suite.
//
// Size + PNG magic is not a sufficient liveness signal: blank/flat frames
// (e.g. a white void or chrome-error page) can exceed the 30KB floor because
// of deflate overhead and embedded text chunks. This module inflates the
// PNG's IDAT stream, reconstructs the per-scanline filters, and requires
// luminance standard deviation above a floor - a real rendered frame has
// shadows, geometry, textures and HUD text that vary; a flat frame does not.
//
// Pure Node (node:zlib only) - deliberately no image library dependency.

import { existsSync, statSync, readFileSync } from 'node:fs';
import { inflateSync } from 'node:zlib';

// Luminance standard deviation below this is treated as a blank/flat frame.
// Real rendered frames score well into the tens; flat frames score ~0.
export const MIN_LUMINANCE_STDDEV = 3.0;

// assertScreenshot validates a screenshot file is a genuine non-blank render.
// Returns the file size in bytes. Throws with a specific reason otherwise.
export function assertScreenshot(file, { minBytes = 30 * 1024 } = {}) {
    if (!existsSync(file)) throw new Error(`screenshot missing: ${file}`);
    const size = statSync(file).size;
    if (size < minBytes) throw new Error(`screenshot too small (${size}B < ${minBytes}B) - likely chrome-error void: ${file}`);
    const buf = readFileSync(file);
    if (buf[0] !== 0x89 || buf[1] !== 0x50 || buf[2] !== 0x4e || buf[3] !== 0x47) {
        throw new Error(`not a PNG: ${file}`);
    }
    assertNotBlank(buf, file);
    return size;
}

// assertNotBlank decodes the pixel stream and rejects flat frames. Takes the
// already-read buffer so callers/tests can reuse it.
export function assertNotBlank(buf, file = '<buffer>') {
    const { width, height, bitDepth, colorType, interlace } = readPngHeader(buf);
    // Only the common case (8-bit, non-interlaced, RGB/RGBA) is decoded;
    // other formats fall back to the size + magic checks that already ran.
    if (bitDepth !== 8 || interlace !== 0) return;
    const bpp = colorType === 6 ? 4 : colorType === 2 ? 3 : 0;
    if (bpp === 0) return; // palette/gray - skip variance check
    const idatChunks = [];
    let off = 8;
    while (off < buf.length) {
        const len = buf.readUInt32BE(off);
        const type = buf.toString('ascii', off + 4, off + 8);
        if (type === 'IDAT') idatChunks.push(buf.subarray(off + 8, off + 8 + len));
        off += 12 + len;
    }
    const raw = inflateSync(Buffer.concat(idatChunks));
    const stride = width * bpp;
    const prev = Buffer.alloc(stride);
    let lumSum = 0, lumSqSum = 0, n = 0;
    // Cap the sample count so large screenshots stay cheap.
    const sampleStep = Math.max(1, Math.floor((width * height) / 4096));
    for (let y = 0; y < height; y++) {
        const rowStart = y * (stride + 1);
        const filter = raw[rowStart];
        const line = raw.subarray(rowStart + 1, rowStart + 1 + stride);
        const cur = Buffer.alloc(stride);
        for (let x = 0; x < stride; x++) {
            const a = x >= bpp ? cur[x - bpp] : 0;
            const b = prev[x];
            const c = x >= bpp ? prev[x - bpp] : 0;
            let v = line[x];
            switch (filter) {
                case 1: v = (v + a) & 0xff; break;
                case 2: v = (v + b) & 0xff; break;
                case 3: v = (v + ((a + b) >> 1)) & 0xff; break;
                case 4: {
                    const p = a + b - c;
                    const pa = Math.abs(p - a), pb = Math.abs(p - b), pc = Math.abs(p - c);
                    const pr = pa <= pb && pa <= pc ? a : pb <= pc ? b : c;
                    v = (v + pr) & 0xff;
                    break;
                }
            }
            cur[x] = v;
        }
        for (let px = 0; px < width; px += sampleStep) {
            const o = px * bpp;
            const lum = 0.299 * cur[o] + 0.587 * cur[o + 1] + 0.114 * cur[o + 2];
            lumSum += lum; lumSqSum += lum * lum; n++;
        }
        cur.copy(prev);
    }
    if (n === 0) return;
    const mean = lumSum / n;
    const variance = lumSqSum / n - mean * mean;
    const stddev = Math.sqrt(Math.max(0, variance));
    if (stddev < MIN_LUMINANCE_STDDEV) {
        throw new Error(`screenshot is a blank/flat frame (luminance stddev ${stddev.toFixed(2)} < ${MIN_LUMINANCE_STDDEV}) - not a genuine render: ${file}`);
    }
}

// readPngHeader parses IHDR fields needed by the variance check.
export function readPngHeader(buf) {
    if (buf.readUInt32BE(12) !== 0x49484452) throw new Error('PNG missing IHDR'); // "IHDR"
    return {
        width: buf.readUInt32BE(16),
        height: buf.readUInt32BE(20),
        bitDepth: buf[24],
        colorType: buf[25],
        interlace: buf[28],
    };
}
