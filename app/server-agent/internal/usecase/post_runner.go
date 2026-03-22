package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

type PostJobRequest struct {
	CampaignID     string
	JobID          string
	AccountID      string
	DeviceID       domain.DeviceID
	LocalImagePath string
	RemoteImageDir string // e.g. "/sdcard/DCIM/ppp_posts"
	Caption        string
	WorkflowName   string // default "instagram-post-script" when empty
}

type PostJobDeps struct {
	Tasks TaskControl
	Adb   AdbFilePusher // nil = skip push
}

// ExecutePostJob pushes the image, creates a post task, polls until terminal.
// Returns the created task ID and any error.
func ExecutePostJob(
	ctx context.Context,
	req PostJobRequest,
	deps PostJobDeps,
	log *slog.Logger,
) (taskID domain.TaskID, err error) {
	if deps.Tasks == nil {
		return "", errors.New("tasks dependency is nil")
	}
	if req.CampaignID == "" {
		return "", errors.New("campaign id is required")
	}
	if req.JobID == "" {
		return "", errors.New("job id is required")
	}
	if req.DeviceID == "" {
		return "", errors.New("device id is required")
	}

	remoteImageDir := req.RemoteImageDir
	if remoteImageDir == "" {
		remoteImageDir = "/sdcard/DCIM/ppp_posts"
	}
	remoteImagePath := strings.TrimRight(remoteImageDir, "/") + "/" + req.CampaignID + "_" + req.JobID + ".jpg"

	// Push image to device (best effort; non-fatal).
	if deps.Adb != nil {
		if err := deps.Adb.PushFile(ctx, req.DeviceID, req.LocalImagePath, remoteImagePath); err != nil {
			if log != nil {
				log.Warn("adb push failed (continuing anyway)", "jobId", req.JobID, "err", err)
			}
		} else {
			if log != nil {
				log.Info("adb push succeeded", "jobId", req.JobID, "remotePath", remoteImagePath)
			}
			if err := deps.Adb.MediaScan(ctx, req.DeviceID, remoteImagePath); err != nil && log != nil {
				log.Warn("media scan failed", "jobId", req.JobID, "err", err)
			}
		}
	} else if log != nil {
		log.Warn("adb push skipped: ADB not configured", "jobId", req.JobID)
	}

	workflowName := req.WorkflowName
	if workflowName == "" {
		workflowName = "instagram-post-script"
	}

	goal := fmt.Sprintf("Post campaign %s job %s on device %s", req.CampaignID, req.JobID, req.DeviceID)
	if req.AccountID != "" {
		goal = fmt.Sprintf("Post to Instagram account %s on device %s", req.AccountID, req.DeviceID)
	}

	// Create task and wait until terminal.
	task, err := deps.Tasks.CreateTask(ctx, CreateTaskRequest{
		Goal:         goal,
		DeviceID:     req.DeviceID,
		WorkflowName: workflowName,
		InputArtifacts: map[string]string{
			"caption":   req.Caption,
			"imagePath": remoteImagePath,
		},
	})
	if err != nil {
		return "", fmt.Errorf("create task: %w", err)
	}

	summary, ok := PollTaskUntilTerminal(ctx, deps.Tasks, task.ID, 5*time.Minute, log)
	if !ok || summary == nil || summary.Task.Status != domain.TaskStatusCompleted {
		if summary != nil {
			return task.ID, fmt.Errorf("task %s: %s", summary.Task.ID, summary.Task.Status)
		}
		return task.ID, errors.New("post task failed")
	}

	return task.ID, nil
}
