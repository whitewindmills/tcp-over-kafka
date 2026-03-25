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

	"tcp-over-kafka/pkg/frame"
)

var (
	testClientEndpoint = Endpoint{PlatformID: "10.0.0.167", DeviceID: "client-proc-42"}
	testServerEndpoint = Endpoint{PlatformID: "10.0.0.168", DeviceID: "service-echo"}
)

// testBus is an in-memory tunnelBus used to connect the client and server loops in tests.
type testBus struct {
	recv <-chan frame.Frame
	send chan<- frame.Frame
}

func (b *testBus) Send(ctx context.Context, f frame.Frame) error {
	select {
	case b.send <- f:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

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

func (b *testBus) Close() error { return nil }

func newTestBusPair() (*testBus, *testBus) {
	c2s := make(chan frame.Frame, 64)
	s2c := make(chan frame.Frame, 64)
	return &testBus{recv: s2c, send: c2s}, &testBus{recv: c2s, send: s2c}
}

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

func runTestServerLoop(ctx context.Context, bus tunnelBus, sessions *serverRegistry, platformID string, services map[string]string, maxFrame int) error {
	for {
		f, err := bus.Receive(ctx)
		if err != nil {
			return err
		}
		if f.DestinationPlatformID != platformID {
			continue
		}
		switch f.Kind {
		case frame.KindOpen:
			if err := serverOpenSession(ctx, bus, sessions, platformID, services, maxFrame, f); err != nil {
				_ = bus.Send(context.Background(), frame.Frame{
					Kind:                  frame.KindError,
					SourcePlatformID:      platformID,
					SourceDeviceID:        f.DestinationDeviceID,
					DestinationPlatformID: f.SourcePlatformID,
					DestinationDeviceID:   f.SourceDeviceID,
					ConnectionID:          f.ConnectionID,
					Err:                   err.Error(),
				})
			}
		case frame.KindReady:
			if sess := sessions.getFrame(f); sess != nil {
				sess.startOutbound(ctx, bus, sessions, maxFrame)
			}
		case frame.KindData:
			if sess := sessions.getFrame(f); sess != nil && len(f.Payload) > 0 {
				if err := writeAll(sess.conn, f.Payload); err != nil {
					sess.close()
					sessions.removeFrame(f)
				}
			}
		case frame.KindClose, frame.KindError:
			if sess := sessions.getFrame(f); sess != nil {
				sess.close()
				sessions.removeFrame(f)
			}
		}
	}
}

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

func performSOCKS5Handshake(t *testing.T, conn net.Conn, target string) {
	t.Helper()
	if err := handshakeSOCKS5(conn, target); err != nil {
		t.Fatalf("socks5 handshake: %v", err)
	}
}

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
		errCh <- runTestServerLoop(ctx, serverBus, serverSessions, testServerEndpoint.PlatformID, map[string]string{
			testServerEndpoint.DeviceID: targetAddr,
		}, 128)
	}()
	go clientReceiveLoop(ctx, clientBus, clientSessions, testClientEndpoint)

	appConn, proxyConn := net.Pipe()
	defer appConn.Close()
	defer proxyConn.Close()

	cfg := ClientConfig{
		PlatformID:   testClientEndpoint.PlatformID,
		DeviceID:     testClientEndpoint.DeviceID,
		Routes:       map[string]Endpoint{targetAddr: testServerEndpoint},
		MaxFrameSize: 128,
	}
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

func TestConcurrentSessionsIsolation(t *testing.T) {
	t.Parallel()

	leftAddr, stopLeft := startPrefixTarget(t, "left:")
	defer stopLeft()
	rightAddr, stopRight := startPrefixTarget(t, "right:")
	defer stopRight()

	clientBus, serverBus := newTestBusPair()
	clientSessions := newClientRegistry()
	serverSessions := newServerRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runTestServerLoop(ctx, serverBus, serverSessions, testServerEndpoint.PlatformID, map[string]string{
			"left-service":  leftAddr,
			"right-service": rightAddr,
		}, 128)
	}()
	go clientReceiveLoop(ctx, clientBus, clientSessions, testClientEndpoint)

	type sessionResult struct {
		name string
		got  []byte
		err  error
	}
	runSession := func(name, targetAddr, deviceID string, payload []byte) sessionResult {
		appConn, proxyConn := net.Pipe()
		defer proxyConn.Close()
		defer appConn.Close()

		cfg := ClientConfig{
			PlatformID: testClientEndpoint.PlatformID,
			DeviceID:   testClientEndpoint.DeviceID,
			Routes: map[string]Endpoint{
				targetAddr: {
					PlatformID: testServerEndpoint.PlatformID,
					DeviceID:   deviceID,
				},
			},
			MaxFrameSize: 128,
		}
		go handleClientConn(ctx, clientBus, clientSessions, proxyConn, cfg)
		if err := handshakeSOCKS5(appConn, targetAddr); err != nil {
			return sessionResult{err: fmt.Errorf("handshake: %w", err)}
		}
		if _, err := appConn.Write(payload); err != nil {
			return sessionResult{err: fmt.Errorf("write payload: %w", err)}
		}
		got := make([]byte, len(name)+1+len(payload))
		if _, err := io.ReadFull(appConn, got); err != nil {
			return sessionResult{err: fmt.Errorf("read payload: %w", err)}
		}
		return sessionResult{name: name, got: got}
	}

	leftPayload := bytes.Repeat([]byte("A"), 64)
	rightPayload := bytes.Repeat([]byte("B"), 64)

	var wg sync.WaitGroup
	results := make(chan sessionResult, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		results <- runSession("left", leftAddr, "left-service", leftPayload)
	}()
	go func() {
		defer wg.Done()
		results <- runSession("right", rightAddr, "right-service", rightPayload)
	}()
	wg.Wait()
	close(results)

	for res := range results {
		if res.err != nil {
			t.Fatalf("session failed: %v", res.err)
		}
		switch res.name {
		case "left":
			want := append([]byte("left:"), leftPayload...)
			if !bytes.Equal(res.got, want) {
				t.Fatalf("left session mismatch")
			}
		case "right":
			want := append([]byte("right:"), rightPayload...)
			if !bytes.Equal(res.got, want) {
				t.Fatalf("right session mismatch")
			}
		default:
			t.Fatalf("unexpected session label %q", res.name)
		}
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
