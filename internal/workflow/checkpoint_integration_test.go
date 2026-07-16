//go:build integration

package workflow_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mdombrov-33/relay/internal/event"
	"github.com/mdombrov-33/relay/internal/model"
	"github.com/mdombrov-33/relay/internal/postgres"
	"github.com/mdombrov-33/relay/internal/run"
	"github.com/mdombrov-33/relay/internal/workflow"
)

func TestEngineExecuteReadsCheckpointedModelResponseAfterPoolRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := run.New(run.ID(fmt.Sprintf("run-workflow-checkpoint-%d", time.Now().UnixNano())))
	request := model.Request{
		Messages: []model.Message{{Role: model.RoleUser, Content: "Summarize the incident."}},
	}
	firstClient := model.NewScriptedClient(model.Response{Text: "The incident is resolved."})

	func() {
		pool := openIntegrationPool(t, ctx)
		defer pool.Close()

		if _, err := pool.Exec(
			ctx,
			`INSERT INTO runs (id, status, created_at, updated_at)
			 VALUES ($1, $2, $3, $3)`,
			r.ID,
			r.Status,
			time.Now().UTC(),
		); err != nil {
			t.Fatalf("insert run: %v", err)
		}

		runner := workflow.StepRunner{Store: postgres.NewStore(pool)}
		engine := newCheckpointedEngine(firstClient, &runner)
		response, err := engine.Execute(ctx, &r, request)
		if err != nil {
			t.Fatalf("first Execute() error = %v", err)
		}
		if response.Text != "The incident is resolved." {
			t.Errorf("first response text = %q, want model response", response.Text)
		}
	}()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	recoveredRun := run.New(r.ID)
	recoveredClient := model.NewScriptedClient()
	runner := workflow.StepRunner{Store: postgres.NewStore(pool), Recover: true}
	engine := newCheckpointedEngine(recoveredClient, &runner)
	response, err := engine.Execute(ctx, &recoveredRun, request)
	if err != nil {
		t.Fatalf("recovered Execute() error = %v", err)
	}
	if response.Text != "The incident is resolved." {
		t.Errorf("recovered response text = %q, want checkpointed model response", response.Text)
	}
	if len(recoveredClient.Requests()) != 0 {
		t.Errorf("recovered model requests = %d, want 0", len(recoveredClient.Requests()))
	}
}

func newCheckpointedEngine(client model.Client, runner *workflow.StepRunner) workflow.Engine {
	return workflow.Engine{
		Client:       client,
		Events:       event.NewLog(),
		MaxSteps:     1,
		ModelTimeout: time.Second,
		ToolTimeout:  time.Second,
		Checkpoints:  runner,
	}
}

func openIntegrationPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL must be set for PostgreSQL integration tests")
	}

	pool, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open PostgreSQL pool: %v", err)
	}

	return pool
}
