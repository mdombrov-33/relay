package run

import "testing"

func TestStatusIsTerminal(t *testing.T) {
	tests := []struct {
		name   string
		status Status
		want   bool
	}{
		{"pending is not terminal", StatusPending, false},
		{"running is not terminal", StatusRunning, false},
		{"waiting is not terminal", StatusWaiting, false},
		{"succeeded is terminal", StatusSucceeded, true},
		{"failed is terminal", StatusFailed, true},
		{"canceled is terminal", StatusCanceled, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.status.IsTerminal(); got != tt.want {
				t.Fatalf("IsTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}
