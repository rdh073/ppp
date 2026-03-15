package workflow

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// ErrWorkflowDefNotFound is returned when a named workflow definition does not
// exist. The orchestrator treats this as a fatal, non-retryable task failure.
var ErrWorkflowDefNotFound = errors.New("workflow def not found")

// DefStore is the read/write interface for WorkflowDef storage.
type DefStore interface {
	Get(ctx context.Context, name string) (*domain.WorkflowDef, error)
	Put(ctx context.Context, name string, def *domain.WorkflowDef) error
	Delete(ctx context.Context, name string) error
	List(ctx context.Context) ([]*domain.WorkflowDef, error)
}

// MemoryDefStore is a concurrent-safe in-memory DefStore.
type MemoryDefStore struct {
	mu   sync.RWMutex
	defs map[string]*domain.WorkflowDef
}

func NewMemoryDefStore() *MemoryDefStore {
	return &MemoryDefStore{defs: make(map[string]*domain.WorkflowDef)}
}

func (s *MemoryDefStore) Get(_ context.Context, name string) (*domain.WorkflowDef, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.defs[name]
	if !ok {
		return nil, fmt.Errorf("workflow def %q: %w", name, ErrWorkflowDefNotFound)
	}
	return d, nil
}

func (s *MemoryDefStore) Put(_ context.Context, name string, def *domain.WorkflowDef) error {
	if err := Validate(def); err != nil {
		return fmt.Errorf("workflow def %q invalid: %w", name, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.defs[name] = def
	return nil
}

func (s *MemoryDefStore) Delete(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.defs, name)
	return nil
}

func (s *MemoryDefStore) List(_ context.Context) ([]*domain.WorkflowDef, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*domain.WorkflowDef, 0, len(s.defs))
	for _, d := range s.defs {
		out = append(out, d)
	}
	return out, nil
}
