# ARIADNE: Gameplay Guide

ARIADNE is an atmospheric first-person escape game built on top of the `go-pabt` planning architecture. The player navigates a living, warring labyrinth populated by four rival elemental factions driven by autonomous behavior trees.

---

## 1. Core Gameplay Loop

1. **Enter the Labyrinth**: The runner spawns at the entrance corridor near `(1, 1)` with 100 HP and Fire affinity.
2. **Explore & Navigate**: Traverse corridors, open platforms, and glowing elemental rifts. Glowing rifts provide tactical refuges: they are walkable by the player and allow line-of-sight shooting, but enemy pathfinding actively avoids crossing them.
3. **Collect Elemental Sigils**: Four elemental sigils are scattered across the quadrant territories of the maze. The player must collect at least **3 of 4 sigils** to unlock the exit gate.
4. **Survive Faction Hunts**: Picking up a sigil immediately triggers a 12-second **Faction Hunt**: every mob of that element acquires the runner's position and coordinates a raid to hunt them down.
5. **Escape the Gate**: Once 3 sigils are collected, the exit beacon near `(19, 19)` shifts from crimson locked to emerald green unlocked. Reaching the gate triggers the escape sequence and presents victory statistics.

---

## 2. Controls & User Interface

### Controls

| Action | Input | Description |
| :--- | :--- | :--- |
| **Move** | `W`, `A`, `S`, `D` | Smooth omnidirectional movement (forward, strafe, reverse) |
| **Sprint** | `Shift` | High-speed dash with FOV kick |
| **Jump** | `Space` | Vertical leap over obstacles |
| **Aim / Look** | `Mouse` | First-person horizontal and vertical camera control (pointer locked) |
| **Elemental Pulse** | `Left Click` | Discharges an elemental projectile along the aim crosshair |
| **BT Tick HUD** | `B` | Toggles the real-time PA-BT decision HUD |
| **Territory Map** | `T` | Toggles the minimap territory influence overlay |
| **BT Inspector** | `Tab` | Opens the PA-BT graph explorer and candidate cost inspector |
| **Restart** | `Click` / `Enter` / `R` | Reloads into a fresh session from the victory or death screen |

### Heads-Up Display (HUD)

- **Reticle**: Honest aim crosshair with real-time wall occlusion checking (turns red only when aiming at a visible enemy with clear line of sight).
- **Health Bar & Text**: Displays current HP (0–100). Flashes on damage and displays directional threat arcs.
- **Sigil Counter**: Tracks collected elemental sigils (`0/3` up to `4/4`).
- **Objective Chip**: Color-coded indicator highlighting whether the gate is locked or ready for extraction.
- **Minimap Radar**: Real-time 2D radar indicating corridor geometry, runner heading, nearby sigils, and perceived threats within awareness range (`<= 6.5u`).
- **Faction Clashes & Intensity**: Real-time counter of autonomous inter-faction battles and world intensity level.

---

## 3. Elemental Affinity & Archetypes

### Elemental Cycle

ARIADNE features a rock-paper-scissors elemental affinity wheel:

$$\text{Fire} \xrightarrow{\quad\times 2\quad} \text{Ice} \xrightarrow{\quad\times 2\quad} \text{Lightning} \xrightarrow{\quad\times 2\quad} \text{Void} \xrightarrow{\quad\times 2\quad} \text{Fire}$$

- **Advantage ($\times 2.0$)**: Dealing damage to a vulnerable element doubles pulse damage and applies significant knockback.
- **Disadvantage ($\times 0.5$)**: Attacking a resisting element halves pulse damage.
- **Elemental Power-ups**: Rotating elemental shards respawn in the maze every 45 seconds. Collecting an active power-up switches the runner's element and immediately restores **+25 HP** (up to the 100 HP ceiling).

### Faction Archetypes

Each elemental faction deploys distinct combat archetypes:

- **Brute** (Mass 3.0, HP 40, Damage 16): Heavy frontline tank. Delivers telegraphed melee slams (0.4s windup) that deal devastating damage and heavy knockback.
- **Swarm** (Mass 0.5, HP 20, Damage 6): Fast, lightweight skirmishers that hunt in packs and close distance rapidly.
- **Caster** (Mass 1.0, HP 25, Damage 8): Ranged artillery. Casts elemental bolts with trailing particles when line-of-sight is established, while maneuvering into attack range.
- **Stalker** (Mass 0.8, HP 25, Damage 12): High-damage flanker that pursues aggressively once detected.

---

## 4. The 12 Design Bands (Fun Gate)

The gameplay balance is validated empirically by the **Fun Gate** across 5 distinct random seeds (`12345, 777, 4242, 31337, 2024`) on the full-run journey (`J6`):

| # | Design Band | Target Threshold | Game Feel Objective |
| :-: | :--- | :--- | :--- |
| **1** | Time to first enemy on screen | `≤ 25 s` | Immediate tension and fast onboarding without boring empty stretches. |
| **2** | Enemy on screen of play | `30% – 75%` | Sustained tactical engagement without overwhelming visual spam. |
| **3** | Longest dead time | `≤ 15 s` | Eliminates lull periods where no active stimuli or threats exist. |
| **4** | On-screen enemies standing still | `≤ 35%` | Prevents lifeless or frozen mobs; enemies actively navigate and fight. |
| **5** | Server mobs closing in | `≥ 60%` | Ensures mobs that detect the runner aggressively advance rather than loitering. |
| **6** | HP lost to never-seen enemies | `≤ 10%` | Guarantees fairness: prevents blind cheap-shots from off-screen angles. |
| **7** | Silent damage occurrences | `0` | Every hit received triggers unmistakable feedback (screen flash, shake, directional arc). |
| **8** | Telegraphed melee hits | `100%` | Every incoming melee strike displays a clear visual windup animation. |
| **9** | Human pilot win rate | `2 – 4 of 5 seeds` | Calibrated difficulty: challenging survival rate (40%–80%) under realistic human play. |
| **10** | Median full run length | `30 – 120 s` | Matches the physical 21×21 corridor geometry for collecting 3 sigils and escaping. |
| **11** | Archetypes engaged per run | `≥ 3 archetypes` | Rich encounter variety across Brutes, Swarms, Casters, and Stalkers. |
| **12** | Visible faction clashes within 8u | `≥ 1 per run` | The world feels alive: autonomous inter-faction battles occur near the player. |
