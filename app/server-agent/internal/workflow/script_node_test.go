package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// --- helpers ---

func makeScriptCmd(def *domain.ScriptDef, stepTimeout string, inputs map[string]string) NodeCommand {
	state := domain.NewWorkflowState(domain.TaskID("t1"), domain.DeviceID("dev1"))
	for k, v := range inputs {
		state.Inputs[k] = v
	}
	return NodeCommand{
		Step:  domain.StepDef{Script: def, Timeout: stepTimeout},
		State: state,
		Task:  &domain.Task{ID: domain.TaskID("t1"), Status: domain.TaskStatusRunning},
	}
}

func scriptSuccess(outputJSON string) domain.CommandResult {
	raw := json.RawMessage(`{"output":` + outputJSON + `,"logs":[],"durationMs":42}`)
	return domain.CommandResult{Success: true, Raw: raw}
}

// --- tests ---

func TestScriptNode_Success_NoOutputs(t *testing.T) {
	def := &domain.ScriptDef{Source: "return {};"}
	n := &ScriptNode{disp: &fixedDispatcher{result: scriptSuccess("{}")}}
	out, err := n.Execute(context.Background(), makeScriptCmd(def, "", nil))
	if err != nil {
		t.Fatalf("system error: %v", err)
	}
	if out.Err != nil {
		t.Errorf("unexpected business error: %v", out.Err)
	}
	if len(out.StateInputs) != 0 {
		t.Errorf("expected empty StateInputs, got %v", out.StateInputs)
	}
}

func TestScriptNode_Success_OutputsMapped(t *testing.T) {
	def := &domain.ScriptDef{
		Source:  "return { configured: true };",
		Outputs: map[string]string{"configured": "dns_configured"},
	}
	n := &ScriptNode{disp: &fixedDispatcher{result: scriptSuccess(`{"configured":true}`)}}
	out, err := n.Execute(context.Background(), makeScriptCmd(def, "", nil))
	if err != nil {
		t.Fatalf("system error: %v", err)
	}
	if out.Err != nil {
		t.Errorf("unexpected business error: %v", out.Err)
	}
	got, ok := out.StateInputs["dns_configured"]
	if !ok {
		t.Fatalf("expected dns_configured in StateInputs, got %v", out.StateInputs)
	}
	// JSON bool true is not a JSON string, so raw form is used.
	if got != "true" {
		t.Errorf("expected StateInputs[dns_configured]=%q, got %q", "true", got)
	}
}

func TestScriptNode_Success_StringOutputExtracted(t *testing.T) {
	def := &domain.ScriptDef{
		Source:  `return { host: "example.com" };`,
		Outputs: map[string]string{"host": "dns_host"},
	}
	n := &ScriptNode{disp: &fixedDispatcher{result: scriptSuccess(`{"host":"example.com"}`)}}
	out, err := n.Execute(context.Background(), makeScriptCmd(def, "", nil))
	if err != nil {
		t.Fatalf("system error: %v", err)
	}
	if out.StateInputs["dns_host"] != "example.com" {
		t.Errorf("expected dns_host=example.com, got %q", out.StateInputs["dns_host"])
	}
}

func TestScriptNode_ParamsInterpolated(t *testing.T) {
	captured := &capturingDispatcher{}
	def := &domain.ScriptDef{
		Source: "return {};",
		Params: map[string]string{"hostname": "{{input.dns_host}}"},
	}
	n := &ScriptNode{disp: captured}
	_, err := n.Execute(context.Background(), makeScriptCmd(def, "", map[string]string{"dns_host": "dns.test"}))
	if err != nil {
		t.Fatalf("system error: %v", err)
	}
	var p struct {
		Params map[string]string `json:"params"`
	}
	if jsonErr := json.Unmarshal(captured.lastCmd.Params, &p); jsonErr != nil {
		t.Fatalf("unmarshal params: %v", jsonErr)
	}
	if p.Params["hostname"] != "dns.test" {
		t.Errorf("expected params.hostname=dns.test, got %q", p.Params["hostname"])
	}
}

func TestScriptNode_DeviceError_BusinessFailure(t *testing.T) {
	deviceErr := domain.CommandResult{
		Success: false,
		Err:     &domain.CommandError{Code: -32008, Message: "script timed out"},
	}
	n := &ScriptNode{disp: &fixedDispatcher{result: deviceErr}}
	out, err := n.Execute(context.Background(), makeScriptCmd(&domain.ScriptDef{Source: ";"}, "", nil))
	if err != nil {
		t.Fatalf("system error: %v", err)
	}
	if out.Err == nil {
		t.Fatal("expected business error, got nil")
	}
}

func TestScriptNode_DispatchError_BusinessFailure(t *testing.T) {
	n := &ScriptNode{disp: &fixedDispatcher{err: errors.New("no connection")}}
	out, err := n.Execute(context.Background(), makeScriptCmd(&domain.ScriptDef{Source: ";"}, "", nil))
	if err != nil {
		t.Fatalf("system error: %v", err)
	}
	if out.Err == nil {
		t.Fatal("expected business error, got nil")
	}
}

func TestScriptNode_CommandKindIsScript(t *testing.T) {
	captured := &capturingDispatcher{}
	n := &ScriptNode{disp: captured}
	_, _ = n.Execute(context.Background(), makeScriptCmd(&domain.ScriptDef{Source: "return {};"}, "", nil))
	if captured.lastCmd.Kind != domain.CommandKindScript {
		t.Errorf("expected CommandKindScript, got %q", captured.lastCmd.Kind)
	}
}

func TestScriptNode_TimeoutDefaultsToScriptDefault(t *testing.T) {
	captured := &capturingDispatcher{}
	n := &ScriptNode{disp: captured}
	_, _ = n.Execute(context.Background(), makeScriptCmd(&domain.ScriptDef{Source: "return {};"}, "", nil))
	var p struct {
		Timeout int64 `json:"timeout"`
	}
	_ = json.Unmarshal(captured.lastCmd.Params, &p)
	expected := defaultScriptTimeout.Milliseconds()
	if p.Timeout != expected {
		t.Errorf("expected timeout=%d ms, got %d", expected, p.Timeout)
	}
}

func TestScriptNode_TimeoutFromScriptDef(t *testing.T) {
	captured := &capturingDispatcher{}
	n := &ScriptNode{disp: captured}
	_, _ = n.Execute(context.Background(), makeScriptCmd(
		&domain.ScriptDef{Source: "return {};", Timeout: "45s"}, "", nil,
	))
	var p struct {
		Timeout int64 `json:"timeout"`
	}
	_ = json.Unmarshal(captured.lastCmd.Params, &p)
	if p.Timeout != 45_000 {
		t.Errorf("expected timeout=45000ms, got %d", p.Timeout)
	}
}

// ---- extra dispatcher for capturing ----

type capturingDispatcher struct {
	lastCmd domain.Command
}

func (c *capturingDispatcher) Dispatch(_ context.Context, cmd domain.Command) (<-chan domain.CommandResult, error) {
	c.lastCmd = cmd
	ch := make(chan domain.CommandResult, 1)
	raw := json.RawMessage(`{"output":{},"logs":[],"durationMs":0}`)
	ch <- domain.CommandResult{Success: true, CommandID: cmd.ID, Raw: raw}
	close(ch)
	return ch, nil
}
