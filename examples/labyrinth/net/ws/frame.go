//go:build example

package ws

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Opcode identifies the frame type per RFC 6455 section 5.2.
type Opcode byte

const (
	OpContinuation Opcode = 0x0
	OpText         Opcode = 0x1
	OpBinary       Opcode = 0x2
	OpClose        Opcode = 0x8
	OpPing         Opcode = 0x9
	OpPong         Opcode = 0xA
)

// Frame is one WebSocket frame.
type Frame struct {
	FIN     bool
	Opcode  Opcode
	Masked  bool
	Mask    [4]byte
	Payload []byte
}

// ReadFrame reads one frame from r. It handles 7-bit, 16-bit, and 64-bit
// payload lengths, and the masking bit.
func ReadFrame(r io.Reader) (Frame, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return Frame{}, err
	}

	f := Frame{
		FIN:    hdr[0]&0x80 != 0,
		Opcode: Opcode(hdr[0] & 0x0F),
		Masked: hdr[1]&0x80 != 0,
	}

	payloadLen := int(hdr[1] & 0x7F)
	switch {
	case payloadLen == 126:
		var ext [2]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return Frame{}, err
		}
		payloadLen = int(binary.BigEndian.Uint16(ext[:]))
	case payloadLen == 127:
		var ext [8]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return Frame{}, err
		}
		v := binary.BigEndian.Uint64(ext[:])
		if v > uint64(^uint(0)) {
			return Frame{}, fmt.Errorf("ws: frame too large: %d", v)
		}
		payloadLen = int(v)
	}

	if f.Masked {
		if _, err := io.ReadFull(r, f.Mask[:]); err != nil {
			return Frame{}, err
		}
	}

	f.Payload = make([]byte, payloadLen)
	if _, err := io.ReadFull(r, f.Payload); err != nil {
		return Frame{}, err
	}

	if f.Masked {
		unmask(f.Payload, f.Mask)
	}

	return f, nil
}

// WriteFrame writes one frame to w. If mask is non-nil, the payload is masked.
func WriteFrame(w io.Writer, fin bool, opcode Opcode, payload []byte, mask *[4]byte) error {
	var hdr []byte
	b0 := byte(opcode)
	if fin {
		b0 |= 0x80
	}
	hdr = append(hdr, b0)

	masked := mask != nil
	var maskBit byte
	if masked {
		maskBit = 0x80
	}

	n := len(payload)
	switch {
	case n < 126:
		hdr = append(hdr, maskBit|byte(n))
	case n < 65536:
		hdr = append(hdr, maskBit|126)
		var ext [2]byte
		binary.BigEndian.PutUint16(ext[:], uint16(n))
		hdr = append(hdr, ext[:]...)
	default:
		hdr = append(hdr, maskBit|127)
		var ext [8]byte
		binary.BigEndian.PutUint64(ext[:], uint64(n))
		hdr = append(hdr, ext[:]...)
	}

	if masked {
		hdr = append(hdr, mask[:]...)
	}

	// Build the entire frame (header + payload) and issue a single Write so
	// the frame is atomic on the wire. Splitting header and payload into two
	// writes deadlocks on synchronous transports (net.Pipe) and risks
	// interleaving under concurrent writes on real sockets.
	if n == 0 {
		_, err := w.Write(hdr)
		return err
	}

	if masked {
		buf := make([]byte, n)
		copy(buf, payload)
		unmask(buf, *mask)
		hdr = append(hdr, buf...)
	} else {
		hdr = append(hdr, payload...)
	}

	_, err := w.Write(hdr)
	return err
}

// unmask XORs payload with the mask key per RFC 6455 section 5.3.
func unmask(payload []byte, mask [4]byte) {
	for i := range payload {
		payload[i] ^= mask[i%4]
	}
}
