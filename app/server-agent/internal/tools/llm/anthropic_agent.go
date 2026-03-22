package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// anthropicAgentLoop implements AgentLoop using the Anthropic Messages API
// with native multi-turn tool use (tool_choice: "any").
type anthropicAgentLoop struct {
	apiURL     string
	apiKey     string
	model      string
	version    string
	httpClient *http.Client
	log        *slog.Logger
}

// NewAnthropicAgentLoop returns an AgentLoop backed by the Anthropic Messages API.
// Returns nil if APIKey or Model are empty (handler returns 503).
func NewAnthropicAgentLoop(cfg ModelToolConfig, apiVersion string, log *slog.Logger) AgentLoop {
	if strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.Model) == "" {
		return nil
	}
	version := strings.TrimSpace(apiVersion)
	if version == "" {
		version = "2023-06-01"
	}
	apiURL := strings.TrimSpace(cfg.APIURL)
	if apiURL == "" {
		apiURL = "https://api.anthropic.com/v1/messages"
	}
	return &anthropicAgentLoop{
		apiURL:  apiURL,
		apiKey:  strings.TrimSpace(cfg.APIKey),
		model:   strings.TrimSpace(cfg.Model),
		version: version,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		log: log,
	}
}

// ---- internal wire types ----

// agentMsg holds one turn in the multi-turn conversation.
// Content is json.RawMessage because it can be a plain string ("user goal text")
// or a content-block array (tool_use / tool_result).
type agentMsg struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type anthropicAgentReqBody struct {
	Model       string               `json:"model"`
	System      string               `json:"system,omitempty"`
	MaxTokens   int                  `json:"max_tokens"`
	Messages    []agentMsg           `json:"messages"`
	Tools       []anthropicTool      `json:"tools"`
	ToolChoice  anthropicToolChoice  `json:"tool_choice"`
	Temperature float64              `json:"temperature"`
}

type agentContentBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id,omitempty"`    // tool_use
	Name  string          `json:"name,omitempty"`  // tool_use
	Input json.RawMessage `json:"input,omitempty"` // tool_use
	Text  string          `json:"text,omitempty"`  // text
}

type anthropicAgentRespBody struct {
	Content    []agentContentBlock `json:"content"`
	StopReason string              `json:"stop_reason,omitempty"`
	Error      *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// ---- Run implementation ----

func (a *anthropicAgentLoop) Run(ctx context.Context, req AgentLoopRequest, exec ToolExecutor) (AgentLoopResult, error) {
	// Convert AgentTool → anthropicTool (input_schema must be parsed object, not raw JSON string).
	tools := make([]anthropicTool, len(req.Tools))
	for i, t := range req.Tools {
		var schema any
		if len(t.InputSchema) > 0 {
			if err := json.Unmarshal(t.InputSchema, &schema); err != nil {
				schema = map[string]any{"type": "object", "properties": map[string]any{}}
			}
		} else {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		tools[i] = anthropicTool{Name: t.Name, Description: t.Description, InputSchema: schema}
	}

	// Initial user message — the goal text.
	goalJSON, _ := json.Marshal(req.Goal)
	messages := []agentMsg{{Role: "user", Content: goalJSON}}

	for step := 1; step <= req.MaxSteps; step++ {
		body := anthropicAgentReqBody{
			Model:       a.model,
			System:      req.SystemPrompt,
			MaxTokens:   2048,
			Messages:    messages,
			Tools:       tools,
			ToolChoice:  anthropicToolChoice{Type: "any"}, // force a tool call every turn
			Temperature: 0.0,
		}

		raw, err := a.post(ctx, body)
		if err != nil {
			return AgentLoopResult{Steps: step - 1}, err
		}

		var resp anthropicAgentRespBody
		if err := json.Unmarshal(raw, &resp); err != nil {
			return AgentLoopResult{Steps: step - 1}, fmt.Errorf("decode agent response: %w", err)
		}
		if resp.Error != nil {
			return AgentLoopResult{Steps: step - 1}, fmt.Errorf("anthropic agent: %s", resp.Error.Message)
		}

		// Find the first tool_use block in the response.
		var tuID, tuName string
		var tuInput json.RawMessage
		for _, block := range resp.Content {
			if block.Type == "tool_use" {
				tuID, tuName, tuInput = block.ID, block.Name, block.Input
				break
			}
		}
		if tuName == "" {
			// No tool call despite tool_choice:"any" — stop_reason is likely "end_turn".
			break
		}

		// Append assistant message (the full content-block array) to history.
		assistantContent, _ := json.Marshal(resp.Content)
		messages = append(messages, agentMsg{Role: "assistant", Content: assistantContent})

		if a.log != nil {
			a.log.Debug("agent loop step", "step", step, "tool", tuName)
		}

		// Execute the tool via the caller-supplied executor.
		toolResult, err := exec(ctx, tuName, tuInput)
		if errors.Is(err, ErrAgentDone) {
			var doneInput struct {
				Reason string `json:"reason"`
			}
			_ = json.Unmarshal(tuInput, &doneInput)
			return AgentLoopResult{Done: true, Reason: doneInput.Reason, Steps: step}, nil
		}

		// Build the tool_result user turn.
		var resultStr string
		switch {
		case err != nil:
			resultStr = "error: " + err.Error()
		case toolResult != nil:
			resultStr = string(toolResult)
		default:
			resultStr = "ok"
		}
		toolResultContent, _ := json.Marshal([]map[string]any{
			{"type": "tool_result", "tool_use_id": tuID, "content": resultStr},
		})
		messages = append(messages, agentMsg{Role: "user", Content: toolResultContent})
	}

	return AgentLoopResult{Done: false, Steps: req.MaxSteps}, nil
}

func (a *anthropicAgentLoop) post(ctx context.Context, body any) ([]byte, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal agent request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.apiURL, bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("build agent request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("anthropic-version", a.version)
	httpReq.Header.Set("x-api-key", a.apiKey)

	resp, err := a.httpClient.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, MarkToolRetryable(fmt.Errorf("agent transport: %w", err))
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<21))
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, MarkToolRetryable(fmt.Errorf("read agent response: %w", err))
	}

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
		return nil, MarkToolRetryable(fmt.Errorf("anthropic agent status %d: %s", resp.StatusCode, CompactProviderError(respBody)))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("anthropic agent status %d: %s", resp.StatusCode, CompactProviderError(respBody))
	}
	return respBody, nil
}
