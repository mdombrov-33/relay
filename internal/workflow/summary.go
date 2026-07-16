package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mdombrov-33/relay/internal/model"
	"github.com/mdombrov-33/relay/internal/run"
)

var (
	ErrSummaryClientNotConfigured     = errors.New("summary client not configured")
	ErrSummaryCheckpointNotConfigured = errors.New("summary checkpoint not configured")
	ErrInvalidSummaryTimeout          = errors.New("summary timeout must be positive")
	ErrSummaryToolCalls               = errors.New("summary response must not contain tool calls")
	ErrEmptySummary                   = errors.New("summary response must contain text")
)

type SummaryState struct {
	Text string `json:"text"`
}

type SummaryStep struct {
	Client      model.Client
	Checkpoints *StepRunner
	Timeout     time.Duration
}

func (s SummaryStep) Summarize(ctx context.Context, runID run.ID, stepKey run.StepKey, previous SummaryState, evicted []model.Message) (SummaryState, error) {
	if len(evicted) == 0 {
		return previous, nil
	}
	if s.Client == nil {
		return SummaryState{}, ErrSummaryClientNotConfigured
	}
	if s.Checkpoints == nil {
		return SummaryState{}, ErrSummaryCheckpointNotConfigured
	}
	if s.Timeout <= 0 {
		return SummaryState{}, ErrInvalidSummaryTimeout
	}

	request := summaryRequest(previous, evicted)
	input, err := json.Marshal(request)
	if err != nil {
		return SummaryState{}, fmt.Errorf("marshal summary step input: %w", err)
	}

	result, err := s.Checkpoints.Run(ctx, runID, stepKey, input, func(stepCtx context.Context) (json.RawMessage, error) {
		modelCtx, cancel := context.WithTimeout(stepCtx, s.Timeout)
		defer cancel()

		response, err := s.Client.Next(modelCtx, request)
		if err != nil {
			return nil, err
		}
		if len(response.ToolCalls) != 0 {
			return nil, ErrSummaryToolCalls
		}
		if strings.TrimSpace(response.Text) == "" {
			return nil, ErrEmptySummary
		}

		encoded, err := json.Marshal(SummaryState{Text: response.Text})
		if err != nil {
			return nil, fmt.Errorf("marshal summary state: %w", err)
		}

		return encoded, nil
	})
	if err != nil {
		return SummaryState{}, fmt.Errorf("run summary step %q: %w", stepKey, err)
	}

	var summary SummaryState
	if err := json.Unmarshal(result, &summary); err != nil {
		return SummaryState{}, fmt.Errorf("decode summary state: %w", err)
	}
	if strings.TrimSpace(summary.Text) == "" {
		return SummaryState{}, ErrEmptySummary
	}

	return summary, nil
}

func summaryRequest(previous SummaryState, evicted []model.Message) model.Request {
	messages := []model.Message{{
		Role:    model.RoleSystem,
		Content: "Summarize the workflow history. Preserve concrete facts, decisions, open work, and tool results. Do not call tools.",
	}}
	if previous.Text != "" {
		messages = append(messages, model.Message{Role: model.RoleSystem, Content: "Current summary:\n" + previous.Text})
	}
	messages = append(messages, cloneContextMessages(evicted)...)
	messages = append(messages, model.Message{Role: model.RoleUser, Content: "Write an updated concise summary of the preceding workflow history."})

	return model.Request{Messages: messages}
}
