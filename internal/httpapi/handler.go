package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/mdombrov-33/relay/internal/event"
	"github.com/mdombrov-33/relay/internal/postgres"
	"github.com/mdombrov-33/relay/internal/run"
)

type runReader interface {
	FindRun(ctx context.Context, runID run.ID) (postgres.RunRecord, error)
	ListRunEvents(ctx context.Context, runID run.ID, afterSequence int64) ([]event.Stored, error)
}

type Handler struct {
	store runReader
	mux   *http.ServeMux
}

type runResponse struct {
	ID              run.ID            `json:"id"`
	Status          run.Status        `json:"status"`
	CreatedAt       time.Time         `json:"createdAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
	PendingApproval *approvalResponse `json:"pendingApproval,omitempty"`
}

type approvalResponse struct {
	ID          string      `json:"id"`
	StepKey     run.StepKey `json:"stepKey"`
	CallID      string      `json:"callId"`
	ToolName    string      `json:"toolName"`
	RequestedAt time.Time   `json:"requestedAt"`
}

type errorResponse struct {
	Error string `json:"error"`
}

type eventsResponse struct {
	Events    []eventResponse `json:"events"`
	NextAfter int64           `json:"nextAfter"`
}

type eventResponse struct {
	Sequence   int64           `json:"sequence"`
	ID         string          `json:"id"`
	RunID      run.ID          `json:"runId"`
	StepKey    run.StepKey     `json:"stepKey"`
	Type       event.Type      `json:"type"`
	OccurredAt time.Time       `json:"occurredAt"`
	Payload    json.RawMessage `json:"payload"`
}

func NewHandler(store runReader) http.Handler {
	h := &Handler{store: store, mux: http.NewServeMux()}
	h.mux.HandleFunc("GET /v1/runs/{id}", h.getRun)
	h.mux.HandleFunc("GET /v1/runs/{id}/events", h.getRunEvents)
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) getRun(w http.ResponseWriter, r *http.Request) {
	record, err := h.store.FindRun(r.Context(), run.ID(r.PathValue("id")))
	if err != nil {
		if errors.Is(err, postgres.ErrRunNotFound) {
			writeJSON(w, http.StatusNotFound, errorResponse{Error: "run not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
		return
	}

	response := runResponse{
		ID:        record.Run.ID,
		Status:    record.Run.Status,
		CreatedAt: record.CreatedAt,
		UpdatedAt: record.UpdatedAt,
	}
	if record.PendingApproval != nil {
		response.PendingApproval = &approvalResponse{
			ID:          record.PendingApproval.ID,
			StepKey:     record.PendingApproval.StepKey,
			CallID:      record.PendingApproval.CallID,
			ToolName:    record.PendingApproval.ToolName,
			RequestedAt: record.PendingApproval.RequestedAt,
		}
	}

	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) getRunEvents(w http.ResponseWriter, r *http.Request) {
	after, err := parseAfter(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "after must be one non-negative integer"})
		return
	}

	runID := run.ID(r.PathValue("id"))
	if _, err := h.store.FindRun(r.Context(), runID); err != nil {
		if errors.Is(err, postgres.ErrRunNotFound) {
			writeJSON(w, http.StatusNotFound, errorResponse{Error: "run not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
		return
	}

	stored, err := h.store.ListRunEvents(r.Context(), runID, after)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
		return
	}

	response := eventsResponse{
		Events:    make([]eventResponse, 0, len(stored)),
		NextAfter: after,
	}
	for _, item := range stored {
		response.Events = append(response.Events, eventResponse{
			Sequence:   item.Sequence,
			ID:         item.ID(),
			RunID:      item.RunID(),
			StepKey:    item.StepKey(),
			Type:       item.Type(),
			OccurredAt: item.OccurredAt(),
			Payload:    item.Payload(),
		})
		response.NextAfter = item.Sequence
	}

	writeJSON(w, http.StatusOK, response)
}

func parseAfter(r *http.Request) (int64, error) {
	values, exists := r.URL.Query()["after"]
	if !exists {
		return 0, nil
	}
	if len(values) != 1 || values[0] == "" {
		return 0, postgres.ErrNegativeEventCursor
	}

	after, err := strconv.ParseInt(values[0], 10, 64)
	if err != nil || after < 0 {
		return 0, postgres.ErrNegativeEventCursor
	}
	return after, nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
