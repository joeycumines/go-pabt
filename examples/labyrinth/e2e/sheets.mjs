// sheets.mjs - Contact sheet and story sheet rendering for the playtest
// harness.
//
// A sheet is a grid of real captured frames with an annotation bar under
// each one. The sheets are rendered in a headless page and screenshotted,
// which gives real text rendering instead of canvas fillText.
//
// The story sheet's beats are selected from the recorded session - the first
// frame, the frame that follows each notable event, the frame with the
// greatest gate progress and the final frame - rather than from a fixed
// list of positions. A bot that never reaches the gate still produces a
// story sheet that tells the truth about what happened.

export const TILE_W = 360;
export const TILE_H = 225;
export const CAPTION_H = 38;

const EVENT_STYLE = {
    hit: ['#ff6b6b', 'HIT'],
    stunned: ['#c084fc', 'STUN'],
    death: ['#f85149', 'DEATH'],
    bolt: ['#58a6ff', 'BOLT'],
    clash: ['#d29922', 'CLASH'],
    spawn: ['#3fb950', 'SPAWN'],
    powerup: ['#f778ba', 'POWER'],
    win: ['#3fb950', 'WIN'],
    caught: ['#f85149', 'CAUGHT'],
};

export function escapeHtml(s) {
    return String(s)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;');
}

function badges(events) {
    if (!events || events.length === 0) return '';
    const seen = new Set();
    const out = [];
    for (const e of events) {
        if (seen.has(e)) continue;
        seen.add(e);
        const style = EVENT_STYLE[e];
        if (!style) continue;
        out.push(`<span class="badge" style="background:${style[0]}">${style[1]}</span>`);
    }
    return out.join('');
}

function caption(f, index) {
    const near = f.nearest
        ? `${escapeHtml(f.nearest.id)} @${f.nearest.dist.toFixed(1)}u${f.nearest.action ? ' [' + escapeHtml(f.nearest.action) + ']' : ''}`
        : 'no actor in range';
    return `
      <figcaption>
        <div class="row1">
          <span class="idx">#${String(index).padStart(3, '0')}</span>
          <span class="t">t=${f.t.toFixed(2)}s</span>
          <span class="round">R${f.round}</span>
          <span class="hp">HP ${f.hp}</span>
          <span class="pos">(${f.x.toFixed(1)}, ${f.z.toFixed(1)})</span>
          <span class="cell">cell ${f.cell[0]},${f.cell[1]}</span>
          <span class="badges">${badges(f.events)}</span>
        </div>
        <div class="row2">${near}</div>
      </figcaption>`;
}

const BASE_CSS = `
  * { box-sizing: border-box; }
  body { margin: 0; background: #0d1117; color: #e6edf3;
         font: 12px ui-monospace, SFMono-Regular, Menlo, monospace; }
  header { padding: 10px 14px 8px; border-bottom: 1px solid #30363d; }
  header h1 { margin: 0 0 3px; font-size: 15px; color: #58a6ff; }
  header .meta { color: #8b949e; font-size: 11px; }
  .grid { display: grid; gap: 6px; padding: 8px; }
  .tile { margin: 0; border: 1px solid #30363d; background: #010409; overflow: hidden; }
  .tile img { display: block; width: ${TILE_W}px; height: ${TILE_H}px; object-fit: cover; }
  figcaption { padding: 3px 5px; background: #0d1117; height: ${CAPTION_H}px; }
  .row1 { display: flex; gap: 8px; align-items: center; white-space: nowrap; }
  .row2 { color: #8b949e; margin-top: 2px; white-space: nowrap; overflow: hidden; }
  .idx { color: #58a6ff; font-weight: bold; }
  .hp { color: #7ee787; }
  .pos, .cell { color: #d29922; }
  .badge { color: #010409; font-weight: bold; padding: 0 4px; border-radius: 3px; margin-left: 4px; }
  .story .tile img { height: 300px; }
  .story figcaption { height: ${CAPTION_H}px; }
`;

function page(title, meta, gridHtml, extraCss = '', extraJs = '', bodyClass = '') {
    return `<!doctype html><html><head><meta charset="utf-8"><style>${BASE_CSS}${extraCss}</style></head>
<body class="${bodyClass}">
<header><h1>${escapeHtml(title)}</h1><div class="meta">${meta}</div></header>
${gridHtml}
${extraJs ? `<script>${extraJs}</script>` : ''}
</body></html>`;
}

// contactSheetHtml renders one page of the chronological contact sheet.
export function contactSheetHtml(frames, opts) {
    const { title, meta, cols = 6 } = opts;
    const tiles = frames.map((f, k) => `
      <figure class="tile">${caption(f, k)}<img src="${f.dataUrl}" /></figure>`).join('');
    const grid = `<div class="grid" style="grid-template-columns:repeat(${cols}, ${TILE_W}px)">${tiles}</div>`;
    return page(title, meta, grid);
}

// storySheetHtml renders the narrative sheet: one beat per cell with a 2D
// minimap inset on the final cell.
export function storySheetHtml(frames, opts) {
    const { title, meta, maze, trail, cols = 3 } = opts;
    const tiles = frames.map((f, k) => `
      <figure class="tile">${caption(f, k)}<img src="${f.dataUrl}" /></figure>`).join('');
    const mapScript = `
      const maze = ${JSON.stringify(maze || null)};
      const trail = ${JSON.stringify(trail || [])};
      const actors = ${JSON.stringify(opts.actors || [])};
      const cv = document.getElementById('minimap');
      if (cv && maze) {
        const ctx = cv.getContext('2d');
        const size = cv.width;
        const pad = 8;
        const inner = size - pad * 2;
        const scale = inner / maze.w;
        const toScreen = (x, y) => ({ x: pad + x * scale, y: pad + y * scale });
        ctx.fillStyle = '#0d1117';
        ctx.fillRect(0, 0, size, size);
        for (let cz = 0; cz < maze.h; cz++) {
          for (let cx = 0; cx < maze.w; cx++) {
            const kind = maze.grid[cz * maze.w + cx];
            if (kind === 0) {
              ctx.fillStyle = '#21262d';
              ctx.fillRect(pad + cx * scale, pad + cz * scale, Math.max(1, scale), Math.max(1, scale));
            }
          }
        }
        const s = toScreen(maze.startX, maze.startY);
        ctx.fillStyle = '#3fb950';
        ctx.beginPath(); ctx.arc(s.x, s.y, 3.5, 0, Math.PI * 2); ctx.fill();
        const g = toScreen(maze.gateX, maze.gateY);
        ctx.fillStyle = '#d29922';
        ctx.beginPath(); ctx.arc(g.x, g.y, 4, 0, Math.PI * 2); ctx.fill();
        if (trail.length > 1) {
          ctx.strokeStyle = '#38bdf8';
          ctx.lineWidth = 1.6;
          ctx.beginPath();
          ctx.moveTo(toScreen(trail[0].x, trail[0].z).x, toScreen(trail[0].x, trail[0].z).y);
          for (let k = 1; k < trail.length; k++) {
            const p = toScreen(trail[k].x, trail[k].z);
            ctx.lineTo(p.x, p.y);
          }
          ctx.stroke();
          const last = toScreen(trail[trail.length - 1].x, trail[trail.length - 1].z);
          ctx.fillStyle = '#44ff66';
          ctx.beginPath(); ctx.arc(last.x, last.y, 3.5, 0, Math.PI * 2); ctx.fill();
        }
        for (const a of actors) {
          const p = toScreen(a.x, a.z);
          ctx.fillStyle = a.stunned ? '#c084fc' : (a.strategy === 2 ? '#ff9900' : '#f85149');
          ctx.beginPath(); ctx.arc(p.x, p.y, 2.2, 0, Math.PI * 2); ctx.fill();
        }
        ctx.fillStyle = '#79c0ff';
        ctx.font = '9px monospace';
        ctx.fillText('2D MINIMAP 21x21', pad, size - 2);
      }
    `;
    const minimap = `<figure class="tile" style="grid-column: 1 / -1; display:flex; gap:8px; align-items:center;">
        <canvas id="minimap" width="300" height="300" style="border:1px solid #30363d;"></canvas>
        <figcaption style="height:auto; flex:1;">
          <div class="row1"><span class="idx">MAP</span><span class="t">whole session trajectory</span></div>
          <div class="row2">cyan = runner trail, green = start, amber = gate, red/amber dots = actors</div>
        </figcaption>
      </figure>`;
    const grid = `<div class="grid" style="grid-template-columns:repeat(${cols}, ${TILE_W}px)">${tiles}${minimap}</div>`;
    return page(title, meta, grid, '.story .tile img { height: 300px; }', mapScript, 'story');
}

// selectStoryBeats picks the beats of the story sheet from the recorded
// session. The selection is derived from the session data, not hard-coded,
// so it works for any bot and any outcome.
export function selectStoryBeats(frames, opts = {}) {
    const maxBeats = opts.maxBeats || 9;
    const cooldown = opts.cooldown || 12;
    const picks = [];
    const taken = new Set();

    const push = (f, why) => {
        if (!f || taken.has(f.i)) return;
        taken.add(f.i);
        picks.push({ frame: f, why });
    };

    if (frames.length === 0) return picks;

    push(frames[0], 'first frame');

    // The frame that follows each notable event, most severe first, with a
    // cooldown so a burst of hits does not crowd out the story.
    const severity = { caught: 5, win: 5, death: 4, hit: 3, stunned: 2, bolt: 2, powerup: 2, spawn: 1, clash: 1 };
    const notable = [];
    for (const f of frames) {
        for (const e of f.events || []) {
            notable.push({ f, e, weight: severity[e] || 1 });
        }
    }
    notable.sort((a, b) => (b.weight - a.weight) || (a.f.i - b.f.i));
    let lastPicked = -Infinity;
    for (const n of notable) {
        if (picks.length >= maxBeats - 2) break;
        if (n.f.i - lastPicked < cooldown) continue;
        push(n.f, `after ${n.e}`);
        lastPicked = n.f.i;
    }

    // The frame with the greatest gate progress (closest approach).
    let best = null;
    let bestD = Infinity;
    for (const f of frames) {
        if (f.gateDistance === null || f.gateDistance === undefined) continue;
        if (f.gateDistance < bestD) {
            bestD = f.gateDistance;
            best = f;
        }
    }
    push(best, 'closest approach to the gate');

    push(frames[frames.length - 1], 'final frame');

    // Fill the remaining slots with evenly spaced frames so the sheet is a
    // complete picture of the attempt rather than a sparse highlight reel.
    if (picks.length < maxBeats && frames.length > picks.length) {
        const wanted = maxBeats - picks.length;
        const step = frames.length / wanted;
        for (let k = 0; k < wanted; k++) {
            const f = frames[Math.min(frames.length - 1, Math.floor(k * step))];
            push(f, `sample ${k + 1}/${wanted}`);
        }
    }

    picks.sort((a, b) => a.frame.i - b.frame.i);
    return picks.slice(0, maxBeats);
}
