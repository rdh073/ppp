package appport

import (
	"context"
	"log/slog"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// TaskSummaryGetter is a minimal interface for polling — a subset of TaskControl.
// Exported so that test mocks can reference the exact parameter type without
// importing the full TaskControl interface.
type TaskSummaryGetter interface {
	GetTaskSummary(ctx context.Context, taskID domain.TaskID) (*TaskSummary, error)
}

// PollTaskUntilTerminal polls a task every 3 seconds until it reaches a terminal
// status or the timeout/context expires. Returns the final summary and true if
// the task terminated, or nil and false on timeout/cancellation.
func PollTaskUntilTerminal(ctx context.Context, tasks TaskSummaryGetter, taskID domain.TaskID, timeout time.Duration, log *slog.Logger) (*TaskSummary, bool) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, false
		case <-ticker.C:
			if time.Now().After(deadline) {
				return nil, false
			}
			summary, err := tasks.GetTaskSummary(ctx, taskID)
			if err != nil {
				if log != nil {
					log.Warn("poll task error", "taskId", taskID, "err", err)
				}
				continue
			}
			if summary.Task.Status.IsTerminal() {
				return summary, true
			}
		}
	}
}
