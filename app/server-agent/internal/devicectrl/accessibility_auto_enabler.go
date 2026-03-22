package devicectrl

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/electricbubble/gadb"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

const DefaultAccessibilityServiceComponent = "com.autosdk.agent/com.autosdk.agent.service.AgentAccessibilityService"
const defaultAdbServerHost = "localhost"

// AccessibilityAutoEnabler enables the accessibility service for a device
// after an explicit android.accessibility.* disabled event.
type AccessibilityAutoEnabler interface {
	Enable(ctx context.Context, deviceID domain.DeviceID, adbSerial, serviceComponent string) error
}

type noopAccessibilityAutoEnabler struct{}

func (noopAccessibilityAutoEnabler) Enable(context.Context, domain.DeviceID, string, string) error {
	return nil
}

func (noopAccessibilityAutoEnabler) EnableBinding(context.Context, AccessibilityRemediationRequest) AccessibilityRemediationResult {
	return AccessibilityRemediationResult{
		IdentityStatus:    domain.DeviceIdentityStatusUnknown,
		RemediationStatus: domain.AccessibilityRemediationStatusPendingNoADB,
	}
}

// AdbAccessibilityAutoEnabler runs adb shell settings commands to ensure the
// agent accessibility service is enabled on the target device.
type AdbAccessibilityAutoEnabler struct {
	adbHost                 string
	adbPort                 int
	adbSerialByDeviceID     map[domain.DeviceID]string
	defaultServiceComponent string
	newClient               func(host string, port int) (adbClient, error)
}

type adbClient interface {
	DeviceList() ([]gadb.Device, error)
}

func NewAdbAccessibilityAutoEnabler(
	adbServerHost string,
	adbServerPort string,
	defaultServiceComponent string,
	adbSerialByDevice string,
) *AdbAccessibilityAutoEnabler {
	host := strings.TrimSpace(adbServerHost)
	if host == "" {
		host = defaultAdbServerHost
	}

	port := gadb.AdbServerPort
	if raw := strings.TrimSpace(adbServerPort); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			port = parsed
		}
	}

	if strings.TrimSpace(defaultServiceComponent) == "" {
		defaultServiceComponent = DefaultAccessibilityServiceComponent
	}

	return &AdbAccessibilityAutoEnabler{
		adbHost:                 host,
		adbPort:                 port,
		adbSerialByDeviceID:     parseADBSerialByDeviceMap(adbSerialByDevice),
		defaultServiceComponent: defaultServiceComponent,
		newClient: func(host string, port int) (adbClient, error) {
			return gadb.NewClientWith(host, port)
		},
	}
}

func (a *AdbAccessibilityAutoEnabler) Enable(
	ctx context.Context,
	deviceID domain.DeviceID,
	adbSerial string,
	serviceComponent string,
) error {
	result := a.EnableBinding(ctx, AccessibilityRemediationRequest{
		DeviceID:         deviceID,
		ADBSerial:        adbSerial,
		ServiceComponent: serviceComponent,
	})
	return result.Err
}

func (a *AdbAccessibilityAutoEnabler) EnableBinding(
	ctx context.Context,
	req AccessibilityRemediationRequest,
) AccessibilityRemediationResult {
	var last AccessibilityRemediationResult
	for attempt := 0; attempt < 3; attempt++ {
		last = a.enableOnce(ctx, req)
		if last.Err == nil || !isRetryableRemediationStatus(last.RemediationStatus) {
			return last
		}
		if attempt == 2 {
			break
		}
		delay := time.Second * (1 << uint(attempt))
		select {
		case <-ctx.Done():
			return AccessibilityRemediationResult{
				ADBSerial:         last.ADBSerial,
				ObservedAndroidID: last.ObservedAndroidID,
				IdentityStatus:    last.IdentityStatus,
				RemediationStatus: domain.AccessibilityRemediationStatusInfraError,
				SerialSource:      last.SerialSource,
				Err:               ctx.Err(),
			}
		case <-time.After(delay):
		}
	}
	return last
}

func (a *AdbAccessibilityAutoEnabler) enableOnce(
	ctx context.Context,
	req AccessibilityRemediationRequest,
) AccessibilityRemediationResult {
	if err := ctx.Err(); err != nil {
		return AccessibilityRemediationResult{
			ADBSerial:         strings.TrimSpace(req.ADBSerial),
			IdentityStatus:    domain.DeviceIdentityStatusUnknown,
			RemediationStatus: domain.AccessibilityRemediationStatusInfraError,
			Err:               err,
		}
	}

	serial := strings.TrimSpace(req.ADBSerial)
	serialSource := "event"
	if serial == "" {
		serial = strings.TrimSpace(a.adbSerialByDeviceID[req.DeviceID])
		serialSource = "config"
	}
	component := strings.TrimSpace(req.ServiceComponent)
	if component == "" {
		component = a.defaultServiceComponent
	}

	result := AccessibilityRemediationResult{
		ADBSerial:         serial,
		IdentityStatus:    domain.DeviceIdentityStatusUnknown,
		RemediationStatus: domain.AccessibilityRemediationStatusPending,
		SerialSource:      serialSource,
	}
	if serial != "" {
		result.IdentityStatus = domain.DeviceIdentityStatusSerialKnownUnverified
	}
	if serial == "" {
		result.RemediationStatus = domain.AccessibilityRemediationStatusPendingNoSerial
		result.Err = fmt.Errorf("adb serial unknown for deviceID=%q", req.DeviceID)
		return result
	}

	client, err := a.newClient(a.adbHost, a.adbPort)
	if err != nil {
		result.RemediationStatus = domain.AccessibilityRemediationStatusPendingNoADB
		result.Err = fmt.Errorf("adb client init failed: %w", err)
		return result
	}

	devices, err := client.DeviceList()
	if err != nil {
		result.RemediationStatus = domain.AccessibilityRemediationStatusPendingNoADB
		result.Err = fmt.Errorf("adb device list failed: %w", err)
		return result
	}
	if len(devices) == 0 {
		result.RemediationStatus = domain.AccessibilityRemediationStatusPendingNoADB
		result.Err = fmt.Errorf("adb has no connected devices; cannot enable accessibility for deviceID=%q", req.DeviceID)
		return result
	}

	device, err := selectTargetDevice(devices, serial, req.DeviceID)
	if err != nil {
		if strings.Contains(err.Error(), "adbSerial is required") {
			result.RemediationStatus = domain.AccessibilityRemediationStatusPendingNoSerial
		} else {
			result.RemediationStatus = domain.AccessibilityRemediationStatusPendingNoADB
		}
		result.Err = err
		return result
	}
	result.ADBSerial = device.Serial()

	observed, verifyErr := a.verifyDeviceIdentity(ctx, device, req.DeviceID)
	result.ObservedAndroidID = observed
	if verifyErr != nil {
		if observed != "" && observed != "null" && observed != normalizeAndroidID(string(req.DeviceID)) {
			result.IdentityStatus = domain.DeviceIdentityStatusMismatch
			result.RemediationStatus = domain.AccessibilityRemediationStatusBlockedMismatch
		} else {
			result.RemediationStatus = domain.AccessibilityRemediationStatusCommandError
		}
		result.Err = verifyErr
		return result
	}
	result.IdentityStatus = domain.DeviceIdentityStatusVerified

	current, err := a.getEnabledServices(ctx, device)
	if err != nil {
		result.RemediationStatus = domain.AccessibilityRemediationStatusCommandError
		result.Err = err
		return result
	}

	if containsComponent(current, component) {
		result.RemediationStatus = domain.AccessibilityRemediationStatusAlreadyEnabled
		return result
	}

	next := component
	if current != "" {
		next = current + ":" + component
	}
	if _, err := a.runShell(ctx, device, "settings", "put", "secure", "enabled_accessibility_services", next); err != nil {
		result.RemediationStatus = domain.AccessibilityRemediationStatusCommandError
		result.Err = err
		return result
	}
	if _, err := a.runShell(ctx, device, "settings", "put", "secure", "accessibility_enabled", "1"); err != nil {
		result.RemediationStatus = domain.AccessibilityRemediationStatusCommandError
		result.Err = err
		return result
	}
	result.RemediationStatus = domain.AccessibilityRemediationStatusEnabled
	return result
}

func (a *AdbAccessibilityAutoEnabler) getEnabledServices(ctx context.Context, device gadb.Device) (string, error) {
	out, err := a.runShell(ctx, device, "settings", "get", "secure", "enabled_accessibility_services")
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(out)
	if value == "null" {
		return "", nil
	}
	return value, nil
}

func (a *AdbAccessibilityAutoEnabler) runShell(
	ctx context.Context,
	device gadb.Device,
	cmd string,
	args ...string,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	out, err := device.RunShellCommand(cmd, args...)
	if err != nil {
		return "", fmt.Errorf("adb shell %s %s failed on %s: %w", cmd, strings.Join(args, " "), device.Serial(), err)
	}
	return out, nil
}

func (a *AdbAccessibilityAutoEnabler) verifyDeviceIdentity(
	ctx context.Context,
	device gadb.Device,
	deviceID domain.DeviceID,
) (string, error) {
	out, err := a.runShell(ctx, device, "settings", "get", "secure", "android_id")
	if err != nil {
		return "", fmt.Errorf("failed to verify android_id on %s: %w", device.Serial(), err)
	}

	observed := normalizeAndroidID(out)
	expected := normalizeAndroidID(string(deviceID))
	if observed == "" || observed == "null" || expected == "" {
		return observed, fmt.Errorf("cannot verify device identity: expected=%q observed=%q serial=%q", expected, observed, device.Serial())
	}
	if observed != expected {
		return observed, fmt.Errorf(
			"device identity mismatch: deviceID=%q serial=%q android_id=%q",
			deviceID,
			device.Serial(),
			observed,
		)
	}
	return observed, nil
}

func selectTargetDevice(devices []gadb.Device, adbSerial string, deviceID domain.DeviceID) (gadb.Device, error) {
	available := make([]string, 0, len(devices))
	for _, d := range devices {
		available = append(available, d.Serial())
	}

	serial := strings.TrimSpace(adbSerial)
	if serial == "" {
		if len(devices) == 1 {
			return devices[0], nil
		}
		if len(devices) == 0 {
			return gadb.Device{}, fmt.Errorf("adb has no connected devices; cannot enable accessibility for deviceID=%q", deviceID)
		}
		return gadb.Device{}, fmt.Errorf(
			"adbSerial is required when multiple devices are connected for deviceID=%q (available=%s)",
			deviceID,
			strings.Join(available, ","),
		)
	}

	for _, d := range devices {
		if d.Serial() == serial {
			return d, nil
		}
	}

	return gadb.Device{}, fmt.Errorf("adb device %q not found (available=%s)", serial, strings.Join(available, ","))
}

func containsComponent(enabled, component string) bool {
	if enabled == "" || component == "" {
		return false
	}
	for _, item := range strings.Split(enabled, ":") {
		if strings.TrimSpace(item) == component {
			return true
		}
	}
	return false
}

func normalizeAndroidID(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func parseADBSerialByDeviceMap(raw string) map[domain.DeviceID]string {
	result := make(map[domain.DeviceID]string)
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		deviceID := domain.DeviceID(strings.TrimSpace(parts[0]))
		serial := strings.TrimSpace(parts[1])
		if deviceID == "" || serial == "" {
			continue
		}
		result[deviceID] = serial
	}
	return result
}

func isRetryableRemediationStatus(status domain.AccessibilityRemediationStatus) bool {
	switch status {
	case domain.AccessibilityRemediationStatusPendingNoADB,
		domain.AccessibilityRemediationStatusInfraError,
		domain.AccessibilityRemediationStatusCommandError:
		return true
	default:
		return false
	}
}
