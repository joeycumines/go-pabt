//go:build example

package ws

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"strings"
)

const magicGUID = "258EAFA5-E914-47DA-95CA-5AB5DC85B11B"

// AcceptKey computes the Sec-WebSocket-Accept value per RFC 6455 section 4.2.2.
func AcceptKey(key string) string {
	h := sha1.New()
	h.Write([]byte(key + magicGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// Upgrade performs the server-side WebSocket upgrade handshake on conn.
// It reads the HTTP request, validates the WebSocket headers, and writes the
// 101 response. Returns the buffered reader for subsequent frame reads.
func Upgrade(conn net.Conn) (*bufio.Reader, error) {
	br := bufio.NewReader(conn)
	req, err := http.ReadRequest(br)
	if err != nil {
		return nil, fmt.Errorf("ws: read request: %w", err)
	}
	defer req.Body.Close()

	if !strings.EqualFold(req.Header.Get("Upgrade"), "websocket") {
		return nil, fmt.Errorf("ws: missing Upgrade: websocket")
	}
	if !strings.Contains(strings.ToLower(req.Header.Get("Connection")), "upgrade") {
		return nil, fmt.Errorf("ws: missing Connection: upgrade")
	}
	key := req.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		return nil, fmt.Errorf("ws: missing Sec-WebSocket-Key")
	}
	if req.Header.Get("Sec-WebSocket-Version") != "13" {
		return nil, fmt.Errorf("ws: unsupported version: %s", req.Header.Get("Sec-WebSocket-Version"))
	}

	accept := AcceptKey(key)
	resp := "HTTP/1.1 101 Switching Protocols\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + accept + "\r\n" +
		"\r\n"
	if _, err := conn.Write([]byte(resp)); err != nil {
		return nil, fmt.Errorf("ws: write response: %w", err)
	}

	return br, nil
}
