package store

import (
	"context"
	"fmt"
	"sync"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// MemoryTaskStore is a thread-safe in-memory TaskStore.
type MemoryTaskStore struct {
	mu    sync.RWMutex
	tasks map[domain.TaskID]*domain.Task
}

func NewMemoryTaskStore() *MemoryTaskStore {
	return &MemoryTaskStore{tasks: make(map[domain.TaskID]*domain.Task)}
}

func (s *MemoryTaskStore) Save(_ context.Context, t *domain.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := *t
	s.tasks[t.ID] = &cp
	return nil
}

func (s *MemoryTaskStore) Get(_ context.Context, id domain.TaskID) (*domain.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task not found: %s", id)
	}
	cp := *t
	return &cp, nil
}

func (s *MemoryTaskStore) ListByDevice(_ context.Context, deviceID domain.DeviceID) ([]*domain.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*domain.Task
	for _, t := range s.tasks {
		if t.AssignedDevice == deviceID {
			cp := *t
			out = append(out, &cp)
		}
	}
	return out, nil
}

// MemoryWorkflowStateStore is a thread-safe in-memory WorkflowStateStore.
type MemoryWorkflowStateStore struct {
	mu     sync.RWMutex
	states map[stateKey]*domain.WorkflowState
}

type stateKey struct {
	taskID   domain.TaskID
	deviceID domain.DeviceID
}

func NewMemoryWorkflowStateStore() *MemoryWorkflowStateStore {
	return &MemoryWorkflowStateStore{states: make(map[stateKey]*domain.WorkflowState)}
}

func (s *MemoryWorkflowStateStore) Save(_ context.Context, ws *domain.WorkflowState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := cloneWorkflowState(ws)
	s.states[stateKey{ws.TaskID, ws.DeviceID}] = cp
	return nil
}

func (s *MemoryWorkflowStateStore) Get(_ context.Context, taskID domain.TaskID, deviceID domain.DeviceID) (*domain.WorkflowState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ws, ok := s.states[stateKey{taskID, deviceID}]
	if !ok {
		return nil, fmt.Errorf("workflow state not found: task=%s device=%s", taskID, deviceID)
	}
	return cloneWorkflowState(ws), nil
}

func (s *MemoryWorkflowStateStore) ListActiveByDevice(_ context.Context, deviceID domain.DeviceID) ([]*domain.WorkflowState, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []*domain.WorkflowState
	for k, ws := range s.states {
		if k.deviceID == deviceID && ws.CurrentNode != domain.NodeKindTerminal {
			out = append(out, cloneWorkflowState(ws))
		}
	}
	return out, nil
}

func cloneWorkflowState(ws *domain.WorkflowState) *domain.WorkflowState {
	cp := *ws
	cp.Artifacts = make(map[string]string, len(ws.Artifacts))
	for k, v := range ws.Artifacts {
		cp.Artifacts[k] = v
	}
	return &cp
}
