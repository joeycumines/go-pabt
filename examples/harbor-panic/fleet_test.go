//go:build example
// +build example

package main

import (
	"context"
	"testing"
	"time"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt/examples/harbor-panic/logic"
	hsim "github.com/joeycumines/go-pabt/examples/harbor-panic/sim"
	"github.com/joeycumines/go-pabt/pabtdebug"
)

func TestFleet(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fleet := hsim.NewFleet(3, 42)
	if len(fleet) != 3 {
		t.Fatalf("expected 3 fleet members, got %d", len(fleet))
	}

	var trackers []*pabtdebug.Tracker
	var planNodes []bt.Node

	for mi, m := range fleet {
		actors := m.Harbor.State().Actors()
		if len(actors) != 4 {
			t.Fatalf("fleet member %d: expected 4 actors, got %d", mi, len(actors))
		}
		for ai, actor := range actors {
			planID := hsim.FleetPlanID(mi, ai)
			result := logic.HarborPlan(ctx, m.Harbor, actor.ID)
			tracker := pabtdebug.NewTrackerWithID(result.Plan, planID)
			trackers = append(trackers, tracker)
			planNodes = append(planNodes, result.Node)
		}
	}

	if len(trackers) != 12 {
		t.Fatalf("expected 12 trackers, got %d", len(trackers))
	}

	for tick := 0; tick < 100; tick++ {
		for _, m := range fleet {
			if err := m.Harbor.Step(ctx); err != nil {
				t.Fatalf("tick %d: step error: %v", tick, err)
			}
		}
		for idx, pn := range planNodes {
			status, err := pn.Tick()
			trackers[idx].Track(status, err)
		}
	}

	for idx, tr := range trackers {
		tree := tr.BuildTree()
		if tree == nil {
			t.Errorf("tracker %d (%s): BuildTree returned nil", idx, tr.ID())
		}
		ev := tr.Events()
		if len(ev) == 0 {
			t.Errorf("tracker %d (%s): no events", idx, tr.ID())
		}
	}

	t.Logf("TestFleet: 3 harbors x 4 actors = 12 plans, 100 ticks, all trees built")
}
