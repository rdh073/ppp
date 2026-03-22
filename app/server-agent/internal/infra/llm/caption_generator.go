package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// LLMCaptionGenerator tries Anthropic first, then OpenAI.
type LLMCaptionGenerator struct {
	httpClient *http.Client
}

func NewLLMCaptionGenerator() *LLMCaptionGenerator {
	return &LLMCaptionGenerator{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (g *LLMCaptionGenerator) GenerateCaption(ctx context.Context, prompt string) (string, error) {
	if key := os.Getenv("AUTO_TOOL_ANTHROPIC_API_KEY"); key != "" {
		model := os.Getenv("AUTO_TOOL_ANTHROPIC_MODEL")
		if model == "" {
			model = "claude-3-5-haiku-20241022"
		}
		if caption, err := g.anthropic(ctx, key, model, prompt); err == nil {
			return caption, nil
		}
	}
	if key := os.Getenv("AUTO_TOOL_OPENAI_API_KEY"); key != "" {
		model := os.Getenv("AUTO_TOOL_OPENAI_MODEL")
		if model == "" {
			model = "gpt-4o-mini"
		}
		if caption, err := g.openai(ctx, key, model, prompt); err == nil {
			return caption, nil
		}
	}
	return "", fmt.Errorf("no LLM provider configured (set AUTO_TOOL_ANTHROPIC_API_KEY or AUTO_TOOL_OPENAI_API_KEY)")
}

func (g *LLMCaptionGenerator) anthropic(ctx context.Context, apiKey, model, prompt string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": 512,
		"system":     "You are an Instagram content creator. Write an engaging, concise Instagram caption. Reply with only the caption text, no quotes, no explanation.",
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
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

func (g *LLMCaptionGenerator) openai(ctx context.Context, apiKey, model, prompt string) (string, error) {
	body, _ := json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "system", "content": "You are an Instagram content creator. Write an engaging, concise Instagram caption. Reply with only the caption text, no quotes, no explanation."},
			{"role": "user", "content": prompt},
		},
		"max_tokens": 512,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
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
