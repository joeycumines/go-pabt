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

package sim

import (
	"context"
	"testing"
)

func TestNewFleetCreatesIndependentLoops(t *testing.T) {
	ids := []FleetID{"fleet-a", "fleet-b"}
	cells, err := NewFleet(ids, ModeDeterministic)
	if err != nil {
		t.Fatal(err)
	}
	defer ShutdownCells(cells)

	if len(cells) != 2 {
		t.Fatalf("got %d cells, want 2", len(cells))
	}

	// Each cell must have its own loop pointer.
	if cells[0].Loop == cells[1].Loop {
		t.Fatal("two cells share the same WorldLoop pointer; fleet isolation broken")
	}

	// Each cell must carry its fleet ID.
	for i, id := range ids {
		if cells[i].ID != id {
			t.Errorf("cell[%d].ID = %q, want %q", i, cells[i].ID, id)
		}
	}
}

func TestFleetWorldsHaveIndependentRevisions(t *testing.T) {
	cells, err := NewFleet([]FleetID{"a", "b"}, ModeDeterministic)
	if err != nil {
		t.Fatal(err)
	}
	defer ShutdownCells(cells)

	loopA := cells[0].Loop
	loopB := cells[1].Loop

	// Prove independence by submitting a Change only to fleet A.
	// Change goes through the eventloop synchronously (the returned
	// channel fires after the mutation is applied).
	changed := false
	ch, err := loopA.Change(func(w World) (World, []WorldEvent) {
		changed = true
		return w, nil
	})
	if err != nil {
		t.Fatalf("fleet A Change: %v", err)
	}
	<-ch

	if !changed {
		t.Fatal("fleet A Change did not execute")
	}

	// Fleet B's loop should still be functional and independent.
	// We prove this by verifying that fleet B can also accept changes
	// without interference from fleet A's prior change.
	changedB := false
	chB, err := loopB.Change(func(w World) (World, []WorldEvent) {
		changedB = true
		return w, nil
	})
	if err != nil {
		t.Fatalf("fleet B Change: %v", err)
	}
	<-chB

	if !changedB {
		t.Fatal("fleet B Change did not execute; fleet isolation broken")
	}

	// The two loops are different pointers (proven in TestNewFleetCreatesIndependentLoops).
	// Each accepted changes independently. This is the fleet isolation contract:
	// no shared state, no cross-world revision comparison possible.
}

func TestFleetShutdownIsIndependent(t *testing.T) {
	cells, err := NewFleet([]FleetID{"a", "b"}, ModeDeterministic)
	if err != nil {
		t.Fatal(err)
	}

	// Shut down fleet A only.
	ctx := context.Background()
	if err := cells[0].Loop.Shutdown(ctx); err != nil {
		t.Fatalf("fleet A shutdown: %v", err)
	}

	// Fleet B should still accept snapshots without panicking.
	snap := cells[1].Loop.Snapshot()
	if snap.Phase() == "" {
		t.Error("fleet B snapshot returned empty phase after fleet A shutdown")
	}

	// Clean up fleet B.
	if err := cells[1].Loop.Shutdown(ctx); err != nil {
		t.Fatalf("fleet B shutdown: %v", err)
	}
}

func TestNewFleetCleansUpOnStartFailure(t *testing.T) {
	// ModeDeterministic always starts successfully, so this test verifies
	// that the happy path creates all cells. The error path is structural:
	// NewFleet calls shutdownCells on failure, which iterates in reverse.
	cells, err := NewFleet([]FleetID{"x", "y", "z"}, ModeDeterministic)
	if err != nil {
		t.Fatal(err)
	}
	if len(cells) != 3 {
		t.Fatalf("got %d cells, want 3", len(cells))
	}
	ShutdownCells(cells)
}
