package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/autosdk/ppp/server-agent/internal/store"
	"github.com/autosdk/ppp/server-agent/internal/usecase"
)

// EventPlaneHandler exposes inspection and replay for accepted events and dead letters.
//
//	GET  /events/accepted
//	GET  /events/accepted/{eventId}
//	POST /events/accepted/{eventId}/replay
//	GET  /events/deadletters
//	GET  /events/deadletters/{deadLetterId}
//	POST /events/deadletters/{deadLetterId}/replay
type EventPlaneHandler struct {
	uc  *usecase.EventPlaneControlUseCase
	log *slog.Logger
}

func NewEventPlaneHandler(uc *usecase.EventPlaneControlUseCase, log *slog.Logger) *EventPlaneHandler {
	return &EventPlaneHandler{uc: uc, log: log}
}

func (h *EventPlaneHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/events")
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	parts := strings.Split(path, "/")
	switch {
	case r.Method == http.MethodGet && len(parts) == 1 && parts[0] == "accepted":
		h.listAccepted(w, r)
	case r.Method == http.MethodGet && len(parts) == 2 && parts[0] == "accepted":
		h.getAccepted(w, r, parts[1])
	case r.Method == http.MethodPost && len(parts) == 3 && parts[0] == "accepted" && parts[2] == "replay":
		h.replayAccepted(w, r, parts[1])
	case r.Method == http.MethodGet && len(parts) == 1 && parts[0] == "deadletters":
		h.listDeadLetters(w, r)
	case r.Method == http.MethodGet && len(parts) == 2 && parts[0] == "deadletters":
		h.getDeadLetter(w, r, parts[1])
	case r.Method == http.MethodPost && len(parts) == 3 && parts[0] == "deadletters" && parts[2] == "replay":
		h.replayDeadLetter(w, r, parts[1])
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (h *EventPlaneHandler) listAccepted(w http.ResponseWriter, r *http.Request) {
	records, err := h.uc.ListAccepted(r.Context())
	if err != nil {
		h.log.Error("list accepted events failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, records)
}

func (h *EventPlaneHandler) getAccepted(w http.ResponseWriter, r *http.Request, eventID string) {
	record, err := h.uc.GetAccepted(r.Context(), eventID)
	if err != nil {
		writeUseCaseError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (h *EventPlaneHandler) replayAccepted(w http.ResponseWriter, r *http.Request, eventID string) {
	if err := h.uc.ReplayAccepted(r.Context(), eventID); err != nil {
		writeUseCaseError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":  "replayed",
		"eventId": eventID,
	})
}

func (h *EventPlaneHandler) listDeadLetters(w http.ResponseWriter, r *http.Request) {
	records, err := h.uc.ListDeadLetters(r.Context())
	if err != nil {
		h.log.Error("list dead letters failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, records)
}

func (h *EventPlaneHandler) getDeadLetter(w http.ResponseWriter, r *http.Request, deadLetterID string) {
	record, err := h.uc.GetDeadLetter(r.Context(), deadLetterID)
	if err != nil {
		writeUseCaseError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (h *EventPlaneHandler) replayDeadLetter(w http.ResponseWriter, r *http.Request, deadLetterID string) {
	if err := h.uc.ReplayDeadLetter(r.Context(), deadLetterID); err != nil {
		writeUseCaseError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":       "replayed",
		"deadLetterId": deadLetterID,
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeUseCaseError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case strings.Contains(err.Error(), "not replayable"), strings.Contains(err.Error(), "has no event kind"):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
