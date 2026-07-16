package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mdombrov-33/relay/internal/postgres"
	"github.com/mdombrov-33/relay/internal/run"
)

var (
	ErrCheckpointStoreNotConfigured = errors.New("checkpoint store not configured")
	ErrStepFunctionNotConfigured    = errors.New("step function not configured")
	ErrInvalidCheckpointResult      = errors.New("checkpoint result must be valid JSON")
	ErrUnexpectedCheckpointStatus   = errors.New("unexpected checkpoint status")
)

type CheckpointStore interface {
	ClaimStep(context.Context, run.ID, run.StepKey, [sha256.Size]byte, time.Time) (postgres.StepCheckpoint, error)
	RecoverStep(context.Context, run.ID, run.StepKey, [sha256.Size]byte, time.Time) (postgres.StepCheckpoint, error)
	CompleteStep(context.Context, run.ID, run.StepKey, [sha256.Size]byte, int, json.RawMessage, time.Time) (postgres.StepCheckpoint, error)
}

type StepRunner struct {
	Store   CheckpointStore
	Recover bool
	Now     func() time.Time
}

func (r StepRunner) Run(ctx context.Context, runID run.ID, stepKey run.StepKey, input []byte, execute func(context.Context) (json.RawMessage, error)) (json.RawMessage, error) {
	if r.Store == nil {
		return nil, ErrCheckpointStoreNotConfigured
	}
	if execute == nil {
		return nil, ErrStepFunctionNotConfigured
	}

	inputHash := sha256.Sum256(input)
	startedAt := r.now()
	checkpoint, err := r.loadCheckpoint(ctx, runID, stepKey, inputHash, startedAt)
	if err != nil {
		return nil, err
	}
	if checkpoint.Status == postgres.StepStatusCompleted {
		return bytes.Clone(checkpoint.Result), nil
	}
	if checkpoint.Status != postgres.StepStatusRunning {
		return nil, fmt.Errorf("run step %q: %w", stepKey, ErrUnexpectedCheckpointStatus)
	}

	result, err := execute(ctx)
	if err != nil {
		return nil, fmt.Errorf("execute step %q: %w", stepKey, err)
	}
	if !json.Valid(result) {
		return nil, fmt.Errorf("complete step %q: %w", stepKey, ErrInvalidCheckpointResult)
	}

	checkpoint, err = r.Store.CompleteStep(ctx, runID, stepKey, inputHash, checkpoint.Attempt, result, r.now())
	if err != nil {
		return nil, fmt.Errorf("complete step %q: %w", stepKey, err)
	}

	return bytes.Clone(checkpoint.Result), nil
}

func (r StepRunner) loadCheckpoint(ctx context.Context, runID run.ID, stepKey run.StepKey, inputHash [sha256.Size]byte, startedAt time.Time) (postgres.StepCheckpoint, error) {
	if !r.Recover {
		checkpoint, err := r.Store.ClaimStep(ctx, runID, stepKey, inputHash, startedAt)
		if err != nil {
			return postgres.StepCheckpoint{}, fmt.Errorf("claim step %q: %w", stepKey, err)
		}

		return checkpoint, nil
	}

	checkpoint, err := r.Store.RecoverStep(ctx, runID, stepKey, inputHash, startedAt)
	if err == nil {
		return checkpoint, nil
	}
	if !errors.Is(err, postgres.ErrStepNotFound) {
		return postgres.StepCheckpoint{}, fmt.Errorf("recover step %q: %w", stepKey, err)
	}

	checkpoint, err = r.Store.ClaimStep(ctx, runID, stepKey, inputHash, startedAt)
	if err != nil {
		return postgres.StepCheckpoint{}, fmt.Errorf("claim recovered step %q: %w", stepKey, err)
	}

	return checkpoint, nil
}

func (r StepRunner) now() time.Time {
	if r.Now != nil {
		return r.Now().UTC()
	}

	return time.Now().UTC()
}
