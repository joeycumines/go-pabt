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
	"context"
	"errors"
	"testing"

	bt "github.com/joeycumines/go-behaviortree"
)

// hubFixture commits records through a real session whose announcements go to a hub,
// so every test here drives the production path from a receipt rather than a
// hand-built one.
type hubFixture struct {
	store   *SQLiteStore
	key     RunKey
	hub     *DurableHub
	session *Session
	ticks   int
}

func newHubFixture(t *testing.T, limits DurableHubLimits) *hubFixture {
	t.Helper()
	plan := newSessionFixture(t).plan
	store := openTestStore(t, testStorePath(t))
	identity := bootstrapTestStore(t, store)
	key := RunKey{SessionID: identity.SessionID, PlanID: identity.PlanID, RunID: identity.RunID}
	hub, err := NewDurableHub(key, 1, limits)
	if err != nil {
		t.Fatalf("NewDurableHub: %v", err)
	}
	session, err := OpenSession(context.Background(), store, plan, SessionConfig{
		Identity:    identity,
		Codecs:      NewCodecRegistry(DefaultValueLimits()),
		ValueLimits: DefaultValueLimits(),
		Announce:    hub.Publish,
	})
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	return &hubFixture{store: store, key: key, hub: hub, session: session}
}

// commit ticks until a settled tick is recorded, so every test has real records with
// real trees rather than fabricated ones.
func (f *hubFixture) commit(t *testing.T, n int) {
	t.Helper()
	for f.ticks < n {
		result := f.session.Tick(context.Background())
		if result.Err != nil {
			t.Fatalf("Tick %d: %v", f.ticks+1, result.Err)
		}
		if result.Kind == RecordTick && result.StatusPresent && bt.Status(result.Status) == bt.Success {
			f.ticks++
			continue
		}
		if result.Sequence != 0 {
			f.ticks++
		}
	}
}

func TestAHubIsFedFromAReceiptAndNotARecord(t *testing.T) {
	ctx := context.Background()
	f := newHubFixture(t, DefaultDurableHubLimits())
	// A record with no receipt names no committed position, so a hub that accepted one
	// could tell a client about a sequence the store does not hold.
	ticket, err := f.store.Begin(ctx, BeginRequest{
		RunKey: f.key, ExpectedPlanRevision: testPlanRevision,
	})
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	record := makeRecord(smallTree("n"))
	if err := f.hub.Publish(ctx, CommitReceipt{}, record); !errors.Is(err, ErrNotFound) {
		t.Errorf("a record with no receipt = %v, want a refusal", err)
	}
	// A receipt that does not describe the record in hand is refused as well, which is
	// what a caller that shuffled the two would produce.
	if err := f.hub.Publish(ctx, CommitReceipt{
		Sequence:     ticket.Sequence,
		EventUID:     "someone-elses-event",
		RecordSHA256: record.RecordSHA256,
	}, record); !errors.Is(err, ErrNotFound) {
		t.Errorf("a receipt for another record = %v, want a refusal", err)
	}
	if f.hub.LastSequence() != 0 {
		t.Errorf("a refused publish moved the hub to %d", f.hub.LastSequence())
	}
}

func TestASubscriberResumesFromTheStoreWithNoGapAndNoDuplicate(t *testing.T) {
	ctx := context.Background()
	f := newHubFixture(t, DefaultDurableHubLimits())
	// Three records are committed before anyone is listening, which is the gap a
	// reconnecting client reads from the store rather than from the queue.
	f.commit(t, 3)
	sub, err := f.hub.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	if sub.LastSequence != 3 {
		t.Fatalf("the subscription reports position %d, want 3", sub.LastSequence)
	}
	page, err := f.store.RecordsSince(ctx, f.key, 0)
	if err != nil {
		t.Fatalf("RecordsSince: %v", err)
	}
	if len(page.Records) != 3 || page.Through != 3 {
		t.Fatalf("the store replayed %d records through %d, want 3 through 3",
			len(page.Records), page.Through)
	}
	// Then the live half. A record committed after the subscription is queued, and the
	// client filters it against what the store already gave it, so nothing is missed
	// and nothing arrives twice.
	written := page.Through
	f.commit(t, 6)
	for written < 6 {
		message := <-sub.Client.Messages()
		if message.Sequence <= written {
			t.Fatalf("the queue handed out sequence %d after the client had %d",
				message.Sequence, written)
		}
		var record StoredRecord
		if err := CanonicalDecode(message.Bytes, &record); err != nil {
			t.Fatalf("a queued message is not a record: %v", err)
		}
		if record.EventUID != message.EventUID {
			t.Errorf("the message names event %q and carries %q", message.EventUID, record.EventUID)
		}
		written = message.Sequence
	}
	if f.hub.Overflowed() != 0 {
		t.Errorf("a client that kept up was dropped %d times", f.hub.Overflowed())
	}
}

func TestASlowClientIsDisconnectedRatherThanServedAnUnboundedQueue(t *testing.T) {
	limits := DefaultDurableHubLimits()
	limits.MaxQueuedEvents = 2
	f := newHubFixture(t, limits)
	sub, err := f.hub.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	// Three commits against a queue that holds two, with a client that reads nothing.
	f.commit(t, 3)
	if !sub.Client.Behind() {
		t.Error("a client that read nothing was not told it had fallen behind")
	}
	if f.hub.Overflowed() != 1 {
		t.Errorf("the hub counted %d disconnects, want 1", f.hub.Overflowed())
	}
	// The two that fit are still delivered, in order, and no third is queued: a client
	// that is behind is never silently skipped records.
	var seen []uint64
	for range 2 {
		message := <-sub.Client.Messages()
		sub.Client.Took(message)
		seen = append(seen, message.Sequence)
	}
	if len(seen) != 2 || seen[0] != 1 || seen[1] != 2 {
		t.Errorf("the client received %v, want the first two in order", seen)
	}
	select {
	case extra := <-sub.Client.Messages():
		t.Errorf("a client that fell behind was handed sequence %d anyway", extra.Sequence)
	default:
	}
}

func TestAByteBoundDisconnectsAClientThatAnEventCountAloneWouldNot(t *testing.T) {
	limits := DefaultDurableHubLimits()
	// Room for a thousand events and almost no bytes, so the bound that fires is the
	// one a count alone would have missed.
	limits.MaxQueuedEvents = 1000
	limits.MaxQueuedBytes = 1
	f := newHubFixture(t, limits)
	sub, err := f.hub.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	f.commit(t, 1)
	if !sub.Client.Behind() {
		t.Error("a queue past its byte bound still accepted the record")
	}
}

func TestAHubRefusesAnOutOfOrderOrRepeatedAnnouncement(t *testing.T) {
	f := newHubFixture(t, DefaultDurableHubLimits())
	f.commit(t, 1)
	if f.hub.LastSequence() != 1 {
		t.Fatalf("the hub is at %d after one commit, want 1", f.hub.LastSequence())
	}
	record, err := f.store.RecordAt(context.Background(), f.key, 1)
	if err != nil {
		t.Fatalf("RecordAt: %v", err)
	}
	// The same sequence twice would make a client reading in order see it twice.
	if err := f.hub.Publish(context.Background(), CommitReceipt{
		Sequence: 1, EventUID: record.EventUID, RecordSHA256: record.RecordSHA256,
	}, record); !errors.Is(err, ErrHeadMismatch) {
		t.Errorf("a repeated sequence = %v, want a head mismatch", err)
	}
	if f.hub.LastSequence() != 1 {
		t.Errorf("the hub sits at %d after a refusal, want 1", f.hub.LastSequence())
	}
}

func TestAClosedHubRefusesFurtherWork(t *testing.T) {
	f := newHubFixture(t, DefaultDurableHubLimits())
	sub, err := f.hub.Subscribe()
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	f.hub.Close()
	// A client attached when the hub closed is told by its channel closing, not by a
	// write that fails later.
	if _, open := <-sub.Client.Messages(); open {
		t.Error("a closed hub left a client's channel open")
	}
	if _, err := f.hub.Subscribe(); err == nil {
		t.Error("a closed hub accepted a subscription")
	}
	// A closed hub's refusal reaches the caller as an announcement failure, not as a
	// failed tick: the record is committed either way, and a caller that saw the tick
	// fail would conclude the opposite.
	result := f.session.Tick(context.Background())
	if result.Err != nil {
		t.Errorf("a closed hub turned a committed tick into an error: %v", result.Err)
	}
	if result.AnnounceErr == nil {
		t.Error("a closed hub reported no announcement failure")
	}
	if result.Sequence == 0 {
		t.Error("a closed hub stopped the record from being committed")
	}
	if f.hub.Clients() != 0 {
		t.Errorf("a closed hub still holds %d clients", f.hub.Clients())
	}
}

func TestAStoreReplayIsBoundedAndSaysWhenThereIsMore(t *testing.T) {
	ctx := context.Background()
	f := newHubFixture(t, DefaultDurableHubLimits())
	f.commit(t, 6)
	// A byte bound the records cannot fit inside still returns a page rather than
	// nothing, and says that more remains.
	limits := f.store.limits
	limits.MaxPageRows = 100
	limits.MaxPageBytes = 1
	f.store.limits = limits
	page, err := f.store.RecordsSince(ctx, f.key, 0)
	if err != nil {
		t.Fatalf("RecordsSince: %v", err)
	}
	if len(page.Records) != 1 {
		t.Fatalf("a one-byte bound returned %d records, want the first", len(page.Records))
	}
	if !page.HasMore {
		t.Error("a bounded page did not say that more records remain")
	}
	f.store.limits = DefaultStoreLimits()
	// A resumed walk pages to the end with no gap and no repeat.
	page, err = f.store.RecordsSince(ctx, f.key, 0)
	if err != nil {
		t.Fatalf("RecordsSince: %v", err)
	}
	seen := page.Through
	for page.HasMore {
		next, err := f.store.RecordsSince(ctx, f.key, seen)
		if err != nil {
			t.Fatalf("RecordsSince: %v", err)
		}
		if len(next.Records) == 0 {
			t.Fatal("a page claimed more records and then returned none")
		}
		for _, record := range next.Records {
			if record.Sequence != seen+1 {
				t.Fatalf("the walk jumped from %d to %d", seen, record.Sequence)
			}
			seen = record.Sequence
		}
		page = next
	}
	if seen != 6 {
		t.Errorf("the walk reached %d, want 6", seen)
	}
	// A record whose bytes were altered is refused on the way out, like every other
	// read path, so a replay cannot serve a history that was rewritten underneath it.
	row := f.store.readers.QueryRowContext(ctx,
		"SELECT record_json FROM records WHERE session_id = ? AND run_id = ? AND seq = 1",
		f.key.SessionID, f.key.RunID)
	var original []byte
	if err := row.Scan(&original); err != nil {
		t.Fatalf("scan: %v", err)
	}
	altered := append([]byte(nil), original...)
	altered[len(altered)-2] ^= 0x01
	if _, err := f.store.writer.ExecContext(ctx,
		"UPDATE records SET record_json = ? WHERE session_id = ? AND run_id = ? AND seq = 1",
		altered, f.key.SessionID, f.key.RunID); err != nil {
		t.Fatalf("alter: %v", err)
	}
	if _, err := f.store.RecordsSince(ctx, f.key, 0); !errors.Is(err, ErrStoreCorrupt) {
		t.Errorf("a replay of an altered record = %v, want a corruption report", err)
	}
}
