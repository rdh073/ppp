package tools

import (
	"errors"
	"log/slog"
)

// NewDefaultToolRegistry builds the production tool registry: deterministic
// local tools first, then model-backed tools behind the same boundary.
func NewDefaultToolRegistry(log *slog.Logger, cfg ModelToolConfig) ToolRegistry {
	local := NewLocalToolRegistry()

	var client JSONModelClient
	if cfg.Enabled() {
		client = NewOpenAICompatibleJSONClient(cfg, log)
		if log != nil {
			log.Info("model-backed tools enabled", "model", cfg.Model, "apiUrl", cfg.APIURL)
		}
	} else if log != nil {
		log.Info("model-backed tools disabled", "reason", "AUTO_TOOL_LLM_API_URL or AUTO_TOOL_LLM_MODEL not set")
	}

	return NewCompositeToolRegistry(local, NewModelToolRegistry(log, client))
}

// NewModelToolRegistry registers the model-backed tools. A nil client keeps the
// tools visible to workflows but disabled, allowing explicit workflow fallback.
func NewModelToolRegistry(log *slog.Logger, client JSONModelClient) StaticToolRegistry {
	return NewStaticToolRegistry(ModelToolDefinitions(log, client)...)
}

func ModelToolDefinitions(_ *slog.Logger, _ JSONModelClient) []ToolDefinition {
	return nil
}

func errorsIsRetryable(err error) bool {
	return errors.Is(err, ErrToolRetryable)
}
