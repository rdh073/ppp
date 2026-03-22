package accountmanager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
)

type LoginRunRequest struct {
	Account         domain.Account
	DeviceID        domain.DeviceID
	AccountKind     string
	PackagesToClear []string
	WorkflowName    string
}

type LoginRunDeps struct {
	Tasks    TaskControl
	Accounts store.AccountStore
	Adb      AdbPmClearer // nil = skip pm clear
}

// ExecuteLoginRun runs the 6-step login flow.
// Returns the created task ID (for observability) and any error.
func ExecuteLoginRun(
	ctx context.Context,
	req LoginRunRequest,
	deps LoginRunDeps,
	log *slog.Logger,
) (taskID domain.TaskID, err error) {
	if deps.Tasks == nil {
		return "", errors.New("tasks dependency is nil")
	}
	if deps.Accounts == nil {
		return "", errors.New("accounts dependency is nil")
	}
	if req.Account.ID == "" {
		return "", errors.New("account id is required")
	}
	if req.DeviceID == "" {
		return "", errors.New("device id is required")
	}
	if req.WorkflowName == "" {
		return "", errors.New("workflow name is required")
	}

	accountKind := req.AccountKind
	if accountKind == "" {
		accountKind = req.Account.Kind
	}
	platform := accountKind
	if platform == "" {
		platform = "account"
	}

	// Step 1-2: Deactivate current active account + clear app data on device.
	if prev, ok := deps.Accounts.FindActiveByKindOnDevice(string(req.DeviceID), accountKind); ok {
		if log != nil {
			log.Info("deactivating previous account on device",
				"platform", platform,
				"deviceId", req.DeviceID,
				"accountId", prev.ID)
		}
		_ = deps.Accounts.UpdateStatus(prev.ID, domain.AccountStatusDeactive)

		if deps.Adb != nil {
			for _, pkg := range req.PackagesToClear {
				if err := deps.Adb.PmClear(ctx, req.DeviceID, pkg); err != nil {
					if log != nil {
						log.Warn("pm clear failed (continuing anyway)",
							"deviceId", req.DeviceID,
							"pkg", pkg,
							"err", err)
					}
				} else if log != nil {
					log.Info("pm clear succeeded", "deviceId", req.DeviceID, "pkg", pkg)
				}
			}
		} else if log != nil {
			log.Warn("pm clear skipped: ADB not configured", "deviceId", req.DeviceID)
		}
	}

	// Step 3: Mark target account as in progress.
	_ = deps.Accounts.UpdateStatus(req.Account.ID, domain.AccountStatusInProgress)

	// Step 4: Create login task.
	task, err := deps.Tasks.CreateTask(ctx, CreateTaskRequest{
		Goal:         fmt.Sprintf("Login %s account %s on device %s", platform, req.Account.ID, req.DeviceID),
		DeviceID:     req.DeviceID,
		WorkflowName: req.WorkflowName,
		InputArtifacts: map[string]string{
			"email":    req.Account.Email,
			"password": req.Account.Password,
		},
	})
	if err != nil {
		_ = deps.Accounts.UpdateStatus(req.Account.ID, domain.AccountStatusDeactive)
		return "", fmt.Errorf("create task: %w", err)
	}

	// Step 5: Poll until terminal.
	summary, ok := pollTaskUntilTerminal(ctx, deps.Tasks, task.ID, 5*time.Minute, log)
	if !ok || summary == nil || summary.Task.Status != domain.TaskStatusCompleted {
		_ = deps.Accounts.UpdateStatus(req.Account.ID, domain.AccountStatusDeactive)
		if summary != nil {
			return task.ID, fmt.Errorf("task %s: %s", summary.Task.ID, summary.Task.Status)
		}
		return task.ID, errors.New("login task failed")
	}

	// Step 6: Mark account active + bind to device.
	_ = deps.Accounts.UpdateStatus(req.Account.ID, domain.AccountStatusActive)
	_ = deps.Accounts.UpdateDeviceID(req.Account.ID, string(req.DeviceID))

	return task.ID, nil
}
