package usecase

import (
	"context"
	"encoding/json"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/registry"
)

// AgentLifecycle is the primary port for the JSON-RPC agent connection handler.
type AgentLifecycle interface {
	Hello(ctx context.Context, req HelloRequest, conn registry.Sender) (HelloResponse, error)
	Resume(ctx context.Context, req ResumeRequest, conn registry.Sender) (ResumeResponse, error)
	Heartbeat(ctx context.Context, deviceID domain.DeviceID, sessionID domain.SessionID)
	Disconnect(ctx context.Context, deviceID domain.DeviceID, sessionID domain.SessionID)
}

// TaskControl is the primary port for the HTTP task handler.
type TaskControl interface {
	CreateTask(ctx context.Context, req CreateTaskRequest) (*domain.Task, error)
	ListTasks(ctx context.Context, query ListTaskQuery) ([]TaskSummary, error)
	GetTask(ctx context.Context, taskID domain.TaskID) (*domain.Task, error)
	GetTaskSummary(ctx context.Context, taskID domain.TaskID) (*TaskSummary, error)
	CancelTask(ctx context.Context, taskID domain.TaskID) error
}

// EventPlaneControl is the primary port for the HTTP event-plane handler.
type EventPlaneControl interface {
	ListAccepted(ctx context.Context, query AcceptedEventListQuery) (AcceptedEventPage, error)
	ListDeadLetters(ctx context.Context, query DeadLetterListQuery) (DeadLetterPage, error)
	GetAccepted(ctx context.Context, eventID string) (*domain.AcceptedEventRecord, error)
	GetAcceptedPayload(ctx context.Context, eventID string, maxBytes int) (*AcceptedPayloadPreview, error)
	GetDeadLetter(ctx context.Context, deadLetterID string) (*domain.DeadLetterRecord, error)
	ReplayAccepted(ctx context.Context, eventID string) error
	ReplayDeadLetter(ctx context.Context, deadLetterID string) error
}

// EventIngestion is the primary port for the WebSocket transport adapter.
type EventIngestion interface {
	IngestNotification(ctx context.Context, deviceID domain.DeviceID, method string, rawParams json.RawMessage) error
}

// AdbPmClearer clears package data via ADB shell.
type AdbPmClearer interface {
	PmClear(ctx context.Context, deviceID domain.DeviceID, pkg string) error
}

// AdbFilePusher pushes files to a device and triggers the media scanner.
type AdbFilePusher interface {
	PushFile(ctx context.Context, deviceID domain.DeviceID, localPath, remotePath string) error
	MediaScan(ctx context.Context, deviceID domain.DeviceID, remotePath string) error
}

// Compile-time interface satisfaction checks.
var (
	_ AgentLifecycle    = (*AgentLifecycleUseCase)(nil)
	_ TaskControl       = (*TaskControlUseCase)(nil)
	_ EventPlaneControl = (*EventPlaneControlUseCase)(nil)
	_ EventIngestion    = (*EventIngestionUseCase)(nil)
)
