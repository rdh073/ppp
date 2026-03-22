package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// DallE3ImageGenerator calls OpenAI DALL-E 3 to generate an image.
type DallE3ImageGenerator struct {
	apiKey     string
	httpClient *http.Client
}

func NewDallE3ImageGenerator() (*DallE3ImageGenerator, error) {
	key := os.Getenv("AUTO_TOOL_OPENAI_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("AUTO_TOOL_OPENAI_API_KEY is not set")
	}
	return &DallE3ImageGenerator{
		apiKey:     key,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (g *DallE3ImageGenerator) GenerateImage(ctx context.Context, prompt string) ([]byte, error) {
	body, _ := json.Marshal(map[string]any{
		"model":           "dall-e-3",
		"prompt":          prompt,
		"n":               1,
		"size":            "1024x1024",
		"response_format": "url",
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/images/generations", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+g.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dalle3 request: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dalle3: %d %s", resp.StatusCode, string(raw))
	}

	var out struct {
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Data) == 0 {
		return nil, fmt.Errorf("dalle3: unexpected response")
	}

	imgReq, err := http.NewRequestWithContext(ctx, http.MethodGet, out.Data[0].URL, nil)
	if err != nil {
		return nil, fmt.Errorf("build image download request: %w", err)
	}
	imgResp, err := g.httpClient.Do(imgReq)
	if err != nil {
		return nil, fmt.Errorf("download image: %w", err)
	}
	defer imgResp.Body.Close()
	return io.ReadAll(imgResp.Body)
}
