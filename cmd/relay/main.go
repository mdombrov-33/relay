package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/mdombrov-33/relay/internal/event"
	"github.com/mdombrov-33/relay/internal/model"
	"github.com/mdombrov-33/relay/internal/policy"
	"github.com/mdombrov-33/relay/internal/run"
	"github.com/mdombrov-33/relay/internal/tool"
	"github.com/mdombrov-33/relay/internal/workflow"
)

func main() {
	scenario := flag.String("scenario", "success", "demo scenario: success or failed")
	flag.Parse()

	switch *scenario {
	case "success":
		runSuccessfulDemo()
	case "failed":
		runFailedDemo()
	default:
		log.Fatalf("unknown demo scenario %q", *scenario)
	}
}

func runSuccessfulDemo() {
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

	tools, err := tool.NewRegistry(customerLookup, incidentLookup)
	if err != nil {
		log.Fatalf("create tool registry: %v", err)
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

	events := event.NewLog()
	engine := workflow.Engine{
		Client:             client,
		Events:             events,
		Tools:              tools,
		ToolPolicy:         policy.NewAllowlist(tool.AuthorityRead),
		MaxSteps:           3,
		ModelTimeout:       time.Second,
		ToolTimeout:        time.Second,
		ContextBudgetBytes: workflow.DefaultContextBudgetBytes,
	}

	r := run.New("demo-001")
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
		log.Fatalf("execute demo workflow: %v", err)
	}

	fmt.Println(response.Text)
	fmt.Print(formatTimeline(events.Events()))
}

func runFailedDemo() {
	events := event.NewLog()
	engine := workflow.Engine{
		Client:             model.NewScriptedClient(),
		Events:             events,
		MaxSteps:           1,
		ModelTimeout:       time.Second,
		ToolTimeout:        time.Second,
		ContextBudgetBytes: workflow.DefaultContextBudgetBytes,
	}

	r := run.New("demo-failed-001")
	_, err := engine.Execute(context.Background(), &r, model.Request{
		Messages: []model.Message{
			{Role: model.RoleUser, Content: "Demonstrate a failed workflow."},
		},
	})
	if err == nil {
		log.Fatal("failed demo workflow unexpectedly succeeded")
	}
	if r.Status != run.StatusFailed {
		log.Fatalf("failed demo run status = %q, want %q", r.Status, run.StatusFailed)
	}

	fmt.Printf("Workflow failed as expected: %v\n", err)
	fmt.Print(formatTimeline(events.Events()))
}

func formatTimeline(events []event.Envelope) string {
	var timeline strings.Builder
	timeline.WriteString("Event timeline:\n")

	for _, event := range events {
		timeline.WriteString(event.OccurredAt().Format(time.RFC3339))
		timeline.WriteByte(' ')
		timeline.WriteString(event.ID())
		timeline.WriteByte(' ')
		timeline.WriteString("run=")
		timeline.WriteString(string(event.RunID()))
		timeline.WriteByte(' ')
		timeline.WriteString("step=")
		timeline.WriteString(string(event.StepKey()))
		timeline.WriteByte(' ')
		timeline.WriteString(string(event.Type()))
		timeline.WriteByte(' ')
		timeline.Write(event.Payload())
		timeline.WriteByte('\n')
	}

	return timeline.String()
}
