package accountmanager

import (
	"github.com/autosdk/ppp/server-agent/internal/appport"
	"github.com/autosdk/ppp/server-agent/internal/projection"
	"github.com/autosdk/ppp/server-agent/internal/workflowruntime"
)

type TaskControl = appport.TaskControl

type AdbPmClearer = appport.AdbPmClearer

type ProjectionPublisher = projection.Publisher

type ProjectionEvent = projection.Event

type CreateTaskRequest = workflowruntime.CreateTaskRequest

var pollTaskUntilTerminal = workflowruntime.PollTaskUntilTerminal
