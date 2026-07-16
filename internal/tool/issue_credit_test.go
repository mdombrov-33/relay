package tool

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/mdombrov-33/relay/internal/postgres"
	"github.com/mdombrov-33/relay/internal/run"
)

type effectRecorderStub struct {
	record func(context.Context, postgres.Effect) (postgres.Effect, bool, error)
}

func (s effectRecorderStub) RecordEffect(ctx context.Context, effect postgres.Effect) (postgres.Effect, bool, error) {
	return s.record(ctx, effect)
}

func TestIssueCreditRecordsStableEffect(t *testing.T) {
	recordedAt := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	issuer := NewIssueCredit(effectRecorderStub{
		record: func(_ context.Context, effect postgres.Effect) (postgres.Effect, bool, error) {
			if effect.IdempotencyKey != "issue_credit/run-123/tool/1/call_credit" || effect.RunID != "run-123" || effect.StepKey != "tool/1/call_credit" || effect.Type != IssueCreditEffectType || !effect.RecordedAt.Equal(recordedAt) {
				t.Fatalf("RecordEffect() effect = %#v, want stable issue credit identity", effect)
			}
			return effect, true, nil
		},
	})
	issuer.now = func() time.Time { return recordedAt }

	output, err := issuer.Execute(context.Background(), Execution{
		Call: Call{
			ID:        "call_credit",
			Name:      "issue_credit",
			Arguments: json.RawMessage(`{"customer_id":"cust_123","incident_id":"inc_123","amount_cents":500}`),
		},
		RunID:   run.ID("run-123"),
		StepKey: run.StepKey("tool/1/call_credit"),
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var credit Credit
	if err := json.Unmarshal([]byte(output.Content), &credit); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	if credit.ID != "issue_credit/run-123/tool/1/call_credit" || credit.CustomerID != "cust_123" || credit.IncidentID != "inc_123" || credit.AmountCents != 500 {
		t.Errorf("credit = %#v, want recorded issue credit", credit)
	}
}

func TestIssueCreditRejectsInvalidExecution(t *testing.T) {
	issuer := NewIssueCredit(nil)

	_, err := issuer.Execute(context.Background(), Execution{})
	if !errors.Is(err, ErrEffectRecorderNotConfigured) {
		t.Errorf("Execute() error = %v, want %v", err, ErrEffectRecorderNotConfigured)
	}
}
