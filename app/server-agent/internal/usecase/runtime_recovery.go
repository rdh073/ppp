package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
)

// RuntimeRecoveryUseCase reconciles persisted task and workflow state on
// process startup before the server begins accepting new agent traffic.
type RuntimeRecoveryUseCase struct {
	tasks  store.TaskStore
	states store.WorkflowStateStore
	queue  store.TaskQueue // optional; nil = no queue recovery
	log    *slog.Logger
}

type RuntimeRecoveryReport struct {
	TasksScanned       int
	StatesBootstrapped int
	TasksReconciled    int
	TasksRequeued      int
}

func NewRuntimeRecovery(
	tasks store.TaskStore,
	states store.WorkflowStateStore,
	log *slog.Logger,
) *RuntimeRecoveryUseCase {
	return &RuntimeRecoveryUseCase{
		tasks:  tasks,
		states: states,
		log:    log,
	}
}

// SetQueue wires the TaskQueue so that pending and paused tasks are re-enqueued
// on startup recovery, ready to be picked up when devices reconnect.
func (u *RuntimeRecoveryUseCase) SetQueue(q store.TaskQueue) {
	u.queue = q
}

func (u *RuntimeRecoveryUseCase) Recover(ctx context.Context) (RuntimeRecoveryReport, error) {
	report := RuntimeRecoveryReport{}

	tasks, err := u.tasks.List(ctx)
	if err != nil {
		return report, fmt.Errorf("list tasks: %w", err)
	}

	for _, task := range tasks {
		report.TasksScanned++
		if task.Status.IsTerminal() || task.AssignedDevice == "" {
			continue
		}

		state, err := u.states.Get(ctx, task.ID, task.AssignedDevice)
		if err != nil {
			if !errors.Is(err, store.ErrNotFound) {
				return report, fmt.Errorf("get workflow state for task %s: %w", task.ID, err)
			}

			bootstrap := domain.NewBootstrapWorkflowState(task, task.AssignedDevice)
			bootstrap.Inputs["recovery_bootstrap"] = "true"
			bootstrap.Inputs["recovery_bootstrap_reason"] = "startup_missing_checkpoint"
			if err := u.states.Save(ctx, bootstrap); err != nil {
				return report, fmt.Errorf("bootstrap workflow state for task %s: %w", task.ID, err)
			}
			report.StatesBootstrapped++
			u.log.Info("bootstrapped workflow state",
				"taskId", task.ID,
				"deviceId", task.AssignedDevice,
				"revision", bootstrap.Revision,
			)
			continue
		}

		if !state.IsTerminal() {
			continue
		}

		reconciledStatus := domain.TaskStatusCompleted
		if !state.TerminalSuccess {
			reconciledStatus = domain.TaskStatusFailed
		}
		if task.Status == reconciledStatus {
			continue
		}

		task.Status = reconciledStatus
		task.UpdatedAt = time.Now()
		if err := u.tasks.Save(ctx, task); err != nil {
			return report, fmt.Errorf("reconcile task %s status: %w", task.ID, err)
		}
		report.TasksReconciled++
		u.log.Info("reconciled terminal task status",
			"taskId", task.ID,
			"deviceId", task.AssignedDevice,
			"status", task.Status,
		)
	}

	// Re-enqueue tasks that have no device assignment so they are picked up
	// when a device connects. This covers both fresh pending tasks (created
	// before any device was available) and paused tasks (device went offline
	// before the previous run called OnDeviceOffline, e.g. a hard crash).
	if u.queue != nil {
		for _, task := range tasks {
			needsQueue := (task.Status == domain.TaskStatusPending || task.Status == domain.TaskStatusPaused) &&
				task.AssignedDevice == ""
			if !needsQueue {
				continue
			}
			if err := u.queue.Enqueue(ctx, task.ID); err != nil {
				u.log.Warn("re-enqueue task failed", "taskId", task.ID, "err", err)
				continue
			}
			report.TasksRequeued++
			u.log.Info("re-enqueued task on startup", "taskId", task.ID, "status", task.Status)
		}
	}

	return report, nil
}
