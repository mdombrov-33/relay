package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/mdombrov-33/relay/internal/postgres"
	"github.com/mdombrov-33/relay/internal/run"
)

type checkpointStoreStub struct {
	claim    func(context.Context, run.ID, run.StepKey, [sha256.Size]byte, time.Time) (postgres.StepCheckpoint, error)
	recover  func(context.Context, run.ID, run.StepKey, [sha256.Size]byte, time.Time) (postgres.StepCheckpoint, error)
	complete func(context.Context, run.ID, run.StepKey, [sha256.Size]byte, int, json.RawMessage, time.Time) (postgres.StepCheckpoint, error)
}

var _ CheckpointStore = (*checkpointStoreStub)(nil)

func (s *checkpointStoreStub) ClaimStep(ctx context.Context, runID run.ID, stepKey run.StepKey, inputHash [sha256.Size]byte, startedAt time.Time) (postgres.StepCheckpoint, error) {
	return s.claim(ctx, runID, stepKey, inputHash, startedAt)
}

func (s *checkpointStoreStub) RecoverStep(ctx context.Context, runID run.ID, stepKey run.StepKey, inputHash [sha256.Size]byte, startedAt time.Time) (postgres.StepCheckpoint, error) {
	return s.recover(ctx, runID, stepKey, inputHash, startedAt)
}

func (s *checkpointStoreStub) CompleteStep(ctx context.Context, runID run.ID, stepKey run.StepKey, inputHash [sha256.Size]byte, attempt int, result json.RawMessage, completedAt time.Time) (postgres.StepCheckpoint, error) {
	return s.complete(ctx, runID, stepKey, inputHash, attempt, result, completedAt)
}

func TestStepRunnerRun(t *testing.T) {
	const (
		runID   = run.ID("run-123")
		stepKey = run.StepKey("model/1")
	)
	input := []byte(`{"messages":["hello"]}`)
	fixedNow := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)

	t.Run("executes and completes a claimed checkpoint", func(t *testing.T) {
		var executed int
		store := &checkpointStoreStub{
			claim: func(_ context.Context, gotRunID run.ID, gotStepKey run.StepKey, gotHash [sha256.Size]byte, startedAt time.Time) (postgres.StepCheckpoint, error) {
				if gotRunID != runID || gotStepKey != stepKey || gotHash != sha256.Sum256(input) || !startedAt.Equal(fixedNow) {
					t.Fatalf("ClaimStep() arguments = (%q, %q, %x, %s)", gotRunID, gotStepKey, gotHash, startedAt)
				}
				return postgres.StepCheckpoint{Attempt: 1, Status: postgres.StepStatusRunning}, nil
			},
			complete: func(_ context.Context, gotRunID run.ID, gotStepKey run.StepKey, gotHash [sha256.Size]byte, attempt int, result json.RawMessage, completedAt time.Time) (postgres.StepCheckpoint, error) {
				if gotRunID != runID || gotStepKey != stepKey || gotHash != sha256.Sum256(input) || attempt != 1 || string(result) != `{"text":"fresh"}` || !completedAt.Equal(fixedNow) {
					t.Fatalf("CompleteStep() arguments = (%q, %q, %x, %d, %s, %s)", gotRunID, gotStepKey, gotHash, attempt, result, completedAt)
				}
				return postgres.StepCheckpoint{Status: postgres.StepStatusCompleted, Result: json.RawMessage(`{"text":"fresh"}`)}, nil
			},
		}

		runner := StepRunner{Store: store, Now: func() time.Time { return fixedNow }}
		result, err := runner.Run(context.Background(), runID, stepKey, input, func(context.Context) (json.RawMessage, error) {
			executed++
			return json.RawMessage(`{"text":"fresh"}`), nil
		})
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		if executed != 1 {
			t.Errorf("executions = %d, want 1", executed)
		}
		if string(result) != `{"text":"fresh"}` {
			t.Errorf("Run() result = %s, want fresh result", result)
		}
	})

	t.Run("returns a completed checkpoint without executing", func(t *testing.T) {
		store := &checkpointStoreStub{
			claim: func(context.Context, run.ID, run.StepKey, [sha256.Size]byte, time.Time) (postgres.StepCheckpoint, error) {
				return postgres.StepCheckpoint{Status: postgres.StepStatusCompleted, Result: json.RawMessage(`{"text":"cached"}`)}, nil
			},
		}

		runner := StepRunner{Store: store, Now: func() time.Time { return fixedNow }}
		result, err := runner.Run(context.Background(), runID, stepKey, input, func(context.Context) (json.RawMessage, error) {
			t.Fatal("step function executed for a completed checkpoint")
			return nil, nil
		})
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		if string(result) != `{"text":"cached"}` {
			t.Errorf("Run() result = %s, want cached result", result)
		}
	})

	t.Run("recovers a running checkpoint", func(t *testing.T) {
		var executed int
		store := &checkpointStoreStub{
			recover: func(context.Context, run.ID, run.StepKey, [sha256.Size]byte, time.Time) (postgres.StepCheckpoint, error) {
				return postgres.StepCheckpoint{Attempt: 2, Status: postgres.StepStatusRunning}, nil
			},
			complete: func(_ context.Context, _ run.ID, _ run.StepKey, _ [sha256.Size]byte, attempt int, _ json.RawMessage, _ time.Time) (postgres.StepCheckpoint, error) {
				if attempt != 2 {
					t.Fatalf("completion attempt = %d, want 2", attempt)
				}
				return postgres.StepCheckpoint{Status: postgres.StepStatusCompleted, Result: json.RawMessage(`{"text":"recovered"}`)}, nil
			},
		}

		runner := StepRunner{Store: store, Recover: true, Now: func() time.Time { return fixedNow }}
		result, err := runner.Run(context.Background(), runID, stepKey, input, func(context.Context) (json.RawMessage, error) {
			executed++
			return json.RawMessage(`{"text":"recovered"}`), nil
		})
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		if executed != 1 || string(result) != `{"text":"recovered"}` {
			t.Errorf("Run() = (%s, %d executions), want recovered result and 1 execution", result, executed)
		}
	})

	t.Run("claims a missing step during recovery", func(t *testing.T) {
		var claimed bool
		store := &checkpointStoreStub{
			recover: func(context.Context, run.ID, run.StepKey, [sha256.Size]byte, time.Time) (postgres.StepCheckpoint, error) {
				return postgres.StepCheckpoint{}, postgres.ErrStepNotFound
			},
			claim: func(context.Context, run.ID, run.StepKey, [sha256.Size]byte, time.Time) (postgres.StepCheckpoint, error) {
				claimed = true
				return postgres.StepCheckpoint{Attempt: 1, Status: postgres.StepStatusRunning}, nil
			},
			complete: func(context.Context, run.ID, run.StepKey, [sha256.Size]byte, int, json.RawMessage, time.Time) (postgres.StepCheckpoint, error) {
				return postgres.StepCheckpoint{Status: postgres.StepStatusCompleted, Result: json.RawMessage(`{"text":"new"}`)}, nil
			},
		}

		runner := StepRunner{Store: store, Recover: true, Now: func() time.Time { return fixedNow }}
		if _, err := runner.Run(context.Background(), runID, stepKey, input, func(context.Context) (json.RawMessage, error) {
			return json.RawMessage(`{"text":"new"}`), nil
		}); err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		if !claimed {
			t.Error("ClaimStep() was not called after missing recovery checkpoint")
		}
	})

	t.Run("preserves checkpoint errors", func(t *testing.T) {
		store := &checkpointStoreStub{
			claim: func(context.Context, run.ID, run.StepKey, [sha256.Size]byte, time.Time) (postgres.StepCheckpoint, error) {
				return postgres.StepCheckpoint{}, postgres.ErrStepInputMismatch
			},
		}

		runner := StepRunner{Store: store, Now: func() time.Time { return fixedNow }}
		_, err := runner.Run(context.Background(), runID, stepKey, input, func(context.Context) (json.RawMessage, error) {
			return json.RawMessage(`{"text":"never"}`), nil
		})
		if !errors.Is(err, postgres.ErrStepInputMismatch) {
			t.Errorf("Run() error = %v, want ErrStepInputMismatch", err)
		}
	})
}
