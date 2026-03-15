package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/registry"
)

// DeviceHandler serves the connected-device API.
//
//	GET /devices      — list all currently connected devices
//	GET /devices/{id} — get a single device by deviceId
type DeviceHandler struct {
	reg registry.AgentRegistry
	log *slog.Logger
}

func NewDeviceHandler(reg registry.AgentRegistry, log *slog.Logger) *DeviceHandler {
	return &DeviceHandler{reg: reg, log: log}
}

func (h *DeviceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/devices")
	id = strings.TrimPrefix(id, "/")

	if id == "" {
		h.list(w)
	} else {
		h.get(w, domain.DeviceID(id))
	}
}

type deviceView struct {
	DeviceID        string           `json:"deviceId"`
	SessionID       string           `json:"sessionId"`
	AgentInstanceID string           `json:"agentInstanceId,omitempty"`
	Capabilities    []domain.Capability `json:"capabilities"`
	ConnectedAt     time.Time        `json:"connectedAt"`
	LastHeartbeatAt time.Time        `json:"lastHeartbeatAt"`
}

func sessionToView(s *domain.Session) deviceView {
	caps := s.Capabilities
	if caps == nil {
		caps = []domain.Capability{}
	}
	return deviceView{
		DeviceID:        string(s.DeviceID),
		SessionID:       string(s.ID),
		AgentInstanceID: s.AgentInstanceID,
		Capabilities:    caps,
		ConnectedAt:     s.ConnectedAt,
		LastHeartbeatAt: s.LastHeartbeatAt,
	}
}

func (h *DeviceHandler) list(w http.ResponseWriter) {
	sessions := h.reg.ListAll()
	views := make([]deviceView, len(sessions))
	for i, s := range sessions {
		views[i] = sessionToView(s)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(views)
}

func (h *DeviceHandler) get(w http.ResponseWriter, id domain.DeviceID) {
	s, _, ok := h.reg.GetByDevice(id)
	if !ok {
		http.Error(w, "device not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sessionToView(s))
}
