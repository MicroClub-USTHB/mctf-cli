// Package instancer manages per-team challenge instances.
//
// The store is the persistence layer: it tracks live instances on
// disk so the daemon survives restarts. Lifecycle planning (project
// name, port allocation, env) lives in lifecycle.go. The HTTP daemon
// and CLI subcommands sit on top of both.
package instancer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Instance is one per-team spawn of a challenge.
type Instance struct {
	// ID is the unique handle, format "<slug>-<team>-<short>". Used as
	// the docker compose project name and as the URL token.
	ID string `json:"id"`
	// ChallengeSlug is "<category>/<slug>" — the parent challenge.
	ChallengeSlug string `json:"challenge_slug"`
	// TeamID is the per-event team identifier (any string the
	// platform integration chooses).
	TeamID string `json:"team_id"`
	// ProjectName is the docker compose project name (=== ID for v0).
	ProjectName string `json:"project_name"`
	// Containers carries the docker container ID for each compose
	// service after spawn — used by destroy + healthcheck.
	Containers []string `json:"containers,omitempty"`
	// Ports map internal container ports to allocated host ports.
	Ports map[int]int `json:"ports,omitempty"`
	// Flag is the dynamic per-team flag value (empty when the
	// challenge uses a static flag).
	Flag string `json:"flag,omitempty"`

	SpawnedAt    time.Time `json:"spawned_at"`
	ExpiresAt    time.Time `json:"expires_at"`
	LastExtended time.Time `json:"last_extended,omitempty"`
}

// Store persists instances to a JSON file on disk.
//
// Operations are mutex-serialized; writes go through a temp file +
// rename so a crash mid-write can't corrupt the store.
type Store struct {
	path  string
	mu    sync.Mutex
	cache map[string]Instance
}

// NewStore opens (or creates) the store at path. Loading a missing
// file is not an error — the store starts empty.
func NewStore(path string) (*Store, error) {
	s := &Store{path: path, cache: map[string]Instance{}}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(data, &s.cache); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return s, nil
}

// Path returns the on-disk store path.
func (s *Store) Path() string { return s.path }

// List returns a snapshot of all instances, sorted by ID.
func (s *Store) List() []Instance {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Instance, 0, len(s.cache))
	for _, v := range s.cache {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Get returns the instance with the given ID, if present.
func (s *Store) Get(id string) (Instance, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.cache[id]
	return v, ok
}

// ByTeamAndChallenge returns the live instance for (team, slug), or
// false if no spawn exists. Used to enforce max_per_team and to
// short-circuit duplicate spawns.
func (s *Store) ByTeamAndChallenge(team, slug string) (Instance, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range s.cache {
		if v.TeamID == team && v.ChallengeSlug == slug {
			return v, true
		}
	}
	return Instance{}, false
}

// Put inserts or replaces an instance and persists.
func (s *Store) Put(i Instance) error {
	if i.ID == "" {
		return fmt.Errorf("instance has no ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cache[i.ID] = i
	return s.persistLocked()
}

// Delete removes an instance by ID. Returns nil even when the ID
// isn't present — destroy idempotency.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cache, id)
	return s.persistLocked()
}

// Expired returns instances with ExpiresAt <= now, sorted by ExpiresAt.
// Doesn't delete — callers reap explicitly so they can also tear down
// the underlying compose project.
func (s *Store) Expired(now time.Time) []Instance {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Instance
	for _, v := range s.cache {
		if !v.ExpiresAt.IsZero() && !v.ExpiresAt.After(now) {
			out = append(out, v)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ExpiresAt.Before(out[j].ExpiresAt) })
	return out
}

// AllocatedHostPorts returns the set of host ports currently bound by
// any live instance. Used by the lifecycle planner to avoid collisions.
func (s *Store) AllocatedHostPorts() map[int]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[int]bool{}
	for _, v := range s.cache {
		for _, hp := range v.Ports {
			if hp > 0 {
				out[hp] = true
			}
		}
	}
	return out
}

// persistLocked rewrites the store file atomically. Caller must hold s.mu.
func (s *Store) persistLocked() error {
	body, err := json.MarshalIndent(s.cache, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("rename %s -> %s: %w", tmp, s.path, err)
	}
	return nil
}
