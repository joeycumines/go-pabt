// pngcheck.test.mjs - unit tests for the screenshot-liveness gate.
//
// Self-contained: synthesizes PNGs in-process (no fixtures committed).
// Run: node pngcheck.test.mjs

import { deflateSync } from 'node:zlib';
import assert from 'node:assert';
import path from 'node:path';
import { writeFileSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { assertScreenshot, assertNotBlank, readPngHeader } from './pngcheck.mjs';

// --- minimal PNG encoder (8-bit RGBA, filter 0) ---------------------------

const CRC_TABLE = (() => {
    const t = new Uint32Array(256);
    for (let n = 0; n < 256; n++) {
        let c = n;
        for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
        t[n] = c >>> 0;
    }
    return t;
})();

function crc32(buf) {
    let c = 0xffffffff;
    for (let i = 0; i < buf.length; i++) c = CRC_TABLE[(c ^ buf[i]) & 0xff] ^ (c >>> 8);
    return (c ^ 0xffffffff) >>> 0;
}

function chunk(type, data) {
    const len = Buffer.alloc(4);
    len.writeUInt32BE(data.length);
    const typeBuf = Buffer.from(type, 'ascii');
    const crcBuf = Buffer.alloc(4);
    crcBuf.writeUInt32BE(crc32(Buffer.concat([typeBuf, data])));
    return Buffer.concat([len, typeBuf, data, crcBuf]);
}

// encodePng(width, height, pixelFn) where pixelFn(x, y) returns [r,g,b,a].
function encodePng(width, height, pixelFn) {
    const ihdr = Buffer.alloc(13);
    ihdr.writeUInt32BE(width, 0);
    ihdr.writeUInt32BE(height, 4);
    ihdr[8] = 8;  // bit depth
    ihdr[9] = 6;  // color type RGBA
    const stride = width * 4;
    const raw = Buffer.alloc((stride + 1) * height);
    for (let y = 0; y < height; y++) {
        const rowOff = y * (stride + 1);
        raw[rowOff] = 0; // filter: none
        for (let x = 0; x < width; x++) {
            const [r, g, b, a] = pixelFn(x, y);
            const o = rowOff + 1 + x * 4;
            raw[o] = r; raw[o + 1] = g; raw[o + 2] = b; raw[o + 3] = a;
        }
    }
    return Buffer.concat([
        Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
        chunk('IHDR', ihdr),
        chunk('IDAT', deflateSync(raw)),
        chunk('IEND', Buffer.alloc(0)),
    ]);
}

// --- tests ----------------------------------------------------------------

let failures = 0;
function test(name, fn) {
    try {
        fn();
        console.log(`PASS  ${name}`);
    } catch (err) {
        failures++;
        console.log(`FAIL  ${name}: ${err.message}`);
    }
}

test('header parses width/height/bitDepth/colorType', () => {
    const buf = encodePng(64, 32, () => [0, 0, 0, 255]);
    const h = readPngHeader(buf);
    assert.strictEqual(h.width, 64);
    assert.strictEqual(h.height, 32);
    assert.strictEqual(h.bitDepth, 8);
    assert.strictEqual(h.colorType, 6);
    assert.strictEqual(h.interlace, 0);
});

test('flat white frame is rejected as blank', () => {
    const buf = encodePng(128, 128, () => [255, 255, 255, 255]);
    assert.throws(() => assertNotBlank(buf), /blank\/flat frame/);
});

test('flat black frame is rejected as blank', () => {
    const buf = encodePng(128, 128, () => [0, 0, 0, 255]);
    assert.throws(() => assertNotBlank(buf), /blank\/flat frame/);
});

test('near-flat frame (tiny noise) is rejected as blank', () => {
    const buf = encodePng(128, 128, (x, y) => {
        const v = 200 + ((x * 7 + y * 13) % 3); // stddev of {200,201,202} ~0.8
        return [v, v, v, 255];
    });
    assert.throws(() => assertNotBlank(buf), /blank\/flat frame/);
});

test('checkerboard frame passes (real render has contrast)', () => {
    const buf = encodePng(128, 128, (x, y) =>
        ((x >> 3) + (y >> 3)) % 2 ? [240, 40, 40, 255] : [30, 30, 60, 255]);
    assert.doesNotThrow(() => assertNotBlank(buf));
});

test('gradient frame passes', () => {
    const buf = encodePng(256, 256, (x, y) => [x & 0xff, y & 0xff, 128, 255]);
    assert.doesNotThrow(() => assertNotBlank(buf));
});

test('assertScreenshot rejects non-PNG bytes', async () => {
    // synchronous via writeFileSync in a prior step is overkill; use buffer path
    const buf = Buffer.from('this is not a png at all, just text padding here');
    assert.throws(() => assertNotBlank(Buffer.concat([Buffer.alloc(8), buf])), /PNG missing IHDR|not a PNG/);
});

// assertScreenshot file-level checks: size gate before decode.
test('assertScreenshot rejects undersized file before decode', () => {
    const dir = mkdtempSync(path.join(tmpdir(), 'pngcheck-'));
    try {
        const f = `${dir}/tiny.png`;
        writeFileSync(f, encodePng(16, 16, () => [255, 255, 255, 255]));
        assert.throws(() => assertScreenshot(f), /too small/);
    } finally {
        rmSync(dir, { recursive: true, force: true });
    }
});

test('assertScreenshot rejects blank frame above size floor', () => {
    const dir = mkdtempSync(path.join(tmpdir(), 'pngcheck-'));
    try {
        // Build a large flat frame whose deflated size exceeds 30KB by
        // padding with an uncompressed-ish noise... simpler: lower minBytes
        // to exercise the decode path with a small flat png.
        const f = `${dir}/flat.png`;
        writeFileSync(f, encodePng(64, 64, () => [255, 255, 255, 255]));
        assert.throws(() => assertScreenshot(f, { minBytes: 1 }), /blank\/flat frame/);
    } finally {
        rmSync(dir, { recursive: true, force: true });
    }
});

test('assertScreenshot accepts a detailed frame', () => {
    const dir = mkdtempSync(path.join(tmpdir(), 'pngcheck-'));
    try {
        const f = `${dir}/detail.png`;
        writeFileSync(f, encodePng(64, 64, (x, y) =>
            ((x >> 3) + (y >> 3)) % 2 ? [240, 40, 40, 255] : [30, 30, 60, 255]));
        const size = assertScreenshot(f, { minBytes: 1 });
        assert.ok(size > 0);
    } finally {
        rmSync(dir, { recursive: true, force: true });
    }
});

if (failures > 0) {
    console.log(`\n${failures} failed`);
    process.exit(1);
}
console.log('\nall pngcheck tests passed');
