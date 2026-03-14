package store

import (
	"context"

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
