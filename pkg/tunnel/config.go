package tunnel

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
)

const (
	// DefaultMaxFrameSize bounds the payload size copied into one Kafka frame.
	DefaultMaxFrameSize = 32 * 1024
	proxyDeviceID       = "proxy"
	consumerGroupPrefix = "tcp-over-kafka.node."
)

// Config controls one symmetric tunnel node.
type Config struct {
	Broker       string              `json:"broker"`
	Topic        string              `json:"topic"`
	PlatformID   string              `json:"platformID"`
	ListenAddr   string              `json:"listen"`
	MaxFrameSize int                 `json:"maxFrameSize"`
	Routes       map[string]Endpoint `json:"routes"`
	Services     map[string]string   `json:"services"`
}

// LoadConfig reads and validates one node configuration file.
func LoadConfig(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}

	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("decode config %s: %w", path, err)
	}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c *Config) applyDefaults() {
	if c.MaxFrameSize == 0 {
		c.MaxFrameSize = DefaultMaxFrameSize
	}
}

// ConsumerGroup derives the per-node Kafka consumer group from the PlatformID.
func (c Config) ConsumerGroup() string {
	return consumerGroupPrefix + strings.TrimSpace(c.PlatformID)
}

// ProxyEndpoint returns the fixed local endpoint used for outbound SOCKS sessions.
func (c Config) ProxyEndpoint() Endpoint {
	return Endpoint{
		PlatformID: strings.TrimSpace(c.PlatformID),
		DeviceID:   proxyDeviceID,
	}
}

// validate ensures the node config has the minimum required fields.
func (c Config) validate() error {
	if strings.TrimSpace(c.ListenAddr) == "" {
		return fmt.Errorf("missing node listen address")
	}
	if _, _, err := net.SplitHostPort(strings.TrimSpace(c.ListenAddr)); err != nil {
		return fmt.Errorf("invalid node listen address %q: %w", c.ListenAddr, err)
	}
	if strings.TrimSpace(c.Broker) == "" {
		return fmt.Errorf("missing node broker address")
	}
	if strings.TrimSpace(c.Topic) == "" {
		return fmt.Errorf("missing node topic")
	}
	if strings.TrimSpace(c.PlatformID) == "" {
		return fmt.Errorf("missing node platform ID")
	}
	if len(c.Routes) == 0 {
		return fmt.Errorf("missing node routes")
	}
	for targetAddr, dest := range c.Routes {
		if _, _, err := net.SplitHostPort(strings.TrimSpace(targetAddr)); err != nil {
			return fmt.Errorf("invalid route address %q: %w", targetAddr, err)
		}
		if err := dest.validate(); err != nil {
			return fmt.Errorf("invalid route %q: %w", targetAddr, err)
		}
	}
	if len(c.Services) == 0 {
		return fmt.Errorf("missing node service mappings")
	}
	for deviceID, targetAddr := range c.Services {
		if strings.TrimSpace(deviceID) == "" {
			return fmt.Errorf("invalid service mapping: missing device ID")
		}
		if _, _, err := net.SplitHostPort(strings.TrimSpace(targetAddr)); err != nil {
			return fmt.Errorf("invalid service target %q for %q: %w", targetAddr, deviceID, err)
		}
	}
	if c.MaxFrameSize <= 0 {
		return fmt.Errorf("max frame size must be positive")
	}
	return nil
}
