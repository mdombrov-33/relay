//go:build integration

package workflow_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mdombrov-33/relay/internal/event"
	"github.com/mdombrov-33/relay/internal/model"
	"github.com/mdombrov-33/relay/internal/policy"
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

func (t *countingTool) Execute(context.Context, tool.Execution) (tool.Output, error) {
	t.calls++
	return t.output, nil
}

var errInterruptedBeforeToolCheckpoint = errors.New("interrupted before tool checkpoint")

type failFirstCompleteStore struct {
	*postgres.Store
	failed      bool
	failStepKey run.StepKey
}

var _ workflow.CheckpointStore = (*failFirstCompleteStore)(nil)

func (s *failFirstCompleteStore) CompleteStep(ctx context.Context, runID run.ID, stepKey run.StepKey, inputHash [sha256.Size]byte, attempt int, result json.RawMessage, completedAt time.Time) (postgres.StepCheckpoint, error) {
	if !s.failed && stepKey == s.failStepKey {
		s.failed = true
		return postgres.StepCheckpoint{}, errInterruptedBeforeToolCheckpoint
	}

	return s.Store.CompleteStep(ctx, runID, stepKey, inputHash, attempt, result, completedAt)
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
			Authority:   tool.AuthorityRead,
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
		engine.ToolPolicy = policy.NewAllowlist(tool.AuthorityRead)
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
	engine.ToolPolicy = policy.NewAllowlist(tool.AuthorityRead)
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

func TestEngineExecuteRetriesInterruptedIssueCreditAsOneLogicalEffect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := run.New(run.ID(fmt.Sprintf("run-workflow-issue-credit-%d", time.Now().UnixNano())))
	call := tool.Call{
		ID:        "call_credit",
		Name:      "issue_credit",
		Arguments: json.RawMessage(`{"customer_id":"cust_123","incident_id":"inc_123","amount_cents":500}`),
	}
	request := model.Request{
		Messages: []model.Message{{Role: model.RoleUser, Content: "Issue a $5 support credit."}},
		Tools:    []tool.Spec{{Name: call.Name, Description: "Issues a synthetic support credit", Authority: tool.AuthorityEffect}},
	}

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

		store := postgres.NewStore(pool)
		issueCredit := tool.NewIssueCredit(store)
		registry, err := tool.NewRegistry(issueCredit)
		if err != nil {
			t.Fatalf("create tool registry: %v", err)
		}

		runner := workflow.StepRunner{Store: &failFirstCompleteStore{Store: store, failStepKey: run.StepKey("tool/1/call_credit")}}
		engine := newCheckpointedEngine(model.NewScriptedClient(model.Response{ToolCalls: []tool.Call{call}}), &runner)
		engine.Tools = registry
		engine.ToolPolicy = policy.NewAllowlist(tool.AuthorityEffect)
		engine.MaxSteps = 2
		if _, err := engine.Execute(ctx, &r, request); !errors.Is(err, errInterruptedBeforeToolCheckpoint) {
			t.Fatalf("first Execute() error = %v, want %v", err, errInterruptedBeforeToolCheckpoint)
		}

		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM effects WHERE run_id = $1", r.ID).Scan(&count); err != nil {
			t.Fatalf("count first effects: %v", err)
		}
		if count != 1 {
			t.Fatalf("first effect count = %d, want 1", count)
		}
	}()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	store := postgres.NewStore(pool)
	issueCredit := tool.NewIssueCredit(store)
	registry, err := tool.NewRegistry(issueCredit)
	if err != nil {
		t.Fatalf("create recovered tool registry: %v", err)
	}
	recoveredClient := model.NewScriptedClient(model.Response{Text: "Issued the $5 support credit."})
	runner := workflow.StepRunner{Store: store, Recover: true}
	engine := newCheckpointedEngine(recoveredClient, &runner)
	engine.Tools = registry
	engine.ToolPolicy = policy.NewAllowlist(tool.AuthorityEffect)
	engine.MaxSteps = 2

	recoveredRun := run.New(r.ID)
	response, err := engine.Execute(ctx, &recoveredRun, request)
	if err != nil {
		t.Fatalf("recovered Execute() error = %v", err)
	}
	if response.Text != "Issued the $5 support credit." {
		t.Errorf("recovered response = %q, want final response", response.Text)
	}
	if len(recoveredClient.Requests()) != 1 {
		t.Errorf("recovered model requests = %d, want 1 for the uncached final response", len(recoveredClient.Requests()))
	}

	var (
		count   int
		attempt int
		status  postgres.StepStatus
		result  []byte
	)
	if err := pool.QueryRow(
		ctx,
		`SELECT count(*) FROM effects WHERE run_id = $1`,
		r.ID,
	).Scan(&count); err != nil {
		t.Fatalf("count recovered effects: %v", err)
	}
	if count != 1 {
		t.Errorf("recovered effect count = %d, want 1", count)
	}
	if err := pool.QueryRow(
		ctx,
		`SELECT attempt, status, result FROM steps WHERE run_id = $1 AND step_key = $2`,
		r.ID,
		run.StepKey("tool/1/call_credit"),
	).Scan(&attempt, &status, &result); err != nil {
		t.Fatalf("read recovered tool checkpoint: %v", err)
	}
	if attempt != 2 || status != postgres.StepStatusCompleted {
		t.Errorf("tool checkpoint = (attempt %d, status %q), want recovered completed attempt 2", attempt, status)
	}

	var output tool.Output
	if err := json.Unmarshal(result, &output); err != nil {
		t.Fatalf("decode recovered tool checkpoint: %v", err)
	}

	var credit tool.Credit
	if err := json.Unmarshal([]byte(output.Content), &credit); err != nil {
		t.Fatalf("decode recovered credit: %v", err)
	}
	if credit.ID != "issue_credit/"+string(r.ID)+"/tool/1/call_credit" || credit.AmountCents != 500 {
		t.Errorf("recovered credit = %#v, want one stable $5 credit", credit)
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
