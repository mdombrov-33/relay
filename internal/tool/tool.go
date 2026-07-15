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

type Result struct {
	Content string
}

type Tool interface {
	Spec() Spec
	Execute(ctx context.Context, call Call) (Result, error)
}
