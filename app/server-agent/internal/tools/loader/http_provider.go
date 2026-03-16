package loader

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/tools"
	"github.com/autosdk/ppp/server-agent/internal/tools/llm"
)

type httpProvider struct {
	cfg            providerConfig
	baseURL        string
	apiKey         string
	httpClient     *http.Client
	discovered     map[string]remoteProviderTool
	disabledReason string
}

func (p *httpProvider) Build(ctx context.Context, manifest catalogToolManifest) (tools.ToolDefinition, error) {
	if p.disabledReason != "" {
		return disabledToolDefinition(manifest.Manifest, fmt.Sprintf("provider %s %s", p.cfg.ID, p.disabledReason))
	}
	remoteTools, err := p.discovery(ctx)
	if err != nil {
		return tools.ToolDefinition{}, err
	}
	remote, ok := remoteTools[manifest.Manifest.ProviderToolName]
	if !ok {
		return tools.ToolDefinition{}, fmt.Errorf("remote tool %s not found", manifest.Manifest.ProviderToolName)
	}
	merged := manifest.Manifest
	if merged.Description == "" {
		merged.Description = remote.Description
	}
	if remote.Deterministic != nil && !merged.Deterministic {
		merged.Deterministic = *remote.Deterministic
	}
	if merged.Timeout == 0 && strings.TrimSpace(remote.Timeout) != "" {
		parsed, err := time.ParseDuration(strings.TrimSpace(remote.Timeout))
		if err != nil {
			return tools.ToolDefinition{}, fmt.Errorf("remote timeout: %w", err)
		}
		merged.Timeout = parsed
	}
	if merged.RetryBudget == 0 && remote.RetryBudget != nil {
		merged.RetryBudget = *remote.RetryBudget
	}
	if len(merged.InputSchema) == 0 {
		merged.InputSchema = remote.InputSchema
	} else if len(remote.InputSchema) > 0 && !jsonSchemaEquivalent(merged.InputSchema, remote.InputSchema) {
		return tools.ToolDefinition{}, fmt.Errorf("input schema mismatch with remote tool %s", remote.Name)
	}
	if len(merged.OutputSchema) == 0 {
		merged.OutputSchema = remote.OutputSchema
	} else if len(remote.OutputSchema) > 0 && !jsonSchemaEquivalent(merged.OutputSchema, remote.OutputSchema) {
		return tools.ToolDefinition{}, fmt.Errorf("output schema mismatch with remote tool %s", remote.Name)
	}
	validateParams, err := tools.CompileSchemaValidator(merged.InputSchema)
	if err != nil {
		return tools.ToolDefinition{}, err
	}
	validateResult, err := tools.CompileSchemaValidator(merged.OutputSchema)
	if err != nil {
		return tools.ToolDefinition{}, err
	}
	return tools.ToolDefinition{
		Manifest:       merged,
		ValidateParams: validateParams,
		ValidateResult: validateResult,
		Handler: func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
			return p.invoke(ctx, merged.ProviderToolName, params)
		},
	}, nil
}

func (p *httpProvider) discovery(ctx context.Context) (map[string]remoteProviderTool, error) {
	if p.discovered != nil {
		return p.discovered, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/v1/tools", nil)
	if err != nil {
		return nil, err
	}
	if p.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	response, err := p.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var discovered []remoteProviderTool
	if err := json.Unmarshal(body, &discovered); err != nil {
		var envelope remoteProviderListEnvelope
		if errEnvelope := json.Unmarshal(body, &envelope); errEnvelope != nil {
			return nil, fmt.Errorf("decode tool list: %w", err)
		}
		discovered = envelope.Items
	}
	p.discovered = make(map[string]remoteProviderTool, len(discovered))
	for _, tool := range discovered {
		p.discovered[tool.Name] = tool
	}
	return p.discovered, nil
}

func (p *httpProvider) invoke(ctx context.Context, remoteToolName string, params json.RawMessage) (json.RawMessage, error) {
	endpoint := p.baseURL + "/v1/tools/" + url.PathEscape(remoteToolName) + ":invoke"
	body, err := json.Marshal(remoteInvokeRequest{
		CallID: fmt.Sprintf("call-%d", time.Now().UnixNano()),
		Params: append(json.RawMessage(nil), params...),
	})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		request.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	response, err := p.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, tools.MarkToolRetryable(fmt.Errorf("remote provider transport: %w", err))
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, tools.MarkToolRetryable(fmt.Errorf("remote provider read: %w", err))
	}
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= http.StatusInternalServerError {
		return nil, tools.MarkToolRetryable(fmt.Errorf("remote provider status %d: %s", response.StatusCode, llm.CompactProviderError(responseBody)))
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("remote provider status %d: %s", response.StatusCode, llm.CompactProviderError(responseBody))
	}
	var decoded remoteInvokeResponse
	if err := json.Unmarshal(responseBody, &decoded); err != nil {
		return nil, fmt.Errorf("decode invoke response: %w", err)
	}
	if decoded.Error != nil {
		wrapped := fmt.Errorf("remote provider error %s: %s", strings.TrimSpace(decoded.Error.Code), strings.TrimSpace(decoded.Error.Message))
		if decoded.Error.Retryable {
			return nil, tools.MarkToolRetryable(wrapped)
		}
		return nil, wrapped
	}
	if len(decoded.Result) == 0 {
		return nil, fmt.Errorf("remote provider returned empty result")
	}
	return decoded.Result, nil
}
