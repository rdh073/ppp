package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

const defaultScriptTimeout = 30 * time.Second // longer than action default (10s)

// ScriptNode handles steps with a non-nil Script field.
// It sends the JS source to the device via device.script, then maps the
// script's return value into NodeOutput.StateInputs via def.Outputs.
type ScriptNode struct {
	disp ActionDispatcher
}

type scriptCommandParams struct {
	Script  string            `json:"script"`
	Params  map[string]string `json:"params,omitempty"`
	Timeout int64             `json:"timeout"` // ms
}

func (n *ScriptNode) Execute(ctx context.Context, cmd NodeCommand) (NodeOutput, error) {
	def := cmd.Step.Script
	state := cmd.State
	task := cmd.Task

	timeout := parseDuration(def.Timeout, parseDuration(cmd.Step.Timeout, defaultScriptTimeout))

	// Interpolate param values from workflow state inputs.
	interpolatedParams := make(map[string]string, len(def.Params))
	for k, v := range def.Params {
		interpolatedParams[k] = Interpolate(v, state.Inputs)
	}

	raw, err := json.Marshal(scriptCommandParams{
		Script:  def.Source,
		Params:  interpolatedParams,
		Timeout: timeout.Milliseconds(),
	})
	if err != nil {
		return NodeOutput{Err: fmt.Errorf("marshal script params: %w", err)}, nil
	}

	devCmd := domain.Command{
		ID:       domain.NewCommandID(),
		Kind:     domain.CommandKindScript,
		DeviceID: state.DeviceID,
		TaskID:   task.ID,
		Params:   raw,
		IssuedAt: time.Now(),
	}

	dispatchCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ch, err := n.disp.Dispatch(dispatchCtx, devCmd)
	if err != nil {
		return NodeOutput{Err: fmt.Errorf("dispatch script: %w", err)}, nil
	}

	select {
	case result := <-ch:
		if !result.Success {
			if result.Err != nil {
				return NodeOutput{Err: fmt.Errorf("script error %d: %s", result.Err.Code, result.Err.Message)}, nil
			}
			return NodeOutput{Err: fmt.Errorf("script returned failure")}, nil
		}
		if len(def.Outputs) == 0 {
			return NodeOutput{}, nil
		}
		// Extract output fields from {output: {...}, logs: [...], durationMs: N}
		var envelope struct {
			Output map[string]json.RawMessage `json:"output"`
		}
		if err := json.Unmarshal(result.Raw, &envelope); err != nil {
			return NodeOutput{Err: fmt.Errorf("unmarshal script result: %w", err)}, nil
		}
		outputs := make(map[string]string, len(def.Outputs))
		for scriptKey, inputKey := range def.Outputs {
			rawVal, ok := envelope.Output[scriptKey]
			if !ok {
				continue
			}
			var s string
			if err := json.Unmarshal(rawVal, &s); err != nil {
				s = string(rawVal)
			}
			outputs[inputKey] = s
		}
		return NodeOutput{StateInputs: outputs}, nil

	case <-dispatchCtx.Done():
		if ctx.Err() != nil {
			return NodeOutput{}, ctx.Err()
		}
		return NodeOutput{Err: fmt.Errorf("script command timeout after %s", timeout)}, nil
	}
}

// Ensure ScriptNode implements Node at compile time.
var _ Node = (*ScriptNode)(nil)
