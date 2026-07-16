package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mdombrov-33/relay/internal/event"
	"github.com/mdombrov-33/relay/internal/run"
)

func TestStoreCancelRunRejectsInvalidEvent(t *testing.T) {
	runID := run.ID("run-123")
	canceledAt := time.Date(2026, time.July, 17, 13, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		event    event.Envelope
		expected error
	}{
		{
			name:     "wrong type",
			event:    cancelEvent(t, runID, event.TypeWorkflowFailed, run.StatusCanceled, canceledAt),
			expected: ErrCancellationExpected,
		},
		{
			name:     "wrong run",
			event:    cancelEvent(t, run.ID("run-456"), event.TypeWorkflowCancelled, run.StatusCanceled, canceledAt),
			expected: ErrEventRunIDMismatch,
		},
		{
			name:     "wrong payload",
			event:    cancelEvent(t, runID, event.TypeWorkflowCancelled, run.StatusRunning, canceledAt),
			expected: ErrCancellationMismatch,
		},
	}

	store := &Store{}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := store.CancelRun(context.Background(), runID, test.event)
			if !errors.Is(err, test.expected) {
				t.Fatalf("CancelRun() error = %v, want %v", err, test.expected)
			}
		})
	}
}

func cancelEvent(t *testing.T, runID run.ID, typ event.Type, status run.Status, occurredAt time.Time) event.Envelope {
	t.Helper()
	envelope, err := event.New(
		"event-canceled",
		runID,
		run.StepKey("workflow"),
		typ,
		occurredAt,
		event.LifecyclePayload{Status: status},
	)
	if err != nil {
		t.Fatalf("new cancel event: %v", err)
	}
	return envelope
}
