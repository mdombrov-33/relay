//go:build integration

package postgres

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
	"github.com/mdombrov-33/relay/internal/run"
)

func TestStoreCreateRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	r := run.New(integrationRunID(t, "created"))
	queued := newQueuedEvent(t, integrationEventID(t, "created"), r.ID)

	if err := NewStore(pool).CreateRun(ctx, r, queued); err != nil {
		t.Fatalf("CreateRun() error = %v", err)
	}

	var (
		status    run.Status
		createdAt time.Time
		updatedAt time.Time
	)
	if err := pool.QueryRow(
		ctx,
		"SELECT status, created_at, updated_at FROM runs WHERE id = $1",
		r.ID,
	).Scan(&status, &createdAt, &updatedAt); err != nil {
		t.Fatalf("query created run: %v", err)
	}
	if status != run.StatusPending {
		t.Errorf("run status = %q, want %q", status, run.StatusPending)
	}
	if !createdAt.Equal(queued.OccurredAt()) || !updatedAt.Equal(queued.OccurredAt()) {
		t.Errorf("run timestamps = (%s, %s), want both %s", createdAt, updatedAt, queued.OccurredAt())
	}

	var (
		eventID string
		typ     event.Type
		payload string
	)
	if err := pool.QueryRow(
		ctx,
		"SELECT id, type, payload::text FROM events WHERE run_id = $1",
		r.ID,
	).Scan(&eventID, &typ, &payload); err != nil {
		t.Fatalf("query queued event: %v", err)
	}
	if eventID != queued.ID() {
		t.Errorf("event ID = %q, want %q", eventID, queued.ID())
	}
	if typ != event.TypeWorkflowQueued {
		t.Errorf("event type = %q, want %q", typ, event.TypeWorkflowQueued)
	}
	if payload != `{"status": "pending"}` {
		t.Errorf("event payload = %s, want pending lifecycle payload", payload)
	}
}

func TestStoreCreateRunRollsBackWhenEventInsertFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	r := run.New(integrationRunID(t, "rolled-back"))
	queued := newQueuedEvent(t, "", r.ID)

	if err := NewStore(pool).CreateRun(ctx, r, queued); err == nil {
		t.Fatal("CreateRun() error = nil, want event insert failure")
	}

	var runCount, eventCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM runs WHERE id = $1", r.ID).Scan(&runCount); err != nil {
		t.Fatalf("count rolled-back runs: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM events WHERE run_id = $1", r.ID).Scan(&eventCount); err != nil {
		t.Fatalf("count rolled-back events: %v", err)
	}
	if runCount != 0 || eventCount != 0 {
		t.Errorf("rolled-back records = %d runs, %d events; want neither", runCount, eventCount)
	}
}

func TestStorePersistsRunAndEventAcrossPoolRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	r := run.New(integrationRunID(t, "pool-restart"))
	queued := newQueuedEvent(t, integrationEventID(t, "pool-restart"), r.ID)

	func() {
		writerPool := openIntegrationPool(t, ctx)
		defer writerPool.Close()

		if err := NewStore(writerPool).CreateRun(ctx, r, queued); err != nil {
			t.Fatalf("CreateRun() error = %v", err)
		}
	}()

	readerPool := openIntegrationPool(t, ctx)
	defer readerPool.Close()

	var status run.Status
	if err := readerPool.QueryRow(ctx, "SELECT status FROM runs WHERE id = $1", r.ID).Scan(&status); err != nil {
		t.Fatalf("query persisted run: %v", err)
	}
	if status != run.StatusPending {
		t.Errorf("persisted run status = %q, want %q", status, run.StatusPending)
	}

	stored, err := NewStore(readerPool).ListRunEvents(ctx, r.ID, 0)
	if err != nil {
		t.Fatalf("ListRunEvents() error = %v", err)
	}
	if len(stored) != 1 {
		t.Fatalf("ListRunEvents() length = %d, want 1", len(stored))
	}
	if stored[0].ID() != queued.ID() || stored[0].Type() != event.TypeWorkflowQueued {
		t.Errorf("persisted event = (ID %q, type %q), want (ID %q, type %q)", stored[0].ID(), stored[0].Type(), queued.ID(), event.TypeWorkflowQueued)
	}
}

func TestStoreFindRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	store := NewStore(pool)
	pending := pendingIntegrationRun(t, pool, ctx, "find-pending")
	pendingRecord, err := store.FindRun(ctx, pending.ID)
	if err != nil {
		t.Fatalf("FindRun() pending error = %v", err)
	}
	if pendingRecord.Run != pending || pendingRecord.PendingApproval != nil {
		t.Errorf("FindRun() pending = %#v, want pending run without approval", pendingRecord)
	}
	if pendingRecord.CreatedAt.IsZero() || pendingRecord.UpdatedAt.IsZero() {
		t.Errorf("FindRun() timestamps = (%s, %s), want persisted timestamps", pendingRecord.CreatedAt, pendingRecord.UpdatedAt)
	}

	waiting, request := waitingApprovalIntegrationRun(t, pool, ctx, "find-waiting")
	waitingRecord, err := store.FindRun(ctx, waiting.ID)
	if err != nil {
		t.Fatalf("FindRun() waiting error = %v", err)
	}
	if waitingRecord.Run.ID != waiting.ID || waitingRecord.Run.Status != run.StatusWaiting {
		t.Errorf("FindRun() run = %#v, want waiting run %q", waitingRecord.Run, waiting.ID)
	}
	if waitingRecord.PendingApproval == nil {
		t.Fatal("FindRun() pending approval = nil, want approval")
	}
	if *waitingRecord.PendingApproval != (ApprovalRequestRecord{
		ApprovalRequest: request,
		Status:          ApprovalStatusPending,
		RequestedAt:     waitingRecord.PendingApproval.RequestedAt,
	}) {
		t.Errorf("FindRun() pending approval = %#v, want request %#v", waitingRecord.PendingApproval, request)
	}
	if waitingRecord.PendingApproval.RequestedAt.IsZero() {
		t.Error("FindRun() approval requested at is zero")
	}

	if _, err := store.FindRun(ctx, run.ID("missing-run")); !errors.Is(err, ErrRunNotFound) {
		t.Errorf("FindRun() missing error = %v, want %v", err, ErrRunNotFound)
	}
}

func TestStoreListRuns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	store := NewStore(pool)
	older := pendingIntegrationRun(t, pool, ctx, "list-older")
	newer, request := waitingApprovalIntegrationRun(t, pool, ctx, "list-newer")

	records, err := store.ListRuns(ctx)
	if err != nil {
		t.Fatalf("ListRuns() error = %v", err)
	}

	olderIndex, newerIndex := -1, -1
	for i, record := range records {
		switch record.Run.ID {
		case older.ID:
			olderIndex = i
			if record.Run.Status != run.StatusPending || record.PendingApproval != nil {
				t.Errorf("ListRuns() older = %#v, want pending run without approval", record)
			}
		case newer.ID:
			newerIndex = i
			if record.Run.Status != run.StatusWaiting {
				t.Errorf("ListRuns() newer status = %q, want %q", record.Run.Status, run.StatusWaiting)
			}
			if record.PendingApproval == nil || record.PendingApproval.ID != request.ID {
				t.Errorf("ListRuns() newer approval = %#v, want request %q", record.PendingApproval, request.ID)
			}
		}
	}
	if olderIndex < 0 || newerIndex < 0 {
		t.Fatalf("ListRuns() indices = (%d, %d), want both created runs listed", olderIndex, newerIndex)
	}
	if newerIndex > olderIndex {
		t.Errorf("ListRuns() order = newer at %d after older at %d, want newest first", newerIndex, olderIndex)
	}
}

func TestStoreTransitionToTerminal(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	r := runningIntegrationRun(t, pool, ctx, "terminal")
	if err := r.Succeed(); err != nil {
		t.Fatalf("Succeed() error = %v", err)
	}
	terminal := newLifecycleEvent(t, integrationEventID(t, "completed"), r.ID, event.TypeWorkflowCompleted, r.Status)

	if err := NewStore(pool).TransitionToTerminal(ctx, r, terminal); err != nil {
		t.Fatalf("TransitionToTerminal() error = %v", err)
	}

	var status run.Status
	if err := pool.QueryRow(ctx, "SELECT status FROM runs WHERE id = $1", r.ID).Scan(&status); err != nil {
		t.Fatalf("query terminal run: %v", err)
	}
	if status != run.StatusSucceeded {
		t.Errorf("run status = %q, want %q", status, run.StatusSucceeded)
	}

	var eventID string
	if err := pool.QueryRow(ctx, "SELECT id FROM events WHERE run_id = $1", r.ID).Scan(&eventID); err != nil {
		t.Fatalf("query terminal event: %v", err)
	}
	if eventID != terminal.ID() {
		t.Errorf("event ID = %q, want %q", eventID, terminal.ID())
	}
}

func TestStoreTransitionToTerminalRollsBackWhenEventInsertFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	r := runningIntegrationRun(t, pool, ctx, "terminal-rolled-back")
	if err := r.Fail(); err != nil {
		t.Fatalf("Fail() error = %v", err)
	}
	terminal := newLifecycleEvent(t, "", r.ID, event.TypeWorkflowFailed, r.Status)

	if err := NewStore(pool).TransitionToTerminal(ctx, r, terminal); err == nil {
		t.Fatal("TransitionToTerminal() error = nil, want event insert failure")
	}

	var status run.Status
	if err := pool.QueryRow(ctx, "SELECT status FROM runs WHERE id = $1", r.ID).Scan(&status); err != nil {
		t.Fatalf("query rolled-back run: %v", err)
	}
	if status != run.StatusRunning {
		t.Errorf("run status after rollback = %q, want %q", status, run.StatusRunning)
	}

	var eventCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM events WHERE run_id = $1", r.ID).Scan(&eventCount); err != nil {
		t.Fatalf("count rolled-back terminal events: %v", err)
	}
	if eventCount != 0 {
		t.Errorf("rolled-back terminal events = %d, want 0", eventCount)
	}
}

func TestStoreCancelRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	tests := []struct {
		name   string
		create func(*testing.T, *pgxpool.Pool, context.Context, string) run.Run
	}{
		{name: "pending", create: pendingIntegrationRun},
		{name: "running", create: runningIntegrationRun},
		{
			name: "waiting",
			create: func(t *testing.T, pool *pgxpool.Pool, ctx context.Context, suffix string) run.Run {
				r, _ := waitingApprovalIntegrationRun(t, pool, ctx, suffix)
				return r
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := test.create(t, pool, ctx, "cancel-"+test.name)
			canceled := newLifecycleEvent(t, integrationEventID(t, "canceled-"+test.name), r.ID, event.TypeWorkflowCancelled, run.StatusCanceled)

			if err := NewStore(pool).CancelRun(ctx, r.ID, canceled); err != nil {
				t.Fatalf("CancelRun() error = %v", err)
			}

			var status run.Status
			if err := pool.QueryRow(ctx, "SELECT status FROM runs WHERE id = $1", r.ID).Scan(&status); err != nil {
				t.Fatalf("query canceled run: %v", err)
			}
			if status != run.StatusCanceled {
				t.Errorf("run status = %q, want %q", status, run.StatusCanceled)
			}

			stored, err := NewStore(pool).ListRunEvents(ctx, r.ID, 0)
			if err != nil {
				t.Fatalf("ListRunEvents() error = %v", err)
			}
			if stored[len(stored)-1].ID() != canceled.ID() || stored[len(stored)-1].Type() != event.TypeWorkflowCancelled {
				t.Errorf("last event = %#v, want cancellation %q", stored[len(stored)-1], canceled.ID())
			}

			if test.name == "waiting" {
				var approvalStatus ApprovalStatus
				var resolvedAt *time.Time
				if err := pool.QueryRow(ctx, "SELECT status, resolved_at FROM approval_requests WHERE run_id = $1", r.ID).Scan(&approvalStatus, &resolvedAt); err != nil {
					t.Fatalf("query canceled approval: %v", err)
				}
				if approvalStatus != ApprovalStatusCanceled || resolvedAt == nil || !resolvedAt.Equal(canceled.OccurredAt()) {
					t.Errorf("approval = (%q, %v), want canceled at %s", approvalStatus, resolvedAt, canceled.OccurredAt())
				}
			}
		})
	}
}

func TestStoreCancelRunRejectsMissingAndTerminalRuns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()
	store := NewStore(pool)

	missingID := integrationRunID(t, "cancel-missing")
	missingEvent := newLifecycleEvent(t, integrationEventID(t, "cancel-missing"), missingID, event.TypeWorkflowCancelled, run.StatusCanceled)
	if err := store.CancelRun(ctx, missingID, missingEvent); !errors.Is(err, ErrRunNotFound) {
		t.Errorf("CancelRun() missing error = %v, want %v", err, ErrRunNotFound)
	}

	r := runningIntegrationRun(t, pool, ctx, "cancel-terminal")
	if err := r.Succeed(); err != nil {
		t.Fatalf("Succeed() error = %v", err)
	}
	completed := newLifecycleEvent(t, integrationEventID(t, "cancel-terminal-completed"), r.ID, event.TypeWorkflowCompleted, r.Status)
	if err := store.TransitionToTerminal(ctx, r, completed); err != nil {
		t.Fatalf("TransitionToTerminal() error = %v", err)
	}
	canceled := newLifecycleEvent(t, integrationEventID(t, "cancel-terminal-canceled"), r.ID, event.TypeWorkflowCancelled, run.StatusCanceled)
	if err := store.CancelRun(ctx, r.ID, canceled); !errors.Is(err, ErrRunAlreadyTerminal) {
		t.Errorf("CancelRun() terminal error = %v, want %v", err, ErrRunAlreadyTerminal)
	}
}

func TestStoreCancelRunRollsBackWhenEventInsertFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	r, request := waitingApprovalIntegrationRun(t, pool, ctx, "cancel-rollback")
	canceled := newLifecycleEvent(t, "", r.ID, event.TypeWorkflowCancelled, run.StatusCanceled)
	if err := NewStore(pool).CancelRun(ctx, r.ID, canceled); err == nil {
		t.Fatal("CancelRun() error = nil, want event insert failure")
	}

	var runStatus run.Status
	var approvalStatus ApprovalStatus
	var resolvedAt *time.Time
	if err := pool.QueryRow(ctx, "SELECT status FROM runs WHERE id = $1", r.ID).Scan(&runStatus); err != nil {
		t.Fatalf("query rolled-back run: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT status, resolved_at FROM approval_requests WHERE id = $1", request.ID).Scan(&approvalStatus, &resolvedAt); err != nil {
		t.Fatalf("query rolled-back approval: %v", err)
	}
	if runStatus != run.StatusWaiting || approvalStatus != ApprovalStatusPending || resolvedAt != nil {
		t.Errorf("rolled-back cancellation = (%q, %q, %v), want waiting, pending, nil", runStatus, approvalStatus, resolvedAt)
	}
}

func TestStoreRequestApproval(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	r := runningIntegrationRun(t, pool, ctx, "approval-requested")
	if err := r.Wait(); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}

	request := ApprovalRequest{
		ID:       "approval-" + string(r.ID),
		RunID:    r.ID,
		StepKey:  run.StepKey("tool/1/issue-credit"),
		CallID:   "call-123",
		ToolName: "issue_credit",
	}
	requested := newApprovalRequestedEvent(t, integrationEventID(t, "approval-requested"), request)

	if err := NewStore(pool).RequestApproval(ctx, r, request, requested); err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}

	var (
		status    run.Status
		updatedAt time.Time
	)
	if err := pool.QueryRow(
		ctx,
		"SELECT status, updated_at FROM runs WHERE id = $1",
		r.ID,
	).Scan(&status, &updatedAt); err != nil {
		t.Fatalf("query waiting run: %v", err)
	}
	if status != run.StatusWaiting {
		t.Errorf("run status = %q, want %q", status, run.StatusWaiting)
	}
	if !updatedAt.Equal(requested.OccurredAt()) {
		t.Errorf("run updated at = %s, want %s", updatedAt, requested.OccurredAt())
	}

	var (
		requestID     string
		requestRun    run.ID
		stepKey       run.StepKey
		callID        string
		toolName      string
		requestStatus ApprovalStatus
		requestedAt   time.Time
		resolvedAt    *time.Time
	)
	if err := pool.QueryRow(
		ctx,
		`SELECT id, run_id, step_key, call_id, tool_name, status, requested_at, resolved_at
		 FROM approval_requests
		 WHERE id = $1`,
		request.ID,
	).Scan(&requestID, &requestRun, &stepKey, &callID, &toolName, &requestStatus, &requestedAt, &resolvedAt); err != nil {
		t.Fatalf("query approval request: %v", err)
	}
	if requestID != request.ID || requestRun != request.RunID || stepKey != request.StepKey || callID != request.CallID || toolName != request.ToolName {
		t.Errorf("approval request identity = (%q, %q, %q, %q, %q), want (%q, %q, %q, %q, %q)", requestID, requestRun, stepKey, callID, toolName, request.ID, request.RunID, request.StepKey, request.CallID, request.ToolName)
	}
	if requestStatus != ApprovalStatusPending || resolvedAt != nil {
		t.Errorf("approval request resolution = (%q, %v), want pending and unresolved", requestStatus, resolvedAt)
	}
	if !requestedAt.Equal(requested.OccurredAt()) {
		t.Errorf("approval requested at = %s, want %s", requestedAt, requested.OccurredAt())
	}

	stored, err := NewStore(pool).ListRunEvents(ctx, r.ID, 0)
	if err != nil {
		t.Fatalf("ListRunEvents() error = %v", err)
	}
	if len(stored) != 1 || stored[0].ID() != requested.ID() || stored[0].Type() != event.TypeApprovalRequested {
		t.Errorf("approval events = %#v, want only %q", stored, requested.ID())
	}
}

func TestStoreRequestApprovalRollsBackWhenEventInsertFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	r := runningIntegrationRun(t, pool, ctx, "approval-rolled-back")
	if err := r.Wait(); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	request := ApprovalRequest{
		ID:       "approval-" + string(r.ID),
		RunID:    r.ID,
		StepKey:  run.StepKey("tool/1/issue-credit"),
		CallID:   "call-123",
		ToolName: "issue_credit",
	}
	requested := newApprovalRequestedEvent(t, "", request)

	if err := NewStore(pool).RequestApproval(ctx, r, request, requested); err == nil {
		t.Fatal("RequestApproval() error = nil, want event insert failure")
	}

	var status run.Status
	if err := pool.QueryRow(ctx, "SELECT status FROM runs WHERE id = $1", r.ID).Scan(&status); err != nil {
		t.Fatalf("query rolled-back approval run: %v", err)
	}
	if status != run.StatusRunning {
		t.Errorf("run status after rollback = %q, want %q", status, run.StatusRunning)
	}

	var requestCount, eventCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM approval_requests WHERE run_id = $1", r.ID).Scan(&requestCount); err != nil {
		t.Fatalf("count rolled-back approval requests: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM events WHERE run_id = $1", r.ID).Scan(&eventCount); err != nil {
		t.Fatalf("count rolled-back approval events: %v", err)
	}
	if requestCount != 0 || eventCount != 0 {
		t.Errorf("rolled-back approval records = %d requests, %d events; want neither", requestCount, eventCount)
	}
}

func TestStoreResolveApproval(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	tests := []struct {
		name     string
		decision ApprovalDecision
		approved bool
	}{
		{name: "approved", decision: ApprovalDecisionApproved, approved: true},
		{name: "rejected", decision: ApprovalDecisionRejected, approved: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r, request := waitingApprovalIntegrationRun(t, pool, ctx, "resolved-"+test.name)
			signal := ApprovalSignal{
				ID:        "signal-" + string(r.ID),
				RequestID: request.ID,
				RunID:     r.ID,
				Decision:  test.decision,
			}
			resolved := newApprovalResolvedEvent(t, integrationEventID(t, "approval-resolved-"+test.name), request.StepKey, signal, test.approved)

			created, err := NewStore(pool).ResolveApproval(ctx, signal, resolved)
			if err != nil {
				t.Fatalf("ResolveApproval() error = %v", err)
			}
			if !created {
				t.Fatal("ResolveApproval() created = false, want true")
			}

			var (
				runStatus      run.Status
				runUpdatedAt   time.Time
				requestStatus  ApprovalStatus
				resolvedAt     *time.Time
				signalID       string
				signalDecision ApprovalDecision
				receivedAt     time.Time
			)
			if err := pool.QueryRow(ctx, "SELECT status, updated_at FROM runs WHERE id = $1", r.ID).Scan(&runStatus, &runUpdatedAt); err != nil {
				t.Fatalf("query resumed run: %v", err)
			}
			if err := pool.QueryRow(ctx, "SELECT status, resolved_at FROM approval_requests WHERE id = $1", request.ID).Scan(&requestStatus, &resolvedAt); err != nil {
				t.Fatalf("query resolved approval request: %v", err)
			}
			if err := pool.QueryRow(
				ctx,
				"SELECT id, decision, received_at FROM approval_signals WHERE request_id = $1",
				request.ID,
			).Scan(&signalID, &signalDecision, &receivedAt); err != nil {
				t.Fatalf("query approval signal: %v", err)
			}

			if runStatus != run.StatusRunning || !runUpdatedAt.Equal(resolved.OccurredAt()) {
				t.Errorf("resumed run = (%q, %s), want (%q, %s)", runStatus, runUpdatedAt, run.StatusRunning, resolved.OccurredAt())
			}
			if requestStatus != ApprovalStatus(test.decision) || resolvedAt == nil || !resolvedAt.Equal(resolved.OccurredAt()) {
				t.Errorf("resolved request = (%q, %v), want (%q, %s)", requestStatus, resolvedAt, test.decision, resolved.OccurredAt())
			}
			if signalID != signal.ID || signalDecision != signal.Decision || !receivedAt.Equal(resolved.OccurredAt()) {
				t.Errorf("stored signal = (%q, %q, %s), want (%q, %q, %s)", signalID, signalDecision, receivedAt, signal.ID, signal.Decision, resolved.OccurredAt())
			}

			stored, err := NewStore(pool).ListRunEvents(ctx, r.ID, 0)
			if err != nil {
				t.Fatalf("ListRunEvents() error = %v", err)
			}
			if len(stored) != 2 || stored[1].ID() != resolved.ID() || stored[1].Type() != event.TypeApprovalResolved {
				t.Errorf("approval events = %#v, want request followed by %q", stored, resolved.ID())
			}
		})
	}
}

func TestStoreResolveApprovalIsIdempotentAndRejectsConflict(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	r, request := waitingApprovalIntegrationRun(t, pool, ctx, "resolution-idempotent")
	first := ApprovalSignal{
		ID:        "signal-first-" + string(r.ID),
		RequestID: request.ID,
		RunID:     r.ID,
		Decision:  ApprovalDecisionApproved,
	}
	firstEvent := newApprovalResolvedEvent(t, integrationEventID(t, "approval-resolved-first"), request.StepKey, first, true)
	store := NewStore(pool)
	if created, err := store.ResolveApproval(ctx, first, firstEvent); err != nil || !created {
		t.Fatalf("ResolveApproval() first = (%v, %v), want (true, nil)", created, err)
	}

	duplicate := first
	duplicate.ID = "signal-duplicate-" + string(r.ID)
	duplicateEvent := newApprovalResolvedEvent(t, integrationEventID(t, "approval-resolved-duplicate"), request.StepKey, duplicate, true)
	if created, err := store.ResolveApproval(ctx, duplicate, duplicateEvent); err != nil || created {
		t.Fatalf("ResolveApproval() duplicate = (%v, %v), want (false, nil)", created, err)
	}

	conflict := duplicate
	conflict.ID = "signal-conflict-" + string(r.ID)
	conflict.Decision = ApprovalDecisionRejected
	conflictEvent := newApprovalResolvedEvent(t, integrationEventID(t, "approval-resolved-conflict"), request.StepKey, conflict, false)
	if _, err := store.ResolveApproval(ctx, conflict, conflictEvent); !errors.Is(err, ErrApprovalDecisionConflict) {
		t.Fatalf("ResolveApproval() conflict error = %v, want %v", err, ErrApprovalDecisionConflict)
	}

	var signalCount, resolvedEventCount int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM approval_signals WHERE request_id = $1", request.ID).Scan(&signalCount); err != nil {
		t.Fatalf("count approval signals: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM events WHERE run_id = $1 AND type = $2", r.ID, event.TypeApprovalResolved).Scan(&resolvedEventCount); err != nil {
		t.Fatalf("count approval resolved events: %v", err)
	}
	if signalCount != 1 || resolvedEventCount != 1 {
		t.Errorf("durable resolution records = %d signals, %d events; want one each", signalCount, resolvedEventCount)
	}
}

func TestStoreResolveApprovalRejectsMismatchedRun(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	waitingRun, request := waitingApprovalIntegrationRun(t, pool, ctx, "resolution-mismatch-waiting")
	otherRun := runningIntegrationRun(t, pool, ctx, "resolution-mismatch-other")
	signal := ApprovalSignal{
		ID:        "signal-" + string(otherRun.ID),
		RequestID: request.ID,
		RunID:     otherRun.ID,
		Decision:  ApprovalDecisionApproved,
	}
	resolved := newApprovalResolvedEvent(t, integrationEventID(t, "approval-resolved-mismatch"), request.StepKey, signal, true)

	if _, err := NewStore(pool).ResolveApproval(ctx, signal, resolved); !errors.Is(err, ErrApprovalSignalRunIDMismatch) {
		t.Fatalf("ResolveApproval() error = %v, want %v", err, ErrApprovalSignalRunIDMismatch)
	}

	for _, expected := range []struct {
		id     run.ID
		status run.Status
	}{
		{id: waitingRun.ID, status: run.StatusWaiting},
		{id: otherRun.ID, status: run.StatusRunning},
	} {
		var status run.Status
		if err := pool.QueryRow(ctx, "SELECT status FROM runs WHERE id = $1", expected.id).Scan(&status); err != nil {
			t.Fatalf("query unchanged run %q: %v", expected.id, err)
		}
		if status != expected.status {
			t.Errorf("run %q status = %q, want %q", expected.id, status, expected.status)
		}
	}
}

func TestStoreResolveApprovalRollsBackWhenEventInsertFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	r, request := waitingApprovalIntegrationRun(t, pool, ctx, "resolution-rolled-back")
	signal := ApprovalSignal{
		ID:        "signal-" + string(r.ID),
		RequestID: request.ID,
		RunID:     r.ID,
		Decision:  ApprovalDecisionApproved,
	}
	resolved := newApprovalResolvedEvent(t, "", request.StepKey, signal, true)

	if _, err := NewStore(pool).ResolveApproval(ctx, signal, resolved); err == nil {
		t.Fatal("ResolveApproval() error = nil, want event insert failure")
	}

	var (
		runStatus     run.Status
		requestStatus ApprovalStatus
		resolvedAt    *time.Time
		signalCount   int
	)
	if err := pool.QueryRow(ctx, "SELECT status FROM runs WHERE id = $1", r.ID).Scan(&runStatus); err != nil {
		t.Fatalf("query rolled-back run: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT status, resolved_at FROM approval_requests WHERE id = $1", request.ID).Scan(&requestStatus, &resolvedAt); err != nil {
		t.Fatalf("query rolled-back approval request: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM approval_signals WHERE request_id = $1", request.ID).Scan(&signalCount); err != nil {
		t.Fatalf("count rolled-back approval signals: %v", err)
	}
	if runStatus != run.StatusWaiting || requestStatus != ApprovalStatusPending || resolvedAt != nil || signalCount != 0 {
		t.Errorf("rolled-back resolution = (%q, %q, %v, %d signals), want waiting, pending, nil, zero", runStatus, requestStatus, resolvedAt, signalCount)
	}
}

func TestStoreListRunEvents(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	r := pendingIntegrationRun(t, pool, ctx, "listed")
	first := insertIntegrationEvent(t, pool, ctx, integrationEventID(t, "queued"), r.ID, event.TypeWorkflowQueued, run.StatusPending)
	second := insertIntegrationEvent(t, pool, ctx, integrationEventID(t, "started"), r.ID, event.TypeWorkflowStarted, run.StatusRunning)

	stored, err := NewStore(pool).ListRunEvents(ctx, r.ID, 0)
	if err != nil {
		t.Fatalf("ListRunEvents() error = %v", err)
	}
	if len(stored) != 2 {
		t.Fatalf("ListRunEvents() length = %d, want 2", len(stored))
	}
	if stored[0].Sequence != first.Sequence || stored[1].Sequence != second.Sequence {
		t.Errorf("event sequences = (%d, %d), want (%d, %d)", stored[0].Sequence, stored[1].Sequence, first.Sequence, second.Sequence)
	}
	if stored[0].ID() != first.ID() || stored[1].ID() != second.ID() {
		t.Errorf("event IDs = (%q, %q), want (%q, %q)", stored[0].ID(), stored[1].ID(), first.ID(), second.ID())
	}

	afterFirst, err := NewStore(pool).ListRunEvents(ctx, r.ID, first.Sequence)
	if err != nil {
		t.Fatalf("ListRunEvents() after first error = %v", err)
	}
	if len(afterFirst) != 1 || afterFirst[0].Sequence != second.Sequence {
		t.Errorf("ListRunEvents() after first = %#v, want only sequence %d", afterFirst, second.Sequence)
	}
}

func TestStoreListEventsAfter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	before := latestEventSequence(t, pool, ctx)
	firstRun := pendingIntegrationRun(t, pool, ctx, "global-first")
	first := insertIntegrationEvent(t, pool, ctx, integrationEventID(t, "global-first"), firstRun.ID, event.TypeWorkflowQueued, run.StatusPending)
	secondRun := pendingIntegrationRun(t, pool, ctx, "global-second")
	second := insertIntegrationEvent(t, pool, ctx, integrationEventID(t, "global-second"), secondRun.ID, event.TypeWorkflowQueued, run.StatusPending)
	third := insertIntegrationEvent(t, pool, ctx, integrationEventID(t, "global-third"), secondRun.ID, event.TypeWorkflowStarted, run.StatusRunning)

	stored, err := NewStore(pool).ListEventsAfter(ctx, before)
	if err != nil {
		t.Fatalf("ListEventsAfter() error = %v", err)
	}
	if len(stored) != 3 {
		t.Fatalf("ListEventsAfter() length = %d, want 3", len(stored))
	}
	for index, want := range []event.Stored{first, second, third} {
		if stored[index].Sequence != want.Sequence || stored[index].ID() != want.ID() {
			t.Errorf("event %d = (sequence %d, ID %q), want (sequence %d, ID %q)", index, stored[index].Sequence, stored[index].ID(), want.Sequence, want.ID())
		}
	}

	afterSecond, err := NewStore(pool).ListEventsAfter(ctx, second.Sequence)
	if err != nil {
		t.Fatalf("ListEventsAfter() after second error = %v", err)
	}
	if len(afterSecond) != 1 || afterSecond[0].Sequence != third.Sequence {
		t.Errorf("ListEventsAfter() after second = %#v, want only sequence %d", afterSecond, third.Sequence)
	}
}

func TestStepsProjectionInvariants(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	r := pendingIntegrationRun(t, pool, ctx, "step")
	startedAt := time.Now().UTC()
	inputHash := make([]byte, 32)
	for index := range inputHash {
		inputHash[index] = byte(index)
	}

	if _, err := pool.Exec(
		ctx,
		`INSERT INTO steps (run_id, step_key, input_hash, attempt, status, started_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		r.ID,
		run.StepKey("model/1"),
		inputHash,
		1,
		"running",
		startedAt,
	); err != nil {
		t.Fatalf("insert running step: %v", err)
	}

	completedAt := startedAt.Add(time.Second)
	if _, err := pool.Exec(
		ctx,
		`UPDATE steps
		 SET status = $3, result = $4::jsonb, completed_at = $5
		 WHERE run_id = $1 AND step_key = $2`,
		r.ID,
		run.StepKey("model/1"),
		"completed",
		`{"response":"cached"}`,
		completedAt,
	); err != nil {
		t.Fatalf("complete step: %v", err)
	}

	var (
		gotHash        []byte
		gotAttempt     int
		gotStatus      string
		gotResult      string
		gotCompletedAt time.Time
	)
	if err := pool.QueryRow(
		ctx,
		`SELECT input_hash, attempt, status, result::text, completed_at
		 FROM steps
		 WHERE run_id = $1 AND step_key = $2`,
		r.ID,
		run.StepKey("model/1"),
	).Scan(&gotHash, &gotAttempt, &gotStatus, &gotResult, &gotCompletedAt); err != nil {
		t.Fatalf("read completed step: %v", err)
	}
	if string(gotHash) != string(inputHash) || gotAttempt != 1 || gotStatus != "completed" || gotResult != `{"response": "cached"}` || !gotCompletedAt.Equal(completedAt) {
		t.Errorf("completed step = (hash %v, attempt %d, status %q, result %s, completed_at %s), want stored checkpoint", gotHash, gotAttempt, gotStatus, gotResult, gotCompletedAt)
	}

	tests := []struct {
		name  string
		query string
		args  []any
	}{
		{
			name: "duplicate run and step key",
			query: `INSERT INTO steps (run_id, step_key, input_hash, attempt, status, started_at)
				VALUES ($1, $2, $3, $4, $5, $6)`,
			args: []any{r.ID, run.StepKey("model/1"), inputHash, 2, "running", startedAt},
		},
		{
			name: "empty step key",
			query: `INSERT INTO steps (run_id, step_key, input_hash, attempt, status, started_at)
				VALUES ($1, $2, $3, $4, $5, $6)`,
			args: []any{r.ID, run.StepKey(""), inputHash, 1, "running", startedAt},
		},
		{
			name: "non SHA-256 input hash",
			query: `INSERT INTO steps (run_id, step_key, input_hash, attempt, status, started_at)
				VALUES ($1, $2, $3, $4, $5, $6)`,
			args: []any{r.ID, run.StepKey("model/2"), make([]byte, 31), 1, "running", startedAt},
		},
		{
			name: "nonpositive attempt",
			query: `INSERT INTO steps (run_id, step_key, input_hash, attempt, status, started_at)
				VALUES ($1, $2, $3, $4, $5, $6)`,
			args: []any{r.ID, run.StepKey("model/attempt"), inputHash, 0, "running", startedAt},
		},
		{
			name: "completed step without result",
			query: `INSERT INTO steps (run_id, step_key, input_hash, attempt, status, started_at, completed_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			args: []any{r.ID, run.StepKey("model/3"), inputHash, 1, "completed", startedAt, completedAt},
		},
		{
			name: "completed step without completion time",
			query: `INSERT INTO steps (run_id, step_key, input_hash, attempt, status, result, started_at)
				VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7)`,
			args: []any{r.ID, run.StepKey("model/4"), inputHash, 1, "completed", `{"response":"cached"}`, startedAt},
		},
		{
			name: "completion before start",
			query: `INSERT INTO steps (run_id, step_key, input_hash, attempt, status, result, started_at, completed_at)
				VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8)`,
			args: []any{r.ID, run.StepKey("model/5"), inputHash, 1, "completed", `{"response":"cached"}`, completedAt, startedAt},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, test.query, test.args...); err == nil {
				t.Fatal("Exec() error = nil, want schema constraint violation")
			}
		})
	}
}

func TestStoreClaimStepCreatesRunningCheckpoint(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	r := pendingIntegrationRun(t, pool, ctx, "claim")
	startedAt := time.Now().UTC()
	inputHash := sha256.Sum256([]byte("model input"))

	checkpoint, err := NewStore(pool).ClaimStep(ctx, r.ID, run.StepKey("model/1"), inputHash, startedAt)
	if err != nil {
		t.Fatalf("ClaimStep() error = %v", err)
	}
	if checkpoint.RunID != r.ID || checkpoint.StepKey != run.StepKey("model/1") || checkpoint.InputHash != inputHash || checkpoint.Attempt != 1 || checkpoint.Status != StepStatusRunning || checkpoint.Result != nil || checkpoint.CompletedAt != nil || !checkpoint.StartedAt.Equal(startedAt) {
		t.Errorf("ClaimStep() = %#v, want running checkpoint", checkpoint)
	}
}

func TestStoreClaimStepReturnsCompletedCheckpoint(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	r := pendingIntegrationRun(t, pool, ctx, "cached")
	startedAt := time.Now().UTC()
	inputHash := sha256.Sum256([]byte("model input"))
	store := NewStore(pool)

	claimed, err := store.ClaimStep(ctx, r.ID, run.StepKey("model/1"), inputHash, startedAt)
	if err != nil {
		t.Fatalf("ClaimStep() initial error = %v", err)
	}
	completedAt := startedAt.Add(time.Second)
	result := json.RawMessage(`{"response":"cached"}`)
	if _, err := store.CompleteStep(ctx, r.ID, run.StepKey("model/1"), inputHash, claimed.Attempt, result, completedAt); err != nil {
		t.Fatalf("CompleteStep() error = %v", err)
	}

	checkpoint, err := store.ClaimStep(ctx, r.ID, run.StepKey("model/1"), inputHash, startedAt.Add(2*time.Second))
	if err != nil {
		t.Fatalf("ClaimStep() cached error = %v", err)
	}
	if checkpoint.Attempt != 1 || checkpoint.Status != StepStatusCompleted || string(checkpoint.Result) != `{"response": "cached"}` || checkpoint.CompletedAt == nil || !checkpoint.CompletedAt.Equal(completedAt) {
		t.Errorf("ClaimStep() cached = %#v, want completed checkpoint", checkpoint)
	}
}

func TestStoreClaimStepRejectsDuplicateOrChangedInput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	r := pendingIntegrationRun(t, pool, ctx, "claim-rejected")
	startedAt := time.Now().UTC()
	inputHash := sha256.Sum256([]byte("model input"))
	store := NewStore(pool)

	if _, err := store.ClaimStep(ctx, r.ID, run.StepKey("model/1"), inputHash, startedAt); err != nil {
		t.Fatalf("ClaimStep() initial error = %v", err)
	}

	tests := []struct {
		name      string
		inputHash [sha256.Size]byte
		want      error
	}{
		{
			name:      "already running",
			inputHash: inputHash,
			want:      ErrStepAlreadyRunning,
		},
		{
			name:      "changed input",
			inputHash: sha256.Sum256([]byte("changed model input")),
			want:      ErrStepInputMismatch,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := store.ClaimStep(ctx, r.ID, run.StepKey("model/1"), test.inputHash, startedAt.Add(time.Second))
			if !errors.Is(err, test.want) {
				t.Fatalf("ClaimStep() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestStoreCompleteStep(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	r := pendingIntegrationRun(t, pool, ctx, "complete")
	startedAt := time.Now().UTC()
	completedAt := startedAt.Add(time.Second)
	inputHash := sha256.Sum256([]byte("tool input"))
	store := NewStore(pool)

	claimed, err := store.ClaimStep(ctx, r.ID, run.StepKey("tool/1/lookup"), inputHash, startedAt)
	if err != nil {
		t.Fatalf("ClaimStep() error = %v", err)
	}
	if _, err := store.CompleteStep(ctx, r.ID, run.StepKey("tool/1/lookup"), sha256.Sum256([]byte("changed tool input")), claimed.Attempt, json.RawMessage(`{"customer":"changed"}`), completedAt); !errors.Is(err, ErrStepNotRunning) {
		t.Fatalf("CompleteStep() changed input error = %v, want %v", err, ErrStepNotRunning)
	}

	checkpoint, err := store.CompleteStep(ctx, r.ID, run.StepKey("tool/1/lookup"), inputHash, claimed.Attempt, json.RawMessage(`{"customer":"found"}`), completedAt)
	if err != nil {
		t.Fatalf("CompleteStep() error = %v", err)
	}
	if checkpoint.Status != StepStatusCompleted || string(checkpoint.Result) != `{"customer": "found"}` || checkpoint.CompletedAt == nil || !checkpoint.CompletedAt.Equal(completedAt) {
		t.Errorf("CompleteStep() = %#v, want completed checkpoint", checkpoint)
	}
	if _, err := store.CompleteStep(ctx, r.ID, run.StepKey("tool/1/lookup"), inputHash, claimed.Attempt, json.RawMessage(`{"customer":"changed"}`), completedAt.Add(time.Second)); !errors.Is(err, ErrStepNotRunning) {
		t.Errorf("CompleteStep() duplicate error = %v, want %v", err, ErrStepNotRunning)
	}
}

func TestStoreCompleteStepRejectsInvalidStateOrResult(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	r := pendingIntegrationRun(t, pool, ctx, "complete-rejected")
	startedAt := time.Now().UTC()
	inputHash := sha256.Sum256([]byte("tool input"))
	store := NewStore(pool)

	tests := []struct {
		name   string
		result json.RawMessage
		want   error
	}{
		{
			name:   "missing running checkpoint",
			result: json.RawMessage(`{"customer":"found"}`),
			want:   ErrStepNotRunning,
		},
		{
			name:   "invalid JSON result",
			result: json.RawMessage(`{"customer":`),
			want:   ErrInvalidStepResult,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := store.CompleteStep(ctx, r.ID, run.StepKey("tool/1/lookup"), inputHash, 1, test.result, startedAt.Add(time.Second))
			if !errors.Is(err, test.want) {
				t.Fatalf("CompleteStep() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestStoreRecoverStepStartsNewAttempt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	r := pendingIntegrationRun(t, pool, ctx, "recovered")
	startedAt := time.Now().UTC()
	recoveredAt := startedAt.Add(time.Second)
	inputHash := sha256.Sum256([]byte("tool input"))
	store := NewStore(pool)

	claimed, err := store.ClaimStep(ctx, r.ID, run.StepKey("tool/1/lookup"), inputHash, startedAt)
	if err != nil {
		t.Fatalf("ClaimStep() error = %v", err)
	}

	recovered, err := store.RecoverStep(ctx, r.ID, run.StepKey("tool/1/lookup"), inputHash, recoveredAt)
	if err != nil {
		t.Fatalf("RecoverStep() error = %v", err)
	}
	if recovered.Attempt != claimed.Attempt+1 || recovered.Status != StepStatusRunning || !recovered.StartedAt.Equal(recoveredAt) || recovered.CompletedAt != nil {
		t.Errorf("RecoverStep() = %#v, want second running attempt", recovered)
	}

	if _, err := store.CompleteStep(ctx, r.ID, run.StepKey("tool/1/lookup"), inputHash, claimed.Attempt, json.RawMessage(`{"customer":"stale"}`), recoveredAt.Add(time.Second)); !errors.Is(err, ErrStepNotRunning) {
		t.Fatalf("CompleteStep() stale attempt error = %v, want %v", err, ErrStepNotRunning)
	}
	if _, err := store.CompleteStep(ctx, r.ID, run.StepKey("tool/1/lookup"), inputHash, recovered.Attempt, json.RawMessage(`{"customer":"found"}`), recoveredAt.Add(time.Second)); err != nil {
		t.Fatalf("CompleteStep() recovered attempt error = %v", err)
	}
}

func TestStoreRecoverStepRejectsMissingOrChangedInput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	r := pendingIntegrationRun(t, pool, ctx, "recover-rejected")
	startedAt := time.Now().UTC()
	inputHash := sha256.Sum256([]byte("model input"))
	store := NewStore(pool)

	if _, err := store.RecoverStep(ctx, r.ID, run.StepKey("model/1"), inputHash, startedAt); !errors.Is(err, ErrStepNotFound) {
		t.Fatalf("RecoverStep() missing error = %v, want %v", err, ErrStepNotFound)
	}
	if _, err := store.ClaimStep(ctx, r.ID, run.StepKey("model/1"), inputHash, startedAt); err != nil {
		t.Fatalf("ClaimStep() error = %v", err)
	}
	if _, err := store.RecoverStep(ctx, r.ID, run.StepKey("model/1"), sha256.Sum256([]byte("changed model input")), startedAt.Add(time.Second)); !errors.Is(err, ErrStepInputMismatch) {
		t.Fatalf("RecoverStep() changed input error = %v, want %v", err, ErrStepInputMismatch)
	}
}

func TestStoreRecoverStepReturnsCompletedCheckpointAfterPoolRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	startedAt := time.Now().UTC()
	completedAt := startedAt.Add(time.Second)
	inputHash := sha256.Sum256([]byte("model input"))
	var r run.Run

	func() {
		writerPool := openIntegrationPool(t, ctx)
		defer writerPool.Close()

		r = pendingIntegrationRun(t, writerPool, ctx, "recovered-completed")
		store := NewStore(writerPool)
		claimed, err := store.ClaimStep(ctx, r.ID, run.StepKey("model/1"), inputHash, startedAt)
		if err != nil {
			t.Fatalf("ClaimStep() error = %v", err)
		}
		if _, err := store.CompleteStep(ctx, r.ID, run.StepKey("model/1"), inputHash, claimed.Attempt, json.RawMessage(`{"response":"cached"}`), completedAt); err != nil {
			t.Fatalf("CompleteStep() error = %v", err)
		}
	}()

	readerPool := openIntegrationPool(t, ctx)
	defer readerPool.Close()

	checkpoint, err := NewStore(readerPool).RecoverStep(ctx, r.ID, run.StepKey("model/1"), inputHash, completedAt.Add(time.Second))
	if err != nil {
		t.Fatalf("RecoverStep() error = %v", err)
	}
	if checkpoint.Attempt != 1 || checkpoint.Status != StepStatusCompleted || string(checkpoint.Result) != `{"response": "cached"}` || checkpoint.CompletedAt == nil || !checkpoint.CompletedAt.Equal(completedAt) {
		t.Errorf("RecoverStep() = %#v, want persisted completed checkpoint", checkpoint)
	}
}

func TestEffectsProjectionInvariants(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	r := pendingIntegrationRun(t, pool, ctx, "effect")
	recordedAt := time.Now().UTC()
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO effects (idempotency_key, run_id, step_key, effect_type, result, recorded_at)
		 VALUES ($1, $2, $3, $4, $5::jsonb, $6)`,
		"effect-key-"+string(r.ID),
		r.ID,
		run.StepKey("tool/1/issue-credit"),
		"issue_credit",
		`{"credit_id":"credit-123"}`,
		recordedAt,
	); err != nil {
		t.Fatalf("insert effect: %v", err)
	}

	tests := []struct {
		name  string
		query string
		args  []any
	}{
		{
			name: "duplicate idempotency key",
			query: `INSERT INTO effects (idempotency_key, run_id, step_key, effect_type, result, recorded_at)
				VALUES ($1, $2, $3, $4, $5::jsonb, $6)`,
			args: []any{"effect-key-" + string(r.ID), r.ID, run.StepKey("tool/2/issue-credit"), "issue_credit", `{"credit_id":"credit-456"}`, recordedAt},
		},
		{
			name: "empty idempotency key",
			query: `INSERT INTO effects (idempotency_key, run_id, step_key, effect_type, result, recorded_at)
				VALUES ($1, $2, $3, $4, $5::jsonb, $6)`,
			args: []any{"", r.ID, run.StepKey("tool/2/issue-credit"), "issue_credit", `{"credit_id":"credit-456"}`, recordedAt},
		},
		{
			name: "missing run",
			query: `INSERT INTO effects (idempotency_key, run_id, step_key, effect_type, result, recorded_at)
				VALUES ($1, $2, $3, $4, $5::jsonb, $6)`,
			args: []any{"missing-run-key", run.ID("missing-run"), run.StepKey("tool/2/issue-credit"), "issue_credit", `{"credit_id":"credit-456"}`, recordedAt},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, test.query, test.args...); err == nil {
				t.Fatal("Exec() error = nil, want schema constraint violation")
			}
		})
	}
}

func TestStoreRecordEffectReturnsRecordedEffectForDuplicateKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	r := pendingIntegrationRun(t, pool, ctx, "record-effect")
	store := NewStore(pool)
	recordedAt := time.Now().UTC()
	effect := Effect{
		IdempotencyKey: "issue-credit-" + string(r.ID),
		RunID:          r.ID,
		StepKey:        run.StepKey("tool/1/issue-credit"),
		Type:           EffectType("issue_credit"),
		Result:         json.RawMessage(`{"credit_id":"credit-123"}`),
		RecordedAt:     recordedAt,
	}

	first, created, err := store.RecordEffect(ctx, effect)
	if err != nil {
		t.Fatalf("RecordEffect() first error = %v", err)
	}
	if !created || first.IdempotencyKey != effect.IdempotencyKey || string(first.Result) != `{"credit_id": "credit-123"}` || !first.RecordedAt.Equal(recordedAt) {
		t.Fatalf("RecordEffect() first = %#v, created %t, want stored effect", first, created)
	}

	effect.Result = json.RawMessage(`{"credit_id":"credit-456"}`)
	effect.RecordedAt = recordedAt.Add(time.Second)
	duplicate, created, err := store.RecordEffect(ctx, effect)
	if err != nil {
		t.Fatalf("RecordEffect() duplicate error = %v", err)
	}
	if created || duplicate.IdempotencyKey != first.IdempotencyKey || string(duplicate.Result) != string(first.Result) || !duplicate.RecordedAt.Equal(first.RecordedAt) {
		t.Errorf("RecordEffect() duplicate = %#v, created %t, want original stored effect", duplicate, created)
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM effects WHERE idempotency_key = $1", effect.IdempotencyKey).Scan(&count); err != nil {
		t.Fatalf("count effects: %v", err)
	}
	if count != 1 {
		t.Errorf("effect count = %d, want 1", count)
	}
}

func TestStoreRecordEffectRejectsInvalidOrMismatchedIdentity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool := openIntegrationPool(t, ctx)
	defer pool.Close()

	r := pendingIntegrationRun(t, pool, ctx, "record-effect-rejected")
	store := NewStore(pool)
	effect := Effect{
		IdempotencyKey: "issue-credit-" + string(r.ID),
		RunID:          r.ID,
		StepKey:        run.StepKey("tool/1/issue-credit"),
		Type:           EffectType("issue_credit"),
		Result:         json.RawMessage(`{"credit_id":"credit-123"}`),
		RecordedAt:     time.Now().UTC(),
	}
	if _, _, err := store.RecordEffect(ctx, effect); err != nil {
		t.Fatalf("RecordEffect() initial error = %v", err)
	}

	tests := []struct {
		name   string
		effect Effect
		want   error
	}{
		{
			name: "invalid JSON result",
			effect: Effect{
				IdempotencyKey: "invalid-result-" + string(r.ID),
				RunID:          r.ID,
				StepKey:        run.StepKey("tool/1/issue-credit"),
				Type:           EffectType("issue_credit"),
				Result:         json.RawMessage(`{"credit_id":`),
				RecordedAt:     effect.RecordedAt,
			},
			want: ErrInvalidEffectResult,
		},
		{
			name: "changed step key",
			effect: Effect{
				IdempotencyKey: effect.IdempotencyKey,
				RunID:          effect.RunID,
				StepKey:        run.StepKey("tool/2/issue-credit"),
				Type:           effect.Type,
				Result:         effect.Result,
				RecordedAt:     effect.RecordedAt,
			},
			want: ErrEffectIdentityMismatch,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := store.RecordEffect(ctx, test.effect)
			if !errors.Is(err, test.want) {
				t.Fatalf("RecordEffect() error = %v, want %v", err, test.want)
			}
		})
	}
}

func openIntegrationPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Fatal("DATABASE_URL must be set for PostgreSQL integration tests")
	}

	pool, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	return pool
}

func integrationRunID(t *testing.T, suffix string) run.ID {
	t.Helper()
	return run.ID(fmt.Sprintf("run-%s-%d", suffix, time.Now().UnixNano()))
}

func integrationEventID(t *testing.T, suffix string) string {
	t.Helper()
	return fmt.Sprintf("event-%s-%d", suffix, time.Now().UnixNano())
}

func newQueuedEvent(t *testing.T, eventID string, runID run.ID) event.Envelope {
	return newLifecycleEvent(t, eventID, runID, event.TypeWorkflowQueued, run.StatusPending)
}

func newLifecycleEvent(t *testing.T, eventID string, runID run.ID, typ event.Type, status run.Status) event.Envelope {
	t.Helper()

	envelope, err := event.New(
		eventID,
		runID,
		run.StepKey("workflow"),
		typ,
		time.Now().UTC(),
		event.LifecyclePayload{Status: status},
	)
	if err != nil {
		t.Fatalf("new lifecycle event: %v", err)
	}

	return envelope
}

func newApprovalRequestedEvent(t *testing.T, eventID string, request ApprovalRequest) event.Envelope {
	t.Helper()

	envelope, err := event.New(
		eventID,
		request.RunID,
		request.StepKey,
		event.TypeApprovalRequested,
		time.Now().UTC(),
		event.ToolPayload{CallID: request.CallID, ToolName: request.ToolName},
	)
	if err != nil {
		t.Fatalf("new approval requested event: %v", err)
	}

	return envelope
}

func newApprovalResolvedEvent(t *testing.T, eventID string, stepKey run.StepKey, signal ApprovalSignal, approved bool) event.Envelope {
	t.Helper()

	envelope, err := event.New(
		eventID,
		signal.RunID,
		stepKey,
		event.TypeApprovalResolved,
		time.Now().UTC(),
		event.ApprovalPayload{RequestID: signal.RequestID, Approved: approved},
	)
	if err != nil {
		t.Fatalf("new approval resolved event: %v", err)
	}

	return envelope
}

func waitingApprovalIntegrationRun(t *testing.T, pool *pgxpool.Pool, ctx context.Context, suffix string) (run.Run, ApprovalRequest) {
	t.Helper()

	r := runningIntegrationRun(t, pool, ctx, suffix)
	if err := r.Wait(); err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	request := ApprovalRequest{
		ID:       "approval-" + string(r.ID),
		RunID:    r.ID,
		StepKey:  run.StepKey("tool/1/issue-credit"),
		CallID:   "call-" + string(r.ID),
		ToolName: "issue_credit",
	}
	requested := newApprovalRequestedEvent(t, integrationEventID(t, "approval-requested-"+suffix), request)
	if err := NewStore(pool).RequestApproval(ctx, r, request, requested); err != nil {
		t.Fatalf("RequestApproval() error = %v", err)
	}

	return r, request
}

func runningIntegrationRun(t *testing.T, pool *pgxpool.Pool, ctx context.Context, suffix string) run.Run {
	t.Helper()

	r := pendingIntegrationRun(t, pool, ctx, suffix)
	if err := r.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	if _, err := pool.Exec(
		ctx,
		"UPDATE runs SET status = $2, updated_at = $3 WHERE id = $1",
		r.ID,
		r.Status,
		time.Now().UTC(),
	); err != nil {
		t.Fatalf("update running run: %v", err)
	}

	return r
}

func pendingIntegrationRun(t *testing.T, pool *pgxpool.Pool, ctx context.Context, suffix string) run.Run {
	t.Helper()

	r := run.New(integrationRunID(t, suffix))
	now := time.Now().UTC()
	if _, err := pool.Exec(
		ctx,
		`INSERT INTO runs (id, status, created_at, updated_at)
		 VALUES ($1, $2, $3, $3)`,
		r.ID,
		r.Status,
		now,
	); err != nil {
		t.Fatalf("insert pending run: %v", err)
	}

	return r
}

func insertIntegrationEvent(t *testing.T, pool *pgxpool.Pool, ctx context.Context, eventID string, runID run.ID, typ event.Type, status run.Status) event.Stored {
	t.Helper()

	envelope := newLifecycleEvent(t, eventID, runID, typ, status)
	var sequence int64
	if err := pool.QueryRow(
		ctx,
		`INSERT INTO events (id, run_id, step_key, type, occurred_at, payload)
		 VALUES ($1, $2, $3, $4, $5, $6::jsonb)
		 RETURNING sequence`,
		envelope.ID(),
		envelope.RunID(),
		envelope.StepKey(),
		envelope.Type(),
		envelope.OccurredAt(),
		string(envelope.Payload()),
	).Scan(&sequence); err != nil {
		t.Fatalf("insert event: %v", err)
	}

	stored, err := event.NewStored(sequence, envelope.ID(), envelope.RunID(), envelope.StepKey(), envelope.Type(), envelope.OccurredAt(), envelope.Payload())
	if err != nil {
		t.Fatalf("new stored event: %v", err)
	}

	return stored
}

func latestEventSequence(t *testing.T, pool *pgxpool.Pool, ctx context.Context) int64 {
	t.Helper()

	var sequence int64
	if err := pool.QueryRow(ctx, "SELECT coalesce(max(sequence), 0) FROM events").Scan(&sequence); err != nil {
		t.Fatalf("query latest event sequence: %v", err)
	}

	return sequence
}
