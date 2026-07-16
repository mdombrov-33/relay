package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mdombrov-33/relay/internal/event"
	"github.com/mdombrov-33/relay/internal/run"
)

func TestParseEventsOptions(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    eventsOptions
		wantErr bool
	}{
		{name: "global page after cursor", args: []string{"events", "-after", "12"}, want: eventsOptions{after: 12}},
		{name: "run page after cursor", args: []string{"events", "-run", "run-123", "-after", "4"}, want: eventsOptions{runID: run.ID("run-123"), after: 4}},
		{name: "missing command", wantErr: true},
		{name: "unknown command", args: []string{"runs"}, wantErr: true},
		{name: "negative cursor", args: []string{"events", "-after", "-1"}, wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseEventsOptions(test.args)
			if test.wantErr {
				if err == nil {
					t.Fatal("parseEventsOptions() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("parseEventsOptions() error = %v", err)
			}
			if got != test.want {
				t.Errorf("parseEventsOptions() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestReadEvents(t *testing.T) {
	tests := []struct {
		name       string
		options    eventsOptions
		wantRunID  run.ID
		wantAfter  int64
		wantGlobal bool
	}{
		{name: "global event page", options: eventsOptions{after: 9}, wantAfter: 9, wantGlobal: true},
		{name: "run event page", options: eventsOptions{runID: run.ID("run-123"), after: 3}, wantRunID: run.ID("run-123"), wantAfter: 3},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader := &fakeEventReader{events: []event.Stored{storedEvent(t)}}

			got, err := readEvents(context.Background(), reader, test.options)
			if err != nil {
				t.Fatalf("readEvents() error = %v", err)
			}
			if len(got) != 1 || got[0].ID() != "event-123" {
				t.Errorf("readEvents() = %#v, want event-123", got)
			}
			if reader.global != test.wantGlobal {
				t.Errorf("global read = %t, want %t", reader.global, test.wantGlobal)
			}
			if reader.runID != test.wantRunID || reader.after != test.wantAfter {
				t.Errorf("read arguments = (run %q, after %d), want (run %q, after %d)", reader.runID, reader.after, test.wantRunID, test.wantAfter)
			}
		})
	}
}

func TestFormatEventHistory(t *testing.T) {
	const expected = "Event history:\n" +
		"7 2026-07-16T12:34:56Z event-123 run=run-123 step=workflow workflow.queued.v1 {\"status\":\"pending\"}\n"

	if got := formatEventHistory([]event.Stored{storedEvent(t)}); got != expected {
		t.Errorf("formatEventHistory() =\n%s\nwant:\n%s", got, expected)
	}
}

func TestFormatEventHistoryEmpty(t *testing.T) {
	const expected = "Event history:\n(no events)\n"

	if got := formatEventHistory(nil); got != expected {
		t.Errorf("formatEventHistory() = %q, want %q", got, expected)
	}
}

func TestReadEventsPropagatesStoreError(t *testing.T) {
	wantErr := errors.New("database unavailable")
	_, err := readEvents(context.Background(), &fakeEventReader{err: wantErr}, eventsOptions{})
	if !errors.Is(err, wantErr) {
		t.Errorf("readEvents() error = %v, want wrapped %v", err, wantErr)
	}
}

type fakeEventReader struct {
	events []event.Stored
	err    error
	global bool
	runID  run.ID
	after  int64
}

func (f *fakeEventReader) ListRunEvents(_ context.Context, runID run.ID, after int64) ([]event.Stored, error) {
	f.runID = runID
	f.after = after
	return f.events, f.err
}

func (f *fakeEventReader) ListEventsAfter(_ context.Context, after int64) ([]event.Stored, error) {
	f.global = true
	f.after = after
	return f.events, f.err
}

func storedEvent(t *testing.T) event.Stored {
	t.Helper()

	occurredAt := time.Date(2026, time.July, 16, 12, 34, 56, 0, time.UTC)
	envelope, err := event.New(
		"event-123",
		run.ID("run-123"),
		run.StepKey("workflow"),
		event.TypeWorkflowQueued,
		occurredAt,
		event.LifecyclePayload{Status: run.StatusPending},
	)
	if err != nil {
		t.Fatalf("event.New() error = %v", err)
	}

	stored, err := event.NewStored(7, envelope.ID(), envelope.RunID(), envelope.StepKey(), envelope.Type(), envelope.OccurredAt(), envelope.Payload())
	if err != nil {
		t.Fatalf("event.NewStored() error = %v", err)
	}

	return stored
}
