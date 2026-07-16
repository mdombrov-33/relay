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
	"github.com/mdombrov-33/relay/internal/tool"
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

type countingTool struct {
	spec   tool.Spec
	output tool.Output
	calls  int
}

var _ tool.Tool = (*countingTool)(nil)

func (t *countingTool) Spec() tool.Spec {
	return t.spec
}

func (t *countingTool) Execute(context.Context, tool.Call) (tool.Output, error) {
	t.calls++
	return t.output, nil
}

func TestEngineExecuteReadsCheckpointedToolOutputAfterPoolRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := run.New(run.ID(fmt.Sprintf("run-workflow-tool-checkpoint-%d", time.Now().UnixNano())))
	call := tool.Call{
		ID:        "call_customer",
		Name:      "lookup_customer",
		Arguments: []byte(`{"customer_id":"cust_123"}`),
	}
	firstTool := &countingTool{
		spec: tool.Spec{
			Name:        call.Name,
			Description: "Looks up a customer",
		},
		output: tool.Output{Content: `{"customer":"Ada Lovelace"}`},
	}
	firstRegistry, err := tool.NewRegistry(firstTool)
	if err != nil {
		t.Fatalf("create first tool registry: %v", err)
	}
	request := model.Request{
		Messages: []model.Message{{Role: model.RoleUser, Content: "Find customer cust_123."}},
		Tools:    []tool.Spec{firstTool.Spec()},
	}
	firstClient := model.NewScriptedClient(
		model.Response{ToolCalls: []tool.Call{call}},
		model.Response{Text: "Ada Lovelace was found."},
	)

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
		engine.Tools = firstRegistry
		engine.MaxSteps = 2
		response, err := engine.Execute(ctx, &r, request)
		if err != nil {
			t.Fatalf("first Execute() error = %v", err)
		}
		if response.Text != "Ada Lovelace was found." {
			t.Errorf("first response text = %q, want model response", response.Text)
		}
		if firstTool.calls != 1 {
			t.Errorf("first tool calls = %d, want 1", firstTool.calls)
		}
	}()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	recoveredTool := &countingTool{spec: firstTool.Spec(), output: firstTool.output}
	recoveredRegistry, err := tool.NewRegistry(recoveredTool)
	if err != nil {
		t.Fatalf("create recovered tool registry: %v", err)
	}
	recoveredRun := run.New(r.ID)
	recoveredClient := model.NewScriptedClient()
	runner := workflow.StepRunner{Store: postgres.NewStore(pool), Recover: true}
	engine := newCheckpointedEngine(recoveredClient, &runner)
	engine.Tools = recoveredRegistry
	engine.MaxSteps = 2
	response, err := engine.Execute(ctx, &recoveredRun, request)
	if err != nil {
		t.Fatalf("recovered Execute() error = %v", err)
	}
	if response.Text != "Ada Lovelace was found." {
		t.Errorf("recovered response text = %q, want checkpointed model response", response.Text)
	}
	if len(recoveredClient.Requests()) != 0 {
		t.Errorf("recovered model requests = %d, want 0", len(recoveredClient.Requests()))
	}
	if recoveredTool.calls != 0 {
		t.Errorf("recovered tool calls = %d, want 0", recoveredTool.calls)
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
