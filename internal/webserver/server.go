package webserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// Serve starts an HTTP server only on a loopback address. It owns request
// timeouts so callers cannot accidentally expose an unbounded endpoint.
func Serve(ctx context.Context, address string, handler http.Handler) error {
	if err := validateLoopbackAddress(address); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second}
	errs := make(chan error, 1)
	go func() { errs <- server.Serve(listener) }()
	select {
	case err := <-errs:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		err := <-errs
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func validateLoopbackAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("web server must listen on a loopback address")
	}
	return nil
}
