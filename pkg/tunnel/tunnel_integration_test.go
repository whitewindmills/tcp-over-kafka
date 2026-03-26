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
	testNodeA = Config{
		Broker:     "127.0.0.1:9092",
		Topic:      "tcp-over-kafka",
		PlatformID: "10.0.0.167",
		ListenAddr: "127.0.0.1:12345",
		Routes: map[string]Endpoint{
			"10.0.0.168:22":  {PlatformID: "10.0.0.168", DeviceID: "ssh"},
			"10.0.0.168:443": {PlatformID: "10.0.0.168", DeviceID: "web"},
		},
		Services: map[string]string{
			"ssh": "127.0.0.1:22",
			"web": "127.0.0.1:443",
		},
		MaxFrameSize: 128,
	}
	testNodeB = Config{
		Broker:     "127.0.0.1:9092",
		Topic:      "tcp-over-kafka",
		PlatformID: "10.0.0.168",
		ListenAddr: "127.0.0.1:12345",
		Routes: map[string]Endpoint{
			"10.0.0.167:22":  {PlatformID: "10.0.0.167", DeviceID: "ssh"},
			"10.0.0.167:443": {PlatformID: "10.0.0.167", DeviceID: "web"},
		},
		Services: map[string]string{
			"ssh": "127.0.0.1:22",
			"web": "127.0.0.1:443",
		},
		MaxFrameSize: 128,
	}
)

// testBus is an in-memory tunnelBus used to connect node loops in tests.
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
	aToB := make(chan frame.Frame, 64)
	bToA := make(chan frame.Frame, 64)
	return &testBus{recv: bToA, send: aToB}, &testBus{recv: aToB, send: bToA}
}

type pipeHandler func(net.Conn)

func echoPipeHandler(conn net.Conn) {
	defer conn.Close()
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			if _, werr := conn.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func prefixPipeHandler(prefix string) pipeHandler {
	return func(conn net.Conn) {
		defer conn.Close()
		buf := make([]byte, 4096)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				out := append([]byte(prefix), buf[:n]...)
				if _, werr := conn.Write(out); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}
}

func dialerForHandlers(handlers map[string]pipeHandler) dialContextFunc {
	return func(_ context.Context, _ string, address string) (net.Conn, error) {
		handler, ok := handlers[address]
		if !ok {
			return nil, fmt.Errorf("unexpected dial target %q", address)
		}
		clientConn, serverConn := net.Pipe()
		go handler(serverConn)
		return clientConn, nil
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

func startNodeLoop(t *testing.T, ctx context.Context, bus tunnelBus, cfg Config, outbound *clientRegistry, inbound *serverRegistry, dialer dialContextFunc) <-chan error {
	t.Helper()

	errCh := make(chan error, 1)
	go func() {
		errCh <- nodeReceiveLoop(ctx, bus, outbound, inbound, cfg, dialer)
	}()
	return errCh
}

func expectLoopExit(t *testing.T, errCh <-chan error) {
	t.Helper()
	select {
	case err := <-errCh:
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
			t.Fatalf("node loop: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("node loop did not exit")
	}
}

func runProxySession(t *testing.T, ctx context.Context, bus tunnelBus, sessions *clientRegistry, cfg Config, target string, payload []byte, wantLen int) []byte {
	t.Helper()

	appConn, proxyConn := net.Pipe()
	defer appConn.Close()
	defer proxyConn.Close()

	go handleClientConn(ctx, bus, sessions, proxyConn, cfg.ProxyEndpoint(), cfg.Routes, cfg.MaxFrameSize)
	performSOCKS5Handshake(t, appConn, target)

	writeErrCh := make(chan error, 1)
	go func() {
		_, err := appConn.Write(payload)
		writeErrCh <- err
	}()

	got := make([]byte, wantLen)
	if _, err := io.ReadFull(appConn, got); err != nil {
		t.Fatalf("read payload: %v", err)
	}
	if err := <-writeErrCh; err != nil {
		t.Fatalf("write payload: %v", err)
	}

	_ = appConn.Close()
	return got
}

func TestNodeTunnelLifecycleLargePayload(t *testing.T) {
	t.Parallel()

	nodeABus, nodeBBus := newTestBusPair()
	nodeAOutbound := newClientRegistry()
	nodeBOutbound := newClientRegistry()
	nodeAInbound := newServerRegistry()
	nodeBInbound := newServerRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	nodeAErr := startNodeLoop(t, ctx, nodeABus, testNodeA, nodeAOutbound, nodeAInbound, dialerForHandlers(map[string]pipeHandler{
		"127.0.0.1:22":  echoPipeHandler,
		"127.0.0.1:443": echoPipeHandler,
	}))
	nodeBErr := startNodeLoop(t, ctx, nodeBBus, testNodeB, nodeBOutbound, nodeBInbound, dialerForHandlers(map[string]pipeHandler{
		"127.0.0.1:22":  echoPipeHandler,
		"127.0.0.1:443": echoPipeHandler,
	}))

	payload := bytes.Repeat([]byte("0123456789abcdef"), 4096)
	got := runProxySession(t, ctx, nodeABus, nodeAOutbound, testNodeA, "10.0.0.168:22", payload, len(payload))
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch")
	}

	cancel()
	expectLoopExit(t, nodeAErr)
	expectLoopExit(t, nodeBErr)
}

func TestBidirectionalNodesCanServeEachOther(t *testing.T) {
	t.Parallel()

	nodeABus, nodeBBus := newTestBusPair()
	nodeAOutbound := newClientRegistry()
	nodeBOutbound := newClientRegistry()
	nodeAInbound := newServerRegistry()
	nodeBInbound := newServerRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	nodeAErr := startNodeLoop(t, ctx, nodeABus, testNodeA, nodeAOutbound, nodeAInbound, dialerForHandlers(map[string]pipeHandler{
		"127.0.0.1:22":  echoPipeHandler,
		"127.0.0.1:443": prefixPipeHandler("a-web:"),
	}))
	nodeBErr := startNodeLoop(t, ctx, nodeBBus, testNodeB, nodeBOutbound, nodeBInbound, dialerForHandlers(map[string]pipeHandler{
		"127.0.0.1:22":  echoPipeHandler,
		"127.0.0.1:443": prefixPipeHandler("b-web:"),
	}))

	sshPayload := []byte("ssh-check")
	if got := runProxySession(t, ctx, nodeABus, nodeAOutbound, testNodeA, "10.0.0.168:22", sshPayload, len(sshPayload)); !bytes.Equal(got, sshPayload) {
		t.Fatalf("A->B ssh payload mismatch: %q", got)
	}

	webPayload := []byte("https-check")
	want := append([]byte("a-web:"), webPayload...)
	if got := runProxySession(t, ctx, nodeBBus, nodeBOutbound, testNodeB, "10.0.0.167:443", webPayload, len(want)); !bytes.Equal(got, want) {
		t.Fatalf("B->A web payload mismatch: %q", got)
	}

	cancel()
	expectLoopExit(t, nodeAErr)
	expectLoopExit(t, nodeBErr)
}

func TestConcurrentSessionsIsolation(t *testing.T) {
	t.Parallel()

	nodeABus, nodeBBus := newTestBusPair()
	nodeAOutbound := newClientRegistry()
	nodeBOutbound := newClientRegistry()
	nodeAInbound := newServerRegistry()
	nodeBInbound := newServerRegistry()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	nodeAErr := startNodeLoop(t, ctx, nodeABus, testNodeA, nodeAOutbound, nodeAInbound, dialerForHandlers(map[string]pipeHandler{
		"127.0.0.1:22":  echoPipeHandler,
		"127.0.0.1:443": echoPipeHandler,
	}))
	nodeBErr := startNodeLoop(t, ctx, nodeBBus, testNodeB, nodeBOutbound, nodeBInbound, dialerForHandlers(map[string]pipeHandler{
		"127.0.0.1:22":  prefixPipeHandler("left:"),
		"127.0.0.1:443": prefixPipeHandler("right:"),
	}))

	type sessionResult struct {
		name string
		got  []byte
		err  error
	}

	runSession := func(name, target string, payload []byte, wantLen int) sessionResult {
		defer func() {
			if r := recover(); r != nil {
				panic(r)
			}
		}()
		appConn, proxyConn := net.Pipe()
		defer appConn.Close()
		defer proxyConn.Close()

		go handleClientConn(ctx, nodeABus, nodeAOutbound, proxyConn, testNodeA.ProxyEndpoint(), testNodeA.Routes, testNodeA.MaxFrameSize)
		if err := handshakeSOCKS5(appConn, target); err != nil {
			return sessionResult{err: fmt.Errorf("handshake: %w", err)}
		}
		writeErrCh := make(chan error, 1)
		go func() {
			_, err := appConn.Write(payload)
			writeErrCh <- err
		}()
		got := make([]byte, wantLen)
		if _, err := io.ReadFull(appConn, got); err != nil {
			return sessionResult{err: fmt.Errorf("read payload: %w", err)}
		}
		if err := <-writeErrCh; err != nil {
			return sessionResult{err: fmt.Errorf("write payload: %w", err)}
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
		results <- runSession("left", "10.0.0.168:22", leftPayload, len("left:")+len(leftPayload))
	}()
	go func() {
		defer wg.Done()
		results <- runSession("right", "10.0.0.168:443", rightPayload, len("right:")+len(rightPayload))
	}()

	wg.Wait()
	close(results)

	for result := range results {
		if result.err != nil {
			t.Fatalf("%s session failed: %v", result.name, result.err)
		}
		switch result.name {
		case "left":
			want := append([]byte("left:"), leftPayload...)
			if !bytes.Equal(result.got, want) {
				t.Fatalf("left payload mismatch: %q", result.got)
			}
		case "right":
			want := append([]byte("right:"), rightPayload...)
			if !bytes.Equal(result.got, want) {
				t.Fatalf("right payload mismatch: %q", result.got)
			}
		}
	}

	cancel()
	expectLoopExit(t, nodeAErr)
	expectLoopExit(t, nodeBErr)
}
