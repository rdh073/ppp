package workflow

import "github.com/autosdk/ppp/server-agent/internal/domain"

const (
	DefaultWorkflowName                   = "default"
	LocalIdentityProfileWorkflowName      = "local-identity-profile"
	LocalIdentityWelcomeEmailWorkflowName = "local-identity-welcome-email"
)

// DefaultWorkflowDef replicates the hardcoded node routing that was previously
// embedded in each node's Run method.
//
// Routing table:
//
//	observe:  success→decide, failure→resync
//	decide:   goal_reached=='true'→terminal, errorCount>=5→terminal,
//	          pending_tool!=''→toolcall, pending_action!=''→act, default→observe
//	act:      success→verify, failure→resync
//	verify:   success→decide, failure→resync
//	resync:   success→decide, failure→decide
//	toolcall: success→decide, failure→resync
//	terminal: (no transitions)
var DefaultWorkflowDef = newStandardWorkflowDef(DefaultWorkflowName)

// LocalIdentityProfileWorkflowDef is the first shipped workflow path that uses
// real deterministic ToolCall steps to assemble a local profile.
var LocalIdentityProfileWorkflowDef = newStandardWorkflowDef(LocalIdentityProfileWorkflowName)

// LocalIdentityWelcomeEmailWorkflowDef extends the local identity profile flow
// with one optional model-backed email generation step and a deterministic
// fallback template.
var LocalIdentityWelcomeEmailWorkflowDef = newStandardWorkflowDef(LocalIdentityWelcomeEmailWorkflowName)

func newStandardWorkflowDef(name string) *domain.WorkflowDef {
	return &domain.WorkflowDef{
		Name:    name,
		Version: 1,
		Entry:   domain.NodeKindObserve,
		Nodes: map[domain.NodeKind]domain.NodeDef{
			domain.NodeKindObserve: {
				Transitions: []domain.Transition{
					{To: domain.NodeKindDecide, When: "success"},
					{To: domain.NodeKindResync, When: "failure"},
				},
			},
			domain.NodeKindDecide: {
				Transitions: []domain.Transition{
					{To: domain.NodeKindTerminal, When: "artifacts.goal_reached == 'true'"},
					{To: domain.NodeKindTerminal, When: "errorCount >= 5"},
					{To: domain.NodeKindToolCall, When: "artifacts.pending_tool != ''"},
					{To: domain.NodeKindAct, When: "artifacts.pending_action != ''"},
					{To: domain.NodeKindObserve, When: ""},
				},
			},
			domain.NodeKindAct: {
				Transitions: []domain.Transition{
					{To: domain.NodeKindVerify, When: "success"},
					{To: domain.NodeKindResync, When: "failure"},
				},
			},
			domain.NodeKindVerify: {
				Transitions: []domain.Transition{
					{To: domain.NodeKindDecide, When: "success"},
					{To: domain.NodeKindResync, When: "failure"},
				},
			},
			domain.NodeKindResync: {
				Transitions: []domain.Transition{
					{To: domain.NodeKindDecide, When: "success"},
					{To: domain.NodeKindDecide, When: "failure"},
				},
			},
			domain.NodeKindToolCall: {
				Transitions: []domain.Transition{
					{To: domain.NodeKindDecide, When: "success"},
					{To: domain.NodeKindResync, When: "failure"},
				},
			},
			domain.NodeKindTerminal: {
				Transitions: []domain.Transition{},
			},
		},
	}
}
