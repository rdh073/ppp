package usecase

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/electricbubble/gadb"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// AdbShellRunner resolves a deviceID to an ADB serial via the DeviceBindingManager
// and runs arbitrary shell commands on the target device.
type AdbShellRunner struct {
	host    string
	port    int
	manager *DeviceBindingManager
}

func NewAdbShellRunner(host, portStr string, manager *DeviceBindingManager) *AdbShellRunner {
	h := strings.TrimSpace(host)
	if h == "" {
		h = defaultAdbServerHost
	}
	port := gadb.AdbServerPort
	if raw := strings.TrimSpace(portStr); raw != "" {
		var p int
		if _, err := fmt.Sscanf(raw, "%d", &p); err == nil && p > 0 {
			port = p
		}
	}
	return &AdbShellRunner{host: h, port: port, manager: manager}
}

// PmClear clears package data on the device via `adb shell pm clear <pkg>`.
// Implements handler.AdbPmClearer.
func (r *AdbShellRunner) PmClear(ctx context.Context, deviceID domain.DeviceID, pkg string) error {
	target, err := r.resolveDevice(ctx, deviceID)
	if err != nil {
		return err
	}
	out, err := target.RunShellCommand("pm", "clear", pkg)
	if err != nil {
		return fmt.Errorf("pm clear %s: %w", pkg, err)
	}
	if !strings.Contains(out, "Success") {
		return fmt.Errorf("pm clear %s: unexpected output: %s", pkg, strings.TrimSpace(out))
	}
	return nil
}

// PushFile pushes a local file to the device at remotePath.
// Implements handler.AdbFilePusher.
func (r *AdbShellRunner) PushFile(ctx context.Context, deviceID domain.DeviceID, localPath, remotePath string) error {
	target, err := r.resolveDevice(ctx, deviceID)
	if err != nil {
		return err
	}

	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local file %s: %w", localPath, err)
	}
	defer f.Close()

	// Ensure the remote directory exists.
	dir := remotePath[:strings.LastIndex(remotePath, "/")]
	if dir != "" {
		if _, err := target.RunShellCommand("mkdir", "-p", dir); err != nil {
			return fmt.Errorf("mkdir -p %s: %w", dir, err)
		}
	}

	if err := target.PushFile(f, remotePath, time.Now()); err != nil {
		return fmt.Errorf("push file to %s: %w", remotePath, err)
	}
	return nil
}

// MediaScan triggers the Android media scanner for a specific file.
// Implements handler.AdbFilePusher.
func (r *AdbShellRunner) MediaScan(ctx context.Context, deviceID domain.DeviceID, remotePath string) error {
	target, err := r.resolveDevice(ctx, deviceID)
	if err != nil {
		return err
	}
	fileURI := "file://" + remotePath
	_, err = target.RunShellCommand("am", "broadcast",
		"-a", "android.intent.action.MEDIA_SCANNER_SCAN_FILE",
		"-d", fileURI)
	return err
}

// resolveDevice is a shared helper that finds a gadb.Device for a given deviceID.
func (r *AdbShellRunner) resolveDevice(ctx context.Context, deviceID domain.DeviceID) (*gadb.Device, error) {
	binding, ok, err := r.manager.GetBinding(ctx, deviceID)
	if err != nil {
		return nil, fmt.Errorf("get binding for %s: %w", deviceID, err)
	}
	if !ok || binding == nil || binding.ADBSerial == "" {
		return nil, fmt.Errorf("no adb serial known for device %s", deviceID)
	}

	client, err := gadb.NewClientWith(r.host, r.port)
	if err != nil {
		return nil, fmt.Errorf("adb client init: %w", err)
	}
	devices, err := client.DeviceList()
	if err != nil {
		return nil, fmt.Errorf("adb device list: %w", err)
	}

	for i := range devices {
		if devices[i].Serial() == binding.ADBSerial {
			return &devices[i], nil
		}
	}
	return nil, fmt.Errorf("adb device %s not found (device %s)", binding.ADBSerial, deviceID)
}
