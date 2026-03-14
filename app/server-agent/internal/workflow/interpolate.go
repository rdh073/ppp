package workflow

import (
	"fmt"
	"strings"
)

// Interpolate replaces {{input.key}} placeholders in template with values
// from inputs. Missing keys are replaced with an empty string.
// Returns an error if a placeholder references an unknown key and
// requireAll is true.
func Interpolate(template string, inputs map[string]string) string {
	result := template
	for k, v := range inputs {
		result = strings.ReplaceAll(result, fmt.Sprintf("{{input.%s}}", k), v)
	}
	return result
}

// InterpolateStrict returns an error if any {{input.key}} placeholder
// remains unresolved after substitution.
func InterpolateStrict(template string, inputs map[string]string) (string, error) {
	result := Interpolate(template, inputs)
	if idx := strings.Index(result, "{{input."); idx >= 0 {
		end := strings.Index(result[idx:], "}}")
		placeholder := result[idx:]
		if end >= 0 {
			placeholder = result[idx : idx+end+2]
		}
		return "", fmt.Errorf("unresolved placeholder %s", placeholder)
	}
	return result, nil
}
