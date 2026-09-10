package pabtdebug

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// Hub manages connected SSE clients and broadcasts tick events.
type Hub struct {
	mu            sync.Mutex
	clients       map[chan string]struct{}
	lastEvent     []byte
	minInterval   time.Duration
	lastBroadcast time.Time
	seq           int64    // monotonically increasing sequence counter
	replayBuffer  [][]byte // last N events for reconnect
	maxReplay     int      // replay buffer capacity
}

// NewHub creates a new SSE hub.
func NewHub() *Hub {
	return &Hub{
		clients:      make(map[chan string]struct{}),
		minInterval:  200 * time.Millisecond,
		maxReplay:    50,
		replayBuffer: make([][]byte, 0, 50),
	}
}

// Broadcast sends an SSE event to all connected SSE clients.
// Events are throttled to avoid overwhelming the browser.
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

	// Append to replay buffer
	h.replayBuffer = append(h.replayBuffer, data)
	if h.maxReplay > 0 && len(h.replayBuffer) > h.maxReplay {
		h.replayBuffer = h.replayBuffer[len(h.replayBuffer)-h.maxReplay:]
	}

	now := timeNow()
	if h.minInterval > 0 && now.Sub(h.lastBroadcast) < h.minInterval {
		return
	}
	h.lastBroadcast = now

	for ch := range h.clients {
		select {
		case ch <- msg:
		default:
		}
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

	ch := make(chan string, 64)

	h.mu.Lock()
	// Replay buffer for reconnecting clients
	lastEventIDStr := r.Header.Get("Last-Event-ID")
	if lastEventIDStr != "" {
		// Client is reconnecting — send all buffered events
		for _, data := range h.replayBuffer {
			msg := fmt.Sprintf("data: %s\n\n", data)
			select {
			case ch <- msg:
			default:
			}
		}
	} else {
		// New client — send last event (current behavior)
		if h.lastEvent != nil {
			initialMsg := fmt.Sprintf("data: %s\n\n", h.lastEvent)
			select {
			case ch <- initialMsg:
			default:
			}
		}
	}
	h.clients[ch] = struct{}{}
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		if _, exists := h.clients[ch]; exists {
			delete(h.clients, ch)
			close(ch)
		}
		h.mu.Unlock()
	}()

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
