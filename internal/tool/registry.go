package tool

import (
	"errors"
	"fmt"
)

var (
	ErrDuplicateTool   = errors.New("duplicate tool name")
	ErrInvalidToolName = errors.New("invalid tool name")
	ErrToolNotFound    = errors.New("tool not found")
)

type Registry struct {
	tools map[string]Tool
}

func NewRegistry(tools ...Tool) (*Registry, error) {
	byName := make(map[string]Tool, len(tools))

	for _, candidate := range tools {
		name := candidate.Spec().Name
		if name == "" {
			return nil, fmt.Errorf("%w: empty", ErrInvalidToolName)
		}

		if _, exists := byName[name]; exists {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateTool, name)
		}

		byName[name] = candidate
	}

	return &Registry{tools: byName}, nil
}

func (r Registry) Lookup(name string) (Tool, error) {
	candidate, exists := r.tools[name]

	if !exists {
		return nil, fmt.Errorf("%w: %q", ErrToolNotFound, name)
	}

	return candidate, nil
}
