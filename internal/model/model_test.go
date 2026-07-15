package model

import (
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
