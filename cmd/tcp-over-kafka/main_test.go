package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestRootVersionFlags verifies that the root command exposes the build
// version through both the short and long flag forms.
func TestRootVersionFlags(t *testing.T) {
	originalVersion := version
	version = "test-version"
	t.Cleanup(func() {
		version = originalVersion
	})

	testCases := []struct {
		name string
		args []string
	}{
		{
			name: "short",
			args: []string{"-v"},
		},
		{
			name: "long",
			args: []string{"--version"},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			cmd := newRootCmd()
			var output bytes.Buffer
			cmd.SetOut(&output)
			cmd.SetErr(&output)
			cmd.SetArgs(tc.args)

			if err := cmd.Execute(); err != nil {
				t.Fatalf("Execute() error = %v", err)
			}

			if got := output.String(); got != "test-version\n" {
				t.Fatalf("Execute() output = %q, want %q", got, "test-version\n")
			}
		})
	}
}

// TestRootHelpWithoutSubcommand verifies that the root command shows help when
// no subcommand is provided.
func TestRootHelpWithoutSubcommand(t *testing.T) {
	cmd := newRootCmd()
	var output bytes.Buffer
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	cmd.SetArgs(nil)

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	got := output.String()
	if !strings.Contains(got, "Usage:") {
		t.Fatalf("help output missing usage header: %q", got)
	}
	if !strings.Contains(got, "client") || !strings.Contains(got, "server") || !strings.Contains(got, "proxy") {
		t.Fatalf("help output missing expected subcommands: %q", got)
	}
}
