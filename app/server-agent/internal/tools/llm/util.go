package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

func normalizeJSONPayload(content string) string {
	trimmed := strings.TrimSpace(content)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	return strings.TrimSpace(trimmed)
}

func schemaNameForTool(toolName string) string {
	var b strings.Builder
	for _, r := range toolName {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	name := strings.Trim(b.String(), "_")
	if name == "" {
		return "tool_output"
	}
	return name
}

// CompactProviderError extracts a brief human-readable error from a provider
// response body. Used by both LLM and HTTP provider adapters.
func CompactProviderError(raw []byte) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return http.StatusText(http.StatusBadGateway)
	}
	var payload struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil && payload.Error != nil && strings.TrimSpace(payload.Error.Message) != "" {
		return strings.TrimSpace(payload.Error.Message)
	}
	return trimmed
}

func defaultSystemPrompt(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "You are a backend workflow tool. Return a single JSON object that satisfies the schema exactly."
	}
	return value
}

func errorf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}

// isErrAgentDone reports whether err is ErrAgentDone (avoids importing errors in every provider).
func isErrAgentDone(err error) bool {
	return errors.Is(err, ErrAgentDone)
}
