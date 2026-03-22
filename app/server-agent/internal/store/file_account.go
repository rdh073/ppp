package store

import (
	"path/filepath"
	"sync"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// FileAccountStore is a file-backed AccountStore that persists to accounts.json.
type FileAccountStore struct {
	mu      sync.Mutex
	path    string
	entries []domain.Account
}

var _ AccountStore = (*FileAccountStore)(nil)

func NewFileAccountStore(dir string) (*FileAccountStore, error) {
	if err := ensureDir(dir); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "accounts.json")
	s := &FileAccountStore{path: path}
	if err := loadJSONFile(path, &s.entries); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *FileAccountStore) Save(a domain.Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = append(s.entries, a)
	return writeJSONFileAtomically(s.path, s.entries)
}

func (s *FileAccountStore) GetByID(id string) (domain.Account, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		if e.ID == id {
			return e, true
		}
	}
	return domain.Account{}, false
}

func (s *FileAccountStore) List(kind, deviceID string) []domain.Account {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Account, 0, len(s.entries))
	for i := len(s.entries) - 1; i >= 0; i-- {
		e := s.entries[i]
		if kind != "" && e.Kind != kind {
			continue
		}
		if deviceID != "" && e.DeviceID != deviceID {
			continue
		}
		out = append(out, e)
	}
	return out
}

func (s *FileAccountStore) UpdateStatus(id string, status domain.AccountStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.entries {
		if e.ID == id {
			s.entries[i].Status = status
			return writeJSONFileAtomically(s.path, s.entries)
		}
	}
	return nil
}

func (s *FileAccountStore) UpdateDeviceID(id, deviceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, e := range s.entries {
		if e.ID == id {
			s.entries[i].DeviceID = deviceID
			return writeJSONFileAtomically(s.path, s.entries)
		}
	}
	return nil
}

func (s *FileAccountStore) FindActiveByKindOnDevice(deviceID, kind string) (domain.Account, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		if e.DeviceID == deviceID && e.Kind == kind && e.Status == domain.AccountStatusActive {
			return e, true
		}
	}
	return domain.Account{}, false
}
