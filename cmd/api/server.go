package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

const maxHeaderBytes = 1 << 20

type lifecycleServer interface {
	ListenAndServe() error
	Shutdown(context.Context) error
}

func newHTTPServer(ctx context.Context, address string, handler http.Handler, logger *slog.Logger) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
		ReadTimeout:       10 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       time.Minute,
		MaxHeaderBytes:    maxHeaderBytes,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}
}

func serve(ctx context.Context, server lifecycleServer, timeout time.Duration) error {
	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serveErrors:
		if err == nil {
			return errors.New("http server stopped unexpectedly")
		}
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve http: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancelShutdown()

	shutdownErr := server.Shutdown(shutdownCtx)
	serveErr := <-serveErrors
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		serveErr = fmt.Errorf("serve http during shutdown: %w", serveErr)
	} else {
		serveErr = nil
	}
	if shutdownErr != nil {
		shutdownErr = fmt.Errorf("shut down http server: %w", shutdownErr)
	}

	return errors.Join(serveErr, shutdownErr)
}
