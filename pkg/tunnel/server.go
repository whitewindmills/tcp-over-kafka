package tunnel

import (
	"context"
	"fmt"
	"net"
	"sync"

	"k8s.io/klog/v2"

	"tcp-over-kafka/pkg/frame"
)

type dialContextFunc func(context.Context, string, string) (net.Conn, error)

// serverSession tracks one remote TCP connection opened on behalf of a client.
type serverSession struct {
	client       Endpoint
	service      Endpoint
	connectionID string
	conn         net.Conn
	closed       chan struct{}
	once         sync.Once
	pumpOnce     sync.Once
}

// close tears down the remote target socket exactly once.
func (s *serverSession) close() {
	select {
	case <-s.closed:
		return
	default:
		s.once.Do(func() { close(s.closed) })
		_ = s.conn.Close()
	}
}

// serverOpenSession dials the registered local target and registers one inbound session.
func serverOpenSession(ctx context.Context, bus tunnelBus, sessions *serverRegistry, platformID string, services map[string]string, maxFrame int, dialContext dialContextFunc, f frame.Frame) error {
	if sessions.getFrame(f) != nil {
		return nil
	}
	targetAddr, ok := resolveServerService(services, f.DestinationDeviceID)
	if !ok {
		return fmt.Errorf("unknown destination: %q", f.DestinationDeviceID)
	}
	conn, err := dialContext(ctx, "tcp", targetAddr)
	if err != nil {
		return fmt.Errorf("dial target %s: %w", targetAddr, err)
	}
	sess := &serverSession{
		client:       Endpoint{PlatformID: f.SourcePlatformID, DeviceID: f.SourceDeviceID},
		service:      Endpoint{PlatformID: platformID, DeviceID: f.DestinationDeviceID},
		connectionID: f.ConnectionID,
		conn:         conn,
		closed:       make(chan struct{}),
	}
	sessions.add(sess)
	if err := bus.Send(ctx, frame.Frame{
		Kind:                  frame.KindOpenAck,
		SourcePlatformID:      sess.service.PlatformID,
		SourceDeviceID:        sess.service.DeviceID,
		DestinationPlatformID: sess.client.PlatformID,
		DestinationDeviceID:   sess.client.DeviceID,
		ConnectionID:          f.ConnectionID,
	}); err != nil {
		sess.close()
		sessions.remove(sess.client, sess.service, f.ConnectionID)
		return err
	}
	return nil
}

// startOutbound starts the goroutine that copies target bytes back into Kafka.
func (s *serverSession) startOutbound(ctx context.Context, bus tunnelBus, sessions *serverRegistry, maxFrame int) {
	s.pumpOnce.Do(func() {
		go serverPumpOutbound(ctx, bus, sessions, s, maxFrame)
	})
}

// serverPumpOutbound copies target bytes to the client until the socket closes.
func serverPumpOutbound(ctx context.Context, bus tunnelBus, sessions *serverRegistry, sess *serverSession, maxFrame int) {
	defer sessions.remove(sess.client, sess.service, sess.connectionID)

	if maxFrame <= 0 {
		maxFrame = 32 * 1024
	}
	buf := make([]byte, maxFrame)
	for {
		n, err := sess.conn.Read(buf)
		if n > 0 {
			payload := append([]byte(nil), buf[:n]...)
			if err := bus.Send(ctx, frame.Frame{
				Kind:                  frame.KindData,
				SourcePlatformID:      sess.service.PlatformID,
				SourceDeviceID:        sess.service.DeviceID,
				DestinationPlatformID: sess.client.PlatformID,
				DestinationDeviceID:   sess.client.DeviceID,
				ConnectionID:          sess.connectionID,
				Payload:               payload,
			}); err != nil {
				klog.Errorf("Server send failed: %v", err)
				sess.close()
				return
			}
		}
		if err != nil {
			if !isExpectedStreamClose(err) {
				klog.Errorf("Target read failed: %v", err)
			}
			_ = bus.Send(context.Background(), frame.Frame{
				Kind:                  frame.KindClose,
				SourcePlatformID:      sess.service.PlatformID,
				SourceDeviceID:        sess.service.DeviceID,
				DestinationPlatformID: sess.client.PlatformID,
				DestinationDeviceID:   sess.client.DeviceID,
				ConnectionID:          sess.connectionID,
			})
			sess.close()
			return
		}
	}
}

// serverRegistry is the in-memory lookup table for active server sessions.
type serverRegistry struct {
	mu sync.RWMutex
	m  map[string]*serverSession
}

// newServerRegistry creates the session registry.
func newServerRegistry() *serverRegistry {
	return &serverRegistry{m: make(map[string]*serverSession)}
}

// key combines both peers and the connection ID into a stable map key.
func (r *serverRegistry) key(client, service Endpoint, connectionID string) string {
	return conversationKey(client, service, connectionID)
}

// add stores a session in the registry.
func (r *serverRegistry) add(s *serverSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[r.key(s.client, s.service, s.connectionID)] = s
}

// get looks up a session by peer identities and connection ID.
func (r *serverRegistry) get(client, service Endpoint, connectionID string) *serverSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.m[r.key(client, service, connectionID)]
}

// getFrame looks up a session by frame identity.
func (r *serverRegistry) getFrame(f frame.Frame) *serverSession {
	return r.get(
		Endpoint{PlatformID: f.SourcePlatformID, DeviceID: f.SourceDeviceID},
		Endpoint{PlatformID: f.DestinationPlatformID, DeviceID: f.DestinationDeviceID},
		f.ConnectionID,
	)
}

// remove deletes a session from the registry.
func (r *serverRegistry) remove(client, service Endpoint, connectionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.m, r.key(client, service, connectionID))
}

// removeFrame deletes a session using a frame identity.
func (r *serverRegistry) removeFrame(f frame.Frame) {
	r.remove(
		Endpoint{PlatformID: f.SourcePlatformID, DeviceID: f.SourceDeviceID},
		Endpoint{PlatformID: f.DestinationPlatformID, DeviceID: f.DestinationDeviceID},
		f.ConnectionID,
	)
}
