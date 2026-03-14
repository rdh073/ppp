package usecase

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/registry"
	"github.com/autosdk/ppp/server-agent/internal/store"
)

// TaskControlUseCase manages task lifecycle via the HTTP API.
type TaskControlUseCase struct {
	tasks        store.TaskStore
	states       store.WorkflowStateStore
	orchestrator EventProcessor
	registry     registry.AgentRegistry
	assigner     *DeviceAssigner // optional; nil-safe
	log          *slog.Logger
}

func NewTaskControl(
	tasks store.TaskStore,
	states store.WorkflowStateStore,
	orch EventProcessor,
	reg registry.AgentRegistry,
	log *slog.Logger,
) *TaskControlUseCase {
	return &TaskControlUseCase{
		tasks:        tasks,
		states:       states,
		orchestrator: orch,
		registry:     reg,
		log:          log,
	}
}

// SetAssigner wires the DeviceAssigner so that tasks created without an explicit
// deviceId are automatically routed to an idle device or queued.
func (u *TaskControlUseCase) SetAssigner(a *DeviceAssigner) {
	u.assigner = a
}

// CreateTaskRequest specifies the task to create and which device to assign.
// DeviceID is optional; if empty the task is created pending assignment.
// WorkflowName is optional; if empty the server default workflow is used.
type CreateTaskRequest struct {
	Goal           string
	DeviceID       domain.DeviceID   // optional
	WorkflowName   string            // optional
	InputArtifacts map[string]string // optional
}

func (u *TaskControlUseCase) CreateTask(ctx context.Context, req CreateTaskRequest) (*domain.Task, error) {
	if req.Goal == "" {
		return nil, fmt.Errorf("goal is required")
	}

	task := &domain.Task{
		ID:             domain.NewTaskID(),
		Goal:           req.Goal,
		InputArtifacts: req.InputArtifacts,
		Status:         domain.TaskStatusPending,
		WorkflowName:   req.WorkflowName,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	// Assign device if provided explicitly.
	if req.DeviceID != "" {
		if _, _, ok := u.registry.GetByDevice(req.DeviceID); ok {
			task.AssignedDevice = req.DeviceID
			task.Status = domain.TaskStatusRunning
		} else {
			return nil, fmt.Errorf("device %s is not connected", req.DeviceID)
		}
	}

	if err := u.tasks.Save(ctx, task); err != nil {
		return nil, fmt.Errorf("save task: %w", err)
	}

	u.log.Info("task created", "taskId", task.ID, "deviceId", task.AssignedDevice)

	if task.AssignedDevice != "" {
		// Explicit assignment: bootstrap the workflow immediately.
		now := time.Now()
		event := domain.Event{
			ID:         fmt.Sprintf("%s:taskstart:%d", task.AssignedDevice, now.UnixNano()),
			Kind:       domain.EventKindAgentOnline,
			DeviceID:   task.AssignedDevice,
			OccurredAt: now,
		}
		if err := u.orchestrator.ProcessEvent(ctx, event); err != nil {
			u.log.Warn("bootstrap workflow failed", "taskId", task.ID, "err", err)
		}
		return task, nil
	}

	// No deviceId given: let the assigner route to an idle device or queue.
	if u.assigner != nil {
		if err := u.assigner.TryAssignTaskToIdleDevice(ctx, task); err != nil {
			u.log.Warn("auto-assign failed; task remains pending", "taskId", task.ID, "err", err)
		}
	}

	return task, nil
}

func (u *TaskControlUseCase) CancelTask(ctx context.Context, taskID domain.TaskID) error {
	task, err := u.tasks.Get(ctx, taskID)
	if err != nil {
		return fmt.Errorf("get task: %w", err)
	}
	if task.Status.IsTerminal() {
		return fmt.Errorf("task %s is already terminal (%s)", taskID, task.Status)
	}
	task.Status = domain.TaskStatusCancelled
	task.UpdatedAt = time.Now()
	if err := u.tasks.Save(ctx, task); err != nil {
		return err
	}
	// Remove from pending queue so it is not assigned to a device later.
	if u.assigner != nil {
		if err := u.assigner.RemoveFromQueue(ctx, taskID); err != nil {
			u.log.Warn("remove cancelled task from queue failed", "taskId", taskID, "err", err)
		}
	}
	return nil
}

func (u *TaskControlUseCase) GetTask(ctx context.Context, taskID domain.TaskID) (*domain.Task, error) {
	return u.tasks.Get(ctx, taskID)
}
