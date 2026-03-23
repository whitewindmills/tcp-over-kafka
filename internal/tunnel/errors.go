package tunnel

import (
	"errors"
	"io"
	"net"
	"strings"
)

// isExpectedStreamClose reports whether a read error is the normal close path.
func isExpectedStreamClose(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "use of closed network connection")
}
