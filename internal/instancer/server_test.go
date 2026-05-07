package instancer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MicroClub-USTHB/mctf-cli/internal/challenge"
	"github.com/MicroClub-USTHB/mctf-cli/internal/config"
)

func newTestServer(t *testing.T) (*Server, *httptest.Server, *FakeRunner) {
	t.Helper()
	cfg := &config.Config{
		Version:   1,
		Event:     config.Event{Slug: "evt"},
		Instancer: &config.Instancer{DefaultTimeout: 600},
	}
	store, err := NewStore(filepath.Join(t.TempDir(), "instances.json"))
	if err != nil {
		t.Fatal(err)
	}
	runner := NewFakeRunner()
	c := instancedChall("cub3", "web", 8000)
	srv := NewServer(cfg, store, runner, []*challenge.Challenge{c}, Options{Tokens: []string{"secret"}})
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)
	return srv, httpSrv, runner
}

func authedPOST(t *testing.T, base, path, token, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("POST", base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func authedGET(t *testing.T, base, path, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", base+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestServer_Healthz_NoAuth(t *testing.T) {
	_, http, _ := newTestServer(t)
	resp, err := http.Client().Get(http.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestServer_RequiresAuth(t *testing.T) {
	_, http, _ := newTestServer(t)
	resp := authedGET(t, http.URL, "/instances", "")
	if resp.StatusCode != 401 {
		t.Errorf("missing token should be 401, got %d", resp.StatusCode)
	}
	resp = authedGET(t, http.URL, "/instances", "wrong")
	if resp.StatusCode != 401 {
		t.Errorf("wrong token should be 401, got %d", resp.StatusCode)
	}
}

func TestServer_Spawn(t *testing.T) {
	srv, http, runner := newTestServer(t)
	body := `{"team_id": "t1", "challenge_slug": "web/cub3"}`
	resp := authedPOST(t, http.URL, "/spawn", "secret", body)
	if resp.StatusCode != 201 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d body=%s", resp.StatusCode, raw)
	}
	var inst Instance
	if err := json.NewDecoder(resp.Body).Decode(&inst); err != nil {
		t.Fatal(err)
	}
	if inst.TeamID != "t1" || inst.ChallengeSlug != "web/cub3" {
		t.Errorf("spawn returned wrong instance: %+v", inst)
	}
	if len(runner.UpCalls) != 1 {
		t.Errorf("Up should have been called once, got %d", len(runner.UpCalls))
	}
	if len(srv.store.List()) != 1 {
		t.Errorf("store should have 1 entry, got %d", len(srv.store.List()))
	}
}

func TestServer_SpawnIdempotent(t *testing.T) {
	_, http, runner := newTestServer(t)
	body := `{"team_id": "t1", "challenge_slug": "web/cub3"}`
	for i := 0; i < 3; i++ {
		_ = authedPOST(t, http.URL, "/spawn", "secret", body)
	}
	if len(runner.UpCalls) != 1 {
		t.Errorf("Up should run once across 3 spawns for the same team, got %d", len(runner.UpCalls))
	}
}

func TestServer_SpawnRejectsStaticChallenge(t *testing.T) {
	cfg := &config.Config{Version: 1, Event: config.Event{Slug: "evt"}}
	store, _ := NewStore(filepath.Join(t.TempDir(), "i.json"))
	runner := NewFakeRunner()
	c := instancedChall("rsa", "crypto", 8000)
	c.Archetype = challenge.ArchetypeStatic
	c.Manifest.Deploy = nil
	srv := NewServer(cfg, store, runner, []*challenge.Challenge{c}, Options{Tokens: []string{"x"}})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	resp := authedPOST(t, ts.URL, "/spawn", "x", `{"team_id":"t","challenge_slug":"crypto/rsa"}`)
	if resp.StatusCode != 400 {
		raw, _ := io.ReadAll(resp.Body)
		t.Errorf("static spawn should 400, got %d body=%s", resp.StatusCode, raw)
	}
}

func TestServer_SpawnUnknownChallenge_404(t *testing.T) {
	_, http, _ := newTestServer(t)
	resp := authedPOST(t, http.URL, "/spawn", "secret", `{"team_id":"t","challenge_slug":"nope/x"}`)
	if resp.StatusCode != 404 {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestServer_DestroyAfterSpawn(t *testing.T) {
	srv, http, runner := newTestServer(t)
	resp := authedPOST(t, http.URL, "/spawn", "secret",
		`{"team_id":"t1","challenge_slug":"web/cub3"}`)
	var inst Instance
	_ = json.NewDecoder(resp.Body).Decode(&inst)

	resp = authedPOST(t, http.URL, "/destroy/"+inst.ID, "secret", "")
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("destroy status=%d body=%s", resp.StatusCode, raw)
	}
	if len(runner.DownCalls) != 1 {
		t.Errorf("Down calls = %v", runner.DownCalls)
	}
	if _, ok := srv.store.Get(inst.ID); ok {
		t.Errorf("store still has instance %s after destroy", inst.ID)
	}
}

func TestServer_DestroyMissing_404(t *testing.T) {
	_, http, _ := newTestServer(t)
	resp := authedPOST(t, http.URL, "/destroy/nope", "secret", "")
	if resp.StatusCode != 404 {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

func TestServer_DestroyDownFails_500(t *testing.T) {
	srv, http, runner := newTestServer(t)
	resp := authedPOST(t, http.URL, "/spawn", "secret",
		`{"team_id":"t1","challenge_slug":"web/cub3"}`)
	var inst Instance
	_ = json.NewDecoder(resp.Body).Decode(&inst)
	runner.ErrOnDown = map[string]error{inst.ID: errors.New("docker exploded")}

	resp = authedPOST(t, http.URL, "/destroy/"+inst.ID, "secret", "")
	if resp.StatusCode != 500 {
		t.Errorf("status = %d", resp.StatusCode)
	}
	// Instance should remain in the store so the operator can retry.
	if _, ok := srv.store.Get(inst.ID); !ok {
		t.Errorf("instance should still be in store after failed down")
	}
}

func TestServer_Extend(t *testing.T) {
	_, http, _ := newTestServer(t)
	resp := authedPOST(t, http.URL, "/spawn", "secret",
		`{"team_id":"t1","challenge_slug":"web/cub3"}`)
	var inst Instance
	_ = json.NewDecoder(resp.Body).Decode(&inst)
	original := inst.ExpiresAt

	resp = authedPOST(t, http.URL, "/extend/"+inst.ID, "secret", `{"by_seconds": 600}`)
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	var got Instance
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if !got.ExpiresAt.After(original) {
		t.Errorf("extend should bump ExpiresAt: was %v now %v", original, got.ExpiresAt)
	}
}

func TestServer_ListInstances(t *testing.T) {
	_, http, _ := newTestServer(t)
	for _, team := range []string{"t1", "t2", "t3"} {
		body := `{"team_id":"` + team + `","challenge_slug":"web/cub3"}`
		_ = authedPOST(t, http.URL, "/spawn", "secret", body)
	}
	resp := authedGET(t, http.URL, "/instances", "secret")
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	var got []Instance
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if len(got) != 3 {
		t.Errorf("want 3 instances, got %d", len(got))
	}
}

// Ensure the server builds with the bytes package — unused-import guard.
var _ = bytes.NewReader

// Suppress unused imports for context if compiler ever complains.
var _ = context.Background
