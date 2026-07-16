package postgres

import (
	"context"
	"errors"
	"fmt"

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
)

// Store persists Relay run projections and their event history in PostgreSQL.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a Store backed by pool. The caller retains ownership of the
// pool and must close it during process shutdown.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// CreateRun records a pending run and its workflow.queued.v1 event in one
// transaction. A caller must provide an event for the same run so the durable
// projection and event history cannot disagree after this operation succeeds.
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

// TransitionToTerminal updates a running run and records its corresponding
// terminal lifecycle event in one transaction. The status predicate in the
// update prevents duplicate terminal events when more than one caller tries to
// complete the same run.
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
