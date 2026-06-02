package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// SavedRequest is a Postman-style stored HTTP request template the user can
// reuse to drive load tests.
type SavedRequest struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Method    string     `json:"method"`
	URL       string     `json:"url"`
	Headers   []HeaderKV `json:"headers"`
	Body      string     `json:"body"`
	CreatedAt string     `json:"createdAt"` // RFC3339
	UpdatedAt string     `json:"updatedAt"` // RFC3339
}

// HeaderKV preserves header order and lets duplicates exist on disk. The
// engine flattens to a map for application.
type HeaderKV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// requestStore is a small JSON-backed CRUD store under UserConfigDir.
// Concurrency-safe via mu; file writes are atomic via tmp+rename.
type requestStore struct {
	mu sync.Mutex
}

func newRequestStore() *requestStore {
	return &requestStore{}
}

func (s *requestStore) path() (string, error) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cfgDir, "loadcell")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "requests.json"), nil
}

func (s *requestStore) loadLocked() ([]SavedRequest, error) {
	p, err := s.path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []SavedRequest{}, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return []SavedRequest{}, nil
	}
	var reqs []SavedRequest
	if err := json.Unmarshal(data, &reqs); err != nil {
		return nil, fmt.Errorf("requests.json malformed: %w", err)
	}
	return reqs, nil
}

func (s *requestStore) saveLocked(reqs []SavedRequest) error {
	p, err := s.path()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(reqs, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// List returns all saved requests sorted by UpdatedAt descending.
func (s *requestStore) List() ([]SavedRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	reqs, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	sort.Slice(reqs, func(i, j int) bool {
		return reqs[i].UpdatedAt > reqs[j].UpdatedAt
	})
	return reqs, nil
}

// Upsert creates (when req.ID is empty) or updates (when req.ID matches an
// existing entry). It returns the persisted version with timestamps and ID
// filled in.
func (s *requestStore) Upsert(req SavedRequest) (SavedRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	reqs, err := s.loadLocked()
	if err != nil {
		return req, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if req.ID == "" {
		req.ID = newID()
		req.CreatedAt = now
	}
	req.UpdatedAt = now
	if req.CreatedAt == "" {
		req.CreatedAt = now
	}
	updated := false
	for i, r := range reqs {
		if r.ID == req.ID {
			// Preserve original CreatedAt when updating.
			req.CreatedAt = r.CreatedAt
			reqs[i] = req
			updated = true
			break
		}
	}
	if !updated {
		reqs = append(reqs, req)
	}
	if err := s.saveLocked(reqs); err != nil {
		return req, err
	}
	return req, nil
}

// Delete removes the entry with the given ID. Missing ID is not an error.
func (s *requestStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	reqs, err := s.loadLocked()
	if err != nil {
		return err
	}
	filtered := make([]SavedRequest, 0, len(reqs))
	for _, r := range reqs {
		if r.ID != id {
			filtered = append(filtered, r)
		}
	}
	return s.saveLocked(filtered)
}

// newID returns a short, URL-safe identifier. 9 random bytes → 12 base64
// characters, ample for a local request store.
func newID() string {
	var b [9]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fall back to nanosecond timestamp — never panic on local IO.
		return fmt.Sprintf("t%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
