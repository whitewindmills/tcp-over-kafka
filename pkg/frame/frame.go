package frame

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	Magic   = "TKK1"
	Version = 3
)

// Kind identifies the semantic meaning of a frame on the Kafka wire.
type Kind uint8

const (
	KindOpen Kind = iota + 1
	KindOpenAck
	KindData
	KindClose
	KindError
	KindReady
)

type Frame struct {
	Kind           Kind
	SourceNID      string
	SourceEID      string
	DestinationNID string
	DestinationEID string
	ConnectionID   string
	Sequence       uint64
	Payload        []byte
	Err            string
}

// MarshalBinary encodes a frame into the on-the-wire binary representation.
func (f Frame) MarshalBinary() ([]byte, error) {
	var buf bytes.Buffer
	if err := Encode(&buf, f); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Encode writes a frame to an io.Writer using the current wire format.
func Encode(w io.Writer, f Frame) error {
	if _, err := io.WriteString(w, Magic); err != nil {
		return err
	}
	if err := binary.Write(w, binary.BigEndian, uint8(Version)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.BigEndian, uint8(f.Kind)); err != nil {
		return err
	}
	if err := binary.Write(w, binary.BigEndian, uint16(0)); err != nil {
		return err
	}
	if err := writeString(w, f.SourceNID); err != nil {
		return err
	}
	if err := writeString(w, f.SourceEID); err != nil {
		return err
	}
	if err := writeString(w, f.DestinationNID); err != nil {
		return err
	}
	if err := writeString(w, f.DestinationEID); err != nil {
		return err
	}
	if err := writeString(w, f.ConnectionID); err != nil {
		return err
	}
	if err := binary.Write(w, binary.BigEndian, f.Sequence); err != nil {
		return err
	}
	if err := writeBytes(w, f.Payload); err != nil {
		return err
	}
	return writeString(w, f.Err)
}

// Decode parses one frame from an io.Reader.
func Decode(r io.Reader) (Frame, error) {
	var f Frame
	var hdr [8]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return f, err
	}
	if string(hdr[:4]) != Magic {
		return f, errors.New("invalid frame magic")
	}
	if hdr[4] != Version {
		return f, fmt.Errorf("unsupported frame version %d", hdr[4])
	}
	f.Kind = Kind(hdr[5])
	srcPlatform, err := readString(r)
	if err != nil {
		return f, err
	}
	srcDevice, err := readString(r)
	if err != nil {
		return f, err
	}
	dstPlatform, err := readString(r)
	if err != nil {
		return f, err
	}
	dstDevice, err := readString(r)
	if err != nil {
		return f, err
	}
	connectionID, err := readString(r)
	if err != nil {
		return f, err
	}
	f.SourceNID = srcPlatform
	f.SourceEID = srcDevice
	f.DestinationNID = dstPlatform
	f.DestinationEID = dstDevice
	f.ConnectionID = connectionID
	if err := binary.Read(r, binary.BigEndian, &f.Sequence); err != nil {
		return f, err
	}
	payload, err := readBytes(r)
	if err != nil {
		return f, err
	}
	f.Payload = payload
	errStr, err := readString(r)
	if err != nil {
		return f, err
	}
	f.Err = errStr
	return f, nil
}

// writeString stores a length-prefixed string.
func writeString(w io.Writer, s string) error {
	if len(s) > int(^uint16(0)) {
		return fmt.Errorf("string too long: %d", len(s))
	}
	if err := binary.Write(w, binary.BigEndian, uint16(len(s))); err != nil {
		return err
	}
	_, err := io.WriteString(w, s)
	return err
}

// writeBytes stores a length-prefixed byte slice.
func writeBytes(w io.Writer, b []byte) error {
	if len(b) > int(^uint32(0)) {
		return fmt.Errorf("payload too large: %d", len(b))
	}
	if err := binary.Write(w, binary.BigEndian, uint32(len(b))); err != nil {
		return err
	}
	_, err := w.Write(b)
	return err
}

// readString reads a length-prefixed string.
func readString(r io.Reader) (string, error) {
	var n uint16
	if err := binary.Read(r, binary.BigEndian, &n); err != nil {
		return "", err
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

// readBytes reads a length-prefixed byte slice.
func readBytes(r io.Reader) ([]byte, error) {
	var n uint32
	if err := binary.Read(r, binary.BigEndian, &n); err != nil {
		return nil, err
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}
