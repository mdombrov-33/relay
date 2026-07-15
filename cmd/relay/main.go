package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/mdombrov-33/relay/internal/model"
	"github.com/mdombrov-33/relay/internal/run"
	"github.com/mdombrov-33/relay/internal/tool"
	"github.com/mdombrov-33/relay/internal/workflow"
)

func main() {
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

	engine := workflow.Engine{
		Client:       client,
		Tools:        tools,
		MaxSteps:     3,
		ModelTimeout: time.Second,
		ToolTimeout:  time.Second,
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
}
