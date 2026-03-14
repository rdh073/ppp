package orchestrator_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/orchestrator"
)

// collectingInject collects injected events for test assertions.
type collectingInject struct {
	mu     sync.Mutex
	events []domain.Event
}

func (c *collectingInject) inject(_ context.Context, e domain.Event) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
	return nil
}

func (c *collectingInject) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.events)
}

func (c *collectingInject) kinds() []domain.EventKind {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]domain.EventKind, len(c.events))
	for i, e := range c.events {
		out[i] = e.Kind
	}
	return out
}

// TestWatchdog_ExpiredEntry_Fires verifies that a tracked entry whose deadline
// has passed triggers an inject call on the next tick.
func TestWatchdog_ExpiredEntry_Fires(t *testing.T) {
	col := &collectingInject{}
	w := orchestrator.NewDeadlineWatchdog(col.inject, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	taskID := domain.TaskID("task-1")
	deviceID := domain.DeviceID("dev-1")
	// Deadline already in the past.
	w.Track(taskID, deviceID, time.Now().Add(-1*time.Second))

	// Wait for one tick to fire.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if col.count() > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	if col.count() == 0 {
		t.Fatal("expected inject to be called for expired entry")
	}
	if kinds := col.kinds(); kinds[0] != domain.EventKindWorkflowTick {
		t.Errorf("expected workflow.tick kind, got %q", kinds[0])
	}
}

// TestWatchdog_Untrack_Suppresses verifies that an untracked entry is never injected.
func TestWatchdog_Untrack_Suppresses(t *testing.T) {
	col := &collectingInject{}
	w := orchestrator.NewDeadlineWatchdog(col.inject, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	taskID := domain.TaskID("task-untrack")
	deviceID := domain.DeviceID("dev-1")
	w.Track(taskID, deviceID, time.Now().Add(-1*time.Second))
	w.Untrack(taskID) // remove before tick fires

	time.Sleep(50 * time.Millisecond)
	if col.count() != 0 {
		t.Errorf("expected no inject after Untrack, got %d", col.count())
	}
}

// TestWatchdog_NonExpired_NotFired verifies that a tracked entry whose deadline
// is in the future is not injected.
func TestWatchdog_NonExpired_NotFired(t *testing.T) {
	col := &collectingInject{}
	w := orchestrator.NewDeadlineWatchdog(col.inject, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	taskID := domain.TaskID("task-future")
	deviceID := domain.DeviceID("dev-1")
	// Deadline 10 seconds in the future.
	w.Track(taskID, deviceID, time.Now().Add(10*time.Second))

	time.Sleep(50 * time.Millisecond)
	if col.count() != 0 {
		t.Errorf("expected no inject for non-expired entry, got %d", col.count())
	}
}

// TestWatchdog_ExpiredEntry_InjectedOnce verifies that an expired entry fires
// exactly once (removed from tracking after first inject).
func TestWatchdog_ExpiredEntry_InjectedOnce(t *testing.T) {
	col := &collectingInject{}
	w := orchestrator.NewDeadlineWatchdog(col.inject, 10*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go w.Run(ctx)

	taskID := domain.TaskID("task-once")
	deviceID := domain.DeviceID("dev-1")
	w.Track(taskID, deviceID, time.Now().Add(-1*time.Second))

	// Wait for inject to fire.
	time.Sleep(100 * time.Millisecond)

	count := col.count()
	if count == 0 {
		t.Fatal("expected at least one inject")
	}
	// Wait another few ticks to confirm it doesn't fire again.
	time.Sleep(50 * time.Millisecond)
	if col.count() != count {
		t.Errorf("entry should fire only once; fired %d times", col.count())
	}
}
