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

import "fmt"

// ScenarioConfig holds the parameters applied by a named scenario.
type ScenarioConfig struct {
	Name       string
	StormEvery int
	HumanSpeed float64
	Walls      []*Sprite
}

// Scenarios returns the registry of known scenario configurations.
func Scenarios() map[string]ScenarioConfig {
	return map[string]ScenarioConfig{
		"calm": {
			Name:       "calm",
			StormEvery: 0,
			HumanSpeed: 0,
			Walls:      nil,
		},
		"stormy": {
			Name:       "stormy",
			StormEvery: 60,
			HumanSpeed: 2.5,
			Walls: []*Sprite{
				{ID: "WALL-STORM-0", Kind: KindWall, X: 5, Y: 4, W: 1, H: 3, Rune: '#'},
				{ID: "WALL-STORM-1", Kind: KindWall, X: 12, Y: 6, W: 1, H: 4, Rune: '#'},
				{ID: "WALL-STORM-2", Kind: KindWall, X: 18, Y: 3, W: 1, H: 3, Rune: '#'},
				{ID: "WALL-STORM-3", Kind: KindWall, X: 22, Y: 10, W: 1, H: 4, Rune: '#'},
				{ID: "WALL-STORM-4", Kind: KindWall, X: 28, Y: 4, W: 1, H: 3, Rune: '#'},
				{ID: "WALL-STORM-5", Kind: KindWall, X: 32, Y: 8, W: 1, H: 3, Rune: '#'},
				{ID: "WALL-STORM-6", Kind: KindWall, X: 10, Y: 12, W: 6, H: 1, Rune: '#'},
				{ID: "WALL-STORM-7", Kind: KindWall, X: 20, Y: 13, W: 5, H: 1, Rune: '#'},
			},
		},
		"maze": {
			Name:       "maze",
			StormEvery: 120,
			HumanSpeed: 4,
			Walls: []*Sprite{
				{ID: "WALL-MAZE-0", Kind: KindWall, X: 9, Y: 5, W: 1, H: 8, Rune: '#'},
				{ID: "WALL-MAZE-1", Kind: KindWall, X: 15, Y: 3, W: 1, H: 8, Rune: '#'},
				{ID: "WALL-MAZE-2", Kind: KindWall, X: 22, Y: 5, W: 1, H: 8, Rune: '#'},
				{ID: "WALL-MAZE-3", Kind: KindWall, X: 30, Y: 3, W: 1, H: 8, Rune: '#'},
				{ID: "WALL-MAZE-4", Kind: KindWall, X: 8, Y: 13, W: 24, H: 1, Rune: '#'},
				{ID: "WALL-MAZE-5", Kind: KindWall, X: 8, Y: 2, W: 1, H: 2, Rune: '#'},
			},
		},
	}
}

// ApplyScenario configures the harbor for the named scenario.
// It sets storm frequency, human speed, and wall layout.
// Returns an error if the scenario name is unknown.
func ApplyScenario(harbor *Harbor, name string) error {
	scenarios := Scenarios()
	cfg, ok := scenarios[name]
	if !ok {
		return fmt.Errorf("unknown scenario %q (valid: calm|stormy|maze)", name)
	}
	harbor.mu.Lock()
	harbor.stormEvery = cfg.StormEvery
	harbor.humanSpeed = cfg.HumanSpeed
	harbor.mu.Unlock()
	harbor.SetWalls(cfg.Walls)
	return nil
}

// WallCount returns the number of walls for a given scenario name.
// Returns -1 if the scenario is unknown.
func WallCount(name string) int {
	scenarios := Scenarios()
	cfg, ok := scenarios[name]
	if !ok {
		return -1
	}
	return len(cfg.Walls)
}
