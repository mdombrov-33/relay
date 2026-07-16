package tool

import (
	"errors"
	"fmt"
)

var (
	ErrDuplicateTool        = errors.New("duplicate tool name")
	ErrInvalidToolName      = errors.New("invalid tool name")
	ErrInvalidToolAuthority = errors.New("invalid tool authority")
	ErrToolNotFound         = errors.New("tool not found")
)

type Registry struct {
	tools map[string]registeredTool
}

type registeredTool struct {
	tool Tool
	spec Spec
}

func NewRegistry(tools ...Tool) (*Registry, error) {
	byName := make(map[string]registeredTool, len(tools))

	for _, candidate := range tools {
		spec := candidate.Spec()
		name := spec.Name
		if name == "" {
			return nil, fmt.Errorf("%w: empty", ErrInvalidToolName)
		}
		if !spec.Authority.Valid() {
			return nil, fmt.Errorf("%w: %q", ErrInvalidToolAuthority, spec.Authority)
		}

		if _, exists := byName[name]; exists {
			return nil, fmt.Errorf("%w: %q", ErrDuplicateTool, name)
		}

		byName[name] = registeredTool{tool: candidate, spec: spec}
	}

	return &Registry{tools: byName}, nil
}

func (r Registry) Lookup(name string) (Tool, error) {
	candidate, exists := r.tools[name]

	if !exists {
		return nil, fmt.Errorf("%w: %q", ErrToolNotFound, name)
	}

	return candidate.tool, nil
}

func (r Registry) Spec(name string) (Spec, error) {
	candidate, exists := r.tools[name]
	if !exists {
		return Spec{}, fmt.Errorf("%w: %q", ErrToolNotFound, name)
	}

	return candidate.spec, nil
}
