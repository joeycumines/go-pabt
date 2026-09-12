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
