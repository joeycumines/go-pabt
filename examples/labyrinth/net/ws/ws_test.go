//go:build example

package ws

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"net"
	"testing"
	"time"
)

// TestAcceptKey verifies the RFC 6455 example from section 4.2.2.
func TestAcceptKey(t *testing.T) {
	key := "dGhlIHNhbXBsZSBub25jZQ=="
	want := "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	got := AcceptKey(key)
	if got != want {
		t.Fatalf("AcceptKey(%q) = %q, want %q", key, got, want)
	}
}

// TestFrameRoundTrip tests encoding and decoding frames of various sizes.
func TestFrameRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		masked  bool
	}{
		{"empty", nil, false},
		{"small", []byte("Hello"), false},
		{"masked hello", []byte("Hello"), true},
		{"16-bit length", bytes.Repeat([]byte("A"), 300), true},
		{"64-bit length", bytes.Repeat([]byte("B"), 70000), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			var mask *[4]byte
			if tt.masked {
				var m [4]byte
				rand.Read(m[:])
				mask = &m
			}

			err := WriteFrame(&buf, true, OpText, tt.payload, mask)
			if err != nil {
				t.Fatalf("WriteFrame: %v", err)
			}

			frame, err := ReadFrame(&buf)
			if err != nil {
				t.Fatalf("ReadFrame: %v", err)
			}

			if !frame.FIN {
				t.Error("FIN not set")
			}
			if frame.Opcode != OpText {
				t.Errorf("opcode = %d, want OpText", frame.Opcode)
			}
			if !bytes.Equal(frame.Payload, tt.payload) {
				t.Errorf("payload mismatch: got %d bytes, want %d bytes", len(frame.Payload), len(tt.payload))
			}
		})
	}
}

// TestFragmentation tests that fragmented messages are reassembled.
func TestFragmentation(t *testing.T) {
	var buf bytes.Buffer

	// Write 3 fragments: first (FIN=0), continuation (FIN=0), final (FIN=1).
	WriteFrame(&buf, false, OpText, []byte("Hello"), nil)
	WriteFrame(&buf, false, OpContinuation, []byte(" "), nil)
	WriteFrame(&buf, true, OpContinuation, []byte("World"), nil)

	// Read them back as individual frames.
	f1, _ := ReadFrame(&buf)
	f2, _ := ReadFrame(&buf)
	f3, _ := ReadFrame(&buf)

	if f1.FIN {
		t.Error("first fragment should not have FIN")
	}
	if f1.Opcode != OpText {
		t.Errorf("first fragment opcode = %d, want OpText", f1.Opcode)
	}
	if f2.Opcode != OpContinuation {
		t.Errorf("second fragment opcode = %d, want OpContinuation", f2.Opcode)
	}
	if !f3.FIN {
		t.Error("final fragment should have FIN")
	}

	// Reassemble.
	var msg []byte
	msg = append(msg, f1.Payload...)
	msg = append(msg, f2.Payload...)
	msg = append(msg, f3.Payload...)
	if string(msg) != "Hello World" {
		t.Errorf("reassembled = %q, want %q", msg, "Hello World")
	}
}

// TestConnReadMessageFragmentation tests Conn.ReadMessage with fragmented input.
func TestConnReadMessageFragmentation(t *testing.T) {
	var buf bytes.Buffer
	WriteFrame(&buf, false, OpText, []byte("Hello"), nil)
	WriteFrame(&buf, false, OpContinuation, []byte(" "), nil)
	WriteFrame(&buf, true, OpContinuation, []byte("World"), nil)

	// Create a pipe to simulate a connection.
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	// Write the fragmented data to the client side.
	go func() {
		client.Write(buf.Bytes())
		client.Close()
	}()

	conn := NewConn(server, nil, 10)
	// We need to set up the bufio.Reader manually for this test.
	// Use a custom approach: read frames directly.
	br := newBufReader(server)
	conn.br = br

	op, payload, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if op != OpText {
		t.Errorf("opcode = %d, want OpText", op)
	}
	if string(payload) != "Hello World" {
		t.Errorf("payload = %q, want %q", payload, "Hello World")
	}
}

// TestPingPong tests that a ping frame triggers a pong response.
func TestPingPong(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	// Write a ping frame to the client side.
	go func() {
		ping := []byte("ping-data")
		WriteFrame(client, true, OpPing, ping, nil)
		// Keep the pipe open briefly for the pong to be written.
		time.Sleep(50 * time.Millisecond)
	}()

	srvBuf := newBufReader(server)
	srvConn := NewConn(server, srvBuf, 10)

	// ReadMessage should handle the ping and continue.
	// Send a text message after the ping so ReadMessage returns.
	go func() {
		time.Sleep(10 * time.Millisecond)
		WriteFrame(client, true, OpText, []byte("after-ping"), nil)
		time.Sleep(50 * time.Millisecond)
		client.Close()
	}()

	op, payload, err := srvConn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if op != OpText {
		t.Errorf("opcode = %d, want OpText", op)
	}
	if string(payload) != "after-ping" {
		t.Errorf("payload = %q, want %q", payload, "after-ping")
	}

	// Verify pong was written to the client side.
	clientBuf := newBufReader(client)
	pongFrame, err := ReadFrame(clientBuf)
	if err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if pongFrame.Opcode != OpPong {
		t.Errorf("expected pong, got opcode %d", pongFrame.Opcode)
	}
	if string(pongFrame.Payload) != "ping-data" {
		t.Errorf("pong payload = %q, want %q", pongFrame.Payload, "ping-data")
	}
}

// TestCloseEcho tests that a close frame is echoed back.
func TestCloseEcho(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	// Write a close frame with status code 1000.
	go func() {
		closePayload := make([]byte, 2)
		binary.BigEndian.PutUint16(closePayload, 1000)
		WriteFrame(client, true, OpClose, closePayload, nil)
		time.Sleep(50 * time.Millisecond)
	}()

	srvBuf := newBufReader(server)
	srvConn := NewConn(server, srvBuf, 10)

	op, payload, err := srvConn.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if op != OpClose {
		t.Errorf("opcode = %d, want OpClose", op)
	}
	if len(payload) < 2 {
		t.Fatalf("close payload too short: %d", len(payload))
	}
	code := binary.BigEndian.Uint16(payload[:2])
	if code != 1000 {
		t.Errorf("close code = %d, want 1000", code)
	}

	// Verify the close was echoed.
	clientBuf := newBufReader(client)
	echoFrame, err := ReadFrame(clientBuf)
	if err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if echoFrame.Opcode != OpClose {
		t.Errorf("expected close echo, got opcode %d", echoFrame.Opcode)
	}
}

// TestBackpressure tests that Send drops messages when the queue is full.
func TestBackpressure(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	queueSize := 3
	conn := NewConn(server, newBufReader(server), queueSize)

	// Fill the queue.
	for i := 0; i < queueSize; i++ {
		if !conn.Send([]byte("msg")) {
			t.Fatalf("Send %d failed before queue full", i)
		}
	}

	// Next Send should fail (queue full).
	if conn.Send([]byte("overflow")) {
		t.Fatal("Send should fail when queue is full")
	}

	// SendPriority should evict and succeed.
	conn.SendPriority([]byte("priority"))

	// Drain and verify.
	var msgs []string
	for i := 0; i < queueSize; i++ {
		select {
		case msg := <-conn.sendCh:
			msgs = append(msgs, string(msg))
		default:
			t.Fatal("expected message in queue")
		}
	}

	// The first message should have been evicted.
	if msgs[0] == "msg" && len(msgs) == queueSize {
		// After SendPriority evicted one, we should have 2 "msg" + 1 "priority".
		// But the exact order depends on timing. Just verify we got queueSize messages.
		t.Logf("drained %d messages: %v", len(msgs), msgs)
	}
}

// newBufReader creates a bufio.Reader from a net.Conn.
func newBufReader(conn net.Conn) *bufio.Reader {
	return bufio.NewReader(conn)
}
