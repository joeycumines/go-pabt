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

import "testing"

func TestBeliefStatusString(t *testing.T) {
	tests := []struct {
		status BeliefStatus
		want   string
	}{
		{BeliefUnknown, "Unknown"},
		{BeliefKnown, "Known"},
		{BeliefStale, "Stale"},
		{BeliefStatus(99), "Unknown"}, // out-of-range defaults to Unknown
	}
	for _, tt := range tests {
		if got := tt.status.String(); got != tt.want {
			t.Errorf("BeliefStatus(%d).String() = %q, want %q", tt.status, got, tt.want)
		}
	}
}

func TestObservationBeliefsInitializeAsKnown(t *testing.T) {
	w := NewWorld()
	o, ok := Observe(w, Tug1, Reefer1)
	if !ok {
		t.Fatal("Observe failed")
	}

	// All four post-condition keys must be Known.
	keys := []VarKey{
		DeliveredKey(Reefer1),
		HoldingKey(Tug1, Reefer1),
		AtKey(Tug1),
		TagKey(Reefer1),
	}
	for _, k := range keys {
		b, ok := o.BeliefOf(k)
		if !ok {
			t.Fatalf("belief missing for %s", k)
		}
		if b.Status != BeliefKnown {
			t.Errorf("%s: status = %s, want Known", k, b.Status)
		}
	}
}

func TestObservationReadRoutesThroughBelief(t *testing.T) {
	w := NewWorld()
	o, ok := Observe(w, Tug1, Reefer1)
	if !ok {
		t.Fatal("Observe failed")
	}

	// Read should return values since all beliefs are Known.
	val, ok := o.Read(TagKey(Reefer1))
	if !ok {
		t.Fatal("Read(TagKey) returned false for Known belief")
	}
	// REEFER-1's tag starts readable in the default world.
	if val != true {
		t.Errorf("Read(TagKey) = %v, want true", val)
	}
}

func TestObservationReadReturnsFalseForUnknownBelief(t *testing.T) {
	w := NewWorld()
	o, ok := Observe(w, Tug1, Reefer1)
	if !ok {
		t.Fatal("Observe failed")
	}

	// Force a belief to Unknown.
	k := TagKey(Reefer1)
	o.beliefs[k] = Belief{Status: BeliefUnknown, Value: nil, ObservedSeq: 0, ObservedVer: 0}

	_, ok = o.Read(k)
	if ok {
		t.Error("Read returned true for Unknown belief; hidden ground truth leaked")
	}
}

func TestMarkStaleTransitionsKnownToStale(t *testing.T) {
	w := NewWorld()
	o, ok := Observe(w, Tug1, Reefer1)
	if !ok {
		t.Fatal("Observe failed")
	}

	k := TagKey(Reefer1)
	b, _ := o.BeliefOf(k)
	if b.Status != BeliefKnown {
		t.Fatalf("precondition: status = %s, want Known", b.Status)
	}

	o.MarkStale(k)

	b, _ = o.BeliefOf(k)
	if b.Status != BeliefStale {
		t.Errorf("after MarkStale: status = %s, want Stale", b.Status)
	}
	// Value is preserved.
	if b.Value == nil {
		t.Error("MarkStale cleared the belief value")
	}
}

func TestMarkStaleDoesNotAffectUnknown(t *testing.T) {
	w := NewWorld()
	o, ok := Observe(w, Tug1, Reefer1)
	if !ok {
		t.Fatal("Observe failed")
	}

	k := TagKey(Reefer1)
	o.beliefs[k] = Belief{Status: BeliefUnknown}

	o.MarkStale(k)

	b, _ := o.BeliefOf(k)
	if b.Status != BeliefUnknown {
		t.Errorf("MarkStale changed Unknown to %s; should be no-op", b.Status)
	}
}

func TestGenerationTimeReadsBypassBeliefs(t *testing.T) {
	w := NewWorld()
	o, ok := Observe(w, Tug1, Reefer1)
	if !ok {
		t.Fatal("Observe failed")
	}

	// Temp is a generation-time read; it should work even without a belief entry.
	val, ok := o.Read(TempKey(Reefer1))
	if !ok {
		t.Fatal("Read(TempKey) returned false; generation-time reads must bypass beliefs")
	}
	if _, isInt := val.(int); !isInt {
		t.Errorf("Read(TempKey) = %T, want int", val)
	}
}
