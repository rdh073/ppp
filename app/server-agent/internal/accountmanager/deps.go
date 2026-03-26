package accountmanager

import (
	"github.com/autosdk/ppp/server-agent/internal/appport"
	"github.com/autosdk/ppp/server-agent/internal/projection"
)

type TaskControl = appport.TaskControl

type AdbPmClearer = appport.AdbPmClearer

type ProjectionPublisher = projection.Publisher

type ProjectionEvent = projection.Event

type CreateTaskRequest = appport.CreateTaskRequest

var pollTaskUntilTerminal = appport.PollTaskUntilTerminal
