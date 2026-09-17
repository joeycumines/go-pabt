# Harbor Panic Cinematic Storyboard

This document describes the five-beat narrative arc executed by the cinematic auto-tour system in `sim/tour.go`. Each beat represents a distinct phase of harbor operations designed to showcase the PA-BT planning algorithm's capabilities under escalating complexity.

## Beat 1: Calm Start

The tour begins with four straddle carriers (CRANE-0 through CRANE-3) positioned along the top dock at coordinates (2,2), (11,2), (20,2), and (29,2). Six containers labeled R, G, B, Y, M, C are scattered across the mid-dock area. Four berths wait at the bottom. No storms are scheduled and the human forklift is stationary. Each crane receives exactly two cube-to-goal assignment pairs from the planner via `HarborPlan`, producing compact behavior trees of 30-80 nodes. The PA-BT algorithm expands preconditions lazily: only when a condition fails does the tree grow. During calm operations, the tree remains stable because no external force invalidates existing plans. The debug UI shows green success glyphs propagating up the tree as cranes pick, carry, and place containers onto their assigned berths.

## Beat 2: Storm Hits

At tick 50, the storm system activates. Berths begin teleporting to new dock positions every 120 ticks, with ghost preview markers appearing 10 ticks before relocation. When a berth moves, every crane whose plan targeted that berth immediately encounters a failed precondition: the position variable no longer matches. The PA-BT planner responds by searching for the first unexpanded failed leaf, expanding it into a new PPA (Precondition-Postcondition-Action) subtree, and resolving conflicts if the new actions clobber existing plans. The debug UI captures this as structural tree growth visible through delta encoding: keyframe snapshots show the added subtrees while SSE deltas highlight changed nodes in amber. This beat proves that replanning is not a catastrophic reset but an incremental surgical repair of the affected branch.

## Beat 3: Human Steals

The human forklift pursues the nearest visible container using BFS pathfinding through wall corridors. When it reaches pickup distance (1.5 cells), it teleports the container to a random free edge cell. This action invalidates any crane currently executing a pick or carry operation targeting that container. The planner detects the failure on the next tick when the position precondition returns false. Conflict resolution becomes visible here: if CRANE-1 was carrying CONTAINER-3 and the human steals it, CRANE-1's entire action chain collapses. The planner must find a new cube-to-goal pair or re-attempt the same pair at the container's new location. The `resolve()` function may reorder PPA siblings leftward to prioritize the stolen container's recovery over less urgent placements.

## Beat 4: Belief Reveal

When belief mode is enabled, two containers start hidden (rendered as `?` in the TUI). Cranes cannot plan actions involving hidden containers because the position variable returns unknown rather than true or false. As a crane approaches within sensing distance (3 cells), the `templateSense` action generates a reveal operation that transitions the container from hidden to visible. This demonstrates three-valued logic in the planner: conditions can be satisfied, unsatisfied, or unknown. The debug tree shows sense actions as additional branches under ActionSelector nodes, competing with standard pick/place/move actions. Once revealed, the container enters normal planning flow and subsequent beats treat it identically to always-visible containers.

## Beat 5: Fleet Scale

The final beat activates fleet mode, spawning three independent harbor instances with twelve total plans registered on the debug server. Each harbor maintains its own state, safety counters, and revision tracking. The `/plans` endpoint returns all twelve plan IDs (fleet-0-bot-0 through fleet-2-bot-3). The multi-plan selector dropdown in the debug UI allows switching between any plan's live tree view. Delta encoding operates independently per plan: each Hub broadcasts keyframes every 20 ticks with sparse deltas between them. The profile endpoint returns O(nodes) timing regardless of how many plans are active, proving that the incremental profiling architecture scales linearly with tree size rather than quadratically with plan count. This beat demonstrates that the entire introspection stack (SSE streaming, virtual timeline, SVG diffing, viewport culling) handles concurrent multi-plan workloads without degradation.

## Technical Notes

The tour executes via `RunTour(ctx, harbor, DefaultBeats())` which steps the harbor simulation for the specified tick count per beat. Results serialize to JSON via `WriteTourJSON` capturing revision ranges for verification. The `verify_cinematic.sh` script drives this headlessly and validates the output structure. All five beats compose into a single continuous simulation rather than isolated scenarios, meaning state carries forward: a container stolen in beat 3 remains displaced into beats 4 and 5, forcing genuine cross-beat replanning chains that exercise the full PA-BT lifecycle.
