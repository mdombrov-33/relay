package tool

import (
	"context"
	"encoding/json"
)

type Spec struct {
	Name        string
	Description string
}

type Call struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

type Output struct {
	Content string
}

type Result struct {
	CallID   string
	ToolName string
	Content  string
}

type Tool interface {
	Spec() Spec
	Execute(ctx context.Context, call Call) (Output, error)
}
