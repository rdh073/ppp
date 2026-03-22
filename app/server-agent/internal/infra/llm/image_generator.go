package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ImageGeneratorConfig holds the endpoint and credentials for image generation.
type ImageGeneratorConfig struct {
	APIKey string
	Model  string // default: "dall-e-3"
	APIURL string // default: "https://api.openai.com/v1/images/generations"
}

// DallE3ImageGenerator calls OpenAI DALL-E 3 to generate an image.
type DallE3ImageGenerator struct {
	cfg        ImageGeneratorConfig
	httpClient *http.Client
}

func NewDallE3ImageGenerator(cfg ImageGeneratorConfig) (*DallE3ImageGenerator, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("image generation: api_key not configured")
	}
	if cfg.Model == "" {
		cfg.Model = "dall-e-3"
	}
	if cfg.APIURL == "" {
		cfg.APIURL = "https://api.openai.com/v1/images/generations"
	}
	return &DallE3ImageGenerator{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (g *DallE3ImageGenerator) GenerateImage(ctx context.Context, prompt string) ([]byte, error) {
	body, _ := json.Marshal(map[string]any{
		"model":           g.cfg.Model,
		"prompt":          prompt,
		"n":               1,
		"size":            "1024x1024",
		"response_format": "url",
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.cfg.APIURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+g.cfg.APIKey)
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
