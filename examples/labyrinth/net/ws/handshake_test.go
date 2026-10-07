//go:build example

package ws

import (
	"net"
	"testing"
	"time"
)

// TestHandshakeAgainstRawTCP tests the full upgrade handshake with a raw
// TCP client that speaks HTTP/1.1.
func TestHandshakeAgainstRawTCP(t *testing.T) {
	// Start a TCP listener.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	// Server goroutine: accept and upgrade.
	errCh := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			errCh <- err
			return
		}
		defer conn.Close()
		_, err = Upgrade(conn)
		errCh <- err
	}()

	// Client: send the HTTP upgrade request.
	conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	handshake := "GET /ws HTTP/1.1\r\n" +
		"Host: localhost\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"\r\n"
	if _, err := conn.Write([]byte(handshake)); err != nil {
		t.Fatalf("write handshake: %v", err)
	}

	// Read the 101 response.
	buf := make([]byte, 4096)
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	response := string(buf[:n])

	if len(response) < 12 || response[:12] != "HTTP/1.1 101" {
		t.Fatalf("expected 101 response, got: %q", response[:min(len(response), 50)])
	}
	if !contains(response, "Sec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=") {
		t.Fatalf("wrong accept key in response: %q", response)
	}

	// Wait for server to finish.
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("server Upgrade: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server timed out")
	}
}

// TestHandshakeRejectsBadRequests tests that invalid upgrade requests are rejected.
func TestHandshakeRejectsBadRequests(t *testing.T) {
	tests := []struct {
		name    string
		request string
	}{
		{
			"missing upgrade",
			"GET /ws HTTP/1.1\r\nHost: localhost\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n",
		},
		{
			"missing key",
			"GET /ws HTTP/1.1\r\nHost: localhost\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\n\r\n",
		},
		{
			"wrong version",
			"GET /ws HTTP/1.1\r\nHost: localhost\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 12\r\n\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("listen: %v", err)
			}
			defer ln.Close()

			errCh := make(chan error, 1)
			go func() {
				conn, err := ln.Accept()
				if err != nil {
					errCh <- err
					return
				}
				defer conn.Close()
				_, err = Upgrade(conn)
				errCh <- err
			}()

			conn, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
			if err != nil {
				t.Fatalf("dial: %v", err)
			}
			defer conn.Close()

			conn.Write([]byte(tt.request))

			select {
			case err := <-errCh:
				if err == nil {
					t.Fatal("expected error for bad request, got nil")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("server timed out")
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
