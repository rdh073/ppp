package store

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// FilePersonaStore is a file-backed PersonaStore that persists to personas.json.
type FilePersonaStore struct {
	mu      sync.Mutex
	path    string
	entries []domain.Persona
}

var _ PersonaStore = (*FilePersonaStore)(nil)

func NewFilePersonaStore(dir string) (*FilePersonaStore, error) {
	if err := ensureDir(dir); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "personas.json")
	s := &FilePersonaStore{path: path}
	if err := loadJSONFile(path, &s.entries); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *FilePersonaStore) Save(p domain.Persona) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Upsert by ID.
	for i, e := range s.entries {
		if e.ID == p.ID {
			s.entries[i] = p
			return writeJSONFileAtomically(s.path, s.entries)
		}
	}
	s.entries = append(s.entries, p)
	return writeJSONFileAtomically(s.path, s.entries)
}

func (s *FilePersonaStore) GetByID(id string) (domain.Persona, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		if e.ID == id {
			return e, true
		}
	}
	return domain.Persona{}, false
}

func (s *FilePersonaStore) List(kind, status string) []domain.Persona {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Persona, 0, len(s.entries))
	for i := len(s.entries) - 1; i >= 0; i-- {
		e := s.entries[i]
		if kind != "" && e.Kind != kind {
			continue
		}
		if status != "" && string(e.Status) != status {
			continue
		}
		out = append(out, e)
	}
	return out
}

func (s *FilePersonaStore) UpdateStatus(id string, status domain.PersonaStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.entries {
		if e.ID == id {
			s.entries[i].Status = status
			s.entries[i].UpdatedAt = time.Now()
			return writeJSONFileAtomically(s.path, s.entries)
		}
	}
	return nil
}

func (s *FilePersonaStore) Delete(id string) (bool, error) {
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
