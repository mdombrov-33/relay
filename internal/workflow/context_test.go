package workflow

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/mdombrov-33/relay/internal/model"
	"github.com/mdombrov-33/relay/internal/tool"
)

func TestContextHydratorHydrate(t *testing.T) {
	pinned := []model.Message{{Role: model.RoleSystem, Content: "Follow the support policy."}, {Role: model.RoleUser, Content: "Resolve the incident."}}
	history := []model.Message{
		{Role: model.RoleAssistant, Content: "I found the customer."},
		{Role: model.RoleTool, Content: `{"id":"cust_123"}`, ToolCallID: "call_customer", ToolName: "lookup_customer"},
		{Role: model.RoleAssistant, Content: "I found the incident."},
	}

	pinnedSize := mustMessagesSize(t, pinned)
	allSize := pinnedSize + mustMessagesSize(t, history)
	recentSize := pinnedSize + mustMessagesSize(t, history[1:])

	tests := []struct {
		name    string
		budget  int
		want    []model.Message
		wantErr error
	}{
		{
			name:   "keeps all messages that fit",
			budget: allSize,
			want:   append(append([]model.Message(nil), pinned...), history...),
		},
		{
			name:   "omits the oldest history first",
			budget: recentSize,
			want:   append(append([]model.Message(nil), pinned...), history[1:]...),
		},
		{
			name:    "rejects a nonpositive budget",
			budget:  0,
			wantErr: ErrInvalidContextBudget,
		},
		{
			name:    "rejects a pinned context that cannot fit",
			budget:  pinnedSize - 1,
			wantErr: ErrPinnedContextExceedsBudget,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := (ContextHydrator{MaxBytes: test.budget}).Hydrate(pinned, history)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Hydrate() error = %v, want %v", err, test.wantErr)
			}
			if test.wantErr != nil {
				return
			}

			if !messagesEqual(got, test.want) {
				t.Fatalf("Hydrate() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestContextHydratorHydrateReturnsIndependentMessages(t *testing.T) {
	pinned := []model.Message{{
		Role: model.RoleAssistant,
		ToolCalls: []tool.Call{{
			ID:        "call_customer",
			Name:      "lookup_customer",
			Arguments: json.RawMessage(`{"customer_id":"cust_123"}`),
		}},
	}}
	history := []model.Message{{Role: model.RoleTool, Content: `{"id":"cust_123"}`}}
	budget := mustMessagesSize(t, pinned) + mustMessagesSize(t, history)

	got, err := (ContextHydrator{MaxBytes: budget}).Hydrate(pinned, history)
	if err != nil {
		t.Fatalf("Hydrate() error = %v", err)
	}

	pinned[0].ToolCalls[0].Arguments[2] = 'X'
	history[0].Content = "mutated"

	if got[0].ToolCalls[0].Arguments[2] != 'c' {
		t.Errorf("tool arguments = %q, want original value", got[0].ToolCalls[0].Arguments)
	}
	if got[1].Content != `{"id":"cust_123"}` {
		t.Errorf("history content = %q, want original value", got[1].Content)
	}
}

func mustMessagesSize(t *testing.T, messages []model.Message) int {
	t.Helper()

	size, err := messagesSize(messages)
	if err != nil {
		t.Fatalf("messagesSize() error = %v", err)
	}

	return size
}

func messagesEqual(got, want []model.Message) bool {
	if len(got) != len(want) {
		return false
	}

	for index := 0; index < len(got) && index < len(want); index++ {
		gotMessage := got[index]
		wantMessage := want[index]
		if gotMessage.Role != wantMessage.Role || gotMessage.Content != wantMessage.Content || gotMessage.ToolCallID != wantMessage.ToolCallID || gotMessage.ToolName != wantMessage.ToolName || len(gotMessage.ToolCalls) != len(wantMessage.ToolCalls) {
			return false
		}

		for callIndex := 0; callIndex < len(gotMessage.ToolCalls) && callIndex < len(wantMessage.ToolCalls); callIndex++ {
			gotCall := gotMessage.ToolCalls[callIndex]
			wantCall := wantMessage.ToolCalls[callIndex]
			if gotCall.ID != wantCall.ID || gotCall.Name != wantCall.Name || string(gotCall.Arguments) != string(wantCall.Arguments) {
				return false
			}
		}
	}

	return true
}
