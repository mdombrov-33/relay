package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/mdombrov-33/relay/internal/postgres"
	"github.com/mdombrov-33/relay/internal/run"
)

type runReader interface {
	FindRun(ctx context.Context, runID run.ID) (postgres.RunRecord, error)
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

func NewHandler(store runReader) http.Handler {
	h := &Handler{store: store, mux: http.NewServeMux()}
	h.mux.HandleFunc("GET /v1/runs/{id}", h.getRun)
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

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
