package protocol

import (
	"bytes"
	"io"
	"testing"
)

func TestWriteReadFrame_RoundTrip(t *testing.T) {
	payload := []byte(`{"type":"ping"}`)
	var buf bytes.Buffer
	if err := WriteFrame(&buf, payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("got %q, want %q", got, payload)
	}
}

func TestReadFrame_ZeroLength(t *testing.T) {
	// Header says 0 -> must be rejected.
	raw := []byte{0, 0, 0, 0}
	_, err := ReadFrame(bytes.NewReader(raw))
	if err != ErrInvalidFrameLength {
		t.Fatalf("err = %v, want ErrInvalidFrameLength", err)
	}
}

func TestReadFrame_OverMax(t *testing.T) {
	// Header says > MaxFramePayload -> rejected before allocation.
	var n uint32 = MaxFramePayload + 1
	raw := []byte{byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}
	_, err := ReadFrame(bytes.NewReader(raw))
	if err != ErrInvalidFrameLength {
		t.Fatalf("err = %v, want ErrInvalidFrameLength", err)
	}
}

func TestReadFrame_Truncated(t *testing.T) {
	// Header says 10 bytes but only 3 available.
	raw := []byte{0, 0, 0, 10, 1, 2, 3}
	r := bytes.NewReader(raw)
	// Wrap in a reader that returns what it has (io.ReadFull handles EOF).
	_, err := ReadFrame(r)
	if err == nil {
		t.Fatal("expected error on truncated frame")
	}
	if err == io.EOF {
		t.Fatal("ReadFull should return ErrUnexpectedEOF, not EOF")
	}
}

func TestWriteFrame_TooLarge(t *testing.T) {
	payload := make([]byte, MaxFramePayload+1)
	err := WriteFrame(&bytes.Buffer{}, payload)
	if err != ErrFrameTooLarge {
		t.Fatalf("err = %v, want ErrFrameTooLarge", err)
	}
}

func TestWriteFrame_Empty(t *testing.T) {
	err := WriteFrame(&bytes.Buffer{}, nil)
	if err != ErrInvalidFrameLength {
		t.Fatalf("err = %v, want ErrInvalidFrameLength", err)
	}
}

func TestReadFrame_StickyPackets(t *testing.T) {
	// Two frames concatenated into one stream must decode independently.
	a := []byte(`{"a":1}`)
	b := []byte(`{"b":2}`)
	var buf bytes.Buffer
	WriteFrame(&buf, a)
	WriteFrame(&buf, b)

	first, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if !bytes.Equal(first, a) {
		t.Fatalf("first = %q, want %q", first, a)
	}
	second, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}
	if !bytes.Equal(second, b) {
		t.Fatalf("second = %q, want %q", second, b)
	}
	// Stream exhausted.
	if _, err := ReadFrame(&buf); err == nil {
		t.Fatal("expected EOF on exhausted stream")
	}
}
