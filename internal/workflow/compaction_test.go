package workflow

import (
	"errors"
	"testing"

	"github.com/mdombrov-33/relay/internal/model"
)

func TestCompactionPlannerPlan(t *testing.T) {
	history := []model.Message{
		{Role: model.RoleAssistant, Content: "first assistant message"},
		{Role: model.RoleTool, Content: "first tool result"},
		{Role: model.RoleAssistant, Content: "second assistant message"},
		{Role: model.RoleTool, Content: "second tool result"},
	}
	recent := history[2:]
	total := mustMessagesSize(t, history)
	recentSize := mustMessagesSize(t, recent)

	tests := []struct {
		name         string
		planner      CompactionPlanner
		wantRequired bool
		wantEvicted  []model.Message
		wantRetained []model.Message
		wantErr      error
	}{
		{
			name: "retains all history below the maximum",
			planner: CompactionPlanner{
				MaxBytes:  total,
				KeepBytes: total - 1,
			},
			wantRetained: history,
		},
		{
			name: "evicts oldest history down to the lower watermark",
			planner: CompactionPlanner{
				MaxBytes:  total - 1,
				KeepBytes: recentSize,
			},
			wantRequired: true,
			wantEvicted:  history[:2],
			wantRetained: recent,
		},
		{
			name: "rejects a nonpositive maximum",
			planner: CompactionPlanner{
				MaxBytes:  0,
				KeepBytes: 1,
			},
			wantErr: ErrInvalidCompactionMaxBytes,
		},
		{
			name: "rejects a lower watermark outside the maximum",
			planner: CompactionPlanner{
				MaxBytes:  total,
				KeepBytes: total,
			},
			wantErr: ErrInvalidCompactionKeepBytes,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.planner.Plan(history)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Plan() error = %v, want %v", err, test.wantErr)
			}
			if test.wantErr != nil {
				return
			}
			if got.Required != test.wantRequired {
				t.Errorf("Plan() required = %t, want %t", got.Required, test.wantRequired)
			}
			if !messagesEqual(got.Evicted, test.wantEvicted) {
				t.Errorf("Plan() evicted = %#v, want %#v", got.Evicted, test.wantEvicted)
			}
			if !messagesEqual(got.Retained, test.wantRetained) {
				t.Errorf("Plan() retained = %#v, want %#v", got.Retained, test.wantRetained)
			}
		})
	}
}

func TestCompactionPlannerPlanKeepsNewestOversizedMessage(t *testing.T) {
	history := []model.Message{
		{Role: model.RoleAssistant, Content: "old message"},
		{Role: model.RoleTool, Content: "new message that exceeds the lower watermark"},
	}
	lastSize := mustMessagesSize(t, history[1:])
	total := mustMessagesSize(t, history)

	plan, err := (CompactionPlanner{MaxBytes: total - 1, KeepBytes: lastSize - 1}).Plan(history)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if !plan.Required {
		t.Error("Plan() required = false, want true")
	}
	if !messagesEqual(plan.Evicted, history[:1]) {
		t.Errorf("Plan() evicted = %#v, want %#v", plan.Evicted, history[:1])
	}
	if !messagesEqual(plan.Retained, history[1:]) {
		t.Errorf("Plan() retained = %#v, want %#v", plan.Retained, history[1:])
	}
}

func TestCompactionPlannerPlanReturnsIndependentMessages(t *testing.T) {
	history := []model.Message{
		{Role: model.RoleAssistant, Content: "old message"},
		{Role: model.RoleTool, Content: "new message"},
	}
	total := mustMessagesSize(t, history)

	plan, err := (CompactionPlanner{MaxBytes: total - 1, KeepBytes: mustMessagesSize(t, history[1:])}).Plan(history)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}

	history[0].Content = "mutated"
	history[1].Content = "mutated"

	if plan.Evicted[0].Content != "old message" {
		t.Errorf("evicted content = %q, want original value", plan.Evicted[0].Content)
	}
	if plan.Retained[0].Content != "new message" {
		t.Errorf("retained content = %q, want original value", plan.Retained[0].Content)
	}
}
