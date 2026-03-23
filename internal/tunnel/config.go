package tunnel

import "fmt"

// ClientConfig controls the SOCKS5 listener side of the tunnel.
type ClientConfig struct {
	ListenAddr   string
	Broker       string
	TunnelID     string
	ClientGroup  string
	MaxFrameSize int
}

// ServerConfig controls the remote relay side of the tunnel.
type ServerConfig struct {
	Broker       string
	TunnelID     string
	ServerGroup  string
	TargetAddr   string
	MaxFrameSize int
}

// validate ensures the client config has the minimum required fields.
func (c ClientConfig) validate() error {
	if c.ListenAddr == "" || c.Broker == "" || c.TunnelID == "" || c.ClientGroup == "" {
		return fmt.Errorf("missing required client config")
	}
	if c.MaxFrameSize <= 0 {
		return fmt.Errorf("max frame size must be positive")
	}
	return nil
}

// validate ensures the server config has the minimum required fields.
func (c ServerConfig) validate() error {
	if c.Broker == "" || c.TunnelID == "" || c.ServerGroup == "" {
		return fmt.Errorf("missing required server config")
	}
	if c.MaxFrameSize <= 0 {
		return fmt.Errorf("max frame size must be positive")
	}
	return nil
}
