package workflow_test

import (
	"strings"
	"testing"

	"github.com/autosdk/ppp/server-agent/internal/workflow"
)

func TestInterpolateStrict_UnclosedPlaceholder(t *testing.T) {
	// Unclosed "{{input.key" (no closing "}}") must still return an error and
	// must include the raw unclosed placeholder text in the message.
	_, err := workflow.InterpolateStrict("value={{input.key", map[string]string{})
	if err == nil {
		t.Fatal("expected error for unclosed placeholder, got nil")
	}
	if !strings.Contains(err.Error(), "{{input.key") {
		t.Fatalf("expected placeholder text in error, got: %v", err)
	}
}

func TestInterpolateStrict_ClosedPlaceholder_Missing(t *testing.T) {
	// Well-formed placeholder whose key is absent from inputs → error.
	_, err := workflow.InterpolateStrict("value={{input.missing}}", map[string]string{})
	if err == nil {
		t.Fatal("expected error for unresolved placeholder, got nil")
	}
}

func TestInterpolateStrict_ClosedPlaceholder_Resolved(t *testing.T) {
	// All placeholders resolved → no error, substituted result returned.
	got, err := workflow.InterpolateStrict("hello={{input.name}}", map[string]string{"name": "world"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "hello=world" {
		t.Fatalf("expected %q, got %q", "hello=world", got)
	}
}

func TestInterpolateStrict_NoPlaceholder(t *testing.T) {
	// Template with no placeholder → unchanged, no error.
	got, err := workflow.InterpolateStrict("plain string", map[string]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "plain string" {
		t.Fatalf("expected %q, got %q", "plain string", got)
	}
}
