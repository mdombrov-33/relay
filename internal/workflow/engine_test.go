package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/mdombrov-33/relay/internal/model"
	"github.com/mdombrov-33/relay/internal/run"
)

func TestEngineExecute(t *testing.T) {
	t.Run("returns the model response and succeeds the run", func(t *testing.T) {
		r := run.New("run-123")
		engine := Engine{
			Client: model.NewScriptedClient(
				model.Response{Text: "Hello from Relay"},
			),
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
			Client: model.NewScriptedClient(),
		}

		_, err := engine.Execute(context.Background(), &r, model.Request{})
		if !errors.Is(err, model.ErrNoResponses) {
			t.Fatalf("Execute() error = %v, want ErrNoResponses", err)
		}

		if r.Status != run.StatusFailed {
			t.Fatalf("run status = %q, want %q", r.Status, run.StatusFailed)
		}
	})
}
