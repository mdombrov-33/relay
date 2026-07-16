package tool

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestIncidentLookupExecutes(t *testing.T) {
	lookup := NewIncidentLookup(
		Incident{
			ID:         "inc_123",
			CustomerID: "cust_123",
			Summary:    "Service outage",
			Status:     "resolved",
		},
	)

	arguments, err := json.Marshal(lookupIncidentArgs{
		IncidentID: "inc_123",
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	result, err := lookup.Execute(context.Background(), Execution{Call: Call{
		ID:        "call_123",
		Name:      "lookup_incident",
		Arguments: arguments,
	}})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var incident Incident
	if err := json.Unmarshal([]byte(result.Content), &incident); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if incident.Status != "resolved" {
		t.Errorf("incident.Status = %q, want %q", incident.Status, "resolved")
	}
}

func TestIncidentLookupRejectsMalformedArguments(t *testing.T) {
	lookup := NewIncidentLookup()

	_, err := lookup.Execute(context.Background(), Execution{Call: Call{
		Name:      "lookup_incident",
		Arguments: json.RawMessage(`{"incident_id":`),
	}})

	if !errors.Is(err, ErrInvalidArguments) {
		t.Errorf("Execute() error = %v, want ErrInvalidArguments", err)
	}
}

func TestIncidentLookupRejectsMissingIncidentID(t *testing.T) {
	lookup := NewIncidentLookup()

	_, err := lookup.Execute(context.Background(), Execution{Call: Call{
		Name:      "lookup_incident",
		Arguments: json.RawMessage(`{}`),
	}})

	if !errors.Is(err, ErrInvalidArguments) {
		t.Errorf("Execute() error = %v, want ErrInvalidArguments", err)
	}
}

func TestIncidentLookupReturnsNotFoundError(t *testing.T) {
	lookup := NewIncidentLookup()

	arguments, err := json.Marshal(lookupIncidentArgs{
		IncidentID: "missing",
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	_, err = lookup.Execute(context.Background(), Execution{Call: Call{
		Name:      "lookup_incident",
		Arguments: arguments,
	}})

	if !errors.Is(err, ErrIncidentNotFound) {
		t.Errorf("Execute() error = %v, want ErrIncidentNotFound", err)
	}
}
