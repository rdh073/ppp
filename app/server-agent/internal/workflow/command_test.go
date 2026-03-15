package workflow

import (
	"encoding/json"
	"testing"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

func TestBuildCommand_SemanticKeyTarget(t *testing.T) {
	cmd, err := buildCommand(
		&domain.ActionDef{
			Kind: domain.ActionKindClick,
			Target: &domain.TargetDef{
				Kind:  domain.TargetKindSemanticKey,
				Value: "editor.toolbar.export",
			},
		},
		domain.DeviceID("dev1"),
		domain.TaskID("task1"),
		nil,
	)
	if err != nil {
		t.Fatalf("buildCommand returned error: %v", err)
	}

	var params struct {
		Action struct {
			Kind   string `json:"kind"`
			Target struct {
				Kind  string `json:"kind"`
				Value string `json:"value"`
			} `json:"target"`
		} `json:"action"`
	}
	if err := json.Unmarshal(cmd.Params, &params); err != nil {
		t.Fatalf("unmarshal command params: %v", err)
	}

	if params.Action.Kind != string(domain.ActionKindClick) {
		t.Fatalf("action kind = %q, want %q", params.Action.Kind, domain.ActionKindClick)
	}
	if params.Action.Target.Kind != string(domain.TargetKindSemanticKey) {
		t.Fatalf("target kind = %q, want %q", params.Action.Target.Kind, domain.TargetKindSemanticKey)
	}
	if params.Action.Target.Value != "editor.toolbar.export" {
		t.Fatalf("target value = %q, want %q", params.Action.Target.Value, "editor.toolbar.export")
	}
}

func TestBuildCommand_FillFormSemanticKeyTargets(t *testing.T) {
	cmd, err := buildCommand(
		&domain.ActionDef{
			Kind: domain.ActionKindFillForm,
			Fields: []domain.FieldEntry{
				{
					Target: domain.TargetDef{
						Kind:  domain.TargetKindSemanticKey,
						Value: "login.email",
					},
					Value: "user@example.com",
				},
			},
		},
		domain.DeviceID("dev1"),
		domain.TaskID("task1"),
		nil,
	)
	if err != nil {
		t.Fatalf("buildCommand returned error: %v", err)
	}

	var params struct {
		Action struct {
			Kind   string `json:"kind"`
			Fields []struct {
				Target struct {
					Kind  string `json:"kind"`
					Value string `json:"value"`
				} `json:"target"`
				Value string `json:"value"`
			} `json:"fields"`
		} `json:"action"`
	}
	if err := json.Unmarshal(cmd.Params, &params); err != nil {
		t.Fatalf("unmarshal fill_form params: %v", err)
	}

	if params.Action.Kind != string(domain.ActionKindFillForm) {
		t.Fatalf("action kind = %q, want %q", params.Action.Kind, domain.ActionKindFillForm)
	}
	if len(params.Action.Fields) != 1 {
		t.Fatalf("field count = %d, want 1", len(params.Action.Fields))
	}
	if params.Action.Fields[0].Target.Kind != string(domain.TargetKindSemanticKey) {
		t.Fatalf("field target kind = %q, want %q", params.Action.Fields[0].Target.Kind, domain.TargetKindSemanticKey)
	}
	if params.Action.Fields[0].Target.Value != "login.email" {
		t.Fatalf("field target value = %q, want %q", params.Action.Fields[0].Target.Value, "login.email")
	}
}
