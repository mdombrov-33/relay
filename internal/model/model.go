package model

import (
	"context"

	"github.com/mdombrov-33/relay/internal/tool"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type Message struct {
	Role    Role
	Content string
}

type Request struct {
	Messages []Message
	Tools    []tool.Spec
}

type Response struct {
	Text      string
	ToolCalls []tool.Call
}

type Client interface {
	Next(ctx context.Context, request Request) (Response, error)
}
