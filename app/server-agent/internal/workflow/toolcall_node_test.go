package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// --- fakes ---

type fixedToolInvoker struct {
	result json.RawMessage
	err    error
	calls  int
}

func (t *fixedToolInvoker) Invoke(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
	t.calls++
	return t.result, t.err
}

type capturingToolInvoker struct {
	captured json.RawMessage
	result   json.RawMessage
}

func (c *capturingToolInvoker) Invoke(_ context.Context, _ string, params json.RawMessage) (json.RawMessage, error) {
	c.captured = params
	return c.result, nil
}

// --- helpers ---

func makeToolCmd(def *domain.ToolCallDef, inputs map[string]string) NodeCommand {
	state := domain.NewWorkflowState(domain.TaskID("t1"), domain.DeviceID("dev1"))
	for k, v := range inputs {
		state.Inputs[k] = v
	}
	return NodeCommand{
		Step:  domain.StepDef{ToolCall: def},
		State: state,
		Task:  &domain.Task{ID: domain.TaskID("t1"), Status: domain.TaskStatusRunning},
	}
}

// --- tests ---

// TestToolCallNode_Success_OutputsMapped: tool succeeds → StateInputs populated from Outputs map.
func TestToolCallNode_Success_OutputsMapped(t *testing.T) {
	inv := &fixedToolInvoker{result: json.RawMessage(`{"fullName":"Budi Santoso","score":42}`)}
	n := &ToolCallNode{tools: inv}
	out, err := n.Execute(context.Background(), makeToolCmd(
		&domain.ToolCallDef{
			ToolName: "identity.generate",
			Outputs:  map[string]string{"fullName": "username", "score": "user_score"},
		},
		nil,
	))
	if err != nil {
		t.Fatalf("system error: %v", err)
	}
	if out.Err != nil {
		t.Fatalf("unexpected business error: %v", out.Err)
	}
	if out.StateInputs["username"] != "Budi Santoso" {
		t.Errorf("username: want %q, got %q", "Budi Santoso", out.StateInputs["username"])
	}
	// score is a number, not a JSON string — raw representation used.
	if out.StateInputs["user_score"] != "42" {
		t.Errorf("user_score: want %q, got %q", "42", out.StateInputs["user_score"])
	}
}

// TestToolCallNode_OptionalFailure_SuccessPath: optional tool fails → empty NodeOutput (success path).
func TestToolCallNode_OptionalFailure_SuccessPath(t *testing.T) {
	inv := &fixedToolInvoker{err: errors.New("tool unavailable")}
	n := &ToolCallNode{tools: inv}
	out, err := n.Execute(context.Background(), makeToolCmd(
		&domain.ToolCallDef{ToolName: "flaky.tool", Optional: true},
		nil,
	))
	if err != nil {
		t.Fatalf("system error: %v", err)
	}
	if out.Err != nil {
		t.Errorf("optional failure should produce empty NodeOutput, got Err: %v", out.Err)
	}
}

// TestToolCallNode_RequiredFailure_ErrSet: non-optional tool fails → NodeOutput.Err non-nil.
func TestToolCallNode_RequiredFailure_ErrSet(t *testing.T) {
	inv := &fixedToolInvoker{err: errors.New("tool error")}
	n := &ToolCallNode{tools: inv}
	out, err := n.Execute(context.Background(), makeToolCmd(
		&domain.ToolCallDef{ToolName: "required.tool", Optional: false},
		nil,
	))
	if err != nil {
		t.Fatalf("system error: %v", err)
	}
	if out.Err == nil {
		t.Error("expected Err set for required tool failure")
	}
}

// TestToolCallNode_NilInvoker_Required: nil tools, required → NodeOutput.Err non-nil.
func TestToolCallNode_NilInvoker_Required(t *testing.T) {
	n := &ToolCallNode{tools: nil}
	out, err := n.Execute(context.Background(), makeToolCmd(
		&domain.ToolCallDef{ToolName: "some.tool", Optional: false},
		nil,
	))
	if err != nil {
		t.Fatalf("system error: %v", err)
	}
	if out.Err == nil {
		t.Error("expected Err when tool invoker is nil and tool is required")
	}
}

// TestToolCallNode_NilInvoker_Optional: nil tools, optional → empty NodeOutput (success path).
func TestToolCallNode_NilInvoker_Optional(t *testing.T) {
	n := &ToolCallNode{tools: nil}
	out, err := n.Execute(context.Background(), makeToolCmd(
		&domain.ToolCallDef{ToolName: "some.tool", Optional: true},
		nil,
	))
	if err != nil {
		t.Fatalf("system error: %v", err)
	}
	if out.Err != nil {
		t.Errorf("optional tool with nil invoker should succeed: %v", out.Err)
	}
}

// TestToolCallNode_ParamInterpolation: {{input.key}} substituted before Invoke.
func TestToolCallNode_ParamInterpolation(t *testing.T) {
	cap := &capturingToolInvoker{result: json.RawMessage(`{}`)}
	n := &ToolCallNode{tools: cap}
	_, err := n.Execute(context.Background(), makeToolCmd(
		&domain.ToolCallDef{
			ToolName: "some.tool",
			Params:   map[string]string{"hostname": "{{input.private_dns_hostname}}"},
			Optional: true,
		},
		map[string]string{"private_dns_hostname": "dns.example.com"},
	))
	if err != nil {
		t.Fatalf("system error: %v", err)
	}
	var params map[string]string
	if err := json.Unmarshal(cap.captured, &params); err != nil {
		t.Fatalf("decode captured params: %v", err)
	}
	if params["hostname"] != "dns.example.com" {
		t.Errorf("want hostname=dns.example.com, got %q", params["hostname"])
	}
}

// TestToolCallNode_LiteralParams_KeepJSONTypes: non-template params should be
// marshaled as JSON literals so tools expecting integer/boolean types can parse.
func TestToolCallNode_LiteralParams_KeepJSONTypes(t *testing.T) {
	cap := &capturingToolInvoker{result: json.RawMessage(`{}`)}
	n := &ToolCallNode{tools: cap}
	_, err := n.Execute(context.Background(), makeToolCmd(
		&domain.ToolCallDef{
			ToolName: "some.tool",
			Params: map[string]string{
				"minAge":         "25",
				"includeSymbols": "true",
				"name":           "agus",
			},
			Optional: true,
		},
		nil,
	))
	if err != nil {
		t.Fatalf("system error: %v", err)
	}

	var params map[string]any
	if err := json.Unmarshal(cap.captured, &params); err != nil {
		t.Fatalf("decode captured params: %v", err)
	}
	if got, ok := params["minAge"].(float64); !ok || got != 25 {
		t.Fatalf("minAge: want numeric 25, got %#v", params["minAge"])
	}
	if got, ok := params["includeSymbols"].(bool); !ok || !got {
		t.Fatalf("includeSymbols: want true bool, got %#v", params["includeSymbols"])
	}
	if got, ok := params["name"].(string); !ok || got != "agus" {
		t.Fatalf("name: want string 'agus', got %#v", params["name"])
	}
}
