package workflow

import (
	"context"
	"errors"
	"fmt"

	"github.com/mdombrov-33/relay/internal/event"
	"github.com/mdombrov-33/relay/internal/postgres"
	"github.com/mdombrov-33/relay/internal/run"
	"github.com/mdombrov-33/relay/internal/tool"
)

var (
	ErrApprovalStoreNotConfigured = errors.New("approval store not configured")
	ErrApprovalRequestMismatch    = errors.New("stored approval request does not match tool call")
	ErrUnexpectedApprovalStatus   = errors.New("unexpected approval request status")
)

type ApprovalStore interface {
	FindApprovalRequest(context.Context, string) (postgres.ApprovalRequestRecord, error)
	RequestApproval(context.Context, run.Run, postgres.ApprovalRequest, event.Envelope) error
}

type ApprovalState string

const (
	ApprovalStatePending  ApprovalState = "pending"
	ApprovalStateApproved ApprovalState = "approved"
	ApprovalStateRejected ApprovalState = "rejected"
)

type ApprovalGate struct {
	Store ApprovalStore
}

func (g ApprovalGate) Evaluate(ctx context.Context, r *run.Run, stepKey run.StepKey, call tool.Call, events *event.Log) (ApprovalState, error) {
	if g.Store == nil {
		return "", ErrApprovalStoreNotConfigured
	}

	request := postgres.ApprovalRequest{
		ID:       approvalRequestID(r.ID, stepKey),
		RunID:    r.ID,
		StepKey:  stepKey,
		CallID:   call.ID,
		ToolName: call.Name,
	}
	record, err := g.Store.FindApprovalRequest(ctx, request.ID)
	if err == nil {
		return existingApprovalState(r, request, record)
	}
	if !errors.Is(err, postgres.ErrApprovalRequestNotFound) {
		return "", fmt.Errorf("find approval request: %w", err)
	}

	if err := r.Wait(); err != nil {
		return "", fmt.Errorf("wait for approval: %w", err)
	}
	requested, err := events.Record(r.ID, stepKey, event.TypeApprovalRequested, event.ToolPayload{CallID: call.ID, ToolName: call.Name})
	if err != nil {
		if resumeErr := r.Resume(); resumeErr != nil {
			return "", fmt.Errorf("resume after approval event error: %w", resumeErr)
		}
		return "", fmt.Errorf("record approval requested event: %w", err)
	}
	if err := g.Store.RequestApproval(ctx, *r, request, requested); err != nil {
		if resumeErr := r.Resume(); resumeErr != nil {
			return "", fmt.Errorf("resume after approval request error: %w", resumeErr)
		}
		return "", fmt.Errorf("persist approval request: %w", err)
	}

	return ApprovalStatePending, nil
}

func existingApprovalState(r *run.Run, request postgres.ApprovalRequest, record postgres.ApprovalRequestRecord) (ApprovalState, error) {
	if record.ApprovalRequest != request {
		return "", ErrApprovalRequestMismatch
	}

	switch record.Status {
	case postgres.ApprovalStatusPending:
		if err := r.Wait(); err != nil {
			return "", fmt.Errorf("restore waiting approval: %w", err)
		}
		return ApprovalStatePending, nil
	case postgres.ApprovalStatusApproved:
		return ApprovalStateApproved, nil
	case postgres.ApprovalStatusRejected:
		return ApprovalStateRejected, nil
	default:
		return "", fmt.Errorf("approval status %q: %w", record.Status, ErrUnexpectedApprovalStatus)
	}
}

func approvalRequestID(runID run.ID, stepKey run.StepKey) string {
	return "approval/" + string(runID) + "/" + string(stepKey)
}
