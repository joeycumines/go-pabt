//go:build example

package netcode

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
)

type GoldenFile struct {
	W     int          `json:"w"`
	H     int          `json:"h"`
	Grid  []int        `json:"grid"`
	Start maze.Vec2    `json:"start"`
	Gate  maze.Vec2    `json:"gate"`
	Steps []GoldenStep `json:"steps"`
}

type GoldenStep struct {
	Step    int       `json:"step"`
	Forward float64   `json:"forward"`
	Strafe  float64   `json:"strafe"`
	Yaw     float64   `json:"yaw"`
	Sprint  bool      `json:"sprint"`
	DT      float64   `json:"dt"`
	InPos   maze.Vec2 `json:"inPos"`
	OutPos  maze.Vec2 `json:"outPos"`
}

func TestMovementGolden(t *testing.T) {
	m := maze.Generate(12345, 21, 21)
	grid := make([]int, len(m.Grid))
	for i, c := range m.Grid {
		grid[i] = int(c.Kind)
	}

	const totalSteps = 200
	const dt = 1.0 / 30.0
	pos := m.Start

	steps := make([]GoldenStep, totalSteps)
	for i := 0; i < totalSteps; i++ {
		// Mixed deterministic inputs: forward, strafe, turning yaw, sprint
		forward := math.Cos(float64(i) * 0.15)
		strafe := math.Sin(float64(i) * 0.22)
		yaw := float64(i) * 0.08
		sprint := (i%4 < 2)

		out := Move(m, pos, yaw, forward, strafe, sprint, dt)
		steps[i] = GoldenStep{
			Step:    i,
			Forward: forward,
			Strafe:  strafe,
			Yaw:     yaw,
			Sprint:  sprint,
			DT:      dt,
			InPos:   pos,
			OutPos:  out,
		}
		pos = out
	}

	golden := GoldenFile{
		W:     m.W,
		H:     m.H,
		Grid:  grid,
		Start: m.Start,
		Gate:  m.Gate,
		Steps: steps,
	}

	dir := filepath.Join("testdata")
	path := filepath.Join(dir, "move_golden.json")

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("failed to create testdata dir: %v", err)
		}
		marshaled, err := json.MarshalIndent(golden, "", "  ")
		if err != nil {
			t.Fatalf("failed to marshal golden file: %v", err)
		}
		if err := os.WriteFile(path, marshaled, 0644); err != nil {
			t.Fatalf("failed to write golden file: %v", err)
		}
		t.Logf("wrote %s with %d steps", path, totalSteps)
		return
	} else if err != nil {
		t.Fatalf("failed to read golden file: %v", err)
	}

	var loaded GoldenFile
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("failed to parse golden file: %v", err)
	}

	if len(loaded.Steps) != totalSteps {
		t.Fatalf("step count mismatch: got %d, want %d", len(loaded.Steps), totalSteps)
	}

	for i, s := range loaded.Steps {
		want := steps[i]
		if math.Abs(s.OutPos.X-want.OutPos.X) > 1e-9 || math.Abs(s.OutPos.Y-want.OutPos.Y) > 1e-9 {
			t.Fatalf("step %d mismatch: got %v, want %v", i, s.OutPos, want.OutPos)
		}
	}
}
