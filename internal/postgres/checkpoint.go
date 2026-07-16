package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mdombrov-33/relay/internal/run"
)

var (
	ErrStepAlreadyRunning = errors.New("step is already running")
	ErrStepInputMismatch  = errors.New("step input hash does not match checkpoint")
	ErrStepNotFound       = errors.New("step checkpoint was not found")
	ErrStepNotRunning     = errors.New("step is not running")
	ErrInvalidStepResult  = errors.New("step result must be valid JSON")
)

type StepStatus string

const (
	StepStatusRunning   StepStatus = "running"
	StepStatusCompleted StepStatus = "completed"
)

type StepCheckpoint struct {
	RunID       run.ID
	StepKey     run.StepKey
	InputHash   [sha256.Size]byte
	Attempt     int
	Status      StepStatus
	Result      json.RawMessage
	StartedAt   time.Time
	CompletedAt *time.Time
}

func (s *Store) ClaimStep(ctx context.Context, runID run.ID, stepKey run.StepKey, inputHash [sha256.Size]byte, startedAt time.Time) (StepCheckpoint, error) {
	checkpoint, err := scanStep(s.pool.QueryRow(
		ctx,
		`INSERT INTO steps (run_id, step_key, input_hash, attempt, status, started_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (run_id, step_key) DO NOTHING
		 RETURNING run_id, step_key, input_hash, attempt, status, result, started_at, completed_at`,
		runID,
		stepKey,
		inputHash[:],
		1,
		StepStatusRunning,
		startedAt,
	))
	if err == nil {
		return checkpoint, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return StepCheckpoint{}, fmt.Errorf("claim step checkpoint: %w", err)
	}

	checkpoint, err = s.findStep(ctx, runID, stepKey)
	if err != nil {
		return StepCheckpoint{}, err
	}
	if checkpoint.InputHash != inputHash {
		return StepCheckpoint{}, ErrStepInputMismatch
	}
	if checkpoint.Status == StepStatusCompleted {
		return checkpoint, nil
	}

	return StepCheckpoint{}, ErrStepAlreadyRunning
}

func (s *Store) RecoverStep(ctx context.Context, runID run.ID, stepKey run.StepKey, inputHash [sha256.Size]byte, startedAt time.Time) (StepCheckpoint, error) {
	checkpoint, err := scanStep(s.pool.QueryRow(
		ctx,
		`UPDATE steps
		 SET attempt = attempt + 1, started_at = $4
		 WHERE run_id = $1 AND step_key = $2 AND input_hash = $3 AND status = $5
		 RETURNING run_id, step_key, input_hash, attempt, status, result, started_at, completed_at`,
		runID,
		stepKey,
		inputHash[:],
		startedAt,
		StepStatusRunning,
	))
	if err == nil {
		return checkpoint, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return StepCheckpoint{}, fmt.Errorf("recover step checkpoint: %w", err)
	}

	checkpoint, err = s.findStep(ctx, runID, stepKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return StepCheckpoint{}, ErrStepNotFound
	}
	if err != nil {
		return StepCheckpoint{}, err
	}
	if checkpoint.InputHash != inputHash {
		return StepCheckpoint{}, ErrStepInputMismatch
	}
	if checkpoint.Status == StepStatusCompleted {
		return checkpoint, nil
	}

	return StepCheckpoint{}, ErrStepAlreadyRunning
}

func (s *Store) CompleteStep(ctx context.Context, runID run.ID, stepKey run.StepKey, inputHash [sha256.Size]byte, attempt int, result json.RawMessage, completedAt time.Time) (StepCheckpoint, error) {
	if !json.Valid(result) {
		return StepCheckpoint{}, ErrInvalidStepResult
	}

	checkpoint, err := scanStep(s.pool.QueryRow(
		ctx,
		`UPDATE steps
		 SET status = $5, result = $6::jsonb, completed_at = $7
		 WHERE run_id = $1 AND step_key = $2 AND input_hash = $3 AND attempt = $4 AND status = $8
		 RETURNING run_id, step_key, input_hash, attempt, status, result, started_at, completed_at`,
		runID,
		stepKey,
		inputHash[:],
		attempt,
		StepStatusCompleted,
		string(result),
		completedAt,
		StepStatusRunning,
	))
	if errors.Is(err, pgx.ErrNoRows) {
		return StepCheckpoint{}, ErrStepNotRunning
	}
	if err != nil {
		return StepCheckpoint{}, fmt.Errorf("complete step checkpoint: %w", err)
	}

	return checkpoint, nil
}

func (s *Store) findStep(ctx context.Context, runID run.ID, stepKey run.StepKey) (StepCheckpoint, error) {
	checkpoint, err := scanStep(s.pool.QueryRow(
		ctx,
		`SELECT run_id, step_key, input_hash, attempt, status, result, started_at, completed_at
		 FROM steps
		 WHERE run_id = $1 AND step_key = $2`,
		runID,
		stepKey,
	))
	if err != nil {
		return StepCheckpoint{}, fmt.Errorf("find step checkpoint: %w", err)
	}

	return checkpoint, nil
}

func scanStep(row pgx.Row) (StepCheckpoint, error) {
	var (
		checkpoint StepCheckpoint
		inputHash  []byte
		result     []byte
	)
	if err := row.Scan(
		&checkpoint.RunID,
		&checkpoint.StepKey,
		&inputHash,
		&checkpoint.Attempt,
		&checkpoint.Status,
		&result,
		&checkpoint.StartedAt,
		&checkpoint.CompletedAt,
	); err != nil {
		return StepCheckpoint{}, fmt.Errorf("scan step checkpoint: %w", err)
	}
	if len(inputHash) != sha256.Size {
		return StepCheckpoint{}, fmt.Errorf("step input hash length = %d, want %d", len(inputHash), sha256.Size)
	}
	if checkpoint.Status != StepStatusRunning && checkpoint.Status != StepStatusCompleted {
		return StepCheckpoint{}, fmt.Errorf("unknown step status %q", checkpoint.Status)
	}

	copy(checkpoint.InputHash[:], inputHash)
	checkpoint.Result = bytes.Clone(result)
	return checkpoint, nil
}
