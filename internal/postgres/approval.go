package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/mdombrov-33/relay/internal/event"
	"github.com/mdombrov-33/relay/internal/run"
)

var (
	ErrInvalidApprovalRequest       = errors.New("approval request identity must be complete")
	ErrApprovalRunNotWaiting        = errors.New("approval request requires a waiting run")
	ErrApprovalRunNotRunning        = errors.New("only a running run can request approval")
	ErrApprovalRunIDMismatch        = errors.New("approval request run ID does not match run")
	ErrApprovalEventExpected        = errors.New("approval request requires an approval requested event")
	ErrApprovalEventStepKeyMismatch = errors.New("approval event step key does not match request")
	ErrApprovalEventPayloadMismatch = errors.New("approval event payload does not match request")
)

type ApprovalStatus string

const (
	ApprovalStatusPending  ApprovalStatus = "pending"
	ApprovalStatusApproved ApprovalStatus = "approved"
	ApprovalStatusRejected ApprovalStatus = "rejected"
)

type ApprovalRequest struct {
	ID       string
	RunID    run.ID
	StepKey  run.StepKey
	CallID   string
	ToolName string
}

func (s *Store) RequestApproval(ctx context.Context, r run.Run, request ApprovalRequest, requested event.Envelope) error {
	if request.ID == "" || request.RunID == "" || request.StepKey == "" || request.CallID == "" || request.ToolName == "" {
		return ErrInvalidApprovalRequest
	}
	if r.Status != run.StatusWaiting {
		return ErrApprovalRunNotWaiting
	}
	if request.RunID != r.ID || requested.RunID() != r.ID {
		return ErrApprovalRunIDMismatch
	}
	if requested.Type() != event.TypeApprovalRequested {
		return ErrApprovalEventExpected
	}
	if requested.StepKey() != request.StepKey {
		return ErrApprovalEventStepKeyMismatch
	}

	var payload event.ToolPayload
	if err := json.Unmarshal(requested.Payload(), &payload); err != nil || payload.CallID != request.CallID || payload.ToolName != request.ToolName {
		return ErrApprovalEventPayloadMismatch
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin approval request transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	result, err := tx.Exec(
		ctx,
		`UPDATE runs
		 SET status = $2, updated_at = $3
		 WHERE id = $1 AND status = $4`,
		r.ID,
		r.Status,
		requested.OccurredAt(),
		run.StatusRunning,
	)
	if err != nil {
		return fmt.Errorf("update run to waiting: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrApprovalRunNotRunning
	}

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO approval_requests (id, run_id, step_key, call_id, tool_name, status, requested_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		request.ID,
		request.RunID,
		request.StepKey,
		request.CallID,
		request.ToolName,
		ApprovalStatusPending,
		requested.OccurredAt(),
	); err != nil {
		return fmt.Errorf("insert approval request: %w", err)
	}

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO events (id, run_id, step_key, type, occurred_at, payload)
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb)`,
		requested.ID(),
		requested.RunID(),
		requested.StepKey(),
		requested.Type(),
		requested.OccurredAt(),
		string(requested.Payload()),
	); err != nil {
		return fmt.Errorf("insert approval requested event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit approval request transaction: %w", err)
	}

	return nil
}
