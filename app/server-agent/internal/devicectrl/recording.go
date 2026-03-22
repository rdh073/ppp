package devicectrl

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/store"
)

// ---- recording domain ----

// RecordedEntry holds one captured execute action and its resulting snapshots.
type RecordedEntry struct {
	Sequence      int
	ActionParams  json.RawMessage // raw body from POST /devices/{id}/execute
	SnapshotAfter json.RawMessage // result.Raw from device.execute: {snapshotBefore, snapshotAfter}
	RecordedAt    time.Time
}

// Recording accumulates entries for one active recording session.
type Recording struct {
	ID        string
	DeviceID  domain.DeviceID
	StartedAt time.Time
	entries   []RecordedEntry
	mu        sync.Mutex
}

// Append adds a new entry. Safe for concurrent use.
func (r *Recording) Append(actionParams, snapshotAfter json.RawMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, RecordedEntry{
		Sequence:      len(r.entries) + 1,
		ActionParams:  actionParams,
		SnapshotAfter: snapshotAfter,
		RecordedAt:    time.Now(),
	})
}

// snapshot returns a point-in-time copy of entries and the start time.
func (r *Recording) snapshot() ([]RecordedEntry, time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := make([]RecordedEntry, len(r.entries))
	copy(cp, r.entries)
	return cp, r.StartedAt
}

// ---- RecordingStore ----

// RecordingStore holds at most one active recording per device (in-memory).
type RecordingStore struct {
	mu     sync.Mutex
	active map[domain.DeviceID]*Recording
}

// NewRecordingStore returns an empty store.
func NewRecordingStore() *RecordingStore {
	return &RecordingStore{active: make(map[domain.DeviceID]*Recording)}
}

func newRecordingID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("rec-%d", time.Now().UnixNano())
	}
	return "rec-" + hex.EncodeToString(b)
}

// Start begins a new recording for deviceID. Returns conflict error if one is already active.
func (s *RecordingStore) Start(deviceID domain.DeviceID) (*Recording, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.active[deviceID]; ok {
		return nil, fmt.Errorf("recording already active for device %s", deviceID)
	}
	rec := &Recording{
		ID:        newRecordingID(),
		DeviceID:  deviceID,
		StartedAt: time.Now(),
	}
	s.active[deviceID] = rec
	return rec, nil
}

// Get returns the active recording for deviceID if any.
func (s *RecordingStore) Get(deviceID domain.DeviceID) (*Recording, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.active[deviceID]
	return rec, ok
}

// Stop removes and returns the active recording for deviceID.
func (s *RecordingStore) Stop(deviceID domain.DeviceID) (*Recording, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.active[deviceID]
	if ok {
		delete(s.active, deviceID)
	}
	return rec, ok
}

// ---- script generator ----

// generateScript converts a sequence of recorded entries into a RhinoJS source string.
func generateScript(entries []RecordedEntry) string {
	var sb strings.Builder
	for _, entry := range entries {
		line, err := actionToJS(entry.ActionParams)
		if err != nil || line == "" {
			continue
		}
		sb.WriteString(line)
		sb.WriteByte('\n')
		if wait := insertWait(entry.SnapshotAfter); wait != "" {
			sb.WriteString(wait)
		}
	}
	sb.WriteString("return { done: true };\n")
	return sb.String()
}

// actionToJS converts one execute body to a JS statement.
func actionToJS(params json.RawMessage) (string, error) {
	var body struct {
		Action struct {
			Kind         string           `json:"kind"`
			Target       *json.RawMessage `json:"target"`
			InputText    string           `json:"inputText"`
			IntentAction string           `json:"intentAction"`
			Package      string           `json:"package"`
			Direction    string           `json:"direction"`
		} `json:"action"`
	}
	if err := json.Unmarshal(params, &body); err != nil {
		return "", err
	}
	a := body.Action
	switch a.Kind {
	case "click", "long_press":
		target, err := selectorToJS(a.Target)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("tap({ kind: %s, target: %s });", jsStr(a.Kind), target), nil
	case "input_text":
		target, err := selectorToJS(a.Target)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("input(%s, %s);", target, jsStr(a.InputText)), nil
	case "open_app":
		target, err := selectorToJS(a.Target)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("tap({ kind: \"open_app\", target: %s });", target), nil
	case "open_intent":
		if a.Package != "" {
			return fmt.Sprintf("tap({ kind: \"open_intent\", intentAction: %s, package: %s });", jsStr(a.IntentAction), jsStr(a.Package)), nil
		}
		return fmt.Sprintf("tap({ kind: \"open_intent\", intentAction: %s });", jsStr(a.IntentAction)), nil
	case "scroll":
		dir := a.Direction
		if dir == "" {
			dir = "forward"
		}
		if a.Target != nil {
			target, err := selectorToJS(a.Target)
			if err == nil {
				return fmt.Sprintf("scroll(%s, %s);", target, jsStr(dir)), nil
			}
		}
		return fmt.Sprintf("scroll(null, %s);", jsStr(dir)), nil
	case "back":
		return "back();", nil
	case "home":
		return "home();", nil
	default:
		return fmt.Sprintf("// unrecognised action kind: %s", a.Kind), nil
	}
}

// selectorToJS converts a selector JSON object to an inline JS object literal.
func selectorToJS(raw *json.RawMessage) (string, error) {
	if raw == nil {
		return "null", nil
	}
	var sel struct {
		Kind  string `json:"kind"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(*raw, &sel); err != nil {
		return "", err
	}
	return fmt.Sprintf("{ kind: %s, value: %s }", jsStr(sel.Kind), jsStr(sel.Value)), nil
}

// insertWait inspects the execute result (contains snapshotBefore + snapshotAfter) and
// returns zero or more JS wait statements to insert after the action.
func insertWait(executeResult json.RawMessage) string {
	if len(executeResult) == 0 {
		return ""
	}
	var result struct {
		SnapshotBefore *struct {
			Semantic *struct {
				ActiveUIKey string `json:"activeUiKey"`
			} `json:"semantic"`
		} `json:"snapshotBefore"`
		SnapshotAfter *struct {
			Semantic *struct {
				ActiveUIKey string `json:"activeUiKey"`
			} `json:"semantic"`
			Targets []struct {
				Text string `json:"text"`
			} `json:"targets"`
		} `json:"snapshotAfter"`
	}
	if err := json.Unmarshal(executeResult, &result); err != nil {
		return ""
	}

	var sb strings.Builder
	beforeKey := ""
	afterKey := ""
	if result.SnapshotBefore != nil && result.SnapshotBefore.Semantic != nil {
		beforeKey = result.SnapshotBefore.Semantic.ActiveUIKey
	}
	if result.SnapshotAfter != nil && result.SnapshotAfter.Semantic != nil {
		afterKey = result.SnapshotAfter.Semantic.ActiveUIKey
	}

	if beforeKey != afterKey && afterKey != "" {
		sb.WriteString("awaitEvent(\"activity_created\", {}, 5000);\n")
	}

	// Safety-net: waitFor the first visible text token in the new snapshot.
	if result.SnapshotAfter != nil {
		for _, t := range result.SnapshotAfter.Targets {
			if t.Text != "" {
				sb.WriteString(fmt.Sprintf("waitFor({ kind: \"text\", value: %s }, 3000);\n", jsStr(t.Text)))
				break
			}
		}
	}

	return sb.String()
}

// generateWorkflowYAML wraps a JS script in a minimal single-step workflow YAML.
func generateWorkflowYAML(name, scriptSource string) string {
	lines := strings.Split(strings.TrimRight(scriptSource, "\n"), "\n")
	var indented strings.Builder
	for _, l := range lines {
		indented.WriteString("        " + l + "\n")
	}
	return fmt.Sprintf(`name: %s
version: 1
entry: run
steps:
  run:
    trigger: {}
    script:
      source: |
%s      timeout: 120s
    on_success: terminal
    on_failure: terminal
`, name, indented.String())
}

func GenerateWorkflowYAML(name, scriptSource string) string {
	return generateWorkflowYAML(name, scriptSource)
}

// jsStr encodes s as a JSON/JS string literal (e.g. "hello world").
func jsStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// ---- HTTP handler methods (attached to DeviceHandler) ----

// handleRecordStart begins a new recording session for the device.
//
//	POST /devices/{id}/record/start
func (h *DeviceHandler) handleRecordStart(w http.ResponseWriter, r *http.Request, id domain.DeviceID) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.recordings == nil {
		http.Error(w, "recording not configured", http.StatusNotImplemented)
		return
	}
	rec, err := h.recordings.Start(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"recordingId": rec.ID,
		"deviceId":    string(id),
		"startedAt":   rec.StartedAt,
	})
}

// handleRecordStop ends the recording session, generates a JS script and workflow YAML.
//
//	POST /devices/{id}/record/stop
//	Body (optional): {"workflowName": "my-flow"}
func (h *DeviceHandler) handleRecordStop(w http.ResponseWriter, r *http.Request, id domain.DeviceID) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.recordings == nil {
		http.Error(w, "recording not configured", http.StatusNotImplemented)
		return
	}
	var body struct {
		WorkflowName string `json:"workflowName"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body)

	rec, ok := h.recordings.Stop(id)
	if !ok {
		http.Error(w, "no active recording for device", http.StatusNotFound)
		return
	}

	entries, startedAt := rec.snapshot()
	durationMs := time.Since(startedAt).Milliseconds()

	workflowName := body.WorkflowName
	if workflowName == "" {
		workflowName = "recorded-" + startedAt.UTC().Format("2006-01-02-15-04-05")
	}

	script := generateScript(entries)

	if h.library != nil {
		saved := store.SavedMacro{
			ID:           rec.ID,
			DeviceID:     string(id),
			WorkflowName: workflowName,
			Source:       "manual",
			ActionCount:  len(entries),
			DurationMs:   durationMs,
			Script:       script,
			CreatedAt:    startedAt,
		}
		if err := h.library.Save(saved); err != nil && h.log != nil {
			h.log.Warn("failed to persist recording to library", "id", rec.ID, "err", err)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"recordingId":  rec.ID,
		"deviceId":     string(id),
		"actionCount":  len(entries),
		"durationMs":   durationMs,
		"script":       script,
		"workflowName": workflowName,
	})
}

// handleRecordStatus returns the current recording status for a device.
//
//	GET /devices/{id}/record/status
func (h *DeviceHandler) handleRecordStatus(w http.ResponseWriter, r *http.Request, id domain.DeviceID) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if h.recordings == nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"active": false, "deviceId": string(id)})
		return
	}
	rec, ok := h.recordings.Get(id)
	if !ok {
		_ = json.NewEncoder(w).Encode(map[string]any{"active": false, "deviceId": string(id)})
		return
	}
	entries, startedAt := rec.snapshot()
	_ = json.NewEncoder(w).Encode(map[string]any{
		"active":      true,
		"recordingId": rec.ID,
		"deviceId":    string(id),
		"actionCount": len(entries),
		"startedAt":   startedAt,
	})
}
