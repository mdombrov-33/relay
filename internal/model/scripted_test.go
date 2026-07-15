package model

import (
	"context"
	"errors"
	"testing"
)

func TestScriptedClientReturnsResponsesInOrder(t *testing.T) {
	client := NewScriptedClient(
		Response{Text: "first"},
		Response{Text: "second"},
	)

	for _, want := range []string{"first", "second"} {
		got, err := client.Next(context.Background(), Request{})
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}

		if got.Text != want {
			t.Fatalf("Next() text = %q, want %q", got.Text, want)
		}
	}

	_, err := client.Next(context.Background(), Request{})
	if !errors.Is(err, ErrNoResponses) {
		t.Fatalf("Next() error = %v, want ErrNoResponses", err)
	}
}

func TestScriptedClientHonorsCancelledContext(t *testing.T) {
	client := NewScriptedClient(Response{Text: "unused"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.Next(ctx, Request{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Next() error = %v, want context.Canceled", err)
	}
}
