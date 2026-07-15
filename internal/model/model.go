package model

import (
	"context"
	"encoding/json"
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
}

type ToolCall struct {
	ID        string
	Name      string
	Arguments json.RawMessage
}

type Response struct {
	Text      string
	ToolCalls []ToolCall
}

type Client interface {
	Next(ctx context.Context, request Request) (Response, error)
}
