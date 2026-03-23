package tunnel

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"

	"tcp-over-kafka/internal/frame"
)

// serverSession tracks one remote TCP connection opened on behalf of a client.
type serverSession struct {
	sessionID    string
	connectionID string
	targetAddr   string
	conn         net.Conn
	closed       chan struct{}
	once         sync.Once
	pumpOnce     sync.Once
	needsReady   bool
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

// RunServer consumes tunnel frames and proxies them to the requested target.
func RunServer(ctx context.Context, cfg ServerConfig) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	bus := NewBus(cfg.Broker, topicS2C(cfg.TunnelID), topicC2S(cfg.TunnelID), cfg.ServerGroup)
	defer bus.Close()

	log.Printf("server consuming tunnel=%s, broker=%s", cfg.TunnelID, cfg.Broker)
	sessions := newServerRegistry()

	for {
		f, err := bus.Receive(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		switch f.Kind {
		case frame.KindOpen:
			if err := serverOpenSession(ctx, bus, sessions, cfg.TargetAddr, cfg.MaxFrameSize, f); err != nil {
				log.Printf("open session failed: %v", err)
				_ = bus.Send(context.Background(), frame.Frame{
					Kind:         frame.KindError,
					SessionID:    f.SessionID,
					ConnectionID: f.ConnectionID,
					Err:          err.Error(),
				})
			}
		case frame.KindReady:
			if sess := sessions.get(f.SessionID, f.ConnectionID); sess != nil {
				sess.startOutbound(ctx, bus, sessions, cfg.MaxFrameSize)
			}
		case frame.KindData:
			if sess := sessions.get(f.SessionID, f.ConnectionID); sess != nil {
				if len(f.Payload) > 0 {
					if err := writeAll(sess.conn, f.Payload); err != nil {
						log.Printf("server write failed: %v", err)
						sess.close()
						sessions.remove(f.SessionID, f.ConnectionID)
					}
				}
			}
		case frame.KindClose, frame.KindError:
			if sess := sessions.get(f.SessionID, f.ConnectionID); sess != nil {
				sess.close()
				sessions.remove(f.SessionID, f.ConnectionID)
			}
		default:
			log.Printf("unknown frame kind: %d", f.Kind)
		}
	}
}

// serverOpenSession dials the target socket and registers a new server session.
func serverOpenSession(ctx context.Context, bus *Bus, sessions *serverRegistry, defaultTarget string, maxFrame int, f frame.Frame) error {
	if sessions.get(f.SessionID, f.ConnectionID) != nil {
		return nil
	}
	targetAddr := f.TargetAddr
	if targetAddr == "" {
		targetAddr = defaultTarget
	}
	if targetAddr == "" {
		return fmt.Errorf("missing target address")
	}
	conn, err := net.Dial("tcp", targetAddr)
	if err != nil {
		return fmt.Errorf("dial target %s: %w", targetAddr, err)
	}
	sess := &serverSession{
		sessionID:    f.SessionID,
		connectionID: f.ConnectionID,
		targetAddr:   targetAddr,
		conn:         conn,
		closed:       make(chan struct{}),
		needsReady:   f.TargetAddr != "",
	}
	sessions.add(sess)
	if err := bus.Send(ctx, frame.Frame{
		Kind:         frame.KindOpenAck,
		SessionID:    f.SessionID,
		ConnectionID: f.ConnectionID,
	}); err != nil {
		sess.close()
		sessions.remove(f.SessionID, f.ConnectionID)
		return err
	}
	if !sess.needsReady {
		sess.startOutbound(ctx, bus, sessions, maxFrame)
	}
	return nil
}

// startOutbound starts the goroutine that copies target bytes back into Kafka.
func (s *serverSession) startOutbound(ctx context.Context, bus *Bus, sessions *serverRegistry, maxFrame int) {
	s.pumpOnce.Do(func() {
		go serverPumpOutbound(ctx, bus, sessions, s, maxFrame)
	})
}

// serverPumpOutbound copies target bytes to the client until the socket closes.
func serverPumpOutbound(ctx context.Context, bus *Bus, sessions *serverRegistry, sess *serverSession, maxFrame int) {
	defer sessions.remove(sess.sessionID, sess.connectionID)

	if maxFrame <= 0 {
		maxFrame = 32 * 1024
	}
	buf := make([]byte, maxFrame)
	for {
		n, err := sess.conn.Read(buf)
		if n > 0 {
			payload := append([]byte(nil), buf[:n]...)
			if err := bus.Send(ctx, frame.Frame{
				Kind:         frame.KindData,
				SessionID:    sess.sessionID,
				ConnectionID: sess.connectionID,
				TargetAddr:   sess.targetAddr,
				Payload:      payload,
			}); err != nil {
				log.Printf("server send failed: %v", err)
				sess.close()
				return
			}
		}
		if err != nil {
			if !isExpectedStreamClose(err) {
				log.Printf("target read failed: %v", err)
			}
			_ = bus.Send(context.Background(), frame.Frame{
				Kind:         frame.KindClose,
				SessionID:    sess.sessionID,
				ConnectionID: sess.connectionID,
				TargetAddr:   sess.targetAddr,
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

// key combines session and connection IDs into a stable map key.
func (r *serverRegistry) key(sessionID, connectionID string) string {
	return sessionID + "/" + connectionID
}

// add stores a session in the registry.
func (r *serverRegistry) add(s *serverSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[r.key(s.sessionID, s.connectionID)] = s
}

// get looks up a session by IDs.
func (r *serverRegistry) get(sessionID, connectionID string) *serverSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.m[r.key(sessionID, connectionID)]
}

// remove deletes a session from the registry.
func (r *serverRegistry) remove(sessionID, connectionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.m, r.key(sessionID, connectionID))
}
