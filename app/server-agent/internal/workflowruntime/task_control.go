package workflowruntime

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/registry"
	"github.com/autosdk/ppp/server-agent/internal/store"
)

// TaskControlUseCase manages task lifecycle via the HTTP API.
type TaskControlUseCase struct {
	tasks        store.TaskStore
	states       store.WorkflowStateStore
	outbox       store.CommandOutboxStore
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

// SetCommandOutbox wires command outbox lookup for task diagnostics.
func (u *TaskControlUseCase) SetCommandOutbox(outbox store.CommandOutboxStore) {
	u.outbox = outbox
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

type ListTaskQuery struct {
	Status       domain.TaskStatus // optional exact match
	DeviceID     domain.DeviceID   // optional exact match
	WorkflowName string            // optional exact match
	Limit        int               // optional; default 100, max 500
	Offset       int               // optional; default 0
}

type TaskSummary struct {
	Task              *domain.Task
	CurrentStep       string
	RetryCount        int
	LastCommandStatus domain.CommandOutboxStatus
	LastCommandError  string
	OutputArtifacts   map[string]string // populated for terminal tasks from WorkflowState.Inputs
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
			activeTasks, err := u.tasks.ListActiveByDevice(ctx, req.DeviceID)
			if err != nil {
				return nil, fmt.Errorf("check active task for device %s: %w", req.DeviceID, err)
			}
			for _, active := range activeTasks {
				if active.Status == domain.TaskStatusPaused {
					continue
				}
				return nil, fmt.Errorf("device %s has active task %s (%s)", req.DeviceID, active.ID, active.Status)
			}
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

func (u *TaskControlUseCase) GetTaskSummary(ctx context.Context, taskID domain.TaskID) (*TaskSummary, error) {
	task, err := u.tasks.Get(ctx, taskID)
	if err != nil {
		return nil, err
	}
	summaries, err := u.enrichTaskSummaries(ctx, []*domain.Task{task})
	if err != nil {
		return nil, err
	}
	if len(summaries) == 0 {
		return nil, fmt.Errorf("task %s not found", taskID)
	}
	return &summaries[0], nil
}

func (u *TaskControlUseCase) ListTasks(ctx context.Context, query ListTaskQuery) ([]TaskSummary, error) {
	tasks, err := u.tasks.List(ctx)
	if err != nil {
		return nil, err
	}

	filtered := make([]*domain.Task, 0, len(tasks))
	for _, task := range tasks {
		if task == nil {
			continue
		}
		if query.Status != "" && task.Status != query.Status {
			continue
		}
		if query.DeviceID != "" && task.AssignedDevice != query.DeviceID {
			continue
		}
		if query.WorkflowName != "" && !strings.EqualFold(task.WorkflowName, query.WorkflowName) {
			continue
		}
		filtered = append(filtered, task)
	}

	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].UpdatedAt.After(filtered[j].UpdatedAt)
	})

	limit := query.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}

	offset := query.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= len(filtered) {
		return []TaskSummary{}, nil
	}

	filtered = filtered[offset:]
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}

	return u.enrichTaskSummaries(ctx, filtered)
}

func (u *TaskControlUseCase) enrichTaskSummaries(ctx context.Context, tasks []*domain.Task) ([]TaskSummary, error) {
	summaries := make([]TaskSummary, 0, len(tasks))

	latestByTask := map[domain.TaskID]*domain.CommandOutboxRecord{}
	if u.outbox != nil {
		records, err := u.outbox.List(ctx)
		if err != nil {
			return nil, fmt.Errorf("list command outbox: %w", err)
		}
		for _, record := range records {
			if record == nil || record.Command.TaskID == "" {
				continue
			}
			prev, exists := latestByTask[record.Command.TaskID]
			if !exists || record.UpdatedAt.After(prev.UpdatedAt) {
				latestByTask[record.Command.TaskID] = record
			}
		}
	}

	for _, task := range tasks {
		summary := TaskSummary{Task: task}

		if task.AssignedDevice != "" {
			state, err := u.states.Get(ctx, task.ID, task.AssignedDevice)
			if err == nil && state != nil {
				summary.CurrentStep = state.CurrentStep
				summary.RetryCount = state.RetryCount
				if task.Status == domain.TaskStatusCompleted || task.Status == domain.TaskStatusFailed {
					summary.OutputArtifacts = state.Inputs
				}
			}
		}

		if record := latestByTask[task.ID]; record != nil {
			summary.LastCommandStatus = record.Status
			summary.LastCommandError = record.LastError
		}
		summaries = append(summaries, summary)
	}

	return summaries, nil
}
