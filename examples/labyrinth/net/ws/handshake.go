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

const magicGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// AcceptKey computes the Sec-WebSocket-Accept value per RFC 6455 section 4.2.2.
func AcceptKey(key string) string {
	h := sha1.New()
	h.Write([]byte(key + magicGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// Upgrade performs the server-side WebSocket upgrade handshake on conn.
// It reads the HTTP request, validates the WebSocket headers, and writes the
// 101 response. Returns the buffered reader for subsequent frame reads.
// Use this when you own the raw connection before any HTTP parsing (e.g.
// a raw TCP listener). For connections already parsed by net/http, use
// UpgradeFromRequest instead.
func Upgrade(conn net.Conn) (*bufio.Reader, error) {
	br := bufio.NewReader(conn)
	req, err := http.ReadRequest(br)
	if err != nil {
		return nil, fmt.Errorf("ws: read request: %w", err)
	}
	defer req.Body.Close()

	br, err = upgradeFromParsed(conn, req)
	if err != nil {
		return nil, err
	}
	return br, nil
}

// UpgradeFromRequest performs the WebSocket upgrade on a connection whose
// HTTP request has already been parsed by net/http (e.g. after
// http.Hijacker.Hijack). It validates the request headers and writes the
// 101 response. The caller supplies the buffered reader to use for
// subsequent frame reads (typically the reader from Hijack).
func UpgradeFromRequest(conn net.Conn, br *bufio.Reader, req *http.Request) (*bufio.Reader, error) {
	return upgradeFromParsed(conn, req)
}

// upgradeFromParsed validates an already-parsed request and writes the 101.
func upgradeFromParsed(conn net.Conn, req *http.Request) (*bufio.Reader, error) {
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

	return bufio.NewReader(conn), nil
}
