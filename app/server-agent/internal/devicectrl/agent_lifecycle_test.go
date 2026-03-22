package devicectrl

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/registry"
)

func TestAgentLifecycleHelloRegistersSessionPublishesOnlineAndInfersSerial(t *testing.T) {
	reg := registry.New()
	orch := &fakeEventProcessor{}
	assigner := &fakePendingTaskAssigner{}
	notifier := &fakeAgentConnectedNotifier{}

	uc := NewAgentLifecycle(reg, orch, notifier, nil, testLogger())
	uc.SetAssigner(assigner)

	resp, err := uc.Hello(context.Background(), HelloRequest{
		DeviceID:        "device-1",
		AgentInstanceID: "agent-1",
		Capabilities: []domain.Capability{
			{Name: "device.observe", Available: true},
		},
		RemoteAddr: "10.0.0.7:34567",
	}, noopSender{})
	if err != nil {
		t.Fatalf("Hello() error = %v", err)
	}
	if resp.SessionID == "" {
		t.Fatal("Hello() returned empty session ID")
	}

	session, _, ok := reg.GetBySession(resp.SessionID)
	if !ok {
		t.Fatal("session not registered in registry")
	}
	if session.DeviceID != "device-1" {
		t.Fatalf("session deviceID = %q", session.DeviceID)
	}

	if len(orch.events) != 1 {
		t.Fatalf("orchestrator events = %d, want 1", len(orch.events))
	}
	if got, want := orch.events[0].Kind, domain.EventKindAgentOnline; got != want {
		t.Fatalf("online event kind = %q, want %q", got, want)
	}
	if len(assigner.onlineDevices) != 1 || assigner.onlineDevices[0] != "device-1" {
		t.Fatalf("assigner onlineDevices = %#v", assigner.onlineDevices)
	}
	if len(notifier.calls) != 1 {
		t.Fatalf("notifier calls = %d, want 1", len(notifier.calls))
	}
	if got := notifier.calls[0].serial; got != "10.0.0.7:5555" {
		t.Fatalf("inferred serial = %q, want 10.0.0.7:5555", got)
	}
}

func TestAgentLifecycleDisconnectRemovesSessionForgetsDeviceAndPublishesOffline(t *testing.T) {
	reg := registry.New()
	now := time.Now()
	session := &domain.Session{
		ID:              domain.SessionID("sess-1"),
		DeviceID:        domain.DeviceID("device-2"),
		AgentInstanceID: "agent-2",
		ConnectedAt:     now,
		LastHeartbeatAt: now,
	}
	if err := reg.Add(session, noopSender{}); err != nil {
		t.Fatalf("registry.Add() error = %v", err)
	}

	orch := &fakeEventProcessor{}
	assigner := &fakePendingTaskAssigner{}
	var forgotten []domain.DeviceID

	uc := NewAgentLifecycle(reg, orch, nil, func(deviceID domain.DeviceID) {
		forgotten = append(forgotten, deviceID)
	}, testLogger())
	uc.SetAssigner(assigner)

	uc.Disconnect(context.Background(), "", session.ID)

	if _, _, ok := reg.GetBySession(session.ID); ok {
		t.Fatal("session still present after Disconnect()")
	}
	if len(forgotten) != 1 || forgotten[0] != "device-2" {
		t.Fatalf("forgotten devices = %#v", forgotten)
	}
	if len(assigner.offlineDevices) != 1 || assigner.offlineDevices[0] != "device-2" {
		t.Fatalf("assigner offlineDevices = %#v", assigner.offlineDevices)
	}
	if len(orch.events) != 1 {
		t.Fatalf("orchestrator events = %d, want 1", len(orch.events))
	}
	if got, want := orch.events[0].Kind, domain.EventKindAgentOffline; got != want {
		t.Fatalf("offline event kind = %q, want %q", got, want)
	}
}

type fakeEventProcessor struct {
	events []domain.Event
}

func (f *fakeEventProcessor) ProcessEvent(_ context.Context, e domain.Event) error {
	f.events = append(f.events, e)
	return nil
}

type fakePendingTaskAssigner struct {
	onlineDevices  []domain.DeviceID
	offlineDevices []domain.DeviceID
}

func (f *fakePendingTaskAssigner) TryAssignPendingToDevice(_ context.Context, deviceID domain.DeviceID) error {
	f.onlineDevices = append(f.onlineDevices, deviceID)
	return nil
}

func (f *fakePendingTaskAssigner) OnDeviceOffline(_ context.Context, deviceID domain.DeviceID) error {
	f.offlineDevices = append(f.offlineDevices, deviceID)
	return nil
}

type connectedCall struct {
	deviceID domain.DeviceID
	serial   string
}

type fakeAgentConnectedNotifier struct {
	calls []connectedCall
}

func (f *fakeAgentConnectedNotifier) NoteAgentConnected(_ context.Context, deviceID domain.DeviceID, inferredSerial string) error {
	f.calls = append(f.calls, connectedCall{deviceID: deviceID, serial: inferredSerial})
	return nil
}

type noopSender struct{}

func (noopSender) SendRequest(id, method string, params any) error { return nil }
func (noopSender) SendSuccess(id string, result any) error         { return nil }
func (noopSender) SendError(id string, code int, message string) error {
	return nil
}
func (noopSender) Close() error { return nil }

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
