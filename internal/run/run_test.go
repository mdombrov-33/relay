package run

import (
	"errors"
	"testing"
)

func TestRunTransitions(t *testing.T) {
	tests := []struct {
		name           string
		initialStatus  Status
		transition     func(*Run) error
		expectedStatus Status
		expectedError  error
	}{
		{
			name:           "starts from pending",
			initialStatus:  StatusPending,
			transition:     (*Run).Start,
			expectedStatus: StatusRunning,
		},
		{
			name:           "does not start from running",
			initialStatus:  StatusRunning,
			transition:     (*Run).Start,
			expectedStatus: StatusRunning,
			expectedError:  ErrCannotStart,
		},
		{
			name:           "waits from running",
			initialStatus:  StatusRunning,
			transition:     (*Run).Wait,
			expectedStatus: StatusWaiting,
		},
		{
			name:           "does not wait from pending",
			initialStatus:  StatusPending,
			transition:     (*Run).Wait,
			expectedStatus: StatusPending,
			expectedError:  ErrCannotWait,
		},
		{
			name:           "resumes from waiting",
			initialStatus:  StatusWaiting,
			transition:     (*Run).Resume,
			expectedStatus: StatusRunning,
		},
		{
			name:           "does not resume from running",
			initialStatus:  StatusRunning,
			transition:     (*Run).Resume,
			expectedStatus: StatusRunning,
			expectedError:  ErrCannotResume,
		},
		{
			name:           "succeeds from running",
			initialStatus:  StatusRunning,
			transition:     (*Run).Succeed,
			expectedStatus: StatusSucceeded,
		},
		{
			name:           "does not succeed from pending",
			initialStatus:  StatusPending,
			transition:     (*Run).Succeed,
			expectedStatus: StatusPending,
			expectedError:  ErrCannotSucceed,
		},
		{
			name:           "does not succeed from waiting",
			initialStatus:  StatusWaiting,
			transition:     (*Run).Succeed,
			expectedStatus: StatusWaiting,
			expectedError:  ErrCannotSucceed,
		},
		{
			name:           "fails from running",
			initialStatus:  StatusRunning,
			transition:     (*Run).Fail,
			expectedStatus: StatusFailed,
		},
		{
			name:           "does not fail from succeeded",
			initialStatus:  StatusSucceeded,
			transition:     (*Run).Fail,
			expectedStatus: StatusSucceeded,
			expectedError:  ErrCannotFail,
		},
		{
			name:           "does not fail from waiting",
			initialStatus:  StatusWaiting,
			transition:     (*Run).Fail,
			expectedStatus: StatusWaiting,
			expectedError:  ErrCannotFail,
		},
		{
			name:           "cancels from pending",
			initialStatus:  StatusPending,
			transition:     (*Run).Cancel,
			expectedStatus: StatusCanceled,
		},
		{
			name:           "cancels from waiting",
			initialStatus:  StatusWaiting,
			transition:     (*Run).Cancel,
			expectedStatus: StatusCanceled,
		},
		{
			name:           "cancels from running",
			initialStatus:  StatusRunning,
			transition:     (*Run).Cancel,
			expectedStatus: StatusCanceled,
		},
		{
			name:           "does not cancel from succeeded",
			initialStatus:  StatusSucceeded,
			transition:     (*Run).Cancel,
			expectedStatus: StatusSucceeded,
			expectedError:  ErrCannotCancel,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			run := Run{
				ID:     "run-123",
				Status: test.initialStatus,
			}

			err := test.transition(&run)
			if !errors.Is(err, test.expectedError) {
				t.Fatalf("transition error = %v, want %v", err, test.expectedError)
			}

			if run.Status != test.expectedStatus {
				t.Fatalf("Status = %q, want %q", run.Status, test.expectedStatus)
			}
		})
	}
}
