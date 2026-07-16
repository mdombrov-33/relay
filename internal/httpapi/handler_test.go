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

type fakeRunReader struct {
	record      postgres.RunRecord
	events      []event.Stored
	err         error
	eventsErr   error
	runID       run.ID
	eventsRunID run.ID
	after       int64
	findCalls   int
	eventCalls  int
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

func serveRequest(handler http.Handler, method, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequestWithContext(context.Background(), method, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
