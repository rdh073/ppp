package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/registry"
)

type deviceBindingController interface {
	ListBindings(ctx context.Context) ([]*domain.DeviceBinding, error)
	GetBinding(ctx context.Context, deviceID domain.DeviceID) (*domain.DeviceBinding, bool, error)
	ClearBinding(ctx context.Context, deviceID domain.DeviceID) error
	Remediate(ctx context.Context, deviceID domain.DeviceID) (*domain.DeviceBinding, error)
}

// DeviceHandler serves the connected-device API.
//
//	GET /devices      — list all currently connected devices
//	GET /devices/{id} — get a single device by deviceId
type DeviceHandler struct {
	reg      registry.AgentRegistry
	bindings deviceBindingController
	log      *slog.Logger
}

func NewDeviceHandler(reg registry.AgentRegistry, log *slog.Logger, bindings ...deviceBindingController) *DeviceHandler {
	var controller deviceBindingController
	if len(bindings) > 0 {
		controller = bindings[0]
	}
	return &DeviceHandler{reg: reg, bindings: controller, log: log}
}

func (h *DeviceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/devices")
	id = strings.TrimPrefix(id, "/")

	parts := strings.SplitN(id, "/", 2)
	switch {
	case len(parts) == 1 && parts[0] == "":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.list(w)
	case len(parts) == 1:
		switch r.Method {
		case http.MethodGet:
			h.get(w, r, domain.DeviceID(parts[0]))
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	case len(parts) == 2 && parts[1] == "adb-ws":
		h.adbWS(w, r, domain.DeviceID(parts[0]))
	default:
		h.handleBindingActions(w, r, domain.DeviceID(parts[0]), parts[1])
	}
}

type deviceView struct {
	DeviceID          string                                `json:"deviceId"`
	AndroidIdentity   string                                `json:"androidIdentity,omitempty"`
	Connected         bool                                  `json:"connected"`
	SessionID         string                                `json:"sessionId,omitempty"`
	AgentInstanceID   string                                `json:"agentInstanceId,omitempty"`
	Capabilities      []domain.Capability                   `json:"capabilities"`
	DeviceMetadata    domain.AgentDeviceMetadata            `json:"deviceMetadata,omitempty"`
	ConnectedAt       time.Time                             `json:"connectedAt"`
	LastHeartbeatAt   time.Time                             `json:"lastHeartbeatAt"`
	ADBSerial         string                                `json:"adbSerial,omitempty"`
	ObservedAndroidID string                                `json:"observedAndroidId,omitempty"`
	IdentityStatus    domain.DeviceIdentityStatus           `json:"identityStatus"`
	RemediationStatus domain.AccessibilityRemediationStatus `json:"remediationStatus"`
	PendingEnable     bool                                  `json:"pendingEnable"`
	ServiceComponent  string                                `json:"serviceComponent,omitempty"`
	LastSeenAt        time.Time                             `json:"lastSeenAt"`
	LastVerifiedAt    time.Time                             `json:"lastVerifiedAt"`
	LastFailureAt     time.Time                             `json:"lastFailureAt"`
	LastError         string                                `json:"lastError,omitempty"`
	SerialSource      string                                `json:"serialSource,omitempty"`
}

func sessionToView(s *domain.Session, binding *domain.DeviceBinding) deviceView {
	view := deviceView{
		Capabilities:      []domain.Capability{},
		IdentityStatus:    domain.DeviceIdentityStatusUnknown,
		RemediationStatus: domain.AccessibilityRemediationStatusIdle,
	}
	if s != nil {
		caps := s.Capabilities
		if caps == nil {
			caps = []domain.Capability{}
		}
		view.DeviceID = string(s.DeviceID)
		view.Connected = true
		view.SessionID = string(s.ID)
		view.AgentInstanceID = s.AgentInstanceID
		view.Capabilities = caps
		view.DeviceMetadata = s.DeviceMetadata
		view.ConnectedAt = s.ConnectedAt
		view.LastHeartbeatAt = s.LastHeartbeatAt
		view.AndroidIdentity = androidIdentityFromMetadata(s.DeviceMetadata)
	}
	if binding != nil {
		if view.DeviceID == "" {
			view.DeviceID = string(binding.DeviceID)
		}
		view.ADBSerial = binding.ADBSerial
		view.ObservedAndroidID = binding.ObservedAndroidID
		view.IdentityStatus = binding.IdentityStatus
		view.RemediationStatus = binding.RemediationStatus
		view.PendingEnable = binding.PendingEnable
		view.ServiceComponent = binding.ServiceComponent
		view.LastSeenAt = binding.LastSeenAt
		view.LastVerifiedAt = binding.LastVerifiedAt
		view.LastFailureAt = binding.LastFailureAt
		view.LastError = binding.LastError
		view.SerialSource = binding.SerialSource
	}
	if view.AndroidIdentity == "" {
		switch {
		case view.ObservedAndroidID != "":
			view.AndroidIdentity = view.ObservedAndroidID
		case view.ADBSerial != "":
			view.AndroidIdentity = view.ADBSerial
		case view.DeviceID != "":
			view.AndroidIdentity = view.DeviceID
		}
	}
	return view
}

func androidIdentityFromMetadata(metadata domain.AgentDeviceMetadata) string {
	switch {
	case metadata.Manufacturer != "" && metadata.Model != "":
		return strings.TrimSpace(metadata.Manufacturer + " " + metadata.Model)
	case metadata.Model != "":
		return metadata.Model
	case metadata.Device != "":
		return metadata.Device
	default:
		return ""
	}
}

func (h *DeviceHandler) list(w http.ResponseWriter) {
	sessions := h.reg.ListAll()
	byDevice := make(map[domain.DeviceID]deviceView, len(sessions))
	for _, s := range sessions {
		byDevice[s.DeviceID] = sessionToView(s, nil)
	}
	if h.bindings != nil {
		bindings, err := h.bindings.ListBindings(context.Background())
		if err != nil && h.log != nil {
			h.log.Warn("list device bindings failed", "err", err)
		}
		for _, binding := range bindings {
			if binding == nil {
				continue
			}
			current := byDevice[binding.DeviceID]
			var session *domain.Session
			if current.DeviceID != "" && current.Connected {
				if s, _, ok := h.reg.GetByDevice(binding.DeviceID); ok {
					session = s
				}
			}
			byDevice[binding.DeviceID] = sessionToView(session, binding)
		}
	}
	ids := make([]string, 0, len(byDevice))
	for deviceID := range byDevice {
		ids = append(ids, string(deviceID))
	}
	sort.Strings(ids)
	views := make([]deviceView, 0, len(ids))
	for _, id := range ids {
		views = append(views, byDevice[domain.DeviceID(id)])
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(views)
}

func (h *DeviceHandler) get(w http.ResponseWriter, r *http.Request, id domain.DeviceID) {
	s, _, ok := h.reg.GetByDevice(id)
	var binding *domain.DeviceBinding
	if h.bindings != nil {
		got, exists, err := h.bindings.GetBinding(r.Context(), id)
		if err != nil {
			http.Error(w, "failed to load device binding", http.StatusInternalServerError)
			return
		}
		if exists {
			binding = got
		}
	}
	if !ok && binding == nil {
		http.Error(w, "device not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sessionToView(s, binding))
}

func (h *DeviceHandler) handleBindingActions(w http.ResponseWriter, r *http.Request, id domain.DeviceID, action string) {
	if h.bindings == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	switch action {
	case "binding/clear":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := h.bindings.ClearBinding(r.Context(), id); err != nil {
			http.Error(w, "failed to clear device binding", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case "accessibility/remediate":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		binding, err := h.bindings.Remediate(r.Context(), id)
		if err != nil {
			http.Error(w, "failed to remediate accessibility", http.StatusInternalServerError)
			return
		}
		var session *domain.Session
		if s, _, ok := h.reg.GetByDevice(id); ok {
			session = s
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sessionToView(session, binding))
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}
