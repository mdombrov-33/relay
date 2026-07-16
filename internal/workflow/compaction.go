package workflow

import (
	"errors"
	"fmt"
	"slices"

	"github.com/mdombrov-33/relay/internal/model"
)

var (
	ErrInvalidCompactionMaxBytes  = errors.New("compaction max bytes must be positive")
	ErrInvalidCompactionKeepBytes = errors.New("compaction keep bytes must be positive and less than max bytes")
)

type CompactionPlan struct {
	Required bool
	Evicted  []model.Message
	Retained []model.Message
}

type CompactionPlanner struct {
	MaxBytes  int
	KeepBytes int
}

func (p CompactionPlanner) Plan(history []model.Message) (CompactionPlan, error) {
	if p.MaxBytes <= 0 {
		return CompactionPlan{}, ErrInvalidCompactionMaxBytes
	}
	if p.KeepBytes <= 0 || p.KeepBytes >= p.MaxBytes {
		return CompactionPlan{}, ErrInvalidCompactionKeepBytes
	}

	total, err := messagesSize(history)
	if err != nil {
		return CompactionPlan{}, fmt.Errorf("measure history: %w", err)
	}
	if total <= p.MaxBytes {
		return CompactionPlan{Retained: cloneContextMessages(history)}, nil
	}

	start := len(history)
	var retainedBytes int
	for index, message := range slices.Backward(history) {
		size, err := messageSize(message)
		if err != nil {
			return CompactionPlan{}, fmt.Errorf("measure history message %d: %w", index, err)
		}
		if retainedBytes+size > p.KeepBytes && index != len(history)-1 {
			break
		}

		retainedBytes += size
		start = index
	}

	return CompactionPlan{
		Required: true,
		Evicted:  cloneContextMessages(history[:start]),
		Retained: cloneContextMessages(history[start:]),
	}, nil
}
