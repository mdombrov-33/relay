package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

var ErrIncidentNotFound = errors.New("incident not found")

type Incident struct {
	ID         string `json:"id"`
	CustomerID string `json:"customer_id"`
	Summary    string `json:"summary"`
	Status     string `json:"status"`
}

type lookupIncidentArgs struct {
	IncidentID string `json:"incident_id"`
}

type IncidentLookup struct {
	incidents map[string]Incident
}

var _ Tool = (*IncidentLookup)(nil)

func NewIncidentLookup(incidents ...Incident) *IncidentLookup {
	byID := make(map[string]Incident, len(incidents))

	for _, incident := range incidents {
		byID[incident.ID] = incident
	}

	return &IncidentLookup{
		incidents: byID,
	}
}

func (t *IncidentLookup) Spec() Spec {
	return Spec{
		Name:        "lookup_incident",
		Description: "Looks up an incident by ID",
	}
}

func (t *IncidentLookup) Execute(ctx context.Context, call Call) (Output, error) {
	if err := ctx.Err(); err != nil {
		return Output{}, err
	}

	var arguments lookupIncidentArgs
	if err := json.Unmarshal(call.Arguments, &arguments); err != nil {
		return Output{}, fmt.Errorf("%w: %w", ErrInvalidArguments, err)
	}
	if arguments.IncidentID == "" {
		return Output{}, fmt.Errorf("%w: incident_id is required", ErrInvalidArguments)
	}

	incident, exists := t.incidents[arguments.IncidentID]
	if !exists {
		return Output{}, fmt.Errorf("%w: %q", ErrIncidentNotFound, arguments.IncidentID)
	}

	content, err := json.Marshal(incident)
	if err != nil {
		return Output{}, fmt.Errorf("marshal incident result: %w", err)
	}

	return Output{
		Content: string(content),
	}, nil
}
