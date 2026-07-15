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
	Role       Role
	Content    string
	ToolCallID string
	ToolName   string
}

type Request struct {
	Messages []Message
	Tools    []tool.Spec
}

type Response struct {
	Text      string
	ToolCalls []tool.Call
}

func NewToolMessage(result tool.Result) Message {
	return Message{
		Role:       RoleTool,
		Content:    result.Content,
		ToolCallID: result.CallID,
		ToolName:   result.ToolName,
	}
}

type Client interface {
	Next(ctx context.Context, request Request) (Response, error)
}
