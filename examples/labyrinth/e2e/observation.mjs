// observation.mjs - The observation bundle.
//
// A finished playtest session is turned into a bundle an agent can act on:
// what happened, what the bot did wrong, and which concrete frames and
// cells prove it. The bundle is deliberately diagnostic rather than
// descriptive - "the run failed" is not useful, "the bot held cell (7,3)
// for 18 samples while commanding full forward" is.
//
// Everything here is a pure function of the recorded session, so the same
// code path produces the machine-readable bundle, the markdown report and
// the console output.

const SEVERITY = { caught: 5, win: 5, death: 4, hit: 3, stunned: 2, bolt: 2, powerup: 2, spawn: 1, clash: 1 };

// buildObservation assembles the bundle from a finished session. `extra`
// carries the authoritative server plane (telemetry, oracles, events) joined
// onto the browser record; it is optional so plain client-only callers work.
export function buildObservation(input) {
    const {
        run = {}, bot = {}, stats = {}, frames = [], events = [], errors = [], logs = [],
        extra = {},
    } = input;

    const eventCounts = {};
    for (const e of events) {
        eventCounts[e.type] = (eventCounts[e.type] || 0) + 1;
    }

    const outcome = deriveOutcome(frames, eventCounts, run);
    const statsWithStuck = { ...stats };
    if (statsWithStuck.stuckRatio === undefined) {
        statsWithStuck.stuckRatio = statsWithStuck.samples > 0
            ? Number((statsWithStuck.stuckSamples / statsWithStuck.samples).toFixed(3))
            : 0;
    }
    const findings = deriveFindings({ stats: statsWithStuck, outcome, errors, frames, eventCounts, run, extra });

    return {
        run: {
            name: run.name || 'botlab',
            botSource: run.botSource || '',
            persona: run.persona || '',
            seed: run.seed,
            durationSec: run.durationSec || 0,
            renderFps: run.renderFps || 0,
            captureFps: run.captureFps || 0,
            captureWidth: run.captureWidth || 0,
            startedAt: run.startedAt || '',
        },
        bot: {
            name: bot.name || '',
            frames: bot.frames || 0,
            shotsFired: bot.shotsFired || 0,
            teleportsUsed: bot.teleportsUsed || 0,
            maxSyncDrift: bot.maxSyncDrift || 0,
            errors: errors.slice(),
            logs: logs.slice(-50),
        },
        outcome,
        stats: statsWithStuck,
        eventCounts,
        serverTelemetry: extra.serverTelemetry || null,
        serverOracles: extra.serverOracles || [],
        serverEvents: extra.serverEvents || [],
        frames: frames.map((f) => ({
            i: f.i,
            t: f.t,
            round: f.round,
            hp: f.hp,
            x: f.x,
            z: f.z,
            cell: f.cell,
            nearest: f.nearest || null,
            events: f.events || [],
            file: f.file || '',
        })),
        events: events.map((e) => ({ t: e.t, i: e.i, type: e.type, data: e.data === undefined ? null : e.data })),
        findings,
    };
}

// deriveOutcome turns the terminal state and the event stream into a
// one-line verdict plus the numbers that support it.
function deriveOutcome(frames, eventCounts, run) {
    const last = frames.length > 0 ? frames[frames.length - 1] : null;
    const phase = last ? last.phase : 'unknown';
    let gateMin = null;
    let gateMinAt = null;
    for (const f of frames) {
        if (f.gateDistance === null || f.gateDistance === undefined) continue;
        if (gateMin === null || f.gateDistance < gateMin) {
            gateMin = f.gateDistance;
            gateMinAt = { t: f.t, i: f.i, x: f.x, z: f.z };
        }
    }
    // The win event is authoritative: the sampling rate can miss the exact
    // frame where the runner crossed the gate threshold.
    const reachedGate = (gateMin !== null && gateMin <= 1.5) || (eventCounts.win || 0) > 0;
    return {
        phase,
        reachedGate,
        gateDistanceMin: gateMin,
        gateDistanceMinAt: gateMinAt,
        finalHp: last ? last.hp : null,
        finalRound: last ? last.round : null,
        finalPosition: last ? { x: last.x, z: last.z } : null,
        durationSec: run.durationSec || 0,
        kills: eventCounts.death || 0,
    };
}

// deriveFindings is the "what to fix" section. Each finding names a
// concrete frame index, cell or count so a reader can go and look at it.
function deriveFindings({ stats, outcome, errors, frames, eventCounts, run, extra = {} }) {
    const findings = [];

    for (const err of errors) {
        findings.push({
            kind: 'bot-error',
            severity: 5,
            message: `bot threw at frame ${err.frame} (t=${fmt(err.t)}s): ${err.message}`,
            evidence: err.stack ? err.stack.split('\n').slice(0, 3).join(' | ') : '',
        });
    }

    // Frozen gameplay and client/server desync are verdicts, not anecdotes:
    // a run can carry hundreds of frames and still be a dead capture.
    if ((stats.stuckRatio || 0) > 0.25) {
        findings.push({
            kind: 'stuck-ratio',
            severity: 5,
            message: `bot was stuck for ${((stats.stuckRatio || 0) * 100).toFixed(1)}% of sampled frames (threshold <= 25.0%)`,
        });
    }
    // A transient (one or two samples, e.g. a knockback snap) is the
    // reconciler doing its job; sustained desync is the defect.
    const desync = stats.desyncSamples || 0;
    if (desync > Math.max(1, Math.floor((stats.samples || 0) * 0.01))) {
        findings.push({
            kind: 'sync-drift',
            severity: 4,
            message: `client-server sync drift persisted across ${desync} of ${stats.samples || 0} samples ` +
                `(max ${stats.maxSyncDrift}u, threshold: >1% of samples above 0.60u)`,
        });
    }
    if (Array.isArray(extra.serverOracles)) {
        for (const o of extra.serverOracles) {
            if (o && o.verdict === 'FAIL') {
                findings.push({
                    kind: 'server-oracle-fail',
                    severity: 5,
                    message: `server oracle ${o.id} (${o.name || ''}) FAILED: ${o.summary || ''} [${o.metric || ''}]`,
                });
            }
        }
    }

    if (!outcome.reachedGate) {
        const best = outcome.gateDistanceMin;
        if (best === null) {
            findings.push({
                kind: 'no-gate-data',
                severity: 4,
                message: 'the runner never produced a gate distance sample',
            });
        } else {
            findings.push({
                kind: 'gate-not-reached',
                severity: 4,
                message: `never reached the gate: closest approach ${best.toFixed(2)}u` +
                    (outcome.gateDistanceMinAt ? ` at t=${fmt(outcome.gateDistanceMinAt.t)}s (frame ${outcome.gateDistanceMinAt.i})` : ''),
                evidence: outcome.gateDistanceMinAt
                    ? `cell ${Math.floor(outcome.gateDistanceMinAt.x)},${Math.floor(outcome.gateDistanceMinAt.z)}`
                    : '',
            });
        }
    }

    const samples = stats.samples || 0;
    if (samples > 0 && (stats.stuckSamples || 0) > samples * 0.05) {
        const worst = (stats.stuckCells || [])[0];
        findings.push({
            kind: 'stuck',
            severity: 4,
            message: `commanded movement without displacement in ${stats.stuckSamples} of ${samples} samples` +
                (worst ? `; worst cell (${worst.cell}) held for ${worst.count} samples` : ''),
            evidence: (stats.stuckCells || []).map((c) => `(${c.cell})x${c.count}`).join(' '),
        });
    }

    const hits = eventCounts.hit || 0;
    const stunned = eventCounts.stunned || 0;
    if (hits > stunned * 2 && hits > 0) {
        findings.push({
            kind: 'unfavourable-trade',
            severity: 3,
            message: `took ${hits} hits but landed only ${stunned} stuns - the bot is not trading favourably`,
        });
    }

    if ((stats.shotsFired || 0) === 0 && frames.length > 0) {
        findings.push({
            kind: 'never-fired',
            severity: 2,
            message: 'the bot never fired a shot',
        });
    }

    if ((stats.cellsVisited || 0) < 8 && frames.length > 0) {
        findings.push({
            kind: 'low-coverage',
            severity: 3,
            message: `the bot only visited ${stats.cellsVisited} distinct maze cells`,
        });
    }

    if (outcome.phase === 'caught' && outcome.durationSec > 0 && outcome.durationSec < 3) {
        findings.push({
            kind: 'early-death',
            severity: 4,
            message: `caught after ${outcome.durationSec.toFixed(1)}s - the bot walked into danger immediately`,
        });
    }

    if (!findings.length) {
        findings.push({
            kind: 'clean',
            severity: 1,
            message: `no anomalies detected: ${stats.cellsVisited || 0} cells visited, gate reached`,
        });
    }

    findings.sort((a, b) => b.severity - a.severity);
    return findings;
}

// formatBundle renders the console observation bundle.
export function formatBundle(obs) {
    const lines = [];
    const rule = '='.repeat(78);
    lines.push(rule);
    lines.push('BOTLAB OBSERVATION BUNDLE');
    lines.push(rule);
    lines.push(`run           : ${obs.run.name}  seed=${obs.run.seed}  duration=${obs.run.durationSec.toFixed(1)}s`);
    lines.push(`bot           : ${obs.bot.name || '(unnamed)'}  source=${obs.run.botSource || obs.run.persona || '-'}`);
    lines.push(`capture       : ${obs.frames.length} frames  render=${obs.run.renderFps.toFixed(1)}fps  capture=${obs.run.captureFps}fps  width=${obs.run.captureWidth}px`);
    lines.push(`bot frames    : ${obs.bot.frames}  shots=${obs.bot.shotsFired}  teleports=${obs.bot.teleportsUsed}  maxSyncDrift=${obs.bot.maxSyncDrift}u`);
    lines.push(`outcome       : ${describeOutcome(obs.outcome)}`);
    lines.push(`events        : ${Object.entries(obs.eventCounts).map(([k, v]) => `${k}=${v}`).join(' ') || '(none)'}`);
    lines.push(`diagnostics   : cellsVisited=${obs.stats.cellsVisited || 0}  distance=${obs.stats.distanceTravelled || 0}u  ` +
        `stuck=${obs.stats.stuckSamples || 0}/${obs.stats.samples || 0} samples  damageTaken=${obs.stats.damageTaken || 0}`);
    if (obs.bot.errors.length) {
        lines.push(`bot errors    : ${obs.bot.errors.length}`);
        for (const e of obs.bot.errors) {
            lines.push(`  - frame ${e.frame}: ${e.message}`);
        }
    } else {
        lines.push('bot errors    : none');
    }
    if (obs.serverOracles && obs.serverOracles.length > 0) {
        lines.push('ORACLE SCORECARD (authoritative server plane):');
        for (const o of obs.serverOracles) {
            lines.push(`  - ${o.id} ${o.name || ''}: ${o.verdict || '?'}  ${o.summary || ''} [${o.metric || ''}]`);
        }
    }
    lines.push('what to fix   :');
    for (const f of obs.findings) {
        lines.push(`  - ${f.message}`);
        if (f.evidence) lines.push(`      evidence: ${f.evidence}`);
    }
    if (obs.bot.logs.length) {
        lines.push('bot log (last) :');
        for (const l of obs.bot.logs.slice(-8)) {
            lines.push(`  - f${l.frame}: ${l.message}`);
        }
    }
    lines.push(rule);
    return lines.join('\n');
}

function describeOutcome(outcome) {
    const parts = [];
    parts.push(outcome.phase === 'won' || outcome.reachedGate ? 'REACHED THE GATE' : `did not reach the gate (closest ${outcome.gateDistanceMin === null ? 'n/a' : outcome.gateDistanceMin.toFixed(2) + 'u'})`);
    parts.push(`hp=${outcome.finalHp}`);
    parts.push(`round=${outcome.finalRound}`);
    if (outcome.gateDistanceMinAt) {
        parts.push(`closest at t=${fmt(outcome.gateDistanceMinAt.t)}s`);
    }
    return parts.join('  ');
}

// formatReport renders the markdown report written next to the artifacts.
export function formatReport(obs) {
    const lines = [];
    lines.push(`# BotLab playtest report - ${obs.run.name}`);
    lines.push('');
    lines.push(`- bot: \`${obs.bot.name || 'unnamed'}\` (${obs.run.botSource || obs.run.persona || 'built-in'})`);
    lines.push(`- seed: ${obs.run.seed}`);
    lines.push(`- duration: ${obs.run.durationSec.toFixed(1)}s`);
    lines.push(`- captured frames: ${obs.frames.length} at ${obs.run.captureFps}fps (${obs.run.captureWidth}px wide)`);
    lines.push(`- bot frames: ${obs.bot.frames}, shots fired: ${obs.bot.shotsFired}, teleports: ${obs.bot.teleportsUsed}`);
    lines.push('');
    lines.push('## Outcome');
    lines.push('');
    lines.push(describeOutcome(obs.outcome));
    lines.push('');
    lines.push('## Event histogram');
    lines.push('');
    lines.push('| event | count |');
    lines.push('| --- | --- |');
    for (const [k, v] of Object.entries(obs.eventCounts)) {
        lines.push(`| ${k} | ${v} |`);
    }
    if (!Object.keys(obs.eventCounts).length) {
        lines.push('| (none) | 0 |');
    }
    lines.push('');
    lines.push('## Derived diagnostics');
    lines.push('');
    lines.push('| metric | value |');
    lines.push('| --- | --- |');
    lines.push(`| cells visited | ${obs.stats.cellsVisited || 0} |`);
    lines.push(`| distance travelled | ${obs.stats.distanceTravelled || 0}u |`);
    lines.push(`| stuck samples | ${obs.stats.stuckSamples || 0} of ${obs.stats.samples || 0} |`);
    lines.push(`| shots fired | ${obs.stats.shotsFired || 0} |`);
    lines.push(`| stuns landed | ${obs.stats.shotsHit || 0} |`);
    lines.push(`| damage taken | ${obs.stats.damageTaken || 0} |`);
    lines.push('');
    lines.push('## Findings');
    lines.push('');
    for (const f of obs.findings) {
        lines.push(`- **${f.kind}**: ${f.message}${f.evidence ? ` (${f.evidence})` : ''}`);
    }
    if (obs.bot.errors.length) {
        lines.push('');
        lines.push('## Bot errors');
        lines.push('');
        for (const e of obs.bot.errors) {
            lines.push(`- frame ${e.frame} (t=${fmt(e.t)}s): ${e.message}`);
            if (e.stack) {
                lines.push('');
                lines.push('```');
                lines.push(e.stack.split('\n').slice(0, 6).join('\n'));
                lines.push('```');
            }
        }
    }
    lines.push('');
    lines.push('## Frames');
    lines.push('');
    lines.push('| # | t | round | hp | pos | cell | nearest | events |');
    lines.push('| --- | --- | --- | --- | --- | --- | --- | --- |');
    for (const f of obs.frames) {
        const near = f.nearest ? `${f.nearest.id} @${f.nearest.dist}u` : '-';
        lines.push(`| ${f.i} | ${f.t.toFixed(2)}s | ${f.round} | ${f.hp} | ${f.x.toFixed(1)},${f.z.toFixed(1)} | ${f.cell[0]},${f.cell[1]} | ${near} | ${(f.events || []).join(' ') || '-'} |`);
    }
    return lines.join('\n') + '\n';
}

function fmt(v) {
    return typeof v === 'number' ? v.toFixed(2) : String(v);
}
