package store

import (
	"path/filepath"
	"sync"
	"time"
)

// SavedMacro is a completed macro (RhinoJS script) persisted to the library.
type SavedMacro struct {
	ID           string    `json:"id"`
	DeviceID     string    `json:"deviceId"`
	WorkflowName string    `json:"workflowName"`
	Source       string    `json:"source"` // "manual" | "ai"
	ActionCount  int       `json:"actionCount"`
	DurationMs   int64     `json:"durationMs,omitempty"`
	Steps        int       `json:"steps,omitempty"`  // AI only
	Done         *bool     `json:"done,omitempty"`   // AI only
	Reason       string    `json:"reason,omitempty"` // AI only
	Script       string    `json:"script"`
	CreatedAt    time.Time `json:"createdAt"`
}

// MacroPatch holds optional fields for partial macro updates.
type MacroPatch struct {
	Script       *string `json:"script,omitempty"`
	WorkflowName *string `json:"workflowName,omitempty"`
}

// FileMacroStore is a file-backed MacroStore that persists to macros.json.
type FileMacroStore struct {
	mu      sync.Mutex
	path    string
	entries []SavedMacro
}

var _ MacroStore = (*FileMacroStore)(nil)

func NewFileMacroStore(dir string) (*FileMacroStore, error) {
	if err := ensureDir(dir); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "macros.json")
	s := &FileMacroStore{path: path}
	if err := loadJSONFile(path, &s.entries); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *FileMacroStore) Save(macro SavedMacro) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, macro)
	return writeJSONFileAtomically(s.path, s.entries)
}

func (s *FileMacroStore) List() []SavedMacro {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]SavedMacro, len(s.entries))
	for i, e := range s.entries {
		out[len(s.entries)-1-i] = e
	}
	return out
}

func (s *FileMacroStore) GetByID(id string) (SavedMacro, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		if e.ID == id {
			return e, true
		}
	}
	return SavedMacro{}, false
}

func (s *FileMacroStore) Update(id string, patch MacroPatch) (SavedMacro, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.entries {
		if e.ID == id {
			if patch.Script != nil {
				s.entries[i].Script = *patch.Script
			}
			if patch.WorkflowName != nil {
				s.entries[i].WorkflowName = *patch.WorkflowName
			}
			if err := writeJSONFileAtomically(s.path, s.entries); err != nil {
				return SavedMacro{}, false, err
			}
			return s.entries[i], true, nil
		}
	}
	return SavedMacro{}, false, nil
}

func (s *FileMacroStore) Delete(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.entries[:0]
	found := false
	for _, e := range s.entries {
		if e.ID == id {
			found = true
			continue
		}
		next = append(next, e)
	}
	if !found {
		return false, nil
	}
	s.entries = next
	return true, writeJSONFileAtomically(s.path, s.entries)
}
