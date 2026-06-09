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
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt"
)

// defaultMaxEvents is the default maximum number of events retained by a Tracker.
const defaultMaxEvents = 1000

// Tracker wraps a pabt.IPlan and records TickEvents after each tick.
type Tracker struct {
	plan          *pabt.IPlan
	events        []TickEvent
	mu            sync.Mutex
	iteration     atomic.Int64
	hub           *Hub
	maxEvents     int
	lastTrackTime time.Time
	breakpoints   map[string]*Breakpoint
	breakpointMu  sync.RWMutex
}

// NewTracker creates a new Tracker for the given plan.
func NewTracker(plan *pabt.IPlan) *Tracker {
	t := &Tracker{
		plan:        plan,
		events:      make([]TickEvent, 0),
		hub:         NewHub(),
		maxEvents:   defaultMaxEvents,
		breakpoints: make(map[string]*Breakpoint),
	}
	return t
}

// Track is called after each tick to record the event. It builds a snapshot
// of the current tree and appends it to the event history.
func (t *Tracker) Track(status bt.Status, err error) {
	iter := int(t.iteration.Add(1))

	var durationMs float64
	now := timeNow()
	if !t.lastTrackTime.IsZero() {
		durationMs = now.Sub(t.lastTrackTime).Seconds() * 1000
	}
	t.lastTrackTime = now

	tree := t.BuildTree()
	nodeCount := 0
	if tree != nil {
		nodeCount = countNodes(tree)
	}

	event := TickEvent{
		Iteration:  iter,
		Status:     status,
		Tree:       tree,
		Timestamp:  now,
		DurationMs: durationMs,
		NodeCount:  nodeCount,
	}

	if bp := t.checkBreakpoints(&event); bp != nil {
		event.BreakpointHit = bp
	}

	t.mu.Lock()
	t.events = append(t.events, event)
	if t.maxEvents > 0 && len(t.events) > t.maxEvents {
		t.events = t.events[len(t.events)-t.maxEvents:]
	}
	t.mu.Unlock()
	t.hub.Broadcast(event)
}

// Events returns all recorded tick events.
func (t *Tracker) Events() []TickEvent {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]TickEvent, len(t.events))
	copy(result, t.events)
	return result
}

// Hub returns the SSE hub for this tracker.
func (t *Tracker) Hub() *Hub {
	return t.hub
}

// BuildTree walks the plan's bt.Node tree using bt.Walk and builds a
// TreeNode hierarchy with node types, names, and statuses.
func (t *Tracker) BuildTree() *TreeNode {
	node := t.plan.Node()
	return buildTreeFromMetadata(node, "0")
}

// Timeline returns lightweight timeline entries for all recorded events.
func (t *Tracker) Timeline() []TimelineEntry {
	t.mu.Lock()
	defer t.mu.Unlock()
	entries := make([]TimelineEntry, len(t.events))
	for i, e := range t.events {
		entries[i] = TimelineEntry{
			Iteration:  e.Iteration,
			Status:     statusString(e.Status),
			Timestamp:  e.Timestamp,
			DurationMs: e.DurationMs,
			NodeCount:  e.NodeCount,
		}
	}
	return entries
}

// EventAt returns the tick event for the given iteration number, if it exists.
func (t *Tracker) EventAt(iteration int) (*TickEvent, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := range t.events {
		if t.events[i].Iteration == iteration {
			return &t.events[i], true
		}
	}
	return nil, false
}

// Profile returns aggregated profiling data for all nodes across all events.
func (t *Tracker) Profile() []NodeProfile {
	t.mu.Lock()
	events := make([]TickEvent, len(t.events))
	copy(events, t.events)
	t.mu.Unlock()

	if len(events) == 0 {
		return nil
	}

	profiles := make(map[string]*NodeProfile)

	for _, event := range events {
		if event.Tree == nil {
			continue
		}
		walkTreeForProfile(event.Tree, profiles, event.DurationMs)
	}

	result := make([]NodeProfile, 0, len(profiles))
	for _, p := range profiles {
		if p.TickCount > 0 {
			p.AvgDurationMs = p.TotalDurationMs / float64(p.TickCount)
		}
		result = append(result, *p)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result
}

// Search searches the current tree for nodes matching the query (case-insensitive).
func (t *Tracker) Search(query string) []SearchResult {
	tree := t.BuildTree()
	if tree == nil {
		return nil
	}
	lowerQuery := strings.ToLower(query)
	var results []SearchResult
	searchTree(tree, "", lowerQuery, &results)
	return results
}

// Diff compares two tree snapshots and returns the differences.
func (t *Tracker) Diff(fromIter, toIter int) (*DiffResult, error) {
	fromEvent, ok := t.EventAt(fromIter)
	if !ok {
		return nil, fmt.Errorf("iteration %d not found", fromIter)
	}
	toEvent, ok := t.EventAt(toIter)
	if !ok {
		return nil, fmt.Errorf("iteration %d not found", toIter)
	}

	result := &DiffResult{
		FromIteration: fromIter,
		ToIteration:   toIter,
	}

	fromNodes := flattenTree(fromEvent.Tree)
	toNodes := flattenTree(toEvent.Tree)

	// Find added and changed nodes
	for id, toNode := range toNodes {
		if fromNode, exists := fromNodes[id]; !exists {
			result.Added = append(result.Added, DiffNode{
				ID:         toNode.ID,
				Name:       toNode.Name,
				NodeType:   toNode.NodeType,
				ChangeType: "added",
			})
		} else {
			oldStatus := nodeStatusString(fromNode.Status)
			newStatus := nodeStatusString(toNode.Status)
			if oldStatus != newStatus || effectsDiffer(fromNode.Effects, toNode.Effects) {
				result.Changed = append(result.Changed, DiffNode{
					ID:         toNode.ID,
					Name:       toNode.Name,
					NodeType:   toNode.NodeType,
					ChangeType: "changed",
					OldStatus:  oldStatus,
					NewStatus:  newStatus,
				})
			}
		}
	}

	// Find removed nodes
	for id, fromNode := range fromNodes {
		if _, exists := toNodes[id]; !exists {
			result.Removed = append(result.Removed, DiffNode{
				ID:         fromNode.ID,
				Name:       fromNode.Name,
				NodeType:   fromNode.NodeType,
				ChangeType: "removed",
			})
		}
	}

	return result, nil
}

// SetBreakpoint adds or updates a breakpoint.
func (t *Tracker) SetBreakpoint(bp Breakpoint) {
	t.breakpointMu.Lock()
	defer t.breakpointMu.Unlock()
	t.breakpoints[bp.NodePath] = &bp
}

// RemoveBreakpoint removes a breakpoint by node path.
func (t *Tracker) RemoveBreakpoint(nodePath string) {
	t.breakpointMu.Lock()
	defer t.breakpointMu.Unlock()
	delete(t.breakpoints, nodePath)
}

// Breakpoints returns all configured breakpoints.
func (t *Tracker) Breakpoints() []Breakpoint {
	t.breakpointMu.RLock()
	defer t.breakpointMu.RUnlock()
	result := make([]Breakpoint, 0, len(t.breakpoints))
	for _, bp := range t.breakpoints {
		result = append(result, *bp)
	}
	return result
}

// checkBreakpoints walks the event tree and checks if any node matches a breakpoint.
func (t *Tracker) checkBreakpoints(event *TickEvent) *Breakpoint {
	if event.Tree == nil {
		return nil
	}
	t.breakpointMu.RLock()
	defer t.breakpointMu.RUnlock()

	if len(t.breakpoints) == 0 {
		return nil
	}

	var hit *Breakpoint
	walkTreeNode(event.Tree, func(tn *TreeNode) bool {
		bp, exists := t.breakpoints[tn.ID]
		if !exists || !bp.Enabled {
			return true
		}
		if bp.Condition != "" {
			nodeStatusStr := nodeStatusString(tn.Status)
			if nodeStatusStr != bp.Condition {
				return true
			}
		}
		hit = bp
		return false
	})
	return hit
}

// buildTreeFromMetadata recursively builds a TreeNode from a bt.Metadata node.
func buildTreeFromMetadata(m bt.Metadata, path string) *TreeNode {
	if m == nil {
		return nil
	}

	nodeType := pabt.GetNodeType(m)
	name := bt.GetName(m)
	frame := bt.GetFrame(m)

	displayName := name
	if displayName == "" && frame != nil {
		displayName = fmt.Sprintf("%s:%d", frame.File, frame.Line)
	}

	status, _ := pabt.GetNodeStatus(m)

	tn := &TreeNode{
		ID:       path,
		Name:     displayName,
		NodeType: nodeType.String(),
		Status:   status,
	}

	// Set Frame field from bt.GetFrame
	if frame != nil {
		tn.Frame = fmt.Sprintf("%s:%d", frame.File, frame.Line)
	}

	if effects, ok := pabt.GetEffects(m); ok {
		for _, e := range effects {
			tn.Effects = append(tn.Effects, EffectInfo{
				Key:   fmt.Sprintf("%v", e.Key()),
				Value: fmt.Sprintf("%v", e.Value()),
			})
		}
	}

	if cond, ok := pabt.GetCondition(m); ok {
		tn.Condition = fmt.Sprintf("%v", cond.Key())
	}

	if key, ok := pabt.GetPreconditionKey(m); ok && tn.Condition == "" {
		tn.Condition = fmt.Sprintf("%v", key)
	}

	childIndex := 0
	m.Children(func(child bt.Metadata) bool {
		childPath := fmt.Sprintf("%s.%d", path, childIndex)
		childNode := buildTreeFromMetadata(child, childPath)
		if childNode != nil {
			tn.Children = append(tn.Children, *childNode)
		}
		childIndex++
		return true
	})

	// Set StructureHash after building children
	tn.StructureHash = fmt.Sprintf("%s:%d", tn.NodeType, len(tn.Children))

	return tn
}

// countNodes counts all nodes in a tree recursively.
func countNodes(tree *TreeNode) int {
	if tree == nil {
		return 0
	}
	count := 1
	for i := range tree.Children {
		count += countNodes(&tree.Children[i])
	}
	return count
}

// statusString converts a bt.Status to a human-readable string.
func statusString(s bt.Status) string {
	switch s {
	case bt.Success:
		return "Success"
	case bt.Failure:
		return "Failure"
	case bt.Running:
		return "Running"
	default:
		return "Unknown"
	}
}

// nodeStatusString converts a *pabt.NodeStatus to a string representation.
func nodeStatusString(s *pabt.NodeStatus) string {
	if s == nil {
		return ""
	}
	return statusString(s.LastStatus())
}

// walkTreeForProfile aggregates profiling data from a tree into the profiles map.
func walkTreeForProfile(tree *TreeNode, profiles map[string]*NodeProfile, durationMs float64) {
	if tree == nil {
		return
	}

	p, exists := profiles[tree.ID]
	if !exists {
		p = &NodeProfile{
			ID:       tree.ID,
			Name:     tree.Name,
			NodeType: tree.NodeType,
		}
		profiles[tree.ID] = p
	}

	p.TickCount++
	p.TotalDurationMs += durationMs

	statusStr := nodeStatusString(tree.Status)
	switch statusStr {
	case "Success":
		p.SuccessCount++
	case "Failure":
		p.FailureCount++
	case "Running":
		p.RunningCount++
	}
	p.LastStatus = statusStr

	for i := range tree.Children {
		walkTreeForProfile(&tree.Children[i], profiles, durationMs)
	}
}

// searchTree recursively searches tree nodes for matches against the query.
func searchTree(tree *TreeNode, parentPath, lowerQuery string, results *[]SearchResult) {
	if tree == nil {
		return
	}

	path := tree.ID
	if parentPath != "" {
		path = parentPath + "/" + tree.ID
	}

	if strings.Contains(strings.ToLower(tree.Name), lowerQuery) ||
		strings.Contains(strings.ToLower(tree.NodeType), lowerQuery) ||
		strings.Contains(strings.ToLower(tree.Condition), lowerQuery) ||
		strings.Contains(strings.ToLower(tree.PostCondition), lowerQuery) ||
		containsEffect(tree.Effects, lowerQuery) {
		*results = append(*results, SearchResult{
			ID:       tree.ID,
			Name:     tree.Name,
			NodeType: tree.NodeType,
			Path:     path,
		})
	}

	for i := range tree.Children {
		searchTree(&tree.Children[i], path, lowerQuery, results)
	}
}

// containsEffect checks if any effect contains the query string.
func containsEffect(effects []EffectInfo, lowerQuery string) bool {
	for _, e := range effects {
		if strings.Contains(strings.ToLower(e.Key), lowerQuery) ||
			strings.Contains(strings.ToLower(e.Value), lowerQuery) {
			return true
		}
	}
	return false
}

// flattenTree flattens a TreeNode hierarchy into a map keyed by ID.
func flattenTree(tree *TreeNode) map[string]*TreeNode {
	result := make(map[string]*TreeNode)
	if tree == nil {
		return result
	}
	flattenTreeInto(tree, result)
	return result
}

func flattenTreeInto(tree *TreeNode, m map[string]*TreeNode) {
	if tree == nil {
		return
	}
	m[tree.ID] = tree
	for i := range tree.Children {
		flattenTreeInto(&tree.Children[i], m)
	}
}

// effectsDiffer compares two effect slices for differences.
func effectsDiffer(a, b []EffectInfo) bool {
	if len(a) != len(b) {
		return true
	}
	for i := range a {
		if a[i] != b[i] {
			return true
		}
	}
	return false
}

// walkTreeNode walks a TreeNode hierarchy calling fn for each node.
// If fn returns false, traversal stops.
func walkTreeNode(tree *TreeNode, fn func(*TreeNode) bool) {
	if tree == nil {
		return
	}
	if !fn(tree) {
		return
	}
	for i := range tree.Children {
		walkTreeNode(&tree.Children[i], fn)
	}
}
