// Package ipc implements the daemon transport: 4-byte big-endian length-prefixed JSON
// frames over loopback TCP. Connection lifetime is per-call in V1 — no pooling.
package ipc

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// MaxFrameBytes mirrors the daemon-side Framing.MAX_FRAME_BYTES guard.
const MaxFrameBytes = 32 * 1024 * 1024

// WriteFrame writes one length-prefixed frame to w.
func WriteFrame(w io.Writer, payload []byte) error {
	if len(payload) > MaxFrameBytes {
		return fmt.Errorf("frame too large: %d bytes", len(payload))
	}
	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, uint32(len(payload)))
	if _, err := w.Write(header); err != nil {
		return fmt.Errorf("write header: %w", err)
	}
	if _, err := w.Write(payload); err != nil {
		return fmt.Errorf("write body: %w", err)
	}
	return nil
}

// ReadFrame reads one length-prefixed frame from r.
func ReadFrame(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, err
		}
		return nil, fmt.Errorf("read header: %w", err)
	}
	length := binary.BigEndian.Uint32(header[:])
	if length > MaxFrameBytes {
		return nil, fmt.Errorf("invalid frame length: %d", length)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return body, nil
}
