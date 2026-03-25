package tunnel

import (
	"errors"
	"io"
	"net"
	"testing"
)

// TestIsExpectedStreamClose verifies the close filter only hides normal teardown cases.
func TestIsExpectedStreamClose(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "eof", err: io.EOF, want: true},
		{name: "net closed", err: net.ErrClosed, want: true},
		{name: "wrapped closed", err: errors.New("read tcp 127.0.0.1:1->127.0.0.1:2: use of closed network connection"), want: true},
		{name: "other", err: errors.New("temporary failure"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isExpectedStreamClose(tt.err); got != tt.want {
				t.Fatalf("isExpectedStreamClose(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
