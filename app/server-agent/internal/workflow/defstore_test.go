package workflow_test

import (
	"context"
	"strings"
	"testing"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/workflow"
)

func TestMemoryDefStorePut_RejectsInvalidWorkflow(t *testing.T) {
	store := workflow.NewMemoryDefStore()

	err := store.Put(context.Background(), "legacy", &domain.WorkflowDef{
		Name:  "legacy",
		Entry: "start",
		Steps: map[string]domain.StepDef{
			"start": {
				Trigger:   domain.EventMatch{Kind: "android.window.state_changed"},
				OnSuccess: "terminal",
				OnFailure: "terminal",
			},
		},
	})
	if err == nil {
		t.Fatal("expected invalid workflow def to be rejected")
	}
	if !strings.Contains(err.Error(), string(domain.EventKindScreenChanged)) {
		t.Fatalf("expected replacement hint for %q, got: %v", domain.EventKindScreenChanged, err)
	}
}

