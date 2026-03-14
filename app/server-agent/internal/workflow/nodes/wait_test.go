package nodes_test

import (
	"context"
	"testing"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/workflow"
	"github.com/autosdk/ppp/server-agent/internal/workflow/nodes"
)

func TestWaitNode_NoArtifact(t *testing.T) {
	n := nodes.NewWaitNode()
	out, err := n.Run(context.Background(), workflow.NodeInput{
		State: &domain.WorkflowState{
			Artifacts: map[string]string{},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != workflow.NodeStatusSuccess {
		t.Errorf("expected Success, got %v", out.Status)
	}
}

func TestWaitNode_FirstEntry_Arms(t *testing.T) {
	n := nodes.NewWaitNode()
	out, err := n.Run(context.Background(), workflow.NodeInput{
		State: &domain.WorkflowState{
			Artifacts:  map[string]string{"wait_for_events": "android.activity.created"},
			WaitingFor: nil,
		},
		Event: domain.Event{Kind: "android.screen.changed"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != workflow.NodeStatusPending {
		t.Errorf("expected Pending, got %v", out.Status)
	}
	if len(out.WaitingFor) != 1 || out.WaitingFor[0] != "android.activity.created" {
		t.Errorf("expected WaitingFor=[android.activity.created], got %v", out.WaitingFor)
	}
}

func TestWaitNode_FirstEntry_MatchingEvent(t *testing.T) {
	n := nodes.NewWaitNode()
	out, err := n.Run(context.Background(), workflow.NodeInput{
		State: &domain.WorkflowState{
			Artifacts:  map[string]string{"wait_for_events": "android.activity.created"},
			WaitingFor: nil,
		},
		Event: domain.Event{Kind: "android.activity.created"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != workflow.NodeStatusSuccess {
		t.Errorf("expected Success, got %v", out.Status)
	}
	found := false
	for _, d := range out.DeleteArtifacts {
		if d == "wait_for_events" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected DeleteArtifacts to contain 'wait_for_events', got %v", out.DeleteArtifacts)
	}
}

func TestWaitNode_ReArm_NoMatch(t *testing.T) {
	n := nodes.NewWaitNode()
	out, err := n.Run(context.Background(), workflow.NodeInput{
		State: &domain.WorkflowState{
			Artifacts:  map[string]string{"wait_for_events": "android.activity.created"},
			WaitingFor: []domain.EventKind{"android.activity.created"},
		},
		Event: domain.Event{Kind: "android.screen.changed"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != workflow.NodeStatusPending {
		t.Errorf("expected Pending, got %v", out.Status)
	}
}

func TestWaitNode_ReArm_Match(t *testing.T) {
	n := nodes.NewWaitNode()
	out, err := n.Run(context.Background(), workflow.NodeInput{
		State: &domain.WorkflowState{
			Artifacts:  map[string]string{"wait_for_events": "android.activity.created"},
			WaitingFor: []domain.EventKind{"android.activity.created"},
		},
		Event: domain.Event{Kind: "android.activity.created"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Status != workflow.NodeStatusSuccess {
		t.Errorf("expected Success, got %v", out.Status)
	}
}
