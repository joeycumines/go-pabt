// report.mjs - turns a journey's timeline + findings into evidence a
// reviewer (human or model) can actually look at:
//
//  - flipbooks/<n>-<id>.png: per finding, the frames from 1s before to 1s
//    after, each beside a state panel and a top-down ground-truth map
//    (maze, gap cells, player + facing, enemies) so "what was shown" sits
//    next to "what was true";
//  - filmstrip.png: one frame every ~2s across the whole journey;
//  - video.mp4: the journey's real frames at their real timing (ffmpeg, if
//    installed);
//  - findings.md / findings.json.
//
// Composite images are rendered by the browser from HTML (no image library);
// the frames inside them are the untouched screencast PNGs.

import { writeFileSync, readFileSync, mkdirSync, existsSync, rmSync, readdirSync } from 'node:fs';
import { execFileSync } from 'node:child_process';
import path from 'node:path';
import { playing, sampleAt, marksOf, fmt } from './detectors/common.mjs';
import { WALL, GAP, PLATFORM } from './maze.mjs';
import { plainVerdicts } from './narrative.mjs';
import { gridOf } from './detectors/index.mjs';

function esc(s) { return String(s).replace(/[&<>"]/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c])); }

function heldKeys(tl, t) {
    const held = new Set();
    for (const m of tl.marks) {
        if (m.t > t) break;
        if (m.kind === 'keydown') held.add(m.key);
        if (m.kind === 'keyup' || m.kind === 'keyup-elsewhere') held.delete(m.key);
    }
    return [...held];
}

export function mapSvg(grid, s, size = 210) {
    if (!grid) return '';
    const c = size / Math.max(grid.w, grid.h);
    const cells = [];
    for (let z = 0; z < grid.h; z++) {
        for (let x = 0; x < grid.w; x++) {
            const t = grid.cell(x, z);
            const fill = t === WALL ? '#3b3f58' : t === GAP ? '#c2410c' : t === PLATFORM ? '#475569' : '#111320';
            cells.push(`<rect x="${x * c}" y="${z * c}" width="${c}" height="${c}" fill="${fill}"/>`);
        }
    }
    const parts = [cells.join('')];
    if (grid.gate) parts.push(`<rect x="${(grid.gate[0] - 0.5) * c}" y="${(grid.gate[1] - 0.5) * c}" width="${c}" height="${c}" fill="#22c55e"/>`);
    if (s && s.pos) {
        for (const a of s.actors || []) {
            if (a.hp <= 0) continue;
            parts.push(`<circle cx="${a.x * c}" cy="${a.z * c}" r="${c * 0.4}" fill="${a.st ? '#94a3b8' : '#ef4444'}"/>`);
        }
        const [px, pz] = [s.pos[0] * c, s.pos[2] * c];
        const ray = grid.castRay(s.pos[0], s.pos[2], Math.sin(s.yaw), Math.cos(s.yaw), 30);
        const rd = Math.min(ray.dist, 30) * c;
        parts.push(`<line x1="${px}" y1="${pz}" x2="${px + Math.sin(s.yaw) * rd}" y2="${pz + Math.cos(s.yaw) * rd}" stroke="#facc15" stroke-width="1.5"/>`);
        parts.push(`<circle cx="${px}" cy="${pz}" r="${c * 0.45}" fill="#e879f9"/>`);
    }
    return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 ${size} ${size}">${parts.join('')}</svg>`;
}

function panel(tl, grid, t, t0) {
    const ps = playing(tl);
    const s = sampleAt(ps, t);
    const d = sampleAt(tl.dom, t);
    const rows = [
        ['t', `${fmt((t - t0) / 1000, 2)}s`],
        ['state', s ? s.gs : '-'],
        ['hp', s ? s.hp : '-'],
        ['pos', s && s.pos ? `${fmt(s.pos[0])}, ${fmt(s.pos[2])}` : '-'],
        ['server', s && s.spos ? `${fmt(s.spos[0])}, ${fmt(s.spos[2])}` : '-'],
        ['yaw', s ? fmt(s.yaw) : '-'],
        ['keys', heldKeys(tl, t).join(' ') || '-'],
        ['crosshair', s && s.aim ? 'LOCKED (red)' : 'idle'],
        ['cue', s ? [s.flash && 'flash', s.shake && 'shake', s.marker && 'marker'].filter(Boolean).join(' ') || '-' : '-'],
        ['menus', d ? Object.keys(d.modals || {}).join(' ') || '-' : '-'],
        ['pointer', s && s.locked ? 'locked' : 'free'],
    ];
    return `<table>${rows.map(([k, v]) => `<tr><th>${k}</th><td>${esc(v)}</td></tr>`).join('')}</table>${mapSvg(grid, s, 170)}`;
}

const CSS = `body{margin:0;background:#0b0c14;color:#e2e8f0;font:12px/1.35 ui-monospace,Menlo,monospace;padding:12px}
h1{font-size:15px;margin:0 0 4px} p{margin:0 0 10px;color:#94a3b8;max-width:1500px}
.row{display:flex;gap:10px;margin-bottom:10px;align-items:flex-start}
.row img{width:640px;height:400px;display:block;border:1px solid #334155}
.row.at img{border:2px solid #f43f5e}
table{border-collapse:collapse;margin-bottom:6px} th{color:#94a3b8;text-align:left;padding:0 8px 0 0;font-weight:normal} td{color:#f8fafc}
.strip{display:grid;grid-template-columns:repeat(4,320px);gap:6px}
.strip figure{margin:0} .strip img{width:320px;height:200px;display:block} figcaption{color:#cbd5e1;font-size:11px}`;

async function renderHtml(browser, html, file, width) {
    const page = await browser.newPage({ viewport: { width, height: 400 }, deviceScaleFactor: 1 });
    await page.setContent(html, { waitUntil: 'load' });
    await page.screenshot({ path: file, fullPage: true });
    await page.close();
}

function dataUri(runDir, frame) {
    return `data:image/png;base64,${readFileSync(path.join(runDir, frame.file)).toString('base64')}`;
}

export async function buildEvidence({ browser, tl, findings, runDir, grid, frames }) {
    const t0 = (tl.marks[0] && tl.marks[0].t) || (tl.samples[0] && tl.samples[0].t) || 0;
    const fbDir = path.join(runDir, 'flipbooks');
    mkdirSync(fbDir, { recursive: true });
    const keep = new Set();
    let n = 0;
    for (const f of findings) {
        if (f.t === null || !tl.frames.length) continue;
        const span = f.until && f.until - f.t > 1500 ? [f.t - 800, f.t, f.t + 800, (f.t + f.until) / 2, f.until] : [f.t - 1000, f.t - 500, f.t, f.t + 500, f.t + 1000];
        const picks = [];
        for (const t of span) {
            const fr = frames.at(t);
            if (fr && !picks.some((p) => p.fr.file === fr.file)) picks.push({ fr, at: t === f.t });
        }
        const rows = picks.map(({ fr, at }) => {
            keep.add(fr.file);
            return `<div class="row${at ? ' at' : ''}"><img src="${dataUri(runDir, fr)}"><div>${panel(tl, grid, fr.t, t0)}</div></div>`;
        }).join('');
        const name = `${String(++n).padStart(2, '0')}-${f.id}.png`;
        const html = `<!doctype html><meta charset="utf-8"><style>${CSS}</style><h1>[${f.severity}] ${f.id}: ${esc(f.title)}</h1><p>${esc(f.detail || '')} ${esc(JSON.stringify(f.evidence))}</p>${rows}<p>Red border = the moment the detector fired. Map: magenta = player, yellow = facing ray to the first wall, red = enemy, grey = stunned enemy, orange = GAP cell (drawn as floor in game), green = gate.</p>`;
        await renderHtml(browser, html, path.join(fbDir, name), 900);
        f.flipbook = path.relative(runDir, path.join(fbDir, name));
    }
    // Filmstrip: one frame every 2s.
    const strip = [];
    let next = -Infinity;
    for (const fr of tl.frames) {
        if (fr.t < next) continue;
        next = fr.t + 2000;
        strip.push(fr);
        keep.add(fr.file);
    }
    const ps = playing(tl);
    const figs = strip.map((fr) => {
        const s = sampleAt(ps, fr.t);
        const seg = marksOf(tl, 'segment').filter((m) => m.t <= fr.t).pop();
        return `<figure><img src="${dataUri(runDir, fr)}"><figcaption>${fmt((fr.t - t0) / 1000, 1)}s ${esc(seg ? seg.name : 'loading')} | ${s ? `${s.gs} hp ${s.hp}` : ''} | keys ${esc(heldKeys(tl, fr.t).join('') || '-')}</figcaption></figure>`;
    }).join('');
    await renderHtml(browser, `<!doctype html><meta charset="utf-8"><style>${CSS}</style><h1>${esc(tl.name)} seed ${esc(tl.seed)} filmstrip (every 2s)</h1><div class="strip">${figs}</div>`, path.join(runDir, 'filmstrip.png'), 1320);
    return keep;
}

export function encodeVideo(tl, runDir) {
    if (!tl.frames.length) return null;
    try { execFileSync('ffmpeg', ['-version'], { stdio: 'ignore' }); } catch { return null; }
    const lines = [];
    for (let i = 0; i < tl.frames.length; i++) {
        const dur = i + 1 < tl.frames.length ? Math.min(2, (tl.frames[i + 1].t - tl.frames[i].t) / 1000) : 0.1;
        lines.push(`file '${path.join(runDir, tl.frames[i].file)}'`, `duration ${dur.toFixed(4)}`);
    }
    lines.push(`file '${path.join(runDir, tl.frames[tl.frames.length - 1].file)}'`);
    const list = path.join(runDir, 'frames.txt');
    writeFileSync(list, lines.join('\n'));
    const out = path.join(runDir, 'video.mp4');
    try {
        execFileSync('ffmpeg', ['-y', '-loglevel', 'error', '-f', 'concat', '-safe', '0', '-i', list, '-fps_mode', 'vfr', '-pix_fmt', 'yuv420p', '-c:v', 'libx264', '-crf', '28', out], { stdio: ['ignore', 'ignore', 'pipe'] });
    } catch (e) {
        console.warn(`video encoding failed (evidence still has flipbooks and filmstrip): ${String(e.stderr || e).slice(0, 200)}`);
        return null;
    } finally {
        rmSync(list, { force: true });
    }
    return path.relative(runDir, out);
}

// pruneFrames keeps only frames that evidence references plus one per second,
// so a run directory stays reviewable without hundreds of MB of PNGs.
export function pruneFrames(tl, runDir, keep) {
    let next = -Infinity;
    for (const fr of tl.frames) {
        if (fr.t >= next) { keep.add(fr.file); next = fr.t + 1000; }
    }
    const dir = path.join(runDir, 'frames');
    if (!existsSync(dir)) return;
    writeFileSync(path.join(runDir, 'PRUNED'), 'frames/ was pruned to evidence + 1/s; pixel detectors need --keep-frames to re-judge this run.\n');
    for (const f of readdirSync(dir)) if (!keep.has(path.join('frames', f))) rmSync(path.join(dir, f));
}

export function findingsMarkdown(runs, meta) {
    const all = runs.flatMap((r) => r.findings.map((f) => ({ ...f, run: r })));
    const count = (sev) => all.filter((f) => f.severity === sev).length;
    const lines = [
        '# Player-eye playtest findings', '',
        `Build: ${meta.commit} | renderer: ${meta.renderer} | ${meta.headless ? 'HEADLESS (pointer lock emulated)' : 'headed, real pointer lock'} | ${meta.date}`, '',
        `**${count('S1')} S1** (breaks the experience) | **${count('S2')} S2** (clearly wrong) | **${count('S3')} S3** (polish / needs a look)`, '',
        'A clean or short list is not a verdict. Open every journey\'s filmstrip and every flipbook and fill in the review charter (docs/playtesting.md) before reporting.', '',
        '| Sev | ID | Finding | Journey | Seed | At | Flipbook |', '|---|---|---|---|---|---|---|',
    ];
    for (const f of all) {
        const t0 = (f.run.tl.marks[0] && f.run.tl.marks[0].t) || 0;
        lines.push(`| ${f.severity} | ${f.id} | ${f.title} | ${f.run.journey} | ${f.run.seed} | ${f.t !== null ? `${fmt((f.t - t0) / 1000, 1)}s` : '-'} | ${f.flipbook ? `${f.run.dirName}/${f.flipbook}` : '-'} |`);
    }
    lines.push('', '## Details', '');
    for (const f of all) {
        lines.push(`### [${f.severity}] ${f.id}: ${f.title} (${f.run.journey}, seed ${f.run.seed})`, '');
        if (f.detail) lines.push(f.detail, '');
        lines.push('```json', JSON.stringify(f.evidence, null, 1), '```', '');
        lines.push(`Repro: \`node e2e/player/playtest-player.mjs --seed ${f.run.seed} --journeys ${f.run.journey}\``, '');
    }
    lines.push('## How each journey played', '');
    for (const r of runs) {
        lines.push(`### ${r.journey}, seed ${r.seed} (diary: ${r.dirName}/diary.md)`, '', ...plainVerdicts(r.tl, gridOf(r.tl)).map((v) => `- ${v}`), '');
    }
    lines.push('## Runs', '');
    for (const r of runs) {
        lines.push(`- ${r.journey} seed ${r.seed}: ${r.durationS}s, ${r.tl.frames.length} frames, ${r.tl.samples.length} samples, ${r.tl.events.length} server events | filmstrip ${r.dirName}/filmstrip.png${r.video ? ` | video ${r.dirName}/${r.video}` : ''}${r.error ? ` | JOURNEY ERROR: ${r.error}` : ''}`);
    }
    return lines.join('\n') + '\n';
}
