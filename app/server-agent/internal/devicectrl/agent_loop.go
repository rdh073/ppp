package devicectrl

import (
	"context"
	"encoding/json"
	"errors"
)

// RecordingAgentTool describes one tool the LLM agent can call during a recording run.
type RecordingAgentTool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
}

// RecordingAgentRequest carries all runtime config for one AI recording run.
type RecordingAgentRequest struct {
	SystemPrompt string
	Goal         string
	Tools        []RecordingAgentTool
	MaxSteps     int
	OnChunk      func(string)
}

// RecordingAgentResult is returned when the agent loop terminates.
type RecordingAgentResult struct {
	Done   bool
	Reason string
	Steps  int
}

// ErrAgentDone is returned by ToolExecutor to signal the agent loop should stop.
var ErrAgentDone = errors.New("agent done")

// ToolExecutor executes one tool call and returns the result as JSON.
// Return ErrAgentDone to stop the loop (typically from the "done" tool).
type ToolExecutor func(ctx context.Context, toolName string, input json.RawMessage) (json.RawMessage, error)

// RecordingAgentLoop runs a multi-turn LLM tool-use loop that drives a device
// and records actions. Defined here so devicectrl has no dependency on tools/llm.
// The concrete implementation is injected at the composition root.
type RecordingAgentLoop interface {
	Run(ctx context.Context, req RecordingAgentRequest, exec ToolExecutor) (RecordingAgentResult, error)
}
