package event_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/mdombrov-33/relay/internal/event"
	"github.com/mdombrov-33/relay/internal/run"
)

func TestNew(t *testing.T) {
	occurredAt := time.Date(2026, time.July, 15, 12, 34, 56, 0, time.UTC)

	event, err := event.New(
		"event-123",
		run.ID("run-123"),
		run.StepKey("tool/1/call-123"),
		event.TypeToolCompleted,
		occurredAt,
		event.ToolPayload{
			CallID:   "call-123",
			ToolName: "lookup_customer",
		},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	const expected = `{"id":"event-123","runId":"run-123","type":"tool.completed.v1","occurredAt":"2026-07-15T12:34:56Z","stepKey":"tool/1/call-123","payload":{"callId":"call-123","toolName":"lookup_customer"}}`
	if string(encoded) != expected {
		t.Fatalf("json.Marshal() = %s, want %s", encoded, expected)
	}
}

func TestNewEncodesTypedPayloads(t *testing.T) {
	tests := []struct {
		name     string
		typ      event.Type
		payload  event.Payload
		expected string
	}{
		{
			name:     "lifecycle",
			typ:      event.TypeWorkflowStarted,
			payload:  event.LifecyclePayload{Status: run.StatusRunning},
			expected: `{"status":"running"}`,
		},
		{
			name:     "model",
			typ:      event.TypeModelCompleted,
			payload:  event.ModelPayload{},
			expected: `{}`,
		},
		{
			name: "tool",
			typ:  event.TypeToolRequested,
			payload: event.ToolPayload{
				CallID:   "call-123",
				ToolName: "lookup_customer",
			},
			expected: `{"callId":"call-123","toolName":"lookup_customer"}`,
		},
		{
			name: "approval request",
			typ:  event.TypeApprovalRequested,
			payload: event.ToolPayload{
				CallID:   "call-123",
				ToolName: "issue_credit",
			},
			expected: `{"callId":"call-123","toolName":"issue_credit"}`,
		},
		{
			name: "memory",
			typ:  event.TypeMemoryCompacted,
			payload: event.MemoryPayload{
				EvictedMessages:  3,
				RetainedMessages: 2,
			},
			expected: `{"evictedMessages":3,"retainedMessages":2}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			envelope, err := event.New(
				"event-123",
				run.ID("run-123"),
				run.StepKey("workflow"),
				test.typ,
				time.Date(2026, time.July, 15, 12, 34, 56, 0, time.UTC),
				test.payload,
			)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			if got := string(envelope.Payload()); got != test.expected {
				t.Errorf("Payload() = %s, want %s", got, test.expected)
			}
		})
	}
}

func TestEnvelopePayloadReturnsCopy(t *testing.T) {
	event, err := event.New(
		"event-123",
		run.ID("run-123"),
		run.StepKey("model/1"),
		event.TypeModelCompleted,
		time.Date(2026, time.July, 15, 12, 34, 56, 0, time.UTC),
		event.ModelPayload{},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	payload := event.Payload()
	payload[0] = '['

	if got := string(event.Payload()); got != "{}" {
		t.Fatalf("Payload() after caller mutation = %s, want {}", got)
	}
}

func TestEnvelopeUnmarshalJSONPreservesUnknownType(t *testing.T) {
	const encoded = `{"id":"event-123","runId":"run-123","type":"future.event.v2","occurredAt":"2026-07-15T12:34:56Z","stepKey":"future/1","payload":{"futureField":"value"}}`

	var envelope event.Envelope
	if err := json.Unmarshal([]byte(encoded), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if got := envelope.ID(); got != "event-123" {
		t.Errorf("ID() = %q, want %q", got, "event-123")
	}
	if got := envelope.RunID(); got != run.ID("run-123") {
		t.Errorf("RunID() = %q, want %q", got, run.ID("run-123"))
	}
	if got := envelope.StepKey(); got != run.StepKey("future/1") {
		t.Errorf("StepKey() = %q, want %q", got, run.StepKey("future/1"))
	}
	if got := envelope.Type(); got != event.Type("future.event.v2") {
		t.Errorf("Type() = %q, want %q", got, event.Type("future.event.v2"))
	}
	if got := string(envelope.Payload()); got != `{"futureField":"value"}` {
		t.Errorf("Payload() = %s, want unknown payload JSON", got)
	}
}
