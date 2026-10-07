//go:build example

package ws

import (
	"bufio"
	"encoding/binary"
	"net"
	"sync"
	"sync/atomic"
)

// Conn is a server-side WebSocket connection. It manages reader/writer
// goroutines and a bounded send queue with backpressure.
type Conn struct {
	conn net.Conn
	br   *bufio.Reader

	// Send queue. Bounded; drops oldest snapshots under pressure.
	sendCh   chan []byte
	closeCh  chan struct{}
	closed   atomic.Bool

	// Fragmentation reassembly.
	fragBuf   []byte
	fragOp    Opcode

	writeMu sync.Mutex
}

// NewConn wraps an upgraded connection. The caller must have already
// performed the handshake via Upgrade.
func NewConn(conn net.Conn, br *bufio.Reader, queueSize int) *Conn {
	c := &Conn{
		conn:    conn,
		br:      br,
		sendCh:  make(chan []byte, queueSize),
		closeCh: make(chan struct{}),
	}
	return c
}

// ReadMessage reads the next complete message, handling fragmentation,
// ping/pong, and close frames. Returns the opcode and payload.
func (c *Conn) ReadMessage() (Opcode, []byte, error) {
	for {
		frame, err := ReadFrame(c.br)
		if err != nil {
			return 0, nil, err
		}

		switch frame.Opcode {
		case OpClose:
			// Echo the close frame back.
			c.writeClose(frame.Payload)
			return OpClose, frame.Payload, nil

		case OpPing:
			// Respond with pong.
			c.WriteFrame(true, OpPong, frame.Payload)
			continue

		case OpPong:
			// Unsolicited pong; ignore.
			continue

		case OpContinuation:
			c.fragBuf = append(c.fragBuf, frame.Payload...)
			if frame.FIN {
				op := c.fragOp
				payload := c.fragBuf
				c.fragBuf = nil
				c.fragOp = 0
				return op, payload, nil
			}
			continue

		default:
			// Text or Binary.
			if frame.FIN {
				return frame.Opcode, frame.Payload, nil
			}
			// Start fragmentation.
			c.fragOp = frame.Opcode
			c.fragBuf = append([]byte(nil), frame.Payload...)
			continue
		}
	}
}

// WriteFrame writes a single frame. Thread-safe.
func (c *Conn) WriteFrame(fin bool, opcode Opcode, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return WriteFrame(c.conn, fin, opcode, payload, nil)
}

// WriteMessage sends a complete message (no fragmentation).
func (c *Conn) WriteMessage(opcode Opcode, payload []byte) error {
	return c.WriteFrame(true, opcode, payload)
}

// Send enqueues a message for async sending. If the queue is full, the
// message is dropped (backpressure). Returns false if dropped.
func (c *Conn) Send(msg []byte) bool {
	select {
	case c.sendCh <- msg:
		return true
	default:
		return false
	}
}

// SendPriority enqueues a message, evicting the oldest message if full.
// Used for events that must never be dropped.
func (c *Conn) SendPriority(msg []byte) {
	for {
		select {
		case c.sendCh <- msg:
			return
		default:
			// Evict oldest.
			select {
			case <-c.sendCh:
			default:
			}
		}
	}
}

// Serve starts the writer goroutine that drains the send queue. Call in a
// goroutine. Returns when the connection is closed.
func (c *Conn) ServeWriter() {
	for {
		select {
		case <-c.closeCh:
			return
		case msg := <-c.sendCh:
			if err := c.WriteMessage(OpText, msg); err != nil {
				c.Close()
				return
			}
		}
	}
}

// Close closes the connection.
func (c *Conn) Close() {
	if c.closed.CompareAndSwap(false, true) {
		close(c.closeCh)
		c.conn.Close()
	}
}

// Closed reports whether the connection has been closed.
func (c *Conn) Closed() bool {
	return c.closed.Load()
}

// writeClose sends a close frame with the given payload (status code + reason).
func (c *Conn) writeClose(payload []byte) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	WriteFrame(c.conn, true, OpClose, payload, nil)
}

// CloseWithStatus sends a close frame with a status code and reason.
func (c *Conn) CloseWithStatus(code uint16, reason string) {
	payload := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(payload[:2], code)
	copy(payload[2:], reason)
	c.writeMu.Lock()
	WriteFrame(c.conn, true, OpClose, payload, nil)
	c.writeMu.Unlock()
	c.Close()
}
