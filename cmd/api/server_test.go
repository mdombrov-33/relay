package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"testing"
	"time"
)

func TestNewHTTPServer(t *testing.T) {
	ctx := context.Background()
	handler := http.NewServeMux()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	server := newHTTPServer(ctx, "127.0.0.1:4000", handler, logger)

	if server.Addr != "127.0.0.1:4000" || server.Handler != handler {
		t.Errorf("server address and handler = (%q, %v), want configured values", server.Addr, server.Handler)
	}
	if server.ReadTimeout != 10*time.Second || server.ReadHeaderTimeout != 5*time.Second || server.WriteTimeout != 0 || server.IdleTimeout != time.Minute {
		t.Errorf("server timeouts = (%s, %s, %s, %s), want bounded reads and SSE-compatible writes", server.ReadTimeout, server.ReadHeaderTimeout, server.WriteTimeout, server.IdleTimeout)
	}
	if server.MaxHeaderBytes != maxHeaderBytes {
		t.Errorf("MaxHeaderBytes = %d, want %d", server.MaxHeaderBytes, maxHeaderBytes)
	}
	if server.BaseContext(nil) != ctx {
		t.Error("BaseContext() did not return process context")
	}
}

func TestServeReturnsListenerFailure(t *testing.T) {
	wantErr := errors.New("listener failed")
	server := &fakeLifecycleServer{serveErr: wantErr, stopped: make(chan struct{})}
	close(server.stopped)

	err := serve(context.Background(), server, time.Second)

	if !errors.Is(err, wantErr) {
		t.Errorf("serve() error = %v, want %v", err, wantErr)
	}
	if server.shutdownCalls != 0 {
		t.Errorf("Shutdown() calls = %d, want 0", server.shutdownCalls)
	}
}

func TestServeShutsDownAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	server := &fakeLifecycleServer{serveErr: http.ErrServerClosed, stopped: make(chan struct{})}

	err := serve(ctx, server, time.Second)
	if err != nil {
		t.Fatalf("serve() error = %v", err)
	}
	if server.shutdownCalls != 1 {
		t.Errorf("Shutdown() calls = %d, want 1", server.shutdownCalls)
	}
	if server.shutdownContextErr != nil || !server.hasShutdownDeadline {
		t.Errorf("shutdown context = (error %v, deadline %t), want live bounded context", server.shutdownContextErr, server.hasShutdownDeadline)
	}
}

func TestServeCombinesShutdownFailures(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	serveErr := errors.New("listener close failed")
	shutdownErr := errors.New("connection drain failed")
	server := &fakeLifecycleServer{
		serveErr:    serveErr,
		shutdownErr: shutdownErr,
		stopped:     make(chan struct{}),
	}

	err := serve(ctx, server, time.Second)

	if !errors.Is(err, serveErr) || !errors.Is(err, shutdownErr) {
		t.Errorf("serve() error = %v, want joined serve and shutdown failures", err)
	}
}

type fakeLifecycleServer struct {
	serveErr            error
	shutdownErr         error
	stopped             chan struct{}
	shutdownCalls       int
	shutdownContextErr  error
	hasShutdownDeadline bool
}

func (f *fakeLifecycleServer) ListenAndServe() error {
	<-f.stopped
	return f.serveErr
}

func (f *fakeLifecycleServer) Shutdown(ctx context.Context) error {
	f.shutdownCalls++
	f.shutdownContextErr = ctx.Err()
	_, f.hasShutdownDeadline = ctx.Deadline()
	close(f.stopped)
	return f.shutdownErr
}
