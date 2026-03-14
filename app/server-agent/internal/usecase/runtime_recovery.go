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
	log    *slog.Logger
}

type RuntimeRecoveryReport struct {
	TasksScanned       int
	StatesBootstrapped int
	TasksReconciled    int
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

			bootstrap := domain.NewWorkflowState(task.ID, task.AssignedDevice)
			bootstrap.Artifacts["recovery_bootstrap"] = "true"
			bootstrap.Artifacts["recovery_bootstrap_reason"] = "startup_missing_checkpoint"
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

		if state.CurrentNode != domain.NodeKindTerminal {
			continue
		}

		reconciledStatus := domain.TaskStatusFailed
		if state.Artifacts["goal_reached"] == "true" {
			reconciledStatus = domain.TaskStatusCompleted
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

	return report, nil
}
