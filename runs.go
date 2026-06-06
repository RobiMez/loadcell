package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// runStore mirrors requestStore: JSON-backed CRUD under UserConfigDir,
// atomic writes via tmp+rename, sync.Mutex around file access.
type runStore struct {
	mu sync.Mutex
}

func newRunStore() *runStore { return &runStore{} }

func (s *runStore) path() (string, error) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cfgDir, "loadcell")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "runs.json"), nil
}

func (s *runStore) loadLocked() ([]SavedRun, error) {
	p, err := s.path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []SavedRun{}, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return []SavedRun{}, nil
	}
	var runs []SavedRun
	if err := json.Unmarshal(data, &runs); err != nil {
		return nil, fmt.Errorf("runs.json malformed: %w", err)
	}
	return runs, nil
}

func (s *runStore) saveLocked(runs []SavedRun) error {
	p, err := s.path()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(runs, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// List returns saved runs newest-first.
func (s *runStore) List() ([]SavedRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	runs, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].StartedAt > runs[j].StartedAt })
	return runs, nil
}

// Save creates (if ID empty) or replaces an entry, returning the persisted form.
func (s *runStore) Save(r SavedRun) (SavedRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	runs, err := s.loadLocked()
	if err != nil {
		return r, err
	}
	if r.ID == "" {
		r.ID = newID()
	}
	if r.StartedAt == 0 {
		r.StartedAt = time.Now().UnixMilli()
	}
	updated := false
	for i, x := range runs {
		if x.ID == r.ID {
			runs[i] = r
			updated = true
			break
		}
	}
	if !updated {
		runs = append(runs, r)
	}
	if err := s.saveLocked(runs); err != nil {
		return r, err
	}
	return r, nil
}

// Delete removes the entry with the given ID. Missing ID is not an error.
func (s *runStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	runs, err := s.loadLocked()
	if err != nil {
		return err
	}
	filtered := make([]SavedRun, 0, len(runs))
	for _, r := range runs {
		if r.ID != id {
			filtered = append(filtered, r)
		}
	}
	return s.saveLocked(filtered)
}
