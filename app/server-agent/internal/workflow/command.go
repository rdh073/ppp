package workflow

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// executeParams is the JSON body for device.execute.
type executeParams struct {
	Action executeAction `json:"action"`
}

type executeAction struct {
	Kind      string         `json:"kind"`
	Target    *executeTarget `json:"target,omitempty"`
	InputText string         `json:"inputText,omitempty"`
	Package   string         `json:"package,omitempty"`
	Direction string         `json:"direction,omitempty"`
}

type executeTarget struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// fillFormParams is the JSON body for device.execute with kind=fill_form.
// The android-agent iterates fields locally, eliminating per-field round-trips.
type fillFormParams struct {
	Action fillFormAction `json:"action"`
}

type fillFormAction struct {
	Kind   string          `json:"kind"`
	Fields []fillFormField `json:"fields"`
}

type fillFormField struct {
	Target executeTarget `json:"target"`
	Value  string        `json:"value"`
}

func buildCommand(
	action *domain.ActionDef,
	deviceID domain.DeviceID,
	taskID domain.TaskID,
	inputs map[string]string,
) (domain.Command, error) {
	cmdKind := domain.CommandKindExecute
	if action.Kind == domain.ActionKindObserve {
		cmdKind = domain.CommandKindObserve
	}

	var params json.RawMessage
	var err error

	switch {
	case cmdKind == domain.CommandKindObserve:
		params = json.RawMessage(`{}`)

	case action.Kind == domain.ActionKindFillForm:
		fields := make([]fillFormField, len(action.Fields))
		for i, f := range action.Fields {
			fields[i] = fillFormField{
				Target: executeTarget{
					Kind:  string(f.Target.Kind),
					Value: Interpolate(f.Target.Value, inputs),
				},
				Value: Interpolate(f.Value, inputs),
			}
		}
		params, err = json.Marshal(fillFormParams{Action: fillFormAction{
			Kind:   string(action.Kind),
			Fields: fields,
		}})
		if err != nil {
			return domain.Command{}, fmt.Errorf("marshal fill_form params: %w", err)
		}

	default:
		act := executeAction{Kind: string(action.Kind)}
		if action.Target != nil {
			act.Target = &executeTarget{
				Kind:  string(action.Target.Kind),
				Value: Interpolate(action.Target.Value, inputs),
			}
		}
		// open_app: agent reads package from target.value, not the top-level package field.
		if action.Kind == domain.ActionKindOpenApp && action.Package != "" && act.Target == nil {
			pkg := Interpolate(action.Package, inputs)
			act.Target = &executeTarget{Kind: "package_name", Value: pkg}
		}
		act.InputText = Interpolate(action.InputText, inputs)
		act.Package = Interpolate(action.Package, inputs)
		act.Direction = action.Direction

		params, err = json.Marshal(executeParams{Action: act})
		if err != nil {
			return domain.Command{}, fmt.Errorf("marshal action params: %w", err)
		}
	}

	return domain.Command{
		ID:       domain.NewCommandID(),
		Kind:     cmdKind,
		DeviceID: deviceID,
		TaskID:   taskID,
		Params:   params,
		IssuedAt: time.Now(),
	}, nil
}
