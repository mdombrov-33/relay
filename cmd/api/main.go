package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mdombrov-33/relay/internal/httpapi"
	"github.com/mdombrov-33/relay/internal/postgres"
)

const (
	defaultAddress  = "127.0.0.1:4000"
	startupTimeout  = 5 * time.Second
	shutdownTimeout = 20 * time.Second
)

type config struct {
	address     string
	databaseURL string
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := parseConfig(os.Args[1:], os.Getenv)
	if err == nil {
		err = run(ctx, cfg, logger)
	}
	if err != nil {
		logger.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

func parseConfig(args []string, getenv func(string) string) (config, error) {
	address := getenv("RELAY_API_ADDR")
	if address == "" {
		address = defaultAddress
	}

	flags := flag.NewFlagSet("relay-api", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&address, "addr", address, "HTTP listen address")
	if err := flags.Parse(args); err != nil {
		return config{}, fmt.Errorf("parse api flags: %w", err)
	}
	if flags.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected api argument %q", flags.Arg(0))
	}
	if address == "" {
		return config{}, errors.New("api address must not be empty")
	}

	databaseURL := getenv("DATABASE_URL")
	if databaseURL == "" {
		return config{}, errors.New("environment variable DATABASE_URL must be set")
	}

	return config{address: address, databaseURL: databaseURL}, nil
}

func run(ctx context.Context, cfg config, logger *slog.Logger) error {
	startupCtx, cancelStartup := context.WithTimeout(ctx, startupTimeout)
	pool, err := postgres.Open(startupCtx, cfg.databaseURL)
	cancelStartup()
	if err != nil {
		return fmt.Errorf("open postgresql: %w", err)
	}
	defer pool.Close()

	handler := httpapi.NewHandler(postgres.NewStore(pool))
	server := newHTTPServer(ctx, cfg.address, handler, logger)
	logger.Info("starting API", "address", cfg.address)
	if err := serve(ctx, server, shutdownTimeout); err != nil {
		return err
	}
	logger.Info("stopped API", "address", cfg.address)
	return nil
}
