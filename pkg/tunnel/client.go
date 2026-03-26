package tunnel

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"k8s.io/klog/v2"

	"tcp-over-kafka/pkg/frame"
	"tcp-over-kafka/pkg/socks5"
)

// clientSession tracks one proxied TCP connection and its Kafka state.
type clientSession struct {
	self          Endpoint
	peer          Endpoint
	connectionID  string
	conn          net.Conn
	ready         chan struct{}
	closed        chan struct{}
	once          sync.Once
	handshakeMu   sync.Mutex
	handshakeDone bool
}

// markReady unblocks the outbound pump after the SOCKS5 success reply is sent.
func (s *clientSession) markReady() {
	s.once.Do(func() { close(s.ready) })
}

// close tears down the SOCKS5 socket and marks the session closed once.
func (s *clientSession) close() {
	select {
	case <-s.closed:
		return
	default:
		close(s.closed)
		_ = s.conn.Close()
	}
}

func (s *clientSession) frame(kind frame.Kind) frame.Frame {
	return frame.Frame{
		Kind:                  kind,
		SourcePlatformID:      s.self.PlatformID,
		SourceDeviceID:        s.self.DeviceID,
		DestinationPlatformID: s.peer.PlatformID,
		DestinationDeviceID:   s.peer.DeviceID,
		ConnectionID:          s.connectionID,
	}
}

// clientPumpOutbound streams local bytes into Kafka after the session is ready.
func clientPumpOutbound(ctx context.Context, bus tunnelBus, sessions *clientRegistry, s *clientSession, maxFrame int) {
	defer sessions.remove(s.self, s.peer, s.connectionID)

	wait := time.NewTimer(10 * time.Second)
	select {
	case <-s.ready:
	case <-s.closed:
		wait.Stop()
		return
	case <-ctx.Done():
		wait.Stop()
		if err := s.onRemoteClose(); err != nil {
			klog.Errorf("Client shutdown reply failed: %v", err)
		}
		_ = bus.Send(context.Background(), s.frame(frame.KindClose))
		s.close()
		return
	case <-wait.C:
		klog.Warningf("Session %s did not open in time", conversationKey(s.self, s.peer, s.connectionID))
		if err := s.onRemoteClose(); err != nil {
			klog.Errorf("Client timeout reply failed: %v", err)
		}
		_ = bus.Send(context.Background(), s.frame(frame.KindClose))
		s.close()
		return
	}
	wait.Stop()

	buf := make([]byte, maxFrame)
	for {
		n, err := s.conn.Read(buf)
		if n > 0 {
			payload := append([]byte(nil), buf[:n]...)
			msg := s.frame(frame.KindData)
			msg.Payload = payload
			if err := bus.Send(ctx, msg); err != nil {
				klog.Errorf("Send data failed: %v", err)
				_ = bus.Send(context.Background(), s.frame(frame.KindClose))
				s.close()
				return
			}
		}
		if err != nil {
			if !isExpectedStreamClose(err) {
				klog.Errorf("Local read failed: %v", err)
			}
			_ = bus.Send(context.Background(), s.frame(frame.KindClose))
			s.close()
			return
		}
	}
}

// handleClientConn accepts the SOCKS5 CONNECT request and opens an outbound tunnel session.
func handleClientConn(ctx context.Context, bus tunnelBus, sessions *clientRegistry, conn net.Conn, self Endpoint, routes map[string]Endpoint, maxFrameSize int) {
	targetAddr, err := socks5.Accept(conn)
	if err != nil {
		klog.Errorf("SOCKS5 handshake failed: %v", err)
		var reqErr *socks5.RequestError
		if errors.As(err, &reqErr) {
			_ = socks5.WriteReply(conn, reqErr.ReplyCode())
		}
		_ = conn.Close()
		return
	}

	dest, ok := resolveClientRoute(routes, targetAddr)
	if !ok {
		klog.Warningf("Route miss for target %s", targetAddr)
		_ = socks5.WriteReply(conn, socks5.ReplyGeneralFailure)
		_ = conn.Close()
		return
	}

	s := &clientSession{
		self:   self,
		peer:   dest,
		conn:   conn,
		ready:  make(chan struct{}),
		closed: make(chan struct{}),
	}
	connectionID, err := randomID()
	if err != nil {
		klog.Errorf("Connection ID generation failed: %v", err)
		_ = socks5.WriteReply(conn, socks5.ReplyGeneralFailure)
		_ = conn.Close()
		return
	}
	s.connectionID = connectionID
	sessions.add(s)
	openFrame := s.frame(frame.KindOpen)
	if err := bus.Send(ctx, openFrame); err != nil {
		klog.Errorf("Open send failed: %v", err)
		_ = socks5.WriteReply(conn, socks5.ReplyGeneralFailure)
		s.close()
		sessions.remove(s.self, s.peer, s.connectionID)
		return
	}
	go clientPumpOutbound(ctx, bus, sessions, s, maxFrameSize)
}

// onOpenAck sends the SOCKS5 success reply and then marks the session ready.
func (s *clientSession) onOpenAck(ctx context.Context, bus tunnelBus) error {
	s.handshakeMu.Lock()
	if s.handshakeDone {
		s.handshakeMu.Unlock()
		return nil
	}
	s.handshakeMu.Unlock()

	if err := socks5.WriteReply(s.conn, socks5.ReplySucceeded); err != nil {
		return err
	}
	s.handshakeMu.Lock()
	s.handshakeDone = true
	s.handshakeMu.Unlock()
	if err := bus.Send(ctx, s.frame(frame.KindReady)); err != nil {
		return err
	}
	s.markReady()
	return nil
}

// onRemoteClose mirrors a remote close back to the SOCKS5 client when needed.
func (s *clientSession) onRemoteClose() error {
	s.handshakeMu.Lock()
	done := s.handshakeDone
	s.handshakeMu.Unlock()
	if done {
		return nil
	}
	return socks5.WriteReply(s.conn, socks5.ReplyGeneralFailure)
}

// clientRegistry is the in-memory lookup table for active client sessions.
type clientRegistry struct {
	mu sync.RWMutex
	m  map[string]*clientSession
}

// newClientRegistry creates the session registry.
func newClientRegistry() *clientRegistry {
	return &clientRegistry{m: make(map[string]*clientSession)}
}

// key combines both peers and the connection ID into a stable map key.
func (r *clientRegistry) key(self, peer Endpoint, connectionID string) string {
	return conversationKey(self, peer, connectionID)
}

// add stores a session in the registry.
func (r *clientRegistry) add(s *clientSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[r.key(s.self, s.peer, s.connectionID)] = s
}

// get looks up a session by peer identities and connection ID.
func (r *clientRegistry) get(self, peer Endpoint, connectionID string) *clientSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.m[r.key(self, peer, connectionID)]
}

// getFrame looks up a session by frame identity.
func (r *clientRegistry) getFrame(f frame.Frame) *clientSession {
	return r.get(
		Endpoint{PlatformID: f.SourcePlatformID, DeviceID: f.SourceDeviceID},
		Endpoint{PlatformID: f.DestinationPlatformID, DeviceID: f.DestinationDeviceID},
		f.ConnectionID,
	)
}

// remove deletes a session from the registry.
func (r *clientRegistry) remove(self, peer Endpoint, connectionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.m, r.key(self, peer, connectionID))
}

// removeFrame deletes a session using a frame identity.
func (r *clientRegistry) removeFrame(f frame.Frame) {
	r.remove(
		Endpoint{PlatformID: f.SourcePlatformID, DeviceID: f.SourceDeviceID},
		Endpoint{PlatformID: f.DestinationPlatformID, DeviceID: f.DestinationDeviceID},
		f.ConnectionID,
	)
}

// randomID generates a short random connection identifier.
func randomID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// writeAll keeps writing until the whole payload is flushed to the connection.
func writeAll(w io.Writer, payload []byte) error {
	for len(payload) > 0 {
		n, err := w.Write(payload)
		if err != nil {
			return err
		}
		payload = payload[n:]
	}
	return nil
}
