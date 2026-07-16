package event

import (
	"fmt"
	"sync"
	"time"

	"github.com/mdombrov-33/relay/internal/run"
)

type Log struct {
	mu     sync.RWMutex
	clock  Clock
	newID  IDGenerator
	events []Envelope
}

type Clock func() time.Time

type IDGenerator func() string

func NewLog() *Log {
	var nextID uint64

	return NewLogWith(
		func() time.Time { return time.Now().UTC() },
		func() string {
			nextID++
			return fmt.Sprintf("event-%d", nextID)
		},
	)
}

func NewLogWith(clock Clock, newID IDGenerator) *Log {
	return &Log{
		clock: clock,
		newID: newID,
	}
}

func (l *Log) Record(runID run.ID, stepKey run.StepKey, typ Type, payload Payload) (Envelope, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	encodedPayload, err := encodePayload(payload)
	if err != nil {
		return Envelope{}, fmt.Errorf("create event envelope: %w", err)
	}
	envelope := newEnvelope(l.newID(), runID, stepKey, typ, l.clock(), encodedPayload)

	l.events = append(l.events, envelope)

	return envelope, nil
}

func (l *Log) Events() []Envelope {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return append([]Envelope(nil), l.events...)
}
