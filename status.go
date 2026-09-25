/*
   Copyright 2021 Joseph Cumines

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package pabt

import (
	"encoding/json"
	"fmt"
	"sync/atomic"

	bt "github.com/joeycumines/go-behaviortree"
)

// statusKey is the unexported key type for retrieving NodeStatus via Value.
type statusKey struct{}

// ValidateStatus reports whether s is one of the three statuses
// go-behaviortree defines.
//
// The dependency derives its statuses from a blank iota, so [bt.Running],
// [bt.Success], and [bt.Failure] are 1, 2, and 3 and the zero value is not a
// status at all. A node that has never ticked therefore has an absent status,
// which is reported by NodeStatusSnapshot.StatusPresent rather than by a
// substituted number.
func ValidateStatus(s bt.Status) error {
	switch s {
	case bt.Running, bt.Success, bt.Failure:
		return nil
	}
	return fmt.Errorf(`pabt: invalid status %d`, int(s))
}

// NodeStatusSnapshot is a coherent view of a node's tick accounting.
//
// Tick count, last status, and version are read from a single immutable value,
// so a reader can never observe a tick count paired with the status of a
// different tick. Version increases once per recorded tick and lets a consumer
// detect that two reads came from different ticks.
type NodeStatusSnapshot struct {
	TickCount     int
	LastStatus    bt.Status
	StatusPresent bool
	Version       uint64
}

// NodeStatus tracks tick counts and last status for a node.
//
// State is published through a single atomic pointer rather than independent
// counters, because the tick goroutine and the debug server goroutine read and
// write concurrently and a torn pair would misreport a node.
type NodeStatus struct {
	state atomic.Pointer[NodeStatusSnapshot]
}

// Snapshot returns the node's current coherent status.
func (s *NodeStatus) Snapshot() NodeStatusSnapshot {
	if s == nil {
		return NodeStatusSnapshot{}
	}
	if v := s.state.Load(); v != nil {
		return *v
	}
	return NodeStatusSnapshot{}
}

// TickCount returns the number of ticks recorded for this node.
func (s *NodeStatus) TickCount() int {
	return s.Snapshot().TickCount
}

// SetTickCount sets the tick count to n.
func (s *NodeStatus) SetTickCount(n int) {
	s.update(func(v NodeStatusSnapshot) NodeStatusSnapshot {
		v.TickCount = n
		return v
	})
}

// IncrTickCount increments the tick count and returns the new value.
func (s *NodeStatus) IncrTickCount() int {
	var out int
	s.update(func(v NodeStatusSnapshot) NodeStatusSnapshot {
		v.TickCount++
		out = v.TickCount
		return v
	})
	return out
}

// LastStatus returns the last status recorded for this node.
func (s *NodeStatus) LastStatus() bt.Status {
	return s.Snapshot().LastStatus
}

// SetLastStatus sets the last status.
func (s *NodeStatus) SetLastStatus(status bt.Status) {
	s.update(func(v NodeStatusSnapshot) NodeStatusSnapshot {
		v.LastStatus = status
		v.StatusPresent = ValidateStatus(status) == nil
		return v
	})
}

// record publishes a completed tick in one atomic step. It is the only writer
// the planner tick path uses, which is what makes the published pair coherent.
func (s *NodeStatus) record(status bt.Status) {
	s.update(func(v NodeStatusSnapshot) NodeStatusSnapshot {
		v.TickCount++
		v.LastStatus = status
		v.StatusPresent = ValidateStatus(status) == nil
		v.Version++
		return v
	})
}

func (s *NodeStatus) update(f func(NodeStatusSnapshot) NodeStatusSnapshot) {
	for {
		old := s.state.Load()
		var cur NodeStatusSnapshot
		if old != nil {
			cur = *old
		}
		next := f(cur)
		if s.state.CompareAndSwap(old, &next) {
			return
		}
	}
}

// MarshalJSON implements custom JSON serialization for NodeStatus,
// reading published state safely.
func (s *NodeStatus) MarshalJSON() ([]byte, error) {
	v := s.Snapshot()
	return json.Marshal(map[string]any{
		"TickCount":     v.TickCount,
		"LastStatus":    v.LastStatus,
		"StatusPresent": v.StatusPresent,
		"Version":       v.Version,
	})
}

// UnmarshalJSON restores NodeStatus from JSON produced by MarshalJSON.
func (s *NodeStatus) UnmarshalJSON(data []byte) error {
	if s == nil {
		return fmt.Errorf("pabt: UnmarshalJSON on nil NodeStatus")
	}
	// Handle explicit null
	if string(data) == "null" {
		return nil
	}
	// StatusPresent is a pointer so a blob written before the field existed does
	// not decode a recorded status as an absence. Absence is derived from the
	// status when the field is missing, which is the one thing the old format
	// could express.
	var aux struct {
		TickCount     int       `json:"TickCount"`
		LastStatus    bt.Status `json:"LastStatus"`
		StatusPresent *bool     `json:"StatusPresent"`
		Version       uint64    `json:"Version"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	present := ValidateStatus(aux.LastStatus) == nil
	if aux.StatusPresent != nil {
		present = *aux.StatusPresent
	}
	s.state.Store(&NodeStatusSnapshot{
		TickCount:     aux.TickCount,
		LastStatus:    aux.LastStatus,
		StatusPresent: present,
		Version:       aux.Version,
	})
	return nil
}

// GetNodeStatus retrieves the NodeStatus from a bt.Valuer.
func GetNodeStatus(v bt.Valuer) (*NodeStatus, bool) {
	if v == nil {
		return nil, false
	}
	s := v.Value(statusKey{})
	if s == nil {
		return nil, false
	}
	return s.(*NodeStatus), true
}
