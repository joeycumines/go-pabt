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
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
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

const defaultKeyframeInterval = 20

type eventEntry struct {
	Iteration     int
	Status        bt.Status
	Timestamp     time.Time
	DurationMs    float64
	NodeCount     int
	BreakpointHit *Breakpoint
}

type Tracker struct {
	id               string
	plan             *pabt.IPlan
	entries          []eventEntry
	treeStore        map[int]*TreeNode
	treeOrder        []int
	eventIndex       map[int]int
	maxEvents        int
	maxTrees         int
	mu               sync.Mutex
	iteration        atomic.Int64
	hub              *Hub
	lastTrackTime    time.Time
	breakpoints      map[string]*Breakpoint
	breakpointMu     sync.RWMutex
	profiles         map[string]*NodeProfile
	profileSeq       int
	keyframeInterval int
	lastTree         *TreeNode
	deltaStore       map[int]*TreeDelta
	overflowPath     string
	overflowFile     *os.File
	overflowMu       sync.Mutex
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
		id:               id,
		plan:             plan,
		entries:          make([]eventEntry, 0),
		treeStore:        make(map[int]*TreeNode),
		treeOrder:        make([]int, 0),
		eventIndex:       make(map[int]int),
		maxEvents:        defaultMaxEvents,
		maxTrees:         defaultMaxTrees,
		hub:              NewHub(),
		breakpoints:      make(map[string]*Breakpoint),
		profiles:         make(map[string]*NodeProfile),
		keyframeInterval: defaultKeyframeInterval,
		deltaStore:       make(map[int]*TreeDelta),
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
	// Capture evicted entries for overflow persistence before mutating.
	var evictedEntries []eventEntry
	var evictedTrees map[int]*TreeNode
	if t.maxEvents > 0 && len(t.entries)+1 > t.maxEvents {
		excess := len(t.entries) + 1 - t.maxEvents
		// Make copy of entries that will be evicted (oldest)
		evictedEntries = make([]eventEntry, excess)
		copy(evictedEntries, t.entries[:excess])
	}
	// Also capture trees that will be evicted due to maxTrees
	if tree != nil && t.maxTrees > 0 && len(t.treeStore)+1 > t.maxTrees {
		excessT := len(t.treeStore) + 1 - t.maxTrees
		evictedTrees = make(map[int]*TreeNode, excessT)
		for i := 0; i < excessT && i < len(t.treeOrder); i++ {
			iterEv := t.treeOrder[i]
			if tn, ok := t.treeStore[iterEv]; ok {
				evictedTrees[iterEv] = CloneTree(tn)
			}
		}
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
	// Persist evicted slice to overflow file if configured.
	if (len(evictedEntries) > 0 || len(evictedTrees) > 0) && t.overflowFile != nil {
		t.overflowMu.Lock()
		if t.overflowFile != nil {
			enc := json.NewEncoder(t.overflowFile)
			for _, ev := range evictedEntries {
				// Build TickEvent with its tree if we have it in evictedTrees
				te := TickEvent{
					Iteration:     ev.Iteration,
					Status:        ev.Status,
					Timestamp:     ev.Timestamp,
					DurationMs:    ev.DurationMs,
					NodeCount:     ev.NodeCount,
					BreakpointHit: ev.BreakpointHit,
				}
				if tn, ok := evictedTrees[ev.Iteration]; ok {
					te.Tree = tn
				}
				_ = enc.Encode(te)
			}
			// Trees evicted due to maxTrees but whose entries remain (not in evictedEntries) — write standalone tree snapshot line?
			// For those, write a minimal TickEvent with just Tree and iteration so it can be recovered.
			for iterEv, tn := range evictedTrees {
				// If already handled via evictedEntries, skip.
				skip := false
				for _, ev := range evictedEntries {
					if ev.Iteration == iterEv {
						skip = true
						break
					}
				}
				if skip {
					continue
				}
				te := TickEvent{Iteration: iterEv, Tree: tn}
				// Timestamp zero for tree-only eviction; not critical.
				_ = enc.Encode(te)
			}
		}
		t.overflowMu.Unlock()
	}

	if tree != nil {
		walkTreeForProfile(tree, t.profiles, durationMs)
		t.profileSeq++
	}

	// Delta computation for SSE broadcast (keyframe every N).
	// We keep full tree in treeStore for backward compatibility and test stability,
	// but SSE payload is delta-optimized.
	var sseEvent SSEEvent
	isKeyframe := false
	if tree != nil {
		if t.lastTree == nil || iter == 1 || (t.keyframeInterval > 0 && iter%t.keyframeInterval == 1) {
			isKeyframe = true
		}
		// Also treat structural changes that add/remove many nodes as keyframe if delta would be large?
		// For now strict interval-based.
		if isKeyframe {
			sseEvent = SSEEvent{
				Iteration:     iter,
				Status:        status,
				Tree:          tree,
				IsKeyframe:    true,
				Timestamp:     now,
				DurationMs:    durationMs,
				NodeCount:     nodeCount,
				BreakpointHit: event.BreakpointHit,
			}
		} else {
			delta := ComputeDelta(t.lastTree, tree)
			if delta != nil {
				delta.BaseIteration = iter - 1
				delta.TargetIteration = iter
				// Store delta for potential reconstruction tests (optional).
				if t.deltaStore != nil {
					t.deltaStore[iter] = delta
				}
			}
			sseEvent = SSEEvent{
				Iteration:     iter,
				Status:        status,
				Delta:         delta,
				IsKeyframe:    false,
				Timestamp:     now,
				DurationMs:    durationMs,
				NodeCount:     nodeCount,
				BreakpointHit: event.BreakpointHit,
			}
			if delta == nil {
				sseEvent.Tree = tree
				sseEvent.IsKeyframe = true
				sseEvent.Delta = nil
			}
		}
		// Update lastTree snapshot for next delta.
		t.lastTree = CloneTree(tree)
		// Also update lastIter implicitly via iteration counter.
	} else {
		sseEvent = SSEEvent{
			Iteration:     iter,
			Status:        status,
			Timestamp:     now,
			DurationMs:    durationMs,
			NodeCount:     nodeCount,
			BreakpointHit: event.BreakpointHit,
		}
	}

	t.mu.Unlock()

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
	var clonedStatus *pabt.NodeStatus
	if status != nil {
		clonedStatus = &pabt.NodeStatus{}
		clonedStatus.SetTickCount(status.TickCount())
		clonedStatus.SetLastStatus(status.LastStatus())
	}
	tn := &TreeNode{
		ID:       path,
		Name:     displayName,
		NodeType: nodeType.String(),
		Status:   clonedStatus,
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

// CloneTree performs a deep copy of a TreeNode tree.
func CloneTree(tree *TreeNode) *TreeNode {
	if tree == nil {
		return nil
	}
	cp := *tree
	if tree.Status != nil {
		s := &pabt.NodeStatus{}
		s.SetTickCount(tree.Status.TickCount())
		s.SetLastStatus(tree.Status.LastStatus())
		cp.Status = s
	}
	if len(tree.Children) > 0 {
		cp.Children = make([]TreeNode, len(tree.Children))
		for i := range tree.Children {
			child := CloneTree(&tree.Children[i])
			if child != nil {
				cp.Children[i] = *child
			}
		}
	}
	if len(tree.Effects) > 0 {
		cp.Effects = make([]EffectInfo, len(tree.Effects))
		copy(cp.Effects, tree.Effects)
	}
	return &cp
}

// WithKeyframeInterval sets the keyframe interval for delta encoding.
// Every Nth tick (1-indexed) will be a keyframe (full tree). 0 disables interval-based keyframes.
func (t *Tracker) WithKeyframeInterval(n int) *Tracker {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.keyframeInterval = n
	return t
}

// KeyframeInterval returns the current keyframe interval.
func (t *Tracker) KeyframeInterval() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.keyframeInterval
}

// SetOverflowFile configures a file to which evicted events are appended as JSONL.
// When set, sliding-window eviction appends evicted entries instead of discarding them.
func (t *Tracker) SetOverflowFile(path string) error {
	t.overflowMu.Lock()
	defer t.overflowMu.Unlock()
	if t.overflowFile != nil {
		t.overflowFile.Close()
		t.overflowFile = nil
	}
	t.overflowPath = path
	if path == "" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	t.overflowFile = f
	return nil
}

// CloseOverflow closes the overflow file if any.
func (t *Tracker) CloseOverflow() error {
	t.overflowMu.Lock()
	defer t.overflowMu.Unlock()
	if t.overflowFile != nil {
		err := t.overflowFile.Close()
		t.overflowFile = nil
		return err
	}
	return nil
}

// OverflowPath returns the configured overflow file path.
func (t *Tracker) OverflowPath() string {
	t.overflowMu.Lock()
	defer t.overflowMu.Unlock()
	return t.overflowPath
}

// WriteJSONL writes all current events (with trees) as JSONL to w. One TickEvent per line.
// If an overflow file is configured, its contents (evicted events) are written first, followed by the in-memory window,
// providing a lossless export that includes evicted history.
func (t *Tracker) WriteJSONL(w io.Writer) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.overflowMu.Lock()
	overflowPath := t.overflowPath
	t.overflowMu.Unlock()
	// If overflow file exists, copy its contents first (evicted history).
	if overflowPath != "" {
		if f, err := os.Open(overflowPath); err == nil {
			_, _ = io.Copy(w, f)
			f.Close()
			// Ensure we start a new line if file didn't end with newline (Copy preserves)
		}
	}
	enc := json.NewEncoder(w)
	for _, e := range t.entries {
		ev := TickEvent{
			Iteration:     e.Iteration,
			Status:        e.Status,
			Timestamp:     e.Timestamp,
			DurationMs:    e.DurationMs,
			NodeCount:     e.NodeCount,
			BreakpointHit: e.BreakpointHit,
		}
		if tree, ok := t.treeStore[e.Iteration]; ok {
			ev.Tree = CloneTree(tree)
		}
		if err := enc.Encode(ev); err != nil {
			return err
		}
	}
	return nil
}

// ReadJSONL replaces tracker state from JSONL produced by WriteJSONL. Returns number of events imported.
// Existing state is cleared. Imported events are fully navigable (timeline, diff, search, profile).
func (t *Tracker) ReadJSONL(r io.Reader) (int, error) {
	scanner := bufio.NewScanner(r)
	// Increase buffer for large tree lines (500 nodes)
	const maxLine = 20 << 20 // 20MB per line
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, maxLine)
	var events []TickEvent
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev TickEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			return len(events), fmt.Errorf("jsonl decode iteration %d: %w", len(events)+1, err)
		}
		events = append(events, ev)
	}
	if err := scanner.Err(); err != nil {
		return len(events), err
	}
	if len(events) == 0 {
		return 0, nil
	}
	// Reset state atomically
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries = make([]eventEntry, 0, len(events))
	t.treeStore = make(map[int]*TreeNode, len(events))
	t.treeOrder = make([]int, 0, len(events))
	t.eventIndex = make(map[int]int, len(events))
	t.profiles = make(map[string]*NodeProfile)
	t.profileSeq = 0
	t.deltaStore = make(map[int]*TreeDelta)
	t.lastTree = nil
	// Use max iteration to set atomic counter
	maxIter := 0
	for _, ev := range events {
		if ev.Iteration > maxIter {
			maxIter = ev.Iteration
		}
		e := eventEntry{
			Iteration:     ev.Iteration,
			Status:        ev.Status,
			Timestamp:     ev.Timestamp,
			DurationMs:    ev.DurationMs,
			NodeCount:     ev.NodeCount,
			BreakpointHit: ev.BreakpointHit,
		}
		t.entries = append(t.entries, e)
		t.eventIndex[ev.Iteration] = len(t.entries) - 1
		if ev.Tree != nil {
			cl := CloneTree(ev.Tree)
			t.treeStore[ev.Iteration] = cl
			t.treeOrder = append(t.treeOrder, ev.Iteration)
			walkTreeForProfile(cl, t.profiles, ev.DurationMs)
			t.profileSeq++
			t.lastTree = cl
		}
	}
	// Respect maxTrees eviction after import (keep newest maxTrees)
	if t.maxTrees > 0 && len(t.treeStore) > t.maxTrees {
		sort.Ints(t.treeOrder)
		excess := len(t.treeStore) - t.maxTrees
		for i := 0; i < excess; i++ {
			oldest := t.treeOrder[i]
			delete(t.treeStore, oldest)
		}
		t.treeOrder = t.treeOrder[excess:]
	}
	// Respect maxEvents eviction (keep newest)
	if t.maxEvents > 0 && len(t.entries) > t.maxEvents {
		excess := len(t.entries) - t.maxEvents
		t.entries = t.entries[excess:]
		t.eventIndex = make(map[int]int, len(t.entries))
		for i, e := range t.entries {
			t.eventIndex[e.Iteration] = i
		}
		// Also evict corresponding trees if they were beyond window
		allowed := make(map[int]struct{}, len(t.entries))
		for _, e := range t.entries {
			allowed[e.Iteration] = struct{}{}
		}
		newOrder := t.treeOrder[:0]
		for _, iter := range t.treeOrder {
			if _, ok := allowed[iter]; ok {
				newOrder = append(newOrder, iter)
			} else {
				delete(t.treeStore, iter)
			}
		}
		t.treeOrder = newOrder
	}
	t.iteration.Store(int64(maxIter))
	// Ensure lastTrackTime reflects newest timestamp for duration continuity
	if len(t.entries) > 0 {
		t.lastTrackTime = t.entries[len(t.entries)-1].Timestamp
	}
	// Rebuild lastTree to newest tree for delta continuity
	if len(t.treeOrder) > 0 {
		lastIter := t.treeOrder[len(t.treeOrder)-1]
		t.lastTree = CloneTree(t.treeStore[lastIter])
	}
	return len(events), nil
}

// DeltaForIteration returns the delta stored for the given iteration, if any.
func (t *Tracker) DeltaForIteration(iter int) (*TreeDelta, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	d, ok := t.deltaStore[iter]
	return d, ok
}

// ComputeDelta computes the incremental delta from prev to cur.
// Returns nil if either tree is nil.
func ComputeDelta(prev, cur *TreeNode) *TreeDelta {
	if prev == nil || cur == nil {
		return nil
	}
	prevMap := flattenTree(prev)
	curMap := flattenTree(cur)

	delta := &TreeDelta{}

	// Collect added IDs (in cur not in prev).
	addedIDs := make(map[string]struct{})
	for id := range curMap {
		if _, exists := prevMap[id]; !exists {
			addedIDs[id] = struct{}{}
		}
	}
	// Filter to top-most added (parent not also added).
	for id := range addedIDs {
		parent := parentID(id)
		if parent != "" {
			if _, parentAdded := addedIDs[parent]; parentAdded {
				continue
			}
		}
		if node, ok := curMap[id]; ok {
			clone := CloneTree(node)
			if clone != nil {
				delta.Added = append(delta.Added, *clone)
			}
		}
	}

	// Removed: in prev not in cur, top-most only.
	removedIDs := make(map[string]struct{})
	for id := range prevMap {
		if _, exists := curMap[id]; !exists {
			removedIDs[id] = struct{}{}
		}
	}
	for id := range removedIDs {
		parent := parentID(id)
		if parent != "" {
			if _, parentRemoved := removedIDs[parent]; parentRemoved {
				continue
			}
		}
		delta.Removed = append(delta.Removed, id)
	}

	// Changed: present in both but fields differ.
	for id, curNode := range curMap {
		prevNode, exists := prevMap[id]
		if !exists {
			continue
		}
		if nodesDiffer(prevNode, curNode) {
			dc := DeltaChangedNode{
				ID:            curNode.ID,
				Name:          curNode.Name,
				NodeType:      curNode.NodeType,
				Condition:     curNode.Condition,
				PostCondition: curNode.PostCondition,
				Frame:         curNode.Frame,
				StructureHash: curNode.StructureHash,
			}
			if curNode.Status != nil {
				s := &pabt.NodeStatus{}
				s.SetTickCount(curNode.Status.TickCount())
				s.SetLastStatus(curNode.Status.LastStatus())
				dc.Status = s
			}
			if len(curNode.Effects) > 0 {
				dc.Effects = make([]EffectInfo, len(curNode.Effects))
				copy(dc.Effects, curNode.Effects)
			}
			delta.Changed = append(delta.Changed, dc)
		}
	}

	if len(delta.Added) == 0 && len(delta.Removed) == 0 && len(delta.Changed) == 0 {
		return delta
	}
	return delta
}

func parentID(id string) string {
	idx := -1
	for i := len(id) - 1; i >= 0; i-- {
		if id[i] == '.' {
			idx = i
			break
		}
	}
	if idx == -1 {
		return ""
	}
	return id[:idx]
}

func nodesDiffer(a, b *TreeNode) bool {
	if a.Name != b.Name || a.NodeType != b.NodeType || a.Condition != b.Condition || a.PostCondition != b.PostCondition || a.Frame != b.Frame || a.StructureHash != b.StructureHash {
		return true
	}
	if a.Status == nil && b.Status != nil || a.Status != nil && b.Status == nil {
		return true
	}
	if a.Status != nil && b.Status != nil {
		if a.Status.LastStatus() != b.Status.LastStatus() || a.Status.TickCount() != b.Status.TickCount() {
			return true
		}
	}
	if effectsDiffer(a.Effects, b.Effects) {
		return true
	}
	return false
}

// ApplyDelta reconstructs a tree by applying delta to base.
// Returns a new cloned tree; base is not mutated. If delta is nil, returns CloneTree(base).
func ApplyDelta(base *TreeNode, delta *TreeDelta) *TreeNode {
	if delta == nil {
		return CloneTree(base)
	}
	if base == nil {
		// If base is nil but delta has Added root, use it.
		if len(delta.Added) > 0 {
			// Find root (ID "0")
			for i := range delta.Added {
				if delta.Added[i].ID == "0" {
					return CloneTree(&delta.Added[i])
				}
			}
		}
		return nil
	}
	result := CloneTree(base)
	if result == nil {
		return nil
	}
	// Build ID map for mutable result.
	idMap := flattenTree(result)

	// Apply Removed (deepest first).
	// Sort removed by length descending to remove leaves first.
	// Simple: iterate multiple times already top-most filtered; order doesn't matter much if top-most.
	for _, id := range delta.Removed {
		parent := parentID(id)
		if parent == "" {
			continue
		}
		parentNode, ok := idMap[parent]
		if !ok {
			continue
		}
		suffix := id[len(parent)+1:]
		idx := parseIndex(suffix)
		if idx < 0 || idx >= len(parentNode.Children) {
			continue
		}
		// Verify child ID matches before removal.
		if parentNode.Children[idx].ID != id {
			// Fallback: linear search.
			found := -1
			for i := range parentNode.Children {
				if parentNode.Children[i].ID == id {
					found = i
					break
				}
			}
			if found == -1 {
				continue
			}
			idx = found
		}
		// Remove child.
		parentNode.Children = append(parentNode.Children[:idx], parentNode.Children[idx+1:]...)
		// Rebuild map after structural change for subsequent ops.
		idMap = flattenTree(result)
	}

	// Apply Added.
	for i := range delta.Added {
		added := &delta.Added[i]
		parent := parentID(added.ID)
		if parent == "" {
			// Adding root: replace entire tree.
			result = CloneTree(added)
			idMap = flattenTree(result)
			continue
		}
		parentNode, ok := idMap[parent]
		if !ok {
			continue
		}
		suffix := added.ID[len(parent)+1:]
		idx := parseIndex(suffix)
		if idx < 0 {
			continue
		}
		cloneAdded := CloneTree(added)
		if cloneAdded == nil {
			continue
		}
		if idx >= len(parentNode.Children) {
			// Append if index beyond current length (handles append).
			parentNode.Children = append(parentNode.Children, *cloneAdded)
		} else {
			// Insert at idx.
			parentNode.Children = append(parentNode.Children[:idx+1], parentNode.Children[idx:]...)
			parentNode.Children[idx] = *cloneAdded
		}
		idMap = flattenTree(result)
	}

	// Apply Changed.
	for i := range delta.Changed {
		ch := &delta.Changed[i]
		node, ok := idMap[ch.ID]
		if !ok {
			continue
		}
		node.Name = ch.Name
		node.NodeType = ch.NodeType
		node.Condition = ch.Condition
		node.PostCondition = ch.PostCondition
		node.Frame = ch.Frame
		node.StructureHash = ch.StructureHash
		if ch.Status != nil {
			s := &pabt.NodeStatus{}
			s.SetTickCount(ch.Status.TickCount())
			s.SetLastStatus(ch.Status.LastStatus())
			node.Status = s
		} else {
			node.Status = nil
		}
		if len(ch.Effects) > 0 {
			node.Effects = make([]EffectInfo, len(ch.Effects))
			copy(node.Effects, ch.Effects)
		} else {
			node.Effects = nil
		}
	}

	return result
}

func parseIndex(s string) int {
	n := 0
	if s == "" {
		return -1
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return -1
		}
		n = n*10 + int(s[i]-'0')
	}
	return n
}

// TreesEqual compares two trees for deep equality (ignoring pointer identity).
func TreesEqual(a, b *TreeNode) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.ID != b.ID || a.Name != b.Name || a.NodeType != b.NodeType || a.Condition != b.Condition || a.PostCondition != b.PostCondition || a.Frame != b.Frame || a.StructureHash != b.StructureHash {
		return false
	}
	if a.Status == nil && b.Status != nil || a.Status != nil && b.Status == nil {
		return false
	}
	if a.Status != nil && b.Status != nil {
		if a.Status.LastStatus() != b.Status.LastStatus() || a.Status.TickCount() != b.Status.TickCount() {
			return false
		}
	}
	if !effectsEqual(a.Effects, b.Effects) {
		return false
	}
	if len(a.Children) != len(b.Children) {
		return false
	}
	for i := range a.Children {
		if !TreesEqual(&a.Children[i], &b.Children[i]) {
			return false
		}
	}
	return true
}

func effectsEqual(a, b []EffectInfo) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
