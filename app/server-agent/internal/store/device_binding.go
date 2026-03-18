package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/redis/go-redis/v9"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

const (
	deviceBindingsSnapshotFilename = "device_bindings_snapshot.json"
	redisDeviceBindingPrefix       = "device_binding:"
)

type DeviceBindingStore interface {
	Save(ctx context.Context, binding *domain.DeviceBinding) error
	Get(ctx context.Context, deviceID domain.DeviceID) (*domain.DeviceBinding, error)
	List(ctx context.Context) ([]*domain.DeviceBinding, error)
	Clear(ctx context.Context, deviceID domain.DeviceID) error
}

type MemoryDeviceBindingStore struct {
	mu       sync.RWMutex
	bindings map[domain.DeviceID]*domain.DeviceBinding
}

func NewMemoryDeviceBindingStore() *MemoryDeviceBindingStore {
	return &MemoryDeviceBindingStore{
		bindings: make(map[domain.DeviceID]*domain.DeviceBinding),
	}
}

func (s *MemoryDeviceBindingStore) Save(_ context.Context, binding *domain.DeviceBinding) error {
	if binding == nil {
		return ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bindings[binding.DeviceID] = binding.Clone()
	return nil
}

func (s *MemoryDeviceBindingStore) Get(_ context.Context, deviceID domain.DeviceID) (*domain.DeviceBinding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	binding, ok := s.bindings[deviceID]
	if !ok {
		return nil, ErrNotFound
	}
	return binding.Clone(), nil
}

func (s *MemoryDeviceBindingStore) List(_ context.Context) ([]*domain.DeviceBinding, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := make([]string, 0, len(s.bindings))
	for deviceID := range s.bindings {
		ids = append(ids, string(deviceID))
	}
	sort.Strings(ids)
	out := make([]*domain.DeviceBinding, 0, len(ids))
	for _, id := range ids {
		out = append(out, s.bindings[domain.DeviceID(id)].Clone())
	}
	return out, nil
}

func (s *MemoryDeviceBindingStore) Clear(_ context.Context, deviceID domain.DeviceID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.bindings, deviceID)
	return nil
}

type FileDeviceBindingStore struct {
	mu       sync.Mutex
	path     string
	bindings map[domain.DeviceID]*domain.DeviceBinding
}

func NewFileDeviceBindingStore(dir string) (*FileDeviceBindingStore, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	store := &FileDeviceBindingStore{
		path:     filepath.Join(dir, deviceBindingsSnapshotFilename),
		bindings: make(map[domain.DeviceID]*domain.DeviceBinding),
	}
	raw, err := os.ReadFile(store.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return store, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		return store, nil
	}
	var snapshot []*domain.DeviceBinding
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, err
	}
	for _, binding := range snapshot {
		if binding == nil || binding.DeviceID == "" {
			continue
		}
		store.bindings[binding.DeviceID] = binding.Clone()
	}
	return store, nil
}

func (s *FileDeviceBindingStore) Save(_ context.Context, binding *domain.DeviceBinding) error {
	if binding == nil {
		return ErrNotFound
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bindings[binding.DeviceID] = binding.Clone()
	return s.persistLocked()
}

func (s *FileDeviceBindingStore) Get(_ context.Context, deviceID domain.DeviceID) (*domain.DeviceBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	binding, ok := s.bindings[deviceID]
	if !ok {
		return nil, ErrNotFound
	}
	return binding.Clone(), nil
}

func (s *FileDeviceBindingStore) List(_ context.Context) ([]*domain.DeviceBinding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.bindings))
	for deviceID := range s.bindings {
		ids = append(ids, string(deviceID))
	}
	sort.Strings(ids)
	out := make([]*domain.DeviceBinding, 0, len(ids))
	for _, id := range ids {
		out = append(out, s.bindings[domain.DeviceID(id)].Clone())
	}
	return out, nil
}

func (s *FileDeviceBindingStore) Clear(_ context.Context, deviceID domain.DeviceID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.bindings, deviceID)
	return s.persistLocked()
}

func (s *FileDeviceBindingStore) persistLocked() error {
	ids := make([]string, 0, len(s.bindings))
	for deviceID := range s.bindings {
		ids = append(ids, string(deviceID))
	}
	sort.Strings(ids)
	snapshot := make([]*domain.DeviceBinding, 0, len(ids))
	for _, id := range ids {
		snapshot = append(snapshot, s.bindings[domain.DeviceID(id)].Clone())
	}
	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, raw, 0o644)
}

type RedisDeviceBindingStore struct {
	client *redis.Client
}

func NewRedisDeviceBindingStore(client *redis.Client) *RedisDeviceBindingStore {
	return &RedisDeviceBindingStore{client: client}
}

func (s *RedisDeviceBindingStore) Save(ctx context.Context, binding *domain.DeviceBinding) error {
	if binding == nil {
		return ErrNotFound
	}
	raw, err := json.Marshal(binding)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, redisDeviceBindingKey(binding.DeviceID), raw, 0).Err()
}

func (s *RedisDeviceBindingStore) Get(ctx context.Context, deviceID domain.DeviceID) (*domain.DeviceBinding, error) {
	raw, err := s.client.Get(ctx, redisDeviceBindingKey(deviceID)).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var binding domain.DeviceBinding
	if err := json.Unmarshal(raw, &binding); err != nil {
		return nil, err
	}
	return binding.Clone(), nil
}

func (s *RedisDeviceBindingStore) List(ctx context.Context) ([]*domain.DeviceBinding, error) {
	var cursor uint64
	keys := make([]string, 0)
	for {
		batch, next, err := s.client.Scan(ctx, cursor, redisDeviceBindingPrefix+"*", 100).Result()
		if err != nil {
			return nil, err
		}
		keys = append(keys, batch...)
		cursor = next
		if cursor == 0 {
			break
		}
	}
	sort.Strings(keys)
	out := make([]*domain.DeviceBinding, 0, len(keys))
	for _, key := range keys {
		raw, err := s.client.Get(ctx, key).Bytes()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				continue
			}
			return nil, err
		}
		var binding domain.DeviceBinding
		if err := json.Unmarshal(raw, &binding); err != nil {
			return nil, err
		}
		out = append(out, binding.Clone())
	}
	return out, nil
}

func (s *RedisDeviceBindingStore) Clear(ctx context.Context, deviceID domain.DeviceID) error {
	return s.client.Del(ctx, redisDeviceBindingKey(deviceID)).Err()
}

func redisDeviceBindingKey(deviceID domain.DeviceID) string {
	return redisDeviceBindingPrefix + string(deviceID)
}
