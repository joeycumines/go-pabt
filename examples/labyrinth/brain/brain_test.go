//go:build example

package brain

import (
	"testing"

	"github.com/joeycumines/go-pabt"
	"github.com/joeycumines/go-pabt/examples/labyrinth/maze"
	"github.com/joeycumines/go-pabt/examples/labyrinth/world"
)

// newTestWorld builds a small maze with a runner and one minotaur.
func newTestWorld(t *testing.T) (*maze.Maze, *world.World, *world.Minotaur) {
	t.Helper()
	m := maze.Generate(42, 11, 11)
	w := world.New(m)
	// Place the minotaur at a known floor cell.
	minotaur := &world.Minotaur{ID: "M-1", Pos: m.Start}
	w.Minotaurs = append(w.Minotaurs, minotaur)
	return m, w, minotaur
}

func TestStationaryRunnerDetectedPursuedCaught(t *testing.T) {
	m, w, minotaur := newTestWorld(t)

	// Place the runner adjacent to the minotaur.
	w.Runner.Pos = maze.Vec2{X: m.Start.X + 1, Y: m.Start.Y}
	// Ensure the runner cell is walkable.
	cx := int(w.Runner.Pos.X)
	cy := int(w.Runner.Pos.Y)
	if m.At(cx, cy).Kind != maze.CellFloor {
		t.Skipf("adjacent cell not walkable in this seed; skipping")
	}

	b, err := NewBrain("M-1", m, &minotaur.Pos)
	if err != nil {
		t.Fatalf("NewBrain: %v", err)
	}

	snap := w.Copy()
	b.UpdateBelief(snap, minotaur)

	// The runner should be detected (within sight range).
	if !b.belief.PlayerDetected {
		t.Fatalf("runner not detected: pos=%v minotaur=%v", w.Runner.Pos, minotaur.Pos)
	}

	// Tick until caught or exhausted.
	var caught bool
	for i := 0; i < 200; i++ {
		snap = w.Copy()
		b.UpdateBelief(snap, minotaur)
		b.Tick("attempt")
		if b.belief.Caught {
			caught = true
			break
		}
	}
	if !caught {
		t.Fatalf("runner not caught after 200 ticks")
	}
}

func TestLossOfSightTriggersSearch(t *testing.T) {
	m, w, minotaur := newTestWorld(t)

	// Place the runner far away (at the gate).
	w.Runner.Pos = m.Gate

	b, err := NewBrain("M-1", m, &minotaur.Pos)
	if err != nil {
		t.Fatalf("NewBrain: %v", err)
	}

	// Use InjectBelief to establish initial detection at a known position.
	// This avoids depending on maze layout for the initial detection.
	initialPos := maze.Vec2{X: minotaur.Pos.X + 2, Y: minotaur.Pos.Y}
	b.InjectBelief(initialPos)

	// Verify initial state.
	if !b.belief.PlayerDetected {
		t.Fatalf("belief injection did not set PlayerDetected")
	}
	if !b.belief.HasLastKnown {
		t.Fatalf("belief injection did not set HasLastKnown")
	}

	// Tick once to process the initial state.
	snap := w.Copy()
	b.UpdateBelief(snap, minotaur)
	b.Tick("attempt")

	// Now: the runner is far away (at the gate), so UpdateBelief should
	// clear detection but preserve last-known.
	snap = w.Copy()
	b.UpdateBelief(snap, minotaur)

	// The minotaur should no longer detect the player (gate is far).
	if b.belief.PlayerDetected {
		t.Fatalf("player still detected after moving to gate")
	}

	// The minotaur should still have a last-known position.
	if !b.belief.HasLastKnown {
		t.Fatalf("last-known position lost after loss of sight")
	}

	// Tick: the brain should search the last-known position.
	for i := 0; i < 200; i++ {
		snap = w.Copy()
		b.UpdateBelief(snap, minotaur)
		_, diag := b.TickDebug("attempt")
		if i < 10 || i%20 == 0 {
			t.Logf("tick %d: %s", i, diag)
		}
		if b.belief.SearchExhausted {
			break
		}
	}
	// After enough ticks, search should be exhausted.
	if !b.belief.SearchExhausted {
		t.Fatalf("search not exhausted after 200 ticks")
	}
}

func TestStunnedMinotaurRecoversAndResumes(t *testing.T) {
	m, w, minotaur := newTestWorld(t)

	// Place the runner nearby so there's something to pursue.
	w.Runner.Pos = maze.Vec2{X: minotaur.Pos.X + 2, Y: minotaur.Pos.Y}

	b, err := NewBrain("M-1", m, &minotaur.Pos)
	if err != nil {
		t.Fatalf("NewBrain: %v", err)
	}

	// Stun the minotaur.
	minotaur.Stunned = true
	snap := w.Copy()
	b.UpdateBelief(snap, minotaur)

	if !b.belief.Stunned {
		t.Fatalf("belief not stunned")
	}

	// Tick: the brain should process the stun (recovery action).
	frame := b.Tick("attempt")
	if frame == nil {
		t.Fatalf("no frame while stunned")
	}

	// After enough ticks, the stun should clear.
	for i := 0; i < 10; i++ {
		snap = w.Copy()
		b.UpdateBelief(snap, minotaur)
		b.Tick("attempt")
		if !b.belief.Stunned {
			break
		}
	}
	if b.belief.Stunned {
		t.Fatalf("minotaur still stunned after recovery ticks")
	}

	// Resume pursuing.
	for i := 0; i < 50; i++ {
		snap = w.Copy()
		b.UpdateBelief(snap, minotaur)
		frame := b.Tick("attempt")
		if frame == nil {
			t.Fatalf("frame nil after recovery")
		}
	}
}

func TestEveryTickProducesEncodedFrame(t *testing.T) {
	m, w, minotaur := newTestWorld(t)

	// Place runner nearby.
	w.Runner.Pos = maze.Vec2{X: minotaur.Pos.X + 2, Y: minotaur.Pos.Y}

	b, err := NewBrain("M-1", m, &minotaur.Pos)
	if err != nil {
		t.Fatalf("NewBrain: %v", err)
	}

	snap := w.Copy()
	b.UpdateBelief(snap, minotaur)
	frame := b.Tick("attempt")

	if frame == nil {
		t.Fatalf("no frame")
	}
	if frame.PlanRevision == 0 {
		t.Fatalf("frame plan revision is zero")
	}
	if frame.Coherence != string(pabt.CoherenceRevisioned) {
		t.Fatalf("frame coherence = %q, want %q", frame.Coherence, pabt.CoherenceRevisioned)
	}
	if len(frame.Nodes) == 0 {
		t.Fatalf("frame has no nodes")
	}

	// Check for execution rows when an action ran.
	// Tick until we get an execution.
	var foundExec bool
	for i := 0; i < 20; i++ {
		snap = w.Copy()
		b.UpdateBelief(snap, minotaur)
		frame = b.Tick("attempt")
		if len(frame.Executions) > 0 {
			foundExec = true
			exec := frame.Executions[0]
			if len(exec.Observations) == 0 {
				t.Errorf("execution has no observations")
			}
			if len(exec.Relations) == 0 {
				t.Errorf("execution has no relations")
			}
			break
		}
	}
	if !foundExec {
		t.Fatalf("no execution row produced in 20 ticks")
	}
}

func TestReplanBumpsRevisionAndRebuildsTree(t *testing.T) {
	m, w, minotaur := newTestWorld(t)
	w.Runner.Pos = maze.Vec2{X: minotaur.Pos.X + 2, Y: minotaur.Pos.Y}

	b, err := NewBrain("M-1", m, &minotaur.Pos)
	if err != nil {
		t.Fatalf("NewBrain: %v", err)
	}

	snap := w.Copy()
	b.UpdateBelief(snap, minotaur)
	frame1 := b.Tick("attempt")
	if frame1 == nil {
		t.Fatalf("no frame before replan")
	}
	if len(frame1.Nodes) == 0 {
		t.Fatalf("no nodes before replan")
	}
	// Record node IDs from the first plan.
	ids1 := make(map[string]bool)
	for _, n := range frame1.Nodes {
		ids1[n.ID] = true
	}

	// Replan.
	if err := b.Replan(); err != nil {
		t.Fatalf("Replan: %v", err)
	}

	snap = w.Copy()
	b.UpdateBelief(snap, minotaur)
	frame2 := b.Tick("attempt")
	if frame2 == nil {
		t.Fatalf("no frame after replan")
	}
	if len(frame2.Nodes) == 0 {
		t.Fatalf("no nodes after replan")
	}

	// The tree must be rebuilt: each new plan gets a fresh identity namespace,
	// so ALL node IDs must differ from the previous plan.
	var shared int
	for _, n := range frame2.Nodes {
		if ids1[n.ID] {
			shared++
		}
	}
	if shared > 0 {
		t.Fatalf("replan did not rebuild tree: %d node IDs shared between old and new plan", shared)
	}

	// The new plan must have a valid revision (non-zero after init+tick).
	if frame2.PlanRevision == 0 {
		t.Fatalf("replan produced a plan with zero revision")
	}
}

func TestBeliefInjectionRedirectsPursuit(t *testing.T) {
	m, w, minotaur := newTestWorld(t)

	// Runner at start.
	w.Runner.Pos = m.Start

	b, err := NewBrain("M-1", m, &minotaur.Pos)
	if err != nil {
		t.Fatalf("NewBrain: %v", err)
	}

	snap := w.Copy()
	b.UpdateBelief(snap, minotaur)
	frame1 := b.Tick("attempt")
	if frame1 == nil {
		t.Fatalf("no frame")
	}

	// Inject a false belief: player at the gate.
	b.InjectBelief(m.Gate)

	// The belief should now be redirected.
	if !b.belief.PlayerDetected {
		t.Fatalf("belief injection did not set PlayerDetected")
	}
	if b.belief.PlayerPos != m.Gate {
		t.Fatalf("belief injection did not set PlayerPos to gate: got %v", b.belief.PlayerPos)
	}

	// Tick: the brain should now pursue the injected position.
	snap = w.Copy()
	b.UpdateBelief(snap, minotaur)
	frame2 := b.Tick("attempt")
	if frame2 == nil {
		t.Fatalf("no frame after injection")
	}

	// The path should be toward the gate (injected position).
	if frame2.Path == "" {
		t.Fatalf("no path after injection")
	}
}

func TestPauseSkipsTick(t *testing.T) {
	m, w, minotaur := newTestWorld(t)
	w.Runner.Pos = maze.Vec2{X: minotaur.Pos.X + 2, Y: minotaur.Pos.Y}

	b, err := NewBrain("M-1", m, &minotaur.Pos)
	if err != nil {
		t.Fatalf("NewBrain: %v", err)
	}

	snap := w.Copy()
	b.UpdateBelief(snap, minotaur)
	frame := b.Tick("attempt")
	if frame == nil {
		t.Fatalf("no frame before pause")
	}

	// Pause.
	b.Pause()
	frame = b.Tick("attempt")
	if frame != nil {
		t.Fatalf("frame returned while paused: %+v", frame)
	}

	// Resume.
	b.Resume()
	frame = b.Tick("attempt")
	if frame == nil {
		t.Fatalf("no frame after resume")
	}
}
