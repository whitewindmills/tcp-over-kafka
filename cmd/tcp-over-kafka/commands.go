package main

import (
	"context"
	"fmt"
	"net"

	"github.com/spf13/cobra"

	"tcp-over-kafka/pkg/sshproxy"
	"tcp-over-kafka/pkg/tunnel"
)

// Execute runs the CLI with the provided context and arguments.
func Execute(ctx context.Context, args []string) error {
	root := newRootCmd()
	root.SetArgs(args)
	return root.ExecuteContext(ctx)
}

func newRootCmd() *cobra.Command {
	var showVersion bool

	cmd := &cobra.Command{
		Use:           "tcp-over-kafka",
		Short:         "tcp-over-kafka proof of concept",
		SilenceErrors: false,
		SilenceUsage:  false,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if showVersion {
				fmt.Fprintln(cmd.OutOrStdout(), version)
				return nil
			}
			return cmd.Help()
		},
	}
	cmd.Flags().BoolVarP(&showVersion, "version", "v", false, "print the build version and exit")
	cmd.AddCommand(newNodeCmd(), newProxyCmd())
	return cmd
}

func newNodeCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "node",
		Short: "Run one symmetric Kafka tunnel node",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := tunnel.LoadConfig(configPath)
			if err != nil {
				return err
			}
			return tunnel.RunNode(cmd.Context(), cfg)
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to the node JSON configuration file")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}

func newProxyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "proxy [--socks host:port] <host> <port>",
		Short: "Bridge SSH ProxyCommand I/O to the local SOCKS5 listener",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			socksAddr, err := cmd.Flags().GetString("socks")
			if err != nil {
				return err
			}
			target := net.JoinHostPort(args[0], args[1])
			return sshproxy.Run(cmd.Context(), socksAddr, target)
		},
	}

	cmd.Flags().String("socks", "127.0.0.1:1234", "local SOCKS5 proxy address")
	return cmd
}
