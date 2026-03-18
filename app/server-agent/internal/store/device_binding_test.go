package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

func TestMemoryDeviceBindingStore_CRUD(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := NewMemoryDeviceBindingStore()
	binding := &domain.DeviceBinding{
		DeviceID:          domain.DeviceID("dev-1"),
		ADBSerial:         "emulator-5554",
		IdentityStatus:    domain.DeviceIdentityStatusVerified,
		RemediationStatus: domain.AccessibilityRemediationStatusEnabled,
		PendingEnable:     false,
		LastSeenAt:        time.Unix(1700000000, 0).UTC(),
	}

	if err := s.Save(ctx, binding); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := s.Get(ctx, binding.DeviceID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got == binding {
		t.Fatal("Get() returned original pointer; want clone")
	}
	if got.ADBSerial != binding.ADBSerial {
		t.Fatalf("ADBSerial = %q, want %q", got.ADBSerial, binding.ADBSerial)
	}

	items, err := s.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("List() len = %d, want 1", len(items))
	}

	if err := s.Clear(ctx, binding.DeviceID); err != nil {
		t.Fatalf("Clear() error = %v", err)
	}
	if _, err := s.Get(ctx, binding.DeviceID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() after Clear error = %v, want ErrNotFound", err)
	}
}

func TestFileDeviceBindingStore_PersistsAcrossReopen(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()

	first, err := NewFileDeviceBindingStore(dir)
	if err != nil {
		t.Fatalf("NewFileDeviceBindingStore(first) error = %v", err)
	}

	saved := &domain.DeviceBinding{
		DeviceID:          domain.DeviceID("dev-2"),
		ADBSerial:         "usb-123",
		ObservedAndroidID: "dev-2",
		IdentityStatus:    domain.DeviceIdentityStatusVerified,
		RemediationStatus: domain.AccessibilityRemediationStatusAlreadyEnabled,
		PendingEnable:     false,
		ServiceComponent:  "pkg/.Svc",
		LastSeenAt:        time.Unix(1700000100, 123).UTC(),
		LastVerifiedAt:    time.Unix(1700000200, 456).UTC(),
	}
	if err := first.Save(ctx, saved); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	second, err := NewFileDeviceBindingStore(dir)
	if err != nil {
		t.Fatalf("NewFileDeviceBindingStore(second) error = %v", err)
	}

	got, err := second.Get(ctx, saved.DeviceID)
	if err != nil {
		t.Fatalf("Get() after reopen error = %v", err)
	}
	if got.DeviceID != saved.DeviceID {
		t.Fatalf("DeviceID = %q, want %q", got.DeviceID, saved.DeviceID)
	}
	if got.ADBSerial != saved.ADBSerial {
		t.Fatalf("ADBSerial = %q, want %q", got.ADBSerial, saved.ADBSerial)
	}
	if got.ObservedAndroidID != saved.ObservedAndroidID {
		t.Fatalf("ObservedAndroidID = %q, want %q", got.ObservedAndroidID, saved.ObservedAndroidID)
	}
	if !got.LastVerifiedAt.Equal(saved.LastVerifiedAt) {
		t.Fatalf("LastVerifiedAt = %v, want %v", got.LastVerifiedAt, saved.LastVerifiedAt)
	}
}
