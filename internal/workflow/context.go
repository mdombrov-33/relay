package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/mdombrov-33/relay/internal/model"
	"github.com/mdombrov-33/relay/internal/tool"
)

var (
	ErrInvalidContextBudget       = errors.New("context budget must be positive")
	ErrPinnedContextExceedsBudget = errors.New("pinned context exceeds budget")
)

type ContextHydrator struct {
	MaxBytes int
}

func (h ContextHydrator) Hydrate(pinned, history []model.Message) ([]model.Message, error) {
	if h.MaxBytes <= 0 {
		return nil, ErrInvalidContextBudget
	}

	selected := cloneContextMessages(pinned)
	used, err := messagesSize(selected)
	if err != nil {
		return nil, fmt.Errorf("measure pinned context: %w", err)
	}
	if used > h.MaxBytes {
		return nil, fmt.Errorf("hydrate context: %w", ErrPinnedContextExceedsBudget)
	}

	start := len(history)
	for index, message := range slices.Backward(history) {
		messageSize, err := messageSize(message)
		if err != nil {
			return nil, fmt.Errorf("measure history message %d: %w", index, err)
		}
		if used+messageSize > h.MaxBytes {
			break
		}

		used += messageSize
		start = index
	}

	return append(selected, cloneContextMessages(history[start:])...), nil
}

func messagesSize(messages []model.Message) (int, error) {
	var total int
	for index, message := range messages {
		size, err := messageSize(message)
		if err != nil {
			return 0, fmt.Errorf("measure message %d: %w", index, err)
		}

		total += size
	}

	return total, nil
}

func messageSize(message model.Message) (int, error) {
	encoded, err := json.Marshal(message)
	if err != nil {
		return 0, err
	}

	return len(encoded), nil
}

func cloneContextMessages(messages []model.Message) []model.Message {
	if messages == nil {
		return nil
	}

	cloned := make([]model.Message, len(messages))
	for index, message := range messages {
		cloned[index] = message
		cloned[index].ToolCalls = cloneContextToolCalls(message.ToolCalls)
	}

	return cloned
}

func cloneContextToolCalls(calls []tool.Call) []tool.Call {
	if calls == nil {
		return nil
	}

	cloned := make([]tool.Call, len(calls))
	for index, call := range calls {
		cloned[index] = call
		cloned[index].Arguments = bytes.Clone(call.Arguments)
	}

	return cloned
}
