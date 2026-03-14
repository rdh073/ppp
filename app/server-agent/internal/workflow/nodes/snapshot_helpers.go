package nodes

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

type executeSnapshotEnvelope struct {
	SnapshotBefore json.RawMessage `json:"snapshotBefore"`
	SnapshotAfter  json.RawMessage `json:"snapshotAfter"`
}

func parseSnapshotRaw(raw string) (*domain.UiSnapshot, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("snapshot payload is empty")
	}
	var snapshot domain.UiSnapshot
	if err := json.Unmarshal([]byte(raw), &snapshot); err == nil && (snapshot.ID != "" || len(snapshot.Targets) > 0 || snapshot.PackageName != "") {
		return &snapshot, nil
	}
	var envelope executeSnapshotEnvelope
	if err := json.Unmarshal([]byte(raw), &envelope); err == nil && len(envelope.SnapshotAfter) > 0 {
		if err := json.Unmarshal(envelope.SnapshotAfter, &snapshot); err != nil {
			return nil, fmt.Errorf("decode snapshotAfter: %w", err)
		}
		return &snapshot, nil
	}
	return nil, fmt.Errorf("snapshot payload is not a recognised snapshot shape")
}

func extractSnapshotAfterRaw(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !json.Valid([]byte(raw)) {
		return "", false
	}
	var envelope executeSnapshotEnvelope
	if err := json.Unmarshal([]byte(raw), &envelope); err == nil && len(envelope.SnapshotAfter) > 0 && json.Valid(envelope.SnapshotAfter) {
		return string(envelope.SnapshotAfter), true
	}
	return raw, true
}
