package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/mdombrov-33/relay/internal/event"
	"github.com/mdombrov-33/relay/internal/model"
	"github.com/mdombrov-33/relay/internal/run"
	"github.com/mdombrov-33/relay/internal/tool"
)

type blockingClient struct{}

var _ model.Client = blockingClient{}

func (blockingClient) Next(ctx context.Context, _ model.Request) (model.Response, error) {
	<-ctx.Done()
	return model.Response{}, ctx.Err()
}

type blockingTool struct {
	spec tool.Spec
}

var _ tool.Tool = blockingTool{}

func (t blockingTool) Spec() tool.Spec {
	return t.spec
}

func (blockingTool) Execute(ctx context.Context, _ tool.Call) (tool.Output, error) {
	<-ctx.Done()
	return tool.Output{}, ctx.Err()
}

func TestEngineExecute(t *testing.T) {
	t.Run("requires an event log before starting the run", func(t *testing.T) {
		r := run.New("run-123")
		engine := Engine{
			Client:       model.NewScriptedClient(model.Response{Text: "Hello from Relay"}),
			MaxSteps:     1,
			ModelTimeout: time.Second,
			ToolTimeout:  time.Second,
		}

		_, err := engine.Execute(context.Background(), &r, model.Request{})
		if !errors.Is(err, ErrEventsNotConfigured) {
			t.Fatalf("Execute() error = %v, want ErrEventsNotConfigured", err)
		}
		if r.Status != run.StatusPending {
			t.Errorf("run status = %q, want %q", r.Status, run.StatusPending)
		}
	})

	t.Run("returns the model response and succeeds the run", func(t *testing.T) {
		r := run.New("run-123")
		events := event.NewLog()
		engine := Engine{
			Client: model.NewScriptedClient(
				model.Response{Text: "Hello from Relay"},
			),
			Events:       events,
			MaxSteps:     1,
			ModelTimeout: time.Second,
			ToolTimeout:  time.Second,
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

		assertEventTypes(t, events.Events(),
			event.TypeWorkflowStarted,
			event.TypeModelRequested,
			event.TypeModelCompleted,
			event.TypeWorkflowCompleted,
		)
	})

	t.Run("marks the run failed when the model fails", func(t *testing.T) {
		r := run.New("run-123")
		events := event.NewLog()
		engine := Engine{
			Client:       model.NewScriptedClient(),
			Events:       events,
			MaxSteps:     1,
			ModelTimeout: time.Second,
			ToolTimeout:  time.Second,
		}

		_, err := engine.Execute(context.Background(), &r, model.Request{})
		if !errors.Is(err, model.ErrNoResponses) {
			t.Fatalf("Execute() error = %v, want ErrNoResponses", err)
		}

		if r.Status != run.StatusFailed {
			t.Fatalf("run status = %q, want %q", r.Status, run.StatusFailed)
		}

		assertEventTypes(t, events.Events(),
			event.TypeWorkflowStarted,
			event.TypeModelRequested,
			event.TypeModelFailed,
			event.TypeWorkflowFailed,
		)
	})

	t.Run("cancels the run when the context is canceled", func(t *testing.T) {
		r := run.New("run-123")
		events := event.NewLog()
		engine := Engine{
			Client:       model.NewScriptedClient(),
			Events:       events,
			MaxSteps:     1,
			ModelTimeout: time.Second,
			ToolTimeout:  time.Second,
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

		assertEventTypes(t, events.Events(),
			event.TypeWorkflowStarted,
			event.TypeWorkflowCancelled,
		)
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
		events := event.NewLog()
		engine := Engine{
			Client:       client,
			Events:       events,
			Tools:        registry,
			MaxSteps:     2,
			ModelTimeout: time.Second,
			ToolTimeout:  time.Second,
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

		assertEventTypes(t, events.Events(),
			event.TypeWorkflowStarted,
			event.TypeModelRequested,
			event.TypeModelCompleted,
			event.TypeToolRequested,
			event.TypeToolCompleted,
			event.TypeModelRequested,
			event.TypeModelCompleted,
			event.TypeWorkflowCompleted,
		)

		toolEvent := events.Events()[3]
		var payload event.ToolPayload
		if err := json.Unmarshal(toolEvent.Payload(), &payload); err != nil {
			t.Fatalf("decode tool requested payload: %v", err)
		}
		if payload.CallID != "call_123" {
			t.Errorf("tool requested call ID = %q, want %q", payload.CallID, "call_123")
		}
		if payload.ToolName != "lookup_customer" {
			t.Errorf("tool requested name = %q, want %q", payload.ToolName, "lookup_customer")
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
			Client:       client,
			Events:       event.NewLog(),
			Tools:        registry,
			MaxSteps:     1,
			ModelTimeout: time.Second,
			ToolTimeout:  time.Second,
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

	t.Run("executes sequential tool calls before the final response", func(t *testing.T) {
		customerLookup := tool.NewCustomerLookup(tool.Customer{
			ID:   "cust_123",
			Name: "Ada Lovelace",
			Plan: "pro",
		})
		incidentLookup := tool.NewIncidentLookup(tool.Incident{
			ID:         "inc_123",
			CustomerID: "cust_123",
			Summary:    "Service outage",
			Status:     "resolved",
		})

		registry, err := tool.NewRegistry(customerLookup, incidentLookup)
		if err != nil {
			t.Fatalf("NewRegistry() error = %v", err)
		}

		client := model.NewScriptedClient(
			model.Response{
				ToolCalls: []tool.Call{
					{
						ID:        "call_customer",
						Name:      "lookup_customer",
						Arguments: json.RawMessage(`{"customer_id":"cust_123"}`),
					},
				},
			},
			model.Response{
				ToolCalls: []tool.Call{
					{
						ID:        "call_incident",
						Name:      "lookup_incident",
						Arguments: json.RawMessage(`{"incident_id":"inc_123"}`),
					},
				},
			},
			model.Response{Text: "Ada's resolved service outage is eligible for review."},
		)

		r := run.New("run-123")
		events := event.NewLog()
		engine := Engine{
			Client:       client,
			Events:       events,
			Tools:        registry,
			MaxSteps:     3,
			ModelTimeout: time.Second,
			ToolTimeout:  time.Second,
		}

		response, err := engine.Execute(context.Background(), &r, model.Request{
			Messages: []model.Message{
				{Role: model.RoleUser, Content: "Check customer cust_123 and incident inc_123."},
			},
			Tools: []tool.Spec{
				customerLookup.Spec(),
				incidentLookup.Spec(),
			},
		})
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}

		if response.Text != "Ada's resolved service outage is eligible for review." {
			t.Errorf("response text = %q, want final answer", response.Text)
		}

		if r.Status != run.StatusSucceeded {
			t.Errorf("run status = %q, want %q", r.Status, run.StatusSucceeded)
		}

		requests := client.Requests()
		if len(requests) != 3 {
			t.Fatalf("recorded requests = %d, want 3", len(requests))
		}

		thirdRequest := requests[2]
		if len(thirdRequest.Messages) != 5 {
			t.Fatalf("third request messages = %d, want 5", len(thirdRequest.Messages))
		}

		if got := thirdRequest.Messages[3].ToolCalls[0].Name; got != "lookup_incident" {
			t.Errorf("second assistant tool name = %q, want %q", got, "lookup_incident")
		}

		incidentMessage := thirdRequest.Messages[4]
		if incidentMessage.Role != model.RoleTool {
			t.Errorf("incident result role = %q, want %q", incidentMessage.Role, model.RoleTool)
		}
		if incidentMessage.ToolCallID != "call_incident" {
			t.Errorf("incident result call ID = %q, want %q", incidentMessage.ToolCallID, "call_incident")
		}
		if incidentMessage.ToolName != "lookup_incident" {
			t.Errorf("incident result name = %q, want %q", incidentMessage.ToolName, "lookup_incident")
		}

		var incident tool.Incident
		if err := json.Unmarshal([]byte(incidentMessage.Content), &incident); err != nil {
			t.Fatalf("json.Unmarshal() error = %v", err)
		}
		if incident.ID != "inc_123" {
			t.Errorf("incident result ID = %q, want %q", incident.ID, "inc_123")
		}

		assertEventTypes(t, events.Events(),
			event.TypeWorkflowStarted,
			event.TypeModelRequested,
			event.TypeModelCompleted,
			event.TypeToolRequested,
			event.TypeToolCompleted,
			event.TypeModelRequested,
			event.TypeModelCompleted,
			event.TypeToolRequested,
			event.TypeToolCompleted,
			event.TypeModelRequested,
			event.TypeModelCompleted,
			event.TypeWorkflowCompleted,
		)
		assertEventCorrelation(t, events.Events(), r.ID,
			run.StepKey("workflow"),
			run.StepKey("model/1"),
			run.StepKey("model/1"),
			run.StepKey("tool/1/call_customer"),
			run.StepKey("tool/1/call_customer"),
			run.StepKey("model/2"),
			run.StepKey("model/2"),
			run.StepKey("tool/2/call_incident"),
			run.StepKey("tool/2/call_incident"),
			run.StepKey("model/3"),
			run.StepKey("model/3"),
			run.StepKey("workflow"),
		)
	})

	t.Run("fails the run when a model call times out", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			r := run.New("run-123")
			engine := Engine{
				Client:       blockingClient{},
				Events:       event.NewLog(),
				MaxSteps:     1,
				ModelTimeout: time.Second,
				ToolTimeout:  time.Second,
			}

			_, err := engine.Execute(t.Context(), &r, model.Request{})
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Execute() error = %v, want context.DeadlineExceeded", err)
			}

			if r.Status != run.StatusFailed {
				t.Errorf("run status = %q, want %q", r.Status, run.StatusFailed)
			}
		})
	})

	t.Run("fails the run when a tool call times out", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			blocking := blockingTool{
				spec: tool.Spec{
					Name:        "blocking_tool",
					Description: "Blocks until its context ends",
				},
			}

			registry, err := tool.NewRegistry(blocking)
			if err != nil {
				t.Fatalf("NewRegistry() error = %v", err)
			}

			client := model.NewScriptedClient(model.Response{
				ToolCalls: []tool.Call{
					{
						ID:        "call_123",
						Name:      "blocking_tool",
						Arguments: json.RawMessage(`{}`),
					},
				},
			})

			r := run.New("run-123")
			events := event.NewLog()
			engine := Engine{
				Client:       client,
				Events:       events,
				Tools:        registry,
				MaxSteps:     1,
				ModelTimeout: time.Second,
				ToolTimeout:  time.Second,
			}

			_, err = engine.Execute(t.Context(), &r, model.Request{})
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("Execute() error = %v, want context.DeadlineExceeded", err)
			}

			if r.Status != run.StatusFailed {
				t.Errorf("run status = %q, want %q", r.Status, run.StatusFailed)
			}

			if len(client.Requests()) != 1 {
				t.Errorf("recorded requests = %d, want 1", len(client.Requests()))
			}

			assertEventTypes(t, events.Events(),
				event.TypeWorkflowStarted,
				event.TypeModelRequested,
				event.TypeModelCompleted,
				event.TypeToolRequested,
				event.TypeToolFailed,
				event.TypeWorkflowFailed,
			)
		})
	})
}

func TestEngineExecuteLosesProgressAfterRestart(t *testing.T) {
	newProcess := func() (Engine, *model.ScriptedClient, run.Run, model.Request) {
		customerLookup := tool.NewCustomerLookup(tool.Customer{
			ID:   "cust_123",
			Name: "Ada Lovelace",
			Plan: "pro",
		})
		incidentLookup := tool.NewIncidentLookup(tool.Incident{
			ID:         "inc_123",
			CustomerID: "cust_123",
			Summary:    "Service outage",
			Status:     "resolved",
		})

		registry, err := tool.NewRegistry(customerLookup, incidentLookup)
		if err != nil {
			t.Fatalf("NewRegistry() error = %v", err)
		}

		client := model.NewScriptedClient(
			model.Response{
				ToolCalls: []tool.Call{
					{
						ID:        "call_customer",
						Name:      "lookup_customer",
						Arguments: json.RawMessage(`{"customer_id":"cust_123"}`),
					},
				},
			},
			model.Response{
				ToolCalls: []tool.Call{
					{
						ID:        "call_incident",
						Name:      "lookup_incident",
						Arguments: json.RawMessage(`{"incident_id":"inc_123"}`),
					},
				},
			},
			model.Response{Text: "Ada's resolved service outage is eligible for review."},
		)

		engine := Engine{
			Client:       client,
			Events:       event.NewLog(),
			Tools:        registry,
			MaxSteps:     3,
			ModelTimeout: time.Second,
			ToolTimeout:  time.Second,
		}

		r := run.New("run-123")
		request := model.Request{
			Messages: []model.Message{
				{Role: model.RoleUser, Content: "Check customer cust_123 and incident inc_123."},
			},
			Tools: []tool.Spec{
				customerLookup.Spec(),
				incidentLookup.Spec(),
			},
		}

		return engine, client, r, request
	}

	firstEngine, firstClient, firstRun, firstRequest := newProcess()

	_, err := firstEngine.Execute(context.Background(), &firstRun, firstRequest)
	if err != nil {
		t.Fatalf("first Execute() error = %v", err)
	}
	if firstRun.Status != run.StatusSucceeded {
		t.Fatalf("first run status = %q, want %q", firstRun.Status, run.StatusSucceeded)
	}
	if got := len(firstClient.Requests()); got != 3 {
		t.Fatalf("first process model requests = %d, want 3", got)
	}

	restartedEngine, restartedClient, restartedRun, restartedRequest := newProcess()

	if restartedRun.ID != firstRun.ID {
		t.Fatalf("restarted run ID = %q, want %q", restartedRun.ID, firstRun.ID)
	}
	if restartedRun.Status != run.StatusPending {
		t.Fatalf("restarted run status = %q, want %q", restartedRun.Status, run.StatusPending)
	}
	if got := len(restartedClient.Requests()); got != 0 {
		t.Fatalf("restarted process model requests = %d, want 0", got)
	}

	_, err = restartedEngine.Execute(context.Background(), &restartedRun, restartedRequest)
	if err != nil {
		t.Fatalf("restarted Execute() error = %v", err)
	}
	if got := len(restartedClient.Requests()); got != 3 {
		t.Errorf("restarted process model requests = %d, want 3", got)
	}
}

func assertEventTypes(t *testing.T, events []event.Envelope, expected ...event.Type) {
	t.Helper()

	if len(events) != len(expected) {
		t.Fatalf("event count = %d, want %d", len(events), len(expected))
	}

	for i, expectedType := range expected {
		if got := events[i].Type(); got != expectedType {
			t.Errorf("event %d type = %q, want %q", i, got, expectedType)
		}
	}
}

func assertEventCorrelation(t *testing.T, events []event.Envelope, runID run.ID, expected ...run.StepKey) {
	t.Helper()

	if len(events) != len(expected) {
		t.Fatalf("event count = %d, want %d", len(events), len(expected))
	}

	for i, expectedStepKey := range expected {
		if got := events[i].RunID(); got != runID {
			t.Errorf("event %d run ID = %q, want %q", i, got, runID)
		}
		if got := events[i].StepKey(); got != expectedStepKey {
			t.Errorf("event %d step key = %q, want %q", i, got, expectedStepKey)
		}
	}
}
