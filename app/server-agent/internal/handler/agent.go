package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/registry"
	"github.com/autosdk/ppp/server-agent/internal/usecase"
)

// JSON-RPC error codes used by the server.
const (
	ErrInvalidParams  = -32602
	ErrInternalError  = -32603
	ErrSessionUnknown = -32001
)

// Conn is the minimal interface the handler needs to respond to an agent.
// It is a superset of registry.Sender so values can be passed to reg.Add directly.
type Conn interface {
	SendRequest(id, method string, params any) error
	SendSuccess(id string, result any) error
	SendError(id string, code int, message string) error
	Close() error
	// SetSession associates this connection with a session after registration.
	SetSession(id domain.SessionID)
	// SetDevice associates this connection with a device ID for response correlation.
	SetDevice(id domain.DeviceID)
}

// AgentHandler is a thin JSON-RPC dispatcher that delegates to AgentLifecycleUseCase.
// It owns only parameter parsing and response shaping — no business logic.
type AgentHandler struct {
	uc  *usecase.AgentLifecycleUseCase
	log *slog.Logger
}

func NewAgentHandler(uc *usecase.AgentLifecycleUseCase, log *slog.Logger) *AgentHandler {
	return &AgentHandler{uc: uc, log: log}
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

	resp, err := h.uc.Hello(ctx, usecase.HelloRequest{
		DeviceID:        domain.DeviceID(p.DeviceID),
		AgentInstanceID: p.AgentInstanceID,
		Capabilities:    p.Capabilities,
	}, connAsSender(conn))
	if err != nil {
		h.log.Error("hello failed", "err", err)
		_ = conn.SendError(id, ErrInternalError, err.Error())
		return
	}

	conn.SetSession(resp.SessionID)
	conn.SetDevice(domain.DeviceID(p.DeviceID))
	_ = conn.SendSuccess(id, map[string]any{
		"accepted":  true,
		"sessionId": string(resp.SessionID),
	})
}

func (h *AgentHandler) HandleResume(ctx context.Context, id string, rawParams json.RawMessage, conn Conn) {
	var p resumeParams
	if err := json.Unmarshal(rawParams, &p); err != nil || p.DeviceID == "" || p.SessionID == "" {
		_ = conn.SendError(id, ErrInvalidParams, "invalid params: deviceId and sessionId required")
		return
	}

	resp, err := h.uc.Resume(ctx, usecase.ResumeRequest{
		DeviceID:     domain.DeviceID(p.DeviceID),
		SessionID:    domain.SessionID(p.SessionID),
		Capabilities: p.Capabilities,
	}, connAsSender(conn))
	if err != nil {
		_ = conn.SendError(id, ErrSessionUnknown, fmt.Sprintf("session unknown: %s", p.SessionID))
		return
	}

	conn.SetSession(resp.SessionID)
	conn.SetDevice(domain.DeviceID(p.DeviceID))
	_ = conn.SendSuccess(id, map[string]any{
		"accepted":  true,
		"sessionId": string(resp.SessionID),
	})
}

func (h *AgentHandler) HandleHeartbeat(ctx context.Context, id string, rawParams json.RawMessage, conn Conn) {
	var p heartbeatParams
	if err := json.Unmarshal(rawParams, &p); err != nil {
		_ = conn.SendError(id, ErrInvalidParams, "invalid params")
		return
	}
	h.uc.Heartbeat(ctx, domain.DeviceID(p.DeviceID), domain.SessionID(p.SessionID))
	_ = conn.SendSuccess(id, map[string]any{"accepted": true})
}

func (h *AgentHandler) HandleDisconnect(ctx context.Context, id string, rawParams json.RawMessage, conn Conn) {
	var p disconnectParams
	if err := json.Unmarshal(rawParams, &p); err != nil || p.SessionID == "" {
		_ = conn.SendError(id, ErrInvalidParams, "invalid params: sessionId required")
		return
	}
	h.uc.Disconnect(ctx, domain.DeviceID(p.DeviceID), domain.SessionID(p.SessionID))
	_ = conn.SendSuccess(id, map[string]any{"accepted": true})
}

// connAsSender adapts handler.Conn to registry.Sender (subset of the interface).
// Both are satisfied by *ws.Conn so this is always safe.
func connAsSender(conn Conn) registry.Sender {
	return conn.(registry.Sender)
}
