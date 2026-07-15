package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

var (
	ErrInvalidArguments = errors.New("invalid tool arguments")
	ErrCustomerNotFound = errors.New("customer not found")
)

type Customer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Plan string `json:"plan"`
}

type lookupCustomerArgs struct {
	CustomerID string `json:"customer_id"`
}

type CustomerLookup struct {
	customers map[string]Customer
}

var _ Tool = (*CustomerLookup)(nil)

func NewCustomerLookup(customers ...Customer) *CustomerLookup {
	byID := make(map[string]Customer, len(customers))

	for _, customer := range customers {
		byID[customer.ID] = customer
	}

	return &CustomerLookup{
		customers: byID,
	}
}

func (t *CustomerLookup) Spec() Spec {
	return Spec{
		Name:        "lookup_customer",
		Description: "Looks up a customer by ID",
	}
}

func (t *CustomerLookup) Execute(ctx context.Context, call Call) (Output, error) {
	if err := ctx.Err(); err != nil {
		return Output{}, err
	}

	var arguments lookupCustomerArgs
	if err := json.Unmarshal(call.Arguments, &arguments); err != nil {
		return Output{}, fmt.Errorf("%w: %w", ErrInvalidArguments, err)
	}
	if arguments.CustomerID == "" {
		return Output{}, fmt.Errorf("%w: customer_id is required", ErrInvalidArguments)
	}

	customer, exists := t.customers[arguments.CustomerID]
	if !exists {
		return Output{}, fmt.Errorf("%w: %q", ErrCustomerNotFound, arguments.CustomerID)
	}

	content, err := json.Marshal(customer)
	if err != nil {
		return Output{}, fmt.Errorf("marshal customer result: %w", err)
	}

	return Output{
		Content: string(content),
	}, nil
}
