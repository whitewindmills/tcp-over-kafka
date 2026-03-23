package socks5

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
)

// TestAcceptDomainConnect exercises a SOCKS5 CONNECT request with a domain name target.
func TestAcceptDomainConnect(t *testing.T) {
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
	req = append(req, Version, CmdConnect, 0x00, AddrTypeDomain, byte(len("example.com")))
	req = append(req, []byte("example.com")...)
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
	if target != "example.com:443" {
		t.Fatalf("unexpected target %q", target)
	}
}

// TestWriteReply verifies the minimal success reply bytes.
func TestWriteReply(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		_ = WriteReply(server, ReplySucceeded)
	}()

	buf := make([]byte, 10)
	if _, err := io.ReadFull(client, buf); err != nil {
		t.Fatalf("read reply: %v", err)
	}
	if buf[0] != Version || buf[1] != ReplySucceeded || buf[3] != AddrTypeIPv4 {
		t.Fatalf("unexpected reply bytes: %v", buf)
	}
}
