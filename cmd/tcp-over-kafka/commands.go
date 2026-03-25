package main

import (
	"context"
	"fmt"
	"log"
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
	cmd.AddCommand(newClientCmd(), newServerCmd(), newProxyCmd())
	return cmd
}

func newClientCmd() *cobra.Command {
	cfg := tunnel.ClientConfig{}
	var routeFlags []string

	cmd := &cobra.Command{
		Use:   "client",
		Short: "Run the SOCKS5 client proxy",
		Run: func(cmd *cobra.Command, _ []string) {
			routes, err := tunnel.ParseRoutes(routeFlags)
			must(err)
			cfg.Routes = routes
			must(tunnel.RunClient(cmd.Context(), cfg))
		},
	}

	cmd.Flags().StringVar(&cfg.ListenAddr, "listen", "0.0.0.0:1234", "local SOCKS5 listen address")
	cmd.Flags().StringVar(&cfg.Broker, "broker", "127.0.0.1:9092", "Kafka broker address")
	cmd.Flags().StringVar(&cfg.Topic, "topic", "tcp-over-kafka", "shared Kafka topic")
	cmd.Flags().StringVar(&cfg.ClientGroup, "group", "tcp-over-kafka-client", "Kafka consumer group for the client side")
	cmd.Flags().StringVar(&cfg.PlatformID, "platform-id", "", "source platform ID for this client")
	cmd.Flags().StringVar(&cfg.DeviceID, "device-id", "", "source device ID for this client")
	cmd.Flags().StringArrayVar(&routeFlags, "route", nil, "route mapping <host:port>=<destination-platform-id>/<destination-device-id> (repeatable)")
	cmd.Flags().IntVar(&cfg.MaxFrameSize, "max-frame", 32*1024, "maximum payload size per Kafka frame")
	return cmd
}

func newServerCmd() *cobra.Command {
	cfg := tunnel.ServerConfig{}
	var serviceFlags []string

	cmd := &cobra.Command{
		Use:   "server",
		Short: "Run the Kafka relay server",
		Run: func(cmd *cobra.Command, _ []string) {
			services, err := tunnel.ParseServices(serviceFlags)
			must(err)
			cfg.Services = services
			must(tunnel.RunServer(cmd.Context(), cfg))
		},
	}

	cmd.Flags().StringVar(&cfg.Broker, "broker", "127.0.0.1:9092", "Kafka broker address")
	cmd.Flags().StringVar(&cfg.Topic, "topic", "tcp-over-kafka", "shared Kafka topic")
	cmd.Flags().StringVar(&cfg.ServerGroup, "group", "tcp-over-kafka-server", "Kafka consumer group for the server side")
	cmd.Flags().StringVar(&cfg.PlatformID, "platform-id", "", "local destination platform ID for this server")
	cmd.Flags().StringArrayVar(&serviceFlags, "service", nil, "service mapping <destination-device-id>=<target-host:port> (repeatable)")
	cmd.Flags().IntVar(&cfg.MaxFrameSize, "max-frame", 32*1024, "maximum payload size per Kafka frame")
	return cmd
}

func newProxyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "proxy [--socks host:port] <host> <port>",
		Short: "Bridge SSH ProxyCommand I/O to the local SOCKS5 listener",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			socksAddr, err := cmd.Flags().GetString("socks")
			must(err)
			target := net.JoinHostPort(args[0], args[1])
			must(sshproxy.Run(cmd.Context(), socksAddr, target))
		},
	}

	cmd.Flags().String("socks", "127.0.0.1:1234", "local SOCKS5 proxy address")
	return cmd
}

// must terminates the process on a fatal runtime error.
func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
