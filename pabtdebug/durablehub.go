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
	"fmt"
	"sync"
	"time"
)

// DurableHub tells connected clients that a run committed a record.
//
// It holds no history. The store is the history: a client's position is a sequence
// in a run, and everything after it is read from the store. What the hub holds is
// the notification, for the window between a client reading the store and a commit
// landing. That is the whole reason a client subscribes before it reads rather than
// after, and it is why the queue can be small.
//
// The hub is fed from a commit receipt, never from a tick. A receipt is the only
// statement that a record exists, so a client can never be told about a sequence the
// store does not hold, and a tick that failed to commit is not an event.
type DurableHub struct {
	mu       sync.Mutex
	limits   DurableHubLimits
	key      RunKey
	closed   bool
	last     uint64
	clients  map[uint64]*hubClient
	nextID   uint64
	dropped  uint64
	overflow uint64
}

// DurableHubLimits are the bounds one hub holds itself to.
//
// The queue is bounded in bytes rather than only in events, because the events are
// records and a record's size is the application's business: a count bound alone lets
// a run of large records cost far more than a run of small ones for the same
// configuration.
type DurableHubLimits struct {
	// MaxQueuedEvents bounds how many records one client may have waiting.
	MaxQueuedEvents uint32

	// MaxQueuedBytes bounds how many bytes one client may have waiting.
	MaxQueuedBytes uint64

	// KeepAlive is how long a silent connection waits before a comment is written, so
	// an idle client is not mistaken for a dead one by an intermediary.
	KeepAlive time.Duration
}

// DefaultDurableHubLimits returns limits that suit a local debugger.
func DefaultDurableHubLimits() DurableHubLimits {
	return DurableHubLimits{
		MaxQueuedEvents: 256,
		MaxQueuedBytes:  8 << 20,
		KeepAlive:       15 * time.Second,
	}
}

// Validate reports limits that cannot bound anything.
func (l DurableHubLimits) Validate() error {
	if l.MaxQueuedEvents == 0 || l.MaxQueuedBytes == 0 {
		return fmt.Errorf(`pabtdebug: incomplete hub limits`)
	}
	return nil
}

// hubMessage is one committed record, encoded once and shared by every client that
// has not read it yet.
type hubMessage struct {
	Sequence uint64
	EventUID string
	Bytes    []byte
	Size     uint64
}

// hubClient is one connected reader's queue.
type hubClient struct {
	id        uint64
	hub       *DurableHub
	messages  chan *hubMessage
	queued    uint64
	queuedRaw uint64
	// behindCh is signalled the moment the client falls behind. Without it a reader
	// already blocked waiting for its next record would not learn it had missed one
	// until the next keep-alive, which is a connection held open for nothing.
	behindCh chan struct{}
	// behind is set when the client missed the bound. It stops the queue growing and
	// tells the writer to end the connection with a position the client can resume
	// from, rather than serving it a slower and slower stream of records.
	behind bool
}

// NewDurableHub returns a hub for one run. The generation is recorded so a client
// that reconnects with a cursor from another generation is told rather than served a
// stream from a history that is no longer active.
func NewDurableHub(key RunKey, generation uint64, limits DurableHubLimits) (*DurableHub, error) {
	if key.IsZero() {
		return nil, fmt.Errorf(`pabtdebug: a hub needs a run identity`)
	}
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	return &DurableHub{
		limits:  limits,
		key:     key,
		clients: map[uint64]*hubClient{},
	}, nil
}

// RunKey returns the run this hub announces.
func (h *DurableHub) RunKey() RunKey { return h.key }

// Publish announces a committed record.
//
// The record is encoded once and the same bytes are handed to every client, so a run
// of large records costs one copy rather than one per reader. A client whose queue is
// full is marked behind and stops receiving rather than being silently skipped:
// dropping a record without saying so would leave a client believing it had seen a
// history it had not.
func (h *DurableHub) Publish(ctx context.Context, receipt CommitReceipt, record StoredRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if receipt.Sequence == 0 || receipt.RecordSHA256 != record.RecordSHA256 ||
		receipt.EventUID != record.EventUID {
		return fmt.Errorf(`pabtdebug: a hub is fed from a receipt, not a record: %w`, ErrNotFound)
	}
	encoded, err := CanonicalEncode(record)
	if err != nil {
		return err
	}
	message := &hubMessage{
		Sequence: receipt.Sequence,
		EventUID: record.EventUID,
		Bytes:    encoded,
		Size:     uint64(len(encoded)),
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return fmt.Errorf(`pabtdebug: the hub is closed`)
	}
	if message.Sequence <= h.last {
		// A commit at or below what the hub has already announced would make a client
		// reading in order see the same sequence twice, so it is refused rather than
		// fanned out.
		return fmt.Errorf("%w: sequence %d was already announced at %d",
			ErrHeadMismatch, message.Sequence, h.last)
	}
	h.last = message.Sequence
	for _, client := range h.clients {
		h.offer(client, message)
	}
	return nil
}

// offer queues a message for one client, marking it behind when it cannot keep up.
func (h *DurableHub) offer(client *hubClient, message *hubMessage) {
	if client.behind {
		return
	}
	if client.queued >= uint64(h.limits.MaxQueuedEvents) ||
		client.queuedRaw+message.Size > h.limits.MaxQueuedBytes {
		h.markBehind(client)
		return
	}
	select {
	case client.messages <- message:
		client.queued++
		client.queuedRaw += message.Size
	default:
		// The channel is full even though the accounting says otherwise, which means a
		// reader took a message without the accounting seeing it go. The client is
		// treated as behind rather than trusted to keep up.
		h.markBehind(client)
	}
}

// markBehind records that a client cannot keep up and says so at once.
func (h *DurableHub) markBehind(client *hubClient) {
	if client.behind {
		return
	}
	client.behind = true
	h.overflow++
	close(client.behindCh)
}

// HubSubscription is one client's place in the queue.
//
// LastSequence is the position to read the store up to, which is the hub's own
// position at the moment of subscribing. A client reads that range from the store and
// then the messages, skipping anything the store already gave it, so no record is
// missed and none is delivered twice.
type HubSubscription struct {
	Client       *HubClient
	LastSequence uint64
}

// HubClient is a subscription's readable end.
type HubClient struct {
	hub     *DurableHub
	client  *hubClient
	closed  bool
	release func()
}

// Messages returns the channel of committed records.
func (c *HubClient) Messages() <-chan *hubMessage { return c.client.messages }

// Took reports that the client has read a message, which is what makes the bound a
// statement about what is waiting rather than about what was ever sent.
//
// It has to be said. A reader that took a message without saying so would leave the
// hub counting bytes the client no longer holds, and would eventually be disconnected
// for being behind while reading at full speed.
func (c *HubClient) Took(message *hubMessage) {
	c.hub.mu.Lock()
	defer c.hub.mu.Unlock()
	if c.client.queued > 0 {
		c.client.queued--
	}
	if c.client.queuedRaw >= message.Size {
		c.client.queuedRaw -= message.Size
	} else {
		c.client.queuedRaw = 0
	}
}

// Behind reports whether the client missed the queue bound. A client that has is told
// to resume from a cursor, and its connection ends: the alternative is a stream that
// arrives later and later, which is a debugger that lies about being live.
func (c *HubClient) Behind() bool {
	c.hub.mu.Lock()
	defer c.hub.mu.Unlock()
	return c.client.behind
}

// BehindSignal is closed when the client falls behind, so a reader waiting for its
// next record is told at once rather than at the next keep-alive.
func (c *HubClient) BehindSignal() <-chan struct{} { return c.client.behindCh }

// Overflowed reports how many clients the hub has had to disconnect for falling
// behind, which is what tells an operator the bound is too small.
func (h *DurableHub) Overflowed() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.overflow
}

// LastSequence returns the highest sequence the hub has announced.
func (h *DurableHub) LastSequence() uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.last
}

// Subscribe attaches a client and reports the position to read the store up to.
//
// Subscribing before reading the store is what closes the gap: anything committed
// after this call is queued, and the caller filters the queue against the sequences it
// already read.
func (h *DurableHub) Subscribe() (HubSubscription, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return HubSubscription{}, fmt.Errorf(`pabtdebug: the hub is closed`)
	}
	h.nextID++
	client := &hubClient{
		id:       h.nextID,
		hub:      h,
		messages: make(chan *hubMessage, h.limits.MaxQueuedEvents),
		behindCh: make(chan struct{}),
	}
	h.clients[client.id] = client
	return HubSubscription{
		Client: &HubClient{
			hub:     h,
			client:  client,
			release: func() { h.release(client) },
		},
		LastSequence: h.last,
	}, nil
}

// release drops a client from the queue.
func (h *DurableHub) release(client *hubClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, client.id)
}

// Close ends every subscription.
func (h *DurableHub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for id, client := range h.clients {
		close(client.messages)
		delete(h.clients, id)
	}
}

// Clients reports how many clients are attached, which is what tells an operator
// whether a bound is being hit.
func (h *DurableHub) Clients() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}
