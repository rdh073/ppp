package llm

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
)

// AgentTool describes one tool the LLM can call in an agent loop.
type AgentTool struct {
	Name        string
	Description string
	InputSchema json.RawMessage // JSON Schema object for the tool's input
}

// AgentLoopRequest carries all runtime config for one agent run.
type AgentLoopRequest struct {
	SystemPrompt string
	Goal         string
	Tools        []AgentTool
	MaxSteps     int
	// OnChunk is an optional callback invoked for each streamed text chunk.
	// When non-nil the provider uses streaming mode; when nil it uses the
	// standard blocking request (identical behaviour to the pre-streaming code).
	OnChunk func(text string)
}

// AgentLoopResult is returned when the loop terminates.
type AgentLoopResult struct {
	Done   bool
	Reason string
	Steps  int
}

// ToolExecutor executes one tool call and returns its result as JSON.
// Return ErrAgentDone to stop the loop (typically returned by a "done" tool).
type ToolExecutor func(ctx context.Context, toolName string, input json.RawMessage) (json.RawMessage, error)

// ErrAgentDone is returned by ToolExecutor to signal the agent loop should terminate.
var ErrAgentDone = errors.New("agent done")

// AgentLoop runs a multi-turn LLM tool-use loop.
// The LLM receives the available tools and calls them one at a time; the
// executor callback runs each tool call and returns the result to the LLM.
// The loop ends when the executor returns ErrAgentDone, maxSteps is reached,
// or ctx is cancelled.
type AgentLoop interface {
	Run(ctx context.Context, req AgentLoopRequest, exec ToolExecutor) (AgentLoopResult, error)
}

// NewAgentLoop creates an AgentLoop for the given kind.
//
//	kind "anthropic" (default) — Anthropic Messages API via Ingenimax SDK
//	kind "openai"              — OpenAI chat-completions via Ingenimax SDK
//	                             (also compatible with DeepSeek and other OpenAI-compatible endpoints)
//	kind "gemini"              — Gemini generateContent via Ingenimax SDK
//
// Returns nil if cfg.APIKey or cfg.Model are empty (caller should treat nil as disabled/503).
// apiVersion is no longer used; kept for API compatibility.
func NewAgentLoop(kind string, cfg ModelToolConfig, apiVersion string, log *slog.Logger) AgentLoop {
	switch kind {
	case "openai":
		return newIngemaxOpenAILoop(cfg, log)
	case "gemini":
		return newIngemaxGeminiLoop(cfg, log)
	default: // "anthropic"
		return newIngemaxAnthropicLoop(cfg, log)
	}
}
