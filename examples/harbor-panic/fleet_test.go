//go:build example
// +build example

package main

import (
	"context"
	"testing"
	"time"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt"
	"github.com/joeycumines/go-pabt/examples/harbor-panic/logic"
	hsim "github.com/joeycumines/go-pabt/examples/harbor-panic/sim"
)

func TestFleet(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fleet := hsim.NewFleet(3, 42)
	if len(fleet) != 3 {
		t.Fatalf("expected 3 fleet members, got %d", len(fleet))
	}

	var planNodes []bt.Node

	for mi, m := range fleet {
		actors := m.Harbor.State().Actors()
		if len(actors) != 4 {
			t.Fatalf("fleet member %d: expected 4 actors, got %d", mi, len(actors))
		}
		for ai, actor := range actors {
			// The plan identity is built even though nothing registers it: it is
			// the name a debugger would show, and building it for twelve plans is
			// part of what this asserts.
			_ = hsim.FleetPlanID(mi, ai)
			result := logic.HarborPlan(ctx, m.Harbor, actor.ID)
			planNodes = append(planNodes, result.Node)
		}
	}

	if len(planNodes) != 12 {
		t.Fatalf("expected 12 plans, got %d", len(planNodes))
	}

	for tick := 0; tick < 100; tick++ {
		for _, m := range fleet {
			if err := m.Harbor.Step(ctx); err != nil {
				t.Fatalf("tick %d: step error: %v", tick, err)
			}
		}
		for _, pn := range planNodes {
			if _, err := pn.Tick(); err != nil {
				t.Fatalf("tick %d: %v", tick, err)
			}
		}
	}

	for idx, pn := range planNodes {
		nodes := 0
		pabt.Walk(pn, func(*pabt.NodeInfo) bool {
			nodes++
			return true
		})
		if nodes == 0 {
			t.Errorf("plan %d has no nodes after 100 ticks", idx)
		}
	}

	t.Logf("TestFleet: 3 harbors x 4 actors = 12 plans, 100 ticks, all plans built")
}
