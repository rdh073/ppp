package llm

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// Sentinel errors — owned here so the llm adapters don't import the parent package.
var (
	ErrToolDisabled  = errors.New("tool disabled")
	ErrToolRetryable = errors.New("tool retryable")
)

// retryableToolError wraps an error to signal the caller may retry.
type retryableToolError struct{ cause error }

func (e retryableToolError) Error() string      { return e.cause.Error() }
func (e retryableToolError) Unwrap() error      { return e.cause }
func (e retryableToolError) Is(target error) bool { return target == ErrToolRetryable }

// MarkToolRetryable wraps err so callers can detect it as retryable.
func MarkToolRetryable(err error) error { return retryableToolError{cause: err} }

// IsToolRetryable reports whether err is a retryable tool error.
func IsToolRetryable(err error) bool { return errors.Is(err, ErrToolRetryable) }

// ModelToolConfig contains the runtime wiring for model-backed tools.
type ModelToolConfig struct {
	APIURL string
	APIKey string
	Model  string
}

func (c ModelToolConfig) Enabled() bool {
	return strings.TrimSpace(c.APIURL) != "" && strings.TrimSpace(c.Model) != ""
}

// JSONModelRequest is the provider-neutral contract used by model-backed tools.
type JSONModelRequest struct {
	ToolName     string
	SystemPrompt string
	Prompt       string
	OutputSchema json.RawMessage
	Temperature  *float64
}

// JSONModelClient generates a JSON object that satisfies the supplied schema.
type JSONModelClient interface {
	GenerateJSON(ctx context.Context, request JSONModelRequest) (json.RawMessage, error)
}

// VisionAnalyzeRequest carries all runtime parameters for one vision call.
type VisionAnalyzeRequest struct {
	ToolName     string
	ImageBase64  string
	MimeType     string          // e.g. "image/jpeg"
	Prompt       string
	OutputSchema json.RawMessage // caller-declared output shape
}

// VisionModelClient sends an image + runtime prompt to a vision LLM and returns
// structured JSON that satisfies the caller-supplied output schema.
type VisionModelClient interface {
	AnalyzeImage(ctx context.Context, req VisionAnalyzeRequest) (json.RawMessage, error)
}
