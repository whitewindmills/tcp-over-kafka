package tunnel

import (
	"bytes"
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"tcp-over-kafka/internal/frame"
)

type memBus struct {
	send chan frame.Frame
	recv chan frame.Frame
}

func (b *memBus) Send(ctx context.Context, f frame.Frame) error {
	select {
	case b.send <- f:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *memBus) Receive(ctx context.Context) (frame.Frame, error) {
	select {
	case f := <-b.recv:
		return f, nil
	case <-ctx.Done():
		return frame.Frame{}, ctx.Err()
	}
}

func newMemBusPair() (*memBus, *memBus) {
	c2s := make(chan frame.Frame, 128)
	s2c := make(chan frame.Frame, 128)
	return &memBus{send: c2s, recv: s2c}, &memBus{send: s2c, recv: c2s}
}

func startServer(ctx context.Context, bus *memBus, targetConn net.Conn, maxFrame int) <-chan error {
	done := make(chan error, 1)
	var once sync.Once
	report := func(err error) {
		once.Do(func() {
			done <- err
		})
	}
	ready := make(chan struct{})
	go func() {
		go func() {
			select {
			case <-ready:
			case <-ctx.Done():
				return
			}
			buf := make([]byte, maxFrame)
			for {
				n, err := targetConn.Read(buf)
				if n > 0 {
					payload := append([]byte(nil), buf[:n]...)
					_ = bus.Send(ctx, frame.Frame{
						Kind:         frame.KindData,
						SessionID:    "sess",
						ConnectionID: "conn",
						TargetAddr:   "target",
						Payload:      payload,
					})
				}
				if err != nil {
					if !isExpectedStreamClose(err) {
						report(err)
					}
					_ = bus.Send(context.Background(), frame.Frame{
						Kind:         frame.KindClose,
						SessionID:    "sess",
						ConnectionID: "conn",
						TargetAddr:   "target",
					})
					_ = targetConn.Close()
					return
				}
			}
		}()

		for {
			f, err := bus.Receive(ctx)
			if err != nil {
				report(err)
				_ = targetConn.Close()
				return
			}
			switch f.Kind {
			case frame.KindOpen:
				_ = bus.Send(ctx, frame.Frame{
					Kind:         frame.KindOpenAck,
					SessionID:    f.SessionID,
					ConnectionID: f.ConnectionID,
				})
			case frame.KindReady:
				select {
				case <-ready:
				default:
					close(ready)
				}
			case frame.KindData:
				if len(f.Payload) > 0 {
					if err := writeAll(targetConn, f.Payload); err != nil {
						report(err)
						_ = targetConn.Close()
						return
					}
				}
			case frame.KindClose, frame.KindError:
				_ = targetConn.Close()
				report(nil)
				return
			}
		}
	}()
	return done
}

func startTargetEcho(ctx context.Context, conn net.Conn) <-chan error {
	done := make(chan error, 1)
	var once sync.Once
	report := func(err error) {
		once.Do(func() {
			done <- err
		})
	}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				if werr := writeAll(conn, buf[:n]); werr != nil {
					report(werr)
					return
				}
			}
			if err != nil {
				if !isExpectedStreamClose(err) && ctx.Err() == nil {
					report(err)
					return
				}
				report(nil)
				return
			}
		}
	}()
	return done
}

func recvFrame(t *testing.T, ctx context.Context, bus *memBus) frame.Frame {
	t.Helper()
	f, err := bus.Receive(ctx)
	if err != nil {
		t.Fatalf("receive frame: %v", err)
	}
	return f
}

func sendOpenReady(t *testing.T, ctx context.Context, bus *memBus) {
	t.Helper()
	if err := bus.Send(ctx, frame.Frame{
		Kind:         frame.KindOpen,
		SessionID:    "sess",
		ConnectionID: "conn",
		TargetAddr:   "target",
	}); err != nil {
		t.Fatalf("send open: %v", err)
	}
	f := recvFrame(t, ctx, bus)
	if f.Kind != frame.KindOpenAck {
		t.Fatalf("expected open-ack, got %v", f.Kind)
	}
	if err := bus.Send(ctx, frame.Frame{
		Kind:         frame.KindReady,
		SessionID:    "sess",
		ConnectionID: "conn",
		TargetAddr:   "target",
	}); err != nil {
		t.Fatalf("send ready: %v", err)
	}
}

func TestLifecycleFlowInMemory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	clientBus, serverBus := newMemBusPair()
	serverConn, targetConn := net.Pipe()
	defer serverConn.Close()
	defer targetConn.Close()

	targetDone := startTargetEcho(ctx, targetConn)
	serverDone := startServer(ctx, serverBus, serverConn, 2048)

	sendOpenReady(t, ctx, clientBus)
	if err := clientBus.Send(ctx, frame.Frame{
		Kind:         frame.KindData,
		SessionID:    "sess",
		ConnectionID: "conn",
		Payload:      []byte("ping"),
	}); err != nil {
		t.Fatalf("send data: %v", err)
	}

	resp := recvFrame(t, ctx, clientBus)
	if resp.Kind != frame.KindData {
		t.Fatalf("expected data, got %v", resp.Kind)
	}
	if string(resp.Payload) != "ping" {
		t.Fatalf("unexpected payload: %q", resp.Payload)
	}

	if err := clientBus.Send(ctx, frame.Frame{
		Kind:         frame.KindClose,
		SessionID:    "sess",
		ConnectionID: "conn",
	}); err != nil {
		t.Fatalf("send close: %v", err)
	}

	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatalf("server error: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("server timeout: %v", ctx.Err())
	}

	select {
	case err := <-targetDone:
		if err != nil {
			t.Fatalf("target error: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("target timeout: %v", ctx.Err())
	}
}

func TestLargePayloadRelayInMemory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	clientBus, serverBus := newMemBusPair()
	serverConn, targetConn := net.Pipe()
	defer serverConn.Close()
	defer targetConn.Close()

	targetDone := startTargetEcho(ctx, targetConn)
	serverDone := startServer(ctx, serverBus, serverConn, 4096)

	sendOpenReady(t, ctx, clientBus)

	payload := make([]byte, 256*1024)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	for off := 0; off < len(payload); off += 3000 {
		end := off + 3000
		if end > len(payload) {
			end = len(payload)
		}
		if err := clientBus.Send(ctx, frame.Frame{
			Kind:         frame.KindData,
			SessionID:    "sess",
			ConnectionID: "conn",
			Payload:      append([]byte(nil), payload[off:end]...),
		}); err != nil {
			t.Fatalf("send data chunk: %v", err)
		}
	}

	var got []byte
	for len(got) < len(payload) {
		resp := recvFrame(t, ctx, clientBus)
		if resp.Kind != frame.KindData {
			continue
		}
		got = append(got, resp.Payload...)
	}

	if !bytes.Equal(got[:len(payload)], payload) {
		t.Fatalf("payload mismatch: got %d bytes", len(got))
	}

	if err := clientBus.Send(ctx, frame.Frame{
		Kind:         frame.KindClose,
		SessionID:    "sess",
		ConnectionID: "conn",
	}); err != nil {
		t.Fatalf("send close: %v", err)
	}

	select {
	case err := <-serverDone:
		if err != nil && err != context.Canceled {
			t.Fatalf("server error: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("server timeout: %v", ctx.Err())
	}

	select {
	case err := <-targetDone:
		if err != nil && err != context.Canceled {
			t.Fatalf("target error: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("target timeout: %v", ctx.Err())
	}
}
