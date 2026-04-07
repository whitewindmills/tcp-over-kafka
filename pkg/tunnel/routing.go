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
	NID string
	EID string
}

func (e Endpoint) validate() error {
	if strings.TrimSpace(e.NID) == "" {
		return fmt.Errorf("missing nid")
	}
	if strings.TrimSpace(e.EID) == "" {
		return fmt.Errorf("missing eid")
	}
	return nil
}

func (e Endpoint) key() string {
	return e.NID + "/" + e.EID
}

func (e Endpoint) matches(nid, eid string) bool {
	return e.NID == nid && e.EID == eid
}

// ParseRoutes parses repeatable --route entries of the form host:port=nid/eid.
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
		return "", Endpoint{}, fmt.Errorf("invalid route %q: want <host:port>=<nid>/<eid>", entry)
	}
	targetAddr := strings.TrimSpace(left)
	if _, _, err := net.SplitHostPort(targetAddr); err != nil {
		return "", Endpoint{}, fmt.Errorf("invalid route address %q: %w", targetAddr, err)
	}
	nid, eid, ok := strings.Cut(strings.TrimSpace(right), "/")
	if !ok {
		return "", Endpoint{}, fmt.Errorf("invalid route destination %q: want <nid>/<eid>", right)
	}
	dest := Endpoint{
		NID: strings.TrimSpace(nid),
		EID: strings.TrimSpace(eid),
	}
	if err := dest.validate(); err != nil {
		return "", Endpoint{}, fmt.Errorf("invalid route destination %q: %w", right, err)
	}
	return targetAddr, dest, nil
}

// ParseServices parses repeatable --service entries of the form eid=host:port.
func ParseServices(entries []string) (map[string]string, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	services := make(map[string]string, len(entries))
	for _, entry := range entries {
		eid, targetAddr, err := ParseServiceEntry(entry)
		if err != nil {
			return nil, err
		}
		services[eid] = targetAddr
	}
	return services, nil
}

// ParseServiceEntry parses one server service mapping.
func ParseServiceEntry(entry string) (string, string, error) {
	left, right, ok := strings.Cut(entry, "=")
	if !ok {
		return "", "", fmt.Errorf("invalid service %q: want <eid>=<host:port>", entry)
	}
	eid := strings.TrimSpace(left)
	if eid == "" {
		return "", "", fmt.Errorf("invalid service %q: missing eid", entry)
	}
	targetAddr := strings.TrimSpace(right)
	if _, _, err := net.SplitHostPort(targetAddr); err != nil {
		return "", "", fmt.Errorf("invalid service target %q: %w", targetAddr, err)
	}
	return eid, targetAddr, nil
}

func resolveClientRoute(routes map[string]Endpoint, targetAddr string) (Endpoint, bool) {
	dest, ok := routes[targetAddr]
	return dest, ok
}

func resolveServerService(services map[string]string, eid string) (string, bool) {
	targetAddr, ok := services[eid]
	return targetAddr, ok
}

func conversationKey(src, dst Endpoint, connectionID string) string {
	ends := []string{src.key(), dst.key()}
	slices.Sort(ends)
	return ends[0] + "|" + ends[1] + "|" + connectionID
}

func frameConversationKey(f frame.Frame) string {
	return conversationKey(
		Endpoint{NID: f.SourceNID, EID: f.SourceEID},
		Endpoint{NID: f.DestinationNID, EID: f.DestinationEID},
		f.ConnectionID,
	)
}
