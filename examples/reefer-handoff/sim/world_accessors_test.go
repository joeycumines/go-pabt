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
	"testing"
)

func TestWorldSliceAccessorsReturnDefensiveCopies(t *testing.T) {
	w := NewWorld()

	t.Run("Reefers", func(t *testing.T) {
		got := w.Reefers()
		if len(got) != 3 {
			t.Fatalf("Reefers() returned %d, want 3", len(got))
		}
		// Mutate the copy; the original must be unaffected.
		got[0].TempC = 999
		orig, ok := w.Reefer(got[0].ID)
		if !ok {
			t.Fatal("original reefer not found after mutating copy")
		}
		if orig.TempC == 999 {
			t.Fatal("mutating Reefers() copy changed the world's reefer")
		}
	})

	t.Run("Slots", func(t *testing.T) {
		got := w.Slots()
		if len(got) != 3 {
			t.Fatalf("Slots() returned %d, want 3", len(got))
		}
		got[0].RatedMaxC = 0
		orig, _ := w.Slot(got[0].ID)
		if orig.RatedMaxC == 0 {
			t.Fatal("mutating Slots() copy changed the world's slot")
		}
	})

	t.Run("Tractors", func(t *testing.T) {
		got := w.Tractors()
		if len(got) != 3 {
			t.Fatalf("Tractors() returned %d, want 3", len(got))
		}
		got[0].At = "fake-node"
		orig, _ := w.Tractor(got[0].ID)
		if orig.At == "fake-node" {
			t.Fatal("mutating Tractors() copy changed the world's tractor")
		}
	})

	t.Run("Jobs", func(t *testing.T) {
		got := w.Jobs()
		if len(got) != 3 {
			t.Fatalf("Jobs() returned %d, want 3", len(got))
		}
		got[0].Done = true
		orig, _ := w.Job(got[0].ID)
		if orig.Done {
			t.Fatal("mutating Jobs() copy changed the world's job")
		}
	})

	t.Run("Nodes", func(t *testing.T) {
		got := w.Nodes()
		if len(got) != 7 {
			t.Fatalf("Nodes() returned %d, want 7", len(got))
		}
		got[0].ReeferDestination = !got[0].ReeferDestination
		orig, _ := w.Node(got[0].ID)
		if orig.ReeferDestination == got[0].ReeferDestination {
			t.Fatal("mutating Nodes() copy changed the world's node")
		}
	})
}
