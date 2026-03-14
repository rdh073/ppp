package domain

import (
	"encoding/json"
	"time"
)

type CommandKind string

const (
	CommandKindObserve      CommandKind = "device.observe"
	CommandKindQuery        CommandKind = "device.query"
	CommandKindExecute      CommandKind = "device.execute"
	CommandKindCapabilities CommandKind = "device.capabilities.get"
)

// Command is an instruction the server sends to an android-agent via JSON-RPC.
// ID is used as the JSON-RPC request id and as the correlation key for the response.
type Command struct {
	ID       string
	Kind     CommandKind
	DeviceID DeviceID
	TaskID   TaskID
	Params   json.RawMessage
	IssuedAt time.Time
}

// CommandResult is the parsed outcome of a device.* JSON-RPC response from the agent.
type CommandResult struct {
	CommandID  string
	DeviceID   DeviceID
	Success    bool
	Raw        json.RawMessage // result or error payload from agent
	Err        *CommandError
	ReceivedAt time.Time
}

type CommandError struct {
	Code    int
	Message string
}

type CommandOutboxStatus string

const (
	CommandOutboxStatusIssued         CommandOutboxStatus = "issued"
	CommandOutboxStatusDispatched     CommandOutboxStatus = "dispatched"
	CommandOutboxStatusDispatchFailed CommandOutboxStatus = "dispatch_failed"
	CommandOutboxStatusResponded      CommandOutboxStatus = "responded"
)

type CommandOutboxRecord struct {
	Command    Command             `json:"command"`
	Status     CommandOutboxStatus `json:"status"`
	LastError  string              `json:"lastError,omitempty"`
	LastResult *CommandResult      `json:"lastResult,omitempty"`
	UpdatedAt  time.Time           `json:"updatedAt"`
}
