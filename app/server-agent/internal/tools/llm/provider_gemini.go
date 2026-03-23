package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"
)

// geminiProvider implements AgentLoop and JSONModelClient using the Gemini
// generateContent API.
type geminiProvider struct {
	apiURL string
	apiKey string
	model  string
	http   *providerHTTPClient
	log    *slog.Logger
}

func newGeminiProvider(cfg ModelToolConfig, timeout time.Duration, log *slog.Logger) *geminiProvider {
	apiURL := strings.TrimSpace(cfg.APIURL)
	if apiURL == "" {
		apiURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	return &geminiProvider{
		apiURL: apiURL,
		apiKey: strings.TrimSpace(cfg.APIKey),
		model:  strings.TrimSpace(cfg.Model),
		http:   newProviderHTTPClient(timeout),
		log:    log,
	}
}

func (p *geminiProvider) headers() map[string]string {
	if p.apiKey != "" {
		return map[string]string{"x-goog-api-key": p.apiKey}
	}
	return nil
}

func (p *geminiProvider) generateContentEndpoint() string {
	base := strings.TrimRight(p.apiURL, "/")
	if strings.Contains(base, ":generateContent") {
		return base
	}
	return base + "/models/" + url.PathEscape(p.model) + ":generateContent"
}

func (p *geminiProvider) streamGenerateContentEndpoint() string {
	base := strings.TrimRight(p.apiURL, "/")
	if strings.Contains(base, ":streamGenerateContent") {
		return base
	}
	return base + "/models/" + url.PathEscape(p.model) + ":streamGenerateContent"
}

// ---- constructor functions (keep same signatures as old files) ----

// NewGeminiAgentLoop returns an AgentLoop backed by the Gemini generateContent API.
// Returns nil if APIKey or Model are empty (handler returns 503).
func NewGeminiAgentLoop(cfg ModelToolConfig, log *slog.Logger) AgentLoop {
	if strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.Model) == "" {
		return nil
	}
	return newGeminiProvider(cfg, 60*time.Second, log)
}

// NewGeminiGenerateContentJSONClient returns a JSONModelClient backed by the
// Gemini generateContent API with JSON response schema enforcement.
func NewGeminiGenerateContentJSONClient(cfg ModelToolConfig, log *slog.Logger) JSONModelClient {
	if !cfg.Enabled() {
		return nil
	}
	return newGeminiProvider(cfg, 15*time.Second, log)
}

// ---- wire types (agent loop) ----

type geminiAgentContent struct {
	Role  string            `json:"role"`
	Parts []geminiAgentPart `json:"parts"`
}

type geminiAgentPart struct {
	Text             string                  `json:"text,omitempty"`
	FunctionCall     *geminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *geminiFunctionResponse `json:"functionResponse,omitempty"`
}

type geminiFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type geminiFunctionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type geminiFunctionDecl struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type geminiAgentToolDef struct {
	FunctionDeclarations []geminiFunctionDecl `json:"functionDeclarations"`
}

type geminiAgentToolConfig struct {
	FunctionCallingConfig struct {
		Mode string `json:"mode"`
	} `json:"functionCallingConfig"`
}

type geminiAgentGenCfg struct {
	Temperature     float64 `json:"temperature"`
	MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
}

type geminiAgentReqBody struct {
	SystemInstruction *geminiAgentContent    `json:"systemInstruction,omitempty"`
	Contents          []geminiAgentContent   `json:"contents"`
	Tools             []geminiAgentToolDef   `json:"tools"`
	ToolConfig        *geminiAgentToolConfig `json:"toolConfig,omitempty"`
	GenerationConfig  *geminiAgentGenCfg     `json:"generationConfig,omitempty"`
}

type geminiAgentRespBody struct {
	Candidates []struct {
		Content geminiAgentContent `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error,omitempty"`
}

// wire types (JSON client)

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenerationCfg struct {
	ResponseMIMEType string  `json:"responseMimeType"`
	ResponseSchema   any     `json:"responseSchema,omitempty"`
	Temperature      float64 `json:"temperature"`
}

type geminiGenerateContentRequest struct {
	SystemInstruction *geminiContent      `json:"systemInstruction,omitempty"`
	Contents          []geminiContent     `json:"contents"`
	GenerationConfig  geminiGenerationCfg `json:"generationConfig"`
}

type geminiGenerateContentResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error,omitempty"`
}

// ---- AgentLoop implementation ----

func (p *geminiProvider) Run(ctx context.Context, req AgentLoopRequest, exec ToolExecutor) (AgentLoopResult, error) {
	// Convert AgentTool → geminiFunctionDecl.
	decls := make([]geminiFunctionDecl, len(req.Tools))
	for i, t := range req.Tools {
		var schema any
		if len(t.InputSchema) > 0 {
			if err := json.Unmarshal(t.InputSchema, &schema); err != nil {
				schema = map[string]any{"type": "object", "properties": map[string]any{}}
			}
		} else {
			schema = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		decls[i] = geminiFunctionDecl{Name: t.Name, Description: t.Description, Parameters: schema}
	}

	toolConfig := &geminiAgentToolConfig{}
	toolConfig.FunctionCallingConfig.Mode = "ANY" // force a function call every turn

	// Build initial user message with goal text.
	contents := []geminiAgentContent{
		{Role: "user", Parts: []geminiAgentPart{{Text: req.Goal}}},
	}

	// System instruction (optional).
	var sysInstruction *geminiAgentContent
	if req.SystemPrompt != "" {
		sysInstruction = &geminiAgentContent{
			Parts: []geminiAgentPart{{Text: req.SystemPrompt}},
		}
	}

	for step := 1; step <= req.MaxSteps; step++ {
		body := geminiAgentReqBody{
			SystemInstruction: sysInstruction,
			Contents:          contents,
			Tools:             []geminiAgentToolDef{{FunctionDeclarations: decls}},
			ToolConfig:        toolConfig,
			GenerationConfig:  &geminiAgentGenCfg{Temperature: 0.0},
		}

		var fc *geminiFunctionCall
		var err error

		if req.OnChunk != nil {
			fc, err = p.runStreaming(ctx, body, req.OnChunk)
		} else {
			fc, err = p.runBlocking(ctx, body)
		}
		if err != nil {
			return AgentLoopResult{Steps: step - 1}, err
		}

		if fc == nil {
			// No function call — model declined to call a tool.
			break
		}

		// Append model response to history (reconstructed from the function call).
		contents = append(contents, geminiAgentContent{
			Role: "model",
			Parts: []geminiAgentPart{
				{FunctionCall: fc},
			},
		})

		if p.log != nil {
			p.log.Debug("agent loop step", "step", step, "tool", fc.Name)
		}

		// Marshal the function call args to json.RawMessage for the executor.
		argsJSON, _ := json.Marshal(fc.Args)

		// Execute the tool via the caller-supplied executor.
		toolResult, execErr := exec(ctx, fc.Name, argsJSON)
		if errors.Is(execErr, ErrAgentDone) {
			var doneInput struct {
				Reason string `json:"reason"`
			}
			_ = json.Unmarshal(argsJSON, &doneInput)
			return AgentLoopResult{Done: true, Reason: doneInput.Reason, Steps: step}, nil
		}

		// Build the functionResponse user turn.
		var responseMap map[string]any
		switch {
		case execErr != nil:
			responseMap = map[string]any{"error": execErr.Error()}
		case toolResult != nil:
			if jsonErr := json.Unmarshal(toolResult, &responseMap); jsonErr != nil {
				responseMap = map[string]any{"result": string(toolResult)}
			}
		default:
			responseMap = map[string]any{"result": "ok"}
		}

		contents = append(contents, geminiAgentContent{
			Role: "user",
			Parts: []geminiAgentPart{
				{FunctionResponse: &geminiFunctionResponse{Name: fc.Name, Response: responseMap}},
			},
		})
	}

	return AgentLoopResult{Done: false, Steps: req.MaxSteps}, nil
}

// runBlocking performs a non-streaming Gemini request and returns the first functionCall.
func (p *geminiProvider) runBlocking(ctx context.Context, body geminiAgentReqBody) (*geminiFunctionCall, error) {
	raw, err := p.http.PostJSON(ctx, p.generateContentEndpoint(), p.headers(), body)
	if err != nil {
		return nil, err
	}
	var resp geminiAgentRespBody
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode agent response: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("gemini agent: %s", resp.Error.Message)
	}
	if len(resp.Candidates) == 0 {
		return nil, nil
	}
	for _, part := range resp.Candidates[0].Content.Parts {
		if part.FunctionCall != nil {
			return part.FunctionCall, nil
		}
	}
	return nil, nil
}

// runStreaming performs a streaming Gemini request.
// Gemini SSE sends full GenerateContentResponse objects per chunk (not deltas),
// so we parse each chunk as a complete response and extract functionCall or text.
func (p *geminiProvider) runStreaming(ctx context.Context, body geminiAgentReqBody, onChunk func(string)) (*geminiFunctionCall, error) {
	var fc *geminiFunctionCall

	streamErr := p.http.StreamSSE(ctx, p.streamGenerateContentEndpoint(), p.headers(), body, func(data []byte) error {
		var chunk geminiAgentRespBody
		if err := json.Unmarshal(data, &chunk); err != nil {
			return nil
		}
		if len(chunk.Candidates) == 0 {
			return nil
		}
		for _, part := range chunk.Candidates[0].Content.Parts {
			if part.FunctionCall != nil && fc == nil {
				fc = part.FunctionCall
			}
			if onChunk != nil && strings.TrimSpace(part.Text) != "" {
				onChunk(part.Text)
			}
		}
		return nil
	})

	if streamErr != nil {
		return nil, streamErr
	}
	return fc, nil
}

// ---- JSONModelClient implementation ----

func (p *geminiProvider) GenerateJSON(ctx context.Context, request JSONModelRequest) (json.RawMessage, error) {
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

	reqBody := geminiGenerateContentRequest{
		SystemInstruction: &geminiContent{
			Parts: []geminiPart{{Text: defaultSystemPrompt(request.SystemPrompt)}},
		},
		Contents: []geminiContent{
			{Parts: []geminiPart{{Text: request.Prompt}}},
		},
		GenerationConfig: geminiGenerationCfg{
			ResponseMIMEType: "application/json",
			ResponseSchema:   schema,
			Temperature:      temperature,
		},
	}

	start := time.Now()
	raw, err := p.http.PostJSON(ctx, p.generateContentEndpoint(), p.headers(), reqBody)
	if err != nil {
		return nil, err
	}

	if p.log != nil {
		p.log.Debug("gemini provider response",
			"toolName", request.ToolName,
			"elapsed", time.Since(start),
		)
	}

	var decoded geminiGenerateContentResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("decode gemini response: %w", err)
	}
	content, err := extractGeminiJSON(decoded)
	if err != nil {
		return nil, fmt.Errorf("extract gemini content: %w", err)
	}
	content = normalizeJSONPayload(content)
	if !json.Valid([]byte(content)) {
		return nil, fmt.Errorf("gemini provider returned non-json content: %s", content)
	}
	return json.RawMessage(content), nil
}

// ---- internal helpers ----

func extractGeminiJSON(response geminiGenerateContentResponse) (string, error) {
	if response.Error != nil && strings.TrimSpace(response.Error.Message) != "" {
		return "", errorf("%s", response.Error.Message)
	}
	if len(response.Candidates) == 0 {
		return "", errorf("no candidates in gemini response")
	}
	for _, part := range response.Candidates[0].Content.Parts {
		if strings.TrimSpace(part.Text) != "" {
			return part.Text, nil
		}
	}
	return "", errorf("empty gemini content")
}
