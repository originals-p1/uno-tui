// Package protocol implements the TCP framing and JSON message contract
// (readme §6). It depends on the standard library only.
package protocol

import (
	"encoding/binary"
	"errors"
	"io"
)

// MaxFramePayload is the maximum JSON payload size accepted on the wire
// (readme §9 default: 64 KiB).
const MaxFramePayload = 64 * 1024

// Frame length header size in bytes.
const frameHeaderSize = 4

var (
	// ErrInvalidFrameLength indicates a zero-length or over-limit frame.
	ErrInvalidFrameLength = errors.New("invalid frame length")
	// ErrFrameTooLarge indicates a payload exceeding MaxFramePayload.
	ErrFrameTooLarge = errors.New("frame too large")
)

// WriteFrame writes a single length-prefixed frame to w. The payload must
// already be serialized JSON and must not exceed MaxFramePayload.
func WriteFrame(w io.Writer, payload []byte) error {
	if len(payload) == 0 {
		return ErrInvalidFrameLength
	}
	if len(payload) > MaxFramePayload {
		return ErrFrameTooLarge
	}
	var header [frameHeaderSize]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(payload)))
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	if _, err := w.Write(payload); err != nil {
		return err
	}
	return nil
}

// ReadFrame reads one length-prefixed frame from r. It validates the frame
// header before allocating, preventing memory exhaustion and torn packets
// (readme §6.1).
func ReadFrame(r io.Reader) ([]byte, error) {
	var n uint32
	if err := binary.Read(r, binary.BigEndian, &n); err != nil {
		return nil, err
	}
	if n == 0 || n > MaxFramePayload {
		return nil, ErrInvalidFrameLength
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}
