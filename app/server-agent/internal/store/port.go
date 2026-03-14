package store

import (
	"context"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// TaskStore persists Task records.
type TaskStore interface {
	Save(ctx context.Context, t *domain.Task) error
	Get(ctx context.Context, id domain.TaskID) (*domain.Task, error)
	List(ctx context.Context) ([]*domain.Task, error)
	ListByDevice(ctx context.Context, deviceID domain.DeviceID) ([]*domain.Task, error)
}

// WorkflowStateStore checkpoints per-device workflow execution state.
// All writes are upserts keyed by (TaskID, DeviceID).
type WorkflowStateStore interface {
	Save(ctx context.Context, s *domain.WorkflowState) error
	Get(ctx context.Context, taskID domain.TaskID, deviceID domain.DeviceID) (*domain.WorkflowState, error)
	// ListActiveByDevice returns all non-terminal states for a device (for recovery on reconnect).
	ListActiveByDevice(ctx context.Context, deviceID domain.DeviceID) ([]*domain.WorkflowState, error)
}

// EventPlaneStore durably records accepted events, dead letters, and device
// ordering cursors used for watermark and dedup decisions.
type EventPlaneStore interface {
	Accept(ctx context.Context, event domain.Event) (domain.EventAcceptance, error)
	RecordDeadLetter(ctx context.Context, record domain.DeadLetterRecord) error
	QueryAccepted(ctx context.Context, query AcceptedEventListQuery) (AcceptedEventPage, error)
	QueryDeadLetters(ctx context.Context, query DeadLetterListQuery) (DeadLetterPage, error)
	ListAccepted(ctx context.Context) ([]domain.AcceptedEventRecord, error)
	ListDeadLetters(ctx context.Context) ([]domain.DeadLetterRecord, error)
}

// TaskQueue is a durable FIFO for pending tasks waiting to be assigned to a device.
// All implementations must be safe for concurrent use.
// Enqueue is idempotent: enqueueing an already-present ID is a no-op.
// Remove is idempotent: removing a non-member ID is a no-op.
type TaskQueue interface {
	// Enqueue adds taskID at the back of the queue. No-op if already present.
	Enqueue(ctx context.Context, taskID domain.TaskID) error
	// Dequeue removes and returns the front task ID. Returns false when the queue is empty.
	Dequeue(ctx context.Context) (domain.TaskID, bool, error)
	// Remove removes a specific task from the queue (e.g. when the task is cancelled).
	Remove(ctx context.Context, taskID domain.TaskID) error
	// Snapshot returns an ordered copy of the queue contents, front to back.
	Snapshot(ctx context.Context) ([]domain.TaskID, error)
}

// CommandOutboxStore records outbound device commands and their delivery state.
type CommandOutboxStore interface {
	SaveIssued(ctx context.Context, cmd domain.Command) error
	MarkDispatched(ctx context.Context, commandID string, dispatchedAt time.Time) error
	MarkDispatchFailed(ctx context.Context, commandID string, errMessage string, failedAt time.Time) error
	MarkDelivered(ctx context.Context, result domain.CommandResult) error
	Get(ctx context.Context, commandID string) (*domain.CommandOutboxRecord, error)
	List(ctx context.Context) ([]*domain.CommandOutboxRecord, error)
}
