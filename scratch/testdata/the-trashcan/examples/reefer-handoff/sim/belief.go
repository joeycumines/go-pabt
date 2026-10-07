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

// BeliefStatus describes how fresh an agent's knowledge of a resource is.
// Unknown means the agent has never sensed it; Known means the agent sensed it
// and the world's version has not moved since; Stale means the agent sensed it
// but the world's version for that resource has advanced past the observation.
type BeliefStatus uint8

const (
	// BeliefUnknown means the agent has never confirmed this fact.
	BeliefUnknown BeliefStatus = iota

	// BeliefKnown means the agent confirmed this fact and the world's version
	// for the resource still matches the version at confirmation time.
	BeliefKnown

	// BeliefStale means the agent confirmed this fact but the world's version
	// for the resource has advanced past the confirmation version. A stale
	// belief cannot authorize a pick or place.
	BeliefStale
)

// String renders the status for debugger and TUI display.
func (s BeliefStatus) String() string {
	switch s {
	case BeliefUnknown:
		return "Unknown"
	case BeliefKnown:
		return "Known"
	case BeliefStale:
		return "Stale"
	}
	return "Unknown"
}

// Belief is one fact an agent holds about one resource. It carries the value
// the agent last observed, the status describing whether that value is still
// trustworthy, and the provenance stamps needed to detect staleness.
type Belief struct {
	// Status is the freshness of this belief.
	Status BeliefStatus

	// Value is what the agent believes. Nil when Unknown.
	Value any

	// ObservedSeq is the WorldSeq at which this belief was last confirmed.
	ObservedSeq uint64

	// ObservedVer is the resource-specific version at confirmation time.
	// For tags this is TagVersion; for slots, SlotVersion; for lanes, the
	// topology version. Staleness is detected by comparing this against the
	// current version in the world — never by comparing WorldSeq values
	// across worlds.
	ObservedVer uint64
}
