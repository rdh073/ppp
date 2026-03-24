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
	"github.com/autosdk/ppp/server-agent/internal/projection"
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

// recordedSnapshotTarget is a minimal view of a UiTarget from the execute result snapshot,
// used only for coordinate enrichment during script generation.
type recordedSnapshotTarget struct {
	Bounds      [4]int `json:"bounds"` // [left, top, right, bottom]
	ResourceID  string `json:"resourceId"`
	SemanticKey string `json:"semanticKey"`
	Text        string `json:"text"`
	Actionable  bool   `json:"actionable"`
}

// recordedExecuteResult is a minimal parse of the {snapshotBefore, snapshotAfter} JSON
// returned by device.execute, used only to enrich coordinate taps during script generation.
type recordedExecuteResult struct {
	SnapshotBefore *struct {
		Targets []recordedSnapshotTarget `json:"targets"`
	} `json:"snapshotBefore"`
}

// resolveCoordinateSelector finds the best actionable accessibility selector in snapshotBefore
// whose bounds contain the point (x, y). Returns a JS object literal string, or "" if no match.
//
// Priority: semanticKey > resourceId > text. Falls back to "" so the caller keeps the
// raw coordinate selector.
func resolveCoordinateSelector(x, y int, executeRes json.RawMessage) string {
	if len(executeRes) == 0 {
		return ""
	}
	var res recordedExecuteResult
	if err := json.Unmarshal(executeRes, &res); err != nil || res.SnapshotBefore == nil {
		return ""
	}
	// Pick the smallest-area actionable target containing (x, y) — the innermost/most-specific element.
	var best *recordedSnapshotTarget
	bestArea := -1
	for i := range res.SnapshotBefore.Targets {
		t := &res.SnapshotBefore.Targets[i]
		if !t.Actionable {
			continue
		}
		// bounds: [left, top, right, bottom]
		if x < t.Bounds[0] || x > t.Bounds[2] || y < t.Bounds[1] || y > t.Bounds[3] {
			continue
		}
		area := (t.Bounds[2] - t.Bounds[0]) * (t.Bounds[3] - t.Bounds[1])
		if best == nil || area < bestArea {
			best = t
			bestArea = area
		}
	}
	if best == nil {
		return ""
	}
	switch {
	case best.SemanticKey != "":
		return fmt.Sprintf("{ kind: \"semantic_key\", value: %s }", jsStr(best.SemanticKey))
	case best.ResourceID != "":
		return fmt.Sprintf("{ kind: \"resource_id\", value: %s }", jsStr(best.ResourceID))
	case best.Text != "":
		return fmt.Sprintf("{ kind: \"text\", value: %s }", jsStr(best.Text))
	}
	return ""
}

// generateScript converts a sequence of recorded entries into a RhinoJS source string.
func generateScript(entries []RecordedEntry) string {
	var sb strings.Builder
	for _, entry := range entries {
		line, err := actionToJS(entry.ActionParams, entry.SnapshotAfter)
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
// executeResult is the full {snapshotBefore, snapshotAfter} JSON from the execute response;
// it is used to enrich coordinate taps with accessibility selectors. Pass nil to skip enrichment.
func actionToJS(params json.RawMessage, executeResult json.RawMessage) (string, error) {
	var body struct {
		Action struct {
			Kind         string           `json:"kind"`
			Target       *json.RawMessage `json:"target"`
			InputText    string           `json:"inputText"`
			IntentAction string           `json:"intentAction"`
			Package      string           `json:"package"`
			Direction    string           `json:"direction"`
			StartX       int              `json:"startX"`
			StartY       int              `json:"startY"`
			EndX         int              `json:"endX"`
			EndY         int              `json:"endY"`
			DurationMs   int              `json:"durationMs"`
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
		// Enrich coordinate taps with an accessibility selector from snapshotBefore.
		if a.Target != nil {
			var sel struct {
				Kind  string `json:"kind"`
				Value string `json:"value"`
			}
			if json.Unmarshal(*a.Target, &sel) == nil && sel.Kind == "coordinate" {
				var x, y int
				if _, scanErr := fmt.Sscanf(sel.Value, "%d,%d", &x, &y); scanErr == nil {
					if enriched := resolveCoordinateSelector(x, y, executeResult); enriched != "" {
						target = enriched
					}
				}
			}
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
	case "swipe":
		dur := a.DurationMs
		if dur <= 0 {
			dur = 300
		}
		return fmt.Sprintf("swipe(%d, %d, %d, %d, %d);", a.StartX, a.StartY, a.EndX, a.EndY, dur), nil
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

// publishRecordingEvent pushes a recording lifecycle event to the projection hub.
// Topic: "recording.<deviceId>". No-op when h.publish is nil.
func (h *DeviceHandler) publishRecordingEvent(deviceID domain.DeviceID, eventType string, payload any) {
	if h.publish == nil {
		return
	}
	h.publish.PublishProjection(projection.Event{
		Topic:      "recording." + string(deviceID),
		Type:       eventType,
		EntityID:   string(deviceID),
		OccurredAt: time.Now().UTC(),
		Payload:    payload,
	})
}

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
	h.publishRecordingEvent(id, "recording.started", map[string]any{
		"recordingId": rec.ID,
		"startedAt":   rec.StartedAt,
	})
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
	h.publishRecordingEvent(id, "recording.stopped", map[string]any{
		"recordingId": rec.ID,
		"actionCount": len(entries),
		"durationMs":  durationMs,
	})

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

// handleRecordEntry manually appends a pre-built entry to the active recording.
// Used by the dashboard when the user interacts via scrcpy (direct touch injection) so
// the action can be recorded without re-executing it on the device.
//
//	POST /devices/{id}/record/entry
//	Body: {"actionParams": {...}, "executeResult": {...}}
func (h *DeviceHandler) handleRecordEntry(w http.ResponseWriter, r *http.Request, id domain.DeviceID) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.recordings == nil {
		http.Error(w, "recording not configured", http.StatusNotImplemented)
		return
	}
	var body struct {
		ActionParams  json.RawMessage `json:"actionParams"`
		ExecuteResult json.RawMessage `json:"executeResult"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil || len(body.ActionParams) == 0 {
		http.Error(w, "invalid body: actionParams required", http.StatusBadRequest)
		return
	}
	rec, ok := h.recordings.Get(id)
	if !ok {
		http.Error(w, "no active recording for device", http.StatusNotFound)
		return
	}
	rec.Append(body.ActionParams, body.ExecuteResult)
	entries, _ := rec.snapshot()
	h.publishRecordingEvent(id, "recording.entry", map[string]any{
		"recordingId": rec.ID,
		"actionCount": len(entries),
	})
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"actionCount": len(entries)})
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
