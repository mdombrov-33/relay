package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/mdombrov-33/relay/internal/model"
	"github.com/mdombrov-33/relay/internal/postgres"
	"github.com/mdombrov-33/relay/internal/run"
	"github.com/mdombrov-33/relay/internal/tool"
)

func TestSummaryStepSummarizeCheckpointsSummary(t *testing.T) {
	var result json.RawMessage
	store := &checkpointStoreStub{
		claim: func(context.Context, run.ID, run.StepKey, [sha256.Size]byte, time.Time) (postgres.StepCheckpoint, error) {
			if result != nil {
				return postgres.StepCheckpoint{Status: postgres.StepStatusCompleted, Result: result}, nil
			}

			return postgres.StepCheckpoint{Attempt: 1, Status: postgres.StepStatusRunning}, nil
		},
		complete: func(_ context.Context, _ run.ID, _ run.StepKey, _ [sha256.Size]byte, _ int, completed json.RawMessage, _ time.Time) (postgres.StepCheckpoint, error) {
			result = append(json.RawMessage(nil), completed...)
			return postgres.StepCheckpoint{Status: postgres.StepStatusCompleted, Result: result}, nil
		},
	}
	client := model.NewScriptedClient(model.Response{Text: "Ada is eligible for a $5 credit after a resolved outage."})
	step := SummaryStep{
		Client:      client,
		Checkpoints: &StepRunner{Store: store},
		Timeout:     time.Second,
	}
	previous := SummaryState{Text: "Customer is Ada."}
	evicted := []model.Message{
		{Role: model.RoleAssistant, Content: "I found the customer."},
		{Role: model.RoleTool, Content: `{"id":"cust_123","plan":"pro"}`},
	}

	first, err := step.Summarize(context.Background(), "run-123", "memory/summary/1", previous, evicted)
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	second, err := step.Summarize(context.Background(), "run-123", "memory/summary/1", previous, evicted)
	if err != nil {
		t.Fatalf("second Summarize() error = %v", err)
	}
	if first != second {
		t.Errorf("summary states = %#v and %#v, want the checkpointed state", first, second)
	}
	if len(client.Requests()) != 1 {
		t.Fatalf("summary model requests = %d, want 1", len(client.Requests()))
	}

	request := client.Requests()[0]
	if len(request.Tools) != 0 {
		t.Errorf("summary tools = %#v, want none", request.Tools)
	}
	if len(request.Messages) != 5 {
		t.Fatalf("summary messages = %d, want 5", len(request.Messages))
	}
	if request.Messages[1].Content != "Current summary:\nCustomer is Ada." {
		t.Errorf("previous summary message = %q, want previous summary", request.Messages[1].Content)
	}
	if request.Messages[2].Content != evicted[0].Content || request.Messages[3].Content != evicted[1].Content {
		t.Errorf("summary request history = %#v, want evicted messages", request.Messages[2:4])
	}
}

func TestSummaryStepSummarizeRejectsInvalidModelResponses(t *testing.T) {
	tests := []struct {
		name     string
		response model.Response
		wantErr  error
	}{
		{
			name:     "tool calls",
			response: model.Response{ToolCalls: []tool.Call{{ID: "call_123", Name: "lookup"}}},
			wantErr:  ErrSummaryToolCalls,
		},
		{
			name:     "empty text",
			response: model.Response{},
			wantErr:  ErrEmptySummary,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &checkpointStoreStub{
				claim: func(context.Context, run.ID, run.StepKey, [sha256.Size]byte, time.Time) (postgres.StepCheckpoint, error) {
					return postgres.StepCheckpoint{Attempt: 1, Status: postgres.StepStatusRunning}, nil
				},
				complete: func(context.Context, run.ID, run.StepKey, [sha256.Size]byte, int, json.RawMessage, time.Time) (postgres.StepCheckpoint, error) {
					return postgres.StepCheckpoint{}, nil
				},
			}
			step := SummaryStep{
				Client:      model.NewScriptedClient(test.response),
				Checkpoints: &StepRunner{Store: store},
				Timeout:     time.Second,
			}

			_, err := step.Summarize(context.Background(), "run-123", "memory/summary/1", SummaryState{}, []model.Message{{Role: model.RoleTool, Content: "result"}})
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Summarize() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestSummaryStepSummarizeReturnsPreviousStateWithoutEvictedHistory(t *testing.T) {
	previous := SummaryState{Text: "Customer is Ada."}

	got, err := (SummaryStep{}).Summarize(context.Background(), "run-123", "memory/summary/1", previous, nil)
	if err != nil {
		t.Fatalf("Summarize() error = %v", err)
	}
	if got != previous {
		t.Errorf("Summarize() = %#v, want %#v", got, previous)
	}
}
