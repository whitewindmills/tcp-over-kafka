package sshproxy

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"os"
	"testing"
	"time"

	"tcp-over-kafka/pkg/socks5"
)

// TestRunWithFakeSocks5Server verifies Run relays stdin/stdout through a SOCKS5 handshake.
func TestRunWithFakeSocks5Server(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	serverErr := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()

		var greet [3]byte
		if _, err := io.ReadFull(conn, greet[:]); err != nil {
			serverErr <- err
			return
		}
		if greet != [3]byte{socks5.Version, 1, socks5.MethodNoAuth} {
			serverErr <- io.ErrUnexpectedEOF
			return
		}
		if _, err := conn.Write([]byte{socks5.Version, socks5.MethodNoAuth}); err != nil {
			serverErr <- err
			return
		}

		var hdr [4]byte
		if _, err := io.ReadFull(conn, hdr[:]); err != nil {
			serverErr <- err
			return
		}
		switch hdr[3] {
		case socks5.AddrTypeDomain:
			var n [1]byte
			if _, err := io.ReadFull(conn, n[:]); err != nil {
				serverErr <- err
				return
			}
			if _, err := io.CopyN(io.Discard, conn, int64(n[0])); err != nil {
				serverErr <- err
				return
			}
		case socks5.AddrTypeIPv4:
			if _, err := io.CopyN(io.Discard, conn, 4); err != nil {
				serverErr <- err
				return
			}
		case socks5.AddrTypeIPv6:
			if _, err := io.CopyN(io.Discard, conn, 16); err != nil {
				serverErr <- err
				return
			}
		default:
			serverErr <- io.ErrUnexpectedEOF
			return
		}
		var port [2]byte
		if _, err := io.ReadFull(conn, port[:]); err != nil {
			serverErr <- err
			return
		}
		if binary.BigEndian.Uint16(port[:]) != 22 {
			serverErr <- io.ErrUnexpectedEOF
			return
		}
		if _, err := conn.Write([]byte{socks5.Version, socks5.ReplySucceeded, 0x00, socks5.AddrTypeIPv4, 0, 0, 0, 0, 0, 0}); err != nil {
			serverErr <- err
			return
		}

		payload, err := io.ReadAll(conn)
		if err != nil {
			serverErr <- err
			return
		}
		if _, err := conn.Write(payload); err != nil {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	origIn := os.Stdin
	origOut := os.Stdout
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	os.Stdin = stdinR
	os.Stdout = stdoutW
	defer func() {
		os.Stdin = origIn
		os.Stdout = origOut
		_ = stdinR.Close()
		_ = stdinW.Close()
		_ = stdoutR.Close()
		_ = stdoutW.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	runErr := make(chan error, 1)
	go func() {
		runErr <- Run(ctx, ln.Addr().String(), "example.com:22")
	}()

	payload := []byte("hello socks5 relay")
	if _, err := stdinW.Write(payload); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	_ = stdinW.Close()

	if err := <-runErr; err != nil {
		t.Fatalf("run: %v", err)
	}
	_ = stdoutW.Close()

	got, err := io.ReadAll(stdoutR)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("unexpected relay output: %q", string(got))
	}

	if err := <-serverErr; err != nil {
		t.Fatalf("server: %v", err)
	}
}
