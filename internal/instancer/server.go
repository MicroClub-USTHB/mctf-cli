package instancer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

// Server is the HTTP daemon. It owns the store, runner, and challenge
// catalog; routes are bearer-token authenticated.
type Server struct {
	cfg     *config.Config
	store   *Store
	runner  Runner
	tokens  map[string]struct{}
	chalIdx map[string]*challenge.Challenge // "<cat>/<slug>" -> challenge
	mu      sync.Mutex                       // serializes Spawn for the same (team, slug)
}

// Options configures the Server.
type Options struct {
	// Tokens are the bearer tokens accepted on every authed route.
	// At least one token must be set; an empty set means "deny all".
	Tokens []string
}

// NewServer constructs a Server from the given config + challenges +
// runner. challs is snapshotted: changes after construction are not
// reflected (operators restart the daemon on event-repo changes).
func NewServer(cfg *config.Config, store *Store, runner Runner, challs []*challenge.Challenge, opts Options) *Server {
	idx := make(map[string]*challenge.Challenge, len(challs))
	for _, c := range challs {
		idx[c.CategoryDir+"/"+c.Slug] = c
	}
	tok := make(map[string]struct{}, len(opts.Tokens))
	for _, t := range opts.Tokens {
		t = strings.TrimSpace(t)
		if t != "" {
			tok[t] = struct{}{}
		}
	}
	return &Server{cfg: cfg, store: store, runner: runner, tokens: tok, chalIdx: idx}
}

// Handler returns the http.Handler with all routes registered.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /instances", s.auth(s.listInstances))
	mux.HandleFunc("GET /instances/{id}", s.auth(s.getInstance))
	mux.HandleFunc("POST /spawn", s.auth(s.spawn))
	mux.HandleFunc("POST /destroy/{id}", s.auth(s.destroy))
	mux.HandleFunc("POST /extend/{id}", s.auth(s.extend))
	return mux
}

// auth wraps a handler to require a bearer token from s.tokens.
func (s *Server) auth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hdr := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(hdr, prefix) {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		tok := strings.TrimSpace(strings.TrimPrefix(hdr, prefix))
		if _, ok := s.tokens[tok]; !ok {
			writeError(w, http.StatusUnauthorized, "invalid token")
			return
		}
		h(w, r)
	}
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"instances": len(s.store.List()),
	})
}

func (s *Server) listInstances(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.store.List())
}

func (s *Server) getInstance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	inst, ok := s.store.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "no instance with id "+id)
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

// spawnRequest is the POST /spawn body.
type spawnRequest struct {
	TeamID        string `json:"team_id"`
	ChallengeSlug string `json:"challenge_slug"`
	FlagOverride  string `json:"flag,omitempty"`
}

func (s *Server) spawn(w http.ResponseWriter, r *http.Request) {
	var req spawnRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "decode body: "+err.Error())
		return
	}
	if req.TeamID == "" || req.ChallengeSlug == "" {
		writeError(w, http.StatusBadRequest, "team_id and challenge_slug are required")
		return
	}
	c, ok := s.chalIdx[req.ChallengeSlug]
	if !ok {
		writeError(w, http.StatusNotFound, "no challenge at "+req.ChallengeSlug)
		return
	}
	if c.Archetype != challenge.ArchetypeInstanced {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("%s is not instanced (archetype=%s)", req.ChallengeSlug, c.Archetype))
		return
	}

	// Serialize spawn to keep idempotency-by-(team,slug) honest under
	// concurrent requests.
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.store.ByTeamAndChallenge(req.TeamID, req.ChallengeSlug); ok {
		writeJSON(w, http.StatusOK, existing)
		return
	}

	spec, err := PlanSpawn(s.cfg, c, req.TeamID, s.store.AllocatedHostPorts(),
		SpawnOptions{FlagOverride: req.FlagOverride})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	containers, err := s.runner.Up(ctx, *spec)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "up failed: "+err.Error())
		return
	}

	inst := Instance{
		ID:            spec.ID,
		ChallengeSlug: spec.ChallengeSlug,
		TeamID:        spec.TeamID,
		ProjectName:   spec.ID,
		Containers:    containers,
		Ports:         spec.Ports,
		Flag:          spec.Env["FLAG"],
		SpawnedAt:     time.Now().UTC(),
		ExpiresAt:     spec.ExpiresAt,
	}
	if err := s.store.Put(inst); err != nil {
		writeError(w, http.StatusInternalServerError, "persist: "+err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, inst)
}

func (s *Server) destroy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	inst, ok := s.store.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "no instance with id "+id)
		return
	}
	c, ok := s.chalIdx[inst.ChallengeSlug]
	if !ok {
		writeError(w, http.StatusNotFound, "challenge "+inst.ChallengeSlug+" no longer in catalog")
		return
	}

	spec := SpawnSpec{ID: inst.ID, ComposeFile: c.ChallengeDir + "/docker-compose.yml"}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	if err := s.runner.Down(ctx, spec, false); err != nil {
		writeError(w, http.StatusInternalServerError, "down failed: "+err.Error())
		return
	}
	if err := s.store.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, "store delete: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "destroyed": true})
}

// extendRequest is the POST /extend body.
type extendRequest struct {
	BySeconds int `json:"by_seconds"`
}

func (s *Server) extend(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	inst, ok := s.store.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "no instance with id "+id)
		return
	}
	var req extendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, http.ErrBodyReadAfterClose) {
		// Empty body is OK — fall through to default duration.
		if r.ContentLength > 0 {
			writeError(w, http.StatusBadRequest, "decode body: "+err.Error())
			return
		}
	}
	by := time.Duration(req.BySeconds) * time.Second
	if by <= 0 {
		by = 15 * time.Minute
	}
	now := time.Now().UTC()
	if inst.ExpiresAt.Before(now) {
		inst.ExpiresAt = now.Add(by)
	} else {
		inst.ExpiresAt = inst.ExpiresAt.Add(by)
	}
	inst.LastExtended = now
	if err := s.store.Put(inst); err != nil {
		writeError(w, http.StatusInternalServerError, "persist: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]any{"error": msg})
}
