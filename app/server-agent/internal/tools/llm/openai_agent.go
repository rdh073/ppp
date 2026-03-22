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

// openAIAgentLoop implements AgentLoop using the OpenAI chat-completions API
// with native multi-turn tool use (tool_choice: "required").
// Compatible with any OpenAI-compatible endpoint (DeepSeek, Ollama, etc.).
type openAIAgentLoop struct {
	apiURL     string
	apiKey     string
	model      string
	httpClient *http.Client
	log        *slog.Logger
}

// NewOpenAICompatibleAgentLoop returns an AgentLoop backed by the OpenAI chat-completions API.
// Returns nil if APIKey or Model are empty (handler returns 503).
func NewOpenAICompatibleAgentLoop(cfg ModelToolConfig, log *slog.Logger) AgentLoop {
	if strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.Model) == "" {
		return nil
	}
	apiURL := strings.TrimSpace(cfg.APIURL)
	if apiURL == "" {
		apiURL = "https://api.openai.com/v1/chat/completions"
	}
	return &openAIAgentLoop{
		apiURL: apiURL,
		apiKey: strings.TrimSpace(cfg.APIKey),
		model:  strings.TrimSpace(cfg.Model),
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		log: log,
	}
}

// ---- internal wire types ----

type openAIAgentMsg struct {
	Role       string              `json:"role"`
	Content    *string             `json:"content"`
	ToolCalls  []openAIToolCall    `json:"tool_calls,omitempty"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // "function"
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"` // JSON string
	} `json:"function"`
}

type openAIAgentTool struct {
	Type     string `json:"type"` // "function"
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Parameters  any    `json:"parameters"`
	} `json:"function"`
}

type openAIAgentReqBody struct {
	Model      string           `json:"model"`
	Messages   []openAIAgentMsg `json:"messages"`
	Tools      []openAIAgentTool `json:"tools"`
	ToolChoice string            `json:"tool_choice"` // "required"
	MaxTokens  int               `json:"max_tokens"`
	Temperature float64          `json:"temperature"`
}

type openAIAgentRespBody struct {
	Choices []struct {
		Message      openAIAgentMsg `json:"message"`
		FinishReason string         `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// ---- Run implementation ----

func (o *openAIAgentLoop) Run(ctx context.Context, req AgentLoopRequest, exec ToolExecutor) (AgentLoopResult, error) {
	// Convert AgentTool → openAIAgentTool.
	tools := make([]openAIAgentTool, len(req.Tools))
	for i, t := range req.Tools {
		var schema any
		if len(t.InputSchema) > 0 {
			if err := json.Unmarshal(t.InputSchema, &schema); err != nil {
				schema = map[string]any{"type": "object", "properties": map[string]any{}}
			}
		} else {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		tools[i].Type = "function"
		tools[i].Function.Name = t.Name
		tools[i].Function.Description = t.Description
		tools[i].Function.Parameters = schema
	}

	// Build initial messages: system prompt + user goal.
	messages := []openAIAgentMsg{}
	if req.SystemPrompt != "" {
		sp := req.SystemPrompt
		messages = append(messages, openAIAgentMsg{Role: "system", Content: &sp})
	}
	goal := req.Goal
	messages = append(messages, openAIAgentMsg{Role: "user", Content: &goal})

	for step := 1; step <= req.MaxSteps; step++ {
		body := openAIAgentReqBody{
			Model:       o.model,
			Messages:    messages,
			Tools:       tools,
			ToolChoice:  "required", // force a tool call every turn
			MaxTokens:   2048,
			Temperature: 0.0,
		}

		raw, err := o.post(ctx, body)
		if err != nil {
			return AgentLoopResult{Steps: step - 1}, err
		}

		var resp openAIAgentRespBody
		if err := json.Unmarshal(raw, &resp); err != nil {
			return AgentLoopResult{Steps: step - 1}, fmt.Errorf("decode agent response: %w", err)
		}
		if resp.Error != nil {
			return AgentLoopResult{Steps: step - 1}, fmt.Errorf("openai agent: %s", resp.Error.Message)
		}
		if len(resp.Choices) == 0 {
			return AgentLoopResult{Steps: step - 1}, fmt.Errorf("openai agent: no choices in response")
		}

		choice := resp.Choices[0]

		// If no tool calls, the model is done (finish_reason: "stop").
		if len(choice.Message.ToolCalls) == 0 {
			break
		}

		// Append the assistant message (with tool_calls) to history.
		messages = append(messages, choice.Message)

		// Execute the first tool call.
		tc := choice.Message.ToolCalls[0]
		tuInput := json.RawMessage(tc.Function.Arguments)

		if o.log != nil {
			o.log.Debug("agent loop step", "step", step, "tool", tc.Function.Name)
		}

		toolResult, err := exec(ctx, tc.Function.Name, tuInput)
		if errors.Is(err, ErrAgentDone) {
			var doneInput struct {
				Reason string `json:"reason"`
			}
			_ = json.Unmarshal(tuInput, &doneInput)
			return AgentLoopResult{Done: true, Reason: doneInput.Reason, Steps: step}, nil
		}

		// Build the tool result message.
		var resultContent string
		switch {
		case err != nil:
			resultContent = "error: " + err.Error()
		case toolResult != nil:
			resultContent = string(toolResult)
		default:
			resultContent = "ok"
		}
		messages = append(messages, openAIAgentMsg{
			Role:       "tool",
			Content:    &resultContent,
			ToolCallID: tc.ID,
		})
	}

	return AgentLoopResult{Done: false, Steps: req.MaxSteps}, nil
}

func (o *openAIAgentLoop) post(ctx context.Context, body any) ([]byte, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal agent request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.apiURL, bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("build agent request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.httpClient.Do(httpReq)
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
		return nil, MarkToolRetryable(fmt.Errorf("openai agent status %d: %s", resp.StatusCode, CompactProviderError(respBody)))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("openai agent status %d: %s", resp.StatusCode, CompactProviderError(respBody))
	}
	return respBody, nil
}
