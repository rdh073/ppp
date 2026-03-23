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

// providerHTTPClient is a shared HTTP client used by all LLM provider implementations.
// It centralises retry classification, size limits, and context-cancellation handling.
type providerHTTPClient struct {
	client *http.Client
}

func newProviderHTTPClient(timeout time.Duration) *providerHTTPClient {
	return &providerHTTPClient{client: &http.Client{Timeout: timeout}}
}

// PostJSON marshals body as JSON, POSTs it to url with the given headers,
// and returns the full response body.
// HTTP 429 and 5xx responses are wrapped with MarkToolRetryable.
func (c *providerHTTPClient) PostJSON(ctx context.Context, url string, headers map[string]string, body any) ([]byte, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, MarkToolRetryable(fmt.Errorf("transport: %w", err))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<21)) // 2 MiB cap
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, MarkToolRetryable(fmt.Errorf("read response: %w", err))
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
		return nil, MarkToolRetryable(fmt.Errorf("provider status %d: %s", resp.StatusCode, CompactProviderError(raw)))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("provider status %d: %s", resp.StatusCode, CompactProviderError(raw))
	}
	return raw, nil
}

// StreamSSE POSTs body as JSON (provider should set stream:true in body) and
// calls onEvent for each SSE "data: ..." line until the stream ends.
func (c *providerHTTPClient) StreamSSE(ctx context.Context, url string, headers map[string]string, body any, onEvent func([]byte) error) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal stream request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("build stream request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	// For streaming we use a client without timeout — the caller's ctx controls cancellation.
	streamClient := &http.Client{}
	resp, err := streamClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		return MarkToolRetryable(fmt.Errorf("stream transport: %w", err))
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return MarkToolRetryable(fmt.Errorf("provider stream status %d: %s", resp.StatusCode, CompactProviderError(raw)))
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("provider stream status %d: %s", resp.StatusCode, CompactProviderError(raw))
	}
	return ReadSSE(resp.Body, onEvent)
}
