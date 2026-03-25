package tunnel

import (
	"fmt"
	"net"
	"slices"
	"strings"

	"tcp-over-kafka/pkg/frame"
)

// Endpoint identifies one logical tunnel peer or exposed service.
type Endpoint struct {
	PlatformID string
	DeviceID   string
}

func (e Endpoint) validate() error {
	if strings.TrimSpace(e.PlatformID) == "" {
		return fmt.Errorf("missing platform ID")
	}
	if strings.TrimSpace(e.DeviceID) == "" {
		return fmt.Errorf("missing device ID")
	}
	return nil
}

func (e Endpoint) key() string {
	return e.PlatformID + "/" + e.DeviceID
}

func (e Endpoint) matches(platformID, deviceID string) bool {
	return e.PlatformID == platformID && e.DeviceID == deviceID
}

// ParseRoutes parses repeatable --route entries of the form host:port=platform/device.
func ParseRoutes(entries []string) (map[string]Endpoint, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	routes := make(map[string]Endpoint, len(entries))
	for _, entry := range entries {
		targetAddr, dest, err := ParseRouteEntry(entry)
		if err != nil {
			return nil, err
		}
		routes[targetAddr] = dest
	}
	return routes, nil
}

// ParseRouteEntry parses one client route mapping.
func ParseRouteEntry(entry string) (string, Endpoint, error) {
	left, right, ok := strings.Cut(entry, "=")
	if !ok {
		return "", Endpoint{}, fmt.Errorf("invalid route %q: want <host:port>=<platform>/<device>", entry)
	}
	targetAddr := strings.TrimSpace(left)
	if _, _, err := net.SplitHostPort(targetAddr); err != nil {
		return "", Endpoint{}, fmt.Errorf("invalid route address %q: %w", targetAddr, err)
	}
	platformID, deviceID, ok := strings.Cut(strings.TrimSpace(right), "/")
	if !ok {
		return "", Endpoint{}, fmt.Errorf("invalid route destination %q: want <platform>/<device>", right)
	}
	dest := Endpoint{
		PlatformID: strings.TrimSpace(platformID),
		DeviceID:   strings.TrimSpace(deviceID),
	}
	if err := dest.validate(); err != nil {
		return "", Endpoint{}, fmt.Errorf("invalid route destination %q: %w", right, err)
	}
	return targetAddr, dest, nil
}

// ParseServices parses repeatable --service entries of the form device=host:port.
func ParseServices(entries []string) (map[string]string, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	services := make(map[string]string, len(entries))
	for _, entry := range entries {
		deviceID, targetAddr, err := ParseServiceEntry(entry)
		if err != nil {
			return nil, err
		}
		services[deviceID] = targetAddr
	}
	return services, nil
}

// ParseServiceEntry parses one server service mapping.
func ParseServiceEntry(entry string) (string, string, error) {
	left, right, ok := strings.Cut(entry, "=")
	if !ok {
		return "", "", fmt.Errorf("invalid service %q: want <device>=<host:port>", entry)
	}
	deviceID := strings.TrimSpace(left)
	if deviceID == "" {
		return "", "", fmt.Errorf("invalid service %q: missing device ID", entry)
	}
	targetAddr := strings.TrimSpace(right)
	if _, _, err := net.SplitHostPort(targetAddr); err != nil {
		return "", "", fmt.Errorf("invalid service target %q: %w", targetAddr, err)
	}
	return deviceID, targetAddr, nil
}

func resolveClientRoute(routes map[string]Endpoint, targetAddr string) (Endpoint, bool) {
	dest, ok := routes[targetAddr]
	return dest, ok
}

func resolveServerService(services map[string]string, deviceID string) (string, bool) {
	targetAddr, ok := services[deviceID]
	return targetAddr, ok
}

func conversationKey(src, dst Endpoint, connectionID string) string {
	ends := []string{src.key(), dst.key()}
	slices.Sort(ends)
	return ends[0] + "|" + ends[1] + "|" + connectionID
}

func frameConversationKey(f frame.Frame) string {
	return conversationKey(
		Endpoint{PlatformID: f.SourcePlatformID, DeviceID: f.SourceDeviceID},
		Endpoint{PlatformID: f.DestinationPlatformID, DeviceID: f.DestinationDeviceID},
		f.ConnectionID,
	)
}
