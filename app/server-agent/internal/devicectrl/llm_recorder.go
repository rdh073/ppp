package devicectrl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
	"github.com/autosdk/ppp/server-agent/internal/tools/llm"
)

// ---- system prompt ----

const agentSystemPrompt = `You are an Android UI automation agent controlling a real device.
You receive a goal and the current UI state, then call tools one at a time to achieve the goal.

Available selectors (for tap/input tools):
  kind: "text"         — match by visible text
  kind: "content_desc" — match by accessibility content description
  kind: "resource_id"  — match by Android resource ID (e.g. "com.example:id/button")
  kind: "semantic_key" — match by semantic key (e.g. "button.sign_in", "form.primary.email")
  kind: "package_name" — match by app package (only for open_app)

Common Android intent actions:
  android.settings.SETTINGS
  android.settings.PRIVATE_DNS_SETTINGS
  android.settings.WIFI_SETTINGS
  android.settings.ADD_ACCOUNT_SETTINGS
  android.intent.action.VIEW

Always call observe first to see the current screen.
Call done when the goal is achieved or clearly impossible.`

// ---- tool schemas ----

var deviceAgentTools = []llm.AgentTool{
	{
		Name:        "observe",
		Description: "Get the current screen state (package, activity, visible elements). Call this before deciding what to tap.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
	},
	{
		Name: "tap",
		Description: "Tap an element, launch an app, or fire an Android intent. " +
			"Use kind=click/long_press with target for tapping UI elements. " +
			"Use kind=open_app with target={kind:package_name,value:...} to launch apps. " +
			"Use kind=open_intent with intentAction to launch via Android intent.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"required":["kind"],
			"properties":{
				"kind":{"type":"string","enum":["click","long_press","open_app","open_intent"]},
				"target":{"type":"object","properties":{
					"kind":{"type":"string","enum":["text","content_desc","resource_id","semantic_key","package_name"]},
					"value":{"type":"string"}
				}},
				"intentAction":{"type":"string","description":"Android intent action, required for open_intent"},
				"package":{"type":"string","description":"Optional package filter for open_intent"}
			}
		}`),
	},
	{
		Name:        "input",
		Description: "Type text into a field identified by selector.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"required":["selector","text"],
			"properties":{
				"selector":{"type":"object","required":["kind","value"],"properties":{
					"kind":{"type":"string","enum":["text","content_desc","resource_id","semantic_key"]},
					"value":{"type":"string"}
				}},
				"text":{"type":"string","description":"Text to type into the field"}
			}
		}`),
	},
	{
		Name:        "scroll",
		Description: "Scroll the screen. Use forward to scroll down/right, backward to scroll up/left.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"properties":{
				"direction":{"type":"string","enum":["forward","backward"],"description":"forward=down/right, backward=up/left"}
			}
		}`),
	},
	{
		Name:        "back",
		Description: "Press the system Back button.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
	},
	{
		Name:        "home",
		Description: "Press the system Home button.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
	},
	{
		Name:        "done",
		Description: "Signal that the goal has been achieved or cannot be achieved. Always call this when finished.",
		InputSchema: json.RawMessage(`{
			"type":"object",
			"required":["reason"],
			"properties":{
				"reason":{"type":"string","description":"Explanation of outcome"}
			}
		}`),
	},
}

// ---- dispatch helpers ----

// dispatchObserve sends a device.observe command and returns the raw snapshot JSON.
func (h *DeviceHandler) dispatchObserve(ctx context.Context, id domain.DeviceID) (json.RawMessage, error) {
	if h.disp == nil {
		return nil, fmt.Errorf("dispatcher not configured")
	}
	cmd := domain.Command{
		ID:       domain.NewCommandID(),
		Kind:     domain.CommandKindObserve,
		DeviceID: id,
		Params:   json.RawMessage("{}"),
		IssuedAt: time.Now(),
	}
	ch, err := h.disp.Dispatch(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("dispatch observe: %w", err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	select {
	case result := <-ch:
		if !result.Success {
			msg := "observe failed"
			if result.Err != nil {
				msg = result.Err.Message
			}
			return nil, fmt.Errorf("observe: %s", msg)
		}
		return result.Raw, nil
	case <-waitCtx.Done():
		return nil, fmt.Errorf("observe timeout")
	}
}

// dispatchExecute sends a device.execute command and returns the raw result JSON.
func (h *DeviceHandler) dispatchExecute(ctx context.Context, id domain.DeviceID, actionParams json.RawMessage) (json.RawMessage, error) {
	if h.disp == nil {
		return nil, fmt.Errorf("dispatcher not configured")
	}
	cmd := domain.Command{
		ID:       domain.NewCommandID(),
		Kind:     domain.CommandKindExecute,
		DeviceID: id,
		Params:   actionParams,
		IssuedAt: time.Now(),
	}
	ch, err := h.disp.Dispatch(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("dispatch execute: %w", err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	select {
	case result := <-ch:
		if !result.Success {
			msg := "execute failed"
			if result.Err != nil {
				msg = result.Err.Message
			}
			return nil, fmt.Errorf("execute: %s", msg)
		}
		return result.Raw, nil
	case <-waitCtx.Done():
		return nil, fmt.Errorf("execute timeout")
	}
}

// ---- snapshot condensation ----

// condenseSnapshot converts a raw UiSnapshot JSON into a compact text summary for the LLM.
// This avoids sending thousands of tokens of raw JSON per observe call.
func condenseSnapshot(snapRaw json.RawMessage) string {
	var snap struct {
		PackageName  string `json:"packageName"`
		ActivityName string `json:"activityName"`
		Semantic     *struct {
			ActiveUIKey string `json:"activeUiKey"`
		} `json:"semantic"`
		Targets []struct {
			Text        string `json:"text"`
			ContentDesc string `json:"contentDesc"`
			ResourceID  string `json:"resourceId"`
			SemanticKey string `json:"semanticKey"`
			Role        string `json:"role"`
			Actionable  bool   `json:"actionable"`
		} `json:"targets"`
	}
	if err := json.Unmarshal(snapRaw, &snap); err != nil {
		return string(snapRaw) // fallback: raw JSON
	}

	var sb strings.Builder
	sb.WriteString("package: " + snap.PackageName + "\n")
	sb.WriteString("activity: " + snap.ActivityName + "\n")
	if snap.Semantic != nil {
		sb.WriteString("activeUiKey: " + snap.Semantic.ActiveUIKey + "\n")
	}
	sb.WriteString("elements:\n")
	for _, t := range snap.Targets {
		parts := []string{}
		if t.Text != "" {
			parts = append(parts, "text="+jsonQuote(t.Text))
		}
		if t.ContentDesc != "" && t.ContentDesc != t.Text {
			parts = append(parts, "desc="+jsonQuote(t.ContentDesc))
		}
		if t.ResourceID != "" {
			parts = append(parts, "id="+t.ResourceID)
		}
		if t.SemanticKey != "" {
			parts = append(parts, "key="+t.SemanticKey)
		}
		if len(parts) > 0 {
			actionable := ""
			if t.Actionable {
				actionable = " [actionable]"
			}
			sb.WriteString("  " + strings.Join(parts, " ") + actionable + "\n")
		}
	}
	return sb.String()
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// ---- handleLLMRun ----

// handleLLMRun drives the device with an LLM agent loop, records every action,
// and returns a deterministic RhinoJS script + workflow YAML.
//
//	POST /devices/{id}/record/llm-run
//	Body: {"goal":"...", "maxSteps":20, "workflowName":"...", "timeout":120000}
func (h *DeviceHandler) handleLLMRun(w http.ResponseWriter, r *http.Request, id domain.DeviceID) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.agentLoop == nil {
		http.Error(w, "LLM agent not configured: set AUTO_TOOL_ANTHROPIC_API_KEY and AUTO_TOOL_ANTHROPIC_MODEL", http.StatusServiceUnavailable)
		return
	}
	if h.disp == nil {
		http.Error(w, "dispatcher not configured", http.StatusNotImplemented)
		return
	}

	var body struct {
		Goal         string `json:"goal"`
		MaxSteps     int    `json:"maxSteps"`
		WorkflowName string `json:"workflowName"`
		Timeout      int64  `json:"timeout"` // ms
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Goal) == "" {
		http.Error(w, "goal is required", http.StatusBadRequest)
		return
	}
	if body.MaxSteps <= 0 {
		body.MaxSteps = 20
	}
	if body.Timeout <= 0 {
		body.Timeout = 300_000 // 5 min default
	}
	if body.Timeout > 600_000 {
		body.Timeout = 600_000 // 10 min cap
	}
	workflowName := strings.TrimSpace(body.WorkflowName)

	start := time.Now()
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(body.Timeout)*time.Millisecond)
	defer cancel()

	// Local entry accumulator — no RecordingStore needed (this is a self-contained run).
	var entries []RecordedEntry
	seq := 0

	// execTool is the ToolExecutor callback: bridges LLM tool calls to device operations.
	execTool := func(ctx context.Context, toolName string, input json.RawMessage) (json.RawMessage, error) {
		switch toolName {

		case "observe":
			raw, err := h.dispatchObserve(ctx, id)
			if err != nil {
				return nil, err
			}
			return json.RawMessage(jsonQuote(condenseSnapshot(raw))), nil

		case "tap":
			// LLM sends: {"kind":"click","target":{...}} or {"kind":"open_intent","intentAction":"..."}
			// Wrap into execute body: {"action": <input>}
			actionParams, err := buildActionParams(input)
			if err != nil {
				return nil, err
			}
			result, err := h.dispatchExecute(ctx, id, actionParams)
			if err != nil {
				return nil, err
			}
			seq++
			entries = append(entries, RecordedEntry{
				Sequence:      seq,
				ActionParams:  actionParams,
				SnapshotAfter: result,
				RecordedAt:    time.Now(),
			})
			return json.RawMessage(`"ok"`), nil

		case "input":
			// LLM sends: {"selector":{"kind":"...","value":"..."},"text":"..."}
			var inp struct {
				Selector json.RawMessage `json:"selector"`
				Text     string          `json:"text"`
			}
			if err := json.Unmarshal(input, &inp); err != nil {
				return nil, fmt.Errorf("input: invalid params: %w", err)
			}
			actionJSON, _ := json.Marshal(map[string]any{
				"kind":      "input_text",
				"target":    inp.Selector,
				"inputText": inp.Text,
			})
			actionParams, _ := json.Marshal(map[string]json.RawMessage{"action": actionJSON})
			result, err := h.dispatchExecute(ctx, id, actionParams)
			if err != nil {
				return nil, err
			}
			seq++
			entries = append(entries, RecordedEntry{
				Sequence:      seq,
				ActionParams:  actionParams,
				SnapshotAfter: result,
				RecordedAt:    time.Now(),
			})
			return json.RawMessage(`"ok"`), nil

		case "scroll":
			var sc struct {
				Direction string `json:"direction"`
			}
			_ = json.Unmarshal(input, &sc)
			if sc.Direction == "" {
				sc.Direction = "forward"
			}
			actionJSON, _ := json.Marshal(map[string]string{"kind": "scroll", "direction": sc.Direction})
			actionParams, _ := json.Marshal(map[string]json.RawMessage{"action": actionJSON})
			result, err := h.dispatchExecute(ctx, id, actionParams)
			if err != nil {
				return nil, err
			}
			seq++
			entries = append(entries, RecordedEntry{
				Sequence:      seq,
				ActionParams:  actionParams,
				SnapshotAfter: result,
				RecordedAt:    time.Now(),
			})
			return json.RawMessage(`"ok"`), nil

		case "back", "home":
			actionJSON, _ := json.Marshal(map[string]string{"kind": toolName})
			actionParams, _ := json.Marshal(map[string]json.RawMessage{"action": actionJSON})
			result, err := h.dispatchExecute(ctx, id, actionParams)
			if err != nil {
				return nil, err
			}
			seq++
			entries = append(entries, RecordedEntry{
				Sequence:      seq,
				ActionParams:  actionParams,
				SnapshotAfter: result,
				RecordedAt:    time.Now(),
			})
			return json.RawMessage(`"ok"`), nil

		case "done":
			return nil, llm.ErrAgentDone

		default:
			return json.RawMessage(`"unknown tool"`), nil
		}
	}

	result, err := h.agentLoop.Run(ctx, llm.AgentLoopRequest{
		SystemPrompt: agentSystemPrompt,
		Goal:         body.Goal,
		Tools:        deviceAgentTools,
		MaxSteps:     body.MaxSteps,
	}, execTool)

	durationMs := time.Since(start).Milliseconds()

	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		http.Error(w, "agent run failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		http.Error(w, "agent run timeout", http.StatusGatewayTimeout)
		return
	}

	script := generateScript(entries)

	if workflowName == "" {
		workflowName = "llm-recorded-" + start.UTC().Format("2006-01-02-15-04-05")
	}

	if h.library != nil {
		done := result.Done
		saved := store.SavedMacro{
			ID:           newRecordingID(),
			DeviceID:     string(id),
			WorkflowName: workflowName,
			Source:       "ai",
			ActionCount:  len(entries),
			DurationMs:   durationMs,
			Steps:        result.Steps,
			Done:         &done,
			Reason:       result.Reason,
			Script:       script,
			CreatedAt:    start,
		}
		if err := h.library.Save(saved); err != nil && h.log != nil {
			h.log.Warn("failed to persist LLM recording to library", "id", saved.ID, "err", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"workflowName": workflowName,
		"deviceId":     string(id),
		"done":         result.Done,
		"reason":       result.Reason,
		"steps":        result.Steps,
		"durationMs":   durationMs,
		"actionCount":  len(entries),
		"script":       script,
	})
}

// buildActionParams wraps a tap tool input into the device.execute body format.
// LLM sends: {"kind":"click","target":{...}} → {"action":{"kind":"click","target":{...}}}
func buildActionParams(tapInput json.RawMessage) (json.RawMessage, error) {
	// Validate: must have at least a "kind" field.
	var check struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(tapInput, &check); err != nil || check.Kind == "" {
		return nil, fmt.Errorf("tap: invalid action input")
	}
	return json.Marshal(map[string]json.RawMessage{"action": tapInput})
}
