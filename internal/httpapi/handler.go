package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/mdombrov-33/relay/internal/event"
	"github.com/mdombrov-33/relay/internal/postgres"
	"github.com/mdombrov-33/relay/internal/run"
)

const maxCommandBodyBytes = 8 << 10

type store interface {
	CancelRun(ctx context.Context, runID run.ID, canceled event.Envelope) error
	FindRun(ctx context.Context, runID run.ID) (postgres.RunRecord, error)
	FindApprovalRequest(ctx context.Context, requestID string) (postgres.ApprovalRequestRecord, error)
	ListRunEvents(ctx context.Context, runID run.ID, afterSequence int64) ([]event.Stored, error)
	ResolveApproval(ctx context.Context, signal postgres.ApprovalSignal, resolved event.Envelope) (bool, error)
}

type Handler struct {
	store store
	mux   *http.ServeMux
	now   func() time.Time
	newID func(string) (string, error)
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

type approvalCommand struct {
	RequestID string                    `json:"requestId"`
	Decision  postgres.ApprovalDecision `json:"decision"`
}

type approvalCommandResponse struct {
	RequestID string                    `json:"requestId"`
	Decision  postgres.ApprovalDecision `json:"decision"`
}

type cancellationResponse struct {
	ID     run.ID     `json:"id"`
	Status run.Status `json:"status"`
}

func NewHandler(store store) http.Handler {
	return newHandler(store, func() time.Time { return time.Now().UTC() }, randomID)
}

func newHandler(store store, now func() time.Time, newID func(string) (string, error)) http.Handler {
	h := &Handler{store: store, mux: http.NewServeMux(), now: now, newID: newID}
	h.mux.HandleFunc("GET /v1/runs/{id}", h.getRun)
	h.mux.HandleFunc("POST /v1/runs/{id}/cancel", h.cancelRun)
	h.mux.HandleFunc("GET /v1/runs/{id}/events", h.getRunEvents)
	h.mux.HandleFunc("POST /v1/runs/{id}/signals/approval", h.resolveApproval)
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

func (h *Handler) resolveApproval(w http.ResponseWriter, r *http.Request) {
	var command approvalCommand
	if err := decodeCommand(w, r, &command); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid approval command"})
		return
	}
	if command.RequestID == "" || command.Decision != postgres.ApprovalDecisionApproved && command.Decision != postgres.ApprovalDecisionRejected {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "requestId and an approved or rejected decision are required"})
		return
	}

	runID := run.ID(r.PathValue("id"))
	request, err := h.store.FindApprovalRequest(r.Context(), command.RequestID)
	if err != nil {
		h.writeApprovalError(w, err)
		return
	}
	if request.RunID != runID {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "approval request not found"})
		return
	}

	signalID, err := h.newID("signal")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
		return
	}
	eventID, err := h.newID("event")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
		return
	}

	signal := postgres.ApprovalSignal{
		ID:        signalID,
		RequestID: command.RequestID,
		RunID:     runID,
		Decision:  command.Decision,
	}
	resolved, err := event.New(
		eventID,
		runID,
		request.StepKey,
		event.TypeApprovalResolved,
		h.now(),
		event.ApprovalPayload{
			RequestID: command.RequestID,
			Approved:  command.Decision == postgres.ApprovalDecisionApproved,
		},
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
		return
	}

	if _, err := h.store.ResolveApproval(r.Context(), signal, resolved); err != nil {
		h.writeApprovalError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, approvalCommandResponse(command))
}

func (h *Handler) cancelRun(w http.ResponseWriter, r *http.Request) {
	runID := run.ID(r.PathValue("id"))
	eventID, err := h.newID("event")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
		return
	}
	canceled, err := event.New(
		eventID,
		runID,
		run.StepKey("workflow"),
		event.TypeWorkflowCancelled,
		h.now(),
		event.LifecyclePayload{Status: run.StatusCanceled},
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
		return
	}

	if err := h.store.CancelRun(r.Context(), runID, canceled); err != nil {
		switch {
		case errors.Is(err, postgres.ErrRunNotFound):
			writeJSON(w, http.StatusNotFound, errorResponse{Error: "run not found"})
		case errors.Is(err, postgres.ErrRunAlreadyTerminal):
			writeJSON(w, http.StatusConflict, errorResponse{Error: "run is already terminal"})
		default:
			writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
		}
		return
	}

	writeJSON(w, http.StatusOK, cancellationResponse{ID: runID, Status: run.StatusCanceled})
}

func (h *Handler) writeApprovalError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, postgres.ErrApprovalRequestNotFound), errors.Is(err, postgres.ErrApprovalSignalRunIDMismatch):
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "approval request not found"})
	case errors.Is(err, postgres.ErrApprovalDecisionConflict):
		writeJSON(w, http.StatusConflict, errorResponse{Error: "approval decision conflicts with the recorded decision"})
	default:
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
	}
}

func decodeCommand(w http.ResponseWriter, r *http.Request, value any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxCommandBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON value")
	}
	return nil
}

func randomID(prefix string) (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate %s id: %w", prefix, err)
	}
	return prefix + "-" + hex.EncodeToString(bytes), nil
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
