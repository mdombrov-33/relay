package main

import (
	"testing"
	"time"

	"github.com/mdombrov-33/relay/internal/event"
	"github.com/mdombrov-33/relay/internal/run"
)

func TestFormatTimeline(t *testing.T) {
	started, err := event.New(
		"event-1",
		run.ID("run-123"),
		event.TypeWorkflowStarted,
		time.Date(2026, time.July, 15, 12, 34, 56, 0, time.UTC),
		event.LifecyclePayload{Status: run.StatusRunning},
	)
	if err != nil {
		t.Fatalf("create started event: %v", err)
	}

	requested, err := event.New(
		"event-2",
		run.ID("run-123"),
		event.TypeToolRequested,
		time.Date(2026, time.July, 15, 12, 34, 57, 0, time.UTC),
		event.ToolPayload{CallID: "call-123", ToolName: "lookup_customer"},
	)
	if err != nil {
		t.Fatalf("create requested event: %v", err)
	}

	const expected = "Event timeline:\n" +
		"2026-07-15T12:34:56Z event-1 workflow.started.v1 {\"status\":\"running\"}\n" +
		"2026-07-15T12:34:57Z event-2 tool.requested.v1 {\"callId\":\"call-123\",\"toolName\":\"lookup_customer\"}\n"

	if got := formatTimeline([]event.Envelope{started, requested}); got != expected {
		t.Errorf("formatTimeline() =\n%s\nwant:\n%s", got, expected)
	}
}
