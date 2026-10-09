# Asset provenance

Every binary asset shipped in this directory, its source, license, and the
transformation applied. Hana performs the final legality assessment; this
file is the evidence base for it.

## Shipped models (ps1_*.glb)

Source pack: "Animated Creatures PS1 Style" (MonstersPack_Ps1) by Zsky
(@Zsky_01), downloaded from OpenGameArt:
https://opengameart.org/sites/default/files/monsterspack_ps1.zip

License: Creative Commons Attribution 4.0 International (CC BY 4.0), per the
pack's License.txt. Attribution required: "Low Poly Models made by @Zsky_01",
Patreon https://www.patreon.com/Zsky. The in-game credits line and this file
discharge the attribution requirement.

Transformation: the pack's .blend files were converted to glTF-Binary with
Blender 5.2.2 LTS (`bpy.ops.export_scene.gltf`, GLB, animations on, cameras
and lights off). No geometry, rig, or animation edits. Textures were not
carried by the source materials into the export; actors are tinted by faction
element color in the shader instead.

| File | Source creature | Rig | Animations used |
|---|---|---|---|
| ps1_spider.glb | Spider | 23 joints | IdleSpider, WalkSpider, AtackSpider(DamageSpider), DeathSpider |
| ps1_rat.glb | Rat | 22 joints | IdleRat, RunRat, AtackRat, GetDamageRat, DeathRat |
| ps1_wasp.glb | Wasp | 19 joints | IdleWasp, FlyingWasp, AtackWasp, GetDamageWasp, DeathWasp |
| ps1_bat.glb | Bat | 19 joints | IdleBat, FlyingBat, AtackBat, GetDamageBat, DeathBat |
| ps1_eaterplant.glb | EaterPlant | 19 joints | IdleEaterPlant, AtackEaterPlant, GetDamageEaterPlant, DeathEaterPlant |

## Security audit (2026-10-09)

All downloads triple-checked for executable content before use: magic-byte
scan (no ELF/PE/Mach-O/shebang), per-entry archive inventory (only
.obj/.mtl/.png/.txt/.blend/.fbx), and format-internal audit (GLB chunks are
JSON+BIN only, no `scripts` key, no external URIs; BLEND files carry zero
drivers and auto-execute off; the three `#!/` byte hits were coincidental
sequences inside zlib streams). Clean.

## Not shipped / rejected

- Khronos glTF-Sample-Assets (Fox, CesiumMan, BrainStem, RiggedFigure) and
  three.js RobotExpressive (CC0): used only to validate the GLB parsing
  pipeline, then deleted; not part of the game.
- OpenGameArt "Dragon - Fully Animated": PNG sprite-frame sequence, no 3D
  data. Deleted.
- OpenGameArt "50+ Monsters Pack 3D": unrigged OBJ meshes, no animation.
  Deleted.
- OpenGameArt troll.blend / goblin_export.blend: goblin has no armature;
  troll's rig is entangled with editor debris. Deleted.
- TES:IV Oblivion (Hana-approved route, researched 2026-10-09): creature NIFs
  live inside `Oblivion - Meshes.bsa` on the retail disc (archive.org hosts
  disc images); the only rig-preserving NIF->glTF pipeline is Blender's
  NifTools addon. Viable future upgrade path for faction-true creatures
  (Flame/Frost/Storm Atronach, Daedroth); not used now because the CC BY 4.0
  pack above already delivers rigged, animated creatures without a 4.3 GB
  ISO extraction chain.
