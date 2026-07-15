package event

import (
	"fmt"
	"sync"
	"time"

	"github.com/mdombrov-33/relay/internal/run"
)

type Log struct {
	mu     sync.RWMutex
	nextID uint64
	events []Envelope
}

func (l *Log) Record(runID run.ID, stepKey run.StepKey, typ Type, payload Payload) (Envelope, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	eventID := fmt.Sprintf("event-%d", l.nextID+1)
	envelope, err := New(eventID, runID, stepKey, typ, time.Now().UTC(), payload)
	if err != nil {
		return Envelope{}, fmt.Errorf("create event envelope: %w", err)
	}

	l.nextID++
	l.events = append(l.events, envelope)

	return envelope, nil
}

func (l *Log) Events() []Envelope {
	l.mu.RLock()
	defer l.mu.RUnlock()

	return append([]Envelope(nil), l.events...)
}
