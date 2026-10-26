# Bot SDK reference

This is the reference an agent reads before writing a bot for the Labyrinth
playtest harness. It is the same kind of document the RuneScape bot
benchmarks ship with their SDK: the game rules, the surface the bot may
touch, and the contract for the code it writes.

A bot is a **source file** that the harness compiles and runs inside the
real game client. It is not a script that calls a toolchain - it plays the
game through the same input path a human player uses. There are no
teleports, no wall-hacks, and no privileged access to game state.

"Same input path" starts at the client's input state: a bot sets the values
the keyboard and mouse handlers would set and reads game state, not pixels.
That suits exercising AI behaviour. To verify anything a player could see or
feel (controls, rendering, HUD, feedback, pacing), run a player-eye playtest:
[playtesting.md](playtesting.md).

## 1. The game

- **World**: a 21x21 labyrinth. Cell kinds are `0` wall, `1` floor,
  `2` platform, `3` gap. Gaps are holes: the pathfinder will not route
  through them unless you ask for it.
- **You** are the runner. You start in the south-west corner of the maze and
  want to reach the gate in the north-east corner.
- **Actors** hunt you. Each one runs a PA-BT (Partial-Order Behavior Tree)
  brain: it plans backwards from a failed condition, so what it does next
  depends on what it believes about you. The in-game PA-BT HUD shows each
  actor's current action (`Pursue`, `Search`, `Retreat`, `Reinforce`,
  `Recover`, ...) - the bot API exposes that too, as `actor.btAction`.
- **Elements**: Fire beats Ice, Ice beats Lightning, Lightning beats Void,
  Void beats Fire. A pulse does double damage against the element it beats
  and half against the one that beats it.
- **Archetypes**: `0` brute (heavy charger), `1` swarm (pack rusher),
  `2` caster (stands off and fires bolts), `3` stalker (flank ambusher).
- **You die** when your HP reaches zero. **You win** by standing in the gate.

## 2. The bot contract

The harness accepts either form:

```js
// form A (canonical)
module.exports = {
    name: 'my-bot',
    tick(ctx, api) {
        // ... act ...
        return api.command();
    },
};

// form B
function tick(ctx, api) {
    // ... act ...
    return api.command();
}
```

`tick` is called once per rendered frame (60 fps on a GPU-backed browser)
and must return the command to apply. `api.command()` returns the
accumulated command; returning it is the normal thing to do. A bot that
throws is reported with the failing frame index and its stack, and the
session keeps running - the harness records the aftermath so you can see
what the failure looked like.

`ctx` carries `frame` (the tick counter), `time` (seconds since the bot
started), `round` (the server tick) and `dt` (the frame delta).

## 3. The API

Every member below is available on the `api` object handed to `tick`.

### Orientation

| member | kind | meaning |
| --- | --- | --- |
| `api.frame` | number | frames since the bot was loaded |
| `api.time` | number | seconds since the bot was loaded |
| `api.round` | number | authoritative server tick |
| `api.self` | object | `{x, y, z, serverX, serverZ, syncDrift, yaw, pitch, hp, maxHp, element, elementName, phase, intensity}` - you. `serverX`/`serverZ` are the server's authoritative position and `syncDrift` the distance between the two (u); aim and pathing from the server position when drift is large |
| `api.gate` | object | `{x, z}` - the goal |
| `api.start` | object | `{x, z}` - where you began |
| `api.actors` | array | every live actor, nearest first (see below) |
| `api.powerups` | array | `[{x, z, element, elementName}]` - uncollected elemental pickups |
| `api.hazards` | array | `[{x, z, element, ttl}]` - crossfire rifts that damage what stands in them |
| `api.territory` | array | four elements, one per quadrant, currently controlled by that faction |
| `api.maze` | object | `{w, h, grid, start, gate, walkable(x, z), cell(x, z)}` |
| `api.shotsFired` | number | shots the player has actually landed on the server |
| `api.finished` | boolean | true once `api.finish()` was called |

An actor object is
`{id, x, y, z, hp, type, typeName, element, elementName, stunned, strategy, strategyName, dist, los, inFrustum, screenX, screenY, perceivable, btAction, btStatus, btAdvantage}`.
`dist` is the distance from you, `los` is whether you have line of sight to
it, `inFrustum`/`screenX`/`screenY` are the 3D camera frustum projection
(normalised screen coordinates, `null` when outside the view), `perceivable`
is `los && inFrustum` - the actor is one you could actually see and hit on
your screen - and `btAction` is the PA-BT action its brain is currently
running.

### Actions

| member | kind | meaning |
| --- | --- | --- |
| `api.aimAt(x, z)` | action | face the point `(x, z)`; movement keeps its world direction while you turn. When your `syncDrift` exceeds 0.08u the SDK aims from the server position (`serverX`, `serverZ`), matching where the server raycasts pulses from |
| `api.look(yaw, pitch)` | action | set an absolute facing |
| `api.moveTo(x, z, opts)` | action | path to `(x, z)` and steer along it; `opts.arrive` (default `0.35`), `opts.sprint`, `opts.allowGap` |
| `api.moveWorld(dx, dz, scale)` | action | move along a world-space direction, `scale` in `[-1, 1]` |
| `api.moveAwayFrom(x, z, opts)` | action | back off from a point; `opts.keep` is the standoff distance |
| `api.orbit(x, z, opts)` | action | hold a radius around a point and circle it; `opts.radius`, `opts.clockwise` |
| `api.stop()` | action | stop moving |
| `api.fire(on)` | action | pull the trigger; the client enforces its own 0.5 s cooldown |
| `api.sprint(on)` | action | run instead of walk (6.5 u/s instead of 4.0 u/s) |
| `api.jump()` | action | jump |
| `api.note(text)` | action | label the current frame; the label appears in the report |
| `api.log(...args)` | action | write to the bot log; the log is returned in the observation bundle |
| `api.finish(reason)` | action | end the session with a reason |
| `api.command()` | action | the accumulated command; return it from `tick` |

### Queries

| member | kind | meaning |
| --- | --- | --- |
| `api.path(from, to, allowGap)` | query | the walkable waypoints from one point to another |
| `api.los(x1, z1, x2, z2)` | query | is there a clear line of sight |
| `api.nearest(filter)` | query | the closest matching actor, or `null` |
| `api.perceivable(opts)` | query | shorthand for `nearest({ ...opts, visibleOnly: true })` restricted to actors inside the 3D camera frustum (`a.perceivable === true`), or `null` |
| `api.ofType(type)` | query | every actor of an archetype (`0` brute, `1` swarm, `2` caster, `3` stalker) |
| `api.beats(element)` | query | does your element beat this one |
| `api.losesTo(element)` | query | does this element beat yours (never fire at it) |
| `api.advantageAgainst(element)` | query | the damage multiplier: `2`, `1` or `0.5` |

`api.nearest(filter)` accepts `{type, element, maxDist, minDist, visibleOnly,
excludeStunned, test}` where `test` is a predicate on the actor.

## 4. A complete example

```js
module.exports = {
    name: 'gate-runner',

    tick(ctx, api) {
        // Walk to the gate. The SDK routes around walls and gives up on a
        // waypoint it cannot reach instead of grinding into it.
        api.moveTo(api.gate.x, api.gate.z, { arrive: 0.45, sprint: true });

        // Fight back only when something is genuinely close, and never
        // shoot into an elemental disadvantage.
        const threat = api.nearest({ maxDist: 3.0, visibleOnly: true, excludeStunned: true });
        if (threat) {
            api.aimAt(threat.x, threat.z);
            if (api.beats(threat.element) || threat.dist < 1.1) {
                api.fire(true);
            }
            api.note('engage ' + threat.typeName);
        } else {
            api.note('advance to gate');
        }
        return api.command();
    },
};
```

Three more live examples ship with the harness:
`examples/labyrinth/e2e/bots/gate-runner.js`,
`examples/labyrinth/e2e/bots/caster-duelist.js` and
`examples/labyrinth/e2e/bots/loot-runner.js`.

## 5. Running a bot

```sh
node examples/labyrinth/e2e/botlab.mjs \
  --bot examples/labyrinth/e2e/bots/gate-runner.js \
  --seconds 20 --out scratch/botlab
```

Useful flags: `--persona <name>` runs a built-in persona instead of a file,
`--fps` sets the capture rate, `--capture-width` sets the frame width,
`--seed` changes the maze, `--assert-frames N` fails the run when fewer than
`N` frames were captured, and `--max-episodes` bounds how many attempts a
session makes.

The harness prints an **observation bundle** to stdout: the outcome, the
event histogram, any bot error with its frame index, the stuck analysis, the
furthest gate progress, and a list of what to fix.

## 6. The artifacts

```
frames/frame-0001.png ...   every captured frame, one file per frame
frames/index.json           index: frame number, time, round, hp, position, cell, nearest actor, events
contact-sheet-01.png ...    the frames in order, 36 tiles a page
story-sheet.png             the beats of the attempt plus a 2D minimap of the whole trajectory
events.jsonl                the server event stream, one JSON object per line
states.jsonl                the state samples, one JSON object per line
observation.json            the machine-readable observation bundle
report.md                   the same bundle in markdown
bot-source.js               a copy of the bot that ran
```

## 7. Rules the harness enforces

- The bot drives the game through the normal client input path. It cannot
  teleport, and the harness counts teleports (it must stay zero).
- The bot sees the same world a player sees. There is no privileged
  information in the API.
- The bot source is compiled inside the page with `new Function`. It runs
  with the page's privileges: treat it as trusted code and never feed it
  untrusted remote input.
