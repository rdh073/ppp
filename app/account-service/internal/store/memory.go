package store

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/autosdk/ppp/account-service/internal/domain"
)

// MemoryAccountStore is a thread-safe in-memory AccountStore for tests and dev.
type MemoryAccountStore struct {
	mu        sync.RWMutex
	google    map[domain.GoogleAccountID]*domain.GoogleAccount
	instagram map[domain.InstagramAccountID]*domain.InstagramAccount
}

func NewMemoryAccountStore() *MemoryAccountStore {
	return &MemoryAccountStore{
		google:    make(map[domain.GoogleAccountID]*domain.GoogleAccount),
		instagram: make(map[domain.InstagramAccountID]*domain.InstagramAccount),
	}
}

func (s *MemoryAccountStore) SaveGoogle(_ context.Context, a *domain.GoogleAccount) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Check unique email constraint (skip if updating same record).
	for id, existing := range s.google {
		if existing.Email == a.Email && id != a.ID {
			return fmt.Errorf("%w: %s", ErrDuplicateEmail, a.Email)
		}
	}
	cp := *a
	s.google[a.ID] = &cp
	return nil
}

func (s *MemoryAccountStore) GetGoogle(_ context.Context, id domain.GoogleAccountID) (*domain.GoogleAccount, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.google[id]
	if !ok {
		return nil, fmt.Errorf("%w: google account %s", ErrNotFound, id)
	}
	cp := *a
	return &cp, nil
}

func (s *MemoryAccountStore) GetGoogleByEmail(_ context.Context, email string) (*domain.GoogleAccount, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.google {
		if a.Email == email {
			cp := *a
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("%w: google account email %s", ErrNotFound, email)
}

func (s *MemoryAccountStore) QueryGoogle(_ context.Context, q GoogleAccountQuery) (GoogleAccountPage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	limit := clampLimit(q.Limit)
	var filtered []*domain.GoogleAccount
	for _, a := range s.google {
		if q.DeviceID != "" && a.DeviceID != q.DeviceID {
			continue
		}
		if q.Status != "" && a.Status != q.Status {
			continue
		}
		cp := *a
		filtered = append(filtered, &cp)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].UpdatedAt.Equal(filtered[j].UpdatedAt) {
			return filtered[i].ID > filtered[j].ID
		}
		return filtered[i].UpdatedAt.After(filtered[j].UpdatedAt)
	})
	total := len(filtered)
	start := q.Offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	return GoogleAccountPage{
		Items:   filtered[start:end],
		Total:   total,
		Limit:   limit,
		Offset:  q.Offset,
		HasMore: end < total,
	}, nil
}

func (s *MemoryAccountStore) SaveInstagram(_ context.Context, a *domain.InstagramAccount) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, existing := range s.instagram {
		if existing.Username == a.Username && id != a.ID {
			return fmt.Errorf("%w: %s", ErrDuplicateUsername, a.Username)
		}
	}
	cp := *a
	s.instagram[a.ID] = &cp
	return nil
}

func (s *MemoryAccountStore) GetInstagram(_ context.Context, id domain.InstagramAccountID) (*domain.InstagramAccount, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.instagram[id]
	if !ok {
		return nil, fmt.Errorf("%w: instagram account %s", ErrNotFound, id)
	}
	cp := *a
	return &cp, nil
}

func (s *MemoryAccountStore) GetInstagramByUsername(_ context.Context, username string) (*domain.InstagramAccount, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.instagram {
		if a.Username == username {
			cp := *a
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("%w: instagram account username %s", ErrNotFound, username)
}

func (s *MemoryAccountStore) QueryInstagram(_ context.Context, q InstagramAccountQuery) (InstagramAccountPage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	limit := clampLimit(q.Limit)
	var filtered []*domain.InstagramAccount
	for _, a := range s.instagram {
		if q.DeviceID != "" && a.DeviceID != q.DeviceID {
			continue
		}
		if q.Status != "" && a.Status != q.Status {
			continue
		}
		if q.GoogleAccountID != "" && a.GoogleAccountID != q.GoogleAccountID {
			continue
		}
		cp := *a
		filtered = append(filtered, &cp)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].UpdatedAt.Equal(filtered[j].UpdatedAt) {
			return filtered[i].ID > filtered[j].ID
		}
		return filtered[i].UpdatedAt.After(filtered[j].UpdatedAt)
	})
	total := len(filtered)
	start := q.Offset
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}
	return InstagramAccountPage{
		Items:   filtered[start:end],
		Total:   total,
		Limit:   limit,
		Offset:  q.Offset,
		HasMore: end < total,
	}, nil
}

func clampLimit(n int) int {
	if n <= 0 {
		return 100
	}
	if n > 500 {
		return 500
	}
	return n
}
