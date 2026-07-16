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

type approvalRequiredPolicy struct{}

var _ workflow.ToolPolicy = approvalRequiredPolicy{}

func (approvalRequiredPolicy) Decide(tool.Spec) policy.Decision {
	return policy.DecisionRequireApproval
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

func TestEngineExecuteRunsApprovedToolAfterWaitingAcrossRestarts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := run.New(run.ID(fmt.Sprintf("run-workflow-approved-%d", time.Now().UnixNano())))
	call := tool.Call{
		ID:        "call_credit",
		Name:      "issue_credit",
		Arguments: json.RawMessage(`{"amount_cents":500}`),
	}
	executable := &countingTool{
		spec: tool.Spec{
			Name:        call.Name,
			Description: "Issues a synthetic support credit",
			Authority:   tool.AuthorityEffect,
		},
		output: tool.Output{Content: `{"credit":"issued"}`},
	}
	registry, err := tool.NewRegistry(executable)
	if err != nil {
		t.Fatalf("create tool registry: %v", err)
	}
	request := model.Request{
		Messages: []model.Message{{Role: model.RoleUser, Content: "Issue a $5 support credit."}},
		Tools:    []tool.Spec{executable.Spec()},
	}

	func() {
		pool := openIntegrationPool(t, ctx)
		defer pool.Close()
		insertRunningWorkflowRun(t, pool, ctx, r.ID)

		store := postgres.NewStore(pool)
		runner := workflow.StepRunner{Store: store}
		engine := newCheckpointedEngine(model.NewScriptedClient(model.Response{ToolCalls: []tool.Call{call}}), &runner)
		engine.Events = newIntegrationEventLog("approved-request")
		engine.Tools = registry
		engine.ToolPolicy = approvalRequiredPolicy{}
		engine.Approvals = &workflow.ApprovalGate{Store: store}
		engine.MaxSteps = 2

		response, err := engine.Execute(ctx, &r, request)
		if !errors.Is(err, workflow.ErrApprovalPending) {
			t.Fatalf("first Execute() error = %v, want %v", err, workflow.ErrApprovalPending)
		}
		if len(response.ToolCalls) != 1 || r.Status != run.StatusWaiting {
			t.Errorf("first execution = (%#v, status %q), want suspended tool call", response, r.Status)
		}
		if executable.calls != 0 {
			t.Errorf("tool calls before approval = %d, want 0", executable.calls)
		}
	}()

	func() {
		pool := openIntegrationPool(t, ctx)
		defer pool.Close()

		store := postgres.NewStore(pool)
		waitingClient := model.NewScriptedClient()
		runner := workflow.StepRunner{Store: store, Recover: true}
		engine := newCheckpointedEngine(waitingClient, &runner)
		engine.Tools = registry
		engine.ToolPolicy = approvalRequiredPolicy{}
		engine.Approvals = &workflow.ApprovalGate{Store: store}
		engine.MaxSteps = 2
		waitingRun := run.New(r.ID)

		if _, err := engine.Execute(ctx, &waitingRun, request); !errors.Is(err, workflow.ErrApprovalPending) {
			t.Fatalf("waiting Execute() error = %v, want %v", err, workflow.ErrApprovalPending)
		}
		if len(waitingClient.Requests()) != 0 {
			t.Errorf("model calls while still waiting = %d, want 0", len(waitingClient.Requests()))
		}
		if executable.calls != 0 {
			t.Errorf("tool calls while still waiting = %d, want 0", executable.calls)
		}

		resolveApprovalIntegration(t, pool, ctx, r.ID, postgres.ApprovalDecisionApproved)
	}()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	store := postgres.NewStore(pool)
	recoveredClient := model.NewScriptedClient(model.Response{Text: "Issued the $5 support credit."})
	runner := workflow.StepRunner{Store: store, Recover: true}
	engine := newCheckpointedEngine(recoveredClient, &runner)
	engine.Tools = registry
	engine.ToolPolicy = approvalRequiredPolicy{}
	engine.Approvals = &workflow.ApprovalGate{Store: store}
	engine.MaxSteps = 2
	recoveredRun := run.New(r.ID)

	response, err := engine.Execute(ctx, &recoveredRun, request)
	if err != nil {
		t.Fatalf("approved Execute() error = %v", err)
	}
	if response.Text != "Issued the $5 support credit." || recoveredRun.Status != run.StatusSucceeded {
		t.Errorf("approved execution = (%q, status %q), want completed response", response.Text, recoveredRun.Status)
	}
	if executable.calls != 1 {
		t.Errorf("approved tool calls = %d, want 1", executable.calls)
	}
	if len(recoveredClient.Requests()) != 1 {
		t.Fatalf("model calls after approval = %d, want only final turn", len(recoveredClient.Requests()))
	}
	messages := recoveredClient.Requests()[0].Messages
	if got := messages[len(messages)-1]; got.Role != model.RoleTool || got.Content != executable.output.Content {
		t.Errorf("approved tool message = %#v, want stored tool output", got)
	}

	var requestStatus postgres.ApprovalStatus
	var signalCount, eventCount int
	if err := pool.QueryRow(ctx, "SELECT status FROM approval_requests WHERE run_id = $1", r.ID).Scan(&requestStatus); err != nil {
		t.Fatalf("query approved request: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM approval_signals WHERE run_id = $1", r.ID).Scan(&signalCount); err != nil {
		t.Fatalf("count approved signals: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM events WHERE run_id = $1 AND type IN ($2, $3)", r.ID, event.TypeApprovalRequested, event.TypeApprovalResolved).Scan(&eventCount); err != nil {
		t.Fatalf("count approval events: %v", err)
	}
	if requestStatus != postgres.ApprovalStatusApproved || signalCount != 1 || eventCount != 2 {
		t.Errorf("durable approval = (%q, %d signals, %d events), want approved and one complete timeline", requestStatus, signalCount, eventCount)
	}
}

func TestEngineExecuteDoesNotRunRejectedToolAfterRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := run.New(run.ID(fmt.Sprintf("run-workflow-rejected-%d", time.Now().UnixNano())))
	call := tool.Call{ID: "call_credit", Name: "issue_credit", Arguments: json.RawMessage(`{"amount_cents":500}`)}
	executable := &countingTool{
		spec:   tool.Spec{Name: call.Name, Description: "Issues a synthetic support credit", Authority: tool.AuthorityEffect},
		output: tool.Output{Content: `{"credit":"issued"}`},
	}
	registry, err := tool.NewRegistry(executable)
	if err != nil {
		t.Fatalf("create tool registry: %v", err)
	}
	request := model.Request{
		Messages: []model.Message{{Role: model.RoleUser, Content: "Issue a $5 support credit."}},
		Tools:    []tool.Spec{executable.Spec()},
	}

	func() {
		pool := openIntegrationPool(t, ctx)
		defer pool.Close()
		insertRunningWorkflowRun(t, pool, ctx, r.ID)

		store := postgres.NewStore(pool)
		runner := workflow.StepRunner{Store: store}
		engine := newCheckpointedEngine(model.NewScriptedClient(model.Response{ToolCalls: []tool.Call{call}}), &runner)
		engine.Events = newIntegrationEventLog("rejected-request")
		engine.Tools = registry
		engine.ToolPolicy = approvalRequiredPolicy{}
		engine.Approvals = &workflow.ApprovalGate{Store: store}
		engine.MaxSteps = 2
		if _, err := engine.Execute(ctx, &r, request); !errors.Is(err, workflow.ErrApprovalPending) {
			t.Fatalf("first Execute() error = %v, want %v", err, workflow.ErrApprovalPending)
		}
	}()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	store := postgres.NewStore(pool)
	resolveApprovalIntegration(t, pool, ctx, r.ID, postgres.ApprovalDecisionRejected)
	recoveredClient := model.NewScriptedClient(model.Response{Text: "The credit was rejected for manual review."})
	runner := workflow.StepRunner{Store: store, Recover: true}
	engine := newCheckpointedEngine(recoveredClient, &runner)
	engine.Tools = registry
	engine.ToolPolicy = approvalRequiredPolicy{}
	engine.Approvals = &workflow.ApprovalGate{Store: store}
	engine.MaxSteps = 2
	recoveredRun := run.New(r.ID)

	response, err := engine.Execute(ctx, &recoveredRun, request)
	if err != nil {
		t.Fatalf("rejected Execute() error = %v", err)
	}
	if response.Text != "The credit was rejected for manual review." {
		t.Errorf("rejected response = %q, want final model response", response.Text)
	}
	if executable.calls != 0 {
		t.Errorf("rejected tool calls = %d, want 0", executable.calls)
	}
	if len(recoveredClient.Requests()) != 1 {
		t.Fatalf("model calls after rejection = %d, want only final turn", len(recoveredClient.Requests()))
	}
	messages := recoveredClient.Requests()[0].Messages
	if got := messages[len(messages)-1]; got.Role != model.RoleTool || got.Content != "tool call rejected by human reviewer" {
		t.Errorf("rejected tool message = %#v, want safe rejection", got)
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

func insertRunningWorkflowRun(t *testing.T, pool *pgxpool.Pool, ctx context.Context, runID run.ID) {
	t.Helper()

	now := time.Now().UTC()
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO runs (id, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $3)`,
		runID,
		run.StatusRunning,
		now,
	); err != nil {
		t.Fatalf("insert running run: %v", err)
	}
}

func resolveApprovalIntegration(t *testing.T, pool *pgxpool.Pool, ctx context.Context, runID run.ID, decision postgres.ApprovalDecision) {
	t.Helper()

	var (
		requestID string
		stepKey   run.StepKey
	)
	if err := pool.QueryRow(
		ctx,
		"SELECT id, step_key FROM approval_requests WHERE run_id = $1",
		runID,
	).Scan(&requestID, &stepKey); err != nil {
		t.Fatalf("query pending approval request: %v", err)
	}
	signal := postgres.ApprovalSignal{
		ID:        fmt.Sprintf("signal-%s-%d", decision, time.Now().UnixNano()),
		RequestID: requestID,
		RunID:     runID,
		Decision:  decision,
	}
	resolved, err := event.New(
		fmt.Sprintf("event-approval-resolved-%d", time.Now().UnixNano()),
		runID,
		stepKey,
		event.TypeApprovalResolved,
		time.Now().UTC(),
		event.ApprovalPayload{RequestID: requestID, Approved: decision == postgres.ApprovalDecisionApproved},
	)
	if err != nil {
		t.Fatalf("new approval resolved event: %v", err)
	}
	if created, err := postgres.NewStore(pool).ResolveApproval(ctx, signal, resolved); err != nil || !created {
		t.Fatalf("ResolveApproval() = (%v, %v), want (true, nil)", created, err)
	}
}

func newIntegrationEventLog(suffix string) *event.Log {
	prefix := fmt.Sprintf("event-workflow-%s-%d", suffix, time.Now().UnixNano())
	var sequence int
	return event.NewLogWith(
		func() time.Time { return time.Now().UTC() },
		func() string {
			sequence++
			return fmt.Sprintf("%s-%d", prefix, sequence)
		},
	)
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
