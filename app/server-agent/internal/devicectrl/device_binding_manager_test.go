package devicectrl

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
)

type fakeAccessibilityRemediator struct {
	result AccessibilityRemediationResult
	calls  []AccessibilityRemediationRequest
}

func (f *fakeAccessibilityRemediator) EnableBinding(_ context.Context, req AccessibilityRemediationRequest) AccessibilityRemediationResult {
	f.calls = append(f.calls, req)
	return f.result
}

type fakeBindingMetrics struct {
	outcomes []string
	pending  int64
	blocked  int64
}

func (m *fakeBindingMetrics) RecordAccessibilityRemediation(outcome string) {
	m.outcomes = append(m.outcomes, outcome)
}

func (m *fakeBindingMetrics) SetAccessibilityPendingBindings(n int64) {
	m.pending = n
}

func (m *fakeBindingMetrics) SetAccessibilityBlockedBindings(n int64) {
	m.blocked = n
}

func testBindingLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestDeviceBindingManager_NoteDeviceEventLearnsSerialAndQueuesRemediation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := store.NewMemoryDeviceBindingStore()
	manager := NewDeviceBindingManager(store, &fakeAccessibilityRemediator{}, &fakeBindingMetrics{}, testBindingLogger())
	now := time.Unix(1700000000, 0).UTC()

	if err := manager.NoteDeviceEvent(ctx, "dev-1", domain.EventKindAppForeground, 1, "emulator-5554", "", now); err != nil {
		t.Fatalf("NoteDeviceEvent(app foreground) error = %v", err)
	}
	if err := manager.NoteDeviceEvent(ctx, "dev-1", domain.EventKindAccessibilityDisabled, 2, "", "pkg/.Svc", now.Add(time.Second)); err != nil {
		t.Fatalf("NoteDeviceEvent(accessibility disabled) error = %v", err)
	}

	binding, ok, err := manager.GetBinding(ctx, "dev-1")
	if err != nil {
		t.Fatalf("GetBinding() error = %v", err)
	}
	if !ok {
		t.Fatal("GetBinding() ok = false, want true")
	}
	if binding.ADBSerial != "emulator-5554" {
		t.Fatalf("ADBSerial = %q, want emulator-5554", binding.ADBSerial)
	}
	if binding.IdentityStatus != domain.DeviceIdentityStatusSerialKnownUnverified {
		t.Fatalf("IdentityStatus = %q, want %q", binding.IdentityStatus, domain.DeviceIdentityStatusSerialKnownUnverified)
	}
	if !binding.PendingEnable {
		t.Fatal("PendingEnable = false, want true")
	}
	if binding.RemediationStatus != domain.AccessibilityRemediationStatusPending {
		t.Fatalf("RemediationStatus = %q, want %q", binding.RemediationStatus, domain.AccessibilityRemediationStatusPending)
	}
	if binding.ServiceComponent != "pkg/.Svc" {
		t.Fatalf("ServiceComponent = %q, want pkg/.Svc", binding.ServiceComponent)
	}
}

func TestDeviceBindingManager_RemediateSuccessClearsPending(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := store.NewMemoryDeviceBindingStore()
	metrics := &fakeBindingMetrics{}
	remediator := &fakeAccessibilityRemediator{
		result: AccessibilityRemediationResult{
			ADBSerial:         "emulator-5554",
			ObservedAndroidID: "dev-1",
			IdentityStatus:    domain.DeviceIdentityStatusVerified,
			RemediationStatus: domain.AccessibilityRemediationStatusEnabled,
			SerialSource:      "event",
		},
	}
	manager := NewDeviceBindingManager(store, remediator, metrics, testBindingLogger())

	if err := store.Save(ctx, &domain.DeviceBinding{
		DeviceID:          "dev-1",
		ADBSerial:         "emulator-5554",
		IdentityStatus:    domain.DeviceIdentityStatusSerialKnownUnverified,
		RemediationStatus: domain.AccessibilityRemediationStatusPending,
		PendingEnable:     true,
		ServiceComponent:  "pkg/.Svc",
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	binding, err := manager.Remediate(ctx, "dev-1")
	if err != nil {
		t.Fatalf("Remediate() error = %v", err)
	}
	if len(remediator.calls) != 1 {
		t.Fatalf("remediator calls = %d, want 1", len(remediator.calls))
	}
	if binding.RemediationStatus != domain.AccessibilityRemediationStatusEnabled {
		t.Fatalf("RemediationStatus = %q, want %q", binding.RemediationStatus, domain.AccessibilityRemediationStatusEnabled)
	}
	if binding.IdentityStatus != domain.DeviceIdentityStatusVerified {
		t.Fatalf("IdentityStatus = %q, want %q", binding.IdentityStatus, domain.DeviceIdentityStatusVerified)
	}
	if binding.PendingEnable {
		t.Fatal("PendingEnable = true, want false")
	}
	if binding.LastVerifiedAt.IsZero() {
		t.Fatal("LastVerifiedAt is zero, want non-zero")
	}
	if metrics.pending != 0 {
		t.Fatalf("pending metric = %d, want 0", metrics.pending)
	}
	if len(metrics.outcomes) != 1 || metrics.outcomes[0] != string(domain.AccessibilityRemediationStatusEnabled) {
		t.Fatalf("outcomes = %v, want [%q]", metrics.outcomes, domain.AccessibilityRemediationStatusEnabled)
	}
}

func TestDeviceBindingManager_RemediateMismatchBlocksBinding(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := store.NewMemoryDeviceBindingStore()
	metrics := &fakeBindingMetrics{}
	remediator := &fakeAccessibilityRemediator{
		result: AccessibilityRemediationResult{
			ADBSerial:         "emulator-5556",
			ObservedAndroidID: "other-device",
			IdentityStatus:    domain.DeviceIdentityStatusMismatch,
			RemediationStatus: domain.AccessibilityRemediationStatusBlockedMismatch,
			SerialSource:      "event",
			Err:               errors.New("device identity mismatch"),
		},
	}
	manager := NewDeviceBindingManager(store, remediator, metrics, testBindingLogger())

	if err := store.Save(ctx, &domain.DeviceBinding{
		DeviceID:          "dev-2",
		ADBSerial:         "emulator-5556",
		IdentityStatus:    domain.DeviceIdentityStatusSerialKnownUnverified,
		RemediationStatus: domain.AccessibilityRemediationStatusPending,
		PendingEnable:     true,
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	binding, err := manager.Remediate(ctx, "dev-2")
	if err != nil {
		t.Fatalf("Remediate() error = %v", err)
	}
	if binding.IdentityStatus != domain.DeviceIdentityStatusMismatch {
		t.Fatalf("IdentityStatus = %q, want %q", binding.IdentityStatus, domain.DeviceIdentityStatusMismatch)
	}
	if binding.RemediationStatus != domain.AccessibilityRemediationStatusBlockedMismatch {
		t.Fatalf("RemediationStatus = %q, want %q", binding.RemediationStatus, domain.AccessibilityRemediationStatusBlockedMismatch)
	}
	if binding.PendingEnable {
		t.Fatal("PendingEnable = true, want false")
	}
	if binding.LastFailureAt.IsZero() {
		t.Fatal("LastFailureAt is zero, want non-zero")
	}
	if binding.LastError == "" {
		t.Fatal("LastError empty, want mismatch error")
	}
	if metrics.blocked != 1 {
		t.Fatalf("blocked metric = %d, want 1", metrics.blocked)
	}
}
