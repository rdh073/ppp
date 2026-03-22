package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/projection"
)

// ProjectionStreamHandler exposes a server-sent-event stream of read-model updates.
//
//	GET /events/stream?topics=account-manager.account-creations,campaigns.posts
type ProjectionStreamHandler struct {
	hub *projection.Hub
}

func NewProjectionStreamHandler(hub *projection.Hub) *ProjectionStreamHandler {
	return &ProjectionStreamHandler{hub: hub}
}

func (h *ProjectionStreamHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	topics := parseTopics(r.URL.Query()["topics"])
	lastEventID := strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	ch, unsubscribe, err := h.hub.Subscribe(topics, lastEventID)
	if err != nil {
		if errors.Is(err, projection.ErrBackfillUnavailable) {
			writeProjectionStreamHeaders(w)
			_, _ = w.Write([]byte("event: reset\n"))
			_, _ = w.Write([]byte("data: {\"reason\":\"projection history unavailable\"}\n\n"))
			flusher.Flush()
			return
		}
		if errors.Is(err, projection.ErrInvalidCursor) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, "subscribe projection stream: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer unsubscribe()

	writeProjectionStreamHeaders(w)

	_, _ = w.Write([]byte(": connected\n\n"))
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = w.Write([]byte(": heartbeat\n\n"))
			flusher.Flush()
		case event, ok := <-ch:
			if !ok {
				return
			}
			payload, err := json.Marshal(event)
			if err != nil {
				continue
			}
			if event.ID != "" {
				_, _ = w.Write([]byte("id: "))
				_, _ = w.Write([]byte(event.ID))
				_, _ = w.Write([]byte("\n"))
			}
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(payload)
			_, _ = w.Write([]byte("\n\n"))
			flusher.Flush()
		}
	}
}

func writeProjectionStreamHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
}

func parseTopics(raw []string) []string {
	var topics []string
	for _, item := range raw {
		for _, part := range strings.Split(item, ",") {
			topic := strings.TrimSpace(part)
			if topic == "" {
				continue
			}
			topics = append(topics, topic)
		}
	}
	return topics
}
