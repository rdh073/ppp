package domain

import "time"

type DeviceIdentityStatus string

const (
	DeviceIdentityStatusUnknown              DeviceIdentityStatus = "unknown"
	DeviceIdentityStatusSerialKnownUnverified DeviceIdentityStatus = "serial_known_unverified"
	DeviceIdentityStatusVerified             DeviceIdentityStatus = "verified"
	DeviceIdentityStatusMismatch             DeviceIdentityStatus = "mismatch"
)

type AccessibilityRemediationStatus string

const (
	AccessibilityRemediationStatusIdle            AccessibilityRemediationStatus = "idle"
	AccessibilityRemediationStatusPending         AccessibilityRemediationStatus = "pending"
	AccessibilityRemediationStatusAlreadyEnabled  AccessibilityRemediationStatus = "already_enabled"
	AccessibilityRemediationStatusEnabled         AccessibilityRemediationStatus = "enabled"
	AccessibilityRemediationStatusPendingNoADB    AccessibilityRemediationStatus = "pending_no_adb"
	AccessibilityRemediationStatusPendingNoSerial AccessibilityRemediationStatus = "pending_no_serial"
	AccessibilityRemediationStatusInfraError      AccessibilityRemediationStatus = "infra_error"
	AccessibilityRemediationStatusCommandError    AccessibilityRemediationStatus = "command_error"
	AccessibilityRemediationStatusBlockedMismatch AccessibilityRemediationStatus = "blocked_mismatch"
)

type DeviceBinding struct {
	DeviceID          DeviceID                       `json:"deviceId"`
	ADBSerial         string                         `json:"adbSerial,omitempty"`
	ObservedAndroidID string                         `json:"observedAndroidId,omitempty"`
	IdentityStatus    DeviceIdentityStatus           `json:"identityStatus"`
	RemediationStatus AccessibilityRemediationStatus `json:"remediationStatus"`
	PendingEnable     bool                           `json:"pendingEnable"`
	ServiceComponent  string                         `json:"serviceComponent,omitempty"`
	LastSeenAt        time.Time                      `json:"lastSeenAt"`
	LastVerifiedAt    time.Time                      `json:"lastVerifiedAt"`
	LastFailureAt     time.Time                      `json:"lastFailureAt"`
	LastError         string                         `json:"lastError,omitempty"`
	SerialSource      string                         `json:"serialSource,omitempty"`
}

func NewDeviceBinding(deviceID DeviceID) *DeviceBinding {
	return &DeviceBinding{
		DeviceID:          deviceID,
		IdentityStatus:    DeviceIdentityStatusUnknown,
		RemediationStatus: AccessibilityRemediationStatusIdle,
	}
}

func (b *DeviceBinding) Clone() *DeviceBinding {
	if b == nil {
		return nil
	}
	clone := *b
	return &clone
}
