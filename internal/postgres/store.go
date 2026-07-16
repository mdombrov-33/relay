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
	ErrRunNotPending       = errors.New("new run must be pending")
	ErrQueuedEventExpected = errors.New("new run requires a workflow queued event")
	ErrEventRunIDMismatch  = errors.New("queued event run ID does not match run")
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
