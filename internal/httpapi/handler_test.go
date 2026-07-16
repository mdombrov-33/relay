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
		{name: "extra segment", method: http.MethodGet, path: "/v1/runs/run-123/events", want: http.StatusNotFound},
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

type fakeRunReader struct {
	record postgres.RunRecord
	err    error
	runID  run.ID
}

func (f *fakeRunReader) FindRun(_ context.Context, runID run.ID) (postgres.RunRecord, error) {
	f.runID = runID
	return f.record, f.err
}

func serveRequest(handler http.Handler, method, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequestWithContext(context.Background(), method, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
