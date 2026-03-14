package domain

import "time"

type TaskID string

type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusPaused    TaskStatus = "paused" // agent offline
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCancelled TaskStatus = "cancelled"
)

func (s TaskStatus) IsTerminal() bool {
	return s == TaskStatusCompleted || s == TaskStatusFailed || s == TaskStatusCancelled
}

type Task struct {
	ID             TaskID
	Goal           string
	Status         TaskStatus
	AssignedDevice DeviceID // empty until assigned
	WorkflowName   string   // optional; empty means use server default
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func NewTaskID() TaskID {
	return TaskID("task-" + newID())
}
