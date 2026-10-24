//go:build example

package game

import (
	"testing"
)

func TestEventLogAppendAndSnapshotIsolation(t *testing.T) {
	var el eventLog
	el.append(5, Event{Type: "stunned", Data: map[string]any{"minotaur": "m1"}})
	el.append(7, Event{Type: "death", Data: nil})

	recs := el.snapshot()
	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(recs))
	}
	if recs[0].Seq != 1 || recs[0].Tick != 5 || recs[0].Type != "stunned" {
		t.Fatalf("unexpected first record: %+v", recs[0])
	}
	if recs[1].Seq != 2 || recs[1].Tick != 7 || recs[1].Type != "death" {
		t.Fatalf("unexpected second record: %+v", recs[1])
	}

	// Mutating the returned slice must not touch the internal log.
	recs[0].Type = "corrupted"
	again := el.snapshot()
	if again[0].Type != "stunned" {
		t.Fatalf("snapshot() must return a defensive copy, got %+v", again[0])
	}
}

func TestEventLogBoundedEviction(t *testing.T) {
	var el eventLog
	for i := 0; i < MaxEventLogEntries+100; i++ {
		el.append(i, Event{Type: "clash"})
	}
	recs := el.snapshot()
	if len(recs) != MaxEventLogEntries {
		t.Fatalf("log must be bounded to %d entries, got %d", MaxEventLogEntries, len(recs))
	}
	// The oldest entries are the ones dropped.
	if recs[0].Seq != 101 {
		t.Fatalf("expected the first retained record to be seq 101, got %d", recs[0].Seq)
	}
	if recs[len(recs)-1].Seq != MaxEventLogEntries+100 {
		t.Fatalf("expected the last retained record to be seq %d, got %d", MaxEventLogEntries+100, recs[len(recs)-1].Seq)
	}
}

func TestEventLogQueryFilters(t *testing.T) {
	var el eventLog
	// Type + tick matrix: deaths at ticks 3 and 11, stuns at 5 and 15.
	el.append(3, Event{Type: "death"})
	el.append(5, Event{Type: "stunned"})
	el.append(11, Event{Type: "death"})
	el.append(15, Event{Type: "stunned"})

	got := el.query(EventQuery{Type: "death"})
	if len(got) != 2 || got[0].Tick != 3 || got[1].Tick != 11 {
		t.Fatalf("type filter failed: %+v", got)
	}

	got = el.query(EventQuery{HasFrom: true, FromTick: 6})
	if len(got) != 2 || got[0].Tick != 11 || got[1].Tick != 15 {
		t.Fatalf("from filter failed: %+v", got)
	}

	got = el.query(EventQuery{HasTo: true, ToTick: 10})
	if len(got) != 2 || got[0].Tick != 3 || got[1].Tick != 5 {
		t.Fatalf("to filter failed: %+v", got)
	}

	got = el.query(EventQuery{HasFrom: true, FromTick: 4, HasTo: true, ToTick: 12})
	if len(got) != 2 || got[0].Tick != 5 || got[1].Tick != 11 {
		t.Fatalf("window filter failed: %+v", got)
	}

	got = el.query(EventQuery{Type: "death", HasFrom: true, FromTick: 4})
	if len(got) != 1 || got[0].Tick != 11 {
		t.Fatalf("combined filter failed: %+v", got)
	}

	// An empty log or non-matching query yields an empty, non-nil slice.
	if got = el.query(EventQuery{Type: "win"}); got == nil || len(got) != 0 {
		t.Fatalf("expected empty non-nil slice, got %+v", got)
	}
}

func TestEventLogReset(t *testing.T) {
	var el eventLog
	el.append(1, Event{Type: "hit"})
	el.append(2, Event{Type: "hit"})
	el.reset()
	if recs := el.snapshot(); len(recs) != 0 {
		t.Fatalf("reset must clear the log, got %d records", len(recs))
	}
	// Sequence numbering restarts so a fresh session starts at seq 1.
	el.append(3, Event{Type: "hit"})
	if recs := el.snapshot(); recs[0].Seq != 1 {
		t.Fatalf("expected seq to restart at 1, got %d", recs[0].Seq)
	}
}

func TestGameEventLogWiring(t *testing.T) {
	g := newTestGame(t)
	before := len(g.EventRecords())
	g.broadcastEvent(Event{Type: "stunned", Data: map[string]any{"k": "v"}})
	g.broadcastEvent(Event{Type: "death"})
	recs := g.EventRecords()
	if len(recs) != before+2 {
		t.Fatalf("expected %d records after broadcasts, got %d", before+2, len(recs))
	}
	filtered := g.QueryEvents(EventQuery{Type: "death"})
	if len(filtered) != 1 || filtered[0].Type != "death" {
		t.Fatalf("QueryEvents type filter failed: %+v", filtered)
	}
	g.ResetEvents()
	if recs := g.EventRecords(); len(recs) != 0 {
		t.Fatalf("ResetEvents must clear the log, got %d", len(recs))
	}
}
