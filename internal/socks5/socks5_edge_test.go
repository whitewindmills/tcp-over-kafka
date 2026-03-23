package socks5

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
)

// TestNegotiateUnsupportedVersion rejects an unsupported SOCKS version.
func TestNegotiateUnsupportedVersion(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	done := make(chan error, 1)
	go func() {
		done <- Negotiate(server)
	}()

	if _, err := client.Write([]byte{0x04, 0x00}); err != nil {
		t.Fatalf("write greeting: %v", err)
	}

	if err := <-done; !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("expected unsupported version, got %v", err)
	}
}

// TestReadConnectRequestUnsupportedCommand rejects non-CONNECT commands.
func TestReadConnectRequestUnsupportedCommand(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	done := make(chan error, 1)
	go func() {
		_, err := ReadConnectRequest(server)
		done <- err
	}()

	req := []byte{Version, 0x02, 0x00, AddrTypeIPv4}
	if _, err := client.Write(req); err != nil {
		t.Fatalf("write request: %v", err)
	}

	err := <-done
	var reqErr *RequestError
	if !errors.As(err, &reqErr) || reqErr.Reply != ReplyCmdNotSupported {
		t.Fatalf("expected command not supported, got %v", err)
	}
}

// TestAcceptIPv6Connect exercises a CONNECT request with an IPv6 address.
func TestAcceptIPv6Connect(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	done := make(chan struct{})
	var target string
	var err error
	go func() {
		target, err = Accept(server)
		close(done)
	}()

	if _, err := client.Write([]byte{Version, 1, MethodNoAuth}); err != nil {
		t.Fatalf("write greeting: %v", err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(client, reply); err != nil {
		t.Fatalf("read method reply: %v", err)
	}
	if reply[0] != Version || reply[1] != MethodNoAuth {
		t.Fatalf("unexpected method reply: %v", reply)
	}

	req := make([]byte, 0, 32)
	req = append(req, Version, CmdConnect, 0x00, AddrTypeIPv6)
	req = append(req, net.ParseIP("2001:db8::1").To16()...)
	var port [2]byte
	binary.BigEndian.PutUint16(port[:], 443)
	req = append(req, port[:]...)
	if _, err := client.Write(req); err != nil {
		t.Fatalf("write request: %v", err)
	}

	<-done
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if target != "[2001:db8::1]:443" {
		t.Fatalf("unexpected target %q", target)
	}
}

// TestReadConnectRequestTruncated verifies short reads surface a request error.
func TestReadConnectRequestTruncated(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	done := make(chan error, 1)
	go func() {
		_, err := ReadConnectRequest(server)
		done <- err
	}()

	req := []byte{Version, CmdConnect, 0x00, AddrTypeDomain, 5, 'h', 'e'}
	if _, err := client.Write(req); err != nil {
		t.Fatalf("write request: %v", err)
	}
	_ = client.Close()

	err := <-done
	var reqErr *RequestError
	if !errors.As(err, &reqErr) {
		t.Fatalf("expected request error, got %v", err)
	}
}
