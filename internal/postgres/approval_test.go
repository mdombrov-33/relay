package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mdombrov-33/relay/internal/event"
	"github.com/mdombrov-33/relay/internal/run"
)

func TestStoreRequestApprovalRejectsMismatchedInput(t *testing.T) {
	r := run.Run{ID: run.ID("run-123"), Status: run.StatusWaiting}
	request := ApprovalRequest{
		ID:       "approval-123",
		RunID:    r.ID,
		StepKey:  run.StepKey("tool/1/issue-credit"),
		CallID:   "call-123",
		ToolName: "issue_credit",
	}
	requested := approvalEvent(t, request.RunID, request.StepKey, event.TypeApprovalRequested, request.CallID, request.ToolName)

	tests := []struct {
		name      string
		run       run.Run
		request   ApprovalRequest
		requested event.Envelope
		want      error
	}{
		{
			name:      "incomplete request",
			run:       r,
			request:   ApprovalRequest{},
			requested: requested,
			want:      ErrInvalidApprovalRequest,
		},
		{
			name:      "run is not waiting",
			run:       run.Run{ID: r.ID, Status: run.StatusRunning},
			request:   request,
			requested: requested,
			want:      ErrApprovalRunNotWaiting,
		},
		{
			name:      "request run differs",
			run:       r,
			request:   approvalRequestWithRunID(request, run.ID("run-456")),
			requested: requested,
			want:      ErrApprovalRunIDMismatch,
		},
		{
			name:      "event run differs",
			run:       r,
			request:   request,
			requested: approvalEvent(t, run.ID("run-456"), request.StepKey, event.TypeApprovalRequested, request.CallID, request.ToolName),
			want:      ErrApprovalRunIDMismatch,
		},
		{
			name:      "event type differs",
			run:       r,
			request:   request,
			requested: approvalEvent(t, request.RunID, request.StepKey, event.TypeToolRequested, request.CallID, request.ToolName),
			want:      ErrApprovalEventExpected,
		},
		{
			name:      "event step differs",
			run:       r,
			request:   request,
			requested: approvalEvent(t, request.RunID, run.StepKey("tool/2/issue-credit"), event.TypeApprovalRequested, request.CallID, request.ToolName),
			want:      ErrApprovalEventStepKeyMismatch,
		},
		{
			name:      "event payload differs",
			run:       r,
			request:   request,
			requested: approvalEvent(t, request.RunID, request.StepKey, event.TypeApprovalRequested, request.CallID, "lookup_customer"),
			want:      ErrApprovalEventPayloadMismatch,
		},
	}

	store := &Store{}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := store.RequestApproval(context.Background(), test.run, test.request, test.requested)
			if !errors.Is(err, test.want) {
				t.Fatalf("RequestApproval() error = %v, want %v", err, test.want)
			}
		})
	}
}

func approvalRequestWithRunID(request ApprovalRequest, runID run.ID) ApprovalRequest {
	request.RunID = runID
	return request
}

func approvalEvent(t *testing.T, runID run.ID, stepKey run.StepKey, typ event.Type, callID, toolName string) event.Envelope {
	t.Helper()

	envelope, err := event.New(
		"event-123",
		runID,
		stepKey,
		typ,
		time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC),
		event.ToolPayload{CallID: callID, ToolName: toolName},
	)
	if err != nil {
		t.Fatalf("new approval event: %v", err)
	}

	return envelope
}
