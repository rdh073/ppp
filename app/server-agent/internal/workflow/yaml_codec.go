package workflow

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// stringOrJSONMapCodec implements yaml.Unmarshaler for domain.StringOrJSONMap.
// Defined here so that domain does not depend on gopkg.in/yaml.v3.
// Scalar values are stored as-is; non-scalar nodes (objects, sequences) are
// JSON-marshalled so that downstream code always sees a plain string-map.
type stringOrJSONMapCodec domain.StringOrJSONMap

func (m *stringOrJSONMapCodec) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("params must be a YAML mapping, got kind %d", value.Kind)
	}
	result := make(stringOrJSONMapCodec, len(value.Content)/2)
	for i := 0; i+1 < len(value.Content); i += 2 {
		var key string
		if err := value.Content[i].Decode(&key); err != nil {
			return fmt.Errorf("decode params key: %w", err)
		}
		val := value.Content[i+1]
		if val.Kind == yaml.ScalarNode {
			result[key] = val.Value
		} else {
			var v any
			if err := val.Decode(&v); err != nil {
				return fmt.Errorf("decode params.%s: %w", key, err)
			}
			b, err := json.Marshal(v)
			if err != nil {
				return fmt.Errorf("encode params.%s: %w", key, err)
			}
			result[key] = string(b)
		}
	}
	*m = result
	return nil
}

// toolCallDefYAML mirrors domain.ToolCallDef for YAML loading only.
// Uses stringOrJSONMapCodec for Params so the codec lives outside domain.
type toolCallDefYAML struct {
	ToolName string               `yaml:"tool_name"`
	Params   stringOrJSONMapCodec `yaml:"params,omitempty"`
	Outputs  map[string]string    `yaml:"outputs,omitempty"`
	Optional bool                 `yaml:"optional,omitempty"`
}

func (t toolCallDefYAML) toDomain() *domain.ToolCallDef {
	return &domain.ToolCallDef{
		ToolName: t.ToolName,
		Params:   domain.StringOrJSONMap(t.Params),
		Outputs:  t.Outputs,
		Optional: t.Optional,
	}
}

// stepDefYAML mirrors domain.StepDef for YAML loading only.
type stepDefYAML struct {
	Trigger   domain.EventMatch    `yaml:"trigger"`
	Action    *domain.ActionDef    `yaml:"action,omitempty"`
	ToolCall  *toolCallDefYAML     `yaml:"tool_call,omitempty"`
	Script    *domain.ScriptDef    `yaml:"script,omitempty"`
	Expect    *domain.ExpectDef    `yaml:"expect,omitempty"`
	OnSuccess string               `yaml:"on_success"`
	OnFailure string               `yaml:"on_failure"`
	Timeout   string               `yaml:"timeout,omitempty"`
	MaxRetry  int                  `yaml:"max_retry,omitempty"`
}

func (s stepDefYAML) toDomain() domain.StepDef {
	d := domain.StepDef{
		Trigger:   s.Trigger,
		Action:    s.Action,
		Script:    s.Script,
		Expect:    s.Expect,
		OnSuccess: s.OnSuccess,
		OnFailure: s.OnFailure,
		Timeout:   s.Timeout,
		MaxRetry:  s.MaxRetry,
	}
	if s.ToolCall != nil {
		d.ToolCall = s.ToolCall.toDomain()
	}
	return d
}

// workflowDefYAML mirrors domain.WorkflowDef for YAML loading only.
type workflowDefYAML struct {
	Name    string                  `yaml:"name"`
	Version int                     `yaml:"version"`
	Entry   string                  `yaml:"entry"`
	Inputs  map[string]domain.InputDef `yaml:"inputs,omitempty"`
	Steps   map[string]stepDefYAML  `yaml:"steps"`
}

func (w workflowDefYAML) toDomain() domain.WorkflowDef {
	steps := make(map[string]domain.StepDef, len(w.Steps))
	for k, v := range w.Steps {
		steps[k] = v.toDomain()
	}
	return domain.WorkflowDef{
		Name:    w.Name,
		Version: w.Version,
		Entry:   w.Entry,
		Inputs:  w.Inputs,
		Steps:   steps,
	}
}

// unmarshalWorkflowDef decodes a YAML document into a domain.WorkflowDef using
// the intermediate codec types so that domain does not need to import yaml.v3.
func unmarshalWorkflowDef(data []byte) (domain.WorkflowDef, error) {
	var raw workflowDefYAML
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return domain.WorkflowDef{}, err
	}
	return raw.toDomain(), nil
}
