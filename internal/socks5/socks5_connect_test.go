package socks5

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
)

// TestConnectDomain exercises the client-side SOCKS5 CONNECT handshake.
func TestConnectDomain(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		var greet [3]byte
		if _, err := io.ReadFull(server, greet[:]); err != nil {
			t.Errorf("read greeting: %v", err)
			return
		}
		if greet != [3]byte{Version, 1, MethodNoAuth} {
			t.Errorf("unexpected greeting: %v", greet)
			return
		}
		if _, err := server.Write([]byte{Version, MethodNoAuth}); err != nil {
			t.Errorf("write method reply: %v", err)
			return
		}

		var hdr [4]byte
		if _, err := io.ReadFull(server, hdr[:]); err != nil {
			t.Errorf("read request header: %v", err)
			return
		}
		if hdr != [4]byte{Version, CmdConnect, 0x00, AddrTypeDomain} {
			t.Errorf("unexpected request header: %v", hdr)
			return
		}
		var n [1]byte
		if _, err := io.ReadFull(server, n[:]); err != nil {
			t.Errorf("read domain length: %v", err)
			return
		}
		host := make([]byte, int(n[0]))
		if _, err := io.ReadFull(server, host); err != nil {
			t.Errorf("read domain: %v", err)
			return
		}
		if string(host) != "example.com" {
			t.Errorf("unexpected host: %q", string(host))
			return
		}
		var port [2]byte
		if _, err := io.ReadFull(server, port[:]); err != nil {
			t.Errorf("read port: %v", err)
			return
		}
		if got := binary.BigEndian.Uint16(port[:]); got != 443 {
			t.Errorf("unexpected port: %d", got)
			return
		}
		if _, err := server.Write([]byte{Version, ReplySucceeded, 0x00, AddrTypeIPv4, 0, 0, 0, 0, 0, 0}); err != nil {
			t.Errorf("write reply: %v", err)
			return
		}
	}()

	if err := Connect(client, "example.com:443"); err != nil {
		t.Fatalf("connect: %v", err)
	}
	<-done
}
