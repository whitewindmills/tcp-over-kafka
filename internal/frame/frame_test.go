package frame

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// TestRoundTrip verifies the current frame format is stable for all fields.
func TestRoundTrip(t *testing.T) {
	in := Frame{
		Kind:         KindData,
		SessionID:    "poc",
		ConnectionID: "abc123",
		TargetAddr:   "example.com:443",
		Sequence:     42,
		Payload:      []byte("hello"),
		Err:          "nope",
	}
	var buf bytes.Buffer
	if err := Encode(&buf, in); err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := Decode(&buf)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Kind != in.Kind || out.SessionID != in.SessionID || out.ConnectionID != in.ConnectionID || out.TargetAddr != in.TargetAddr || out.Sequence != in.Sequence || out.Err != in.Err || string(out.Payload) != string(in.Payload) {
		t.Fatalf("round trip mismatch: %#v != %#v", out, in)
	}
}

// TestBadMagic ensures obviously invalid data is rejected.
func TestBadMagic(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("BAD!")
	buf.WriteByte(Version)
	buf.WriteByte(byte(KindOpen))
	buf.Write([]byte{0, 0})
	if _, err := Decode(&buf); err == nil {
		t.Fatal("expected decode error")
	}
}

// TestDecodeV1 verifies backward compatibility with the previous frame version.
func TestDecodeV1(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString(Magic)
	buf.WriteByte(1)
	buf.WriteByte(byte(KindOpen))
	buf.Write([]byte{0, 0})
	if err := writeString(&buf, "poc"); err != nil {
		t.Fatalf("write session: %v", err)
	}
	if err := writeString(&buf, "conn"); err != nil {
		t.Fatalf("write conn: %v", err)
	}
	if err := binary.Write(&buf, binary.BigEndian, uint64(7)); err != nil {
		t.Fatalf("write seq: %v", err)
	}
	if err := writeBytes(&buf, []byte("payload")); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	if err := writeString(&buf, ""); err != nil {
		t.Fatalf("write err: %v", err)
	}

	out, err := Decode(&buf)
	if err != nil {
		t.Fatalf("decode v1: %v", err)
	}
	if out.TargetAddr != "" {
		t.Fatalf("expected empty target addr, got %q", out.TargetAddr)
	}
	if out.Kind != KindOpen || out.SessionID != "poc" || out.ConnectionID != "conn" || out.Sequence != 7 || string(out.Payload) != "payload" {
		t.Fatalf("unexpected decoded frame: %#v", out)
	}
}
