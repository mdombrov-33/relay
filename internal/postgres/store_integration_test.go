//go:build integration

package postgres

import (
	"context"
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
