package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/Ingenimax/agent-sdk-go/pkg/interfaces"
	"github.com/Ingenimax/agent-sdk-go/pkg/llm/anthropic"
	openaiSDK "github.com/Ingenimax/agent-sdk-go/pkg/llm/openai"
)

const captionSystemPrompt = "You are an Instagram content creator. Write an engaging, concise Instagram caption. Reply with only the caption text, no quotes, no explanation."

// CaptionProviderConfig holds the endpoint and credentials for a single LLM
// provider used by the caption generator.
type CaptionProviderConfig struct {
	Kind       string // "anthropic" | "openai" (determines request format)
	APIURL     string
	APIKey     string
	Model      string
	APIVersion string // kept for config compatibility; Ingenimax manages versioning internally
}

// LLMCaptionGenerator tries the primary provider first, then the fallback.
type LLMCaptionGenerator struct {
	providers []interfaces.LLM // non-nil entries in priority order
}

// NewLLMCaptionGenerator creates a caption generator using the provided config.
// primary is tried first; fallback is tried when primary fails or is unconfigured.
func NewLLMCaptionGenerator(primary, fallback CaptionProviderConfig) *LLMCaptionGenerator {
	var providers []interfaces.LLM
	if p := buildCaptionLLM(primary); p != nil {
		providers = append(providers, p)
	}
	if f := buildCaptionLLM(fallback); f != nil {
		providers = append(providers, f)
	}
	return &LLMCaptionGenerator{providers: providers}
}

func buildCaptionLLM(cfg CaptionProviderConfig) interfaces.LLM {
	if cfg.APIKey == "" || cfg.Model == "" {
		return nil
	}
	switch cfg.Kind {
	case "anthropic":
		opts := []anthropic.Option{anthropic.WithModel(cfg.Model)}
		if cfg.APIURL != "" {
			opts = append(opts, anthropic.WithBaseURL(cfg.APIURL))
		}
		return anthropic.NewClient(cfg.APIKey, opts...)
	default: // openai-compatible
		opts := []openaiSDK.Option{openaiSDK.WithModel(cfg.Model)}
		if cfg.APIURL != "" {
			opts = append(opts, openaiSDK.WithBaseURL(cfg.APIURL))
		}
		return openaiSDK.NewClient(cfg.APIKey, opts...)
	}
}

func (g *LLMCaptionGenerator) GenerateCaption(ctx context.Context, prompt string) (string, error) {
	opts := []interfaces.GenerateOption{interfaces.WithSystemMessage(captionSystemPrompt)}
	for _, p := range g.providers {
		result, err := p.Generate(ctx, prompt, opts...)
		if err == nil {
			return strings.TrimSpace(result), nil
		}
	}
	return "", fmt.Errorf("no LLM provider configured for text generation")
}

func (g *LLMCaptionGenerator) GenerateCaptionStream(ctx context.Context, prompt string, onChunk func(string)) error {
	opts := []interfaces.GenerateOption{interfaces.WithSystemMessage(captionSystemPrompt)}
	for _, p := range g.providers {
		streamingLLM, ok := p.(interfaces.StreamingLLM)
		if !ok {
			continue
		}
		eventCh, err := streamingLLM.GenerateStream(ctx, prompt, opts...)
		if err != nil {
			continue
		}
		for event := range eventCh {
			if event.Type == interfaces.StreamEventContentDelta && event.Content != "" {
				onChunk(event.Content)
			}
		}
		return nil
	}
	return fmt.Errorf("no streaming LLM provider configured for text generation")
}
