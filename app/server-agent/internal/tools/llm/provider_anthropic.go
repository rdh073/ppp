package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// timeNow and timeSince are thin wrappers to make call sites readable.
func timeNow() time.Time          { return time.Now() }
func timeSince(t time.Time) time.Duration { return time.Since(t) }

// anthropicProvider implements AgentLoop, JSONModelClient, and VisionModelClient
// using the Anthropic Messages API.
type anthropicProvider struct {
	apiURL  string
	apiKey  string
	model   string
	version string
	http    *providerHTTPClient
	log     *slog.Logger
}

func newAnthropicProvider(cfg ModelToolConfig, apiVersion string, timeout time.Duration, log *slog.Logger) *anthropicProvider {
	version := strings.TrimSpace(apiVersion)
	if version == "" {
		version = "2023-06-01"
	}
	apiURL := strings.TrimSpace(cfg.APIURL)
	if apiURL == "" {
		apiURL = "https://api.anthropic.com/v1/messages"
	}
	return &anthropicProvider{
		apiURL:  apiURL,
		apiKey:  strings.TrimSpace(cfg.APIKey),
		model:   strings.TrimSpace(cfg.Model),
		version: version,
		http:    newProviderHTTPClient(timeout),
		log:     log,
	}
}

func (p *anthropicProvider) headers() map[string]string {
	h := map[string]string{
		"anthropic-version": p.version,
	}
	if p.apiKey != "" {
		h["x-api-key"] = p.apiKey
	}
	return h
}

// ---- constructor functions (keep same signatures as old files) ----

// NewAnthropicAgentLoop returns an AgentLoop backed by the Anthropic Messages API.
// Returns nil if APIKey or Model are empty (handler returns 503).
func NewAnthropicAgentLoop(cfg ModelToolConfig, apiVersion string, log *slog.Logger) AgentLoop {
	if strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.Model) == "" {
		return nil
	}
	return newAnthropicProvider(cfg, apiVersion, 60*time.Second, log)
}

// NewAnthropicMessagesJSONClient creates a JSONModelClient backed by the Anthropic Messages API.
func NewAnthropicMessagesJSONClient(cfg ModelToolConfig, apiVersion string, log *slog.Logger) JSONModelClient {
	if !cfg.Enabled() {
		return nil
	}
	return newAnthropicProvider(cfg, apiVersion, 15*time.Second, log)
}

// NewAnthropicVisionClient creates a VisionModelClient backed by the Anthropic Messages API.
// Uses a longer HTTP timeout (vision inference is slower).
func NewAnthropicVisionClient(cfg ModelToolConfig, apiVersion string, log *slog.Logger) VisionModelClient {
	if !cfg.Enabled() {
		return nil
	}
	return newAnthropicProvider(cfg, apiVersion, 45*time.Second, log)
}

// ---- wire types shared by all three implementations ----

type anthropicTool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InputSchema any    `json:"input_schema"`
}

type anthropicToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

// agentMsg holds one turn in the multi-turn conversation.
// Content is json.RawMessage because it can be a plain string ("user goal text")
// or a content-block array (tool_use / tool_result).
type agentMsg struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type agentContentBlock struct {
	Type  string          `json:"type"`
	ID    string          `json:"id,omitempty"`    // tool_use
	Name  string          `json:"name,omitempty"`  // tool_use
	Input json.RawMessage `json:"input,omitempty"` // tool_use
	Text  string          `json:"text,omitempty"`  // text
}

type anthropicAgentReqBody struct {
	Model       string              `json:"model"`
	System      string              `json:"system,omitempty"`
	MaxTokens   int                 `json:"max_tokens"`
	Messages    []agentMsg          `json:"messages"`
	Tools       []anthropicTool     `json:"tools"`
	ToolChoice  anthropicToolChoice `json:"tool_choice"`
	Temperature float64             `json:"temperature"`
	Stream      bool                `json:"stream,omitempty"`
}

type anthropicAgentRespBody struct {
	Content    []agentContentBlock `json:"content"`
	StopReason string              `json:"stop_reason,omitempty"`
	Error      *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicMessagesRequest struct {
	Model       string              `json:"model"`
	System      string              `json:"system,omitempty"`
	MaxTokens   int                 `json:"max_tokens"`
	Messages    []anthropicMessage  `json:"messages"`
	Tools       []anthropicTool     `json:"tools"`
	ToolChoice  anthropicToolChoice `json:"tool_choice"`
	Temperature float64             `json:"temperature"`
}

type anthropicMessagesResponse struct {
	Content []struct {
		Type  string          `json:"type"`
		Text  string          `json:"text,omitempty"`
		Input json.RawMessage `json:"input,omitempty"`
		Name  string          `json:"name,omitempty"`
	} `json:"content"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error,omitempty"`
}

// Anthropic multimodal request wire types.
// anthropicVisionMsg differs from anthropicMessage in that Content is a typed
// slice (for image + text parts) rather than a plain string.

type anthropicVisionRequest struct {
	Model       string               `json:"model"`
	System      string               `json:"system,omitempty"`
	MaxTokens   int                  `json:"max_tokens"`
	Messages    []anthropicVisionMsg `json:"messages"`
	Tools       []anthropicTool      `json:"tools"`
	ToolChoice  anthropicToolChoice  `json:"tool_choice"`
	Temperature float64              `json:"temperature"`
}

type anthropicVisionMsg struct {
	Role    string                 `json:"role"`
	Content []anthropicContentPart `json:"content"`
}

type anthropicContentPart struct {
	Type   string              `json:"type"`             // "text" | "image"
	Text   string              `json:"text,omitempty"`
	Source *anthropicImgSource `json:"source,omitempty"` // present when Type=="image"
}

type anthropicImgSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/jpeg" | "image/png" | ...
	Data      string `json:"data"`       // raw base64 string (no data-URL prefix)
}

// ---- AgentLoop implementation ----

func (p *anthropicProvider) Run(ctx context.Context, req AgentLoopRequest, exec ToolExecutor) (AgentLoopResult, error) {
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
			Model:       p.model,
			System:      req.SystemPrompt,
			MaxTokens:   2048,
			Messages:    messages,
			Tools:       tools,
			ToolChoice:  anthropicToolChoice{Type: "any"}, // force a tool call every turn
			Temperature: 0.0,
		}

		var tuID, tuName string
		var tuInput json.RawMessage
		var err error

		if req.OnChunk != nil {
			tuID, tuName, tuInput, err = p.runStreaming(ctx, body, req.OnChunk)
		} else {
			tuID, tuName, tuInput, err = p.runBlocking(ctx, body)
		}
		if err != nil {
			return AgentLoopResult{Steps: step - 1}, err
		}

		if tuName == "" {
			// No tool call despite tool_choice:"any" — stop_reason is likely "end_turn".
			break
		}

		// We need to rebuild the assistant content block for history.
		// For streaming we only have the final tool call; for blocking we have the full content.
		// We reconstruct a minimal assistant message with just the tool_use block.
		assistantContent, _ := json.Marshal([]agentContentBlock{
			{Type: "tool_use", ID: tuID, Name: tuName, Input: tuInput},
		})
		messages = append(messages, agentMsg{Role: "assistant", Content: assistantContent})

		if p.log != nil {
			p.log.Debug("agent loop step", "step", step, "tool", tuName)
		}

		// Execute the tool via the caller-supplied executor.
		toolResult, execErr := exec(ctx, tuName, tuInput)
		if isErrAgentDone(execErr) {
			var doneInput struct {
				Reason string `json:"reason"`
			}
			_ = json.Unmarshal(tuInput, &doneInput)
			return AgentLoopResult{Done: true, Reason: doneInput.Reason, Steps: step}, nil
		}

		// Build the tool_result user turn.
		var resultStr string
		switch {
		case execErr != nil:
			resultStr = "error: " + execErr.Error()
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

// runBlocking performs a non-streaming Anthropic request and returns the first tool_use block.
func (p *anthropicProvider) runBlocking(ctx context.Context, body anthropicAgentReqBody) (tuID, tuName string, tuInput json.RawMessage, err error) {
	raw, err := p.http.PostJSON(ctx, p.apiURL, p.headers(), body)
	if err != nil {
		return "", "", nil, err
	}
	var resp anthropicAgentRespBody
	if err := json.Unmarshal(raw, &resp); err != nil {
		return "", "", nil, fmt.Errorf("decode agent response: %w", err)
	}
	if resp.Error != nil {
		return "", "", nil, fmt.Errorf("anthropic agent: %s", resp.Error.Message)
	}
	for _, block := range resp.Content {
		if block.Type == "tool_use" {
			return block.ID, block.Name, block.Input, nil
		}
	}
	return "", "", nil, nil
}

// runStreaming performs a streaming Anthropic request, calls onChunk for text deltas,
// and returns the first tool_use block assembled from input_json_delta events.
//
// SSE event types used:
//
//	content_block_start  — carries block type, tool id/name for tool_use blocks
//	content_block_delta  — text_delta (text blocks) or input_json_delta (tool_use blocks)
//	message_stop         — end of stream
func (p *anthropicProvider) runStreaming(ctx context.Context, body anthropicAgentReqBody, onChunk func(string)) (tuID, tuName string, tuInput json.RawMessage, err error) {
	body.Stream = true

	var toolArgsBuf bytes.Buffer
	var streamErr error

	streamErr = p.http.StreamSSE(ctx, p.apiURL, p.headers(), body, func(data []byte) error {
		var ev struct {
			Type  string `json:"type"`
			Index int    `json:"index"`
			// content_block_start
			ContentBlock *struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
			// content_block_delta
			Delta *struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
			} `json:"delta"`
		}
		if err := json.Unmarshal(data, &ev); err != nil {
			return nil // skip unparseable events
		}
		switch ev.Type {
		case "content_block_start":
			if ev.ContentBlock != nil && ev.ContentBlock.Type == "tool_use" {
				tuID = ev.ContentBlock.ID
				tuName = ev.ContentBlock.Name
				toolArgsBuf.Reset()
			}
		case "content_block_delta":
			if ev.Delta == nil {
				return nil
			}
			switch ev.Delta.Type {
			case "text_delta":
				if onChunk != nil && ev.Delta.Text != "" {
					onChunk(ev.Delta.Text)
				}
			case "input_json_delta":
				toolArgsBuf.WriteString(ev.Delta.PartialJSON)
			}
		}
		return nil
	})

	if streamErr != nil {
		return "", "", nil, streamErr
	}
	if tuName != "" {
		tuInput = json.RawMessage(toolArgsBuf.Bytes())
	}
	return tuID, tuName, tuInput, nil
}

// ---- JSONModelClient implementation ----

func (p *anthropicProvider) GenerateJSON(ctx context.Context, request JSONModelRequest) (json.RawMessage, error) {
	if p == nil {
		return nil, fmt.Errorf("%w: model client not configured", ErrToolDisabled)
	}

	var schema any
	if len(request.OutputSchema) == 0 || !json.Valid(request.OutputSchema) {
		return nil, fmt.Errorf("invalid output schema for %s", request.ToolName)
	}
	if err := json.Unmarshal(request.OutputSchema, &schema); err != nil {
		return nil, fmt.Errorf("decode output schema: %w", err)
	}
	temperature := 0.2
	if request.Temperature != nil {
		temperature = *request.Temperature
	}

	reqBody := anthropicMessagesRequest{
		Model:     p.model,
		System:    defaultSystemPrompt(request.SystemPrompt),
		MaxTokens: 1024,
		Messages: []anthropicMessage{
			{Role: "user", Content: request.Prompt},
		},
		Tools: []anthropicTool{
			{
				Name:        schemaNameForTool(request.ToolName),
				Description: "Return the result as structured JSON matching the declared schema.",
				InputSchema: schema,
			},
		},
		ToolChoice: anthropicToolChoice{
			Type: "tool",
			Name: schemaNameForTool(request.ToolName),
		},
		Temperature: temperature,
	}

	start := timeNow()
	raw, err := p.http.PostJSON(ctx, p.apiURL, p.headers(), reqBody)
	if err != nil {
		return nil, err
	}

	if p.log != nil {
		p.log.Debug("anthropic provider response",
			"toolName", request.ToolName,
			"elapsed", timeSince(start),
		)
	}

	var decoded anthropicMessagesResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("decode anthropic response: %w", err)
	}
	content, err := extractAnthropicToolInput(decoded)
	if err != nil {
		return nil, fmt.Errorf("extract anthropic content: %w", err)
	}
	if !json.Valid(content) {
		return nil, fmt.Errorf("anthropic provider returned non-json content: %s", string(content))
	}
	return append(json.RawMessage(nil), content...), nil
}

// ---- VisionModelClient implementation ----

func (p *anthropicProvider) AnalyzeImage(ctx context.Context, req VisionAnalyzeRequest) (json.RawMessage, error) {
	if p == nil {
		return nil, fmt.Errorf("%w: vision client not configured", ErrToolDisabled)
	}
	var schema any
	if len(req.OutputSchema) == 0 || !json.Valid(req.OutputSchema) {
		return nil, fmt.Errorf("invalid output schema for %s", req.ToolName)
	}
	if err := json.Unmarshal(req.OutputSchema, &schema); err != nil {
		return nil, fmt.Errorf("decode output schema: %w", err)
	}
	toolName := schemaNameForTool(req.ToolName)
	reqBody := anthropicVisionRequest{
		Model:     p.model,
		MaxTokens: 1024,
		Messages: []anthropicVisionMsg{
			{
				Role: "user",
				Content: []anthropicContentPart{
					{
						Type: "image",
						Source: &anthropicImgSource{
							Type:      "base64",
							MediaType: req.MimeType,
							Data:      req.ImageBase64,
						},
					},
					{Type: "text", Text: req.Prompt},
				},
			},
		},
		Tools: []anthropicTool{
			{
				Name:        toolName,
				Description: "Return the analysis result as structured JSON matching the declared schema.",
				InputSchema: schema,
			},
		},
		ToolChoice:  anthropicToolChoice{Type: "tool", Name: toolName},
		Temperature: 0.1,
	}

	start := timeNow()
	raw, err := p.http.PostJSON(ctx, p.apiURL, p.headers(), reqBody)
	if err != nil {
		return nil, err
	}

	if p.log != nil {
		p.log.Debug("vision provider response",
			"toolName", req.ToolName,
			"elapsed", timeSince(start),
		)
	}

	var decoded anthropicMessagesResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("decode vision response: %w", err)
	}
	content, err := extractAnthropicToolInput(decoded)
	if err != nil {
		return nil, fmt.Errorf("extract vision content: %w", err)
	}
	if !json.Valid(content) {
		return nil, fmt.Errorf("vision provider returned non-json: %s", string(content))
	}
	return append(json.RawMessage(nil), content...), nil
}

// ---- internal helpers ----

func extractAnthropicToolInput(response anthropicMessagesResponse) (json.RawMessage, error) {
	if response.Error != nil && strings.TrimSpace(response.Error.Message) != "" {
		return nil, errorf("%s", response.Error.Message)
	}
	for _, part := range response.Content {
		if part.Type == "tool_use" && len(part.Input) > 0 {
			return append(json.RawMessage(nil), part.Input...), nil
		}
	}
	return nil, errorf("no tool_use content in anthropic response")
}
