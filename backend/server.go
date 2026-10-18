package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"
)

const (
	maxBodyBytes      int64 = 1 << 20 // 1 MiB for ordinary JSON requests
	maxRelayBodyBytes int64 = 8 << 20 // bundles carry many envelopes
	shutdownTimeout         = 20 * time.Second
)

// newHTTPServer returns a server with every timeout set. Without them a slow
// client (slowloris) can hold connections and goroutines indefinitely.
func newHTTPServer(addr string, h http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
}

// withBodyLimit rejects oversized request bodies: early with 413 when the
// declared length is too large, and by truncating reads otherwise (chunked).
func withBodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := maxBodyBytes
		if r.URL.Path == "/api/v1/relay/import" {
			limit = maxRelayBodyBytes
		}
		if r.ContentLength > limit {
			writeJSON(w, http.StatusRequestEntityTooLarge, &AppError{Code: "payload_too_large", Message: "request body too large"})
			return
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		next.ServeHTTP(w, r)
	})
}

// serveUntil serves on ln until ctx is cancelled, then drains in-flight
// requests for up to `grace` before returning.
func serveUntil(ctx context.Context, srv *http.Server, ln net.Listener, grace time.Duration) error {
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shCtx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	if err := srv.Shutdown(shCtx); err != nil {
		_ = srv.Close()
		return err
	}
	if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
