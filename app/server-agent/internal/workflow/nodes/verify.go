package nodes

import (
	"context"

	"github.com/autosdk/ppp/server-agent/internal/workflow"
)

// VerifyNode compares the pre-action snapshot with the post-action snapshot
// received in the device.execute response (stored in last_execute_raw).
// Success (snapshots differ): clears pre_action_snapshot, resets errorCount → def routes to Decide.
// Failure (unchanged UI): sets resync_reason → def routes to Resync.
type VerifyNode struct{}

func NewVerifyNode() *VerifyNode { return &VerifyNode{} }

func (n *VerifyNode) Run(_ context.Context, input workflow.NodeInput) (workflow.NodeOutput, error) {
	pre := input.State.Artifacts["pre_action_snapshot"]
	post := input.State.Artifacts["last_execute_raw"]

	if pre != "" && post != "" && pre != post {
		zero := 0
		artifacts := map[string]string{}
		if snapshotAfter, ok := extractSnapshotAfterRaw(post); ok {
			artifacts["last_observe_raw"] = snapshotAfter
		}
		return workflow.NodeOutput{
			Status:          workflow.NodeStatusSuccess,
			Artifacts:       artifacts,
			DeleteArtifacts: []string{"pre_action_snapshot"},
			SetErrorCount:   &zero,
		}, nil
	}

	return workflow.NodeOutput{
		Status: workflow.NodeStatusFailure,
		Artifacts: map[string]string{
			"resync_reason": "verify_ui_unchanged",
		},
	}, nil
}
