# Harbor Panic Cinematic Storyboard

> **Fixture context.** This document narrates the five-beat auto-tour in
> `sim/tour.go`: what the planner and the simulator do at each beat. It does not
> describe a UI or an HTTP surface. Where a beat used to end with a sentence
> about what a debug page showed, that sentence is gone — there is no such page
> here — and the planner behaviour it described is unchanged.

This document describes the five-beat narrative arc executed by the cinematic auto-tour system in `sim/tour.go`. Each beat represents a distinct phase of harbor operations designed to showcase the PA-BT planning algorithm's capabilities under escalating complexity.

## Beat 1: Calm Start

The tour begins with four straddle carriers (CRANE-0 through CRANE-3) positioned along the top dock at coordinates (2,2), (11,2), (20,2), and (29,2). Six containers labeled R, G, B, Y, M, C are scattered across the mid-dock area. Four berths wait at the bottom. No storms are scheduled and the human forklift is stationary. Each crane receives exactly two cube-to-goal assignment pairs from the planner via `HarborPlan`, producing compact behavior trees of 30-80 nodes. The PA-BT algorithm expands preconditions lazily: only when a condition fails does the tree grow. During calm operations, the tree remains stable because no external force invalidates existing plans. Success propagates up each tree as cranes pick, carry, and place containers onto their assigned berths.

## Beat 2: Storm Hits

At tick 50, the storm system activates. Berths begin teleporting to new dock positions every 120 ticks, with ghost preview markers appearing 10 ticks before relocation. When a berth moves, every crane whose plan targeted that berth immediately encounters a failed precondition: the position variable no longer matches. The PA-BT planner responds by searching for the first unexpanded failed leaf, expanding it into a new PPA (Precondition-Postcondition-Action) subtree, and resolving conflicts if the new actions clobber existing plans. This shows up as structural tree growth: the added subtrees appear alongside the ones already planned, and the statuses on the surviving subtrees do not all reset. This beat proves that replanning is not a catastrophic reset but an incremental surgical repair of the affected branch.

## Beat 3: Human Steals

The human forklift pursues the nearest visible container using BFS pathfinding through wall corridors. When it reaches pickup distance (1.5 cells), it teleports the container to a random free edge cell. This action invalidates any crane currently executing a pick or carry operation targeting that container. The planner detects the failure on the next tick when the position precondition returns false. Conflict resolution becomes visible here: if CRANE-1 was carrying CONTAINER-3 and the human steals it, CRANE-1's entire action chain collapses. The planner must find a new cube-to-goal pair or re-attempt the same pair at the container's new location. The `resolve()` function may reorder PPA siblings leftward to prioritize the stolen container's recovery over less urgent placements.

## Beat 4: Belief Reveal

When belief mode is enabled, two containers start hidden (rendered as `?` in the TUI). Cranes cannot plan actions involving hidden containers because the position variable returns unknown rather than true or false. As a crane approaches within sensing distance (3 cells), the `templateSense` action generates a reveal operation that transitions the container from hidden to visible. This demonstrates three-valued logic in the planner: conditions can be satisfied, unsatisfied, or unknown. Sense actions appear as additional branches under the action selector, competing with the standard pick/place/move actions. Once revealed, the container enters normal planning flow and subsequent beats treat it identically to always-visible containers.

## Beat 5: Fleet Scale

The final beat activates fleet mode: three independent harbor instances, twelve total plans, identified `fleet-0-bot-0` through `fleet-2-bot-3`. Each harbor keeps its own state, safety counters, and revision tracking, and no plan's bookkeeping is shared with another's. The twelve plans tick concurrently, which is the point of the beat: concurrent multi-plan load degrades nothing. What used to be demonstrated through a twelve-entry plan selector is now asserted by `TestFleet`, which builds all twelve plans, ticks them for 100 rounds, and checks each one's tree.

## Technical Notes

The tour executes via `RunTour(ctx, harbor, DefaultBeats())` which steps the harbor simulation for the specified tick count per beat. Results serialize to JSON via `WriteTourJSON` capturing revision ranges for verification. `TestCinematic` drives this and checks the output structure: five beats, a revision that advanced, and JSON that parses back to five beats. An earlier `cinematic_tour.sh` claimed to validate the same thing and did not - it wrote a hard-coded beat list, checked that its own list had five entries, and reported success without ever calling `RunTour`. It was removed rather than repaired, because its output was a literal. All five beats compose into a single continuous simulation rather than isolated scenarios, meaning state carries forward: a container stolen in beat 3 remains displaced into beats 4 and 5, forcing genuine cross-beat replanning chains that exercise the full PA-BT lifecycle.

---
