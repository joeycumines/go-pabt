package pabtdebug

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// Hub manages connected SSE clients and broadcasts tick events.
type Hub struct {
	mu        sync.Mutex
	clients   map[chan string]struct{}
	lastEvent []byte
}

// NewHub creates a new SSE hub.
func NewHub() *Hub {
	return &Hub{
		clients: make(map[chan string]struct{}),
	}
}

// Broadcast sends a tick event to all connected SSE clients.
func (h *Hub) Broadcast(event TickEvent) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	msg := fmt.Sprintf("data: %s\n\n", data)

	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastEvent = data
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
	// Send the last event to the new client to avoid missing events
	// between initial tree dump and SSE subscription.
	if h.lastEvent != nil {
		initialMsg := fmt.Sprintf("data: %s\n\n", h.lastEvent)
		select {
		case ch <- initialMsg:
		default:
		}
	}
	h.clients[ch] = struct{}{}
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.clients, ch)
		h.mu.Unlock()
		close(ch)
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
