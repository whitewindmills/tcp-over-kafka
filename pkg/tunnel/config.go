package tunnel

import (
	"fmt"
	"strings"
)

// ClientConfig controls the SOCKS5 listener side of the tunnel.
type ClientConfig struct {
	ListenAddr   string
	Broker       string
	Topic        string
	ClientGroup  string
	PlatformID   string
	DeviceID     string
	Routes       map[string]Endpoint
	MaxFrameSize int
}

// ServerConfig controls the remote relay side of the tunnel.
type ServerConfig struct {
	Broker       string
	Topic        string
	ServerGroup  string
	PlatformID   string
	Services     map[string]string
	MaxFrameSize int
}

// validate ensures the client config has the minimum required fields.
func (c ClientConfig) validate() error {
	if strings.TrimSpace(c.ListenAddr) == "" {
		return fmt.Errorf("missing client listen address")
	}
	if strings.TrimSpace(c.Broker) == "" {
		return fmt.Errorf("missing client broker address")
	}
	if strings.TrimSpace(c.Topic) == "" {
		return fmt.Errorf("missing client topic")
	}
	if strings.TrimSpace(c.ClientGroup) == "" {
		return fmt.Errorf("missing client consumer group")
	}
	if err := (Endpoint{PlatformID: c.PlatformID, DeviceID: c.DeviceID}).validate(); err != nil {
		return fmt.Errorf("invalid client identity: %w", err)
	}
	if len(c.Routes) == 0 {
		return fmt.Errorf("missing client routes")
	}
	if c.MaxFrameSize <= 0 {
		return fmt.Errorf("max frame size must be positive")
	}
	return nil
}

// validate ensures the server config has the minimum required fields.
func (c ServerConfig) validate() error {
	if strings.TrimSpace(c.Broker) == "" {
		return fmt.Errorf("missing server broker address")
	}
	if strings.TrimSpace(c.Topic) == "" {
		return fmt.Errorf("missing server topic")
	}
	if strings.TrimSpace(c.ServerGroup) == "" {
		return fmt.Errorf("missing server consumer group")
	}
	if strings.TrimSpace(c.PlatformID) == "" {
		return fmt.Errorf("missing server platform ID")
	}
	if len(c.Services) == 0 {
		return fmt.Errorf("missing server service mappings")
	}
	if c.MaxFrameSize <= 0 {
		return fmt.Errorf("max frame size must be positive")
	}
	return nil
}
