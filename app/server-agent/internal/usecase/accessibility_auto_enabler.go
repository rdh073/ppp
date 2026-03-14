package usecase

import (
	"context"
	"fmt"
	"strconv"
	"strings"

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

// AdbAccessibilityAutoEnabler runs adb shell settings commands to ensure the
// agent accessibility service is enabled on the target device.
type AdbAccessibilityAutoEnabler struct {
	adbHost                 string
	adbPort                 int
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
	if err := ctx.Err(); err != nil {
		return err
	}

	serial := strings.TrimSpace(adbSerial)
	component := strings.TrimSpace(serviceComponent)
	if component == "" {
		component = a.defaultServiceComponent
	}

	client, err := a.newClient(a.adbHost, a.adbPort)
	if err != nil {
		return fmt.Errorf("adb client init failed: %w", err)
	}

	devices, err := client.DeviceList()
	if err != nil {
		return fmt.Errorf("adb device list failed: %w", err)
	}

	device, err := selectTargetDevice(devices, serial, deviceID)
	if err != nil {
		return err
	}

	current, err := a.getEnabledServices(ctx, device)
	if err != nil {
		return err
	}

	if !containsComponent(current, component) {
		next := component
		if current != "" {
			next = current + ":" + component
		}
		if _, err := a.runShell(ctx, device, "settings", "put", "secure", "enabled_accessibility_services", next); err != nil {
			return err
		}
	}

	if _, err := a.runShell(ctx, device, "settings", "put", "secure", "accessibility_enabled", "1"); err != nil {
		return err
	}
	return nil
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
