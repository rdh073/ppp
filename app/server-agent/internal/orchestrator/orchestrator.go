package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
	"github.com/autosdk/ppp/server-agent/internal/workflow"
)

// Orchestrator is the central event processor.
// ProcessEvent is the single entry point: it loads per-device workflow state,
// checks idempotency, runs the appropriate node, and checkpoints the result.
//
// Concurrency: ProcessEvent is safe for concurrent calls across different DeviceIDs.
// Calls for the same DeviceID are serialised by a per-device mutex to prevent
// concurrent node execution on the same workflow instance.
type Orchestrator struct {
	tasks  store.TaskStore
	states store.WorkflowStateStore
	runner *workflow.Runner
	wm     *watermarkTracker
	dedup  *dedupTracker
	log    *slog.Logger

	// deviceLocks provides per-device serialisation without a global lock.
	deviceLocks sync.Map // domain.DeviceID → *sync.Mutex
}

func New(
	tasks store.TaskStore,
	states store.WorkflowStateStore,
	runner *workflow.Runner,
	log *slog.Logger,
) *Orchestrator {
	return &Orchestrator{
		tasks:  tasks,
		states: states,
		runner: runner,
		wm:     newWatermarkTracker(),
		dedup:  newDedupTracker(),
		log:    log,
	}
}

// ProcessEvent drives the workflow for the device identified in e.DeviceID.
// It is idempotent: duplicate or stale events are silently dropped.
func (o *Orchestrator) ProcessEvent(ctx context.Context, e domain.Event) error {
	// --- idempotency checks (no lock needed; these are read-only) ---
	if !o.wm.Accept(e.DeviceID, e.SeqNo) {
		o.log.Debug("dropped stale event", "deviceId", e.DeviceID, "seqNo", e.SeqNo)
		return domain.ErrEventDropped
	}
	if e.ID != "" && o.dedup.IsDuplicate(e.DeviceID, e.ID) {
		o.log.Debug("dropped duplicate event", "deviceId", e.DeviceID, "eventId", e.ID)
		return domain.ErrEventDropped
	}

	// --- per-device serialisation ---
	mu := o.lockFor(e.DeviceID)
	mu.Lock()
	defer mu.Unlock()

	// Re-check after acquiring lock (another goroutine may have advanced the watermark).
	if !o.wm.Accept(e.DeviceID, e.SeqNo) {
		return domain.ErrEventDropped
	}
	if e.ID != "" && o.dedup.IsDuplicate(e.DeviceID, e.ID) {
		return domain.ErrEventDropped
	}

	tasks, err := o.tasks.ListByDevice(ctx, e.DeviceID)
	if err != nil {
		return fmt.Errorf("list tasks for device %s: %w", e.DeviceID, err)
	}

	// Process the event for each active task assigned to this device.
	for _, task := range tasks {
		if task.Status.IsTerminal() {
			continue
		}
		if err := o.processForTask(ctx, e, task); err != nil {
			o.log.Error("process event for task failed",
				"taskId", task.ID, "deviceId", e.DeviceID, "err", err)
		}
	}

	// Advance watermark and mark event as seen after successful processing.
	o.wm.Advance(e.DeviceID, e.SeqNo)
	if e.ID != "" {
		o.dedup.Mark(e.DeviceID, e.ID)
	}

	return nil
}

func (o *Orchestrator) processForTask(ctx context.Context, e domain.Event, task *domain.Task) error {
	state, err := o.states.Get(ctx, task.ID, e.DeviceID)
	if err != nil {
		// No state yet — bootstrap from Observe.
		state = domain.NewWorkflowState(task.ID, e.DeviceID)
	}

	if state.CurrentNode == domain.NodeKindTerminal {
		return nil
	}

	// If the workflow is suspended (WaitNode set WaitingFor), skip unless
	// the incoming event matches one of the expected kinds.
	if len(state.WaitingFor) > 0 && !eventMatchesWaitList(e.Kind, state.WaitingFor) {
		o.log.Debug("skipping event — workflow waiting for specific event",
			"taskId", task.ID, "deviceId", e.DeviceID,
			"event", e.Kind, "waitingFor", state.WaitingFor)
		return nil
	}

	input := workflow.NodeInput{Event: e, State: state, Task: task}
	newState, done, err := o.runner.Run(ctx, input)
	if err != nil {
		return fmt.Errorf("run node %s: %w", state.CurrentNode, err)
	}

	// Checkpoint before considering the node done.
	newState.UpdatedAt = time.Now()
	if err := o.states.Save(ctx, newState); err != nil {
		return fmt.Errorf("checkpoint workflow state: %w", err)
	}

	if done {
		reason := newState.Artifacts["terminal_reason"]
		o.log.Info("workflow terminal",
			"taskId", task.ID, "deviceId", e.DeviceID, "reason", reason)

		status := domain.TaskStatusCompleted
		if newState.Artifacts["goal_reached"] != "true" {
			status = domain.TaskStatusFailed
		}
		task.Status = status
		task.UpdatedAt = time.Now()
		_ = o.tasks.Save(ctx, task)
	} else {
		o.log.Debug("node transition",
			"taskId", task.ID,
			"from", state.CurrentNode,
			"to", newState.CurrentNode,
		)
	}

	return nil
}

func (o *Orchestrator) lockFor(deviceID domain.DeviceID) *sync.Mutex {
	v, _ := o.deviceLocks.LoadOrStore(deviceID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func eventMatchesWaitList(kind domain.EventKind, waitList []domain.EventKind) bool {
	for _, k := range waitList {
		if k == kind {
			return true
		}
	}
	return false
}
