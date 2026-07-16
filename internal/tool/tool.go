package tool

import (
	"context"
	"encoding/json"

	"github.com/mdombrov-33/relay/internal/run"
)

type Spec struct {
	Name        string
	Description string
	Authority   Authority
}

type Authority string

const (
	AuthorityRead   Authority = "read"
	AuthorityEffect Authority = "effect"
)

func (a Authority) Valid() bool {
	return a == AuthorityRead || a == AuthorityEffect
}

type Call struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

type Execution struct {
	Call
	RunID   run.ID
	StepKey run.StepKey
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
	Execute(ctx context.Context, execution Execution) (Output, error)
}
