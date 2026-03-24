package llm

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"

	"github.com/Ingenimax/agent-sdk-go/pkg/interfaces"
	"github.com/Ingenimax/agent-sdk-go/pkg/llm/anthropic"
	"github.com/Ingenimax/agent-sdk-go/pkg/llm/gemini"
	openaiSDK "github.com/Ingenimax/agent-sdk-go/pkg/llm/openai"
)

// ingemaxLoopAdapter implements AgentLoop using the Ingenimax Agent SDK LLM clients.
// It delegates the multi-turn tool-calling loop to the provider's GenerateWithTools
// and adapts our AgentTool/ToolExecutor contract into interfaces.Tool.
type ingemaxLoopAdapter struct {
	llm interfaces.LLM
	log *slog.Logger
}

func (a *ingemaxLoopAdapter) Run(ctx context.Context, req AgentLoopRequest, exec ToolExecutor) (AgentLoopResult, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var agentDone atomic.Bool

	// Build adapters: one dynamicTool per AgentTool.
	tools := make([]interfaces.Tool, len(req.Tools))
	for i, t := range req.Tools {
		tools[i] = &dynamicTool{
			def:       t,
			exec:      exec,
			cancel:    cancel,
			agentDone: &agentDone,
		}
	}

	genOpts := []interfaces.GenerateOption{
		interfaces.WithSystemMessage(req.SystemPrompt),
		interfaces.WithMaxIterations(req.MaxSteps),
	}

	if req.OnChunk != nil {
		if streamingLLM, ok := a.llm.(interfaces.StreamingLLM); ok {
			eventCh, err := streamingLLM.GenerateWithToolsStream(ctx, req.Goal, tools, genOpts...)
			if err != nil && !errors.Is(err, context.Canceled) {
				return AgentLoopResult{}, err
			}
			for event := range eventCh {
				if event.Type == interfaces.StreamEventContentDelta && event.Content != "" {
					req.OnChunk(event.Content)
				}
			}
		} else {
			// Provider does not support streaming; fall back to blocking.
			if _, err := a.llm.GenerateWithTools(ctx, req.Goal, tools, genOpts...); err != nil && !errors.Is(err, context.Canceled) {
				return AgentLoopResult{}, err
			}
		}
	} else {
		if _, err := a.llm.GenerateWithTools(ctx, req.Goal, tools, genOpts...); err != nil && !errors.Is(err, context.Canceled) {
			return AgentLoopResult{}, err
		}
	}

	return AgentLoopResult{Done: agentDone.Load()}, nil
}

// dynamicTool adapts AgentTool + ToolExecutor into the interfaces.Tool interface
// expected by the Ingenimax SDK.
type dynamicTool struct {
	def       AgentTool
	exec      ToolExecutor
	cancel    context.CancelFunc
	agentDone *atomic.Bool
}

func (t *dynamicTool) Name() string        { return t.def.Name }
func (t *dynamicTool) Description() string { return t.def.Description }

// Run delegates to Execute (Ingenimax may call either).
func (t *dynamicTool) Run(ctx context.Context, input string) (string, error) {
	return t.Execute(ctx, input)
}

// Parameters converts the JSON Schema in AgentTool.InputSchema into the
// map[string]ParameterSpec format expected by the SDK.
func (t *dynamicTool) Parameters() map[string]interfaces.ParameterSpec {
	return schemaPropertiesToParameterSpec(t.def.InputSchema)
}

// Execute bridges Ingenimax's string-based args to our json.RawMessage ToolExecutor.
// When the executor signals ErrAgentDone it cancels the context so the SDK
// loop terminates after the current iteration completes.
func (t *dynamicTool) Execute(ctx context.Context, args string) (string, error) {
	result, err := t.exec(ctx, t.def.Name, json.RawMessage(args))
	if errors.Is(err, ErrAgentDone) {
		t.agentDone.Store(true)
		t.cancel()
		return string(result), nil // return nil so SDK records a clean tool result
	}
	return string(result), err
}

// schemaPropertiesToParameterSpec converts a flat JSON Schema object into the
// map[string]ParameterSpec format used by the Ingenimax SDK.
// Nested/complex schemas are normalised to type "object".
func schemaPropertiesToParameterSpec(raw json.RawMessage) map[string]interfaces.ParameterSpec {
	if len(raw) == 0 {
		return nil
	}
	var schema struct {
		Properties map[string]struct {
			Type        string        `json:"type"`
			Description string        `json:"description"`
			Enum        []interface{} `json:"enum,omitempty"`
			Default     interface{}   `json:"default,omitempty"`
			Items       *struct {
				Type string        `json:"type"`
				Enum []interface{} `json:"enum,omitempty"`
			} `json:"items,omitempty"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil || len(schema.Properties) == 0 {
		return nil
	}

	requiredSet := make(map[string]bool, len(schema.Required))
	for _, r := range schema.Required {
		requiredSet[r] = true
	}

	specs := make(map[string]interfaces.ParameterSpec, len(schema.Properties))
	for name, prop := range schema.Properties {
		spec := interfaces.ParameterSpec{
			Type:        prop.Type,
			Description: prop.Description,
			Required:    requiredSet[name],
			Default:     prop.Default,
			Enum:        prop.Enum,
		}
		if prop.Items != nil {
			spec.Items = &interfaces.ParameterSpec{
				Type: prop.Items.Type,
				Enum: prop.Items.Enum,
			}
		}
		if spec.Type == "" {
			spec.Type = "object"
		}
		specs[name] = spec
	}
	return specs
}

// ---- Provider-specific factory functions ----

func newIngemaxAnthropicLoop(cfg ModelToolConfig, log *slog.Logger) AgentLoop {
	if strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.Model) == "" {
		return nil
	}
	opts := []anthropic.Option{anthropic.WithModel(cfg.Model)}
	if u := strings.TrimSpace(cfg.APIURL); u != "" {
		opts = append(opts, anthropic.WithBaseURL(u))
	}
	return &ingemaxLoopAdapter{
		llm: anthropic.NewClient(cfg.APIKey, opts...),
		log: log,
	}
}

func newIngemaxGeminiLoop(cfg ModelToolConfig, log *slog.Logger) AgentLoop {
	if strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.Model) == "" {
		return nil
	}
	opts := []gemini.Option{
		gemini.WithAPIKey(cfg.APIKey),
		gemini.WithModel(cfg.Model),
	}
	if u := strings.TrimSpace(cfg.APIURL); u != "" {
		opts = append(opts, gemini.WithBaseURL(u))
	}
	client, err := gemini.NewClient(context.Background(), opts...)
	if err != nil {
		if log != nil {
			log.Error("failed to create gemini client", "err", err)
		}
		return nil
	}
	return &ingemaxLoopAdapter{llm: client, log: log}
}

func newIngemaxOpenAILoop(cfg ModelToolConfig, log *slog.Logger) AgentLoop {
	if strings.TrimSpace(cfg.APIKey) == "" || strings.TrimSpace(cfg.Model) == "" {
		return nil
	}
	opts := []openaiSDK.Option{openaiSDK.WithModel(cfg.Model)}
	if u := strings.TrimSpace(cfg.APIURL); u != "" {
		opts = append(opts, openaiSDK.WithBaseURL(u))
	}
	return &ingemaxLoopAdapter{
		llm: openaiSDK.NewClient(cfg.APIKey, opts...),
		log: log,
	}
}
