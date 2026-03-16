package loader

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/autosdk/ppp/server-agent/internal/tools"
)

// buildChainToolDefinition implements the Chain of Responsibility pattern:
// members are tried in order; the fallback policy controls when the chain
// advances to the next member.
//
//   - fallback "on_disabled" (default): advance only when a member returns ErrToolDisabled
//   - fallback "on_error":              advance on any error
func buildChainToolDefinition(manifest tools.ToolManifest, members []tools.ToolDefinition, fallback string) tools.ToolDefinition {
	validateParams, _ := tools.CompileSchemaValidator(manifest.InputSchema)
	validateResult, _ := tools.CompileSchemaValidator(manifest.OutputSchema)

	shouldAdvance := func(err error) bool {
		if fallback == "on_error" {
			return true
		}
		return errors.Is(err, tools.ErrToolDisabled)
	}

	handlers := make([]func(context.Context, json.RawMessage) (json.RawMessage, error), len(members))
	for i, m := range members {
		handlers[i] = m.Handler
	}

	return tools.ToolDefinition{
		Manifest:       manifest,
		ValidateParams: validateParams,
		ValidateResult: validateResult,
		Handler: func(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
			var lastErr error
			for _, h := range handlers {
				result, err := h(ctx, params)
				if err == nil {
					return result, nil
				}
				if !shouldAdvance(err) {
					return nil, err
				}
				lastErr = err
			}
			if lastErr != nil {
				return nil, fmt.Errorf("all chain members exhausted: %w", lastErr)
			}
			return nil, fmt.Errorf("%w: no chain members", tools.ErrToolDisabled)
		},
	}
}

func disabledToolDefinition(manifest tools.ToolManifest, reason string) (tools.ToolDefinition, error) {
	validateParams, err := tools.CompileSchemaValidator(manifest.InputSchema)
	if err != nil {
		return tools.ToolDefinition{}, err
	}
	validateResult, err := tools.CompileSchemaValidator(manifest.OutputSchema)
	if err != nil {
		return tools.ToolDefinition{}, err
	}
	if strings.TrimSpace(reason) == "" {
		reason = "provider unavailable"
	}
	return tools.ToolDefinition{
		Manifest:       manifest,
		ValidateParams: validateParams,
		ValidateResult: validateResult,
		Handler: func(context.Context, json.RawMessage) (json.RawMessage, error) {
			return nil, fmt.Errorf("%w: %s", tools.ErrToolDisabled, reason)
		},
	}, nil
}

func jsonSchemaEquivalent(left, right json.RawMessage) bool {
	if len(left) == 0 || len(right) == 0 {
		return len(left) == len(right)
	}
	var leftValue any
	var rightValue any
	if err := json.Unmarshal(left, &leftValue); err != nil {
		return false
	}
	if err := json.Unmarshal(right, &rightValue); err != nil {
		return false
	}
	leftJSON, err := json.Marshal(leftValue)
	if err != nil {
		return false
	}
	rightJSON, err := json.Marshal(rightValue)
	if err != nil {
		return false
	}
	return bytes.Equal(leftJSON, rightJSON)
}
