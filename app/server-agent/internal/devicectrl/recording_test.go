package devicectrl

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/autosdk/ppp/server-agent/internal/domain"
	"github.com/autosdk/ppp/server-agent/internal/registry"
)

// ---- script generator unit tests ----

func TestActionToJS_Click(t *testing.T) {
	params := json.RawMessage(`{"action":{"kind":"click","target":{"kind":"text","value":"Sign in"}}}`)
	got, err := actionToJS(params, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `tap({ kind: "click", target: { kind: "text", value: "Sign in" } });`
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestActionToJS_InputText(t *testing.T) {
	params := json.RawMessage(`{"action":{"kind":"input_text","target":{"kind":"resource_id","value":"com.example:id/email"},"inputText":"user@example.com"}}`)
	got, err := actionToJS(params, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `input({ kind: "resource_id", value: "com.example:id/email" }, "user@example.com");`
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestActionToJS_OpenIntent(t *testing.T) {
	params := json.RawMessage(`{"action":{"kind":"open_intent","intentAction":"android.settings.PRIVATE_DNS_SETTINGS"}}`)
	got, err := actionToJS(params, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `tap({ kind: "open_intent", intentAction: "android.settings.PRIVATE_DNS_SETTINGS" });`
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestActionToJS_Back(t *testing.T) {
	params := json.RawMessage(`{"action":{"kind":"back"}}`)
	got, err := actionToJS(params, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "back();" {
		t.Errorf("got %q, want back();", got)
	}
}

// ---- coordinate enrichment tests ----

func makeExecuteResult(targets []map[string]any) json.RawMessage {
	snap := map[string]any{
		"snapshotBefore": map[string]any{"targets": targets},
		"snapshotAfter":  map[string]any{"targets": []any{}},
	}
	b, _ := json.Marshal(snap)
	return b
}

func TestActionToJS_CoordinateTap_EnrichesSemanticKey(t *testing.T) {
	exec := makeExecuteResult([]map[string]any{
		{"bounds": []int{100, 200, 300, 250}, "semanticKey": "button.sign_in", "resourceId": "com.ex:id/btn", "text": "Sign In", "actionable": true},
	})
	params := json.RawMessage(`{"action":{"kind":"click","target":{"kind":"coordinate","value":"200,225"}}}`)
	got, err := actionToJS(params, exec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `tap({ kind: "click", target: { kind: "semantic_key", value: "button.sign_in" } });`
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestActionToJS_CoordinateTap_FallsBackToResourceID(t *testing.T) {
	exec := makeExecuteResult([]map[string]any{
		{"bounds": []int{0, 0, 400, 100}, "resourceId": "com.example:id/btn_login", "actionable": true},
	})
	params := json.RawMessage(`{"action":{"kind":"click","target":{"kind":"coordinate","value":"200,50"}}}`)
	got, err := actionToJS(params, exec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `tap({ kind: "click", target: { kind: "resource_id", value: "com.example:id/btn_login" } });`
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestActionToJS_CoordinateTap_FallsBackToText(t *testing.T) {
	exec := makeExecuteResult([]map[string]any{
		{"bounds": []int{0, 0, 400, 100}, "text": "Continue", "actionable": true},
	})
	params := json.RawMessage(`{"action":{"kind":"click","target":{"kind":"coordinate","value":"200,50"}}}`)
	got, err := actionToJS(params, exec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `tap({ kind: "click", target: { kind: "text", value: "Continue" } });`
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestActionToJS_CoordinateTap_OutsideBounds_KeepsCoordinate(t *testing.T) {
	exec := makeExecuteResult([]map[string]any{
		{"bounds": []int{0, 0, 100, 100}, "semanticKey": "button.ok", "actionable": true},
	})
	params := json.RawMessage(`{"action":{"kind":"click","target":{"kind":"coordinate","value":"500,500"}}}`)
	got, err := actionToJS(params, exec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `tap({ kind: "click", target: { kind: "coordinate", value: "500,500" } });`
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestActionToJS_CoordinateTap_NotActionable_KeepsCoordinate(t *testing.T) {
	exec := makeExecuteResult([]map[string]any{
		{"bounds": []int{0, 0, 400, 400}, "semanticKey": "text.label", "actionable": false},
	})
	params := json.RawMessage(`{"action":{"kind":"click","target":{"kind":"coordinate","value":"200,200"}}}`)
	got, err := actionToJS(params, exec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `tap({ kind: "click", target: { kind: "coordinate", value: "200,200" } });`
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestActionToJS_CoordinateTap_NilSnapshot_KeepsCoordinate(t *testing.T) {
	params := json.RawMessage(`{"action":{"kind":"click","target":{"kind":"coordinate","value":"100,200"}}}`)
	got, err := actionToJS(params, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `tap({ kind: "click", target: { kind: "coordinate", value: "100,200" } });`
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestResolveCoordinateSelector_SmallestAreaWins(t *testing.T) {
	// Large parent contains the point; small inner button also contains it.
	// The inner (smaller area) target should win even though the parent comes first.
	exec := makeExecuteResult([]map[string]any{
		{"bounds": []int{0, 0, 1080, 1920}, "semanticKey": "list.apps_list_view", "actionable": true},
		{"bounds": []int{333, 207, 542, 373}, "semanticKey": "button.icon", "resourceId": "com.android.launcher3:id/icon", "actionable": true},
	})
	got := resolveCoordinateSelector(437, 290, exec)
	want := `{ kind: "semantic_key", value: "button.icon" }`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveCoordinateSelector_SelectorPriority(t *testing.T) {
	// Single target containing the point — verify semantic_key > resource_id > text priority.
	exec := makeExecuteResult([]map[string]any{
		{"bounds": []int{0, 0, 500, 500}, "semanticKey": "button.ok", "resourceId": "com.ex:id/btn", "text": "OK", "actionable": true},
	})
	got := resolveCoordinateSelector(250, 250, exec)
	want := `{ kind: "semantic_key", value: "button.ok" }`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestInsertWait_ActivityChanged(t *testing.T) {
	result := json.RawMessage(`{
		"snapshotBefore":{"semantic":{"activeUiKey":"com.android.settings.Settings"}},
		"snapshotAfter":{"semantic":{"activeUiKey":"com.android.settings.PrivacyDashboard"},"targets":[{"text":"Private DNS"}]}
	}`)
	got := insertWait(result)
	if !strings.Contains(got, `awaitEvent("activity_created"`) {
		t.Errorf("expected awaitEvent in wait string, got: %q", got)
	}
	if !strings.Contains(got, `waitFor({ kind: "text", value: "Private DNS" }`) {
		t.Errorf("expected waitFor in wait string, got: %q", got)
	}
}

func TestInsertWait_SameScreen_NoAwaitEvent(t *testing.T) {
	result := json.RawMessage(`{
		"snapshotBefore":{"semantic":{"activeUiKey":"com.example.app.Main"}},
		"snapshotAfter":{"semantic":{"activeUiKey":"com.example.app.Main"},"targets":[{"text":"Hello"}]}
	}`)
	got := insertWait(result)
	if strings.Contains(got, "awaitEvent") {
		t.Errorf("should not emit awaitEvent when screen unchanged, got: %q", got)
	}
	if !strings.Contains(got, "waitFor") {
		t.Errorf("should still emit waitFor safety net, got: %q", got)
	}
}

func TestGenerateScript_ProducesReturn(t *testing.T) {
	entries := []RecordedEntry{
		{
			ActionParams:  json.RawMessage(`{"action":{"kind":"back"}}`),
			SnapshotAfter: json.RawMessage(`{}`),
		},
	}
	script := generateScript(entries)
	if !strings.Contains(script, "back();") {
		t.Errorf("expected back() in script, got: %q", script)
	}
	if !strings.HasSuffix(strings.TrimSpace(script), "return { done: true };") {
		t.Errorf("script should end with return statement, got: %q", script)
	}
}

func TestGenerateWorkflowYAML(t *testing.T) {
	yaml := generateWorkflowYAML("my-workflow", "back();\nreturn { done: true };\n")
	if !strings.Contains(yaml, "name: my-workflow") {
		t.Errorf("expected name in yaml")
	}
	if !strings.Contains(yaml, "back();") {
		t.Errorf("expected script content in yaml")
	}
	if !strings.Contains(yaml, "on_success: terminal") {
		t.Errorf("expected on_success in yaml")
	}
}

// ---- RecordingStore unit tests ----

func TestRecordingStore_StartStopGet(t *testing.T) {
	store := NewRecordingStore()
	id := domain.DeviceID("test-device")

	rec, err := store.Start(id)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if rec.ID == "" {
		t.Error("recording ID should not be empty")
	}

	// Second start for same device should fail
	_, err = store.Start(id)
	if err == nil {
		t.Error("expected conflict error on second Start")
	}

	// Get should return the recording
	got, ok := store.Get(id)
	if !ok || got.ID != rec.ID {
		t.Error("Get should return active recording")
	}

	// Append
	rec.Append(json.RawMessage(`{"action":{"kind":"back"}}`), json.RawMessage(`{}`))
	entries, _ := rec.snapshot()
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}

	// Stop
	stopped, ok := store.Stop(id)
	if !ok || stopped.ID != rec.ID {
		t.Error("Stop should return the recording")
	}

	// Get after Stop should return false
	_, ok = store.Get(id)
	if ok {
		t.Error("Get after Stop should return false")
	}
}

// ---- HTTP handler integration tests ----

func TestDeviceHandler_RecordLifecycle(t *testing.T) {
	reg := registry.New()
	store := NewRecordingStore()
	h := NewDeviceHandler(reg, nil).WithRecording(store)

	deviceID := "emulator-5554"

	// POST /devices/{id}/record/start
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/devices/"+deviceID+"/record/start", nil)
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("start: got %d, want 200", w.Code)
	}

	// GET /devices/{id}/record/status
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/devices/"+deviceID+"/record/status", nil)
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want 200", w.Code)
	}
	var statusResp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&statusResp); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if statusResp["active"] != true {
		t.Errorf("expected active=true, got %v", statusResp["active"])
	}

	// POST /devices/{id}/record/start again → 409
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/devices/"+deviceID+"/record/start", nil)
	h.ServeHTTP(w, r)
	if w.Code != http.StatusConflict {
		t.Errorf("duplicate start: want 409, got %d", w.Code)
	}

	// POST /devices/{id}/record/stop
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/devices/"+deviceID+"/record/stop", nil)
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("stop: got %d, want 200", w.Code)
	}
	var stopResp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&stopResp); err != nil {
		t.Fatalf("decode stop: %v", err)
	}
	if _, ok := stopResp["script"]; !ok {
		t.Error("stop response should contain script field")
	}

	// Stop again → 404
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodPost, "/devices/"+deviceID+"/record/stop", nil)
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Errorf("stop after stop: want 404, got %d", w.Code)
	}
}
