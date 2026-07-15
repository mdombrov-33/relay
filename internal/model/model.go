package model

import (
	"bytes"
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
	ToolCalls  []tool.Call
}

type Request struct {
	Messages []Message
	Tools    []tool.Spec
}

type Response struct {
	Text      string
	ToolCalls []tool.Call
}

func NewAssistantMessage(response Response) Message {
	return Message{
		Role:      RoleAssistant,
		Content:   response.Text,
		ToolCalls: cloneToolCalls(response.ToolCalls),
	}
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

func cloneToolCalls(calls []tool.Call) []tool.Call {
	if calls == nil {
		return nil
	}

	cloned := make([]tool.Call, len(calls))
	for i, call := range calls {
		cloned[i] = call
		cloned[i].Arguments = bytes.Clone(call.Arguments)
	}

	return cloned
}
