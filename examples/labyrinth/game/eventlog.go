//go:build example

package game

import "sync"

// MaxEventLogEntries bounds the authoritative server event log. The log is a
// rolling window: once full, the oldest records are dropped.
const MaxEventLogEntries = 2048

// EventRecord is one authoritative server event with its sequence number and
// the tick it happened on.
type EventRecord struct {
	Seq  uint64 `json:"seq"`
	Tick int    `json:"tick"`
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

// EventQuery filters the event log by type and tick window. HasFrom/HasTo
// distinguish "no bound" from "bound at tick 0".
type EventQuery struct {
	Type     string
	FromTick int
	ToTick   int
	HasFrom  bool
	HasTo    bool
}

// eventLog is a thread-safe rolling buffer of authoritative server events.
type eventLog struct {
	mu      sync.RWMutex
	nextSeq uint64
	records []EventRecord
}

// append records one event and trims the buffer to its bounded size.
func (el *eventLog) append(tick int, evt Event) {
	el.mu.Lock()
	defer el.mu.Unlock()
	el.nextSeq++
	el.records = append(el.records, EventRecord{
		Seq:  el.nextSeq,
		Tick: tick,
		Type: evt.Type,
		Data: evt.Data,
	})
	if len(el.records) > MaxEventLogEntries {
		el.records = el.records[len(el.records)-MaxEventLogEntries:]
	}
}

func (el *eventLog) reset() {
	el.mu.Lock()
	defer el.mu.Unlock()
	el.records = nil
	el.nextSeq = 0
}

// snapshot returns a defensive copy of the log.
func (el *eventLog) snapshot() []EventRecord {
	el.mu.RLock()
	defer el.mu.RUnlock()
	out := make([]EventRecord, len(el.records))
	copy(out, el.records)
	return out
}

// query filters the log by type and tick window.
func (el *eventLog) query(q EventQuery) []EventRecord {
	el.mu.RLock()
	defer el.mu.RUnlock()
	out := make([]EventRecord, 0, len(el.records))
	for _, r := range el.records {
		if q.Type != "" && r.Type != q.Type {
			continue
		}
		if q.HasFrom && r.Tick < q.FromTick {
			continue
		}
		if q.HasTo && r.Tick > q.ToTick {
			continue
		}
		out = append(out, r)
	}
	return out
}
