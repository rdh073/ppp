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
	"net/url"
	"strings"
	"time"
)

// geminiAgentLoop implements AgentLoop using the Gemini generateContent API
// with native multi-turn function calling (functionCallingConfig mode: "ANY").
type geminiAgentLoop struct {
	apiURL     string
	apiKey     string
	model      string
	httpClient *http.Client
	log        *slog.Logger
}

// NewGeminiAgentLoop returns an AgentLoop backed by the Gemini generateContent API.
// Returns nil if APIKey or Model are empty (handler returns 503).
func NewGeminiAgentLoop(cfg ModelToolConfig, log *slog.Logger) AgentLoop {
	if strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.Model) == "" {
		return nil
	}
	apiURL := strings.TrimSpace(cfg.APIURL)
	if apiURL == "" {
		apiURL = "https://generativelanguage.googleapis.com/v1beta"
	}
	return &geminiAgentLoop{
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

type geminiAgentContent struct {
	Role  string           `json:"role"`
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

// ---- Run implementation ----

func (g *geminiAgentLoop) Run(ctx context.Context, req AgentLoopRequest, exec ToolExecutor) (AgentLoopResult, error) {
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

		raw, err := g.post(ctx, body)
		if err != nil {
			return AgentLoopResult{Steps: step - 1}, err
		}

		var resp geminiAgentRespBody
		if err := json.Unmarshal(raw, &resp); err != nil {
			return AgentLoopResult{Steps: step - 1}, fmt.Errorf("decode agent response: %w", err)
		}
		if resp.Error != nil {
			return AgentLoopResult{Steps: step - 1}, fmt.Errorf("gemini agent: %s", resp.Error.Message)
		}
		if len(resp.Candidates) == 0 {
			break
		}

		// Find the first functionCall part in the response.
		modelContent := resp.Candidates[0].Content
		var fc *geminiFunctionCall
		for _, part := range modelContent.Parts {
			if part.FunctionCall != nil {
				fc = part.FunctionCall
				break
			}
		}
		if fc == nil {
			// No function call — model declined to call a tool.
			break
		}

		// Append model response to history.
		contents = append(contents, modelContent)

		if g.log != nil {
			g.log.Debug("agent loop step", "step", step, "tool", fc.Name)
		}

		// Marshal the function call args to json.RawMessage for the executor.
		argsJSON, _ := json.Marshal(fc.Args)

		// Execute the tool via the caller-supplied executor.
		toolResult, err := exec(ctx, fc.Name, argsJSON)
		if errors.Is(err, ErrAgentDone) {
			var doneInput struct {
				Reason string `json:"reason"`
			}
			_ = json.Unmarshal(argsJSON, &doneInput)
			return AgentLoopResult{Done: true, Reason: doneInput.Reason, Steps: step}, nil
		}

		// Build the functionResponse user turn.
		var responseMap map[string]any
		switch {
		case err != nil:
			responseMap = map[string]any{"error": err.Error()}
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

func (g *geminiAgentLoop) endpoint() string {
	base := strings.TrimRight(g.apiURL, "/")
	return base + "/models/" + url.PathEscape(g.model) + ":generateContent"
}

func (g *geminiAgentLoop) post(ctx context.Context, body any) ([]byte, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal agent request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.endpoint(), bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("build agent request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", g.apiKey)

	resp, err := g.httpClient.Do(httpReq)
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
		return nil, MarkToolRetryable(fmt.Errorf("gemini agent status %d: %s", resp.StatusCode, CompactProviderError(respBody)))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("gemini agent status %d: %s", resp.StatusCode, CompactProviderError(respBody))
	}
	return respBody, nil
}
