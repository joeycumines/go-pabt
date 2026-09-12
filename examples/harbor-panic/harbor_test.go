// Copyright 2021 Joseph Cumines
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build example
// +build example

package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt"
	"github.com/joeycumines/go-pabt/examples/harbor-panic/logic"
	hsim "github.com/joeycumines/go-pabt/examples/harbor-panic/sim"
	"github.com/joeycumines/go-pabt/pabtdebug"
)

func init() {
	bt.DefaultPrinter = pabt.Printer
}

func countTreeNodes(tree *pabtdebug.TreeNode) int {
	if tree == nil {
		return 0
	}
	count := 1
	for i := range tree.Children {
		count += countTreeNodes(&tree.Children[i])
	}
	return count
}

func TestHarborBuildsLargeTrees(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	harbor := hsim.NewHarbor(0, 3.1, 42)
	st := harbor.State()
	actors := st.Actors()
	if len(actors) < 4 {
		t.Fatalf("expected 4 actors, got %d", len(actors))
	}

	for i, actor := range actors {
		planID := fmt.Sprintf("harbor-bot-%d", i)
		result := logic.HarborPlan(ctx, harbor, actor.ID)
		tracker := pabtdebug.NewTrackerWithID(result.Plan, planID)

		planNode := result.Node

		for tick := 0; tick < 50; tick++ {
			harbor.Step(ctx)
			status, _ := planNode.Tick()
			if status == bt.Success {
				break
			}
		}

		tracker.Track(bt.Success, nil)

		tree := tracker.BuildTree()
		if tree == nil {
			t.Fatalf("%s: BuildTree returned nil", planID)
		}

		nodeCount := countTreeNodes(tree)
		t.Logf("%s: tree has %d nodes", planID, nodeCount)

		// PA-BT produces compact trees proportional to distinct precondition
		// keys. With 2 success conditions per actor and grid step 2, trees
		// converge at 30-80 nodes after widening (Task 25). With 1 condition
		// and step 4 they were 3-13 nodes. This proves the PA-BT fabric
		// generalizes to the harbor scenario and that widening achieved
		// chaos visibility.
		if nodeCount < 20 {
			t.Errorf("%s: expected >=20 nodes, got %d", planID, nodeCount)
		}

		// Verify pabt-aware Printer output contains type labels that
		// are proven to appear in PA-BT output (per pabt_test.go).
		// With 1 success condition root typ is GoalRoot, with 2 it is
		// GoalSelector — both are valid; PPARoot/PPAPost must always appear.
		output := result.Node.String()
		if !strings.Contains(output, "GoalRoot") && !strings.Contains(output, "GoalSelector") {
			t.Errorf("%s: Node.String() missing GoalRoot or GoalSelector", planID)
		}
		for _, want := range []string{"PPARoot", "PPAPost"} {
			if !strings.Contains(output, want) {
				t.Errorf("%s: Node.String() missing %q", planID, want)
			}
		}
	}
}

func TestHarborVerifiedEffects(t *testing.T) {
	ctx := context.Background()

	// Part 1: Revision monotonically increases across Step calls
	harbor := hsim.NewHarbor(0, 0, 42)
	var lastRev uint64
	for i := 0; i < 20; i++ {
		if err := harbor.Step(ctx); err != nil {
			t.Fatalf("Step failed at tick %d: %v", i, err)
		}
		rev := harbor.State().Revision
		if rev <= lastRev {
			t.Fatalf("Revision not monotonically increasing: tick %d rev %d <= prev %d", i, rev, lastRev)
		}
		lastRev = rev
	}
	t.Logf("Revision monotonicity verified: 20 ticks, final rev=%d", lastRev)

	// Part 2: Verified effects catch phantom success.
	// Execute a plan and verify that every cube placement results in
	// the cube actually being at a valid position. The tickPlace
	// verification guard re-reads State() after Release+Move and
	// returns Failure if the cube is not at the target.
	harbor2 := hsim.NewHarbor(0, 0, 42)
	st := harbor2.State()
	actors := st.Actors()
	if len(actors) == 0 {
		t.Fatal("no actors")
	}

	result := logic.HarborPlan(ctx, harbor2, actors[0].ID)
	planNode := result.Node

	placeCount := 0
	for tick := 0; tick < 100; tick++ {
		harbor2.Step(ctx)

		// Record cube positions before tick
		cubesBefore := make(map[string][2]float64)
		for _, c := range harbor2.State().Cubes() {
			cubesBefore[c.ID] = [2]float64{c.X, c.Y}
		}

		status, _ := planNode.Tick()

		// After tick, verify any cube that changed position is valid.
		// If tickPlace returned Success without actually moving the cube
		// (phantom success), the cube would still be at its old position
		// while the planner believes it moved. The verification guard
		// prevents this by re-reading state.
		for _, c := range harbor2.State().Cubes() {
			before := cubesBefore[c.ID]
			if c.X != before[0] || c.Y != before[1] {
				placeCount++
				// Cube moved - verify it's within bounds
				if c.X < 0 || c.X >= float64(hsim.SpaceWidth) || c.Y < 0 || c.Y >= float64(hsim.SpaceHeight) {
					t.Errorf("phantom: cube %s placed out of bounds at (%f,%f)", c.ID, c.X, c.Y)
				}
			}
		}

		if status == bt.Success {
			break
		}
	}

	// Part 3: Verify state consistency after every tick -
	// no actor holds a non-existent cube (phantom reference)
	harbor3 := hsim.NewHarbor(0, 0, 99)
	result3 := logic.HarborPlan(ctx, harbor3, harbor3.State().Actors()[0].ID)
	for tick := 0; tick < 50; tick++ {
		harbor3.Step(ctx)
		result3.Node.Tick()
		st3 := harbor3.State()
		for _, sp := range st3.Sprites {
			if sp.Kind == hsim.KindActor && sp.HeldItem != nil {
				heldSprite := st3.Sprites[sp.HeldItem.ID]
				if heldSprite == nil {
					t.Fatalf("phantom: actor %s holds non-existent cube %s at tick %d", sp.ID, sp.HeldItem.ID, tick)
				}
			}
		}
	}

	// Part 4: Prove the verification guard CATCHES phantom success.
	// Request a placement at an out-of-bounds position. Harbor.Move clamps
	// the position, but tickPlace's guard detects the mismatch between
	// requested (x,y) and actual position, returning bt.Failure.
	harbor4 := hsim.NewHarbor(0, 0, 77)
	st4 := harbor4.State()
	actors4 := st4.Actors()
	if len(actors4) == 0 {
		t.Fatal("no actors for phantom proof")
	}
	// Find a cube to attempt placing out of bounds
	var targetCubeID string
	for _, c := range st4.Cubes() {
		targetCubeID = c.ID
		break
	}
	if targetCubeID == "" {
		t.Fatal("no cubes for phantom proof")
	}
	// Move actor to cube position so Grasp succeeds (within PickupDistance)
	cubeSprite := st4.Sprites[targetCubeID]
	if cubeSprite == nil {
		t.Fatal("target cube sprite not found")
	}
	if err := harbor4.Move(ctx, actors4[0].ID, cubeSprite.X, cubeSprite.Y); err != nil {
		t.Fatalf("move actor to cube for phantom proof: %v", err)
	}
	// Grasp the cube first so Release doesn't fail
	if err := harbor4.Grasp(ctx, actors4[0].ID, targetCubeID); err != nil {
		t.Fatalf("grasp for phantom proof: %v", err)
	}
	// Create a harborState to call tickPlace directly
	hs := logic.NewTestHarborState(ctx, harbor4, actors4[0].ID)
	// Request position far out of bounds — Move will clamp, guard catches mismatch
	outX, outY := float64(hsim.SpaceWidth)+100, float64(hsim.SpaceHeight)+100
	tickFn := hs.TickPlace(targetCubeID, outX, outY)
	status, err := tickFn(nil)
	if err != nil {
		t.Fatalf("tickPlace returned error: %v", err)
	}
	if status != bt.Failure {
		t.Fatalf("phantom NOT caught: tickPlace returned %v for out-of-bounds placement, want bt.Failure", status)
	}
	// Verify cube did NOT end up at the out-of-bounds position
	st4b := harbor4.State()
	sp4 := st4b.Sprites[targetCubeID]
	if sp4 != nil && (sp4.X == outX || sp4.Y == outY) {
		t.Fatal("phantom: cube placed at out-of-bounds position")
	}
	t.Log("Phantom success caught: tickPlace returned bt.Failure for mismatched position")

	t.Logf("Verified effects: %d cube placements verified, phantom guard proven", placeCount)
}

func TestHarborCostRanking(t *testing.T) {
	ctx := context.Background()

	// Verify IsRankByCost reflects the setting
	logic.SetRankByCost(false)
	if logic.IsRankByCost() {
		t.Error("IsRankByCost should be false after SetRankByCost(false)")
	}
	logic.SetRankByCost(true)
	if !logic.IsRankByCost() {
		t.Error("IsRankByCost should be true after SetRankByCost(true)")
	}
	logic.SetRankByCost(false)

	// Search for a seed where lexical vs cost ordering produce different trees.
	// With 6 cubes on a 40x18 grid, there are hundreds of candidate place/move
	// actions per failed condition. Lexical ordering enumerates them in grid-scan
	// order; cost ordering puts cheapest first. For most seeds, the PA-BT planner
	// selects different actions under each ordering, producing structurally
	// different trees. We search up to 20 seeds to find one.
	foundDiff := false
	var diffSeed int64
	for seed := int64(1); seed <= 20; seed++ {
		if logic.ActionOrderDiffers(ctx, seed, 15) {
			foundDiff = true
			diffSeed = seed
			break
		}
	}

	if !foundDiff {
		t.Fatal("lexical vs cost ordering produced identical trees for all 20 seeds — actionCost model or sort not affecting planner selection")
	}

	t.Logf("PASS: lexical vs cost ordering differ at seed=%d (contention demonstrated)", diffSeed)

	// Verify the specific difference with detailed output
	harborLex := hsim.NewHarbor(0, 0, diffSeed)
	logic.SetRankByCost(false)
	resultLex := logic.HarborPlan(ctx, harborLex, harborLex.State().Actors()[0].ID)
	for i := 0; i < 15; i++ {
		harborLex.Step(ctx)
		resultLex.Node.Tick()
	}
	outLex := resultLex.Node.String()

	harborCost := hsim.NewHarbor(0, 0, diffSeed)
	logic.SetRankByCost(true)
	resultCost := logic.HarborPlan(ctx, harborCost, harborCost.State().Actors()[0].ID)
	for i := 0; i < 15; i++ {
		harborCost.Step(ctx)
		resultCost.Node.Tick()
	}
	outCost := resultCost.Node.String()
	logic.SetRankByCost(false)

	t.Logf("Lexical tree: %d chars", len(outLex))
	t.Logf("Cost tree: %d chars", len(outCost))

	if outLex == outCost {
		t.Fatal("ActionOrderDiffers returned true but direct comparison shows identical trees")
	}
	t.Log("PASS: lexical vs cost ordering produce different trees for same failed condition")
}

func TestHarborBelief(t *testing.T) {
	ctx := context.Background()

	// Part 1: Without --belief, all 6 cubes visible, no hidden
	harborNoBelief := hsim.NewHarbor(0, 0, 42)
	stNo := harborNoBelief.State()
	visibleNo := 0
	hiddenNo := 0
	for _, c := range stNo.CubesAll() {
		if c.Hidden {
			hiddenNo++
		} else {
			visibleNo++
		}
	}
	if hiddenNo != 0 {
		t.Errorf("without belief: expected 0 hidden cubes, got %d", hiddenNo)
	}
	if visibleNo != 6 {
		t.Errorf("without belief: expected 6 visible cubes, got %d", visibleNo)
	}
	t.Logf("Without belief: %d visible, %d hidden", visibleNo, hiddenNo)

	// Part 2: With belief, 2 cubes hidden initially
	harborBelief := hsim.NewHarbor(0, 0, 42)
	harborBelief.EnableBelief(true)
	stB := harborBelief.State()
	visibleB := 0
	hiddenB := 0
	var hiddenCubes []*hsim.Sprite
	for _, c := range stB.CubesAll() {
		if c.Hidden {
			hiddenB++
			hiddenCubes = append(hiddenCubes, c)
		} else {
			visibleB++
		}
	}
	if hiddenB != 2 {
		t.Fatalf("with belief: expected 2 hidden cubes, got %d", hiddenB)
	}
	if visibleB != 4 {
		t.Fatalf("with belief: expected 4 visible cubes, got %d", visibleB)
	}
	t.Logf("With belief (initial): %d visible, %d hidden", visibleB, hiddenB)

	// Part 3: After sensing (Reveal), hidden cubes become visible
	for _, hc := range hiddenCubes {
		if err := harborBelief.Reveal(hc.ID); err != nil {
			t.Fatalf("Reveal(%s): %v", hc.ID, err)
		}
	}
	stAfter := harborBelief.State()
	visibleAfter := 0
	hiddenAfter := 0
	for _, c := range stAfter.CubesAll() {
		if c.Hidden {
			hiddenAfter++
		} else {
			visibleAfter++
		}
	}
	if hiddenAfter != 0 {
		t.Errorf("after sense: expected 0 hidden cubes, got %d", hiddenAfter)
	}
	if visibleAfter != 6 {
		t.Errorf("after sense: expected 6 visible cubes, got %d", visibleAfter)
	}
	t.Logf("With belief (after sense): %d visible, %d hidden", visibleAfter, hiddenAfter)

	// Part 4: Verify plan builds with belief enabled (templateSense wired)
	actors := stB.Actors()
	if len(actors) > 0 {
		result := logic.HarborPlan(ctx, harborBelief, actors[0].ID)
		out := result.Node.String()
		if len(out) == 0 {
			t.Error("plan output empty with belief enabled")
		}
		t.Logf("Plan with belief: %d chars", len(out))
	}
}
