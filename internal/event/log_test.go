package event_test

import (
	"testing"

	"github.com/mdombrov-33/relay/internal/event"
	"github.com/mdombrov-33/relay/internal/run"
)

func TestLogRecord(t *testing.T) {
	var log event.Log

	first, err := log.Record(
		run.ID("run-123"),
		run.StepKey("workflow"),
		event.TypeWorkflowStarted,
		event.LifecyclePayload{Status: run.StatusRunning},
	)
	if err != nil {
		t.Fatalf("Record() first event error = %v", err)
	}

	second, err := log.Record(
		run.ID("run-123"),
		run.StepKey("model/1"),
		event.TypeModelRequested,
		event.ModelPayload{},
	)
	if err != nil {
		t.Fatalf("Record() second event error = %v", err)
	}

	if got := first.ID(); got != "event-1" {
		t.Errorf("first event ID = %q, want %q", got, "event-1")
	}
	if got := second.ID(); got != "event-2" {
		t.Errorf("second event ID = %q, want %q", got, "event-2")
	}
	if first.OccurredAt().IsZero() {
		t.Error("first event occurrence time is zero")
	}

	events := log.Events()
	if len(events) != 2 {
		t.Fatalf("Events() length = %d, want 2", len(events))
	}
	if got := events[0].Type(); got != event.TypeWorkflowStarted {
		t.Errorf("first event type = %q, want %q", got, event.TypeWorkflowStarted)
	}
	if got := events[1].Type(); got != event.TypeModelRequested {
		t.Errorf("second event type = %q, want %q", got, event.TypeModelRequested)
	}
	if got := events[1].StepKey(); got != run.StepKey("model/1") {
		t.Errorf("second event step key = %q, want %q", got, run.StepKey("model/1"))
	}

	events[0] = event.Envelope{}
	if got := log.Events()[0].Type(); got != event.TypeWorkflowStarted {
		t.Errorf("first stored event type after caller mutation = %q, want %q", got, event.TypeWorkflowStarted)
	}
}
