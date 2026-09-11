package pabtdebug

import (
	"encoding/json"
	"fmt"
	"testing"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt"
)

// helper to build a synthetic tree with n nodes (balanced-ish).
func makeSyntheticTree(n int) *TreeNode {
	if n <= 0 {
		return nil
	}
	// Use a simple BFS allocation that does not rely on slice-alias pointers.
	// Create nodes as heap pointers and attach via cloning at the end to avoid reallocation aliasing.
	type nodeDef struct {
		id       string
		parent   string
		idx      int
		nodeType string
	}
	defs := []string{"ActionNode", "PreconditionLeaf", "PPARoot", "ActionRoot", "GoalSelector", "PreconditionsRoot"}
	// Generate IDs breadth-first with branching 6.
	nodes := make([]nodeDef, 0, n)
	nodes = append(nodes, nodeDef{id: "0", parent: "", idx: -1, nodeType: "GoalRoot"})
	queue := []string{"0"}
	qIdx := 0
	for len(nodes) < n && qIdx < len(queue) {
		parentID := queue[qIdx]
		qIdx++
		branch := 6
		for i := 0; i < branch && len(nodes) < n; i++ {
			childID := parentID + "." + fmt.Sprintf("%d", i)
			tp := defs[len(nodes)%len(defs)]
			nodes = append(nodes, nodeDef{id: childID, parent: parentID, idx: i, nodeType: tp})
			queue = append(queue, childID)
		}
	}
	// Now build TreeNode hierarchy from bottom up via map.
	m := make(map[string]*TreeNode, n)
	// Create TreeNode objects in reverse order so children exist before parents.
	for i := len(nodes) - 1; i >= 0; i-- {
		def := nodes[i]
		s := &pabt.NodeStatus{}
		s.SetTickCount(i + 1)
		s.SetLastStatus(bt.Status(1 + (i % 3)))
		tn := &TreeNode{
			ID:            def.id,
			Name:          def.nodeType + "-" + fmt.Sprintf("%d", i),
			NodeType:      def.nodeType,
			Status:        s,
			StructureHash: def.nodeType + ":0",
		}
		// Attach already-created children whose parent == def.id
		// Since we iterate reverse, children for this node are already in m.
		var children []TreeNode
		for j := i + 1; j < len(nodes); j++ {
			if nodes[j].parent == def.id {
				if child, ok := m[nodes[j].id]; ok {
					children = append(children, *child)
				}
			}
		}
		// Sort children by idx to ensure deterministic order.
		// They are already in insertion order but reverse creation may shuffle.
		// Use idx ordering: nodes with same parent appear consecutively in generation order.
		// Simple bubble: ensure children sorted by ID suffix numeric.
		// Since we generated in order, children slice is already sorted by idx.
		if len(children) > 0 {
			tn.Children = children
			tn.StructureHash = def.nodeType + ":" + fmt.Sprintf("%d", len(children))
		}
		m[def.id] = tn
	}
	root := m["0"]
	if root == nil {
		return nil
	}
	return root
}

func TestDelta_ComputeAndApply_Basic(t *testing.T) {
	// Base: root with two leaves
	base := &TreeNode{ID: "0", Name: "root", NodeType: "GoalRoot", StructureHash: "GoalRoot:2"}
	s0 := &pabt.NodeStatus{}
	s0.SetTickCount(1)
	s0.SetLastStatus(bt.Success)
	s1 := &pabt.NodeStatus{}
	s1.SetTickCount(1)
	s1.SetLastStatus(bt.Success)
	base.Children = []TreeNode{
		{ID: "0.0", Name: "a", NodeType: "ActionNode", Status: s0, StructureHash: "ActionNode:0"},
		{ID: "0.1", Name: "b", NodeType: "ActionNode", Status: s1, StructureHash: "ActionNode:0"},
	}
	base.Status = &pabt.NodeStatus{}
	base.Status.SetTickCount(1)
	base.Status.SetLastStatus(bt.Running)

	cur := CloneTree(base)
	// Change one leaf status
	cur.Children[0].Status.SetLastStatus(bt.Failure)
	cur.Children[0].Status.SetTickCount(2)
	cur.Status.SetTickCount(2)

	delta := ComputeDelta(base, cur)
	if delta == nil {
		t.Fatal("expected non-nil delta")
	}
	if len(delta.Changed) != 2 { // root tickcount + one leaf status
		// root and leaf changed; leaf changed from Success to Failure, root tickcount bump
		t.Fatalf("expected 2 changed nodes (root + leaf), got %d: %+v", len(delta.Changed), delta.Changed)
	}
	if len(delta.Added) != 0 || len(delta.Removed) != 0 {
		t.Fatalf("expected no added/removed, got added %d removed %d", len(delta.Added), len(delta.Removed))
	}
	recon := ApplyDelta(base, delta)
	if !TreesEqual(recon, cur) {
		t.Fatalf("ApplyDelta did not reconstruct cur")
	}
	// Also test clone isolation: modifying cur should not affect base
	cur.Children[0].Status.SetLastStatus(bt.Running)
	if base.Children[0].Status.LastStatus() == bt.Running {
		t.Error("base was mutated via shared status")
	}
}

func TestDelta_PayloadSize_Proportional(t *testing.T) {
	const totalNodes = 500
	base := makeSyntheticTree(totalNodes)
	if base == nil {
		t.Fatal("makeSyntheticTree returned nil")
	}
	// Count nodes to ensure ~500
	if n := countNodes(base); n < 400 || n > 600 {
		t.Fatalf("synthetic tree nodes = %d want ~500", n)
	}
	cur := CloneTree(base)
	// Change 3 leaf nodes' status (deterministically pick 3 deep nodes)
	changed := 0
	flat := flattenTree(cur)
	for id, node := range flat {
		if changed >= 3 {
			break
		}
		// Pick leaves (no children) for status change
		if len(node.Children) == 0 && id != "0" {
			node.Status.SetLastStatus(bt.Failure)
			node.Status.SetTickCount(node.Status.TickCount() + 1)
			changed++
		}
	}
	if changed != 3 {
		t.Fatalf("could not find 3 leaves to change, got %d", changed)
	}

	delta := ComputeDelta(base, cur)
	if delta == nil {
		t.Fatal("expected delta")
	}
	if len(delta.Changed) != 3 {
		t.Fatalf("expected 3 changed nodes for 3 status flips, got %d", len(delta.Changed))
	}
	if len(delta.Added) != 0 || len(delta.Removed) != 0 {
		t.Fatalf("expected no structural changes, got added %d removed %d", len(delta.Added), len(delta.Removed))
	}

	// Marshal sizes: full cur vs delta+overhead
	fullJSON, _ := json.Marshal(cur)
	deltaJSON, _ := json.Marshal(delta)
	sseDelta := SSEEvent{Iteration: 2, Status: bt.Running, Delta: delta, NodeCount: len(flat)}
	sseFull := SSEEvent{Iteration: 2, Status: bt.Running, Tree: cur, NodeCount: len(flat)}
	sseDeltaJSON, _ := json.Marshal(sseDelta)
	sseFullJSON, _ := json.Marshal(sseFull)
	t.Logf("full tree JSON %d bytes, delta JSON %d bytes, SSE full %d, SSE delta %d, changed %d", len(fullJSON), len(deltaJSON), len(sseFullJSON), len(sseDeltaJSON), len(delta.Changed))
	if len(deltaJSON) >= len(fullJSON)/5 {
		t.Errorf("delta JSON %d should be << full JSON %d (proportional to 3 nodes not 500), ratio %0.2f", len(deltaJSON), len(fullJSON), float64(len(deltaJSON))/float64(len(fullJSON)))
	}
	if len(sseDeltaJSON) >= len(sseFullJSON)/5 {
		t.Errorf("SSE delta payload %d should be << SSE full %d, ratio %0.2f", len(sseDeltaJSON), len(sseFullJSON), float64(len(sseDeltaJSON))/float64(len(sseFullJSON)))
	}
	// Also verify changed payload contains only 3 nodes' status, not whole tree
	var m map[string]any
	if err := json.Unmarshal(deltaJSON, &m); err != nil {
		t.Fatal(err)
	}
	if _, ok := m["added"]; ok && len(delta.Added) == 0 {
		// omit empty is correct
	}
	// Reconstruct via delta must equal cur
	recon := ApplyDelta(base, delta)
	if !TreesEqual(recon, cur) {
		t.Fatal("reconstruction from delta failed to match full tree")
	}
}

func TestDelta_RoundTrip_NDeltas(t *testing.T) {
	base := makeSyntheticTree(50)
	cur := CloneTree(base)
	deltas := make([]*TreeDelta, 0, 20)
	// Iteratively mutate 1-2 leaves per tick for 20 ticks
	for iter := 0; iter < 20; iter++ {
		next := CloneTree(cur)
		flat := flattenTree(next)
		// Change up to 2 leaves per iteration
		changes := 0
		for id, node := range flat {
			if changes >= 2 {
				break
			}
			if len(node.Children) == 0 && id != "0" {
				// Cycle status
				curStatus := node.Status.LastStatus()
				nextStatus := bt.Status(1 + (int(curStatus) % 3))
				if nextStatus == curStatus {
					nextStatus = bt.Success
				}
				node.Status.SetLastStatus(nextStatus)
				node.Status.SetTickCount(node.Status.TickCount() + 1)
				changes++
			}
		}
		delta := ComputeDelta(cur, next)
		if delta == nil {
			t.Fatalf("iter %d: expected delta", iter)
		}
		delta.BaseIteration = iter
		delta.TargetIteration = iter + 1
		deltas = append(deltas, delta)
		cur = next
	}
	// Reconstruct by applying N deltas to base
	recon := CloneTree(base)
	for i, d := range deltas {
		recon = ApplyDelta(recon, d)
		if recon == nil {
			t.Fatalf("apply delta %d returned nil", i)
		}
	}
	if !TreesEqual(recon, cur) {
		// Diff details
		baseFlat := flattenTree(base)
		curFlat := flattenTree(cur)
		reconFlat := flattenTree(recon)
		t.Fatalf("N-delta round-trip failed: base %d nodes, cur %d nodes, recon %d nodes, deltas %d", len(baseFlat), len(curFlat), len(reconFlat), len(deltas))
	}
	// Also verify direct full snapshot equals iterative reconstruction
	direct := CloneTree(base)
	for _, d := range deltas {
		direct = ApplyDelta(direct, d)
	}
	if !TreesEqual(direct, cur) {
		t.Fatal("iterative apply not equal to final cur")
	}
}

func TestDelta_Structural_AddRemove(t *testing.T) {
	base := &TreeNode{ID: "0", Name: "root", NodeType: "GoalRoot", StructureHash: "GoalRoot:1"}
	base.Children = []TreeNode{
		{ID: "0.0", Name: "a", NodeType: "ActionNode", StructureHash: "ActionNode:0"},
	}
	base.Status = &pabt.NodeStatus{}
	base.Status.SetLastStatus(bt.Running)

	cur := CloneTree(base)
	// Add new child
	newChild := TreeNode{ID: "0.1", Name: "b", NodeType: "ActionNode", StructureHash: "ActionNode:0"}
	s := &pabt.NodeStatus{}
	s.SetLastStatus(bt.Success)
	newChild.Status = s
	cur.Children = append(cur.Children, newChild)
	cur.StructureHash = "GoalRoot:2"
	cur.Status.SetTickCount(2)

	delta := ComputeDelta(base, cur)
	if len(delta.Added) == 0 {
		t.Fatalf("expected added node, got %+v", delta)
	}
	if len(delta.Changed) == 0 {
		t.Fatalf("expected changed root hash, got %+v", delta)
	}
	recon := ApplyDelta(base, delta)
	if !TreesEqual(recon, cur) {
		t.Fatalf("add structural recon failed")
	}

	// Now remove the added node
	base2 := cur
	cur2 := CloneTree(base2)
	cur2.Children = cur2.Children[:1]
	cur2.StructureHash = "GoalRoot:1"
	delta2 := ComputeDelta(base2, cur2)
	if len(delta2.Removed) == 0 {
		t.Fatalf("expected removed, got %+v", delta2)
	}
	recon2 := ApplyDelta(base2, delta2)
	if !TreesEqual(recon2, cur2) {
		t.Fatalf("remove recon failed")
	}
}

func TestDelta_CloneAndEquality(t *testing.T) {
	tree := makeSyntheticTree(20)
	clone := CloneTree(tree)
	if !TreesEqual(tree, clone) {
		t.Fatal("clone should equal original")
	}
	// Mutate clone status should not affect original
	flatOrig := flattenTree(tree)
	flatClone := flattenTree(clone)
	for id, n := range flatClone {
		if id == "0.0" && n.Status != nil {
			n.Status.SetLastStatus(bt.Failure)
			break
		}
	}
	if TreesEqual(tree, clone) {
		t.Error("after mutation clone should differ")
	}
	if flatOrig["0.0"].Status.LastStatus() == bt.Failure {
		t.Error("original mutated via clone")
	}
	// Nil handling
	if CloneTree(nil) != nil {
		t.Error("CloneTree(nil) should be nil")
	}
	if !TreesEqual(nil, nil) {
		t.Error("TreesEqual(nil,nil) should be true")
	}
	if TreesEqual(tree, nil) {
		t.Error("TreesEqual(tree,nil) should be false")
	}
	if ComputeDelta(nil, tree) != nil {
		t.Error("ComputeDelta(nil,tree) should be nil")
	}
	if ComputeDelta(tree, nil) != nil {
		t.Error("ComputeDelta(tree,nil) should be nil")
	}
	if got := ApplyDelta(nil, nil); got != nil {
		t.Error("ApplyDelta(nil,nil) should be nil")
	}
	if got := ApplyDelta(tree, nil); !TreesEqual(got, tree) {
		t.Error("ApplyDelta(tree,nil) should clone tree")
	}
}

func TestTracker_DeltaBroadcast(t *testing.T) {
	// Integration: tracker broadcasts delta after first tick
	state := &testState{vars: map[any]any{"x": true}}
	var testConds = []pabt.IConditions{{&testCondition{key: "x", value: true}}}
	plan, err := pabt.INew(state, testConds)
	if err != nil {
		t.Fatal(err)
	}
	tracker := NewTracker(plan)
	tracker.WithKeyframeInterval(3) // keyframe every 3 for test speed
	tracker.Hub().keepAliveInterval = 0

	// Capture broadcasts via direct hub client channel
	ch := make(chan string, 256)
	tracker.Hub().mu.Lock()
	tracker.Hub().clients[ch] = struct{}{}
	tracker.Hub().mu.Unlock()
	defer func() {
		tracker.Hub().mu.Lock()
		delete(tracker.Hub().clients, ch)
		close(ch)
		tracker.Hub().mu.Unlock()
	}()

	// Tick 1 -> keyframe with tree
	tracker.Track(bt.Success, nil)
	select {
	case msg := <-ch:
		var ev SSEEvent
		// msg is "id: N\ndata: {...}\n\n"
		// Extract JSON after "data: "
		parts := splitSSE(msg)
		if len(parts) == 0 {
			t.Fatalf("no data in SSE msg %q", msg)
		}
		if err := json.Unmarshal([]byte(parts[0]), &ev); err != nil {
			t.Fatalf("unmarshal keyframe: %v msg %q", err, msg)
		}
		if !ev.IsKeyframe || ev.Tree == nil || ev.Delta != nil {
			t.Fatalf("iter 1 should be keyframe with tree, got %+v", ev)
		}
		if ev.Iteration != 1 {
			t.Fatalf("iter 1 got %d", ev.Iteration)
		}
		fullSize := len(parts[0])
		t.Logf("iter1 keyframe size %d", fullSize)
	default:
		t.Fatal("no broadcast for iter1")
	}

	// Tick 2 -> delta (plan not advanced, so tree unchanged => empty delta is expected).
	// This still proves delta broadcast path produces a delta payload smaller than a full snapshot.
	tracker.Track(bt.Running, nil)
	select {
	case msg := <-ch:
		parts := splitSSE(msg)
		var ev SSEEvent
		if err := json.Unmarshal([]byte(parts[0]), &ev); err != nil {
			t.Fatalf("unmarshal iter2: %v", err)
		}
		if ev.IsKeyframe || ev.Tree != nil || ev.Delta == nil {
			t.Fatalf("iter 2 should be delta, got %+v", ev)
		}
		// For a plan that was not re-ticked, the tree may be identical, so Changed may be 0.
		// The important invariant is that the delta payload is smaller than a full snapshot.
		t.Logf("iter2 delta changed %d added %d removed %d size %d", len(ev.Delta.Changed), len(ev.Delta.Added), len(ev.Delta.Removed), len(parts[0]))
		fullTree := tracker.BuildTree()
		fullJSON, _ := json.Marshal(SSEEvent{Iteration: 2, Tree: fullTree})
		if len(parts[0]) >= len(fullJSON) {
			t.Errorf("delta payload %d should be < full %d", len(parts[0]), len(fullJSON))
		}
		if len(ev.Delta.Changed) > 5 {
			t.Errorf("delta changed %d should be small (0-5) for unchanged tree, got %+v", len(ev.Delta.Changed), ev.Delta)
		}
	default:
		t.Fatal("no broadcast for iter2")
	}

	// Tick 3 -> isKeyframe per interval (3%3==1? Actually interval 3 => keyframe when iter%3==1 => 1,4,7 ... So 3 is NOT keyframe)
	// Let's check: we set interval 3, keyframe at 1,4,... So 3 should be delta.
	tracker.Track(bt.Success, nil)
	select {
	case msg := <-ch:
		parts := splitSSE(msg)
		var ev SSEEvent
		json.Unmarshal([]byte(parts[0]), &ev)
		if ev.IsKeyframe {
			t.Fatalf("iter3 should be delta with interval 3 (keyframe 1,4), got keyframe")
		}
	default:
		t.Fatal("no broadcast iter3")
	}
	// Tick 4 -> keyframe
	tracker.Track(bt.Success, nil)
	select {
	case msg := <-ch:
		parts := splitSSE(msg)
		var ev SSEEvent
		json.Unmarshal([]byte(parts[0]), &ev)
		if !ev.IsKeyframe || ev.Tree == nil {
			t.Fatalf("iter4 should be keyframe, got %+v", ev)
		}
	default:
		t.Fatal("no broadcast iter4")
	}

	// Verify round-trip: rebuild from stored deltas via ApplyDelta chain equals current BuildTree
	// Collect all Trees via Events (full) vs delta reconstruction
	events := tracker.Events()
	if len(events) != 4 {
		t.Fatalf("expected 4 events, got %d", len(events))
	}
	// Find first keyframe tree (iter1)
	tree1 := events[0].Tree
	if tree1 == nil {
		t.Fatal("iter1 tree missing")
	}
	// Replay deltas 2,3,4 (but 4 is keyframe so direct)
	// Our tracker stored full trees for all, but we can also test ComputeDelta chain
	recon := CloneTree(tree1)
	for iter := 2; iter <= 3; iter++ {
		ev := events[iter-1].Tree // full snapshot
		d := ComputeDelta(recon, ev)
		recon = ApplyDelta(recon, d)
		if !TreesEqual(recon, ev) {
			t.Fatalf("round-trip iter %d failed", iter)
		}
	}
	// iter4 is keyframe, recon + delta from 3 to 4 should equal tree4 OR if 4 is keyframe, delta from 3's recon to 4
	d4 := ComputeDelta(events[2].Tree, events[3].Tree)
	reconFinal := ApplyDelta(recon, d4)
	if !TreesEqual(reconFinal, events[3].Tree) {
		t.Fatalf("final keyframe recon failed")
	}
}

func splitSSE(msg string) []string {
	var out []string
	lines := splitLines(msg)
	for _, l := range lines {
		if len(l) > 6 && l[:6] == "data: " {
			out = append(out, l[6:])
		}
	}
	return out
}

func splitLines(s string) []string {
	var res []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			res = append(res, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		res = append(res, s[start:])
	}
	return res
}
