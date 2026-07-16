package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mdombrov-33/relay/internal/postgres"
)

var ErrEffectRecorderNotConfigured = errors.New("effect recorder not configured")

const IssueCreditEffectType postgres.EffectType = "issue_credit"

type EffectRecorder interface {
	RecordEffect(context.Context, postgres.Effect) (postgres.Effect, bool, error)
}

type Credit struct {
	ID          string `json:"id"`
	CustomerID  string `json:"customer_id"`
	IncidentID  string `json:"incident_id"`
	AmountCents int64  `json:"amount_cents"`
}

type issueCreditArgs struct {
	CustomerID  string `json:"customer_id"`
	IncidentID  string `json:"incident_id"`
	AmountCents int64  `json:"amount_cents"`
}

type IssueCredit struct {
	recorder EffectRecorder
	now      func() time.Time
}

var _ Tool = (*IssueCredit)(nil)

func NewIssueCredit(recorder EffectRecorder) *IssueCredit {
	return &IssueCredit{recorder: recorder}
}

func (t *IssueCredit) Spec() Spec {
	return Spec{
		Name:        "issue_credit",
		Description: "Issues a synthetic support credit",
		Authority:   AuthorityEffect,
	}
}

func (t *IssueCredit) Execute(ctx context.Context, execution Execution) (Output, error) {
	if err := ctx.Err(); err != nil {
		return Output{}, err
	}
	if t.recorder == nil {
		return Output{}, ErrEffectRecorderNotConfigured
	}
	if execution.RunID == "" || execution.StepKey == "" {
		return Output{}, fmt.Errorf("%w: execution identity is required", ErrInvalidArguments)
	}

	var arguments issueCreditArgs
	if err := json.Unmarshal(execution.Arguments, &arguments); err != nil {
		return Output{}, fmt.Errorf("%w: %w", ErrInvalidArguments, err)
	}
	if arguments.CustomerID == "" || arguments.IncidentID == "" || arguments.AmountCents <= 0 {
		return Output{}, fmt.Errorf("%w: customer_id, incident_id, and positive amount_cents are required", ErrInvalidArguments)
	}

	idempotencyKey := fmt.Sprintf("issue_credit/%s/%s", execution.RunID, execution.StepKey)
	credit := Credit{
		ID:          idempotencyKey,
		CustomerID:  arguments.CustomerID,
		IncidentID:  arguments.IncidentID,
		AmountCents: arguments.AmountCents,
	}
	result, err := json.Marshal(credit)
	if err != nil {
		return Output{}, fmt.Errorf("marshal credit result: %w", err)
	}

	recorded, _, err := t.recorder.RecordEffect(ctx, postgres.Effect{
		IdempotencyKey: idempotencyKey,
		RunID:          execution.RunID,
		StepKey:        execution.StepKey,
		Type:           IssueCreditEffectType,
		Result:         result,
		RecordedAt:     t.recordedAt(),
	})
	if err != nil {
		return Output{}, fmt.Errorf("record issue credit effect: %w", err)
	}

	var recordedCredit Credit
	if err := json.Unmarshal(recorded.Result, &recordedCredit); err != nil {
		return Output{}, fmt.Errorf("decode recorded credit: %w", err)
	}

	content, err := json.Marshal(recordedCredit)
	if err != nil {
		return Output{}, fmt.Errorf("marshal recorded credit: %w", err)
	}

	return Output{Content: string(content)}, nil
}

func (t *IssueCredit) recordedAt() time.Time {
	if t.now != nil {
		return t.now().UTC()
	}

	return time.Now().UTC()
}
