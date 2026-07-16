package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mdombrov-33/relay/internal/event"
	"github.com/mdombrov-33/relay/internal/run"
)

var (
	ErrInvalidApprovalRequest          = errors.New("approval request identity must be complete")
	ErrApprovalRunNotWaiting           = errors.New("approval request requires a waiting run")
	ErrApprovalRunNotRunning           = errors.New("only a running run can request approval")
	ErrApprovalRunIDMismatch           = errors.New("approval request run ID does not match run")
	ErrApprovalEventExpected           = errors.New("approval request requires an approval requested event")
	ErrApprovalEventStepKeyMismatch    = errors.New("approval event step key does not match request")
	ErrApprovalEventPayloadMismatch    = errors.New("approval event payload does not match request")
	ErrInvalidApprovalSignal           = errors.New("approval signal identity must be complete")
	ErrInvalidApprovalDecision         = errors.New("approval decision must be approved or rejected")
	ErrApprovalRequestNotFound         = errors.New("approval request was not found")
	ErrApprovalSignalRunIDMismatch     = errors.New("approval signal run ID does not match request")
	ErrApprovalResolvedEventExpected   = errors.New("approval signal requires an approval resolved event")
	ErrApprovalResolvedStepMismatch    = errors.New("approval resolved event step key does not match request")
	ErrApprovalResolvedPayloadMismatch = errors.New("approval resolved event payload does not match signal")
	ErrApprovalDecisionConflict        = errors.New("approval request already has a conflicting decision")
	ErrApprovalRunNotWaitingToResume   = errors.New("approval request run is not waiting")
	ErrApprovalResolutionBeforeRequest = errors.New("approval resolution cannot precede its request")
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

type ApprovalDecision string

const (
	ApprovalDecisionApproved ApprovalDecision = "approved"
	ApprovalDecisionRejected ApprovalDecision = "rejected"
)

type ApprovalSignal struct {
	ID        string
	RequestID string
	RunID     run.ID
	Decision  ApprovalDecision
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

func (s *Store) ResolveApproval(ctx context.Context, signal ApprovalSignal, resolved event.Envelope) (bool, error) {
	if signal.ID == "" || signal.RequestID == "" || signal.RunID == "" {
		return false, ErrInvalidApprovalSignal
	}
	if signal.Decision != ApprovalDecisionApproved && signal.Decision != ApprovalDecisionRejected {
		return false, ErrInvalidApprovalDecision
	}
	if resolved.Type() != event.TypeApprovalResolved {
		return false, ErrApprovalResolvedEventExpected
	}
	if resolved.RunID() != signal.RunID {
		return false, ErrApprovalSignalRunIDMismatch
	}

	var payload event.ApprovalPayload
	if err := json.Unmarshal(resolved.Payload(), &payload); err != nil || payload.RequestID != signal.RequestID || payload.Approved != (signal.Decision == ApprovalDecisionApproved) {
		return false, ErrApprovalResolvedPayloadMismatch
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin approval resolution transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var (
		requestRunID run.ID
		stepKey      run.StepKey
		status       ApprovalStatus
		requestedAt  time.Time
	)
	if err := tx.QueryRow(
		ctx,
		`SELECT run_id, step_key, status, requested_at
		 FROM approval_requests
		 WHERE id = $1
		 FOR UPDATE`,
		signal.RequestID,
	).Scan(&requestRunID, &stepKey, &status, &requestedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrApprovalRequestNotFound
		}
		return false, fmt.Errorf("lock approval request: %w", err)
	}
	if requestRunID != signal.RunID {
		return false, ErrApprovalSignalRunIDMismatch
	}
	if resolved.StepKey() != stepKey {
		return false, ErrApprovalResolvedStepMismatch
	}

	if status != ApprovalStatusPending {
		var recordedDecision ApprovalDecision
		if err := tx.QueryRow(
			ctx,
			`SELECT decision
			 FROM approval_signals
			 WHERE request_id = $1`,
			signal.RequestID,
		).Scan(&recordedDecision); err != nil {
			return false, fmt.Errorf("find approval signal: %w", err)
		}
		if recordedDecision != signal.Decision {
			return false, ErrApprovalDecisionConflict
		}
		return false, nil
	}

	if resolved.OccurredAt().Before(requestedAt) {
		return false, ErrApprovalResolutionBeforeRequest
	}

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO approval_signals (id, request_id, run_id, decision, received_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		signal.ID,
		signal.RequestID,
		signal.RunID,
		signal.Decision,
		resolved.OccurredAt(),
	); err != nil {
		return false, fmt.Errorf("insert approval signal: %w", err)
	}

	result, err := tx.Exec(
		ctx,
		`UPDATE approval_requests
		 SET status = $2, resolved_at = $3
		 WHERE id = $1 AND status = $4`,
		signal.RequestID,
		ApprovalStatus(signal.Decision),
		resolved.OccurredAt(),
		ApprovalStatusPending,
	)
	if err != nil {
		return false, fmt.Errorf("resolve approval request: %w", err)
	}
	if result.RowsAffected() != 1 {
		return false, ErrApprovalDecisionConflict
	}

	result, err = tx.Exec(
		ctx,
		`UPDATE runs
		 SET status = $2, updated_at = $3
		 WHERE id = $1 AND status = $4`,
		signal.RunID,
		run.StatusRunning,
		resolved.OccurredAt(),
		run.StatusWaiting,
	)
	if err != nil {
		return false, fmt.Errorf("resume approval run: %w", err)
	}
	if result.RowsAffected() != 1 {
		return false, ErrApprovalRunNotWaitingToResume
	}

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO events (id, run_id, step_key, type, occurred_at, payload)
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb)`,
		resolved.ID(),
		resolved.RunID(),
		resolved.StepKey(),
		resolved.Type(),
		resolved.OccurredAt(),
		string(resolved.Payload()),
	); err != nil {
		return false, fmt.Errorf("insert approval resolved event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit approval resolution transaction: %w", err)
	}

	return true, nil
}
