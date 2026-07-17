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

const eventPollInterval = time.Second

type store interface {
	CancelRun(ctx context.Context, runID run.ID, canceled event.Envelope) error
	CreateRun(ctx context.Context, r run.Run, queued event.Envelope) error
	FindRun(ctx context.Context, runID run.ID) (postgres.RunRecord, error)
	FindApprovalRequest(ctx context.Context, requestID string) (postgres.ApprovalRequestRecord, error)
	ListEventsAfter(ctx context.Context, afterSequence int64) ([]event.Stored, error)
	ListRunEvents(ctx context.Context, runID run.ID, afterSequence int64) ([]event.Stored, error)
	ListRuns(ctx context.Context) ([]postgres.RunRecord, error)
	ResolveApproval(ctx context.Context, signal postgres.ApprovalSignal, resolved event.Envelope) (bool, error)
}

type Handler struct {
	store store
	mux   *http.ServeMux
	now   func() time.Time
	newID func(string) (string, error)
	wait  func(context.Context) error
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

type runsResponse struct {
	Runs []runResponse `json:"runs"`
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

type creationResponse struct {
	ID     run.ID     `json:"id"`
	Status run.Status `json:"status"`
}

func NewHandler(store store) http.Handler {
	return newHandler(store, func() time.Time { return time.Now().UTC() }, randomID)
}

func newHandler(store store, now func() time.Time, newID func(string) (string, error)) http.Handler {
	return newHandlerWithWait(store, now, newID, waitForEvents)
}

func newHandlerWithWait(store store, now func() time.Time, newID func(string) (string, error), wait func(context.Context) error) http.Handler {
	h := &Handler{store: store, mux: http.NewServeMux(), now: now, newID: newID, wait: wait}
	h.mux.HandleFunc("POST /v1/runs", h.createRun)
	h.mux.HandleFunc("GET /v1/runs", h.listRuns)
	h.mux.HandleFunc("GET /v1/events/stream", h.streamEvents)
	h.mux.HandleFunc("GET /v1/runs/{id}", h.getRun)
	h.mux.HandleFunc("POST /v1/runs/{id}/cancel", h.cancelRun)
	h.mux.HandleFunc("GET /v1/runs/{id}/events", h.getRunEvents)
	h.mux.HandleFunc("POST /v1/runs/{id}/signals/approval", h.resolveApproval)
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) createRun(w http.ResponseWriter, r *http.Request) {
	runIDValue, err := h.newID("run")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
		return
	}
	eventID, err := h.newID("event")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
		return
	}

	createdAt := h.now()
	createdRun := run.New(run.ID(runIDValue))
	queued, err := event.New(
		eventID,
		createdRun.ID,
		run.StepKey("workflow"),
		event.TypeWorkflowQueued,
		createdAt,
		event.LifecyclePayload{Status: createdRun.Status},
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
		return
	}

	if err := h.store.CreateRun(r.Context(), createdRun, queued); err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
		return
	}

	w.Header().Set("Location", "/v1/runs/"+runIDValue)
	writeJSON(w, http.StatusCreated, creationResponse{ID: createdRun.ID, Status: createdRun.Status})
}

func (h *Handler) streamEvents(w http.ResponseWriter, r *http.Request) {
	after, err := parseAfter(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "after must be one non-negative integer"})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "streaming is not supported"})
		return
	}

	stored, err := h.store.ListEventsAfter(r.Context(), after)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		if len(stored) == 0 {
			if err := h.wait(r.Context()); err != nil {
				return
			}
		} else {
			for _, item := range stored {
				if err := writeSSEEvent(w, item); err != nil {
					return
				}
				after = item.Sequence
			}
			flusher.Flush()
		}

		stored, err = h.store.ListEventsAfter(r.Context(), after)
		if err != nil {
			return
		}
	}
}

func (h *Handler) listRuns(w http.ResponseWriter, r *http.Request) {
	records, err := h.store.ListRuns(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
		return
	}

	response := runsResponse{Runs: make([]runResponse, 0, len(records))}
	for _, record := range records {
		response.Runs = append(response.Runs, responseRun(record))
	}

	writeJSON(w, http.StatusOK, response)
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

	writeJSON(w, http.StatusOK, responseRun(record))
}

func responseRun(record postgres.RunRecord) runResponse {
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
	return response
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
		response.Events = append(response.Events, responseEvent(item))
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

func waitForEvents(ctx context.Context) error {
	timer := time.NewTimer(eventPollInterval)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func writeSSEEvent(w io.Writer, stored event.Stored) error {
	data, err := json.Marshal(responseEvent(stored))
	if err != nil {
		return fmt.Errorf("encode SSE event: %w", err)
	}
	if _, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", stored.Sequence, data); err != nil {
		return fmt.Errorf("write SSE event: %w", err)
	}
	return nil
}

func responseEvent(stored event.Stored) eventResponse {
	return eventResponse{
		Sequence:   stored.Sequence,
		ID:         stored.ID(),
		RunID:      stored.RunID(),
		StepKey:    stored.StepKey(),
		Type:       stored.Type(),
		OccurredAt: stored.OccurredAt(),
		Payload:    stored.Payload(),
	}
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
