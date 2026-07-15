package tool

import (
	"context"
	"errors"
	"testing"
)

type fakeTool struct {
	spec Spec
}

func (f fakeTool) Spec() Spec {
	return f.spec
}

func (f fakeTool) Execute(context.Context, Call) (Output, error) {
	return Output{}, nil
}

var _ Tool = fakeTool{}

func TestRegistryLookup(t *testing.T) {
	customerLookup := fakeTool{
		spec: Spec{
			Name:        "lookup_customer",
			Description: "Looks up a customer",
		},
	}

	registry, err := NewRegistry(customerLookup)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	got, err := registry.Lookup("lookup_customer")
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}

	if got.Spec().Name != "lookup_customer" {
		t.Errorf("Lookup().Spec().Name = %q, want %q", got.Spec().Name, "lookup_customer")
	}
}

func TestNewRegistryRejectsDuplicateNames(t *testing.T) {
	first := fakeTool{spec: Spec{Name: "lookup_customer"}}
	second := fakeTool{spec: Spec{Name: "lookup_customer"}}

	_, err := NewRegistry(first, second)

	if !errors.Is(err, ErrDuplicateTool) {
		t.Errorf("NewRegistry() error = %v, want ErrDuplicateTool", err)
	}
}

func TestRegistryLookupReturnsNotFoundError(t *testing.T) {
	registry, err := NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}

	_, err = registry.Lookup("missing_tool")

	if !errors.Is(err, ErrToolNotFound) {
		t.Errorf("Lookup() error = %v, want ErrToolNotFound", err)
	}
}
