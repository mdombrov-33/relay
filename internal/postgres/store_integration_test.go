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
	queued := newQueuedEvent(t, "event-created", r.ID)

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

func newQueuedEvent(t *testing.T, eventID string, runID run.ID) event.Envelope {
	t.Helper()

	queued, err := event.New(
		eventID,
		runID,
		run.StepKey("workflow"),
		event.TypeWorkflowQueued,
		time.Now().UTC(),
		event.LifecyclePayload{Status: run.StatusPending},
	)
	if err != nil {
		t.Fatalf("new queued event: %v", err)
	}

	return queued
}
