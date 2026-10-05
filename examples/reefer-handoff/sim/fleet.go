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
	"fmt"
)

// FleetID names one isolated cell. Each fleet has its own WorldLoop, world
// state, sequence counter, revision history, and shutdown barrier. No API
// compares revisions across fleets — the type system prevents it because
// each fleet holds a distinct *WorldLoop pointer.
type FleetID string

// Cell is one isolated world and the loop that owns it. Runners attached to
// this cell submit commands only to this loop; no command crosses cells.
type Cell struct {
	ID   FleetID
	Loop *WorldLoop
}

// NewFleet creates independent WorldLoop instances, one per fleet ID. Each
// loop is started before returning. On failure, already-started loops are
// shut down so the caller never leaks goroutines.
func NewFleet(ids []FleetID, mode Mode) ([]Cell, error) {
	cells := make([]Cell, 0, len(ids))
	for _, id := range ids {
		loop, err := NewWorldLoop(mode)
		if err != nil {
			shutdownCells(cells)
			return nil, fmt.Errorf("fleet %s: %w", id, err)
		}
		if err := loop.Start(); err != nil {
			_ = loop.Shutdown(context.Background())
			shutdownCells(cells)
			return nil, fmt.Errorf("fleet %s start: %w", id, err)
		}
		cells = append(cells, Cell{ID: id, Loop: loop})
	}
	return cells, nil
}

// ShutdownCells tears down every cell's loop in reverse order. Errors are
// printed but do not stop the teardown — a stuck loop must not prevent the
// others from releasing their resources.
func ShutdownCells(cells []Cell) {
	shutdownCells(cells)
}

func shutdownCells(cells []Cell) {
	ctx := context.Background()
	for i := len(cells) - 1; i >= 0; i-- {
		if err := cells[i].Loop.Shutdown(ctx); err != nil {
			fmt.Printf("fleet %s shutdown: %v\n", cells[i].ID, err)
		}
	}
}
