package workflow

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// FSDefStore loads WorkflowDef YAML files from a directory and polls for changes.
// New/changed files are automatically picked up. Deleted files are removed.
// It wraps a MemoryDefStore internally.
type FSDefStore struct {
	dir string
	mem *MemoryDefStore
	log *slog.Logger
}

// NewFSDefStore creates an FSDefStore by loading all YAML files in dir immediately.
func NewFSDefStore(dir string, log *slog.Logger) (*FSDefStore, error) {
	s := &FSDefStore{
		dir: dir,
		mem: NewMemoryDefStore(),
		log: log,
	}
	if err := s.reload(context.Background()); err != nil {
		return nil, err
	}
	return s, nil
}

// Watch starts a background goroutine that polls for file changes at the given interval.
func (s *FSDefStore) Watch(ctx context.Context, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.reload(ctx); err != nil {
					s.log.Warn("fsdefstore reload error", "err", err)
				}
			}
		}
	}()
}

// reload reads all .yaml/.yml files in s.dir and updates the MemoryDefStore.
// Deleted files (names no longer on disk) are removed from the store.
func (s *FSDefStore) reload(ctx context.Context) error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}

	seen := make(map[string]struct{})

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext != ".yaml" && ext != ".yml" {
			continue
		}

		path := filepath.Join(s.dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			s.log.Warn("fsdefstore: read file error", "file", path, "err", err)
			continue
		}

		var def domain.WorkflowDef
		if err := yaml.Unmarshal(data, &def); err != nil {
			s.log.Warn("fsdefstore: yaml parse error", "file", path, "err", err)
			continue
		}

		if def.Name == "" {
			s.log.Warn("fsdefstore: def missing name field, skipping", "file", path)
			continue
		}

		seen[def.Name] = struct{}{}
		if err := s.mem.Put(ctx, def.Name, &def); err != nil {
			s.log.Warn("fsdefstore: store put error", "name", def.Name, "err", err)
		}
	}

	// Remove entries that no longer exist on disk.
	existing, _ := s.mem.List(ctx)
	for _, d := range existing {
		if _, ok := seen[d.Name]; !ok {
			s.mem.mu.Lock()
			delete(s.mem.defs, d.Name)
			s.mem.mu.Unlock()
			s.log.Info("fsdefstore: removed deleted def", "name", d.Name)
		}
	}

	return nil
}

// Get implements DefStore.
func (s *FSDefStore) Get(ctx context.Context, name string) (*domain.WorkflowDef, error) {
	return s.mem.Get(ctx, name)
}

// Put implements DefStore.
func (s *FSDefStore) Put(ctx context.Context, name string, def *domain.WorkflowDef) error {
	return s.mem.Put(ctx, name, def)
}

// List implements DefStore.
func (s *FSDefStore) List(ctx context.Context) ([]*domain.WorkflowDef, error) {
	return s.mem.List(ctx)
}
