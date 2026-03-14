package orchestrator

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

type deadlineEntry struct {
	DeviceID domain.DeviceID
	TaskID   domain.TaskID
	Deadline time.Time
}

// DeadlineWatchdog tracks steps suspended on WaitingExpect and proactively
// injects a synthetic workflow.tick event when the deadline expires.
//
// This makes timeout enforcement independent of the next device-originated
// event (e.g. heartbeat every 30 s), so a 5 s step timeout fires within
// one tickInterval of expiry.
type DeadlineWatchdog struct {
	mu           sync.Mutex
	entries      map[domain.TaskID]deadlineEntry
	inject       func(ctx context.Context, e domain.Event) error
	tickInterval time.Duration
}

// NewDeadlineWatchdog creates a watchdog. inject is called with a synthetic
// workflow.tick event for each expired entry; typically orch.ProcessAcceptedEvent.
func NewDeadlineWatchdog(inject func(ctx context.Context, e domain.Event) error, tickInterval time.Duration) *DeadlineWatchdog {
	return &DeadlineWatchdog{
		entries:      make(map[domain.TaskID]deadlineEntry),
		inject:       inject,
		tickInterval: tickInterval,
	}
}

// Track registers or replaces a deadline for the given task.
// Called after the engine arms a WaitingExpect.
func (w *DeadlineWatchdog) Track(taskID domain.TaskID, deviceID domain.DeviceID, deadline time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.entries[taskID] = deadlineEntry{DeviceID: deviceID, TaskID: taskID, Deadline: deadline}
}

// Untrack removes a task from watchdog tracking.
// Called when WaitingExpect is cleared (step advanced or failed).
func (w *DeadlineWatchdog) Untrack(taskID domain.TaskID) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.entries, taskID)
}

// Run starts the watchdog loop. Blocks until ctx is cancelled.
func (w *DeadlineWatchdog) Run(ctx context.Context) {
	ticker := time.NewTicker(w.tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.tick(ctx)
		}
	}
}

func (w *DeadlineWatchdog) tick(ctx context.Context) {
	now := time.Now()
	w.mu.Lock()
	var expired []deadlineEntry
	for taskID, entry := range w.entries {
		if now.After(entry.Deadline) {
			expired = append(expired, entry)
			delete(w.entries, taskID)
		}
	}
	w.mu.Unlock()

	for _, entry := range expired {
		e := domain.Event{
			ID:         "tick-" + string(entry.TaskID) + "-" + strconv.FormatInt(now.UnixNano(), 36),
			Kind:       domain.EventKindWorkflowTick,
			DeviceID:   entry.DeviceID,
			OccurredAt: now,
		}
		// Tick events bypass the event plane store; errors are best-effort.
		_ = w.inject(ctx, e)
	}
}
