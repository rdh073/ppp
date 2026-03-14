package usecase_test

import (
	"context"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
	"github.com/autosdk/ppp/server-agent/internal/usecase"
)

func TestRuntimeRecovery_BootstrapsMissingWorkflowState(t *testing.T) {
	tasks := store.NewMemoryTaskStore()
	states := store.NewMemoryWorkflowStateStore()
	ctx := context.Background()

	task := &domain.Task{
		ID:             "task-recovery-bootstrap",
		Goal:           "resume later",
		Status:         domain.TaskStatusRunning,
		AssignedDevice: "dev-bootstrap",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	if err := tasks.Save(ctx, task); err != nil {
		t.Fatalf("Save task: %v", err)
	}

	uc := usecase.NewRuntimeRecovery(tasks, states, newLog())
	report, err := uc.Recover(ctx)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if report.TasksScanned != 1 || report.StatesBootstrapped != 1 || report.TasksReconciled != 0 {
		t.Fatalf("unexpected report: %+v", report)
	}

	state, err := states.Get(ctx, task.ID, task.AssignedDevice)
	if err != nil {
		t.Fatalf("Get state: %v", err)
	}
	if state.CurrentNode != domain.NodeKindObserve {
		t.Fatalf("expected observe bootstrap node, got %s", state.CurrentNode)
	}
	if state.Artifacts["recovery_bootstrap"] != "true" {
		t.Fatalf("expected recovery_bootstrap artifact, got %#v", state.Artifacts)
	}
	if state.Artifacts["recovery_bootstrap_reason"] != "startup_missing_checkpoint" {
		t.Fatalf("expected recovery bootstrap reason, got %#v", state.Artifacts)
	}
	if state.Revision != 1 {
		t.Fatalf("expected bootstrap revision 1, got %d", state.Revision)
	}
}

func TestRuntimeRecovery_ReconcilesTerminalTaskStatus(t *testing.T) {
	tests := []struct {
		name        string
		goalReached string
		wantStatus  domain.TaskStatus
	}{
		{name: "completed", goalReached: "true", wantStatus: domain.TaskStatusCompleted},
		{name: "failed", goalReached: "false", wantStatus: domain.TaskStatusFailed},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tasks := store.NewMemoryTaskStore()
			states := store.NewMemoryWorkflowStateStore()
			ctx := context.Background()

			task := &domain.Task{
				ID:             domain.TaskID("task-" + tc.name),
				Status:         domain.TaskStatusRunning,
				AssignedDevice: "dev-terminal",
				CreatedAt:      time.Now(),
				UpdatedAt:      time.Now(),
			}
			if err := tasks.Save(ctx, task); err != nil {
				t.Fatalf("Save task: %v", err)
			}

			state := domain.NewWorkflowState(task.ID, task.AssignedDevice)
			state.CurrentNode = domain.NodeKindTerminal
			state.Artifacts["goal_reached"] = tc.goalReached
			if err := states.Save(ctx, state); err != nil {
				t.Fatalf("Save state: %v", err)
			}

			uc := usecase.NewRuntimeRecovery(tasks, states, newLog())
			report, err := uc.Recover(ctx)
			if err != nil {
				t.Fatalf("Recover: %v", err)
			}
			if report.TasksReconciled != 1 {
				t.Fatalf("expected one reconciled task, got %+v", report)
			}

			got, err := tasks.Get(ctx, task.ID)
			if err != nil {
				t.Fatalf("Get task: %v", err)
			}
			if got.Status != tc.wantStatus {
				t.Fatalf("expected task status %s, got %s", tc.wantStatus, got.Status)
			}
		})
	}
}

func TestRuntimeRecovery_FileStoresAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	taskStore, err := store.NewFileTaskStore(dir)
	if err != nil {
		t.Fatalf("NewFileTaskStore: %v", err)
	}
	_, err = store.NewFileWorkflowStateStore(dir)
	if err != nil {
		t.Fatalf("NewFileWorkflowStateStore: %v", err)
	}

	task := &domain.Task{
		ID:             "task-reopen",
		Status:         domain.TaskStatusRunning,
		AssignedDevice: "dev-reopen",
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}
	if err := taskStore.Save(ctx, task); err != nil {
		t.Fatalf("Save task: %v", err)
	}

	reopenedTasks, err := store.NewFileTaskStore(dir)
	if err != nil {
		t.Fatalf("reopen task store: %v", err)
	}
	reopenedStates, err := store.NewFileWorkflowStateStore(dir)
	if err != nil {
		t.Fatalf("reopen state store: %v", err)
	}

	uc := usecase.NewRuntimeRecovery(reopenedTasks, reopenedStates, newLog())
	report, err := uc.Recover(ctx)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if report.StatesBootstrapped != 1 {
		t.Fatalf("expected one bootstrapped state, got %+v", report)
	}

	got, err := reopenedStates.Get(ctx, task.ID, task.AssignedDevice)
	if err != nil {
		t.Fatalf("Get state after recover: %v", err)
	}
	if got.Artifacts["recovery_bootstrap"] != "true" {
		t.Fatalf("expected persisted recovery bootstrap artifact, got %#v", got.Artifacts)
	}
	if got.Revision != 1 {
		t.Fatalf("expected persisted recovery revision 1, got %d", got.Revision)
	}
}
