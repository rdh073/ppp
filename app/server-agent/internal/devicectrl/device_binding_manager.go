package devicectrl

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
)

const DefaultAccessibilityReconcileInterval = 15 * time.Second

type AccessibilityRemediationRequest struct {
	DeviceID         domain.DeviceID
	ADBSerial        string
	ServiceComponent string
}

type AccessibilityRemediationResult struct {
	ADBSerial         string
	ObservedAndroidID string
	IdentityStatus    domain.DeviceIdentityStatus
	RemediationStatus domain.AccessibilityRemediationStatus
	SerialSource      string
	Err               error
}

type AccessibilityRemediator interface {
	EnableBinding(ctx context.Context, req AccessibilityRemediationRequest) AccessibilityRemediationResult
}

type deviceBindingMetrics interface {
	RecordAccessibilityRemediation(outcome string)
	SetAccessibilityPendingBindings(n int64)
	SetAccessibilityBlockedBindings(n int64)
}

type DeviceBindingCoordinator interface {
	NoteDeviceEvent(ctx context.Context, deviceID domain.DeviceID, kind domain.EventKind, seqNo uint64, adbSerial, serviceComponent string, at time.Time) error
	NoteAgentConnected(ctx context.Context, deviceID domain.DeviceID, inferredSerial string) error
	Remediate(ctx context.Context, deviceID domain.DeviceID) (*domain.DeviceBinding, error)
	ForgetDevice(deviceID domain.DeviceID)
	ListBindings(ctx context.Context) ([]*domain.DeviceBinding, error)
	GetBinding(ctx context.Context, deviceID domain.DeviceID) (*domain.DeviceBinding, bool, error)
	ClearBinding(ctx context.Context, deviceID domain.DeviceID) error
	Run(ctx context.Context, interval time.Duration)
}

type DeviceBindingManager struct {
	store      store.DeviceBindingStore
	remediator AccessibilityRemediator
	metrics    deviceBindingMetrics
	log        *slog.Logger
	locks      sync.Map // domain.DeviceID -> *sync.Mutex
}

func NewDeviceBindingManager(
	bindingStore store.DeviceBindingStore,
	remediator AccessibilityRemediator,
	metrics deviceBindingMetrics,
	log *slog.Logger,
) *DeviceBindingManager {
	if log == nil {
		log = slog.Default()
	}
	return &DeviceBindingManager{
		store:      bindingStore,
		remediator: remediator,
		metrics:    metrics,
		log:        log,
	}
}

func (m *DeviceBindingManager) NoteDeviceEvent(
	ctx context.Context,
	deviceID domain.DeviceID,
	kind domain.EventKind,
	seqNo uint64,
	adbSerial string,
	serviceComponent string,
	at time.Time,
) error {
	if m == nil || m.store == nil || deviceID == "" {
		return nil
	}
	var refreshMetrics bool
	err := m.withDeviceLock(deviceID, func() error {
		binding, err := m.loadOrNew(ctx, deviceID)
		if err != nil {
			return err
		}
		if !at.IsZero() {
			binding.LastSeenAt = at
		}
		serial := stringsTrim(adbSerial)
		if serial != "" {
			if binding.ADBSerial != "" && binding.ADBSerial != serial && binding.IdentityStatus != domain.DeviceIdentityStatusMismatch {
				binding.IdentityStatus = domain.DeviceIdentityStatusSerialKnownUnverified
				binding.ObservedAndroidID = ""
				binding.LastVerifiedAt = time.Time{}
			}
			binding.ADBSerial = serial
			binding.SerialSource = "event"
			if binding.IdentityStatus == domain.DeviceIdentityStatusUnknown {
				binding.IdentityStatus = domain.DeviceIdentityStatusSerialKnownUnverified
			}
		}
		component := stringsTrim(serviceComponent)
		if component != "" {
			binding.ServiceComponent = component
		}
		if kind == domain.EventKindAccessibilityDisabled && seqNo > 0 {
			binding.PendingEnable = true
			if binding.RemediationStatus == domain.AccessibilityRemediationStatusIdle || binding.RemediationStatus == domain.AccessibilityRemediationStatusAlreadyEnabled {
				binding.RemediationStatus = domain.AccessibilityRemediationStatusPending
			}
			refreshMetrics = true
		}
		return m.store.Save(ctx, binding)
	})
	if err != nil {
		return err
	}
	if refreshMetrics {
		m.refreshMetrics(ctx)
	}
	return nil
}

// NoteAgentConnected records an inferred ADB serial derived from the agent's
// WebSocket remote address. It only sets the serial when the binding has no
// serial yet, so explicitly configured or event-sourced serials are never
// overwritten.
func (m *DeviceBindingManager) NoteAgentConnected(ctx context.Context, deviceID domain.DeviceID, inferredSerial string) error {
	if m == nil || m.store == nil || deviceID == "" || inferredSerial == "" {
		return nil
	}
	return m.withDeviceLock(deviceID, func() error {
		binding, err := m.loadOrNew(ctx, deviceID)
		if err != nil {
			return err
		}
		if binding.ADBSerial != "" {
			// Already known — do not overwrite.
			return nil
		}
		binding.ADBSerial = inferredSerial
		binding.SerialSource = "inferred-connection"
		if binding.IdentityStatus == domain.DeviceIdentityStatusUnknown {
			binding.IdentityStatus = domain.DeviceIdentityStatusSerialKnownUnverified
		}
		return m.store.Save(ctx, binding)
	})
}

func (m *DeviceBindingManager) Remediate(ctx context.Context, deviceID domain.DeviceID) (*domain.DeviceBinding, error) {
	if m == nil || m.store == nil || deviceID == "" {
		return nil, nil
	}
	var resultBinding *domain.DeviceBinding
	err := m.withDeviceLock(deviceID, func() error {
		binding, err := m.loadOrNew(ctx, deviceID)
		if err != nil {
			return err
		}
		component := binding.ServiceComponent
		if component == "" {
			component = DefaultAccessibilityServiceComponent
		}
		result := m.remediator.EnableBinding(ctx, AccessibilityRemediationRequest{
			DeviceID:         deviceID,
			ADBSerial:        binding.ADBSerial,
			ServiceComponent: component,
		})
		now := time.Now()
		if result.ADBSerial != "" {
			binding.ADBSerial = result.ADBSerial
		}
		if result.SerialSource != "" {
			binding.SerialSource = result.SerialSource
		}
		if result.ObservedAndroidID != "" {
			binding.ObservedAndroidID = result.ObservedAndroidID
		}
		if result.IdentityStatus != "" {
			binding.IdentityStatus = result.IdentityStatus
		}
		if result.RemediationStatus != "" {
			binding.RemediationStatus = result.RemediationStatus
		}
		switch result.RemediationStatus {
		case domain.AccessibilityRemediationStatusEnabled, domain.AccessibilityRemediationStatusAlreadyEnabled:
			binding.PendingEnable = false
			binding.LastError = ""
			if binding.IdentityStatus == domain.DeviceIdentityStatusVerified {
				binding.LastVerifiedAt = now
			}
		case domain.AccessibilityRemediationStatusBlockedMismatch:
			binding.PendingEnable = false
			binding.LastFailureAt = now
		case domain.AccessibilityRemediationStatusPendingNoADB,
			domain.AccessibilityRemediationStatusPendingNoSerial,
			domain.AccessibilityRemediationStatusInfraError,
			domain.AccessibilityRemediationStatusCommandError,
			domain.AccessibilityRemediationStatusPending:
			binding.PendingEnable = true
			binding.LastFailureAt = now
		default:
			binding.PendingEnable = false
		}
		if result.Err != nil {
			binding.LastError = result.Err.Error()
		}
		if binding.IdentityStatus == domain.DeviceIdentityStatusVerified && binding.LastVerifiedAt.IsZero() {
			binding.LastVerifiedAt = now
		}
		if err := m.store.Save(ctx, binding); err != nil {
			return err
		}
		if m.metrics != nil {
			m.metrics.RecordAccessibilityRemediation(string(binding.RemediationStatus))
		}
		m.log.Info(
			"accessibility remediation updated",
			"deviceId", binding.DeviceID,
			"adbSerial", binding.ADBSerial,
			"observedAndroidId", binding.ObservedAndroidID,
			"identityStatus", binding.IdentityStatus,
			"remediationStatus", binding.RemediationStatus,
			"pendingEnable", binding.PendingEnable,
			"error", binding.LastError,
		)
		resultBinding = binding.Clone()
		return nil
	})
	if err != nil {
		return nil, err
	}
	m.refreshMetrics(ctx)
	return resultBinding, nil
}

func (m *DeviceBindingManager) ForgetDevice(domain.DeviceID) {}

func (m *DeviceBindingManager) ListBindings(ctx context.Context) ([]*domain.DeviceBinding, error) {
	if m == nil || m.store == nil {
		return nil, nil
	}
	return m.store.List(ctx)
}

func (m *DeviceBindingManager) GetBinding(ctx context.Context, deviceID domain.DeviceID) (*domain.DeviceBinding, bool, error) {
	if m == nil || m.store == nil {
		return nil, false, nil
	}
	binding, err := m.store.Get(ctx, deviceID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return binding, true, nil
}

func (m *DeviceBindingManager) ClearBinding(ctx context.Context, deviceID domain.DeviceID) error {
	if m == nil || m.store == nil || deviceID == "" {
		return nil
	}
	if err := m.withDeviceLock(deviceID, func() error {
		return m.store.Clear(ctx, deviceID)
	}); err != nil {
		return err
	}
	m.refreshMetrics(ctx)
	m.log.Info("device binding cleared", "deviceId", deviceID)
	return nil
}

func (m *DeviceBindingManager) Run(ctx context.Context, interval time.Duration) {
	if m == nil || m.store == nil || m.remediator == nil {
		return
	}
	if interval <= 0 {
		interval = DefaultAccessibilityReconcileInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	m.refreshMetrics(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.reconcilePending(ctx)
		}
	}
}

func (m *DeviceBindingManager) reconcilePending(ctx context.Context) {
	bindings, err := m.store.List(ctx)
	if err != nil {
		m.log.Warn("list device bindings failed", "err", err)
		return
	}
	for _, binding := range bindings {
		if binding == nil || !binding.PendingEnable {
			continue
		}
		if binding.RemediationStatus == domain.AccessibilityRemediationStatusBlockedMismatch {
			continue
		}
		if _, err := m.Remediate(ctx, binding.DeviceID); err != nil {
			m.log.Warn("background remediation failed", "deviceId", binding.DeviceID, "err", err)
		}
	}
}

func (m *DeviceBindingManager) loadOrNew(ctx context.Context, deviceID domain.DeviceID) (*domain.DeviceBinding, error) {
	binding, err := m.store.Get(ctx, deviceID)
	if err == nil {
		return binding, nil
	}
	if errors.Is(err, store.ErrNotFound) {
		return domain.NewDeviceBinding(deviceID), nil
	}
	return nil, err
}

func (m *DeviceBindingManager) withDeviceLock(deviceID domain.DeviceID, fn func() error) error {
	if deviceID == "" {
		return fn()
	}
	v, _ := m.locks.LoadOrStore(deviceID, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	return fn()
}

func (m *DeviceBindingManager) refreshMetrics(ctx context.Context) {
	if m == nil || m.metrics == nil || m.store == nil {
		return
	}
	bindings, err := m.store.List(ctx)
	if err != nil {
		m.log.Warn("refresh device binding metrics failed", "err", err)
		return
	}
	var pending int64
	var blocked int64
	for _, binding := range bindings {
		if binding == nil {
			continue
		}
		if binding.PendingEnable {
			pending++
		}
		if binding.RemediationStatus == domain.AccessibilityRemediationStatusBlockedMismatch {
			blocked++
		}
	}
	m.metrics.SetAccessibilityPendingBindings(pending)
	m.metrics.SetAccessibilityBlockedBindings(blocked)
}

func stringsTrim(v string) string {
	return strings.TrimSpace(v)
}
