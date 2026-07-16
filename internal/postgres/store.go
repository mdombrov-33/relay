package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mdombrov-33/relay/internal/event"
	"github.com/mdombrov-33/relay/internal/run"
)

var (
	ErrRunNotPending         = errors.New("new run must be pending")
	ErrQueuedEventExpected   = errors.New("new run requires a workflow queued event")
	ErrEventRunIDMismatch    = errors.New("event run ID does not match run")
	ErrRunNotTerminal        = errors.New("run must have a terminal status")
	ErrTerminalEventMismatch = errors.New("terminal event type does not match run status")
	ErrRunNotRunning         = errors.New("only a running run can transition to a terminal status")
	ErrRunAlreadyTerminal    = errors.New("run is already terminal")
	ErrCancellationExpected  = errors.New("run cancellation requires a workflow canceled event")
	ErrCancellationMismatch  = errors.New("workflow canceled event payload does not match canceled status")
	ErrNegativeEventCursor   = errors.New("event cursor cannot be negative")
	ErrRunNotFound           = errors.New("run was not found")
)

const eventPageSize = 100

type Store struct {
	pool *pgxpool.Pool
}

type RunRecord struct {
	Run             run.Run
	CreatedAt       time.Time
	UpdatedAt       time.Time
	PendingApproval *ApprovalRequestRecord
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) FindRun(ctx context.Context, runID run.ID) (RunRecord, error) {
	var (
		record              RunRecord
		approvalID          pgtype.Text
		approvalStepKey     pgtype.Text
		approvalCallID      pgtype.Text
		approvalToolName    pgtype.Text
		approvalRequestedAt pgtype.Timestamptz
	)
	if err := s.pool.QueryRow(
		ctx,
		`SELECT r.id, r.status, r.created_at, r.updated_at,
		        ar.id, ar.step_key, ar.call_id, ar.tool_name, ar.requested_at
		 FROM runs r
		 LEFT JOIN approval_requests ar
		   ON ar.run_id = r.id AND ar.status = 'pending'
		 WHERE r.id = $1`,
		runID,
	).Scan(
		&record.Run.ID,
		&record.Run.Status,
		&record.CreatedAt,
		&record.UpdatedAt,
		&approvalID,
		&approvalStepKey,
		&approvalCallID,
		&approvalToolName,
		&approvalRequestedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RunRecord{}, ErrRunNotFound
		}
		return RunRecord{}, fmt.Errorf("find run: %w", err)
	}

	if approvalID.Valid {
		record.PendingApproval = &ApprovalRequestRecord{
			ApprovalRequest: ApprovalRequest{
				ID:       approvalID.String,
				RunID:    record.Run.ID,
				StepKey:  run.StepKey(approvalStepKey.String),
				CallID:   approvalCallID.String,
				ToolName: approvalToolName.String,
			},
			Status:      ApprovalStatusPending,
			RequestedAt: approvalRequestedAt.Time,
		}
	}

	return record, nil
}

func (s *Store) CreateRun(ctx context.Context, r run.Run, queued event.Envelope) error {
	if r.Status != run.StatusPending {
		return ErrRunNotPending
	}
	if queued.Type() != event.TypeWorkflowQueued {
		return ErrQueuedEventExpected
	}
	if queued.RunID() != r.ID {
		return ErrEventRunIDMismatch
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin create run transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO runs (id, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $3)`,
		r.ID,
		r.Status,
		queued.OccurredAt(),
	); err != nil {
		return fmt.Errorf("insert run projection: %w", err)
	}

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO events (id, run_id, step_key, type, occurred_at, payload)
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb)`,
		queued.ID(),
		queued.RunID(),
		queued.StepKey(),
		queued.Type(),
		queued.OccurredAt(),
		string(queued.Payload()),
	); err != nil {
		return fmt.Errorf("insert workflow queued event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit create run transaction: %w", err)
	}

	return nil
}

func (s *Store) TransitionToTerminal(ctx context.Context, r run.Run, terminal event.Envelope) error {
	expectedType, err := terminalEventType(r.Status)
	if err != nil {
		return err
	}
	if terminal.Type() != expectedType {
		return ErrTerminalEventMismatch
	}
	if terminal.RunID() != r.ID {
		return ErrEventRunIDMismatch
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin terminal transition transaction: %w", err)
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
		terminal.OccurredAt(),
		run.StatusRunning,
	)
	if err != nil {
		return fmt.Errorf("update terminal run projection: %w", err)
	}
	if result.RowsAffected() != 1 {
		return ErrRunNotRunning
	}

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO events (id, run_id, step_key, type, occurred_at, payload)
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb)`,
		terminal.ID(),
		terminal.RunID(),
		terminal.StepKey(),
		terminal.Type(),
		terminal.OccurredAt(),
		string(terminal.Payload()),
	); err != nil {
		return fmt.Errorf("insert terminal lifecycle event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit terminal transition transaction: %w", err)
	}

	return nil
}

func (s *Store) CancelRun(ctx context.Context, runID run.ID, canceled event.Envelope) error {
	if canceled.Type() != event.TypeWorkflowCancelled {
		return ErrCancellationExpected
	}
	if canceled.RunID() != runID {
		return ErrEventRunIDMismatch
	}
	var payload event.LifecyclePayload
	if err := json.Unmarshal(canceled.Payload(), &payload); err != nil || payload.Status != run.StatusCanceled {
		return ErrCancellationMismatch
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin cancel run transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var status run.Status
	if err := tx.QueryRow(
		ctx,
		`SELECT status
		 FROM runs
		 WHERE id = $1
		 FOR UPDATE`,
		runID,
	).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRunNotFound
		}
		return fmt.Errorf("lock run for cancellation: %w", err)
	}
	if status.IsTerminal() {
		return ErrRunAlreadyTerminal
	}

	if _, err := tx.Exec(
		ctx,
		`UPDATE runs
		 SET status = $2, updated_at = $3
		 WHERE id = $1`,
		runID,
		run.StatusCanceled,
		canceled.OccurredAt(),
	); err != nil {
		return fmt.Errorf("cancel run projection: %w", err)
	}

	if _, err := tx.Exec(
		ctx,
		`UPDATE approval_requests
		 SET status = $2, resolved_at = $3
		 WHERE run_id = $1 AND status = $4`,
		runID,
		ApprovalStatusCanceled,
		canceled.OccurredAt(),
		ApprovalStatusPending,
	); err != nil {
		return fmt.Errorf("cancel pending approval: %w", err)
	}

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO events (id, run_id, step_key, type, occurred_at, payload)
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb)`,
		canceled.ID(),
		canceled.RunID(),
		canceled.StepKey(),
		canceled.Type(),
		canceled.OccurredAt(),
		string(canceled.Payload()),
	); err != nil {
		return fmt.Errorf("insert workflow canceled event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit cancel run transaction: %w", err)
	}

	return nil
}

func (s *Store) ListRunEvents(ctx context.Context, runID run.ID, afterSequence int64) ([]event.Stored, error) {
	if afterSequence < 0 {
		return nil, ErrNegativeEventCursor
	}

	return s.listEvents(
		ctx,
		`SELECT sequence, id, run_id, step_key, type, occurred_at, payload
		 FROM events
		 WHERE run_id = $1 AND sequence > $2
		 ORDER BY sequence
		 LIMIT $3`,
		runID,
		afterSequence,
		eventPageSize,
	)
}

// exclusive sequence ursor. It is the basis for reconnectable event streams.
func (s *Store) ListEventsAfter(ctx context.Context, afterSequence int64) ([]event.Stored, error) {
	if afterSequence < 0 {
		return nil, ErrNegativeEventCursor
	}

	return s.listEvents(
		ctx,
		`SELECT sequence, id, run_id, step_key, type, occurred_at, payload
		 FROM events
		 WHERE sequence > $1
		 ORDER BY sequence
		 LIMIT $2`,
		afterSequence,
		eventPageSize,
	)
}

func (s *Store) listEvents(ctx context.Context, query string, args ...any) ([]event.Stored, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	storedEvents := make([]event.Stored, 0)
	for rows.Next() {
		var (
			sequence   int64
			id         string
			runID      run.ID
			stepKey    run.StepKey
			typ        event.Type
			occurredAt time.Time
			payload    []byte
		)
		if err := rows.Scan(&sequence, &id, &runID, &stepKey, &typ, &occurredAt, &payload); err != nil {
			return nil, fmt.Errorf("scan event row: %w", err)
		}

		stored, err := event.NewStored(sequence, id, runID, stepKey, typ, occurredAt, payload)
		if err != nil {
			return nil, fmt.Errorf("reconstruct stored event: %w", err)
		}
		storedEvents = append(storedEvents, stored)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate event rows: %w", err)
	}

	return storedEvents, nil
}

func terminalEventType(status run.Status) (event.Type, error) {
	switch status {
	case run.StatusSucceeded:
		return event.TypeWorkflowCompleted, nil
	case run.StatusFailed:
		return event.TypeWorkflowFailed, nil
	case run.StatusCanceled:
		return event.TypeWorkflowCancelled, nil
	default:
		return "", ErrRunNotTerminal
	}
}
