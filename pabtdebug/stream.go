package pabtdebug

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type replayEntry struct {
	seq int64
	msg string
}

// Hub manages connected SSE clients and broadcasts tick events.
type Hub struct {
	mu                sync.Mutex
	clients           map[chan string]struct{}
	seq               int64 // monotonically increasing sequence counter
	replayBuffer      []replayEntry
	maxReplay         int // replay buffer capacity
	keepAliveInterval time.Duration
	// Deprecated: global throttle, kept for compatibility with older tests.
	minInterval   time.Duration
	lastBroadcast time.Time
	lastEvent     []byte
}

// NewHub creates a new SSE hub.
func NewHub() *Hub {
	return &Hub{
		clients:           make(map[chan string]struct{}),
		maxReplay:         50,
		replayBuffer:      make([]replayEntry, 0, 50),
		keepAliveInterval: 15 * time.Second,
	}
}

// Broadcast sends an SSE event to all connected SSE clients.
// Each client has a bounded 256-event buffer. If a client is slow and its
// buffer fills, that client is disconnected rather than silently dropping
// events for others.
func (h *Hub) Broadcast(event SSEEvent) {
	event.Seq = atomic.AddInt64(&h.seq, 1)
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	msg := fmt.Sprintf("id: %d\ndata: %s\n\n", event.Seq, data)

	h.mu.Lock()
	defer h.mu.Unlock()

	h.lastEvent = data
	h.replayBuffer = append(h.replayBuffer, replayEntry{seq: event.Seq, msg: msg})
	if h.maxReplay > 0 && len(h.replayBuffer) > h.maxReplay {
		h.replayBuffer = h.replayBuffer[len(h.replayBuffer)-h.maxReplay:]
	}

	// Per-client delivery with backpressure disconnect.
	var toClose []chan string
	for ch := range h.clients {
		select {
		case ch <- msg:
		default:
			toClose = append(toClose, ch)
		}
	}
	for _, ch := range toClose {
		delete(h.clients, ch)
		close(ch)
	}
}

// ServeHTTP handles SSE connections at the events endpoint.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan string, 256)

	h.mu.Lock()
	lastIDStr := r.Header.Get("Last-Event-ID")
	var toReplay []string
	if lastIDStr != "" {
		if lastID, err := strconv.ParseInt(lastIDStr, 10, 64); err == nil {
			for _, e := range h.replayBuffer {
				if e.seq > lastID {
					toReplay = append(toReplay, e.msg)
				}
			}
		} else {
			// Invalid header: replay all buffered.
			for _, e := range h.replayBuffer {
				toReplay = append(toReplay, e.msg)
			}
		}
	} else {
		// New client: send last event for immediate state.
		if len(h.replayBuffer) > 0 {
			toReplay = append(toReplay, h.replayBuffer[len(h.replayBuffer)-1].msg)
		} else if h.lastEvent != nil {
			toReplay = append(toReplay, fmt.Sprintf("data: %s\n\n", h.lastEvent))
		}
	}
	// Queue replay into channel buffer while holding lock (non-blocking, buffer is 256 > maxReplay 50).
	for _, msg := range toReplay {
		select {
		case ch <- msg:
		default:
			// Should not happen; buffer large enough.
		}
	}
	h.clients[ch] = struct{}{}
	h.mu.Unlock()

	// Ensure cleanup.
	defer func() {
		h.mu.Lock()
		if _, exists := h.clients[ch]; exists {
			delete(h.clients, ch)
			close(ch)
		}
		h.mu.Unlock()
	}()

	keepAlive := h.keepAliveInterval
	var ticker *time.Ticker
	var tickerC <-chan time.Time
	if keepAlive == 0 {
		// Disabled (tests set 0 for determinism); no keep-alive ticker.
	} else {
		if keepAlive < 0 {
			keepAlive = 15 * time.Second
		}
		ticker = time.NewTicker(keepAlive)
		defer ticker.Stop()
		tickerC = ticker.C
	}

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if _, err := fmt.Fprint(w, msg); err != nil {
				return
			}
			flusher.Flush()
		case <-tickerC:
			if _, err := fmt.Fprint(w, ":keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// Close gracefully shuts down the hub by closing all client channels.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		close(ch)
		delete(h.clients, ch)
	}
}
