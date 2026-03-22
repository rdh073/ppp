package devicectrl

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/registry"
)

type fakeSender struct{}

func (fakeSender) SendRequest(id, method string, params any) error { return nil }
func (fakeSender) SendSuccess(id string, result any) error         { return nil }
func (fakeSender) SendError(id string, code int, message string) error {
	return nil
}
func (fakeSender) Close() error { return nil }

type fakeDeviceBindingController struct {
	bindings          map[domain.DeviceID]*domain.DeviceBinding
	cleared           []domain.DeviceID
	remediationCalls  []domain.DeviceID
	remediationResult *domain.DeviceBinding
}

func (f *fakeDeviceBindingController) ListBindings(context.Context) ([]*domain.DeviceBinding, error) {
	out := make([]*domain.DeviceBinding, 0, len(f.bindings))
	for _, binding := range f.bindings {
		clone := *binding
		out = append(out, &clone)
	}
	return out, nil
}

func (f *fakeDeviceBindingController) GetBinding(_ context.Context, deviceID domain.DeviceID) (*domain.DeviceBinding, bool, error) {
	binding, ok := f.bindings[deviceID]
	if !ok {
		return nil, false, nil
	}
	clone := *binding
	return &clone, true, nil
}

func (f *fakeDeviceBindingController) ClearBinding(_ context.Context, deviceID domain.DeviceID) error {
	f.cleared = append(f.cleared, deviceID)
	delete(f.bindings, deviceID)
	return nil
}

func (f *fakeDeviceBindingController) Remediate(_ context.Context, deviceID domain.DeviceID) (*domain.DeviceBinding, error) {
	f.remediationCalls = append(f.remediationCalls, deviceID)
	if f.remediationResult == nil {
		if binding, ok := f.bindings[deviceID]; ok {
			clone := *binding
			return &clone, nil
		}
		return nil, nil
	}
	clone := *f.remediationResult
	f.bindings[deviceID] = &clone
	return &clone, nil
}

func testDeviceLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestDeviceHandler_GetMergesSessionAndBinding(t *testing.T) {
	t.Parallel()

	reg := registry.New()
	now := time.Unix(1700000000, 0).UTC()
	session := &domain.Session{
		ID:              "sess-1",
		DeviceID:        "dev-1",
		AgentInstanceID: "agent-1",
		Capabilities:    []domain.Capability{{Name: "observe"}},
		DeviceMetadata: domain.AgentDeviceMetadata{
			Manufacturer: "Google",
			Model:        "Pixel 7",
		},
		ConnectedAt:     now,
		LastHeartbeatAt: now.Add(time.Minute),
	}
	if err := reg.Add(session, fakeSender{}); err != nil {
		t.Fatalf("reg.Add() error = %v", err)
	}
	controller := &fakeDeviceBindingController{
		bindings: map[domain.DeviceID]*domain.DeviceBinding{
			"dev-1": {
				DeviceID:          "dev-1",
				ADBSerial:         "emulator-5554",
				ObservedAndroidID: "dev-1",
				IdentityStatus:    domain.DeviceIdentityStatusVerified,
				RemediationStatus: domain.AccessibilityRemediationStatusEnabled,
				PendingEnable:     false,
			},
		},
	}
	handler := NewDeviceHandler(reg, testDeviceLogger(), controller)

	req := httptest.NewRequest(http.MethodGet, "/devices/dev-1", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got deviceView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !got.Connected {
		t.Fatal("Connected = false, want true")
	}
	if got.SessionID != "sess-1" {
		t.Fatalf("SessionID = %q, want sess-1", got.SessionID)
	}
	if got.ADBSerial != "emulator-5554" {
		t.Fatalf("ADBSerial = %q, want emulator-5554", got.ADBSerial)
	}
	if got.AndroidIdentity != "Google Pixel 7" {
		t.Fatalf("AndroidIdentity = %q, want %q", got.AndroidIdentity, "Google Pixel 7")
	}
	if got.IdentityStatus != domain.DeviceIdentityStatusVerified {
		t.Fatalf("IdentityStatus = %q, want %q", got.IdentityStatus, domain.DeviceIdentityStatusVerified)
	}
}

func TestDeviceHandler_ClearBinding(t *testing.T) {
	t.Parallel()

	controller := &fakeDeviceBindingController{
		bindings: map[domain.DeviceID]*domain.DeviceBinding{
			"dev-2": {DeviceID: "dev-2"},
		},
	}
	handler := NewDeviceHandler(registry.New(), testDeviceLogger(), controller)

	req := httptest.NewRequest(http.MethodPost, "/devices/dev-2/binding/clear", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
	if len(controller.cleared) != 1 || controller.cleared[0] != "dev-2" {
		t.Fatalf("cleared = %v, want [dev-2]", controller.cleared)
	}
}

func TestDeviceHandler_RemediateReturnsUpdatedBinding(t *testing.T) {
	t.Parallel()

	reg := registry.New()
	session := &domain.Session{
		ID:              "sess-3",
		DeviceID:        "dev-3",
		AgentInstanceID: "agent-3",
		ConnectedAt:     time.Unix(1700000000, 0).UTC(),
		LastHeartbeatAt: time.Unix(1700000060, 0).UTC(),
	}
	if err := reg.Add(session, fakeSender{}); err != nil {
		t.Fatalf("reg.Add() error = %v", err)
	}
	controller := &fakeDeviceBindingController{
		bindings: map[domain.DeviceID]*domain.DeviceBinding{
			"dev-3": {DeviceID: "dev-3"},
		},
		remediationResult: &domain.DeviceBinding{
			DeviceID:          "dev-3",
			ADBSerial:         "usb-9",
			ObservedAndroidID: "dev-3",
			IdentityStatus:    domain.DeviceIdentityStatusVerified,
			RemediationStatus: domain.AccessibilityRemediationStatusAlreadyEnabled,
		},
	}
	handler := NewDeviceHandler(reg, testDeviceLogger(), controller)

	req := httptest.NewRequest(http.MethodPost, "/devices/dev-3/accessibility/remediate", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if len(controller.remediationCalls) != 1 || controller.remediationCalls[0] != "dev-3" {
		t.Fatalf("remediationCalls = %v, want [dev-3]", controller.remediationCalls)
	}
	var got deviceView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if got.ADBSerial != "usb-9" {
		t.Fatalf("ADBSerial = %q, want usb-9", got.ADBSerial)
	}
	if got.RemediationStatus != domain.AccessibilityRemediationStatusAlreadyEnabled {
		t.Fatalf("RemediationStatus = %q, want %q", got.RemediationStatus, domain.AccessibilityRemediationStatusAlreadyEnabled)
	}
}
