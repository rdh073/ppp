package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// CaptionProviderConfig holds the endpoint and credentials for a single LLM
// provider used by the caption generator.
type CaptionProviderConfig struct {
	Kind       string // "anthropic" | "openai" (determines request format)
	APIURL     string
	APIKey     string
	Model      string
	APIVersion string // anthropic only
}

// LLMCaptionGenerator tries the primary provider first, then the fallback.
type LLMCaptionGenerator struct {
	primary    CaptionProviderConfig
	fallback   CaptionProviderConfig
	httpClient *http.Client
}

// NewLLMCaptionGenerator creates a caption generator using the provided config.
// primary is tried first; fallback is tried when primary fails or is unconfigured.
func NewLLMCaptionGenerator(primary, fallback CaptionProviderConfig) *LLMCaptionGenerator {
	return &LLMCaptionGenerator{
		primary:    primary,
		fallback:   fallback,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (g *LLMCaptionGenerator) GenerateCaption(ctx context.Context, prompt string) (string, error) {
	providers := []CaptionProviderConfig{g.primary, g.fallback}
	for _, p := range providers {
		if p.APIKey == "" {
			continue
		}
		var caption string
		var err error
		switch p.Kind {
		case "anthropic":
			caption, err = g.anthropic(ctx, p, prompt)
		default: // openai-compatible
			caption, err = g.openai(ctx, p, prompt)
		}
		if err == nil {
			return caption, nil
		}
	}
	return "", fmt.Errorf("no LLM provider configured for text generation")
}

func (g *LLMCaptionGenerator) anthropic(ctx context.Context, cfg CaptionProviderConfig, prompt string) (string, error) {
	model := cfg.Model
	if model == "" {
		model = "claude-3-5-haiku-20241022"
	}
	apiVersion := cfg.APIVersion
	if apiVersion == "" {
		apiVersion = "2023-06-01"
	}
	body, _ := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": 512,
		"system":     "You are an Instagram content creator. Write an engaging, concise Instagram caption. Reply with only the caption text, no quotes, no explanation.",
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.APIURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("x-api-key", cfg.APIKey)
	req.Header.Set("anthropic-version", apiVersion)
	req.Header.Set("content-type", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("anthropic: %d %s", resp.StatusCode, string(raw))
	}

	var out struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Content) == 0 {
		return "", fmt.Errorf("anthropic: unexpected response")
	}
	return strings.TrimSpace(out.Content[0].Text), nil
}

func (g *LLMCaptionGenerator) openai(ctx context.Context, cfg CaptionProviderConfig, prompt string) (string, error) {
	model := cfg.Model
	if model == "" {
		model = "gpt-4o-mini"
	}
	apiURL := cfg.APIURL
	if apiURL == "" {
		apiURL = "https://api.openai.com/v1/chat/completions"
	}
	body, _ := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "You are an Instagram content creator. Write an engaging, concise Instagram caption. Reply with only the caption text, no quotes, no explanation."},
			{"role": "user", "content": prompt},
		},
		"max_tokens": 512,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("openai: %d %s", resp.StatusCode, string(raw))
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
		return "", fmt.Errorf("openai: unexpected response")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}
