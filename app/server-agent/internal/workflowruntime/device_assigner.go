package workflowruntime

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/registry"
	"github.com/autosdk/ppp/server-agent/internal/store"
)

// DeviceAssigner is the routing layer between pending tasks and idle devices.
//
// It answers two symmetric questions:
//   - A device just came online — is there a pending task for it?
//   - A task was just created — is there an idle device for it?
//
// All public methods are safe for concurrent calls. A single mutex serialises
// the find-idle-device → assign critical section so two tasks cannot race to
// the same device slot.
type DeviceAssigner struct {
	tasks    store.TaskStore
	queue    store.TaskQueue
	registry registry.AgentRegistry
	runtime  EventProcessor
	log      *slog.Logger
	mu       sync.Mutex
}

func NewDeviceAssigner(
	tasks store.TaskStore,
	queue store.TaskQueue,
	reg registry.AgentRegistry,
	runtime EventProcessor,
	log *slog.Logger,
) *DeviceAssigner {
	return &DeviceAssigner{
		tasks:    tasks,
		queue:    queue,
		registry: reg,
		runtime:  runtime,
		log:      log,
	}
}

// TryAssignPendingToDevice checks whether deviceID is idle and, if so, dequeues
// the next pending task and assigns it. Called on agent.hello / agent.resume.
func (a *DeviceAssigner) TryAssignPendingToDevice(ctx context.Context, deviceID domain.DeviceID) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if busy, err := a.deviceBusy(ctx, deviceID); err != nil {
		return fmt.Errorf("check device busy: %w", err)
	} else if busy {
		return nil // already has a running task; existing orchestrator logic handles it
	}

	task, ok, err := a.dequeueValidTask(ctx)
	if err != nil || !ok {
		return err
	}

	return a.assignAndBootstrap(ctx, task, deviceID)
}

// TryAssignTaskToIdleDevice finds an idle connected device and assigns the task
// to it. If no idle device is available the task is enqueued for later.
// Called immediately after CreateTask when no explicit deviceID was given.
func (a *DeviceAssigner) TryAssignTaskToIdleDevice(ctx context.Context, task *domain.Task) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	deviceID, ok := a.findIdleDevice(ctx)
	if !ok {
		// No idle device; park the task in the queue.
		if err := a.queue.Enqueue(ctx, task.ID); err != nil {
			return fmt.Errorf("enqueue task %s: %w", task.ID, err)
		}
		a.log.Info("task queued (no idle device)", "taskId", task.ID)
		return nil
	}

	return a.assignAndBootstrap(ctx, task, deviceID)
}

// OnDeviceOffline marks all running/paused tasks for deviceID as paused and
// re-enqueues them so another device can pick them up.
// Called on agent.disconnect and WebSocket close.
func (a *DeviceAssigner) OnDeviceOffline(ctx context.Context, deviceID domain.DeviceID) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	tasks, err := a.tasks.ListByDevice(ctx, deviceID)
	if err != nil {
		return fmt.Errorf("list tasks for device %s: %w", deviceID, err)
	}

	for _, task := range tasks {
		if task.Status.IsTerminal() {
			continue
		}
		task.Status = domain.TaskStatusPaused
		task.AssignedDevice = ""
		task.UpdatedAt = time.Now()
		if err := a.tasks.Save(ctx, task); err != nil {
			a.log.Error("pause task on disconnect failed", "taskId", task.ID, "err", err)
			continue
		}
		if err := a.queue.Enqueue(ctx, task.ID); err != nil {
			a.log.Error("re-enqueue paused task failed", "taskId", task.ID, "err", err)
		} else {
			a.log.Info("task re-queued after device offline", "taskId", task.ID, "deviceId", deviceID)
		}
	}
	return nil
}

// RemoveFromQueue removes a task from the pending queue. Call this when a task
// is cancelled so it is not assigned to a device later.
func (a *DeviceAssigner) RemoveFromQueue(ctx context.Context, taskID domain.TaskID) error {
	return a.queue.Remove(ctx, taskID)
}

// OnTaskTerminal is called (asynchronously) by the orchestrator after a task
// reaches a terminal state. It tries to assign the next queued task to the
// device that just became free.
func (a *DeviceAssigner) OnTaskTerminal(ctx context.Context, task *domain.Task) {
	if task.AssignedDevice == "" {
		return
	}
	if err := a.TryAssignPendingToDevice(ctx, task.AssignedDevice); err != nil {
		a.log.Warn("assign next task after terminal failed",
			"deviceId", task.AssignedDevice, "err", err)
	}
}

// --- private helpers ---

// deviceBusy returns true if the device has at least one non-terminal,
// non-paused task assigned. Must be called with a.mu held.
func (a *DeviceAssigner) deviceBusy(ctx context.Context, deviceID domain.DeviceID) (bool, error) {
	tasks, err := a.tasks.ListByDevice(ctx, deviceID)
	if err != nil {
		return false, err
	}
	for _, t := range tasks {
		if !t.Status.IsTerminal() && t.Status != domain.TaskStatusPaused {
			return true, nil
		}
	}
	return false, nil
}

// findIdleDevice returns the first connected device that has no running task.
// Must be called with a.mu held.
func (a *DeviceAssigner) findIdleDevice(ctx context.Context) (domain.DeviceID, bool) {
	sessions := a.registry.ListAll()
	for _, s := range sessions {
		busy, err := a.deviceBusy(ctx, s.DeviceID)
		if err != nil || busy {
			continue
		}
		return s.DeviceID, true
	}
	return "", false
}

// dequeueValidTask pops the next task ID from the queue and loads it, skipping
// IDs whose tasks are no longer pending or paused (e.g. cancelled while queued).
// Must be called with a.mu held.
func (a *DeviceAssigner) dequeueValidTask(ctx context.Context) (*domain.Task, bool, error) {
	for {
		taskID, ok, err := a.queue.Dequeue(ctx)
		if err != nil {
			return nil, false, fmt.Errorf("dequeue: %w", err)
		}
		if !ok {
			return nil, false, nil // queue empty
		}

		task, err := a.tasks.Get(ctx, taskID)
		if err != nil {
			a.log.Warn("task from queue not found; skipping", "taskId", taskID, "err", err)
			continue
		}
		if task.Status != domain.TaskStatusPending && task.Status != domain.TaskStatusPaused {
			a.log.Debug("skipping non-assignable task from queue",
				"taskId", taskID, "status", task.Status)
			continue
		}
		return task, true, nil
	}
}

// assignAndBootstrap writes the assignment to the store and emits the synthetic
// agent.online event that wakes the orchestrator for this task.
// Must be called with a.mu held.
func (a *DeviceAssigner) assignAndBootstrap(ctx context.Context, task *domain.Task, deviceID domain.DeviceID) error {
	task.AssignedDevice = deviceID
	task.Status = domain.TaskStatusRunning
	task.UpdatedAt = time.Now()
	if err := a.tasks.Save(ctx, task); err != nil {
		// Roll back: put task back in the queue so it is not lost.
		_ = a.queue.Enqueue(ctx, task.ID)
		return fmt.Errorf("save task assignment: %w", err)
	}

	a.log.Info("task assigned to device", "taskId", task.ID, "deviceId", deviceID)

	now := time.Now()
	event := domain.Event{
		ID:         fmt.Sprintf("%s:taskstart:%d", deviceID, now.UnixNano()),
		Kind:       domain.EventKindAgentOnline,
		DeviceID:   deviceID,
		OccurredAt: now,
	}
	// Run ProcessEvent in a goroutine to avoid deadlocking when assignAndBootstrap
	// is called from within the WebSocket read-loop goroutine (e.g. during Hello).
	// The read loop must remain free to deliver the device.execute response that
	// the workflow engine will dispatch synchronously inside ProcessEvent.
	// Non-fatal: if this fires before the connection is fully ready the orchestrator
	// will retry on the next device event (heartbeat, reconnect, etc.).
	taskID := task.ID
	go func() {
		if err := a.runtime.ProcessEvent(context.Background(), event); err != nil {
			a.log.Warn("bootstrap workflow after assignment failed",
				"taskId", taskID, "deviceId", deviceID, "err", err)
		}
	}()
	return nil
}
