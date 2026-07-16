package event

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mdombrov-33/relay/internal/run"
)

const MaxPayloadBytes = 8 << 10

var ErrPayloadTooLarge = errors.New("event payload exceeds maximum size")

var (
	ErrInvalidStoredSequence = errors.New("stored event sequence must be positive")
	ErrInvalidStoredPayload  = errors.New("stored event payload must be valid JSON")
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
	TypeApprovalRequested Type = "approval.requested.v1"
	TypeMemoryCompacted   Type = "memory.compacted.v1"
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

type MemoryPayload struct {
	EvictedMessages  int `json:"evictedMessages"`
	RetainedMessages int `json:"retainedMessages"`
}

func (MemoryPayload) isPayload() {}

type Envelope struct {
	id         string
	runID      run.ID
	stepKey    run.StepKey
	typ        Type
	occurredAt time.Time
	payload    json.RawMessage
}

type Stored struct {
	Sequence int64
	Envelope
}

type envelopeJSON struct {
	ID         string          `json:"id"`
	RunID      run.ID          `json:"runId"`
	Type       Type            `json:"type"`
	OccurredAt time.Time       `json:"occurredAt"`
	StepKey    run.StepKey     `json:"stepKey"`
	Payload    json.RawMessage `json:"payload"`
}

var (
	_ json.Marshaler   = Envelope{}
	_ json.Unmarshaler = (*Envelope)(nil)
)

func New(id string, runID run.ID, stepKey run.StepKey, typ Type, occurredAt time.Time, payload Payload) (Envelope, error) {
	encodedPayload, err := encodePayload(payload)
	if err != nil {
		return Envelope{}, err
	}

	return newEnvelope(id, runID, stepKey, typ, occurredAt, encodedPayload), nil
}

func encodePayload(payload Payload) (json.RawMessage, error) {
	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal event payload: %w", err)
	}
	if len(encodedPayload) > MaxPayloadBytes {
		return nil, fmt.Errorf("%w: %d bytes exceeds %d", ErrPayloadTooLarge, len(encodedPayload), MaxPayloadBytes)
	}

	return encodedPayload, nil
}

func newEnvelope(id string, runID run.ID, stepKey run.StepKey, typ Type, occurredAt time.Time, payload json.RawMessage) Envelope {
	return Envelope{
		id:         id,
		runID:      runID,
		stepKey:    stepKey,
		typ:        typ,
		occurredAt: occurredAt,
		payload:    payload,
	}
}

func NewStored(sequence int64, id string, runID run.ID, stepKey run.StepKey, typ Type, occurredAt time.Time, payload json.RawMessage) (Stored, error) {
	if sequence < 1 {
		return Stored{}, ErrInvalidStoredSequence
	}
	if !json.Valid(payload) {
		return Stored{}, ErrInvalidStoredPayload
	}

	return Stored{
		Sequence: sequence,
		Envelope: Envelope{
			id:         id,
			runID:      runID,
			stepKey:    stepKey,
			typ:        typ,
			occurredAt: occurredAt,
			payload:    bytes.Clone(payload),
		},
	}, nil
}

func (e Envelope) ID() string {
	return e.id
}

func (e Envelope) RunID() run.ID {
	return e.runID
}

func (e Envelope) StepKey() run.StepKey {
	return e.stepKey
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
		StepKey:    e.stepKey,
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
	e.stepKey = decoded.StepKey
	e.typ = decoded.Type
	e.occurredAt = decoded.OccurredAt
	e.payload = bytes.Clone(decoded.Payload)

	return nil
}
