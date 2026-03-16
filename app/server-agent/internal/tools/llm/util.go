package llm

import (
	"encoding/json"
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

func extractChoiceContent(response openAIChatCompletionsResponse) (string, error) {
	if response.Error != nil && strings.TrimSpace(response.Error.Message) != "" {
		return "", errorf("%s", response.Error.Message)
	}
	if len(response.Choices) == 0 {
		return "", errorf("no choices in llm response")
	}
	raw := response.Choices[0].Message.Content
	if len(raw) == 0 {
		return "", errorf("empty llm content")
	}

	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return text, nil
	}

	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &parts); err == nil {
		var b strings.Builder
		for _, part := range parts {
			if strings.TrimSpace(part.Text) != "" {
				b.WriteString(part.Text)
			}
		}
		if b.Len() == 0 {
			return "", errorf("empty llm content parts")
		}
		return b.String(), nil
	}

	return "", errorf("unsupported llm content shape")
}

func extractAnthropicToolInput(response anthropicMessagesResponse) (json.RawMessage, error) {
	if response.Error != nil && strings.TrimSpace(response.Error.Message) != "" {
		return nil, errorf("%s", response.Error.Message)
	}
	for _, part := range response.Content {
		if part.Type == "tool_use" && len(part.Input) > 0 {
			return append(json.RawMessage(nil), part.Input...), nil
		}
	}
	return nil, errorf("no tool_use content in anthropic response")
}

func extractGeminiJSON(response geminiGenerateContentResponse) (string, error) {
	if response.Error != nil && strings.TrimSpace(response.Error.Message) != "" {
		return "", errorf("%s", response.Error.Message)
	}
	if len(response.Candidates) == 0 {
		return "", errorf("no candidates in gemini response")
	}
	for _, part := range response.Candidates[0].Content.Parts {
		if strings.TrimSpace(part.Text) != "" {
			return part.Text, nil
		}
	}
	return "", errorf("empty gemini content")
}

func extractDeepSeekToolArguments(response deepSeekChatCompletionsResponse) (string, error) {
	if response.Error != nil && strings.TrimSpace(response.Error.Message) != "" {
		return "", errorf("%s", response.Error.Message)
	}
	if len(response.Choices) == 0 {
		return "", errorf("no choices in deepseek response")
	}
	for _, toolCall := range response.Choices[0].Message.ToolCalls {
		if strings.TrimSpace(toolCall.Function.Arguments) != "" {
			return toolCall.Function.Arguments, nil
		}
	}
	return "", errorf("no tool call arguments in deepseek response")
}

func errorf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}
