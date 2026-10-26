// narrative.mjs - the "player diary": a plain-text account of a journey as
// the player experienced it, written so that even a small model reading it
// cold can tell whether the game worked and whether it was any fun.
//
// Only player-perceivable facts go in: what was pressed, what came into or
// left view, what the player's shots did, what hurt them and whether they
// could see it, which menus were up. Detector findings are threaded in at the
// moment they happened. Quiet stretches are called out explicitly, because
// "nothing happened for 25 seconds" is a finding a reader should not have to
// infer from missing lines.

import { playing, sampleAt, eventTimes, fmt } from './detectors/common.mjs';
import { onScreen, feelProfile } from './detectors/feel.mjs';
import { renderTruth } from './detectors/truth.mjs';

const KIND = { B: 'Brute', S: 'Swarm', C: 'Caster', K: 'Stalker' };
const kind = (id) => KIND[String(id)[0]] || 'enemy';

// Where an enemy appears on SCREEN. The renderer's screen-right is the
// game's strafe-right vector (-cos yaw, sin yaw) - the D3 pixel probe is what
// established which way the screen actually goes; this only labels sides.
function side(s, a) {
    const dx = a.x - s.pos[0], dz = a.z - s.pos[2];
    const d = Math.hypot(dx, dz);
    const fwd = (dx * Math.sin(s.yaw) + dz * Math.cos(s.yaw)) / d;
    const right = (dx * -Math.cos(s.yaw) + dz * Math.sin(s.yaw)) / d;
    if (fwd > 0.9) return 'dead ahead';
    return `ahead-${right > 0 ? 'right' : 'left'}`;
}

function describeInput(m) {
    switch (m.kind) {
        case 'keydown': return `press ${m.key.toUpperCase()}`;
        case 'keyup': return `release ${m.key.toUpperCase()}`;
        case 'keyup-elsewhere': return `release ${m.key.toUpperCase()} while another window has focus`;
        case 'click': return 'click (fire)';
        case 'clickAt': return 'click on the page';
        case 'tap': return `tap ${m.key}`;
        case 'blur': return 'switch to another window';
        case 'refocus': return 'come back to the game window';
        case 'resize': return `resize the window to ${m.w}x${m.h}`;
        case 'reloaded': return 'reload the page';
        case 'segment': return null;
        default: return null;
    }
}

export function diary(tl, grid, findings) {
    const ps = playing(tl);
    const t0 = (tl.marks[0] && tl.marks[0].t) || (ps[0] && ps[0].t) || 0;
    const at = (t) => `${fmt((t - t0) / 1000, 1).padStart(5)}s`;
    const lines = [];
    const push = (t, text) => lines.push({ t, text });

    // Inputs, with mouse looks merged into one "turn" per gesture.
    let lookAcc = 0, lookT = null;
    const flushLook = () => {
        if (lookT !== null && Math.abs(lookAcc) >= 40) push(lookT, `you move the mouse ${lookAcc > 0 ? 'right' : 'left'} (${Math.round(Math.abs(lookAcc))}px)`);
        lookAcc = 0; lookT = null;
    };
    for (const m of tl.marks) {
        if (m.kind === 'look') { if (lookT === null) lookT = m.t; lookAcc += m.dx; continue; }
        flushLook();
        if (m.kind === 'segment') { push(m.t, `--- ${m.name} ---`); continue; }
        const d = describeInput(m);
        if (d) push(m.t, `you ${d}`);
    }
    flushLook();

    // Menus coming and going.
    let prevMenus = '';
    for (const d of tl.dom) {
        const menus = Object.keys(d.modals || {}).sort().join('+');
        if (menus !== prevMenus) {
            if (menus) push(d.t, `screen shows: ${Object.entries(d.modals).map(([k, v]) => `${k} "${v.text.slice(0, 50)}"`).join(' and ')}`);
            else if (prevMenus) push(d.t, 'menus gone: you see the game');
            prevMenus = menus;
        }
    }

    // Enemies entering / leaving view (10Hz), HP changes with cues.
    if (grid) {
        const inView = new Map();
        let last = -Infinity;
        for (const s of ps) {
            if (s.gs !== 'playing' || s.t - last < 100) continue;
            last = s.t;
            const now = new Map();
            for (const a of s.actors || []) if (a.hp > 0 && onScreen(grid, s, a)) now.set(a.id, a);
            for (const [id, a] of now) if (!inView.has(id)) push(s.t, `a ${kind(id)} (${id}) comes into view ${side(s, a)}, ${fmt(Math.hypot(a.x - s.pos[0], a.z - s.pos[2]), 1)}u away`);
            for (const [id] of inView) if (!now.has(id)) push(s.t, `${kind(id)} (${id}) is no longer in view`);
            inView.clear();
            for (const [id, a] of now) inView.set(id, a);
        }
    }
    for (let i = 1; i < ps.length; i++) {
        const a = ps[i - 1], b = ps[i];
        if (typeof a.hp !== 'number' || b.hp === a.hp || b.t - a.t > 1000) continue;
        if (b.hp < a.hp) {
            const cue = ps.filter((s) => s.t >= b.t - 100 && s.t <= b.t + 300).some((s) => s.flash || s.shake || s.marker);
            const seen = grid ? (b.actors || []).some((x) => x.hp > 0 && onScreen(grid, b, x)) : null;
            push(b.t, `YOU LOSE ${a.hp - b.hp} HP (now ${b.hp}); ${cue ? 'screen flashes/shakes' : 'NO visible feedback'}; ${seen === null ? '' : seen ? 'an enemy is on screen' : 'NO enemy on screen'}`);
        } else push(b.t, `you regain HP (${a.hp} -> ${b.hp})`);
    }
    for (const e of eventTimes(tl)) {
        if (e.t === null) continue;
        if (e.type === 'stunned') push(e.t, `your shot hits ${kind(e.data && e.data.minotaur)} (${e.data && e.data.minotaur}) for ${e.data && e.data.damage}${e.data && e.data.hp <= 0 ? ' and kills it' : ''}`);
        else if (e.type === 'death' && e.data && e.data.source === 'player') push(e.t, `${kind(e.data.id)} (${e.data.id}) dies`);
        else if (e.type === 'caught') push(e.t, 'YOU ARE CAUGHT (game over)');
        else if (e.type === 'win') push(e.t, 'YOU ESCAPE (win)');
        else if (e.type === 'powerup') push(e.t, 'you pick up a power-up');
    }
    for (const m of tl.marks.filter((x) => x.kind === 'click')) {
        const hit = eventTimes(tl).some((e) => e.type === 'stunned' && e.t >= m.t - 50 && e.t <= m.t + 700);
        if (!hit) push(m.t + 1, 'your shot does nothing');
    }
    for (const f of findings) if (f.t !== null) push(f.t + 0.5, `!! [${f.severity} ${f.id}] ${f.title}`);

    lines.sort((a, b) => a.t - b.t);
    // Quiet-stretch markers: >8s between player-visible happenings.
    const out = [];
    for (let i = 0; i < lines.length; i++) {
        if (i && lines[i].t - lines[i - 1].t > 8000) out.push(`${at(lines[i - 1].t + 1)}  ... ${fmt((lines[i].t - lines[i - 1].t) / 1000, 0)}s pass with nothing new on screen ...`);
        out.push(`${at(lines[i].t)}  ${lines[i].text}`);
    }
    return out;
}

// plainVerdicts turns the feel numbers into sentences with explicit bands,
// so the reader is told what the number means instead of having to know.
export function plainVerdicts(tl, grid) {
    const p = feelProfile(tl, grid);
    const r = renderTruth(tl);
    if (!p) return ['(no gameplay recorded)'];
    const v = [];
    const band = (val, good, bad, unit = '') => (val <= good ? 'OK' : val >= bad ? 'BAD' : 'WEAK') + ` (${val}${unit})`;
    v.push(`Seconds of actual play: ${p.playSeconds}.`);
    v.push(`An enemy was on screen ${p.enemyOnScreenPct}% of the time. ${p.enemyOnScreenPct < 15 ? 'BAD: the player mostly sees empty corridors.' : p.enemyOnScreenPct < 35 ? 'WEAK: long stretches without anything to react to.' : 'OK.'}`);
    v.push(`Longest stretch with nothing happening: ${p.longestDeadTimeS}s - ${band(p.longestDeadTimeS, 10, 20, 's')}.`);
    if (p.enemyOnScreenPct === 0) v.push('Enemies standing still: n/a - no enemy was ever on screen.');
    else {
        const b = band(p.onScreenStillPct, 30, 60, '%');
        v.push(`Enemies on screen were standing still ${p.onScreenStillPct}% of the time - ${b}${b.startsWith('OK') ? '' : '. Enemies that stand still have no presence'}.`);
    }
    if (p.mobClosedInPct !== null) {
        const b = p.mobClosedInPct >= 50 ? 'OK' : p.mobClosedInPct >= 25 ? 'WEAK' : 'BAD';
        v.push(`When an enemy could see the player, it came closer on screen ${p.mobClosedInPct}% of the time - ${b}${b === 'OK' ? '' : ': enemies that ignore the player are not a threat'}.`);
    } else v.push('Enemy pursuit on screen: n/a - no enemy had line of sight to the player within 8u.');
    if (p.server) {
        v.push(`On the SERVER (what the AI actually did): enemies stood still ${p.server.mobsStillPct}% of the time; when one could see the player it closed in ${p.server.mobClosedInPct === null ? 'n/a' : `${p.server.mobClosedInPct}%`} of the time${p.server.mobClosedInPct !== null && p.server.mobClosedInPct < 50 ? ' - the AI itself rarely comes for the player' : ''}. Compare with the on-screen numbers: a big gap means the drawing, not the AI, is wrong.`);
    }
    v.push(`Enemy-vs-enemy fights visible on screen: ${p.onScreenClashes}; hits the player took: ${p.hitsOnPlayer}.`);
    const popped = p.popIns.filter((x) => x.d < 10).length + p.vanishes.length;
    const jumped = p.teleports.filter((x) => x.visible).length;
    v.push(`Enemies popping in/out in view: ${popped}; visible teleports: ${jumped} - ${popped + jumped ? 'BAD: things should not appear from nothing' : 'OK'}.`);
    if (r) v.push(`Where enemies are drawn vs where the server has them: median ${fmt(r.medianErrU)}u apart, ${r.pctOver1u}% of the time more than 1u apart - ${r.medianErrU > 1 ? 'BAD: the player is looking at a picture that is not the game' : r.p95ErrU > 1 ? 'WEAK' : 'OK'}.`);
    return v;
}
