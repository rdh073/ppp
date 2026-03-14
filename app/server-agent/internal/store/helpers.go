package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

func ensureDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("store dir is required")
	}
	return os.MkdirAll(dir, 0o755)
}

func loadJSONFile(path string, dst any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, dst)
}

func writeJSONFileAtomically(path string, src any) error {
	dir := filepath.Dir(path)
	if err := ensureDir(dir); err != nil {
		return err
	}
	data, err := json.MarshalIndent(src, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func cloneTask(task *domain.Task) *domain.Task {
	if task == nil {
		return nil
	}
	cp := *task
	return &cp
}

func cloneWorkflowState(ws *domain.WorkflowState) *domain.WorkflowState {
	if ws == nil {
		return nil
	}
	cp := *ws
	cp.Artifacts = make(map[string]string, len(ws.Artifacts))
	for k, v := range ws.Artifacts {
		cp.Artifacts[k] = v
	}
	cp.WaitingFor = append([]domain.EventKind(nil), ws.WaitingFor...)
	return &cp
}

func cloneAcceptedEventRecord(record domain.AcceptedEventRecord) domain.AcceptedEventRecord {
	return domain.AcceptedEventRecord{
		Event:      cloneEvent(record.Event),
		AcceptedAt: record.AcceptedAt,
		Source:     record.Source,
	}
}

func cloneDeadLetterRecord(record domain.DeadLetterRecord) domain.DeadLetterRecord {
	cp := record
	cp.Payload = append(json.RawMessage(nil), record.Payload...)
	return cp
}

func cloneCommandOutboxRecord(record *domain.CommandOutboxRecord) *domain.CommandOutboxRecord {
	if record == nil {
		return nil
	}
	cp := *record
	cp.Command = cloneCommand(record.Command)
	cp.LastResult = cloneCommandResult(record.LastResult)
	return &cp
}

func cloneEvent(event domain.Event) domain.Event {
	cp := event
	cp.Payload = cloneEventPayload(event.Payload)
	return cp
}

func cloneEventPayload(payload any) any {
	switch v := payload.(type) {
	case nil:
		return nil
	case json.RawMessage:
		return append(json.RawMessage(nil), v...)
	case []byte:
		return append([]byte(nil), v...)
	case domain.ToolResultPayload:
		v.Result = append([]byte(nil), v.Result...)
		return v
	case domain.CommandResponsePayload:
		v.Raw = append([]byte(nil), v.Raw...)
		return v
	case domain.AgentOnlinePayload:
		v.Capabilities = append([]domain.Capability(nil), v.Capabilities...)
		return v
	default:
		return v
	}
}

func cloneCommand(cmd domain.Command) domain.Command {
	cp := cmd
	cp.Params = append(json.RawMessage(nil), cmd.Params...)
	return cp
}

func cloneCommandResult(result *domain.CommandResult) *domain.CommandResult {
	if result == nil {
		return nil
	}
	cp := *result
	cp.Raw = append(json.RawMessage(nil), result.Raw...)
	if result.Err != nil {
		errCopy := *result.Err
		cp.Err = &errCopy
	}
	return &cp
}

func nowOr(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now()
	}
	return t
}
