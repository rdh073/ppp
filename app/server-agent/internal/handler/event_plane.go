package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
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
	uc  usecase.EventPlaneControl
	log *slog.Logger
}

func NewEventPlaneHandler(uc usecase.EventPlaneControl, log *slog.Logger) *EventPlaneHandler {
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
	query, err := parseAcceptedEventListQuery(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	includePayload, err := parseBoolQueryParam(r, "includePayload", true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	records, err := h.uc.ListAccepted(r.Context(), query)
	if err != nil {
		writeUseCaseError(w, err)
		return
	}
	if !includePayload {
		for i := range records.Items {
			records.Items[i].Event.Payload = nil
		}
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
	query, err := parseDeadLetterListQuery(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	records, err := h.uc.ListDeadLetters(r.Context(), query)
	if err != nil {
		writeUseCaseError(w, err)
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
	case errors.Is(err, usecase.ErrInvalidEventListQuery):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case strings.Contains(err.Error(), "not replayable"), strings.Contains(err.Error(), "has no event kind"):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func parseAcceptedEventListQuery(r *http.Request) (usecase.AcceptedEventListQuery, error) {
	limit, offset, err := parseLimitOffset(r)
	if err != nil {
		return usecase.AcceptedEventListQuery{}, err
	}
	from, to, err := parseTimeRange(r)
	if err != nil {
		return usecase.AcceptedEventListQuery{}, err
	}
	return usecase.AcceptedEventListQuery{
		DeviceID: domain.DeviceID(strings.TrimSpace(r.URL.Query().Get("deviceId"))),
		Kind:     domain.EventKind(strings.TrimSpace(r.URL.Query().Get("kind"))),
		Source:   strings.TrimSpace(r.URL.Query().Get("source")),
		From:     from,
		To:       to,
		Cursor:   strings.TrimSpace(r.URL.Query().Get("cursor")),
		Order:    usecase.EventListOrder(strings.TrimSpace(r.URL.Query().Get("order"))),
		Limit:    limit,
		Offset:   offset,
	}, nil
}

func parseDeadLetterListQuery(r *http.Request) (usecase.DeadLetterListQuery, error) {
	limit, offset, err := parseLimitOffset(r)
	if err != nil {
		return usecase.DeadLetterListQuery{}, err
	}
	from, to, err := parseTimeRange(r)
	if err != nil {
		return usecase.DeadLetterListQuery{}, err
	}
	return usecase.DeadLetterListQuery{
		DeviceID: domain.DeviceID(strings.TrimSpace(r.URL.Query().Get("deviceId"))),
		Kind:     domain.EventKind(strings.TrimSpace(r.URL.Query().Get("kind"))),
		Source:   strings.TrimSpace(r.URL.Query().Get("source")),
		EventID:  strings.TrimSpace(r.URL.Query().Get("eventId")),
		From:     from,
		To:       to,
		Cursor:   strings.TrimSpace(r.URL.Query().Get("cursor")),
		Order:    usecase.EventListOrder(strings.TrimSpace(r.URL.Query().Get("order"))),
		Limit:    limit,
		Offset:   offset,
	}, nil
}

func parseLimitOffset(r *http.Request) (int, int, error) {
	limit, err := parseIntQueryParam(r, "limit")
	if err != nil {
		return 0, 0, err
	}
	offset, err := parseIntQueryParam(r, "offset")
	if err != nil {
		return 0, 0, err
	}
	return limit, offset, nil
}

func parseIntQueryParam(r *http.Request, key string) (int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %w", key, err)
	}
	return value, nil
}

func parseTimeRange(r *http.Request) (time.Time, time.Time, error) {
	from, err := parseTimeQueryParam(r, "from")
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	to, err := parseTimeQueryParam(r, "to")
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return from, to, nil
}

func parseBoolQueryParam(r *http.Request, key string, defaultValue bool) (bool, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return defaultValue, nil
	}

	switch strings.ToLower(raw) {
	case "1", "true", "yes", "y", "on":
		return true, nil
	case "0", "false", "no", "n", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid %s: %q", key, raw)
	}
}

func parseTimeQueryParam(r *http.Request, key string) (time.Time, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return time.Time{}, nil
	}
	value, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid %s: %w", key, err)
	}
	return value.UTC(), nil
}
