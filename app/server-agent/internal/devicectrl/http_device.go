package devicectrl

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/registry"
	"github.com/autosdk/ppp/server-agent/internal/store"
	"github.com/autosdk/ppp/server-agent/internal/tools/llm"
)

type deviceBindingController interface {
	ListBindings(ctx context.Context) ([]*domain.DeviceBinding, error)
	GetBinding(ctx context.Context, deviceID domain.DeviceID) (*domain.DeviceBinding, bool, error)
	ClearBinding(ctx context.Context, deviceID domain.DeviceID) error
	Remediate(ctx context.Context, deviceID domain.DeviceID) (*domain.DeviceBinding, error)
}

// deviceCommandDispatcher dispatches a device.* command and returns a channel
// that receives exactly one CommandResult then is closed.
type deviceCommandDispatcher interface {
	Dispatch(ctx context.Context, cmd domain.Command) (<-chan domain.CommandResult, error)
}

// DeviceHandler serves the connected-device API.
//
//	GET  /devices                     — list all currently connected devices
//	GET  /devices/{id}                — get a single device by deviceId
//	POST /devices/{id}/execute        — dispatch a device.execute command directly
//	POST /devices/{id}/observe        — get current UI snapshot (no task required)
//	POST /devices/{id}/script         — run a RhinoJS snippet directly (no task/workflow required)
//	POST /devices/{id}/record/start   — begin recording execute actions
//	POST /devices/{id}/record/stop    — stop recording and return generated JS script + workflow YAML
//	GET  /devices/{id}/record/status  — check whether a recording is active
type DeviceHandler struct {
	reg        registry.AgentRegistry
	bindings   deviceBindingController
	disp       deviceCommandDispatcher
	recordings *RecordingStore
	library    store.MacroStore
	agentLoop  llm.AgentLoop
	log        *slog.Logger
}

func NewDeviceHandler(reg registry.AgentRegistry, log *slog.Logger, bindings ...deviceBindingController) *DeviceHandler {
	var controller deviceBindingController
	if len(bindings) > 0 {
		controller = bindings[0]
	}
	return &DeviceHandler{reg: reg, bindings: controller, log: log}
}

// WithDispatcher attaches a command dispatcher so that POST /devices/{id}/execute is served.
func (h *DeviceHandler) WithDispatcher(disp deviceCommandDispatcher) *DeviceHandler {
	h.disp = disp
	return h
}

// WithRecording attaches a RecordingStore so that record/* endpoints are served.
func (h *DeviceHandler) WithRecording(store *RecordingStore) *DeviceHandler {
	h.recordings = store
	return h
}

// WithRecordingLibrary attaches a MacroLibrary so that completed recordings are persisted.
func (h *DeviceHandler) WithRecordingLibrary(lib store.MacroStore) *DeviceHandler {
	h.library = lib
	return h
}

// WithAgentLoop attaches an LLM AgentLoop so that POST /devices/{id}/record/llm-run is served.
// Pass nil to disable (handler returns 503).
func (h *DeviceHandler) WithAgentLoop(loop llm.AgentLoop) *DeviceHandler {
	h.agentLoop = loop
	return h
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
	case len(parts) == 2 && parts[1] == "execute":
		h.handleExecute(w, r, domain.DeviceID(parts[0]))
	case len(parts) == 2 && parts[1] == "observe":
		h.handleObserve(w, r, domain.DeviceID(parts[0]))
	case len(parts) == 2 && parts[1] == "script":
		h.handleScript(w, r, domain.DeviceID(parts[0]))
	case len(parts) == 2 && parts[1] == "record/start":
		h.handleRecordStart(w, r, domain.DeviceID(parts[0]))
	case len(parts) == 2 && parts[1] == "record/stop":
		h.handleRecordStop(w, r, domain.DeviceID(parts[0]))
	case len(parts) == 2 && parts[1] == "record/status":
		h.handleRecordStatus(w, r, domain.DeviceID(parts[0]))
	case len(parts) == 2 && parts[1] == "record/llm-run":
		h.handleLLMRun(w, r, domain.DeviceID(parts[0]))
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

// handleExecute dispatches a device.execute command directly to the connected agent
// and streams back the raw result.  Body must be valid JSON matching the
// device.execute params schema: {"action": {"kind": "...", ...}}.
func (h *DeviceHandler) handleExecute(w http.ResponseWriter, r *http.Request, id domain.DeviceID) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.disp == nil {
		http.Error(w, "execute not configured", http.StatusNotImplemented)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	cmd := domain.Command{
		ID:       domain.NewCommandID(),
		Kind:     domain.CommandKindExecute,
		DeviceID: id,
		Params:   json.RawMessage(body),
		IssuedAt: time.Now(),
	}
	ch, err := h.disp.Dispatch(r.Context(), cmd)
	if err != nil {
		http.Error(w, "dispatch: "+err.Error(), http.StatusBadGateway)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	select {
	case result := <-ch:
		w.Header().Set("Content-Type", "application/json")
		if !result.Success {
			w.WriteHeader(http.StatusBadGateway)
			msg := "command failed"
			if result.Err != nil {
				msg = result.Err.Message
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
			return
		}
		// Recording intercept: append to active session without affecting the main path.
		if h.recordings != nil {
			if rec, ok := h.recordings.Get(id); ok {
				rec.Append(append(json.RawMessage(nil), body...), result.Raw)
			}
		}
		_, _ = w.Write(result.Raw)
	case <-ctx.Done():
		http.Error(w, "command timeout", http.StatusGatewayTimeout)
	}
}

// handleObserve dispatches a device.observe command directly to the connected agent
// and returns the current UI snapshot. No task or workflow required.
func (h *DeviceHandler) handleObserve(w http.ResponseWriter, r *http.Request, id domain.DeviceID) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.disp == nil {
		http.Error(w, "observe not configured", http.StatusNotImplemented)
		return
	}
	cmd := domain.Command{
		ID:       domain.NewCommandID(),
		Kind:     domain.CommandKindObserve,
		DeviceID: id,
		Params:   json.RawMessage("{}"),
		IssuedAt: time.Now(),
	}
	ch, err := h.disp.Dispatch(r.Context(), cmd)
	if err != nil {
		http.Error(w, "dispatch: "+err.Error(), http.StatusBadGateway)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	select {
	case result := <-ch:
		w.Header().Set("Content-Type", "application/json")
		if !result.Success {
			w.WriteHeader(http.StatusBadGateway)
			msg := "command failed"
			if result.Err != nil {
				msg = result.Err.Message
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
			return
		}
		_, _ = w.Write(result.Raw)
	case <-ctx.Done():
		http.Error(w, "command timeout", http.StatusGatewayTimeout)
	}
}

// handleScript dispatches a device.script command directly to the connected agent
// and returns the script output, logs, and duration. No task or workflow required.
//
// Request body: {"source": "...", "params": {"k": "v"}, "timeout": 30000}
// Response:     {"output": {...}, "logs": [...], "durationMs": N}
func (h *DeviceHandler) handleScript(w http.ResponseWriter, r *http.Request, id domain.DeviceID) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.disp == nil {
		http.Error(w, "script not configured", http.StatusNotImplemented)
		return
	}
	var body struct {
		Source  string            `json:"source"`
		Params  map[string]string `json:"params"`
		Timeout int64             `json:"timeout"` // ms; default 30000
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil || body.Source == "" {
		http.Error(w, "source is required", http.StatusBadRequest)
		return
	}
	if body.Timeout <= 0 {
		body.Timeout = 30_000
	}
	if body.Params == nil {
		body.Params = map[string]string{}
	}
	raw, err := json.Marshal(struct {
		Script  string            `json:"script"`
		Params  map[string]string `json:"params"`
		Timeout int64             `json:"timeout"`
	}{Script: body.Source, Params: body.Params, Timeout: body.Timeout})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	cmd := domain.Command{
		ID:       domain.NewCommandID(),
		Kind:     domain.CommandKindScript,
		DeviceID: id,
		Params:   json.RawMessage(raw),
		IssuedAt: time.Now(),
	}
	ch, err := h.disp.Dispatch(r.Context(), cmd)
	if err != nil {
		http.Error(w, "dispatch: "+err.Error(), http.StatusBadGateway)
		return
	}
	// Allow up to timeout + 5 s buffer so the agent can finish cleanly.
	deadline := time.Duration(body.Timeout)*time.Millisecond + 5*time.Second
	ctx, cancel := context.WithTimeout(r.Context(), deadline)
	defer cancel()
	select {
	case result := <-ch:
		w.Header().Set("Content-Type", "application/json")
		if !result.Success {
			w.WriteHeader(http.StatusBadGateway)
			msg := "script failed"
			if result.Err != nil {
				msg = result.Err.Message
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
			return
		}
		_, _ = w.Write(result.Raw)
	case <-ctx.Done():
		http.Error(w, "script timeout", http.StatusGatewayTimeout)
	}
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
