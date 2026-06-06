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

// SavedFlow is an ordered sequence of saved requests fired in order by
// each worker during a compound load test. StepIDs are SavedRequest.ID
// references; resolution happens at run time so editing a saved request
// flows through to any flow that includes it.
type SavedFlow struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	StepIDs   []string `json:"stepIds"`
	CreatedAt string   `json:"createdAt"` // RFC3339
	UpdatedAt string   `json:"updatedAt"` // RFC3339
}

// flowStore mirrors requestStore: JSON-backed CRUD under UserConfigDir,
// mu-protected, atomic tmp+rename writes.
type flowStore struct {
	mu sync.Mutex
}

func newFlowStore() *flowStore {
	return &flowStore{}
}

func (s *flowStore) path() (string, error) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cfgDir, "loadcell")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "flows.json"), nil
}

func (s *flowStore) loadLocked() ([]SavedFlow, error) {
	p, err := s.path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []SavedFlow{}, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return []SavedFlow{}, nil
	}
	var flows []SavedFlow
	if err := json.Unmarshal(data, &flows); err != nil {
		return nil, fmt.Errorf("flows.json malformed: %w", err)
	}
	return flows, nil
}

func (s *flowStore) saveLocked(flows []SavedFlow) error {
	p, err := s.path()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(flows, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// List returns saved flows sorted by UpdatedAt descending.
func (s *flowStore) List() ([]SavedFlow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	flows, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	sort.Slice(flows, func(i, j int) bool {
		return flows[i].UpdatedAt > flows[j].UpdatedAt
	})
	return flows, nil
}

// Upsert creates (when ID is empty) or replaces a flow, returning the
// persisted version with timestamps and ID populated.
func (s *flowStore) Upsert(f SavedFlow) (SavedFlow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	flows, err := s.loadLocked()
	if err != nil {
		return f, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if f.ID == "" {
		f.ID = newID()
		f.CreatedAt = now
	}
	f.UpdatedAt = now
	updated := false
	for i, x := range flows {
		if x.ID == f.ID {
			// Preserve original CreatedAt across updates.
			f.CreatedAt = x.CreatedAt
			flows[i] = f
			updated = true
			break
		}
	}
	if !updated {
		if f.CreatedAt == "" {
			f.CreatedAt = now
		}
		flows = append(flows, f)
	}
	if err := s.saveLocked(flows); err != nil {
		return f, err
	}
	return f, nil
}

// Delete removes a flow by ID. Missing ID is not an error.
func (s *flowStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	flows, err := s.loadLocked()
	if err != nil {
		return err
	}
	filtered := make([]SavedFlow, 0, len(flows))
	for _, f := range flows {
		if f.ID != id {
			filtered = append(filtered, f)
		}
	}
	return s.saveLocked(filtered)
}
