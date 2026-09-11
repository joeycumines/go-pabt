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

// NodeStatus tracks tick counts and last status for a node.
// Fields are accessed atomically to avoid data races between the tick goroutine
// and the debug server goroutine.
type NodeStatus struct {
	tickCount  atomic.Int64
	lastStatus atomic.Int64 // stores bt.Status as int64
}

// TickCount returns the number of ticks recorded for this node.
func (s *NodeStatus) TickCount() int {
	return int(s.tickCount.Load())
}

// SetTickCount sets the tick count to n.
func (s *NodeStatus) SetTickCount(n int) {
	s.tickCount.Store(int64(n))
}

// IncrTickCount increments the tick count and returns the new value.
func (s *NodeStatus) IncrTickCount() int {
	return int(s.tickCount.Add(1))
}

// LastStatus returns the last status recorded for this node.
func (s *NodeStatus) LastStatus() bt.Status {
	return bt.Status(s.lastStatus.Load())
}

// SetLastStatus sets the last status.
func (s *NodeStatus) SetLastStatus(status bt.Status) {
	s.lastStatus.Store(int64(status))
}

// MarshalJSON implements custom JSON serialization for NodeStatus,
// reading atomic fields safely.
func (s *NodeStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"TickCount":  s.TickCount(),
		"LastStatus": s.LastStatus(),
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
	var aux struct {
		TickCount  int       `json:"TickCount"`
		LastStatus bt.Status `json:"LastStatus"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	s.SetTickCount(aux.TickCount)
	s.SetLastStatus(aux.LastStatus)
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
