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

const maxAutoAdvanceSteps = 32

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
	runner  *workflow.Runner
	events  store.EventPlaneStore
	log     *slog.Logger
	bus     emittedEventPublisher
	metrics *telemetry.Registry

	// deviceLocks provides per-device serialisation without a global lock.
	deviceLocks sync.Map // domain.DeviceID → *sync.Mutex
}

type emittedEventPublisher interface {
	PublishAccepted(ctx context.Context, record domain.AcceptedEventRecord) error
	PublishWakeup(ctx context.Context, event domain.Event) error
}

func New(
	tasks store.TaskStore,
	states store.WorkflowStateStore,
	runner *workflow.Runner,
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
		runner: runner,
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

	tasks, err := o.tasks.ListByDevice(ctx, e.DeviceID)
	if err != nil {
		o.recordDeadLetter(ctx, domain.NewDeadLetterRecord(&e, nil, fmt.Sprintf("list tasks: %v", err), "orchestrator"))
		return fmt.Errorf("list tasks for device %s: %w", e.DeviceID, err)
	}

	var processErr error
	for _, task := range tasks {
		if task.Status.IsTerminal() {
			continue
		}
		if err := o.processForTask(ctx, e, task); err != nil {
			o.log.Error("process event for task failed",
				"taskId", task.ID, "deviceId", e.DeviceID, "err", err)
			if processErr == nil {
				processErr = err
			}
		}
	}
	if processErr != nil {
		o.recordDeadLetter(ctx, domain.NewDeadLetterRecord(&e, nil, processErr.Error(), "orchestrator"))
		return processErr
	}

	return nil
}

func (o *Orchestrator) RecordDeadLetter(ctx context.Context, record domain.DeadLetterRecord) error {
	return o.events.RecordDeadLetter(ctx, record)
}

func (o *Orchestrator) SetEmittedEventPublisher(bus emittedEventPublisher) {
	o.bus = bus
}

func (o *Orchestrator) SetOperationalMetrics(metrics *telemetry.Registry) {
	o.metrics = metrics
}

func (o *Orchestrator) processForTask(ctx context.Context, e domain.Event, task *domain.Task) error {
	state, err := o.states.Get(ctx, task.ID, e.DeviceID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// No state yet — bootstrap from Observe.
			state = domain.NewBootstrapWorkflowState(task, e.DeviceID)
		} else {
			return fmt.Errorf("load workflow state: %w", err)
		}
	}

	if state.CurrentNode == domain.NodeKindTerminal {
		return nil
	}

	currentState := state
	currentEvent := e
	var pendingEvents []domain.Event
	for step := 0; step < maxAutoAdvanceSteps; step++ {
		// If the workflow is suspended (WaitNode set WaitingFor), skip unless
		// the current event matches one of the expected kinds. Inline-emitted
		// events are drained in-order before we return to the caller.
		if len(currentState.WaitingFor) > 0 && !eventMatchesWaitList(currentEvent.Kind, currentState.WaitingFor) {
			o.log.Debug("skipping event — workflow waiting for specific event",
				"taskId", task.ID, "deviceId", e.DeviceID,
				"event", currentEvent.Kind, "waitingFor", currentState.WaitingFor)
			if len(pendingEvents) == 0 {
				return nil
			}
			currentEvent, pendingEvents = pendingEvents[0], pendingEvents[1:]
			continue
		}

		input := workflow.NodeInput{Event: currentEvent, State: currentState, Task: task}
		newState, done, emittedEvents, err := o.runner.Run(ctx, input)
		if err != nil {
			return fmt.Errorf("run node %s: %w", currentState.CurrentNode, err)
		}

		// Checkpoint before considering the node done.
		newState.UpdatedAt = time.Now()
		if err := o.states.Save(ctx, newState); err != nil {
			return fmt.Errorf("checkpoint workflow state: %w", err)
		}

		if len(emittedEvents) > 0 {
			inlineEvents, continueInline, err := o.acceptEmittedEvents(ctx, currentState.DeviceID, emittedEvents)
			if err != nil {
				return err
			}
			if !continueInline {
				return nil
			}
			pendingEvents = append(pendingEvents, inlineEvents...)
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
			if err := o.tasks.Save(ctx, task); err != nil {
				return fmt.Errorf("save terminal task status: %w", err)
			}
			return nil
		}

		o.log.Debug("node transition",
			"taskId", task.ID,
			"from", currentState.CurrentNode,
			"to", newState.CurrentNode,
			"eventKind", currentEvent.Kind,
		)

		currentState = newState
		if len(pendingEvents) > 0 {
			currentEvent, pendingEvents = pendingEvents[0], pendingEvents[1:]
			continue
		}
		if len(newState.WaitingFor) > 0 || !isAutoAdvanceNode(newState.CurrentNode) {
			return nil
		}
	}

	task.Status = domain.TaskStatusFailed
	task.UpdatedAt = time.Now()
	if saveErr := o.tasks.Save(ctx, task); saveErr != nil {
		o.log.Error("save failed task after auto-advance exceeded", "taskId", task.ID, "err", saveErr)
	}
	return fmt.Errorf("workflow auto-advance exceeded %d steps for task %s", maxAutoAdvanceSteps, task.ID)
}

func (o *Orchestrator) acceptEmittedEvents(
	ctx context.Context,
	expectedDeviceID domain.DeviceID,
	events []domain.Event,
) ([]domain.Event, bool, error) {
	inlineEvents := make([]domain.Event, 0, len(events))
	wakeupPublishCount := 0
	busActive := o.bus != nil

	for _, emitted := range events {
		if emitted.DeviceID == "" {
			return nil, false, fmt.Errorf("emitted event %s missing device id", emitted.ID)
		}
		if emitted.DeviceID != expectedDeviceID {
			return nil, false, fmt.Errorf(
				"emitted event %s targets device %s, expected %s",
				emitted.ID, emitted.DeviceID, expectedDeviceID,
			)
		}

		status, err := o.events.Accept(ctx, emitted)
		if err != nil {
			o.recordDeadLetter(ctx, domain.NewDeadLetterRecord(&emitted, nil, fmt.Sprintf("accept emitted event: %v", err), "orchestrator"))
			return nil, false, fmt.Errorf("accept emitted event %s: %w", emitted.ID, err)
		}
		if status != domain.EventAcceptanceAccepted {
			o.recordDeadLetter(ctx, domain.NewDeadLetterRecord(&emitted, nil, fmt.Sprintf("unexpected emitted event status: %s", status), "orchestrator"))
			return nil, false, fmt.Errorf("unexpected emitted event status %s for %s", status, emitted.ID)
		}

		if busActive {
			record := domain.AcceptedEventRecord{
				Event:      emitted,
				AcceptedAt: time.Now(),
				Source:     "internal",
			}
			if err := o.bus.PublishAccepted(ctx, record); err != nil {
				o.log.Warn("publish accepted emitted event failed; continuing with durable store as source of truth",
					"eventId", emitted.ID,
					"deviceId", emitted.DeviceID,
					"err", err,
				)
			}
			if err := o.bus.PublishWakeup(ctx, emitted); err != nil {
				o.log.Warn("publish wakeup emitted event failed; evaluating inline fallback",
					"eventId", emitted.ID,
					"deviceId", emitted.DeviceID,
					"err", err,
				)
				if wakeupPublishCount > 0 {
					return nil, false, fmt.Errorf(
						"publish wakeup emitted event %s after %d prior wakeups: %w",
						emitted.ID,
						wakeupPublishCount,
						err,
					)
				}
				if o.metrics != nil {
					o.metrics.RecordWakeupFallback(telemetry.WakeupFallbackEmittedBatch)
				}
				busActive = false
				inlineEvents = append(inlineEvents, emitted)
				continue
			}
			wakeupPublishCount++
			continue
		}
		inlineEvents = append(inlineEvents, emitted)
	}

	if o.bus != nil && busActive {
		return nil, false, nil
	}
	return inlineEvents, true, nil
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

func eventMatchesWaitList(kind domain.EventKind, waitList []domain.EventKind) bool {
	for _, k := range waitList {
		if k == kind {
			return true
		}
	}
	return false
}

func isAutoAdvanceNode(kind domain.NodeKind) bool {
	switch kind {
	case domain.NodeKindDecide, domain.NodeKindToolCall, domain.NodeKindVerify, domain.NodeKindTerminal:
		return true
	default:
		return false
	}
}
