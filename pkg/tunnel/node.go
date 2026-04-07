package tunnel

import (
	"context"
	"errors"
	"net"
	"time"

	"k8s.io/klog/v2"

	"tcp-over-kafka/pkg/frame"
)

var nodeReceiveRetryDelay = time.Second

type busFactory func(broker, topic, group string) tunnelBus
type listenFunc func(network, address string) (net.Listener, error)

type nodeDeps struct {
	newBus      busFactory
	listen      listenFunc
	dialContext dialContextFunc
}

func defaultDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, address)
}

func defaultNodeDeps() nodeDeps {
	return nodeDeps{
		newBus: func(broker, topic, group string) tunnelBus {
			return NewBus(broker, topic, group)
		},
		listen:      net.Listen,
		dialContext: defaultDialContext,
	}
}

// RunNode starts one symmetric tunnel node that both originates outbound proxy
// sessions and exposes registered local services.
func RunNode(ctx context.Context, cfg Config) error {
	return runNode(ctx, cfg, defaultNodeDeps())
}

func runNode(ctx context.Context, cfg Config, deps nodeDeps) error {
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return err
	}

	bus := deps.newBus(cfg.Broker, cfg.Topic, cfg.ConsumerGroup())
	defer bus.Close()

	ln, err := deps.listen("tcp", cfg.ListenAddr)
	if err != nil {
		return err
	}
	defer ln.Close()

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	self := cfg.ProxyEndpoint()
	outboundSessions := newClientRegistry()
	inboundSessions := newServerRegistry()

	klog.Infof(
		"Node listening on %s, topic=%s, broker=%s, nid=%s, group=%s",
		cfg.ListenAddr,
		cfg.Topic,
		cfg.Broker,
		cfg.NID,
		cfg.ConsumerGroup(),
	)

	receiveErr := make(chan error, 1)
	go func() {
		receiveErr <- nodeReceiveLoop(ctx, bus, outboundSessions, inboundSessions, cfg, deps.dialContext)
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case recvErr := <-receiveErr:
				if recvErr == nil || errors.Is(recvErr, context.Canceled) || ctx.Err() != nil {
					return nil
				}
				return recvErr
			case <-ctx.Done():
				return nil
			default:
				if errors.Is(err, net.ErrClosed) {
					return nil
				}
				return err
			}
		}
		go handleClientConn(ctx, bus, outboundSessions, conn, self, cfg.Routes, cfg.MaxFrameSize)
	}
}

func nodeReceiveLoop(
	ctx context.Context,
	bus tunnelBus,
	outboundSessions *clientRegistry,
	inboundSessions *serverRegistry,
	cfg Config,
	dialContext dialContextFunc,
) error {
	for {
		f, err := bus.Receive(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			klog.Errorf("Receive failed, retrying: %v", err)
			timer := time.NewTimer(nodeReceiveRetryDelay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil
			case <-timer.C:
				continue
			}
		}
		if f.DestinationNID != cfg.NID {
			continue
		}

		switch f.Kind {
		case frame.KindOpen:
			if err := serverOpenSession(ctx, bus, inboundSessions, cfg.NID, cfg.Services, cfg.MaxFrameSize, dialContext, f); err != nil {
				klog.Errorf("Open session failed: %v", err)
				_ = sendErrorFrame(
					context.Background(),
					bus,
					Endpoint{NID: cfg.NID, EID: f.DestinationEID},
					Endpoint{NID: f.SourceNID, EID: f.SourceEID},
					f.ConnectionID,
					err.Error(),
				)
			}
		case frame.KindOpenAck:
			s := outboundSessions.getFrame(f)
			if s == nil {
				continue
			}
			if err := s.onOpenAck(ctx, bus); err != nil {
				klog.Errorf("Open-ack handling failed: %v", err)
				s.close()
				outboundSessions.removeFrame(f)
			}
		case frame.KindReady:
			s := inboundSessions.getFrame(f)
			if s == nil {
				continue
			}
			s.startOutbound(ctx, bus, inboundSessions, cfg.MaxFrameSize)
		case frame.KindData:
			outboundSession := outboundSessions.getFrame(f)
			inboundSession := inboundSessions.getFrame(f)
			switch {
			case outboundSession != nil:
				s := outboundSession
				if len(f.Payload) == 0 {
					continue
				}
				if err := writeAll(s.conn, f.Payload); err != nil {
					klog.Errorf("Outbound write failed: %v", err)
					s.close()
					outboundSessions.removeFrame(f)
				}
			case inboundSession != nil:
				s := inboundSession
				if len(f.Payload) == 0 {
					continue
				}
				if err := writeAll(s.conn, f.Payload); err != nil {
					klog.Errorf("Inbound write failed: %v", err)
					s.close()
					inboundSessions.removeFrame(f)
				}
			}
		case frame.KindClose, frame.KindError:
			if s := outboundSessions.getFrame(f); s != nil {
				if f.Err != "" {
					klog.Errorf("Remote error for %s: %s", frameConversationKey(f), f.Err)
				}
				if err := s.onRemoteClose(); err != nil {
					klog.Errorf("Outbound close reply failed: %v", err)
				}
				s.close()
				outboundSessions.removeFrame(f)
				continue
			}
			if s := inboundSessions.getFrame(f); s != nil {
				s.close()
				inboundSessions.removeFrame(f)
			}
		default:
			klog.Warningf("Unknown frame kind: %d", f.Kind)
		}
	}
}

func sendErrorFrame(ctx context.Context, bus tunnelBus, source, destination Endpoint, connectionID, errText string) error {
	return bus.Send(ctx, frame.Frame{
		Kind:           frame.KindError,
		SourceNID:      source.NID,
		SourceEID:      source.EID,
		DestinationNID: destination.NID,
		DestinationEID: destination.EID,
		ConnectionID:   connectionID,
		Err:            errText,
	})
}
