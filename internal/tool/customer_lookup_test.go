package tool

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestCustomerLookupExecutes(t *testing.T) {
	lookup := NewCustomerLookup(
		Customer{
			ID:   "cust_123",
			Name: "Ada Lovelace",
			Plan: "pro",
		},
	)

	arguments, err := json.Marshal(lookupCustomerArgs{
		CustomerID: "cust_123",
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	result, err := lookup.Execute(context.Background(), Call{
		ID:        "call_123",
		Name:      "lookup_customer",
		Arguments: arguments,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	var customer Customer
	if err := json.Unmarshal([]byte(result.Content), &customer); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if customer.Name != "Ada Lovelace" {
		t.Errorf("customer.Name = %q, want %q", customer.Name, "Ada Lovelace")
	}
}

func TestCustomerLookupRejectsMalformedArguments(t *testing.T) {
	lookup := NewCustomerLookup()

	_, err := lookup.Execute(context.Background(), Call{
		Name:      "lookup_customer",
		Arguments: json.RawMessage(`{"customer_id":`),
	})

	if !errors.Is(err, ErrInvalidArguments) {
		t.Errorf("Execute() error = %v, want ErrInvalidArguments", err)
	}
}

func TestCustomerLookupReturnsNotFoundError(t *testing.T) {
	lookup := NewCustomerLookup()

	arguments, err := json.Marshal(lookupCustomerArgs{
		CustomerID: "missing",
	})
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	_, err = lookup.Execute(context.Background(), Call{
		Name:      "lookup_customer",
		Arguments: arguments,
	})

	if !errors.Is(err, ErrCustomerNotFound) {
		t.Errorf("Execute() error = %v, want ErrCustomerNotFound", err)
	}
}
