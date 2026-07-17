package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mdombrov-33/relay/internal/event"
	"github.com/mdombrov-33/relay/internal/postgres"
	"github.com/mdombrov-33/relay/internal/run"
)

func TestGetRun(t *testing.T) {
	createdAt := time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)
	requestedAt := createdAt.Add(30 * time.Second)
	record := postgres.RunRecord{
		Run:       run.Run{ID: run.ID("run-123"), Status: run.StatusWaiting},
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
		PendingApproval: &postgres.ApprovalRequestRecord{
			ApprovalRequest: postgres.ApprovalRequest{
				ID:       "approval-123",
				RunID:    run.ID("run-123"),
				StepKey:  run.StepKey("tool/1/issue-credit"),
				CallID:   "call-123",
				ToolName: "issue_credit",
			},
			Status:      postgres.ApprovalStatusPending,
			RequestedAt: requestedAt,
		},
	}
	store := &fakeRunReader{record: record}

	response := serveRequest(NewHandler(store), http.MethodGet, "/v1/runs/run-123")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", response.Header().Get("Content-Type"))
	}
	var body runResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.ID != record.Run.ID || body.Status != record.Run.Status || !body.CreatedAt.Equal(createdAt) || !body.UpdatedAt.Equal(updatedAt) {
		t.Errorf("run response = %#v, want record identity, status, and timestamps", body)
	}
	if body.PendingApproval == nil {
		t.Fatal("pendingApproval = nil, want pending approval")
	}
	if body.PendingApproval.ID != record.PendingApproval.ID || body.PendingApproval.StepKey != record.PendingApproval.StepKey || body.PendingApproval.CallID != record.PendingApproval.CallID || body.PendingApproval.ToolName != record.PendingApproval.ToolName || !body.PendingApproval.RequestedAt.Equal(requestedAt) {
		t.Errorf("pendingApproval = %#v, want %#v", body.PendingApproval, record.PendingApproval)
	}
	if store.runID != record.Run.ID {
		t.Errorf("FindRun() run ID = %q, want %q", store.runID, record.Run.ID)
	}
}

func TestListRuns(t *testing.T) {
	createdAt := time.Date(2026, time.July, 17, 10, 0, 0, 0, time.UTC)
	waiting := postgres.RunRecord{
		Run:       run.Run{ID: run.ID("run-2"), Status: run.StatusWaiting},
		CreatedAt: createdAt.Add(time.Minute),
		UpdatedAt: createdAt.Add(2 * time.Minute),
		PendingApproval: &postgres.ApprovalRequestRecord{
			ApprovalRequest: postgres.ApprovalRequest{
				ID:       "approval-2",
				RunID:    run.ID("run-2"),
				StepKey:  run.StepKey("tool/1/issue-credit"),
				CallID:   "call-2",
				ToolName: "issue_credit",
			},
			Status:      postgres.ApprovalStatusPending,
			RequestedAt: createdAt.Add(90 * time.Second),
		},
	}
	pending := postgres.RunRecord{
		Run:       run.Run{ID: run.ID("run-1"), Status: run.StatusPending},
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}
	store := &fakeRunReader{records: []postgres.RunRecord{waiting, pending}}

	response := serveRequest(NewHandler(store), http.MethodGet, "/v1/runs")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var body runsResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Runs) != 2 {
		t.Fatalf("len(runs) = %d, want 2", len(body.Runs))
	}
	if body.Runs[0].ID != waiting.Run.ID || body.Runs[0].Status != run.StatusWaiting {
		t.Errorf("runs[0] = %#v, want waiting run-2 first", body.Runs[0])
	}
	if body.Runs[0].PendingApproval == nil || body.Runs[0].PendingApproval.ID != "approval-2" {
		t.Errorf("runs[0].pendingApproval = %#v, want approval-2", body.Runs[0].PendingApproval)
	}
	if body.Runs[1].ID != pending.Run.ID || body.Runs[1].PendingApproval != nil {
		t.Errorf("runs[1] = %#v, want pending run-1 without approval", body.Runs[1])
	}
	if store.listRunsCalls != 1 {
		t.Errorf("ListRuns() calls = %d, want 1", store.listRunsCalls)
	}
}

func TestListRunsReturnsEmptyList(t *testing.T) {
	response := serveRequest(NewHandler(&fakeRunReader{}), http.MethodGet, "/v1/runs")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if strings.TrimSpace(response.Body.String()) != `{"runs":[]}` {
		t.Errorf("body = %q, want empty runs list", strings.TrimSpace(response.Body.String()))
	}
}

func TestListRunsMapsStoreErrors(t *testing.T) {
	store := &fakeRunReader{listRunsErr: errors.New("password=secret")}

	response := serveRequest(NewHandler(store), http.MethodGet, "/v1/runs")

	if response.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if strings.TrimSpace(response.Body.String()) != `{"error":"internal server error"}` {
		t.Errorf("body = %q, want generic error", strings.TrimSpace(response.Body.String()))
	}
}

func TestCreateRun(t *testing.T) {
	now := time.Date(2026, time.July, 17, 15, 0, 0, 0, time.UTC)
	store := &fakeRunReader{}
	ids := []string{"run-123", "event-123"}
	handler := newHandler(store, func() time.Time { return now }, func(string) (string, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	})

	response := serveRequest(handler, http.MethodPost, "/v1/runs")

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusCreated, response.Body.String())
	}
	if response.Header().Get("Location") != "/v1/runs/run-123" {
		t.Errorf("Location = %q, want /v1/runs/run-123", response.Header().Get("Location"))
	}
	if strings.TrimSpace(response.Body.String()) != `{"id":"run-123","status":"pending"}` {
		t.Errorf("body = %q, want pending run", strings.TrimSpace(response.Body.String()))
	}
	if store.createdRun != (run.Run{ID: run.ID("run-123"), Status: run.StatusPending}) {
		t.Errorf("CreateRun() run = %#v, want pending run-123", store.createdRun)
	}
	if store.queued.ID() != "event-123" || store.queued.RunID() != run.ID("run-123") || store.queued.StepKey() != run.StepKey("workflow") || store.queued.Type() != event.TypeWorkflowQueued || !store.queued.OccurredAt().Equal(now) {
		t.Errorf("CreateRun() event = %#v, want server-owned queued event", store.queued)
	}
	var payload event.LifecyclePayload
	if err := json.Unmarshal(store.queued.Payload(), &payload); err != nil {
		t.Fatalf("decode queued payload: %v", err)
	}
	if payload.Status != run.StatusPending {
		t.Errorf("queued payload status = %q, want %q", payload.Status, run.StatusPending)
	}
}

func TestCreateRunMapsFailures(t *testing.T) {
	tests := []struct {
		name  string
		store *fakeRunReader
		newID func(string) (string, error)
	}{
		{
			name:  "run ID generation",
			store: &fakeRunReader{},
			newID: func(string) (string, error) { return "", errors.New("random source failed") },
		},
		{
			name:  "event ID generation",
			store: &fakeRunReader{},
			newID: func(prefix string) (string, error) {
				if prefix == "run" {
					return "run-123", nil
				}
				return "", errors.New("random source failed")
			},
		},
		{
			name:  "store failure",
			store: &fakeRunReader{createErr: errors.New("password=secret")},
			newID: func(prefix string) (string, error) { return prefix + "-123", nil },
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := newHandler(test.store, time.Now, test.newID)
			response := serveRequest(handler, http.MethodPost, "/v1/runs")

			if response.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want %d", response.Code, http.StatusInternalServerError)
			}
			if strings.TrimSpace(response.Body.String()) != `{"error":"internal server error"}` {
				t.Errorf("body = %q, want generic error", strings.TrimSpace(response.Body.String()))
			}
		})
	}
}

func TestStreamEvents(t *testing.T) {
	occurredAt := time.Date(2026, time.July, 17, 16, 0, 0, 0, time.UTC)
	stored, err := event.NewStored(
		42,
		"event-42",
		run.ID("run-123"),
		run.StepKey("workflow"),
		event.TypeWorkflowQueued,
		occurredAt,
		json.RawMessage(`{"status":"pending"}`),
	)
	if err != nil {
		t.Fatalf("NewStored() error = %v", err)
	}
	store := &fakeRunReader{globalPages: [][]event.Stored{{stored}, {}}}
	handler := newHandlerWithWait(store, time.Now, randomID, func(context.Context) error {
		return context.Canceled
	})

	response := serveRequest(handler, http.MethodGet, "/v1/events/stream?after=41")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", response.Header().Get("Content-Type"))
	}
	if response.Header().Get("Cache-Control") != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", response.Header().Get("Cache-Control"))
	}
	want := "id: 42\ndata: {\"sequence\":42,\"id\":\"event-42\",\"runId\":\"run-123\",\"stepKey\":\"workflow\",\"type\":\"workflow.queued.v1\",\"occurredAt\":\"2026-07-17T16:00:00Z\",\"payload\":{\"status\":\"pending\"}}\n\n"
	if response.Body.String() != want {
		t.Errorf("body = %q, want %q", response.Body.String(), want)
	}
	if len(store.globalAfter) != 2 || store.globalAfter[0] != 41 || store.globalAfter[1] != 42 {
		t.Errorf("ListEventsAfter() cursors = %v, want [41 42]", store.globalAfter)
	}
}

func TestStreamEventsRejectsInvalidCursor(t *testing.T) {
	tests := []string{
		"?after=",
		"?after=-1",
		"?after=abc",
		"?after=1&after=2",
	}

	for _, query := range tests {
		t.Run(query, func(t *testing.T) {
			store := &fakeRunReader{}
			response := serveRequest(NewHandler(store), http.MethodGet, "/v1/events/stream"+query)

			if response.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", response.Code, http.StatusBadRequest)
			}
			if store.globalCalls != 0 {
				t.Errorf("ListEventsAfter() calls = %d, want 0", store.globalCalls)
			}
		})
	}
}

func TestStreamEventsMapsInitialStoreFailure(t *testing.T) {
	store := &fakeRunReader{globalErr: errors.New("password=secret")}

	response := serveRequest(NewHandler(store), http.MethodGet, "/v1/events/stream")

	if response.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if response.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", response.Header().Get("Content-Type"))
	}
	if strings.TrimSpace(response.Body.String()) != `{"error":"internal server error"}` {
		t.Errorf("body = %q, want generic error", strings.TrimSpace(response.Body.String()))
	}
}

func TestStreamEventsStopsWhenRequestIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store := &fakeRunReader{globalPages: [][]event.Stored{{}}}
	waitCalls := 0
	handler := newHandlerWithWait(store, time.Now, randomID, func(ctx context.Context) error {
		waitCalls++
		cancel()
		<-ctx.Done()
		return ctx.Err()
	})
	request := httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/events/stream", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if waitCalls != 1 || store.globalCalls != 1 {
		t.Errorf("calls = (%d waits, %d reads), want (1, 1)", waitCalls, store.globalCalls)
	}
}

func TestStreamEventsClosesAfterStoreFailure(t *testing.T) {
	stored, err := event.NewStored(
		42,
		"event-42",
		run.ID("run-123"),
		run.StepKey("workflow"),
		event.TypeWorkflowQueued,
		time.Date(2026, time.July, 17, 16, 0, 0, 0, time.UTC),
		json.RawMessage(`{"status":"pending"}`),
	)
	if err != nil {
		t.Fatalf("NewStored() error = %v", err)
	}
	store := &fakeRunReader{}
	store.listGlobal = func(_ context.Context, after int64) ([]event.Stored, error) {
		if after == 0 {
			return []event.Stored{stored}, nil
		}
		return nil, errors.New("password=secret")
	}

	response := serveRequest(NewHandler(store), http.MethodGet, "/v1/events/stream")

	if response.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if !strings.Contains(response.Body.String(), "id: 42") {
		t.Errorf("body = %q, want delivered event", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "password") {
		t.Errorf("body = %q, want storage error hidden", response.Body.String())
	}
}

func TestGetRunOmitsPendingApproval(t *testing.T) {
	store := &fakeRunReader{record: postgres.RunRecord{Run: run.Run{ID: run.ID("run-123"), Status: run.StatusRunning}}}

	response := serveRequest(NewHandler(store), http.MethodGet, "/v1/runs/run-123")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if strings.Contains(response.Body.String(), "pendingApproval") {
		t.Errorf("body = %s, want pendingApproval omitted", response.Body.String())
	}
}

func TestGetRunMapsStoreErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{name: "not found", err: postgres.ErrRunNotFound, wantStatus: http.StatusNotFound, wantBody: `{"error":"run not found"}`},
		{name: "store failure", err: errors.New("password=secret"), wantStatus: http.StatusInternalServerError, wantBody: `{"error":"internal server error"}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := serveRequest(NewHandler(&fakeRunReader{err: test.err}), http.MethodGet, "/v1/runs/run-123")

			if response.Code != test.wantStatus {
				t.Errorf("status = %d, want %d", response.Code, test.wantStatus)
			}
			if strings.TrimSpace(response.Body.String()) != test.wantBody {
				t.Errorf("body = %q, want %q", strings.TrimSpace(response.Body.String()), test.wantBody)
			}
		})
	}
}

func TestGetRunRejectsOtherRoutes(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{name: "missing ID", method: http.MethodGet, path: "/v1/runs/", want: http.StatusNotFound},
		{name: "extra segment", method: http.MethodGet, path: "/v1/runs/run-123/events/more", want: http.StatusNotFound},
		{name: "wrong method", method: http.MethodPost, path: "/v1/runs/run-123", want: http.StatusMethodNotAllowed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := serveRequest(NewHandler(&fakeRunReader{}), test.method, test.path)
			if response.Code != test.want {
				t.Errorf("status = %d, want %d", response.Code, test.want)
			}
		})
	}
}

func TestGetRunEvents(t *testing.T) {
	occurredAt := time.Date(2026, time.July, 17, 11, 0, 0, 0, time.UTC)
	stored, err := event.NewStored(
		42,
		"event-42",
		run.ID("run-123"),
		run.StepKey("tool/1/issue-credit"),
		event.TypeToolCompleted,
		occurredAt,
		json.RawMessage(`{"callId":"call-123","toolName":"issue_credit"}`),
	)
	if err != nil {
		t.Fatalf("NewStored() error = %v", err)
	}
	store := &fakeRunReader{
		record: postgres.RunRecord{Run: run.Run{ID: run.ID("run-123"), Status: run.StatusRunning}},
		events: []event.Stored{stored},
	}

	response := serveRequest(NewHandler(store), http.MethodGet, "/v1/runs/run-123/events?after=41")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var body eventsResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Events) != 1 {
		t.Fatalf("events length = %d, want 1", len(body.Events))
	}
	got := body.Events[0]
	if got.Sequence != stored.Sequence || got.ID != stored.ID() || got.RunID != stored.RunID() || got.StepKey != stored.StepKey() || got.Type != stored.Type() || !got.OccurredAt.Equal(occurredAt) || string(got.Payload) != string(stored.Payload()) {
		t.Errorf("event = %#v, want stored event %#v", got, stored)
	}
	if body.NextAfter != stored.Sequence {
		t.Errorf("nextAfter = %d, want %d", body.NextAfter, stored.Sequence)
	}
	if store.eventsRunID != stored.RunID() || store.after != 41 {
		t.Errorf("ListRunEvents() arguments = (%q, %d), want (%q, 41)", store.eventsRunID, store.after, stored.RunID())
	}
}

func TestGetRunEventsReturnsEmptyPageFromCursor(t *testing.T) {
	store := &fakeRunReader{record: postgres.RunRecord{Run: run.Run{ID: run.ID("run-123")}}}

	response := serveRequest(NewHandler(store), http.MethodGet, "/v1/runs/run-123/events?after=42")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var body eventsResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Events == nil || len(body.Events) != 0 || body.NextAfter != 42 {
		t.Errorf("response = %#v, want empty events and nextAfter 42", body)
	}
}

func TestGetRunEventsRejectsInvalidCursor(t *testing.T) {
	tests := []string{
		"?after=",
		"?after=-1",
		"?after=abc",
		"?after=1&after=2",
	}

	for _, query := range tests {
		t.Run(query, func(t *testing.T) {
			store := &fakeRunReader{}
			response := serveRequest(NewHandler(store), http.MethodGet, "/v1/runs/run-123/events"+query)

			if response.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", response.Code, http.StatusBadRequest)
			}
			if store.findCalls != 0 || store.eventCalls != 0 {
				t.Errorf("store calls = (%d find, %d event), want none", store.findCalls, store.eventCalls)
			}
		})
	}
}

func TestGetRunEventsMapsStoreErrors(t *testing.T) {
	tests := []struct {
		name       string
		store      *fakeRunReader
		wantStatus int
		wantBody   string
	}{
		{name: "run not found", store: &fakeRunReader{err: postgres.ErrRunNotFound}, wantStatus: http.StatusNotFound, wantBody: `{"error":"run not found"}`},
		{name: "run read failure", store: &fakeRunReader{err: errors.New("password=secret")}, wantStatus: http.StatusInternalServerError, wantBody: `{"error":"internal server error"}`},
		{name: "event read failure", store: &fakeRunReader{eventsErr: errors.New("password=secret")}, wantStatus: http.StatusInternalServerError, wantBody: `{"error":"internal server error"}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := serveRequest(NewHandler(test.store), http.MethodGet, "/v1/runs/run-123/events")

			if response.Code != test.wantStatus {
				t.Errorf("status = %d, want %d", response.Code, test.wantStatus)
			}
			if strings.TrimSpace(response.Body.String()) != test.wantBody {
				t.Errorf("body = %q, want %q", strings.TrimSpace(response.Body.String()), test.wantBody)
			}
		})
	}
}

func TestResolveApproval(t *testing.T) {
	now := time.Date(2026, time.July, 17, 12, 0, 0, 0, time.UTC)
	store := &fakeRunReader{
		approval: postgres.ApprovalRequestRecord{
			ApprovalRequest: postgres.ApprovalRequest{
				ID:      "approval-123",
				RunID:   run.ID("run-123"),
				StepKey: run.StepKey("tool/1/issue-credit"),
			},
		},
	}
	ids := []string{"signal-123", "event-123"}
	handler := newHandler(store, func() time.Time { return now }, func(string) (string, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	})

	response := serveRequestWithBody(handler, http.MethodPost, "/v1/runs/run-123/signals/approval", `{"requestId":"approval-123","decision":"approved"}`)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if strings.TrimSpace(response.Body.String()) != `{"requestId":"approval-123","decision":"approved"}` {
		t.Errorf("body = %q, want stable approval response", strings.TrimSpace(response.Body.String()))
	}
	if store.approvalRequestID != "approval-123" {
		t.Errorf("FindApprovalRequest() request ID = %q, want approval-123", store.approvalRequestID)
	}
	if store.signal.ID != "signal-123" || store.signal.RequestID != "approval-123" || store.signal.RunID != run.ID("run-123") || store.signal.Decision != postgres.ApprovalDecisionApproved {
		t.Errorf("ResolveApproval() signal = %#v, want server-owned approved signal", store.signal)
	}
	if store.resolved.ID() != "event-123" || store.resolved.RunID() != run.ID("run-123") || store.resolved.StepKey() != run.StepKey("tool/1/issue-credit") || store.resolved.Type() != event.TypeApprovalResolved || !store.resolved.OccurredAt().Equal(now) {
		t.Errorf("ResolveApproval() event = %#v, want server-owned resolution event", store.resolved)
	}
	var payload event.ApprovalPayload
	if err := json.Unmarshal(store.resolved.Payload(), &payload); err != nil {
		t.Fatalf("decode resolved payload: %v", err)
	}
	if payload.RequestID != "approval-123" || !payload.Approved {
		t.Errorf("resolved payload = %#v, want approved request", payload)
	}
}

func TestResolveApprovalReturnsSameOutcomeForMatchingDuplicate(t *testing.T) {
	store := &fakeRunReader{
		approval: postgres.ApprovalRequestRecord{ApprovalRequest: postgres.ApprovalRequest{
			ID: "approval-123", RunID: run.ID("run-123"), StepKey: run.StepKey("tool/1"),
		}},
		approvalCreated: false,
	}
	handler := newHandler(store, time.Now, func(prefix string) (string, error) { return prefix + "-123", nil })

	response := serveRequestWithBody(handler, http.MethodPost, "/v1/runs/run-123/signals/approval", `{"requestId":"approval-123","decision":"rejected"}`)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if strings.TrimSpace(response.Body.String()) != `{"requestId":"approval-123","decision":"rejected"}` {
		t.Errorf("body = %q, want original successful outcome", strings.TrimSpace(response.Body.String()))
	}
}

func TestResolveApprovalRejectsInvalidCommand(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "empty body", body: ""},
		{name: "malformed JSON", body: `{"requestId":`},
		{name: "unknown field", body: `{"requestId":"approval-123","decision":"approved","extra":true}`},
		{name: "multiple values", body: `{"requestId":"approval-123","decision":"approved"} {}`},
		{name: "missing request ID", body: `{"decision":"approved"}`},
		{name: "invalid decision", body: `{"requestId":"approval-123","decision":"deferred"}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeRunReader{}
			response := serveRequestWithBody(NewHandler(store), http.MethodPost, "/v1/runs/run-123/signals/approval", test.body)

			if response.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", response.Code, http.StatusBadRequest)
			}
			if store.approvalFindCalls != 0 || store.approvalResolveCalls != 0 {
				t.Errorf("approval store calls = (%d find, %d resolve), want none", store.approvalFindCalls, store.approvalResolveCalls)
			}
		})
	}
}

func TestResolveApprovalMapsErrors(t *testing.T) {
	tests := []struct {
		name       string
		store      *fakeRunReader
		wantStatus int
		wantBody   string
	}{
		{
			name:       "request not found",
			store:      &fakeRunReader{approvalErr: postgres.ErrApprovalRequestNotFound},
			wantStatus: http.StatusNotFound,
			wantBody:   `{"error":"approval request not found"}`,
		},
		{
			name: "request belongs to another run",
			store: &fakeRunReader{approval: postgres.ApprovalRequestRecord{ApprovalRequest: postgres.ApprovalRequest{
				ID: "approval-123", RunID: run.ID("run-456"), StepKey: run.StepKey("tool/1"),
			}}},
			wantStatus: http.StatusNotFound,
			wantBody:   `{"error":"approval request not found"}`,
		},
		{
			name: "decision conflict",
			store: &fakeRunReader{
				approval:           postgres.ApprovalRequestRecord{ApprovalRequest: postgres.ApprovalRequest{ID: "approval-123", RunID: run.ID("run-123"), StepKey: run.StepKey("tool/1")}},
				approvalResolveErr: postgres.ErrApprovalDecisionConflict,
			},
			wantStatus: http.StatusConflict,
			wantBody:   `{"error":"approval decision conflicts with the recorded decision"}`,
		},
		{
			name:       "store failure",
			store:      &fakeRunReader{approvalErr: errors.New("password=secret")},
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"error":"internal server error"}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := newHandler(test.store, time.Now, func(prefix string) (string, error) { return prefix + "-123", nil })
			response := serveRequestWithBody(handler, http.MethodPost, "/v1/runs/run-123/signals/approval", `{"requestId":"approval-123","decision":"approved"}`)

			if response.Code != test.wantStatus {
				t.Errorf("status = %d, want %d", response.Code, test.wantStatus)
			}
			if strings.TrimSpace(response.Body.String()) != test.wantBody {
				t.Errorf("body = %q, want %q", strings.TrimSpace(response.Body.String()), test.wantBody)
			}
		})
	}
}

func TestCancelRun(t *testing.T) {
	now := time.Date(2026, time.July, 17, 14, 0, 0, 0, time.UTC)
	store := &fakeRunReader{}
	handler := newHandler(store, func() time.Time { return now }, func(prefix string) (string, error) { return prefix + "-123", nil })

	response := serveRequest(handler, http.MethodPost, "/v1/runs/run-123/cancel")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if strings.TrimSpace(response.Body.String()) != `{"id":"run-123","status":"canceled"}` {
		t.Errorf("body = %q, want canceled run", strings.TrimSpace(response.Body.String()))
	}
	if store.canceledRunID != run.ID("run-123") {
		t.Errorf("CancelRun() run ID = %q, want run-123", store.canceledRunID)
	}
	if store.canceled.ID() != "event-123" || store.canceled.RunID() != run.ID("run-123") || store.canceled.StepKey() != run.StepKey("workflow") || store.canceled.Type() != event.TypeWorkflowCancelled || !store.canceled.OccurredAt().Equal(now) {
		t.Errorf("CancelRun() event = %#v, want server-owned cancellation event", store.canceled)
	}
	var payload event.LifecyclePayload
	if err := json.Unmarshal(store.canceled.Payload(), &payload); err != nil {
		t.Fatalf("decode cancellation payload: %v", err)
	}
	if payload.Status != run.StatusCanceled {
		t.Errorf("cancellation payload status = %q, want %q", payload.Status, run.StatusCanceled)
	}
}

func TestCancelRunMapsStoreErrors(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{name: "not found", err: postgres.ErrRunNotFound, wantStatus: http.StatusNotFound, wantBody: `{"error":"run not found"}`},
		{name: "already terminal", err: postgres.ErrRunAlreadyTerminal, wantStatus: http.StatusConflict, wantBody: `{"error":"run is already terminal"}`},
		{name: "store failure", err: errors.New("password=secret"), wantStatus: http.StatusInternalServerError, wantBody: `{"error":"internal server error"}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeRunReader{cancelErr: test.err}
			handler := newHandler(store, time.Now, func(prefix string) (string, error) { return prefix + "-123", nil })
			response := serveRequest(handler, http.MethodPost, "/v1/runs/run-123/cancel")

			if response.Code != test.wantStatus {
				t.Errorf("status = %d, want %d", response.Code, test.wantStatus)
			}
			if strings.TrimSpace(response.Body.String()) != test.wantBody {
				t.Errorf("body = %q, want %q", strings.TrimSpace(response.Body.String()), test.wantBody)
			}
		})
	}
}

type fakeRunReader struct {
	record               postgres.RunRecord
	records              []postgres.RunRecord
	listRunsErr          error
	listRunsCalls        int
	events               []event.Stored
	approval             postgres.ApprovalRequestRecord
	err                  error
	eventsErr            error
	globalErr            error
	approvalErr          error
	approvalResolveErr   error
	cancelErr            error
	createErr            error
	approvalCreated      bool
	runID                run.ID
	eventsRunID          run.ID
	globalPages          [][]event.Stored
	globalAfter          []int64
	listGlobal           func(context.Context, int64) ([]event.Stored, error)
	approvalRequestID    string
	signal               postgres.ApprovalSignal
	resolved             event.Envelope
	canceledRunID        run.ID
	canceled             event.Envelope
	createdRun           run.Run
	queued               event.Envelope
	after                int64
	findCalls            int
	eventCalls           int
	globalCalls          int
	approvalFindCalls    int
	approvalResolveCalls int
	cancelCalls          int
	createCalls          int
}

func (f *fakeRunReader) CancelRun(_ context.Context, runID run.ID, canceled event.Envelope) error {
	f.cancelCalls++
	f.canceledRunID = runID
	f.canceled = canceled
	return f.cancelErr
}

func (f *fakeRunReader) CreateRun(_ context.Context, r run.Run, queued event.Envelope) error {
	f.createCalls++
	f.createdRun = r
	f.queued = queued
	return f.createErr
}

func (f *fakeRunReader) ListRuns(_ context.Context) ([]postgres.RunRecord, error) {
	f.listRunsCalls++
	return f.records, f.listRunsErr
}

func (f *fakeRunReader) FindRun(_ context.Context, runID run.ID) (postgres.RunRecord, error) {
	f.findCalls++
	f.runID = runID
	return f.record, f.err
}

func (f *fakeRunReader) ListRunEvents(_ context.Context, runID run.ID, after int64) ([]event.Stored, error) {
	f.eventCalls++
	f.eventsRunID = runID
	f.after = after
	return f.events, f.eventsErr
}

func (f *fakeRunReader) ListEventsAfter(ctx context.Context, after int64) ([]event.Stored, error) {
	f.globalCalls++
	f.globalAfter = append(f.globalAfter, after)
	if f.listGlobal != nil {
		return f.listGlobal(ctx, after)
	}
	if f.globalErr != nil {
		return nil, f.globalErr
	}
	if len(f.globalPages) == 0 {
		return nil, nil
	}
	page := f.globalPages[0]
	f.globalPages = f.globalPages[1:]
	return page, nil
}

func (f *fakeRunReader) FindApprovalRequest(_ context.Context, requestID string) (postgres.ApprovalRequestRecord, error) {
	f.approvalFindCalls++
	f.approvalRequestID = requestID
	return f.approval, f.approvalErr
}

func (f *fakeRunReader) ResolveApproval(_ context.Context, signal postgres.ApprovalSignal, resolved event.Envelope) (bool, error) {
	f.approvalResolveCalls++
	f.signal = signal
	f.resolved = resolved
	return f.approvalCreated, f.approvalResolveErr
}

func serveRequest(handler http.Handler, method, path string) *httptest.ResponseRecorder {
	return serveRequestWithBody(handler, method, path, "")
}

func serveRequestWithBody(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
