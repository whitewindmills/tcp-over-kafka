package sshproxy

import (
	"context"
	"errors"
	"io"
	"net"
	"os"

	"tcp-over-kafka/pkg/socks5"
)

// Run connects SSH ProxyCommand stdio to the local SOCKS5 listener and pumps bytes until EOF.
func Run(ctx context.Context, socksAddr, target string) error {
	conn, err := net.Dial("tcp", socksAddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := socks5.Connect(conn, target); err != nil {
		return err
	}
	return relay(ctx, conn)
}

// relay copies stdin to the SOCKS5 connection and the connection back to stdout.
func relay(ctx context.Context, conn net.Conn) error {
	stdinDone := make(chan error, 1)
	stdoutDone := make(chan error, 1)

	go func() {
		_, err := io.Copy(conn, os.Stdin)
		if cw, ok := conn.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
		stdinDone <- err
	}()

	go func() {
		_, err := io.Copy(os.Stdout, conn)
		stdoutDone <- err
	}()

	for stdinDone != nil || stdoutDone != nil {
		select {
		case <-ctx.Done():
			_ = conn.Close()
			return ctx.Err()
		case err := <-stdoutDone:
			_ = conn.Close()
			stdoutDone = nil
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			}
			return nil
		case err := <-stdinDone:
			stdinDone = nil
			if err != nil && !errors.Is(err, io.EOF) {
				_ = conn.Close()
				return err
			}
		}
	}

	return nil
}
