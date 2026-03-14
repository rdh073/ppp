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
	ListAccepted(ctx context.Context) ([]domain.AcceptedEventRecord, error)
	ListDeadLetters(ctx context.Context) ([]domain.DeadLetterRecord, error)
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
