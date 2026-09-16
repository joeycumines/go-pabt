package pabtdebug

import (
	"strings"
	"testing"

	bt "github.com/joeycumines/go-behaviortree"
	"github.com/joeycumines/go-pabt"
)

func TestTreeStoreNodeBudget(t *testing.T) {
	state := &testState{vars: map[any]any{"x": true}}
	plan, err := pabt.INew(state, []pabt.IConditions{{&testCondition{key: "x", value: true}}})
	if err != nil {
		t.Fatal(err)
	}
	tr := NewTrackerWithID(plan, "tree-budget")
	const budget = 5
	tr.WithMaxTreeNodes(budget)
	tr.WithMaxTrees(100)
	for i := 0; i < 50; i++ {
		tr.Track(bt.Running, nil)
	}
	tr.mu.Lock()
	stored := len(tr.treeStore)
	got := tr.treeNodes
	sum := 0
	for _, tn := range tr.treeStore {
		sum += countNodes(tn)
	}
	tr.mu.Unlock()
	if sum != got {
		t.Fatalf("treeNodes counter = %d but sum over store = %d", got, sum)
	}
	// A single newest tree may exceed the budget (it is never evicted away);
	// with more than one tree retained the total must respect the budget.
	if stored > 1 && got > budget {
		t.Fatalf("node budget violated: %d nodes across %d trees (budget %d)", got, stored, budget)
	}
	if stored == 0 {
		t.Fatal("treeStore empty after tracking")
	}
}

func TestHubReplayByteBudget(t *testing.T) {
	h := NewHub()
	const budget = 2048
	h.maxReplayBytes = budget
	h.maxReplay = 1000
	big := &TreeNode{ID: "0", Name: strings.Repeat("x", 400), NodeType: "ActionNode", StructureHash: "ActionNode:0"}
	const broadcasts = 50
	for i := 0; i < broadcasts; i++ {
		h.Broadcast(SSEEvent{Iteration: i + 1, Status: bt.Running, Tree: big, IsKeyframe: true})
	}
	h.mu.Lock()
	n := len(h.replayBuffer)
	got := h.replayBytes
	sum := 0
	for _, e := range h.replayBuffer {
		sum += len(e.msg)
	}
	h.mu.Unlock()
	if sum != got {
		t.Fatalf("replayBytes = %d but sum of retained msgs = %d", got, sum)
	}
	// One oversized message is always retained (never evicted to empty); with
	// more than one entry retained the total must respect the byte budget.
	if n > 1 && got > budget {
		t.Fatalf("byte budget violated: %d bytes across %d entries (budget %d)", got, n, budget)
	}
	if n == broadcasts {
		t.Fatalf("byte budget never bound: all %d entries retained", broadcasts)
	}
	if n == 0 {
		t.Fatal("replay buffer empty after broadcast")
	}
}
