/*
   Copyright 2026 Joseph Cumines

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

package pabtdebug

import (
	"time"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt"
)

// EffectInfo represents a single effect for JSON serialization.
type EffectInfo struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// TreeNode represents a node in the planning tree for JSON serialization.
type TreeNode struct {
	ID            string           `json:"id"`
	Name          string           `json:"name"`
	NodeType      string           `json:"nodeType"`
	Children      []TreeNode       `json:"children,omitempty"`
	Status        *pabt.NodeStatus `json:"status,omitempty"`
	Effects       []EffectInfo     `json:"effects,omitempty"`
	Condition     string           `json:"condition,omitempty"`
	PostCondition string           `json:"postCondition,omitempty"`
	Frame         string           `json:"frame,omitempty"`
	StructureHash string           `json:"structureHash,omitempty"`
}

// TickEvent represents a single tick event for streaming and history.
type TickEvent struct {
	Iteration     int          `json:"iteration"`
	Status        bt.Status    `json:"status"`
	Tree          *TreeNode    `json:"tree,omitempty"`
	Timestamp     time.Time    `json:"timestamp"`
	DurationMs    float64      `json:"durationMs,omitempty"`
	NodeCount     int          `json:"nodeCount,omitempty"`
	BreakpointHit *Breakpoint  `json:"breakpointHit,omitempty"`
}

// NodeProfile holds aggregated profiling data for a single node.
type NodeProfile struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	NodeType        string  `json:"nodeType"`
	TickCount       int     `json:"tickCount"`
	SuccessCount    int     `json:"successCount"`
	FailureCount    int     `json:"failureCount"`
	RunningCount    int     `json:"runningCount"`
	AvgDurationMs   float64 `json:"avgDurationMs"`
	TotalDurationMs float64 `json:"totalDurationMs"`
	LastStatus      string  `json:"lastStatus"`
}

// TimelineEntry is a lightweight summary for the timeline view.
type TimelineEntry struct {
	Iteration  int       `json:"iteration"`
	Status     string    `json:"status"`
	Timestamp  time.Time `json:"timestamp"`
	DurationMs float64   `json:"durationMs"`
	NodeCount  int       `json:"nodeCount"`
}

// SearchResult represents a matched node in a search.
type SearchResult struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	NodeType string `json:"nodeType"`
	Path     string `json:"path"`
}

// DiffResult represents the diff between two tree snapshots.
type DiffResult struct {
	FromIteration int        `json:"fromIteration"`
	ToIteration   int        `json:"toIteration"`
	Added         []DiffNode `json:"added,omitempty"`
	Removed       []DiffNode `json:"removed,omitempty"`
	Changed       []DiffNode `json:"changed,omitempty"`
}

// DiffNode represents a single added/removed/changed node in a diff.
type DiffNode struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	NodeType   string `json:"nodeType"`
	ChangeType string `json:"changeType"`
	OldStatus  string `json:"oldStatus,omitempty"`
	NewStatus  string `json:"newStatus,omitempty"`
}

// Breakpoint represents a debug breakpoint on a node.
type Breakpoint struct {
	ID        string `json:"id"`
	NodePath  string `json:"nodePath"`
	NodeType  string `json:"nodeType"`
	Condition string `json:"condition,omitempty"`
	Enabled   bool   `json:"enabled"`
}
