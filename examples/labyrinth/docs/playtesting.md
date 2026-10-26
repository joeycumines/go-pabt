# Player-eye playtesting

A **player-eye** playtest plays the labyrinth the way a person does and reports what a person would notice.

- **Input** is real: Playwright keyboard and mouse events, with real pointer lock.
- **Perception** is what was on screen: screencast frames of the composited page.
- **Judgement** comes from cross-checks between independent channels: pixels vs. maze geometry, input vs. motion, HUD vs. server state, what the server sent vs. what the client drew. A plain-language diary covers what the numbers cannot.

Use it to verify any change a player could see, and to answer "does the game work, and is it any good?".

Earlier harnesses verified the game's description of itself: JS state, server oracles, bots writing `camera.yaw` directly. They passed while the game had inverted mouse look, enemies drawn frozen in place, a blank first screen, and a crosshair that locked on through walls. The player-eye harness exists because those bugs live in the gap between the game's state and the player's screen.

## Steps

### 1. Run

```bash
cd examples/labyrinth/e2e && npm ci        # once (installs the pinned playwright)
cd examples/labyrinth
node e2e/player/playtest-player.mjs --seed 12345,777,4242 --out <dir> --keep-frames
```

- **Journeys:** `--journeys J1,...` picks a subset. J1 is cold start and controls, J2 runs to the gate and plays again, J3 is combat, J4 is dying and restarting, J5 is robustness (alt-tab with a key held, resize, Escape and re-lock, reload).
- **Build and servers:** the CLI rebuilds the game binary, because `client/` is `go:embed`'ed. Each seed × journey gets its own server process, stopped by PID.
- **Visible window:** a Chrome window opens, using real GPU and real pointer lock. Leave the physical mouse alone while it runs; under pointer lock a real mouse turns the camera and corrupts the look checks. `--headless` exists for machines without a display. It emulates pointer lock and may use software rendering, and the report says so.
- **Exit code:** 0 means no S1/S2 findings. 1 means S1/S2 findings or an invalid run. 2 means the harness itself failed.
- **Re-judging:** `--analyze <dir>/<journey-dir>` re-runs every detector on one saved journey and rewrites its diary; `--rejudge <dir>` does that for a whole run and rebuilds the flipbooks and `findings.md`. Neither replays the game. Pixel verdicts are only trustworthy on runs recorded with `--keep-frames`; on pruned runs they work from the surviving frames and the CLI warns.

**Done when** `<dir>/findings.md` exists and every journey line under "Runs" has frames, a filmstrip and a video, with no `JOURNEY ERROR`.

### 2. Look

Open the evidence and look at it. This step catches what no detector covers yet.

For **every journey**, open `filmstrip.png` and `diary.md`, and skim `video.mp4`. For **every finding**, open its flipbook (`flipbooks/NN-ID.png`). Each frame there sits beside a state panel and a top-down ground-truth map: player, facing ray, enemies, gap cells, gate.

**Done when** you can answer every charter question below for every journey, each answer citing a frame, flipbook or diary line.

**Charter (per journey):**

1. What did I see in the first seconds after entering? Could I tell where I was?
2. Did each input do what a player expects (W forward, D right, mouse right turns right, click fires)?
3. Did enemies look alive? Did they move, come for me, react when shot, die cleanly?
4. Every time I lost HP: could I see what hit me and tell that I was hit?
5. Was anything on screen untrue: HUD, crosshair, enemy positions, menus?
6. Was there a stretch where nothing happened? Would a player keep playing?
7. Did menus, the end screen and restart behave?

### 3. Judge each finding

For each finding, confirm or reject it **from its flipbook and diary**, never from its title alone. Detectors can be wrong: an earlier version blamed missed shots on aim when a nearer enemy had taken the hit.

- A confirmed finding is a bug with its repro line (seed + journey).
- A rejected finding is a detector defect. Fix the detector, add a test case for it in `e2e/player/player.test.mjs`, and re-run `--analyze`.

**Done when** every finding is marked confirmed or rejected with a reason.

### 4. Close the loop on new bug classes

A bug you found by eye in step 2 that no detector flagged is a harness gap. Add a detector, or a fixture and test that would have caught it. `node --test e2e/player/*.test.mjs` must pass, and a test must fail when the new rule is weakened.

**Done when** each eye-found bug class has a red-on-the-bug test.

### 5. Report

Write the confirmed bugs (severity, what the player sees, repro, evidence paths), the rejected findings, the charter answers and the "How each journey played" verdicts. An S3 or design-band finding is a judgement for the game's owner. Report it with its numbers rather than suppressing it.

## Reference

### Ground rules

- **Real input only.** Journeys touch the game only through `Actuator` (`e2e/player/actuate.mjs`). The in-page cheat guard records every call to `__ariadne.teleport/setYaw/fire/setOverlays/toggleBTHud/bots.*/playtest.*/inspect.*`, and any call makes the run INVALID. Reading `__ariadne` getters is allowed: the pilot uses position, maze and visible enemies, which a player gets from the screen and minimap.
- **Errors are evidence.** Every console message, page error, failed request and socket close is recorded unfiltered. If one is genuinely benign, write down why in the report.
- **A green exit code is necessary, not sufficient.** "No findings" requires the charter in step 2.

### Detectors

Severity: **S1** breaks the experience, **S2** is clearly wrong and visible to a player, **S3** is polish or needs a look.

| ID | Catches | Channels compared |
|---|---|---|
| D1 | blank or featureless view (e.g. facing a flat wall at spawn) | screencast luminance spread in the maze region, HUD masked, vs. time held |
| D2 | dead, late, mirrored or stuck keys | measured motion per key vs. other keys; on-screen shift for D; distance travelled after a key was released in another window |
| D3 | dead or inverted mouse look | mouse direction vs. on-screen scene shift (pixels, not yaw) |
| D4 | rubber-banding, snaps, hitches, knockback flinging the player | per-frame camera step vs. max speed; server knockback distance; rAF gaps |
| D5 | lying HUD: crosshair locked through walls, HP bar wrong, HP below 0 | HUD DOM vs. maze line of sight and server HP |
| D6 | HP lost with no flash, shake or marker | server HP vs. visible cue |
| D7 | end screen covered, menus stacked, HUD off-screen or overlapping | `elementFromPoint` and rects |
| D8 | camera inside or through walls; enemies in walls; shots silently blocked by floor-looking gap cells | camera position vs. maze; shots vs. server hit events |
| D9 | world running before the player starts; damage under menus; end-screen click ignored; dirty restart | rounds and HP vs. onboarding marks |
| D10 | page errors, console errors and warnings, failed requests, lost connection | browser streams |
| D11 | player and enemies passing through each other | relative position flips at body range |
| D12 | shots that don't visibly move or stun the enemy | drawn enemy position after a confirmed hit |
| D13 | damage from enemies never on screen | field of view + line of sight in the 1.5s before each HP loss |
| D14 | dead time (nothing on screen, nothing happening) | 10Hz activity scan |
| D15 | enemies with no presence: idle on screen, ignoring a visible player | drawn motion; distance trend while the enemy can see the player |
| D16 | pop-in, teleports, killed enemies left standing as ghosts | drawn actor set vs. death events |
| D17 | enemies drawn somewhere other than where the server has them, or frozen | WebSocket wire tap vs. drawn positions |

D14 and D15 use **design bands**: thresholds for what reads as boring or threatless. They are judgement calls stated beside the raw numbers, so a reader can dispute the band without doubting the measurement.

### Artifacts per journey

| File | What it is |
|---|---|
| `findings.json` | the findings, with evidence |
| `diary.md` | plain-language verdicts plus a timestamped account of what the player did, saw and suffered |
| `flipbooks/` | ±1s frames per finding beside state panels and ground-truth maps |
| `filmstrip.png` | one frame every 2s |
| `video.mp4` | the real frames at real timing (needs ffmpeg) |
| `timeline.json` | the raw recording that `--analyze` re-judges |
| `frames/` | about 1 per second after pruning, all of them with `--keep-frames` |

### Traps

- **Pointer lock** needs the page focused: `lockPointer` calls `bringToFront()` immediately before clicking.
- **Playwright fakes focus.** Every page reports focus by default, so a page behind another tab never receives `blur`. J5 turns this off (`Emulation.setFocusEmulationEnabled`) around its alt-tab step and records whether focus was really lost; if it wasn't, the run reports a HARNESS finding instead of a verdict.
- **The screencast only emits a frame when the screen changes.** What the player saw at time t is the last frame at or before t (`FrameStore.at`). Pruned runs lose the in-between frames, so `--analyze` warns and pixel verdicts there are unreliable.
- **Pixel-shift verdicts need confidence.** Flat, untextured walls correlate weakly. D2/D3 only judge a shift with correlation >= 0.7 and |shift| >= 8px, and only for gestures whose expected shift fits the search window. A missing D3 verdict on a low-texture seed is not a pass; the J1 controls probe is the authoritative check.
- **Mark timestamps can tie** within a millisecond. Order marks by array index, never by time.
- **The world is shared and runs from server start**, before any page opens. That is why each journey gets a fresh server.
- **Golden frames** in `e2e/player/fixtures/` come from a live run (provenance in `fixtures.json`). They pin the pixel detectors to real game frames. When the renderer changes on purpose, refresh them from a new run and note why.
