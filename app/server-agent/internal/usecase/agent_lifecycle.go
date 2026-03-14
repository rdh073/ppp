package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/registry"
)

// EventProcessor is the minimal orchestrator interface needed by use cases.
// Extracted here so usecases don't import the orchestrator package directly,
// making them straightforward to test with a simple fake.
type EventProcessor interface {
	ProcessEvent(ctx context.Context, e domain.Event) error
}

// AgentLifecycleUseCase handles agent connection lifecycle.
// It manages sessions in the registry and notifies the orchestrator of
// agent online/offline events so in-progress workflows can be paused/resumed.
type AgentLifecycleUseCase struct {
	reg          registry.AgentRegistry
	orchestrator EventProcessor
	assigner     *DeviceAssigner // optional; nil-safe
	forgetDevice func(domain.DeviceID)
	log          *slog.Logger
}

func NewAgentLifecycle(
	reg registry.AgentRegistry,
	orch EventProcessor,
	log *slog.Logger,
) *AgentLifecycleUseCase {
	return &AgentLifecycleUseCase{reg: reg, orchestrator: orch, log: log}
}

// SetAssigner wires the DeviceAssigner so that newly connected devices are
// automatically assigned pending tasks from the queue.
func (u *AgentLifecycleUseCase) SetAssigner(a *DeviceAssigner) {
	u.assigner = a
}

// SetForgetDevice registers a callback invoked on Disconnect to purge any
// cached per-device state (e.g. ADB serial) from the event ingestion layer.
func (u *AgentLifecycleUseCase) SetForgetDevice(f func(domain.DeviceID)) {
	u.forgetDevice = f
}

// HelloRequest carries parsed parameters from an agent.hello JSON-RPC call.
type HelloRequest struct {
	DeviceID        domain.DeviceID
	AgentInstanceID string
	Capabilities    []domain.Capability
}

// HelloResponse is returned to the transport layer to send back to the agent.
type HelloResponse struct {
	SessionID domain.SessionID
}

func (u *AgentLifecycleUseCase) Hello(ctx context.Context, req HelloRequest, conn registry.Sender) (HelloResponse, error) {
	// Replace any existing session for this device.
	if existing, _, ok := u.reg.GetByDevice(req.DeviceID); ok {
		u.reg.Remove(existing.ID)
		u.log.Info("replaced existing session", "deviceId", req.DeviceID, "old", existing.ID)
	}

	sessionID := domain.NewSessionID()
	now := time.Now()
	session := &domain.Session{
		ID:              sessionID,
		DeviceID:        req.DeviceID,
		AgentInstanceID: req.AgentInstanceID,
		Capabilities:    req.Capabilities,
		ConnectedAt:     now,
		LastHeartbeatAt: now,
	}
	if err := u.reg.Add(session, conn); err != nil {
		return HelloResponse{}, fmt.Errorf("add session to registry: %w", err)
	}

	u.log.Info("agent.hello accepted", "deviceId", req.DeviceID, "sessionId", sessionID)

	// Notify orchestrator so any paused workflow can be resumed.
	event := domain.Event{
		ID:         fmt.Sprintf("%s:online:%d", req.DeviceID, now.UnixNano()),
		Kind:       domain.EventKindAgentOnline,
		DeviceID:   req.DeviceID,
		OccurredAt: now,
		Payload: domain.AgentOnlinePayload{
			SessionID:    sessionID,
			Capabilities: req.Capabilities,
		},
	}
	if err := u.orchestrator.ProcessEvent(ctx, event); err != nil {
		u.log.Warn("orchestrator.ProcessEvent on hello failed", "err", err)
		// Non-fatal: session is registered, agent is functional.
	}

	// Assign next pending task to this device if it is idle.
	if u.assigner != nil {
		if err := u.assigner.TryAssignPendingToDevice(ctx, req.DeviceID); err != nil {
			u.log.Warn("device assigner on hello failed", "deviceId", req.DeviceID, "err", err)
		}
	}

	return HelloResponse{SessionID: sessionID}, nil
}

// ResumeRequest carries parsed parameters from an agent.resume JSON-RPC call.
type ResumeRequest struct {
	DeviceID     domain.DeviceID
	SessionID    domain.SessionID
	Capabilities []domain.Capability
}

type ResumeResponse struct {
	SessionID domain.SessionID
}

func (u *AgentLifecycleUseCase) Resume(ctx context.Context, req ResumeRequest, conn registry.Sender) (ResumeResponse, error) {
	existing, _, ok := u.reg.GetBySession(req.SessionID)
	if !ok {
		return ResumeResponse{}, fmt.Errorf("session unknown: %s", req.SessionID)
	}

	u.reg.Remove(existing.ID)
	existing.Capabilities = req.Capabilities
	existing.LastHeartbeatAt = time.Now()
	if err := u.reg.Add(existing, conn); err != nil {
		return ResumeResponse{}, fmt.Errorf("re-add session to registry: %w", err)
	}

	u.log.Info("agent.resume accepted", "deviceId", req.DeviceID, "sessionId", req.SessionID)

	now := time.Now()
	event := domain.Event{
		ID:         fmt.Sprintf("%s:online:%d", req.DeviceID, now.UnixNano()),
		Kind:       domain.EventKindAgentOnline,
		DeviceID:   req.DeviceID,
		OccurredAt: now,
		Payload: domain.AgentOnlinePayload{
			SessionID:    req.SessionID,
			Capabilities: req.Capabilities,
		},
	}
	if err := u.orchestrator.ProcessEvent(ctx, event); err != nil {
		u.log.Warn("orchestrator.ProcessEvent on resume failed", "err", err)
	}

	if u.assigner != nil {
		if err := u.assigner.TryAssignPendingToDevice(ctx, req.DeviceID); err != nil {
			u.log.Warn("device assigner on resume failed", "deviceId", req.DeviceID, "err", err)
		}
	}

	return ResumeResponse{SessionID: req.SessionID}, nil
}

func (u *AgentLifecycleUseCase) Heartbeat(_ context.Context, deviceID domain.DeviceID, sessionID domain.SessionID) {
	if session, _, ok := u.reg.GetBySession(sessionID); ok {
		session.LastHeartbeatAt = time.Now()
	}
	u.log.Debug("agent.heartbeat", "deviceId", deviceID, "sessionId", sessionID)
}

func (u *AgentLifecycleUseCase) Disconnect(ctx context.Context, deviceID domain.DeviceID, sessionID domain.SessionID) {
	u.reg.Remove(sessionID)
	u.log.Info("agent.disconnect", "deviceId", deviceID, "sessionId", sessionID)

	if u.forgetDevice != nil {
		u.forgetDevice(deviceID)
	}

	// Re-queue any running tasks before emitting the offline event so that
	// another device can pick them up as soon as it connects.
	if u.assigner != nil {
		if err := u.assigner.OnDeviceOffline(ctx, deviceID); err != nil {
			u.log.Warn("device assigner on disconnect failed", "deviceId", deviceID, "err", err)
		}
	}

	now := time.Now()
	event := domain.Event{
		ID:         fmt.Sprintf("%s:offline:%d", deviceID, now.UnixNano()),
		Kind:       domain.EventKindAgentOffline,
		DeviceID:   deviceID,
		OccurredAt: now,
	}
	if err := u.orchestrator.ProcessEvent(ctx, event); err != nil {
		u.log.Warn("orchestrator.ProcessEvent on disconnect failed", "err", err)
	}
}
