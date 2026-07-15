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
		run.StepKey("workflow"),
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
		run.StepKey("tool/1/call-123"),
		event.TypeToolRequested,
		time.Date(2026, time.July, 15, 12, 34, 57, 0, time.UTC),
		event.ToolPayload{CallID: "call-123", ToolName: "lookup_customer"},
	)
	if err != nil {
		t.Fatalf("create requested event: %v", err)
	}

	const expected = "Event timeline:\n" +
		"2026-07-15T12:34:56Z event-1 run=run-123 step=workflow workflow.started.v1 {\"status\":\"running\"}\n" +
		"2026-07-15T12:34:57Z event-2 run=run-123 step=tool/1/call-123 tool.requested.v1 {\"callId\":\"call-123\",\"toolName\":\"lookup_customer\"}\n"

	if got := formatTimeline([]event.Envelope{started, requested}); got != expected {
		t.Errorf("formatTimeline() =\n%s\nwant:\n%s", got, expected)
	}
}

func TestFormatTimelineFailure(t *testing.T) {
	started, err := event.New(
		"event-1",
		run.ID("run-123"),
		run.StepKey("workflow"),
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
		run.StepKey("model/1"),
		event.TypeModelRequested,
		time.Date(2026, time.July, 15, 12, 34, 57, 0, time.UTC),
		event.ModelPayload{},
	)
	if err != nil {
		t.Fatalf("create requested event: %v", err)
	}

	failed, err := event.New(
		"event-3",
		run.ID("run-123"),
		run.StepKey("model/1"),
		event.TypeModelFailed,
		time.Date(2026, time.July, 15, 12, 34, 58, 0, time.UTC),
		event.ModelPayload{},
	)
	if err != nil {
		t.Fatalf("create model failed event: %v", err)
	}

	workflowFailed, err := event.New(
		"event-4",
		run.ID("run-123"),
		run.StepKey("workflow"),
		event.TypeWorkflowFailed,
		time.Date(2026, time.July, 15, 12, 34, 59, 0, time.UTC),
		event.LifecyclePayload{Status: run.StatusFailed},
	)
	if err != nil {
		t.Fatalf("create workflow failed event: %v", err)
	}

	const expected = "Event timeline:\n" +
		"2026-07-15T12:34:56Z event-1 run=run-123 step=workflow workflow.started.v1 {\"status\":\"running\"}\n" +
		"2026-07-15T12:34:57Z event-2 run=run-123 step=model/1 model.requested.v1 {}\n" +
		"2026-07-15T12:34:58Z event-3 run=run-123 step=model/1 model.failed.v1 {}\n" +
		"2026-07-15T12:34:59Z event-4 run=run-123 step=workflow workflow.failed.v1 {\"status\":\"failed\"}\n"

	if got := formatTimeline([]event.Envelope{started, requested, failed, workflowFailed}); got != expected {
		t.Errorf("formatTimeline() =\n%s\nwant:\n%s", got, expected)
	}
}
