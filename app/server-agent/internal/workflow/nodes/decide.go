package nodes

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/autosdk/ppp/server-agent/internal/workflow"
)

// DecideNode is a lightweight artifact planner. Most routing still lives in the
// workflow def, but specific built-in workflows may materialize artifacts here
// so the def can route to ToolCall, Act, or Terminal deterministically.
type DecideNode struct{}

func NewDecideNode() *DecideNode { return &DecideNode{} }

func (n *DecideNode) Run(_ context.Context, input workflow.NodeInput) (workflow.NodeOutput, error) {
	if input.Task != nil && input.Task.WorkflowName == workflow.LocalIdentityProfileWorkflowName {
		return runLocalIdentityProfileDecision(input)
	}
	return workflow.NodeOutput{Status: workflow.NodeStatusSuccess}, nil
}

type localIdentityNameResult struct {
	FullName  string `json:"fullName"`
	FirstName string `json:"firstName"`
	LastName  string `json:"lastName"`
	Gender    string `json:"gender"`
}

type localIdentityEmailResult struct {
	Email     string `json:"email"`
	LocalPart string `json:"localPart"`
	Domain    string `json:"domain"`
}

type localIdentityPasswordResult struct {
	Password  string `json:"password"`
	Length    int    `json:"length"`
	HasSymbol bool   `json:"hasSymbol"`
}

type localIdentityBirthDateResult struct {
	BirthDate     string `json:"birthDate"`
	Age           int    `json:"age"`
	ReferenceDate string `json:"referenceDate"`
}

func runLocalIdentityProfileDecision(input workflow.NodeInput) (workflow.NodeOutput, error) {
	out := workflow.NodeOutput{
		Status:    workflow.NodeStatusSuccess,
		Artifacts: map[string]string{},
	}

	if toolResult := input.State.Artifacts["tool_result"]; toolResult != "" {
		lastToolName := input.State.Artifacts["last_tool_name"]
		if lastToolName == "" {
			return localIdentityFailure(fmt.Errorf("tool_result present without last_tool_name")), nil
		}
		consumed, err := consumeLocalIdentityToolResult(lastToolName, toolResult)
		if err != nil {
			return localIdentityFailure(err), nil
		}
		for k, v := range consumed {
			out.Artifacts[k] = v
		}
		out.DeleteArtifacts = append(out.DeleteArtifacts, "tool_result", "last_tool_name")
	}

	artifacts := mergeArtifacts(input.State.Artifacts, out.Artifacts, out.DeleteArtifacts)
	if artifacts["pending_tool"] != "" || artifacts["goal_reached"] == "true" {
		return out, nil
	}

	switch {
	case artifacts["profile_full_name"] == "":
		return queueLocalIdentityTool(out, "identity.generate_indonesian_name", map[string]any{}), nil
	case artifacts["profile_email"] == "":
		return queueLocalIdentityTool(out, "identity.generate_email", map[string]any{
			"fullName": artifacts["profile_full_name"],
		}), nil
	case artifacts["profile_password"] == "":
		return queueLocalIdentityTool(out, "credential.generate_password", map[string]any{
			"length":         20,
			"includeSymbols": true,
		}), nil
	case artifacts["profile_birth_date"] == "":
		return queueLocalIdentityTool(out, "identity.generate_birth_date", map[string]any{
			"minAge": 25,
			"maxAge": 35,
		}), nil
	default:
		out.Artifacts["goal_reached"] = "true"
		out.Artifacts["terminal_reason"] = "local_identity_profile_ready"
		out.Artifacts["profile_ready"] = "true"
		return out, nil
	}
}

func consumeLocalIdentityToolResult(toolName string, raw string) (map[string]string, error) {
	switch toolName {
	case "identity.generate_indonesian_name":
		var result localIdentityNameResult
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			return nil, fmt.Errorf("decode %s: %w", toolName, err)
		}
		return map[string]string{
			"profile_full_name":  result.FullName,
			"profile_first_name": result.FirstName,
			"profile_last_name":  result.LastName,
			"profile_gender":     result.Gender,
		}, nil
	case "identity.generate_email":
		var result localIdentityEmailResult
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			return nil, fmt.Errorf("decode %s: %w", toolName, err)
		}
		return map[string]string{
			"profile_email":            result.Email,
			"profile_email_local_part": result.LocalPart,
			"profile_email_domain":     result.Domain,
		}, nil
	case "credential.generate_password":
		var result localIdentityPasswordResult
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			return nil, fmt.Errorf("decode %s: %w", toolName, err)
		}
		return map[string]string{
			"profile_password":           result.Password,
			"profile_password_length":    strconv.Itoa(result.Length),
			"profile_password_hasSymbol": strconv.FormatBool(result.HasSymbol),
		}, nil
	case "identity.generate_birth_date":
		var result localIdentityBirthDateResult
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			return nil, fmt.Errorf("decode %s: %w", toolName, err)
		}
		return map[string]string{
			"profile_birth_date":     result.BirthDate,
			"profile_age":            strconv.Itoa(result.Age),
			"profile_reference_date": result.ReferenceDate,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported tool_result source: %s", toolName)
	}
}

func queueLocalIdentityTool(out workflow.NodeOutput, toolName string, params map[string]any) workflow.NodeOutput {
	raw, err := json.Marshal(params)
	if err != nil {
		return localIdentityFailure(fmt.Errorf("marshal params for %s: %w", toolName, err))
	}
	out.Artifacts["pending_tool"] = toolName
	out.Artifacts["pending_tool_params"] = string(raw)
	return out
}

func localIdentityFailure(err error) workflow.NodeOutput {
	return workflow.NodeOutput{
		Status: workflow.NodeStatusFailure,
		Artifacts: map[string]string{
			"resync_reason": fmt.Sprintf("local identity decide failed: %v", err),
		},
		DeleteArtifacts: []string{"tool_result", "last_tool_name"},
	}
}

func mergeArtifacts(base map[string]string, updates map[string]string, deletes []string) map[string]string {
	merged := make(map[string]string, len(base)+len(updates))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range updates {
		merged[k] = v
	}
	for _, k := range deletes {
		delete(merged, k)
	}
	return merged
}
