package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mdombrov-33/relay/internal/model"
	"github.com/mdombrov-33/relay/internal/run"
	"github.com/mdombrov-33/relay/internal/tool"
)

func TestEngineExecute(t *testing.T) {
	t.Run("returns the model response and succeeds the run", func(t *testing.T) {
		r := run.New("run-123")
		engine := Engine{
			Client: model.NewScriptedClient(
				model.Response{Text: "Hello from Relay"},
			),
			MaxSteps: 1,
		}

		got, err := engine.Execute(
			context.Background(),
			&r,
			model.Request{
				Messages: []model.Message{
					{Role: model.RoleUser, Content: "Hello"},
				},
			},
		)
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}

		if got.Text != "Hello from Relay" {
			t.Fatalf("response text = %q, want %q", got.Text, "Hello from Relay")
		}

		if r.Status != run.StatusSucceeded {
			t.Fatalf("run status = %q, want %q", r.Status, run.StatusSucceeded)
		}
	})

	t.Run("marks the run failed when the model fails", func(t *testing.T) {
		r := run.New("run-123")
		engine := Engine{
			Client:   model.NewScriptedClient(),
			MaxSteps: 1,
		}

		_, err := engine.Execute(context.Background(), &r, model.Request{})
		if !errors.Is(err, model.ErrNoResponses) {
			t.Fatalf("Execute() error = %v, want ErrNoResponses", err)
		}

		if r.Status != run.StatusFailed {
			t.Fatalf("run status = %q, want %q", r.Status, run.StatusFailed)
		}
	})

	t.Run("cancels the run when the context is canceled", func(t *testing.T) {
		r := run.New("run-123")
		engine := Engine{
			Client:   model.NewScriptedClient(),
			MaxSteps: 1,
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := engine.Execute(ctx, &r, model.Request{})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Execute() error = %v, want context.Canceled", err)
		}

		if r.Status != run.StatusCanceled {
			t.Fatalf("run status = %q, want %q", r.Status, run.StatusCanceled)
		}
	})

	t.Run("executes a tool call before the final response", func(t *testing.T) {
		lookup := tool.NewCustomerLookup(tool.Customer{
			ID:   "cust_123",
			Name: "Ada Lovelace",
			Plan: "pro",
		})

		registry, err := tool.NewRegistry(lookup)
		if err != nil {
			t.Fatalf("NewRegistry() error = %v", err)
		}

		client := model.NewScriptedClient(
			model.Response{
				ToolCalls: []tool.Call{
					{
						ID:        "call_123",
						Name:      "lookup_customer",
						Arguments: json.RawMessage(`{"customer_id":"cust_123"}`),
					},
				},
			},
			model.Response{Text: "Ada Lovelace is on the pro plan."},
		)

		r := run.New("run-123")
		engine := Engine{
			Client:   client,
			Tools:    registry,
			MaxSteps: 2,
		}

		response, err := engine.Execute(context.Background(), &r, model.Request{
			Messages: []model.Message{
				{Role: model.RoleUser, Content: "What plan is cust_123 on?"},
			},
			Tools: []tool.Spec{lookup.Spec()},
		})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}

		if response.Text != "Ada Lovelace is on the pro plan." {
			t.Errorf("response text = %q, want final answer", response.Text)
		}

		if r.Status != run.StatusSucceeded {
			t.Errorf("run status = %q, want %q", r.Status, run.StatusSucceeded)
		}

		requests := client.Requests()
		if len(requests) != 2 {
			t.Fatalf("recorded requests = %d, want 2", len(requests))
		}

		secondRequest := requests[1]
		if len(secondRequest.Messages) != 3 {
			t.Fatalf("second request messages = %d, want 3", len(secondRequest.Messages))
		}

		assistant := secondRequest.Messages[1]
		if assistant.Role != model.RoleAssistant {
			t.Errorf("assistant message role = %q, want %q", assistant.Role, model.RoleAssistant)
		}
		if len(assistant.ToolCalls) != 1 {
			t.Fatalf("assistant tool calls = %d, want 1", len(assistant.ToolCalls))
		}
		if assistant.ToolCalls[0].ID != "call_123" {
			t.Errorf("assistant tool call ID = %q, want %q", assistant.ToolCalls[0].ID, "call_123")
		}

		toolMessage := secondRequest.Messages[2]
		if toolMessage.Role != model.RoleTool {
			t.Errorf("tool message role = %q, want %q", toolMessage.Role, model.RoleTool)
		}
		if toolMessage.ToolCallID != "call_123" {
			t.Errorf("tool message call ID = %q, want %q", toolMessage.ToolCallID, "call_123")
		}
		if toolMessage.ToolName != "lookup_customer" {
			t.Errorf("tool message name = %q, want %q", toolMessage.ToolName, "lookup_customer")
		}

		var customer tool.Customer
		if err := json.Unmarshal([]byte(toolMessage.Content), &customer); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		if customer.ID != "cust_123" {
			t.Errorf("tool-result customer ID = %q, want %q", customer.ID, "cust_123")
		}
	})

	t.Run("fails when the step limit is exhausted", func(t *testing.T) {
		lookup := tool.NewCustomerLookup(tool.Customer{
			ID:   "cust_123",
			Name: "Ada Lovelace",
			Plan: "pro",
		})

		registry, err := tool.NewRegistry(lookup)
		if err != nil {
			t.Fatalf("NewRegistry() error = %v", err)
		}

		client := model.NewScriptedClient(model.Response{
			ToolCalls: []tool.Call{
				{
					ID:        "call_123",
					Name:      "lookup_customer",
					Arguments: json.RawMessage(`{"customer_id":"cust_123"}`),
				},
			},
		})

		r := run.New("run-123")
		engine := Engine{
			Client:   client,
			Tools:    registry,
			MaxSteps: 1,
		}

		_, err = engine.Execute(context.Background(), &r, model.Request{})
		if !errors.Is(err, ErrStepLimitExceeded) {
			t.Fatalf("Execute() error = %v, want ErrStepLimitExceeded", err)
		}

		if r.Status != run.StatusFailed {
			t.Errorf("run status = %q, want %q", r.Status, run.StatusFailed)
		}

		if len(client.Requests()) != 1 {
			t.Errorf("recorded requests = %d, want 1", len(client.Requests()))
		}
	})
}
