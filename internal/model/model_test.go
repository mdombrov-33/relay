package model

import (
	"encoding/json"
	"testing"

	"github.com/mdombrov-33/relay/internal/tool"
)

func TestNewToolMessagePreservesToolResultCorrelation(t *testing.T) {
	got := NewToolMessage(tool.Result{
		CallID:   "call_123",
		ToolName: "lookup_customer",
		Content:  `{"id":"cust_123"}`,
	})

	if got.Role != RoleTool {
		t.Errorf("Role = %q, want %q", got.Role, RoleTool)
	}

	if got.ToolCallID != "call_123" {
		t.Errorf("ToolCallID = %q, want %q", got.ToolCallID, "call_123")
	}

	if got.ToolName != "lookup_customer" {
		t.Errorf("ToolName = %q, want %q", got.ToolName, "lookup_customer")
	}

	if got.Content != `{"id":"cust_123"}` {
		t.Errorf("Content = %q, want customer JSON", got.Content)
	}
}

func TestNewAssistantMessage(t *testing.T) {
	response := Response{
		Text: "I will look up that customer.",
		ToolCalls: []tool.Call{
			{
				ID:        "call_123",
				Name:      "lookup_customer",
				Arguments: json.RawMessage(`{"customer_id":"cust_123"}`),
			},
		},
	}

	got := NewAssistantMessage(response)

	if got.Role != RoleAssistant {
		t.Errorf("Role = %q, want %q", got.Role, RoleAssistant)
	}

	if got.Content != response.Text {
		t.Errorf("Content = %q, want %q", got.Content, response.Text)
	}

	if len(got.ToolCalls) != 1 {
		t.Fatalf("len(ToolCalls) = %d, want 1", len(got.ToolCalls))
	}

	gotCall := got.ToolCalls[0]
	if gotCall.ID != "call_123" {
		t.Errorf("ToolCalls[0].ID = %q, want %q", gotCall.ID, "call_123")
	}
	if gotCall.Name != "lookup_customer" {
		t.Errorf("ToolCalls[0].Name = %q, want %q", gotCall.Name, "lookup_customer")
	}
	if string(gotCall.Arguments) != `{"customer_id":"cust_123"}` {
		t.Errorf("ToolCalls[0].Arguments = %s, want customer JSON", gotCall.Arguments)
	}
}

func TestNewAssistantMessageCopiesToolCallArguments(t *testing.T) {
	response := Response{
		ToolCalls: []tool.Call{
			{
				ID:        "call_123",
				Name:      "lookup_customer",
				Arguments: json.RawMessage(`{"customer_id":"cust_123"}`),
			},
		},
	}

	message := NewAssistantMessage(response)

	response.ToolCalls[0].Arguments[0] = '['

	if got := string(message.ToolCalls[0].Arguments); got != `{"customer_id":"cust_123"}` {
		t.Errorf("assistant message arguments = %s, want original JSON", got)
	}
}
