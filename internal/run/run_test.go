package run

import (
	"errors"
	"testing"
)

func TestRunStart(t *testing.T) {
	t.Run("a pending run starts", func(t *testing.T) {
		run := New("run-123")

		if err := run.Start(); err != nil {
			t.Fatalf("Start() error = %v", err)
		}

		if run.Status != StatusRunning {
			t.Fatalf("Status = %q, want %q", run.Status, StatusRunning)
		}
	})

	t.Run("a non-pending run cannot start", func(t *testing.T) {
		run := Run{
			ID:     "run-123",
			Status: StatusRunning,
		}

		if err := run.Start(); !errors.Is(err, ErrCannotStart) {
			t.Fatalf("Start() error = %v, want ErrCannotStart", err)
		}
	})
}
