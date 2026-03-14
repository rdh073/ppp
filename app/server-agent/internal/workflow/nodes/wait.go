package nodes

import (
	"context"
	"strings"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/workflow"
)

// WaitNode suspends the workflow until one or more specific device events arrive.
//
// The node reads the artifact "wait_for_events" — a comma-separated list of
// EventKind strings (e.g. "android.activity.created,android.screen.changed").
// It returns NodeStatusPending with the parsed WaitingFor list, which the Runner
// checkpoints into WorkflowState.WaitingFor without evaluating transitions.
//
// The orchestrator will skip this workflow on the next ProcessEvent call unless
// the event kind is in the WaitingFor list. Once a matching event arrives, the
// orchestrator clears WaitingFor and re-runs the current node — at which point
// the node sees the event kind in NodeInput.Event and returns NodeStatusSuccess,
// allowing the def's transitions to fire normally.
//
// YAML workflow usage:
//
//	nodes:
//	  wait:
//	    transitions:
//	      - to: decide
//	        when: "event.kind == 'android.activity.created'"
//	      - to: resync
//	        when: failure
type WaitNode struct{}

func NewWaitNode() *WaitNode { return &WaitNode{} }

func (n *WaitNode) Run(_ context.Context, input workflow.NodeInput) (workflow.NodeOutput, error) {
	raw := input.State.Artifacts["wait_for_events"]
	if raw == "" {
		// No wait list configured — treat as immediate success.
		return workflow.NodeOutput{Status: workflow.NodeStatusSuccess}, nil
	}

	// Determine the wait list: use state (re-arm path) or parse artifact (first entry).
	waitList := input.State.WaitingFor
	if len(waitList) == 0 {
		waitList = parseEventKinds(raw)
	}

	// If the triggering event matches one of the kinds we're waiting for, unblock.
	for _, k := range waitList {
		if k == input.Event.Kind {
			return workflow.NodeOutput{
				Status:          workflow.NodeStatusSuccess,
				DeleteArtifacts: []string{"wait_for_events"},
			}, nil
		}
	}

	// Still waiting — (re-)arm with the resolved list.
	return workflow.NodeOutput{
		Status:     workflow.NodeStatusPending,
		WaitingFor: waitList,
	}, nil
}

func parseEventKinds(s string) []domain.EventKind {
	parts := strings.Split(s, ",")
	out := make([]domain.EventKind, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, domain.EventKind(p))
		}
	}
	return out
}
