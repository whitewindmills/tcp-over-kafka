package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"tcp-over-kafka/internal/sshproxy"
	"tcp-over-kafka/internal/tunnel"
)

// main dispatches the three operating modes:
// client listens locally, server relays on the remote host, and proxy bridges SSH.
func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch os.Args[1] {
	case "client":
		runClient(ctx, os.Args[2:])
	case "server":
		runServer(ctx, os.Args[2:])
	case "proxy":
		runProxy(ctx, os.Args[2:])
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

// runClient starts the SOCKS5 listener that accepts local TCP clients and forwards them into Kafka.
func runClient(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("client", flag.ExitOnError)
	cfg := tunnel.ClientConfig{}
	fs.StringVar(&cfg.ListenAddr, "listen", "0.0.0.0:1234", "local SOCKS5 listen address")
	fs.StringVar(&cfg.Broker, "broker", "127.0.0.1:9092", "Kafka broker address")
	fs.StringVar(&cfg.TunnelID, "tunnel-id", "poc", "shared tunnel identifier")
	fs.StringVar(&cfg.ClientGroup, "group", "tcp-over-kafka-client", "Kafka consumer group for the client side")
	fs.IntVar(&cfg.MaxFrameSize, "max-frame", 32*1024, "maximum payload size per Kafka frame")
	mustParse(fs, args)
	must(tunnel.RunClient(ctx, cfg))
}

// runServer starts the Kafka consumer that opens the target TCP connection and relays bytes back.
func runServer(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	cfg := tunnel.ServerConfig{}
	fs.StringVar(&cfg.Broker, "broker", "127.0.0.1:9092", "Kafka broker address")
	fs.StringVar(&cfg.TunnelID, "tunnel-id", "poc", "shared tunnel identifier")
	fs.StringVar(&cfg.ServerGroup, "group", "tcp-over-kafka-server", "Kafka consumer group for the server side")
	fs.StringVar(&cfg.TargetAddr, "target", "127.0.0.1:22", "fallback TCP target address when the frame omits one")
	fs.IntVar(&cfg.MaxFrameSize, "max-frame", 32*1024, "maximum payload size per Kafka frame")
	mustParse(fs, args)
	must(tunnel.RunServer(ctx, cfg))
}

// runProxy bridges SSH ProxyCommand stdio to the local SOCKS5 listener.
func runProxy(ctx context.Context, args []string) {
	fs := flag.NewFlagSet("proxy", flag.ExitOnError)
	socksAddr := fs.String("socks", "127.0.0.1:1234", "local SOCKS5 proxy address")
	mustParse(fs, args)
	rest := fs.Args()
	if len(rest) != 2 {
		fmt.Fprintln(os.Stderr, "usage: proxy [--socks host:port] <host> <port>")
		os.Exit(2)
	}
	target := net.JoinHostPort(rest[0], rest[1])
	must(sshproxy.Run(ctx, *socksAddr, target))
}

// mustParse exits cleanly when a subcommand flag parse fails.
func mustParse(fs *flag.FlagSet, args []string) {
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
}

// must terminates the process on a fatal runtime error.
func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// usage prints the top-level command help text.
func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s <client|server> [flags]\n", os.Args[0])
	fmt.Fprintln(os.Stderr, "  client listens as a SOCKS5 proxy and forwards TCP over Kafka")
	fmt.Fprintln(os.Stderr, "  server consumes the Kafka stream and connects to the requested target")
	fmt.Fprintln(os.Stderr, "  proxy bridges SSH ProxyCommand I/O to the local SOCKS5 listener")
	fmt.Fprintln(os.Stderr, "flags are available via <command> --help")
	if len(os.Args) > 1 {
		fmt.Fprintln(os.Stderr, "commands:", strings.Join([]string{"client", "server", "proxy"}, ", "))
	}
}
