package appport

import (
	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// --- TaskControl types (moved from workflowruntime) ---

// CreateTaskRequest specifies the task to create and which device to assign.
// DeviceID is optional; if empty the task is created pending assignment.
// WorkflowName is optional; if empty the server default workflow is used.
type CreateTaskRequest struct {
	Goal           string
	DeviceID       domain.DeviceID
	WorkflowName   string
	InputArtifacts map[string]string
}

// ListTaskQuery filters tasks for the ListTasks endpoint.
type ListTaskQuery struct {
	Status       domain.TaskStatus
	DeviceID     domain.DeviceID
	WorkflowName string
	Limit        int
	Offset       int
}

// TaskSummary is the enriched view of a task returned by GetTaskSummary and ListTasks.
type TaskSummary struct {
	Task              *domain.Task
	CurrentStep       string
	RetryCount        int
	LastCommandStatus domain.CommandOutboxStatus
	LastCommandError  string
	OutputArtifacts   map[string]string
}

