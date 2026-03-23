package tunnel

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"tcp-over-kafka/internal/frame"
)

// testBus is an in-memory tunnelBus used to connect the client and server loops in tests.
type testBus struct {
	recv <-chan frame.Frame
	send chan<- frame.Frame
}

// Send enqueues a frame for the peer bus.
func (b *testBus) Send(ctx context.Context, f frame.Frame) error {
	select {
	case b.send <- f:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Receive waits for the next frame from the peer bus.
func (b *testBus) Receive(ctx context.Context) (frame.Frame, error) {
	select {
	case f, ok := <-b.recv:
		if !ok {
			return frame.Frame{}, io.EOF
		}
		return f, nil
	case <-ctx.Done():
		return frame.Frame{}, ctx.Err()
	}
}

// Close is a no-op for the in-memory test bus.
func (b *testBus) Close() error { return nil }

// newTestBusPair returns two connected tunnelBus endpoints.
func newTestBusPair() (*testBus, *testBus) {
	c2s := make(chan frame.Frame, 64)
	s2c := make(chan frame.Frame, 64)
	return &testBus{recv: s2c, send: c2s}, &testBus{recv: c2s, send: s2c}
}

// startEchoTarget listens on localhost and echoes every byte back to the sender.
func startEchoTarget(t *testing.T) (addr string, stop func()) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen echo target: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				for {
					n, err := c.Read(buf)
					if n > 0 {
						if _, werr := c.Write(buf[:n]); werr != nil {
							return
						}
					}
					if err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	return ln.Addr().String(), func() {
		_ = ln.Close()
		<-done
	}
}

// startPrefixTarget listens on localhost and echoes each payload with a stable prefix.
func startPrefixTarget(t *testing.T, prefix string) (addr string, stop func()) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen prefix target: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				for {
					n, err := c.Read(buf)
					if n > 0 {
						out := append([]byte(prefix), buf[:n]...)
						if _, werr := c.Write(out); werr != nil {
							return
						}
					}
					if err != nil {
						return
					}
				}
			}(conn)
		}
	}()

	return ln.Addr().String(), func() {
		_ = ln.Close()
		<-done
	}
}

// runTestServerLoop applies the same frame handling logic as RunServer but against a fake bus.
func runTestServerLoop(ctx context.Context, bus tunnelBus, sessions *serverRegistry, defaultTarget string, maxFrame int) error {
	for {
		f, err := bus.Receive(ctx)
		if err != nil {
			return err
		}
		switch f.Kind {
		case frame.KindOpen:
			if err := serverOpenSession(ctx, bus, sessions, defaultTarget, maxFrame, f); err != nil {
				_ = bus.Send(context.Background(), frame.Frame{
					Kind:         frame.KindError,
					SessionID:    f.SessionID,
					ConnectionID: f.ConnectionID,
					Err:          err.Error(),
				})
			}
		case frame.KindReady:
			if sess := sessions.get(f.SessionID, f.ConnectionID); sess != nil {
				sess.startOutbound(ctx, bus, sessions, maxFrame)
			}
		case frame.KindData:
			if sess := sessions.get(f.SessionID, f.ConnectionID); sess != nil && len(f.Payload) > 0 {
				if err := writeAll(sess.conn, f.Payload); err != nil {
					sess.close()
					sessions.remove(f.SessionID, f.ConnectionID)
				}
			}
		case frame.KindClose, frame.KindError:
			if sess := sessions.get(f.SessionID, f.ConnectionID); sess != nil {
				sess.close()
				sessions.remove(f.SessionID, f.ConnectionID)
			}
		}
	}
}

// handshakeSOCKS5 completes the proxy greeting and CONNECT request from the app side.
func handshakeSOCKS5(conn net.Conn, target string) error {
	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host).To4()
	if ip == nil {
		return fmt.Errorf("target host is not ipv4: %q", host)
	}
	port, err := net.LookupPort("tcp", portStr)
	if err != nil {
		return err
	}

	if _, err := conn.Write([]byte{5, 1, 0}); err != nil {
		return err
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return err
	}
	if !bytes.Equal(reply, []byte{5, 0}) {
		return fmt.Errorf("unexpected method reply: %v", reply)
	}

	req := make([]byte, 0, 10)
	req = append(req, 5, 1, 0, 1)
	req = append(req, ip...)
	var p [2]byte
	binary.BigEndian.PutUint16(p[:], uint16(port))
	req = append(req, p[:]...)
	if _, err := conn.Write(req); err != nil {
		return err
	}

	replyBuf := make([]byte, 10)
	if _, err := io.ReadFull(conn, replyBuf); err != nil {
		return err
	}
	if replyBuf[1] != 0 {
		return fmt.Errorf("unexpected connect reply: %v", replyBuf)
	}
	return nil
}

// performSOCKS5Handshake completes the proxy greeting and CONNECT request from the app side.
func performSOCKS5Handshake(t *testing.T, conn net.Conn, target string) {
	t.Helper()
	if err := handshakeSOCKS5(conn, target); err != nil {
		t.Fatalf("socks5 handshake: %v", err)
	}
}

// TestTunnelLifecycleLargePayload verifies open, ready, data, and close with a large payload.
func TestTunnelLifecycleLargePayload(t *testing.T) {
	t.Parallel()

	targetAddr, stopTarget := startEchoTarget(t)
	defer stopTarget()

	clientBus, serverBus := newTestBusPair()
	clientSessions := newClientRegistry()
	serverSessions := newServerRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runTestServerLoop(ctx, serverBus, serverSessions, "", 128)
	}()
	go clientReceiveLoop(ctx, clientBus, clientSessions)

	appConn, proxyConn := net.Pipe()
	defer appConn.Close()
	defer proxyConn.Close()

	cfg := ClientConfig{TunnelID: "poc-test", MaxFrameSize: 128}
	go handleClientConn(ctx, clientBus, clientSessions, proxyConn, cfg)

	performSOCKS5Handshake(t, appConn, targetAddr)

	payload := bytes.Repeat([]byte("0123456789abcdef"), 4096)
	if _, err := appConn.Write(payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	got := make([]byte, len(payload))
	if _, err := io.ReadFull(appConn, got); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch")
	}

	_ = appConn.Close()
	cancel()
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
			t.Fatalf("server loop: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server loop did not exit")
	}
}

// TestConcurrentSessionsIsolation verifies that simultaneous sessions do not cross streams.
func TestConcurrentSessionsIsolation(t *testing.T) {
	t.Parallel()

	leftAddr, stopLeft := startEchoTarget(t)
	defer stopLeft()
	rightAddr, stopRight := startEchoTarget(t)
	defer stopRight()

	clientBus, serverBus := newTestBusPair()
	clientSessions := newClientRegistry()
	serverSessions := newServerRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runTestServerLoop(ctx, serverBus, serverSessions, "", 128)
	}()
	go clientReceiveLoop(ctx, clientBus, clientSessions)

	type sessionResult struct {
		name string
		got  []byte
		err  error
	}
	runSession := func(name, targetAddr string, payload []byte) sessionResult {
		appConn, proxyConn := net.Pipe()
		defer proxyConn.Close()
		defer appConn.Close()

		cfg := ClientConfig{TunnelID: "poc-test", MaxFrameSize: 128}
		go handleClientConn(ctx, clientBus, clientSessions, proxyConn, cfg)
		if err := handshakeSOCKS5(appConn, targetAddr); err != nil {
			return sessionResult{err: fmt.Errorf("handshake: %w", err)}
		}
		if _, err := appConn.Write(payload); err != nil {
			return sessionResult{err: fmt.Errorf("write payload: %w", err)}
		}
		got := make([]byte, len(payload))
		if _, err := io.ReadFull(appConn, got); err != nil {
			return sessionResult{err: fmt.Errorf("read payload: %w", err)}
		}
		return sessionResult{name: name, got: got}
	}

	leftPayload := bytes.Repeat([]byte("A"), 16*1024)
	rightPayload := bytes.Repeat([]byte("B"), 16*1024)

	var wg sync.WaitGroup
	results := make(chan sessionResult, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		results <- runSession("left", leftAddr, leftPayload)
	}()
	go func() {
		defer wg.Done()
		results <- runSession("right", rightAddr, rightPayload)
	}()
	wg.Wait()
	close(results)

	seenLeft := false
	seenRight := false
	for res := range results {
		if res.err != nil {
			t.Fatalf("session failed: %v", res.err)
		}
		switch res.name {
		case "left":
			seenLeft = true
			if !bytes.Equal(res.got, leftPayload) {
				t.Fatalf("left session mismatch")
			}
		case "right":
			seenRight = true
			if !bytes.Equal(res.got, rightPayload) {
				t.Fatalf("right session mismatch")
			}
		default:
			t.Fatalf("unexpected session label %q", res.name)
		}
	}
	if !seenLeft || !seenRight {
		t.Fatalf("missing session results: left=%v right=%v", seenLeft, seenRight)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
			t.Fatalf("server loop: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server loop did not exit")
	}
}
