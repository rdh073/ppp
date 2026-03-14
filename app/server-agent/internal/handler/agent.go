package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/registry"
)

// JSON-RPC error codes used by the server.
const (
	ErrInvalidParams  = -32602
	ErrInternalError  = -32603
	ErrSessionUnknown = -32001
)

// Conn is the minimal interface the handler needs to respond to an agent
// and register it in the session store.
// It is a superset of registry.Sender so values can be passed to reg.Add directly.
type Conn interface {
	SendRequest(id, method string, params any) error
	SendSuccess(id string, result any) error
	SendError(id string, code int, message string) error
	Close() error
	// SetSession associates this connection with a session after registration.
	// The transport layer uses the session ID for cleanup on disconnect.
	SetSession(id domain.SessionID)
}

// AgentHandler handles the agent lifecycle JSON-RPC methods:
// agent.hello, agent.resume, agent.heartbeat, agent.disconnect.
type AgentHandler struct {
	reg *registry.Registry
	log *slog.Logger
}

func NewAgentHandler(reg *registry.Registry, log *slog.Logger) *AgentHandler {
	return &AgentHandler{reg: reg, log: log}
}

// --- inbound param types ---

type helloParams struct {
	DeviceID        string              `json:"deviceId"`
	AgentInstanceID string              `json:"agentInstanceId"`
	Capabilities    []domain.Capability `json:"capabilities"`
}

type resumeParams struct {
	DeviceID     string              `json:"deviceId"`
	SessionID    string              `json:"sessionId"`
	Capabilities []domain.Capability `json:"capabilities"`
}

type heartbeatParams struct {
	DeviceID  string `json:"deviceId"`
	SessionID string `json:"sessionId"`
}

type disconnectParams struct {
	DeviceID  string `json:"deviceId"`
	SessionID string `json:"sessionId"`
}

// --- handlers ---

func (h *AgentHandler) HandleHello(ctx context.Context, id string, rawParams json.RawMessage, conn Conn) {
	var p helloParams
	if err := json.Unmarshal(rawParams, &p); err != nil || p.DeviceID == "" {
		_ = conn.SendError(id, ErrInvalidParams, "invalid params: deviceId required")
		return
	}

	// Replace any existing session for this device.
	if existing, _, ok := h.reg.GetByDevice(domain.DeviceID(p.DeviceID)); ok {
		h.reg.Remove(existing.ID)
		h.log.Info("replaced existing session", "deviceId", p.DeviceID, "old_session", existing.ID)
	}

	sessionID := domain.NewSessionID()
	now := time.Now()
	session := &domain.Session{
		ID:              sessionID,
		DeviceID:        domain.DeviceID(p.DeviceID),
		AgentInstanceID: p.AgentInstanceID,
		Capabilities:    p.Capabilities,
		ConnectedAt:     now,
		LastHeartbeatAt: now,
	}
	h.reg.Add(session, conn)
	conn.SetSession(sessionID)

	h.log.Info("agent.hello accepted", "deviceId", p.DeviceID, "sessionId", sessionID)
	_ = conn.SendSuccess(id, map[string]any{
		"accepted":  true,
		"sessionId": string(sessionID),
	})
}

func (h *AgentHandler) HandleResume(ctx context.Context, id string, rawParams json.RawMessage, conn Conn) {
	var p resumeParams
	if err := json.Unmarshal(rawParams, &p); err != nil || p.DeviceID == "" || p.SessionID == "" {
		_ = conn.SendError(id, ErrInvalidParams, "invalid params: deviceId and sessionId required")
		return
	}

	existing, _, ok := h.reg.GetBySession(domain.SessionID(p.SessionID))
	if !ok {
		// Agent must fall back to agent.hello.
		h.log.Info("agent.resume rejected: session unknown", "deviceId", p.DeviceID, "sessionId", p.SessionID)
		_ = conn.SendError(id, ErrSessionUnknown, fmt.Sprintf("session unknown: %s", p.SessionID))
		return
	}

	// Re-register with the new connection, preserving the session.
	h.reg.Remove(existing.ID)
	existing.Capabilities = p.Capabilities
	existing.LastHeartbeatAt = time.Now()
	h.reg.Add(existing, conn)
	conn.SetSession(existing.ID)

	h.log.Info("agent.resume accepted", "deviceId", p.DeviceID, "sessionId", p.SessionID)
	_ = conn.SendSuccess(id, map[string]any{
		"accepted":  true,
		"sessionId": p.SessionID,
	})
}

func (h *AgentHandler) HandleHeartbeat(ctx context.Context, id string, rawParams json.RawMessage, conn Conn) {
	var p heartbeatParams
	if err := json.Unmarshal(rawParams, &p); err != nil {
		_ = conn.SendError(id, ErrInvalidParams, "invalid params")
		return
	}
	if p.SessionID != "" {
		if session, _, ok := h.reg.GetBySession(domain.SessionID(p.SessionID)); ok {
			session.LastHeartbeatAt = time.Now()
		}
	}
	_ = conn.SendSuccess(id, map[string]any{"accepted": true})
}

func (h *AgentHandler) HandleDisconnect(ctx context.Context, id string, rawParams json.RawMessage, conn Conn) {
	var p disconnectParams
	if err := json.Unmarshal(rawParams, &p); err != nil || p.SessionID == "" {
		_ = conn.SendError(id, ErrInvalidParams, "invalid params: sessionId required")
		return
	}
	h.reg.Remove(domain.SessionID(p.SessionID))
	h.log.Info("agent.disconnect received", "deviceId", p.DeviceID, "sessionId", p.SessionID)
	_ = conn.SendSuccess(id, map[string]any{"accepted": true})
}
