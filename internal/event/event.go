package event

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mdombrov-33/relay/internal/run"
)

type Type string

const (
	TypeWorkflowQueued    Type = "workflow.queued.v1"
	TypeWorkflowStarted   Type = "workflow.started.v1"
	TypeWorkflowWaiting   Type = "workflow.waiting.v1"
	TypeWorkflowResumed   Type = "workflow.resumed.v1"
	TypeWorkflowCompleted Type = "workflow.completed.v1"
	TypeWorkflowFailed    Type = "workflow.failed.v1"
	TypeWorkflowCancelled Type = "workflow.cancelled.v1"
	TypeModelRequested    Type = "model.requested.v1"
	TypeModelCompleted    Type = "model.completed.v1"
	TypeModelFailed       Type = "model.failed.v1"
	TypeToolRequested     Type = "tool.requested.v1"
	TypeToolCompleted     Type = "tool.completed.v1"
	TypeToolFailed        Type = "tool.failed.v1"
	TypeToolDenied        Type = "tool.denied.v1"
)

type Payload interface {
	isPayload()
}

type LifecyclePayload struct {
	Status run.Status `json:"status"`
}

func (LifecyclePayload) isPayload() {}

type ModelPayload struct{}

func (ModelPayload) isPayload() {}

type ToolPayload struct {
	CallID   string `json:"callId"`
	ToolName string `json:"toolName"`
}

func (ToolPayload) isPayload() {}

type Envelope struct {
	id         string
	runID      run.ID
	typ        Type
	occurredAt time.Time
	payload    json.RawMessage
}

type envelopeJSON struct {
	ID         string          `json:"id"`
	RunID      run.ID          `json:"runId"`
	Type       Type            `json:"type"`
	OccurredAt time.Time       `json:"occurredAt"`
	Payload    json.RawMessage `json:"payload"`
}

var (
	_ json.Marshaler   = Envelope{}
	_ json.Unmarshaler = (*Envelope)(nil)
)

func New(id string, runID run.ID, typ Type, occurredAt time.Time, payload Payload) (Envelope, error) {
	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, fmt.Errorf("marshal event payload: %w", err)
	}

	return Envelope{
		id:         id,
		runID:      runID,
		typ:        typ,
		occurredAt: occurredAt,
		payload:    encodedPayload,
	}, nil
}

func (e Envelope) ID() string {
	return e.id
}

func (e Envelope) RunID() run.ID {
	return e.runID
}

func (e Envelope) Type() Type {
	return e.typ
}

func (e Envelope) OccurredAt() time.Time {
	return e.occurredAt
}

func (e Envelope) Payload() json.RawMessage {
	return bytes.Clone(e.payload)
}

func (e Envelope) MarshalJSON() ([]byte, error) {
	return json.Marshal(envelopeJSON{
		ID:         e.id,
		RunID:      e.runID,
		Type:       e.typ,
		OccurredAt: e.occurredAt,
		Payload:    e.payload,
	})
}

func (e *Envelope) UnmarshalJSON(data []byte) error {
	var decoded envelopeJSON
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("decode event envelope: %w", err)
	}

	e.id = decoded.ID
	e.runID = decoded.RunID
	e.typ = decoded.Type
	e.occurredAt = decoded.OccurredAt
	e.payload = bytes.Clone(decoded.Payload)

	return nil
}
