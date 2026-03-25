package frame

import (
	"bytes"
	"encoding/json"
	"testing"
)

type routedPayload struct {
	SourcePlatformID      string `json:"sourcePlatformID"`
	SourceDeviceID        string `json:"sourceDeviceID"`
	DestinationPlatformID string `json:"destinationPlatformID"`
	DestinationDeviceID   string `json:"destinationDeviceID"`
	Message               string `json:"message"`
}

// TestRoundTrip verifies the current frame format is stable for all fields.
func TestRoundTrip(t *testing.T) {
	in := Frame{
		Kind:                  KindData,
		SourcePlatformID:      "10.0.0.167",
		SourceDeviceID:        "client-42",
		DestinationPlatformID: "10.0.0.168",
		DestinationDeviceID:   "service-7",
		ConnectionID:          "abc123",
		Sequence:              42,
		Payload:               []byte("hello"),
		Err:                   "nope",
	}
	var buf bytes.Buffer
	if err := Encode(&buf, in); err != nil {
		t.Fatalf("encode: %v", err)
	}
	out, err := Decode(&buf)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Kind != in.Kind ||
		out.SourcePlatformID != in.SourcePlatformID ||
		out.SourceDeviceID != in.SourceDeviceID ||
		out.DestinationPlatformID != in.DestinationPlatformID ||
		out.DestinationDeviceID != in.DestinationDeviceID ||
		out.ConnectionID != in.ConnectionID ||
		out.Sequence != in.Sequence ||
		out.Err != in.Err ||
		string(out.Payload) != string(in.Payload) {
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

// TestRejectUnsupportedVersion verifies that the new single-topic frame format is explicit.
func TestRejectUnsupportedVersion(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString(Magic)
	buf.WriteByte(1)
	buf.WriteByte(byte(KindOpen))
	buf.Write([]byte{0, 0})
	if _, err := Decode(&buf); err == nil {
		t.Fatal("expected decode error for old frame version")
	}
}

// TestRoundTripCarriesRoutingEnvelope verifies the frame body preserves the routing IDs used by the single-topic design.
func TestRoundTripCarriesRoutingEnvelope(t *testing.T) {
	inbound := routedPayload{
		SourcePlatformID:      "10.0.0.167",
		SourceDeviceID:        "client-proc-42",
		DestinationPlatformID: "10.0.0.168",
		DestinationDeviceID:   "server-proc-7",
		Message:               "hello over a shared topic",
	}
	payload, err := json.Marshal(inbound)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	in := Frame{
		Kind:                  KindData,
		SourcePlatformID:      inbound.SourcePlatformID,
		SourceDeviceID:        inbound.SourceDeviceID,
		DestinationPlatformID: inbound.DestinationPlatformID,
		DestinationDeviceID:   inbound.DestinationDeviceID,
		ConnectionID:          "opaque-connection",
		Payload:               payload,
	}

	var buf bytes.Buffer
	if err := Encode(&buf, in); err != nil {
		t.Fatalf("encode: %v", err)
	}

	out, err := Decode(&buf)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Kind != in.Kind ||
		out.SourcePlatformID != in.SourcePlatformID ||
		out.SourceDeviceID != in.SourceDeviceID ||
		out.DestinationPlatformID != in.DestinationPlatformID ||
		out.DestinationDeviceID != in.DestinationDeviceID ||
		out.ConnectionID != in.ConnectionID {
		t.Fatalf("frame metadata changed: %#v != %#v", out, in)
	}
	if !bytes.Equal(out.Payload, payload) {
		t.Fatalf("payload changed: %q != %q", out.Payload, payload)
	}

	var decoded routedPayload
	if err := json.Unmarshal(out.Payload, &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if decoded != inbound {
		t.Fatalf("payload round trip mismatch: %#v != %#v", decoded, inbound)
	}
}
