package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
	"github.com/autosdk/ppp/server-agent/internal/telemetry"
	"github.com/autosdk/ppp/server-agent/internal/workflow"
)

// Orchestrator is the central event processor.
// ProcessEvent is the single entry point: it durably accepts the event, loads
// per-device workflow state, runs the appropriate node, and checkpoints the
// result.
//
// Concurrency: ProcessEvent is safe for concurrent calls across different
// DeviceIDs. Calls for the same DeviceID are serialised by a per-device mutex
// to prevent concurrent node execution on the same workflow instance.
type Orchestrator struct {
	tasks   store.TaskStore
	states  store.WorkflowStateStore
	engine  *workflow.Engine
	events  store.EventPlaneStore
	log     *slog.Logger
	metrics *telemetry.Registry

	// deviceLocks provides per-device serialisation without a global lock.
	deviceLocks sync.Map // domain.DeviceID → *sync.Mutex

	watchdog *DeadlineWatchdog // optional; nil-safe

	// onTaskTerminal is called asynchronously after a task reaches a terminal state.
	// Injected by main.go via SetOnTaskTerminal to avoid an import cycle.
	onTaskTerminal func(ctx context.Context, task *domain.Task)
}

func New(
	tasks store.TaskStore,
	states store.WorkflowStateStore,
	engine *workflow.Engine,
	log *slog.Logger,
	eventStores ...store.EventPlaneStore,
) *Orchestrator {
	eventStore := store.EventPlaneStore(store.NewMemoryEventPlaneStore())
	if len(eventStores) > 0 && eventStores[0] != nil {
		eventStore = eventStores[0]
	}
	return &Orchestrator{
		tasks:  tasks,
		states: states,
		engine: engine,
		events: eventStore,
		log:    log,
	}
}

// ProcessEvent drives the workflow for the device identified in e.DeviceID.
// It is idempotent: duplicate or stale events are durably recorded and then
// dropped before any side-effecting workflow execution occurs.
func (o *Orchestrator) ProcessEvent(ctx context.Context, e domain.Event) error {
	status, err := o.events.Accept(ctx, e)
	if err != nil {
		return fmt.Errorf("accept event %s: %w", e.ID, err)
	}
	switch status {
	case domain.EventAcceptanceStale:
		o.log.Debug("dropped stale event", "deviceId", e.DeviceID, "seqNo", e.SeqNo)
		return domain.ErrEventDropped
	case domain.EventAcceptanceDuplicate:
		o.log.Debug("dropped duplicate event", "deviceId", e.DeviceID, "eventId", e.ID)
		return domain.ErrEventDropped
	}

	return o.ProcessAcceptedEvent(ctx, e)
}

// ProcessAcceptedEvent drives workflow execution for an event that has already
// passed acceptance, watermark, and dedup checks.
func (o *Orchestrator) ProcessAcceptedEvent(ctx context.Context, e domain.Event) error {
	mu := o.lockFor(e.DeviceID)
	mu.Lock()
	defer mu.Unlock()

	if o.metrics != nil {
		o.metrics.EnterDeviceLane()
		defer o.metrics.LeaveDeviceLane()
	}
	if o.metrics != nil && !e.OccurredAt.IsZero() {
		source := telemetry.IngestSourceInternal
		if e.Kind.IsDeviceOriginated() {
			source = telemetry.IngestSourceDevice
		}
		o.metrics.ObserveIngestLag(source, time.Since(e.OccurredAt))
	}

	tasks, err := o.tasks.ListActiveByDevice(ctx, e.DeviceID)
	if err != nil {
		o.recordDeadLetter(ctx, domain.NewDeadLetterRecord(&e, nil, fmt.Sprintf("list tasks: %v", err), "orchestrator"))
		return fmt.Errorf("list tasks for device %s: %w", e.DeviceID, err)
	}

	if err := o.processTasksForEvent(ctx, e, tasks); err != nil {
		o.recordDeadLetter(ctx, domain.NewDeadLetterRecord(&e, nil, err.Error(), "orchestrator"))
		return err
	}
	return nil
}

func (o *Orchestrator) processTasksForEvent(ctx context.Context, e domain.Event, tasks []*domain.Task) error {
	var firstErr error
	for _, task := range tasks {
		if err := o.processForTask(ctx, e, task); err != nil {
			o.log.Error("process event for task failed",
				"taskId", task.ID, "deviceId", e.DeviceID, "err", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (o *Orchestrator) RecordDeadLetter(ctx context.Context, record domain.DeadLetterRecord) error {
	return o.events.RecordDeadLetter(ctx, record)
}

func (o *Orchestrator) SetOperationalMetrics(metrics *telemetry.Registry) {
	o.metrics = metrics
}

// SetDeadlineWatchdog wires a DeadlineWatchdog into the orchestrator.
// Must be called before the orchestrator starts processing events.
func (o *Orchestrator) SetDeadlineWatchdog(w *DeadlineWatchdog) {
	o.watchdog = w
}

// SetOnTaskTerminal registers a callback that is invoked asynchronously whenever
// a task reaches a terminal state. Used by the assignment engine to free up the
// device slot and assign the next queued task. Must be called before the server
// starts accepting traffic.
func (o *Orchestrator) SetOnTaskTerminal(f func(ctx context.Context, task *domain.Task)) {
	o.onTaskTerminal = f
}

func (o *Orchestrator) processForTask(ctx context.Context, e domain.Event, task *domain.Task) error {
	state, err := o.loadState(ctx, task, e.DeviceID)
	if err != nil {
		return err
	}
	if state.IsTerminal() {
		return nil
	}

	result, err := o.engine.Handle(ctx, workflow.EngineCommand{
		State: state, WorkflowName: task.WorkflowName, Task: task, Event: e,
	})
	if err != nil {
		return o.handleEngineError(ctx, task, err)
	}
	if result.State == nil {
		return nil // event not relevant to current step
	}

	if err := o.checkpoint(ctx, result.State); err != nil {
		return err
	}
	o.syncWatchdog(task.ID, result.State)

	if result.Terminal {
		return o.finalizeTask(ctx, task, result.State, e.DeviceID)
	}
	return nil
}

func (o *Orchestrator) loadState(ctx context.Context, task *domain.Task, deviceID domain.DeviceID) (*domain.WorkflowState, error) {
	state, err := o.states.Get(ctx, task.ID, deviceID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return domain.NewBootstrapWorkflowState(task, deviceID), nil
		}
		return nil, fmt.Errorf("load workflow state: %w", err)
	}
	return state, nil
}

func (o *Orchestrator) handleEngineError(ctx context.Context, task *domain.Task, err error) error {
	if errors.Is(err, workflow.ErrWorkflowDefNotFound) {
		// Workflow def is unresolvable — permanently fail the task so subsequent
		// device events don't keep retrying an unbounded bootstrap loop.
		task.Status = domain.TaskStatusFailed
		task.UpdatedAt = time.Now()
		if saveErr := o.tasks.Save(ctx, task); saveErr != nil {
			o.log.Error("save failed task after bootstrap error", "taskId", task.ID, "err", saveErr)
		} else if o.onTaskTerminal != nil {
			go o.onTaskTerminal(context.Background(), task)
		}
	}
	return fmt.Errorf("engine: %w", err)
}

func (o *Orchestrator) checkpoint(ctx context.Context, state *domain.WorkflowState) error {
	state.UpdatedAt = time.Now()
	if err := o.states.Save(ctx, state); err != nil {
		return fmt.Errorf("checkpoint: %w", err)
	}
	return nil
}

func (o *Orchestrator) syncWatchdog(taskID domain.TaskID, state *domain.WorkflowState) {
	if o.watchdog == nil {
		return
	}
	if state.WaitingExpect != nil {
		o.watchdog.Track(taskID, state.DeviceID, state.DeadlineAt)
	} else if state.RetryCount > 0 {
		// Retry-pending: action failed but budget remains. Track with a short
		// deadline so the watchdog fires a tick that re-executes the action
		// within one tick interval instead of waiting for the next device event.
		o.watchdog.Track(taskID, state.DeviceID, time.Now().Add(o.watchdog.tickInterval))
	} else {
		o.watchdog.Untrack(taskID)
	}
}

func (o *Orchestrator) finalizeTask(ctx context.Context, task *domain.Task, state *domain.WorkflowState, deviceID domain.DeviceID) error {
	status := domain.TaskStatusCompleted
	if !state.TerminalSuccess {
		status = domain.TaskStatusFailed
	}
	task.Status = status
	task.UpdatedAt = time.Now()
	if err := o.tasks.Save(ctx, task); err != nil {
		return fmt.Errorf("save terminal task: %w", err)
	}
	o.log.Info("workflow terminal",
		"taskId", task.ID,
		"deviceId", deviceID,
		"success", state.TerminalSuccess,
	)
	if o.onTaskTerminal != nil {
		// Run async so the terminal callback (e.g. device re-assignment)
		// does not block or deadlock the current orchestrator lane.
		go o.onTaskTerminal(context.Background(), task)
	}
	return nil
}

func (o *Orchestrator) lockFor(deviceID domain.DeviceID) *sync.Mutex {
	v, _ := o.deviceLocks.LoadOrStore(deviceID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func (o *Orchestrator) recordDeadLetter(ctx context.Context, record domain.DeadLetterRecord) {
	if err := o.events.RecordDeadLetter(ctx, record); err != nil {
		o.log.Error("record dead letter failed", "eventId", record.EventID, "err", err)
	}
}
