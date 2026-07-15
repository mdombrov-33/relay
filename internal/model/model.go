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

func cloneRequest(request Request) Request {
	return Request{
		Messages: cloneMessages(request.Messages),
		Tools:    append([]tool.Spec(nil), request.Tools...),
	}
}

func cloneRequests(requests []Request) []Request {
	if requests == nil {
		return nil
	}

	cloned := make([]Request, len(requests))
	for i, request := range requests {
		cloned[i] = cloneRequest(request)
	}

	return cloned
}

func cloneMessages(messages []Message) []Message {
	if messages == nil {
		return nil
	}

	cloned := make([]Message, len(messages))
	for i, message := range messages {
		cloned[i] = message
		cloned[i].ToolCalls = cloneToolCalls(message.ToolCalls)
	}

	return cloned
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
