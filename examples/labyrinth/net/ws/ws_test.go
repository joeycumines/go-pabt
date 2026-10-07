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

// TestAcceptKey verifies the Sec-WebSocket-Accept derivation: SHA-1 of the
// client key concatenated with the RFC 6455 magic GUID, base64-encoded
// (RFC 6455 section 4.2.2). The expected value is the RFC's own worked example.
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

	conn := NewConn(server, bufio.NewReader(server), 10)

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
//
// net.Pipe is synchronous: every Write blocks until the other end reads. The
// server's pong reply therefore blocks inside ReadMessage until something
// drains the client side, so a dedicated client goroutine reads all inbound
// frames while the test main goroutine drives the server.
func TestPingPong(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	pongCh := make(chan Frame, 1)
	textCh := make(chan Frame, 1)
	go func() {
		buf := bufio.NewReader(client)
		for {
			f, err := ReadFrame(buf)
			if err != nil {
				return
			}
			switch f.Opcode {
			case OpPong:
				pongCh <- f
			case OpText:
				textCh <- f
				return
			}
		}
	}()

	// Send ping, then the text message that ends ReadMessage.
	sendCh := make(chan error, 2)
	go func() {
		if err := WriteFrame(client, true, OpPing, []byte("ping-data"), nil); err != nil {
			sendCh <- err
			return
		}
		sendCh <- WriteFrame(client, true, OpText, []byte("after-ping"), nil)
	}()

	srvConn := NewConn(server, bufio.NewReader(server), 10)
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

	// The pong must have been sent in response to the ping.
	select {
	case pong := <-pongCh:
		if string(pong.Payload) != "ping-data" {
			t.Errorf("pong payload = %q, want %q", pong.Payload, "ping-data")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no pong received")
	}
	for i := 0; i < 2; i++ {
		if err := <-sendCh; err != nil {
			t.Fatalf("client write %d: %v", i, err)
		}
	}
}

// TestCloseEcho tests that a close frame is echoed back. As with ping/pong,
// a client goroutine drains inbound frames so the server's synchronous echo
// write cannot deadlock the pipe.
func TestCloseEcho(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	echoCh := make(chan Frame, 1)
	go func() {
		buf := bufio.NewReader(client)
		f, err := ReadFrame(buf)
		if err == nil {
			echoCh <- f
		}
	}()

	sendCh := make(chan error, 1)
	go func() {
		closePayload := make([]byte, 2)
		binary.BigEndian.PutUint16(closePayload, 1000)
		sendCh <- WriteFrame(client, true, OpClose, closePayload, nil)
	}()

	srvConn := NewConn(server, bufio.NewReader(server), 10)
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

	select {
	case echo := <-echoCh:
		if echo.Opcode != OpClose {
			t.Errorf("expected close echo, got opcode %d", echo.Opcode)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no close echo received")
	}
	if err := <-sendCh; err != nil {
		t.Fatalf("client write: %v", err)
	}
}

// TestBackpressure tests the queue contract: Send drops when full, and a
// full queue is not an error. (Draining conn.sendCh directly would compete
// with ServeWriter, the queue's only legitimate consumer.)
func TestBackpressure(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	queueSize := 3
	conn := NewConn(server, bufio.NewReader(server), queueSize)

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

	// SendPriority must always succeed.
	conn.SendPriority([]byte("priority"))
	conn.Close()
}
