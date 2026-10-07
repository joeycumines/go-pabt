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
// TestPingPong uses a real TCP loopback connection. net.Pipe is synchronous
// and unbuffered; bidirectional control-frame exchanges (server writes pong
// while also needing to read the next frame) deadlock on it because every
// Write blocks until the remote Read consumes the bytes. TCP has kernel
// buffering, so writes and reads proceed independently.
func TestPingPong(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	type result struct {
		op      Opcode
		payload []byte
		err     error
	}
	serverDone := make(chan result, 1)

	// Server goroutine: accept, wrap in Conn, read messages.
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverDone <- result{err: err}
			return
		}
		defer conn.Close()
		srvConn := NewConn(conn, bufio.NewReader(conn), 10)
		op, payload, err := srvConn.ReadMessage()
		serverDone <- result{op: op, payload: payload, err: err}
	}()

	// Client: dial and send ping then text.
	client, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(5 * time.Second))

	if err := WriteFrame(client, true, OpPing, []byte("ping-data"), nil); err != nil {
		t.Fatalf("write ping: %v", err)
	}
	if err := WriteFrame(client, true, OpText, []byte("after-ping"), nil); err != nil {
		t.Fatalf("write text: %v", err)
	}

	// Client reads the pong reply.
	clientReader := bufio.NewReader(client)
	pongFrame, err := ReadFrame(clientReader)
	if err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if pongFrame.Opcode != OpPong {
		t.Fatalf("expected pong, got opcode %d", pongFrame.Opcode)
	}
	if string(pongFrame.Payload) != "ping-data" {
		t.Errorf("pong payload = %q, want %q", pongFrame.Payload, "ping-data")
	}

	// Server must have returned the text message.
	select {
	case res := <-serverDone:
		if res.err != nil {
			t.Fatalf("server ReadMessage: %v", res.err)
		}
		if res.op != OpText {
			t.Errorf("opcode = %d, want OpText", res.op)
		}
		if string(res.payload) != "after-ping" {
			t.Errorf("payload = %q, want %q", res.payload, "after-ping")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server timed out")
	}
}

// TestCloseEcho uses a real TCP loopback connection for the same reason as
// TestPingPong: bidirectional control-frame exchange deadlocks on net.Pipe.
func TestCloseEcho(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	type result struct {
		op      Opcode
		payload []byte
		err     error
	}
	serverDone := make(chan result, 1)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverDone <- result{err: err}
			return
		}
		defer conn.Close()
		srvConn := NewConn(conn, bufio.NewReader(conn), 10)
		op, payload, err := srvConn.ReadMessage()
		serverDone <- result{op: op, payload: payload, err: err}
	}()

	client, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(5 * time.Second))

	closePayload := make([]byte, 2)
	binary.BigEndian.PutUint16(closePayload, 1000)
	if err := WriteFrame(client, true, OpClose, closePayload, nil); err != nil {
		t.Fatalf("write close: %v", err)
	}

	// Read the echoed close frame.
	clientReader := bufio.NewReader(client)
	echoFrame, err := ReadFrame(clientReader)
	if err != nil {
		t.Fatalf("read close echo: %v", err)
	}
	if echoFrame.Opcode != OpClose {
		t.Fatalf("expected close echo, got opcode %d", echoFrame.Opcode)
	}
	if len(echoFrame.Payload) < 2 {
		t.Fatalf("echo payload too short: %d", len(echoFrame.Payload))
	}
	echoCode := binary.BigEndian.Uint16(echoFrame.Payload[:2])
	if echoCode != 1000 {
		t.Errorf("echo close code = %d, want 1000", echoCode)
	}

	// Server must have returned the close frame.
	select {
	case res := <-serverDone:
		if res.err != nil {
			t.Fatalf("server ReadMessage: %v", res.err)
		}
		if res.op != OpClose {
			t.Errorf("opcode = %d, want OpClose", res.op)
		}
		if len(res.payload) < 2 {
			t.Fatalf("close payload too short: %d", len(res.payload))
		}
		code := binary.BigEndian.Uint16(res.payload[:2])
		if code != 1000 {
			t.Errorf("close code = %d, want 1000", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server timed out")
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
