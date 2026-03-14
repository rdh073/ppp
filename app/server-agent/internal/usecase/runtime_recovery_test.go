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
		InputArtifacts: map[string]string{"account.email": "ada@example.com"},
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
	if state.CurrentStep != "" {
		t.Fatalf("expected empty bootstrap step (resolves to entry), got %q", state.CurrentStep)
	}
	if state.Inputs["recovery_bootstrap"] != "true" {
		t.Fatalf("expected recovery_bootstrap in Inputs, got %#v", state.Inputs)
	}
	if state.Inputs["recovery_bootstrap_reason"] != "startup_missing_checkpoint" {
		t.Fatalf("expected recovery bootstrap reason, got %#v", state.Inputs)
	}
	if state.Inputs["account.email"] != "ada@example.com" {
		t.Fatalf("expected recovery bootstrap to seed inputArtifacts, got %#v", state.Inputs)
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
			state.CurrentStep = "terminal"
			state.TerminalSuccess = tc.goalReached == "true"
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
		InputArtifacts: map[string]string{"account.email": "ada@example.com"},
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

	reopenedTask, err := reopenedTasks.Get(ctx, task.ID)
	if err != nil {
		t.Fatalf("Get reopened task: %v", err)
	}
	if reopenedTask.InputArtifacts["account.email"] != "ada@example.com" {
		t.Fatalf("expected persisted task inputArtifacts after reopen, got %#v", reopenedTask.InputArtifacts)
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
	if got.Inputs["recovery_bootstrap"] != "true" {
		t.Fatalf("expected persisted recovery bootstrap in Inputs, got %#v", got.Inputs)
	}
	if got.Inputs["account.email"] != "ada@example.com" {
		t.Fatalf("expected persisted inputArtifacts in recovered state, got %#v", got.Inputs)
	}
	if got.Revision != 1 {
		t.Fatalf("expected persisted recovery revision 1, got %d", got.Revision)
	}
}
