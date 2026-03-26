package workflow

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/autosdk/ppp/server-agent/internal/domain"
)

// FSDefStore loads WorkflowDef YAML files from a directory and polls for changes.
// New/changed files are automatically picked up. Deleted files are removed.
// It wraps a DefStore internally (default: MemoryDefStore).
type FSDefStore struct {
	dir         string
	mem         DefStore
	log         *slog.Logger
	fileToDefName map[string]string // maps file path → last successfully loaded def name
}

// NewFSDefStore creates an FSDefStore by loading all YAML files in dir immediately.
// Uses a MemoryDefStore as the backing store.
func NewFSDefStore(dir string, log *slog.Logger) (*FSDefStore, error) {
	return NewFSDefStoreWithBacking(dir, NewMemoryDefStore(), log)
}

// NewFSDefStoreWithBacking creates an FSDefStore with a caller-supplied backing DefStore.
// Useful in tests to inject a custom or pre-seeded backing store.
func NewFSDefStoreWithBacking(dir string, backing DefStore, log *slog.Logger) (*FSDefStore, error) {
	s := &FSDefStore{
		dir:           dir,
		mem:           backing,
		log:           log,
		fileToDefName: make(map[string]string),
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

// ReloadNow triggers an immediate reload of all YAML files in the directory.
// It is safe to call concurrently with Watch-driven reloads.
func (s *FSDefStore) ReloadNow(ctx context.Context) error {
	return s.reload(ctx)
}

// reload reads all .yaml/.yml files in s.dir (recursively, including subdirectories)
// and updates the backing DefStore. Deleted files are removed from the store.
//
// Retention policy: if a file fails to load (read, parse, or validate error) and
// a previous valid version exists for that file, the old version is kept in the
// store and the file's name is added to seen so the deletion sweep leaves it intact.
func (s *FSDefStore) reload(ctx context.Context) error {
	seen := make(map[string]struct{})

	err := filepath.WalkDir(s.dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			s.log.Warn("fsdefstore: walk error", "path", path, "err", walkErr)
			return nil // continue walking
		}
		if d.IsDir() {
			return nil // descend into subdirectories
		}
		ext := filepath.Ext(d.Name())
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			s.log.Warn("fsdefstore: read file error", "file", path, "err", err)
			if prevName, ok := s.fileToDefName[path]; ok {
				s.log.Warn("fsdefstore: retaining previous version", "file", path, "name", prevName)
				seen[prevName] = struct{}{}
			}
			return nil
		}

		def, err := unmarshalWorkflowDef(data)
		if err != nil {
			s.log.Warn("fsdefstore: yaml parse error", "file", path, "err", err)
			if prevName, ok := s.fileToDefName[path]; ok {
				s.log.Warn("fsdefstore: retaining previous version", "file", path, "name", prevName)
				seen[prevName] = struct{}{}
			}
			return nil
		}

		if def.Name == "" {
			s.log.Warn("fsdefstore: def missing name field, skipping", "file", path)
			return nil
		}

		if err := Validate(&def); err != nil {
			s.log.Warn("fsdefstore: invalid workflow def, retaining previous version",
				"file", path, "name", def.Name, "err", err)
			if prevName, ok := s.fileToDefName[path]; ok {
				seen[prevName] = struct{}{}
			}
			return nil
		}

		s.fileToDefName[path] = def.Name
		seen[def.Name] = struct{}{}
		if err := s.mem.Put(ctx, def.Name, &def); err != nil {
			s.log.Warn("fsdefstore: store put error", "name", def.Name, "err", err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	// Remove entries that no longer exist on disk.
	existing, _ := s.mem.List(ctx)
	for _, d := range existing {
		if _, ok := seen[d.Name]; !ok {
			if err := s.mem.Delete(ctx, d.Name); err != nil {
				s.log.Warn("fsdefstore: delete stale def error", "name", d.Name, "err", err)
			}
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

// Delete implements DefStore.
func (s *FSDefStore) Delete(ctx context.Context, name string) error {
	return s.mem.Delete(ctx, name)
}

// List implements DefStore.
func (s *FSDefStore) List(ctx context.Context) ([]*domain.WorkflowDef, error) {
	return s.mem.List(ctx)
}
