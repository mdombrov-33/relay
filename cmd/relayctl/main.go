package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"

	"github.com/mdombrov-33/relay/internal/event"
	"github.com/mdombrov-33/relay/internal/postgres"
	"github.com/mdombrov-33/relay/internal/run"
)

const eventsUsage = "usage: relayctl events [-run run-id] [-after sequence]"

type eventReader interface {
	ListRunEvents(ctx context.Context, runID run.ID, afterSequence int64) ([]event.Stored, error)
	ListEventsAfter(ctx context.Context, afterSequence int64) ([]event.Stored, error)
}

type eventsOptions struct {
	runID run.ID
	after int64
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := runCommand(ctx, os.Args[1:], os.Getenv("DATABASE_URL"), os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func runCommand(ctx context.Context, args []string, databaseURL string, output io.Writer) error {
	options, err := parseEventsOptions(args)
	if err != nil {
		return err
	}
	if databaseURL == "" {
		return errors.New("DATABASE_URL must be set")
	}

	pool, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("open PostgreSQL: %w", err)
	}
	defer pool.Close()

	events, err := readEvents(ctx, postgres.NewStore(pool), options)
	if err != nil {
		return fmt.Errorf("read event history: %w", err)
	}
	if _, err := io.WriteString(output, formatEventHistory(events)); err != nil {
		return fmt.Errorf("write event history: %w", err)
	}

	return nil
}

func parseEventsOptions(args []string) (eventsOptions, error) {
	if len(args) == 0 {
		return eventsOptions{}, errors.New(eventsUsage)
	}
	if args[0] != "events" {
		return eventsOptions{}, fmt.Errorf("unknown relayctl command %q; %s", args[0], eventsUsage)
	}

	flags := flag.NewFlagSet("events", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	runID := flags.String("run", "", "run ID to inspect")
	after := flags.Int64("after", 0, "exclusive event sequence cursor")
	if err := flags.Parse(args[1:]); err != nil {
		return eventsOptions{}, fmt.Errorf("parse events flags: %w", err)
	}
	if flags.NArg() != 0 {
		return eventsOptions{}, fmt.Errorf("unexpected events argument %q; %s", flags.Arg(0), eventsUsage)
	}
	if *after < 0 {
		return eventsOptions{}, errors.New("event sequence cursor cannot be negative")
	}

	return eventsOptions{runID: run.ID(*runID), after: *after}, nil
}

func readEvents(ctx context.Context, reader eventReader, options eventsOptions) ([]event.Stored, error) {
	if options.runID == "" {
		return reader.ListEventsAfter(ctx, options.after)
	}

	return reader.ListRunEvents(ctx, options.runID, options.after)
}

func formatEventHistory(events []event.Stored) string {
	var history strings.Builder
	history.WriteString("Event history:\n")
	if len(events) == 0 {
		history.WriteString("(no events)\n")
		return history.String()
	}

	for _, stored := range events {
		history.WriteString(strconv.FormatInt(stored.Sequence, 10))
		history.WriteByte(' ')
		history.WriteString(stored.OccurredAt().Format("2006-01-02T15:04:05Z07:00"))
		history.WriteByte(' ')
		history.WriteString(stored.ID())
		history.WriteByte(' ')
		history.WriteString("run=")
		history.WriteString(string(stored.RunID()))
		history.WriteByte(' ')
		history.WriteString("step=")
		history.WriteString(string(stored.StepKey()))
		history.WriteByte(' ')
		history.WriteString(string(stored.Type()))
		history.WriteByte(' ')
		history.Write(stored.Payload())
		history.WriteByte('\n')
	}

	return history.String()
}
