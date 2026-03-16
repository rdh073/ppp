package llm

// Anthropic multimodal request wire types.
// anthropicVisionMsg differs from anthropicMessage in that Content is a typed
// slice (for image + text parts) rather than a plain string.

type anthropicVisionRequest struct {
	Model       string               `json:"model"`
	System      string               `json:"system,omitempty"`
	MaxTokens   int                  `json:"max_tokens"`
	Messages    []anthropicVisionMsg `json:"messages"`
	Tools       []anthropicTool      `json:"tools"`
	ToolChoice  anthropicToolChoice  `json:"tool_choice"`
	Temperature float64              `json:"temperature"`
}

type anthropicVisionMsg struct {
	Role    string                 `json:"role"`
	Content []anthropicContentPart `json:"content"`
}

type anthropicContentPart struct {
	Type   string              `json:"type"`             // "text" | "image"
	Text   string              `json:"text,omitempty"`
	Source *anthropicImgSource `json:"source,omitempty"` // present when Type=="image"
}

type anthropicImgSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/jpeg" | "image/png" | ...
	Data      string `json:"data"`       // raw base64 string (no data-URL prefix)
}
