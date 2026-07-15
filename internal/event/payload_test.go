package event

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mdombrov-33/relay/internal/run"
)

type oversizedPayload struct {
	Content string `json:"content"`
}

func (oversizedPayload) isPayload() {}

func TestNewRejectsPayloadExceedingMaximum(t *testing.T) {
	_, err := New(
		"event-123",
		run.ID("run-123"),
		run.StepKey("model/1"),
		TypeModelCompleted,
		time.Date(2026, time.July, 15, 12, 34, 56, 0, time.UTC),
		oversizedPayload{Content: strings.Repeat("x", MaxPayloadBytes)},
	)
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("New() error = %v, want ErrPayloadTooLarge", err)
	}
}

func TestLogDoesNotRecordPayloadExceedingMaximum(t *testing.T) {
	var log Log

	_, err := log.Record(
		run.ID("run-123"),
		run.StepKey("model/1"),
		TypeModelCompleted,
		oversizedPayload{Content: strings.Repeat("x", MaxPayloadBytes)},
	)
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("Record() error = %v, want ErrPayloadTooLarge", err)
	}
	if got := len(log.Events()); got != 0 {
		t.Fatalf("Events() length after rejected payload = %d, want 0", got)
	}

	recorded, err := log.Record(
		run.ID("run-123"),
		run.StepKey("model/1"),
		TypeModelCompleted,
		ModelPayload{},
	)
	if err != nil {
		t.Fatalf("Record() safe payload error = %v", err)
	}
	if got := recorded.ID(); got != "event-1" {
		t.Errorf("recorded event ID = %q, want event-1", got)
	}
}
