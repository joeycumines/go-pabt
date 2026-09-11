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

const defaultMaxEvents = 1000
const defaultMaxTrees = 100

type eventEntry struct {
	Iteration     int
	Status        bt.Status
	Timestamp     time.Time
	DurationMs    float64
	NodeCount     int
	BreakpointHit *Breakpoint
}

type Tracker struct {
	id            string
	plan          *pabt.IPlan
	entries       []eventEntry
	treeStore     map[int]*TreeNode
	treeOrder     []int
	eventIndex    map[int]int
	maxEvents     int
	maxTrees      int
	mu            sync.Mutex
	iteration     atomic.Int64
	hub           *Hub
	lastTrackTime time.Time
	breakpoints   map[string]*Breakpoint
	breakpointMu  sync.RWMutex
	profiles      map[string]*NodeProfile
	profileSeq    int
}

func NewTracker(plan *pabt.IPlan) *Tracker {
	return NewTrackerWithID(plan, "0")
}

// NewTrackerWithID creates a tracker for the given plan with the specified ID.
// The ID is used to distinguish multiple plans in a multi-plan debug server.
// If id is empty, it defaults to "0" for backward compatibility.
func NewTrackerWithID(plan *pabt.IPlan, id string) *Tracker {
	if id == "" {
		id = "0"
	}
	return &Tracker{
		id:          id,
		plan:        plan,
		entries:     make([]eventEntry, 0),
		treeStore:   make(map[int]*TreeNode),
		treeOrder:   make([]int, 0),
		eventIndex:  make(map[int]int),
		maxEvents:   defaultMaxEvents,
		maxTrees:    defaultMaxTrees,
		hub:         NewHub(),
		breakpoints: make(map[string]*Breakpoint),
		profiles:    make(map[string]*NodeProfile),
	}
}

// ID returns the plan identifier for this tracker.
func (t *Tracker) ID() string {
	if t == nil {
		return ""
	}
	return t.id
}

// WithID sets the tracker ID and returns the tracker for chaining.
// This allows NewTracker(plan).WithID("my-plan") construction.
func (t *Tracker) WithID(id string) *Tracker {
	if t != nil {
		if id == "" {
			id = "0"
		}
		t.id = id
	}
	return t
}

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
	entry := eventEntry{
		Iteration:     iter,
		Status:        status,
		Timestamp:     now,
		DurationMs:    durationMs,
		NodeCount:     nodeCount,
		BreakpointHit: event.BreakpointHit,
	}
	t.entries = append(t.entries, entry)
	if t.maxEvents > 0 && len(t.entries) > t.maxEvents {
		t.entries = t.entries[len(t.entries)-t.maxEvents:]
		t.eventIndex = make(map[int]int, len(t.entries))
		for i, e := range t.entries {
			t.eventIndex[e.Iteration] = i
		}
	} else {
		t.eventIndex[iter] = len(t.entries) - 1
	}

	if tree != nil {
		t.treeStore[iter] = tree
		t.treeOrder = append(t.treeOrder, iter)
		if t.maxTrees > 0 && len(t.treeStore) > t.maxTrees {
			oldest := t.treeOrder[0]
			t.treeOrder = t.treeOrder[1:]
			delete(t.treeStore, oldest)
		}
	}

	if tree != nil {
		walkTreeForProfile(tree, t.profiles, durationMs)
		t.profileSeq++
	}

	t.mu.Unlock()

	sseEvent := SSEEvent{
		Iteration:     event.Iteration,
		Status:        event.Status,
		Tree:          event.Tree,
		Timestamp:     event.Timestamp,
		DurationMs:    event.DurationMs,
		NodeCount:     event.NodeCount,
		BreakpointHit: event.BreakpointHit,
	}
	t.hub.Broadcast(sseEvent)
}

func (t *Tracker) Events() []TickEvent {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]TickEvent, len(t.entries))
	for i, e := range t.entries {
		result[i] = TickEvent{
			Iteration:     e.Iteration,
			Status:        e.Status,
			Timestamp:     e.Timestamp,
			DurationMs:    e.DurationMs,
			NodeCount:     e.NodeCount,
			BreakpointHit: e.BreakpointHit,
		}
		if tree, ok := t.treeStore[e.Iteration]; ok {
			result[i].Tree = tree
		}
	}
	return result
}

func (t *Tracker) Hub() *Hub {
	return t.hub
}

func (t *Tracker) BuildTree() *TreeNode {
	if t == nil || t.plan == nil {
		return nil
	}
	node := t.plan.Node()
	return buildTreeFromMetadata(node, "0")
}

func (t *Tracker) Timeline() []TimelineEntry {
	t.mu.Lock()
	defer t.mu.Unlock()
	entries := make([]TimelineEntry, len(t.entries))
	for i, e := range t.entries {
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

func (t *Tracker) EventAt(iteration int) (*TickEvent, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	idx, ok := t.eventIndex[iteration]
	if !ok {
		return nil, false
	}
	entry := t.entries[idx]
	event := &TickEvent{
		Iteration:     entry.Iteration,
		Status:        entry.Status,
		Timestamp:     entry.Timestamp,
		DurationMs:    entry.DurationMs,
		NodeCount:     entry.NodeCount,
		BreakpointHit: entry.BreakpointHit,
	}
	if tree, ok := t.treeStore[iteration]; ok {
		event.Tree = tree
	}
	return event, true
}

func (t *Tracker) Profile() []NodeProfile {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.profiles) == 0 {
		return nil
	}
	result := make([]NodeProfile, 0, len(t.profiles))
	for _, p := range t.profiles {
		if p.TickCount > 0 {
			cp := *p
			cp.AvgDurationMs = cp.TotalDurationMs / float64(cp.TickCount)
			result = append(result, cp)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].ID < result[j].ID
	})
	return result
}

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

func (t *Tracker) Diff(fromIter, toIter int) (*DiffResult, error) {
	fromEvent, ok := t.EventAt(fromIter)
	if !ok {
		return nil, fmt.Errorf("iteration %d not found", fromIter)
	}
	if fromEvent.Tree == nil {
		return nil, fmt.Errorf("tree snapshot no longer available for iteration %d", fromIter)
	}
	toEvent, ok := t.EventAt(toIter)
	if !ok {
		return nil, fmt.Errorf("iteration %d not found", toIter)
	}
	if toEvent.Tree == nil {
		return nil, fmt.Errorf("tree snapshot no longer available for iteration %d", toIter)
	}

	result := &DiffResult{
		FromIteration: fromIter,
		ToIteration:   toIter,
	}

	fromNodes := flattenTree(fromEvent.Tree)
	toNodes := flattenTree(toEvent.Tree)

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

func (t *Tracker) SetBreakpoint(bp Breakpoint) {
	t.breakpointMu.Lock()
	defer t.breakpointMu.Unlock()
	t.breakpoints[bp.NodePath] = &bp
}

func (t *Tracker) RemoveBreakpoint(nodePath string) {
	t.breakpointMu.Lock()
	defer t.breakpointMu.Unlock()
	delete(t.breakpoints, nodePath)
}

func (t *Tracker) Breakpoints() []Breakpoint {
	t.breakpointMu.RLock()
	defer t.breakpointMu.RUnlock()
	result := make([]Breakpoint, 0, len(t.breakpoints))
	for _, bp := range t.breakpoints {
		result = append(result, *bp)
	}
	return result
}

func (t *Tracker) WithMaxEvents(n int) *Tracker {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.maxEvents = n
	return t
}

func (t *Tracker) WithMaxTrees(n int) *Tracker {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.maxTrees = n
	return t
}

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

	tn.StructureHash = fmt.Sprintf("%s:%d", tn.NodeType, len(tn.Children))

	return tn
}

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

func nodeStatusString(s *pabt.NodeStatus) string {
	if s == nil {
		return ""
	}
	return statusString(s.LastStatus())
}

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

func containsEffect(effects []EffectInfo, lowerQuery string) bool {
	for _, e := range effects {
		if strings.Contains(strings.ToLower(e.Key), lowerQuery) ||
			strings.Contains(strings.ToLower(e.Value), lowerQuery) {
			return true
		}
	}
	return false
}

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
