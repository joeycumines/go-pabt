//go:build example
// +build example

package main

import (
	"testing"

	hsim "github.com/joeycumines/go-pabt/examples/harbor-panic/sim"
)

func TestScenarioWallCounts(t *testing.T) {
	expected := map[string]int{
		"calm":   0,
		"stormy": 8,
		"maze":   6,
	}
	for name, wantWalls := range expected {
		got := hsim.WallCount(name)
		if got != wantWalls {
			t.Errorf("WallCount(%q) = %d, want %d", name, got, wantWalls)
		}
	}
	// unknown scenario returns -1
	if got := hsim.WallCount("unknown"); got != -1 {
		t.Errorf("WallCount(unknown) = %d, want -1", got)
	}
}

func TestApplyScenarioDistinctLayouts(t *testing.T) {
	scenarios := []string{"calm", "stormy", "maze"}
	wallCounts := make(map[string]int)

	for _, name := range scenarios {
		harbor := hsim.NewHarbor(0, 0, 42)
		if err := hsim.ApplyScenario(harbor, name); err != nil {
			t.Fatalf("ApplyScenario(%q): %v", name, err)
		}
		st := harbor.State()
		wc := len(st.Walls)
		wallCounts[name] = wc
		t.Logf("%s: %d walls", name, wc)
	}

	// Verify distinct counts: calm=0, stormy=8, maze=6
	if wallCounts["calm"] != 0 {
		t.Errorf("calm: got %d walls, want 0", wallCounts["calm"])
	}
	if wallCounts["stormy"] != 8 {
		t.Errorf("stormy: got %d walls, want 8", wallCounts["stormy"])
	}
	if wallCounts["maze"] != 6 {
		t.Errorf("maze: got %d walls, want 6", wallCounts["maze"])
	}

	// Verify all three are distinct from each other
	if wallCounts["calm"] == wallCounts["stormy"] || wallCounts["calm"] == wallCounts["maze"] || wallCounts["stormy"] == wallCounts["maze"] {
		t.Errorf("scenarios not distinct: calm=%d stormy=%d maze=%d",
			wallCounts["calm"], wallCounts["stormy"], wallCounts["maze"])
	}
}

func TestApplyScenarioUnknown(t *testing.T) {
	harbor := hsim.NewHarbor(0, 0, 42)
	err := hsim.ApplyScenario(harbor, "nonexistent")
	if err == nil {
		t.Error("expected error for unknown scenario, got nil")
	}
}
