package tools

// llm_compat.go re-exports symbols that moved to internal/tools/llm so that
// existing callers (llm_test.go, catalog_loader.go) continue to compile unchanged.

import (
	"log/slog"

	"github.com/autosdk/ppp/server-agent/internal/tools/llm"
)

// Type aliases — identical to the llm package types.
type ModelToolConfig = llm.ModelToolConfig
type JSONModelClient = llm.JSONModelClient
type JSONModelRequest = llm.JSONModelRequest
type VisionModelClient = llm.VisionModelClient
type VisionAnalyzeRequest = llm.VisionAnalyzeRequest

// Forwarding constructors — keep existing call sites working.

func NewOpenAICompatibleJSONClient(cfg llm.ModelToolConfig, log *slog.Logger) llm.JSONModelClient {
	return llm.NewOpenAICompatibleJSONClient(cfg, log)
}

func NewAnthropicMessagesJSONClient(cfg llm.ModelToolConfig, apiVersion string, log *slog.Logger) llm.JSONModelClient {
	return llm.NewAnthropicMessagesJSONClient(cfg, apiVersion, log)
}

func NewGeminiGenerateContentJSONClient(cfg llm.ModelToolConfig, log *slog.Logger) llm.JSONModelClient {
	return llm.NewGeminiGenerateContentJSONClient(cfg, log)
}

func NewDeepSeekChatCompletionsJSONClient(cfg llm.ModelToolConfig, log *slog.Logger) llm.JSONModelClient {
	return llm.NewDeepSeekChatCompletionsJSONClient(cfg, log)
}

func NewAnthropicVisionClient(cfg llm.ModelToolConfig, apiVersion string, log *slog.Logger) llm.VisionModelClient {
	return llm.NewAnthropicVisionClient(cfg, apiVersion, log)
}
